package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/autotls"
	"github.com/gin-gonic/gin"
	"github.com/meilisearch/meilisearch-go"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/crypto/acme/autocert"

	"ncloud-api/handlers/directories"
	"ncloud-api/handlers/files"
	"ncloud-api/handlers/search"
	"ncloud-api/handlers/user"
	"ncloud-api/middleware/auth"
	"ncloud-api/middleware/cors"
	"ncloud-api/utils/helper"
)

var (
	DbHost      string
	DbPassword  string
	DbUser      string
	DbName      string
	MeiliApiKey string
	MeiliHost   string
	Mode        string
)

func health(c *gin.Context) {
	c.JSON(http.StatusOK, map[string]string{"ok": "true"})
}

// Run if you need to sync meilisearch with primary database
func initMeiliSearch(db *mongo.Database, meiliClient *meilisearch.Client) {
	_, err := db.Collection("user").Indexes().CreateOne(context.TODO(), mongo.IndexModel{
		Keys:    bson.D{{Key: "username", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		log.Println(err)
	}

	if r, err := meiliClient.Index("files").DeleteAllDocuments(); err != nil {
		log.Panic(r, err)
	}
	if r, err := meiliClient.Index("directories").DeleteAllDocuments(); err != nil {
		log.Panic(r, err)
	}

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
		{Key: "_id", Value: 1},
		{Key: "name", Value: 1},
		{Key: "parent_directory", Value: 1},
		{Key: "user", Value: 1},
	},
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

	if len(results) > 0 {
		_, err = meiliClient.Index("directories").AddDocuments(results)
		if err != nil {
			panic(err)
		}
	}

	// Add files to meilisearch
	opts = options.Find().SetProjection(bson.D{
		{Key: "_id", Value: 1},
		{Key: "name", Value: 1},
		{Key: "parent_directory", Value: 1},
		{Key: "user", Value: 1},
		{Key: "type", Value: 1},
	},
	)

	cursor, err = db.Collection("files").Find(context.TODO(), bson.D{}, opts)
	if err != nil {
		log.Panic(err)
	}

	if err = cursor.All(context.TODO(), &results); err != nil {
		log.Panic(err)
	}

	if len(results) > 0 {
		_, err = meiliClient.Index("files").AddDocuments(results)
		if err != nil {
			log.Panic()
		}
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
	MeiliHost = helper.GetEnv("MEILI_HOST", "http://localhost:7700")
	Mode = helper.GetEnv("RUN_MODE", "debug")

	mongoClient, err := mongo.NewClient(
		options.Client().ApplyURI(fmt.Sprintf("mongodb://%s:%s@%s", DbUser, DbPassword, DbHost)),
	)
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

	defer cancel()

	err = mongoClient.Connect(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer mongoClient.Disconnect(ctx)

	meiliClient := meilisearch.NewClient(meilisearch.ClientConfig{
		Host:   MeiliHost,
		APIKey: MeiliApiKey,
	})

	db := mongoClient.Database(DbName)
	initMeiliSearch(db, meiliClient)

	userHandler := user.Handler{Db: db, SearchDb: meiliClient}
	fileHandler := files.Handler{Db: db, SearchDb: meiliClient}
	directoryHandler := directories.Handler{Db: db, SearchDb: meiliClient}
	searchHandler := search.Handler{Db: meiliClient}

	gin.SetMode(Mode)
	router := gin.Default()

	router.Use(cors.Middleware())

	router.GET("/api/health", health)
	router.POST("/api/register", userHandler.Register)
	router.POST("/api/login", userHandler.Login)
	router.GET("/api/token/refresh", userHandler.RefreshToken)
	router.POST("/api/files/delete", fileHandler.DeleteFiles)
	router.POST("/api/files/move", fileHandler.ChangeDirectory)

	router.MaxMultipartMemory = 8 << 20 // 8 MiB

	authorized := router.Group("/")
	authorized.Use(auth.Auth())
	{
		authorized.GET("/api/directories/search", searchHandler.FindDirectoriesAndFiles)

		authorized.GET("/api/directories", directoryHandler.GetDirectoryWithFiles)
		authorized.GET("/api/directories/:id", directoryHandler.GetDirectoryWithFiles)
		authorized.POST("/api/directories/delete", directoryHandler.DeleteDirectories)
		authorized.POST("/api/directories/move", directoryHandler.ChangeDirectory)
		authorized.POST("/api/directories/restore", directoryHandler.RestoreDirectories)
		authorized.POST("/api/files/restore", fileHandler.RestoreFiles)

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

	if gin.Mode() == gin.ReleaseMode {
		m := autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist("api.ncloudapp.com", "api2.ncloudapp.com"),
		}
		log.Fatal(autotls.RunWithManager(router, &m))
	} else {
		router.Run("0.0.0.0:8080")
	}
}
