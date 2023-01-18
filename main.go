package main

import (
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"log"
	"ncloud-api/handlers/upload"
	"ncloud-api/handlers/user"
	"ncloud-api/middleware/auth"
	"ncloud-api/utils/helper"
	"net/http"
	"time"
)

var DbHost string
var DbPassword string
var DbUser string
var DbName string

func health(c *gin.Context) {
	c.IndentedJSON(http.StatusOK, map[string]string{"ok": "true"})
}

func main() {
	// Setup database
	DbHost = helper.GetEnv("DB_HOST", "localhost:27017")
	DbPassword = helper.GetEnv("DB_PASSWORD", "rootpass")
	DbUser = helper.GetEnv("DB_USER", "rootuser")
	DbName = helper.GetEnv("DB_NAME", "ncloud-api")

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

	// Handlers
	userHandler := user.UserHandler{Db: db}
	fileHandler := upload.FileHandler{Db: db}

	// Setup router
	router := gin.Default()

	router.GET("/api/health", health)
	router.POST("/api/register", userHandler.Register)
	router.POST("/api/login", userHandler.Login)




	router.MaxMultipartMemory = 8 << 20  // 8 MiB


	authorized := router.Group("/")
	authorized.Use(auth.Auth())
	{
		authorized.POST("/api/upload", fileHandler.Upload)
		authorized.POST("/api/createDirectory", fileHandler.CreateDirectory)
	}


	router.Run("localhost:8080")
}
