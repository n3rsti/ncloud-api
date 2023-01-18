package upload

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"log"
	"ncloud-api/middleware/auth"
	"ncloud-api/models"
	"net/http"
)

const uploadDestination = "/var/ncloud_upload/"

type FileHandler struct {
	Db *mongo.Database
}

func (h *FileHandler) Upload(c *gin.Context) {
	file, _ := c.FormFile("file")
	directory := c.PostForm("directory")

	claims := auth.ExtractClaimsFromContext(c)
	fmt.Println(claims.Id)

	collection := h.Db.Collection("files")

	res, err := collection.InsertOne(c, bson.D{
		{"name", file.Filename},
		{"user", claims.Id},
		{"directory_id", directory},
	})

	if err != nil {
		log.Panic(err)
		return
	}

	// Convert ID to string
	fileId := res.InsertedID.(primitive.ObjectID).Hex()

	err = c.SaveUploadedFile(file, uploadDestination+fileId)
	if err != nil {
		log.Panic(err)
		return
	}

	c.String(http.StatusOK, fmt.Sprintf("'%s' uploaded!", file.Filename))
}

func (h *FileHandler) CreateDirectory(c *gin.Context) {
	var data models.Directory

	if err := c.BindJSON(&data); err != nil {
		return
	}

	if data.Name == "" {
		c.Status(http.StatusBadRequest)
		return
	}

	user := auth.ExtractClaimsFromContext(c)

	data.User = user.Id

	collection := h.Db.Collection("directories")

	res, err := collection.InsertOne(c, data.ToBSON())

	if err != nil {
		fmt.Println(err)
		c.Status(http.StatusBadRequest)
	}

	data.Id = res.InsertedID.(primitive.ObjectID).Hex()

	c.IndentedJSON(http.StatusCreated, data)
}
