package main

import (
	"context"
	"fmt"
	"log"
	"ncloud-api/handlers/directories"
	"ncloud-api/handlers/files"
	"ncloud-api/handlers/search"
	"ncloud-api/handlers/user"
	"ncloud-api/middleware/auth"
	"ncloud-api/middleware/cors"
	"ncloud-api/utils/helper"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/meilisearch/meilisearch-go"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var DbHost string
var DbPassword string
var DbUser string
var DbName string
var MeiliApiKey string

func health(c *gin.Context) {
	c.JSON(http.StatusOK, map[string]string{"ok": "true"})
}

// Run if you need to sync meilisearch with primary database
func initMeiliSearch(db *mongo.Database, meiliClient *meilisearch.Client) {
	filterableAttributes := []string{
		"name",
		"_id",
		"parent_directory",
		"user",
	}

	searchableAttributes := []string{
		"name",
	}

	opts := options.Find().SetProjection(bson.D{
		{Key: "_id", Value: 1}, {Key: "name", Value: 1},
		{Key: "parent_directory", Value: 1}, {Key: "user", Value: 1}},
	)

	// Add directories to meilisearch
	cursor, err := db.Collection("directories").Find(context.TODO(), bson.D{}, opts)
	if err != nil {
		log.Panic(err)
	}
	var results []bson.M
	if err = cursor.All(context.TODO(), &results); err != nil {
		log.Fatal(err)
	}

	_, err = meiliClient.Index("directories").AddDocuments(results)
	if err != nil {
		panic(err)
	}

	// Add files to meilisearch
	opts = options.Find().SetProjection(bson.D{
		{Key: "_id", Value: 1}, {Key: "name", Value: 1},
		{Key: "parent_directory", Value: 1}, {Key: "user", Value: 1}, {Key: "type", Value: 1}},
	)

	cursor, err = db.Collection("files").Find(context.TODO(), bson.D{}, opts)
	if err != nil {
		log.Panic(err)
	}

	if err = cursor.All(context.TODO(), &results); err != nil {
		log.Panic(err)
	}

	_, err = meiliClient.Index("files").AddDocuments(results)
	if err != nil {
		log.Panic()
	}

	if _, err := meiliClient.Index("files").UpdateFilterableAttributes(&filterableAttributes); err != nil {
		log.Println(err)
	}
	if _, err = meiliClient.Index("directories").UpdateFilterableAttributes(&filterableAttributes); err != nil {
		log.Println(err)
	}

	if _, err = meiliClient.Index("directories").UpdateSearchableAttributes(&searchableAttributes); err != nil {
		log.Println(err)
	}

	if _, err = meiliClient.Index("files").UpdateSearchableAttributes(&searchableAttributes); err != nil {
		log.Println(err)
	}

}

func main() {
	// Setup database
	DbHost = helper.GetEnv("DB_HOST", "localhost:27017")
	DbPassword = helper.GetEnv("DB_PASSWORD", "rootpass")
	DbUser = helper.GetEnv("DB_USER", "rootuser")
	DbName = helper.GetEnv("DB_NAME", "ncloud-api")
	MeiliApiKey = helper.GetEnv("MEILI_MASTER_KEY", "meili_master_key")

	client, err := mongo.NewClient(options.Client().ApplyURI(fmt.Sprintf("mongodb://%s:%s@%s", DbUser, DbPassword, DbHost)))
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

	defer cancel()

	err = client.Connect(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Disconnect(ctx)

	db := client.Database(DbName)

	// meilisearch setup
	meiliClient := meilisearch.NewClient(meilisearch.ClientConfig{
		Host:   "http://localhost:7700",
		APIKey: MeiliApiKey,
	})

	//initMeiliSearch(db, meiliClient)

	// Handlers
	userHandler := user.Handler{Db: db, SearchDb: meiliClient}
	fileHandler := files.Handler{Db: db, SearchDb: meiliClient}
	directoryHandler := directories.Handler{Db: db, SearchDb: meiliClient}
	searchHandler := search.Handler{Db: meiliClient}

	// Setup router
	router := gin.Default()

	router.Use(cors.Middleware())

	router.GET("/api/health", health)
	router.POST("/api/register", userHandler.Register)
	router.POST("/api/login", userHandler.Login)
	router.GET("/api/token/refresh", userHandler.RefreshToken)
	router.POST("/api/files/delete", fileHandler.DeleteFiles)
	router.POST("/api/directories/move", directoryHandler.ChangeDirectory)

	router.MaxMultipartMemory = 8 << 20 // 8 MiB

	authorized := router.Group("/")
	authorized.Use(auth.Auth())
	{
		authorized.GET("/api/directories/search", searchHandler.FindDirectoriesAndFiles)

		authorized.GET("/api/directories", directoryHandler.GetDirectoryWithFiles)
		authorized.GET("/api/directories/:id", directoryHandler.GetDirectoryWithFiles)
		authorized.POST("/api/directories/delete", directoryHandler.DeleteDirectories)

		directoryGroup := authorized.Group("/api/")
		directoryGroup.Use(auth.DirectoryAuth())
		{
			directoryGroup.POST("directories/:id", directoryHandler.CreateDirectory)
			directoryGroup.POST("upload/:id", fileHandler.Upload)
			directoryGroup.PATCH("directories/:id", directoryHandler.ModifyDirectory)
			directoryGroup.DELETE("directories/:id", directoryHandler.DeleteDirectory)

		}

		fileGroup := authorized.Group("/")
		fileGroup.Use(auth.FileAuth())
		{
			fileGroup.GET("/files/:id", fileHandler.GetFile)
			fileGroup.DELETE("/api/files/:id", fileHandler.DeleteFile)
			fileGroup.PATCH("/api/files/:id", fileHandler.UpdateFile)
		}

	}

	router.Run("localhost:8080")
}
