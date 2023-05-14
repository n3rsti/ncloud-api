package directories

import (
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"log"
	"ncloud-api/handlers/files"
	"ncloud-api/middleware/auth"
	"ncloud-api/models"
	"net/http"
	"os"
)

type DirectoryHandler struct {
	Db *mongo.Database
}

func (h *DirectoryHandler) GetDirectoryWithFiles(c *gin.Context) {
	directoryId := c.Param("id")
	reqUser := auth.ExtractClaimsFromContext(c)

	var matchStage bson.D

	if directoryId == "" {
		matchStage = bson.D{
			{"$match", bson.D{
				{"parent_directory", nil},
				{"user", reqUser.Id},
			}},
		}
	} else {
		directoryObjectId, err := primitive.ObjectIDFromHex(directoryId)
		if err != nil {
			c.Status(http.StatusBadRequest)
			return
		}

		matchStage = bson.D{
			{"$match", bson.D{
				{"_id", directoryObjectId},
			}},
		}
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

	collection := h.Db.Collection("directories")

	cursor, err := collection.Aggregate(c, mongo.Pipeline{lookupStage, lookupStage2, matchStage})

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

func (h *DirectoryHandler) CreateDirectory(c *gin.Context) {
	parentDirectoryId := c.Param("id")

	var data models.Directory

	if err := c.BindJSON(&data); err != nil {
		return
	}

	hexId, err := primitive.ObjectIDFromHex(parentDirectoryId)
	if err != nil {
		return
	}

	// Set parentDirectoryId from URL
	data.ParentDirectory = hexId

	if data.Name == "" {
		c.IndentedJSON(http.StatusBadRequest, gin.H{
			"error": "empty name or parent directory",
		})
		return
	}

	user := auth.ExtractClaimsFromContext(c)

	data.User = user.Id

	collection := h.Db.Collection("directories")

	// Check if user is the owner of the directory where he wants to create directory
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

	fileId := res.InsertedID.(primitive.ObjectID).Hex()
	data.Id = fileId

	if err := os.Mkdir(files.UploadDestination + fileId, 0600); err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}

	// Create and set access key to directory
	newDirectoryAccessKey, err := auth.GenerateFileAccessKey(fileId, auth.AllDirectoryPermissions)
	collection.UpdateByID(c, res.InsertedID, bson.D{{"$set", bson.M{"access_key": newDirectoryAccessKey}}})

	data.AccessKey = newDirectoryAccessKey

	c.IndentedJSON(http.StatusCreated, data)
}

func (h *DirectoryHandler) ModifyDirectory(c *gin.Context) {
	directoryId := c.Param("id")
	dirAccessKey := c.GetHeader("DirectoryAccessKey")
	isAuthorized := auth.ValidatePermissions(dirAccessKey, auth.PermissionModify)

	if isAuthorized == false {
		c.IndentedJSON(http.StatusForbidden, gin.H{
			"error": "no modify permission",
		})
	}

	var directory models.Directory

	if err := c.ShouldBindJSON(&directory); err != nil {
		c.IndentedJSON(http.StatusBadRequest, gin.H{
			"error": "couldn't bind json data to object",
		})
	}

	if directory.User != "" || directory.Id != "" || directory.AccessKey != "" {
		c.IndentedJSON(http.StatusBadRequest, gin.H{
			"error": "attempt to modify restricted fields",
		})
		return
	}

	claims, _ := auth.ValidateAccessKey(dirAccessKey)
	if claims.Id == directory.ParentDirectory.Hex() {
		c.IndentedJSON(http.StatusBadRequest, gin.H{
			"error": "can't set same id and parent_directory_id",
		})
	}

	collection := h.Db.Collection("directories")

	directoryObjectId, _ := primitive.ObjectIDFromHex(directoryId)

	_, err := collection.UpdateByID(c, directoryObjectId, bson.D{{"$set", directory.ToBsonNotEmpty()}})
	if err != nil {
		c.IndentedJSON(http.StatusNotFound, gin.H{
			"error": "couldn't find directory",
		})
	}

	c.Status(http.StatusNoContent)

	return

}
/* Return all the directories from directory tree

	Return type:
		Array of ObjectID elements

	Example:

		|-- dir1
		|   `-- dir3
		|       `-- dir5
		|           `-- dir8
		|-- dir2
			`-- dir4
				|-- dir6
				`-- dir7
					`-- dir9
	Return:
		[dir1, dir2, dir3, ..., dir9]

*/
func filterDirectories(data map[primitive.ObjectID][]primitive.ObjectID, parentDirectory []primitive.ObjectID) []primitive.ObjectID {
	var allDirectories []primitive.ObjectID

	for _, childDirectory := range parentDirectory {
		allDirectories = append(allDirectories, childDirectory)
		for _, arrVal := range filterDirectories(data, data[childDirectory]) {
			allDirectories = append(allDirectories, arrVal)
		}
	}

	return allDirectories

}

func (h *DirectoryHandler) DeleteDirectory(c *gin.Context) {
	claims := auth.ExtractClaimsFromContext(c)
	user := claims.Id
	directoryId := c.Param("id")

	collection := h.Db.Collection("directories")

	// Get all directories with user from claims, with existing parent_directory:
	// everything except trash, main directory and potential future directories that can't be deleted anyway
	cursor, err := collection.Find(context.TODO(), bson.D{{"user", user}, {"parent_directory", bson.D{{"$exists", true}}}})
	if err != nil {
		log.Fatal(err)
	}

	var results []bson.M
	if err = cursor.All(context.TODO(), &results); err != nil {
		log.Fatal(err)
	}

	/*
		Map folders into hash map in format:
		parent_directory: [child_directory1, child_directory2, ...]

		(parent and child directories are in ObjectID type for easier filtering)
	*/

	dict := make(map[primitive.ObjectID][]primitive.ObjectID, len(results))
	for _, result := range results {
		resId := result["parent_directory"].(primitive.ObjectID)

		value, ok := dict[resId]
		if ok {
			dict[resId] = append(value, result["_id"].(primitive.ObjectID))
		} else {
			dict[resId] = []primitive.ObjectID{result["_id"].(primitive.ObjectID)}
		}
	}

	dirIdObjectId, _ := primitive.ObjectIDFromHex(directoryId)
	fmt.Println(filterDirectories(dict, dict[dirIdObjectId]))
}
