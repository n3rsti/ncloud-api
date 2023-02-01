package auth

import (
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"net/http"
)

func FileAuth(database *mongo.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims := ExtractClaimsFromContext(c)
		idParam := c.Request.RequestURI[len("/files/"):]

		collection := database.Collection("files")

		var result bson.M

		hexId, err := primitive.ObjectIDFromHex(idParam)

		if err != nil {
			c.Status(http.StatusNotFound)
			c.Abort()
			fmt.Println(err)
			return
		}

		if err := collection.FindOne(context.TODO(), bson.D{{"_id", hexId}}).Decode(&result); err != nil {
			c.Status(http.StatusNotFound)
			c.Abort()
			fmt.Println(err)
			return
		}

		if claims.Id == "" || claims.Id != result["user"] {
			c.Status(http.StatusForbidden)
			c.Abort()
			return
		}
		c.Next()
	}
}
