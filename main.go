package main

import (
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"log"
	"ncloud-api/utils/helper"
	"net/http"
	"time"
)

var DbHost string
var DbPassword string
var DbUser string

func health(c *gin.Context) {
	c.IndentedJSON(http.StatusOK, map[string]string{"ok": "true"})
}

func main() {
	// Setup database
	DbHost = helper.GetEnv("DB_HOST", "")
	DbPassword = helper.GetEnv("DB_NAME", "")
	DbUser = helper.GetEnv("DB_USER", "")

	client, err := mongo.NewClient(options.Client().ApplyURI(fmt.Sprintf("mongodb://%s:%s@%s", DbUser, DbPassword, DbHost)))
	if err != nil {
		log.Fatal(err)
	}
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	err = client.Connect(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Disconnect(ctx)

	// Setup router
	router := gin.Default()

	router.GET("/api/health", health)

	router.Run("localhost:8080")
}
