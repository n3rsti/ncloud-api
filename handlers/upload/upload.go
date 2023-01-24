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

	collection := h.Db.Collection("files")
	dirCollection := h.Db.Collection("directories")


	// Check if user is the owner of directory he wants to upload into
	if directory != "" {
		parentDirHexId, err := primitive.ObjectIDFromHex(directory)
		if err != nil {
			c.Status(http.StatusBadRequest)
			return
		}

		var resultDir bson.M

		if err := dirCollection.FindOne(c, bson.D{{"_id", parentDirHexId}}).Decode(&resultDir); err != nil {
			c.Status(http.StatusNotFound)
			return
		}

		if resultDir["user"] == "" || resultDir["user"] != claims.Id {
			c.Status(http.StatusForbidden)
			return
		}
	}


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

	if err = c.SaveUploadedFile(file, uploadDestination+fileId); err != nil {
		// Remove file document if saving it wasn't successful
		_, _ = collection.DeleteOne(c, bson.D{{"_id", res.InsertedID}})
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

	hexId, err := primitive.ObjectIDFromHex(data.ParentDirectory)
	if err != nil {
		return
	}


	var result bson.M

	if err = collection.FindOne(c, bson.D{{"_id", hexId}}).Decode(&result); err != nil {
		c.Status(http.StatusNotFound)
		return
	}

	if result["user"] != user.Id {
		c.Status(http.StatusForbidden)
		return
	}


	res, err := collection.InsertOne(c, data.ToBSON())

	if err != nil {
		fmt.Println(err)
		c.Status(http.StatusBadRequest)
	}

	data.Id = res.InsertedID.(primitive.ObjectID).Hex()

	c.IndentedJSON(http.StatusCreated, data)
}

func (h *FileHandler) GetDirectoryWithFiles(c *gin.Context){
	directoryId := c.Param("id")
	reqUser := auth.ExtractClaimsFromContext(c)

	// DB aggregation setup
	addFieldsStage := bson.D{
		{"$addFields", bson.D{
			{"_id", bson.D{{"$toString", "$_id"}}},
		}},
	}

	lookupStage := bson.D{{"$lookup", bson.D{
		{"from", "files"},
		{"localField", "_id"},
		{"foreignField", "parent_directory"},
		{"as", "files"},
	}}}

	lookupStage2 := bson.D{{"$lookup", bson.D{
		{"from", "directories"},
		{"localField", "_id"},
		{"foreignField", "parent_directory"},
		{"as", "directories"},
	}}}

	matchStage := bson.D{
		{"$match", bson.D{
			{"_id", directoryId},
		}},
	}

	collection := h.Db.Collection("directories")

	cursor, err := collection.Aggregate(c, mongo.Pipeline{addFieldsStage, lookupStage, lookupStage2, matchStage})

	if err != nil {
		log.Fatal(err)
	}

	// map results to bson.M
	var results []bson.M
	if err = cursor.All(c, &results); err != nil {
		log.Fatal(err)
	}

	if len(results) == 0 {
		c.Status(http.StatusNotFound)
		return
	}

	directoryOwner := results[0]["user"]


	if directoryOwner == "" || directoryOwner != reqUser.Id {
		c.Status(http.StatusForbidden)
		return
	}

	c.IndentedJSON(http.StatusOK, results)
}