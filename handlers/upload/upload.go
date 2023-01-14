package upload

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"log"
	"net/http"
)

const uploadDestination = "/var/ncloud_upload/"

type FileHandler struct {
	Db *mongo.Database
}

func (h *FileHandler) Upload(c *gin.Context) {
	file, _ := c.FormFile("file")

	collection := h.Db.Collection("files")

	res, err := collection.InsertOne(c, bson.D{
		{"name", file.Filename},
	})

	if err != nil {
		log.Panic(err)
		c.Status(http.StatusBadRequest)
		return
	}

	// Convert ID to string
	fileId := res.InsertedID.(primitive.ObjectID).Hex()

	err = c.SaveUploadedFile(file, uploadDestination+fileId)
	if err != nil {
		log.Panic(err)
		c.Status(http.StatusBadRequest)
		return
	}

	c.String(http.StatusOK, fmt.Sprintf("'%s' uploaded!", file.Filename))
}
