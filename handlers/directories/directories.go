package directories

import (
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/meilisearch/meilisearch-go"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"log"
	"ncloud-api/handlers/files"
	"ncloud-api/handlers/search"
	"ncloud-api/middleware/auth"
	"ncloud-api/models"
	"ncloud-api/utils/helper"
	"net/http"
	"os"
)

type Handler struct {
	Db          *mongo.Database
	SearchDb *meilisearch.Client
}

type SearchDatabaseData struct {
	Id        string `json:"_id"`
	Name      string `json:"name,omitempty"`
	Directory string `json:"parent_directory,omitempty"`
	User      string `json:"user,omitempty"`
}

func (h *Handler) UpdateOrAddToSearchDatabase(document interface{}) {
	if err := search.UpdateDocuments(h.SearchDb, "directories", &document); err != nil{
		log.Println(err)
	}
}

func (h *Handler) DeleteFromSearchDatabase(id []string) {
	if err := search.DeleteDocuments(h.SearchDb, "directories", id); err != nil {
		log.Println(err)
	}
}

func (h *Handler) GetDirectoryWithFiles(c *gin.Context) {
	directoryId := c.Param("id")
	claims := auth.ExtractClaimsFromContext(c)

	var matchStage bson.D

	if directoryId == "" {
		matchStage = bson.D{
			{"$match", bson.D{
				{"parent_directory", nil},
				{"user", claims.Id},
			}},
		}
	} else {
		// Attempt to convert url ID parameter to ObjectID
		// If it fails, it means that parameter is not valid
		directoryObjectId, err := primitive.ObjectIDFromHex(directoryId)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "invalid ID",
			})
			return
		}

		matchStage = bson.D{
			{"$match", bson.D{
				{"_id", directoryObjectId},
			}},
		}
	}

	// Join files
	lookupStage := bson.D{{"$lookup", bson.D{
		{"from", "files"},
		{"localField", "_id"},
		{"foreignField", "parent_directory"},
		{"as", "files"},
	}}}

	// Join directories
	lookupStage2 := bson.D{{"$lookup", bson.D{
		{"from", "directories"},
		{"localField", "_id"},
		{"foreignField", "parent_directory"},
		{"as", "directories"},
	}}}

	collection := h.Db.Collection("directories")

	cursor, err := collection.Aggregate(c, mongo.Pipeline{lookupStage, lookupStage2, matchStage})

	if err != nil {
		log.Println(err)
		c.Status(http.StatusNotFound)
		return
	}

	// map results to bson.M
	var results []bson.M
	if err = cursor.All(c, &results); err != nil {
		log.Panic(err)
	}

	if len(results) == 0 {
		c.Status(http.StatusNotFound)
		return
	}

	directoryOwner := results[0]["user"]

	if directoryOwner == "" || directoryOwner != claims.Id {
		c.Status(http.StatusForbidden)
		return
	}

	c.JSON(http.StatusOK, results)
}

func (h *Handler) CreateDirectory(c *gin.Context) {
	parentDirectoryId := c.Param("id")

	// Attempt to bind JSON directory to Directory model
	var directory models.Directory
	if err := c.BindJSON(&directory); err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	// Check if object matches requirements
	if err := directory.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err,
		})
		return
	}

	// Attempt to convert url ID parameter to ObjectID
	// If it fails, it means ID is not valid
	hexId, err := primitive.ObjectIDFromHex(parentDirectoryId)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid ID format",
		})
		return
	}

	// Set parentDirectoryId from URL
	directory.ParentDirectory = hexId

	if directory.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "empty name or parent directory",
		})
		return
	}

	user := auth.ExtractClaimsFromContext(c)

	directory.User = user.Id

	collection := h.Db.Collection("directories")

	// Check if user is the owner of the directory where he wants to create directory
	var dbResult bson.M

	if err = collection.FindOne(c, bson.D{{"_id", hexId}}).Decode(&dbResult); err != nil {
		c.Status(http.StatusNotFound)
		return
	}

	if dbResult["user"] != user.Id {
		c.Status(http.StatusForbidden)
		return
	}

	res, err := collection.InsertOne(c, directory.ToBSON())
	if err != nil {
		fmt.Println(err)
		c.Status(http.StatusBadRequest)
		return
	}

	// Get ID of created directory from mongodb insert query response
	directoryId := res.InsertedID.(primitive.ObjectID).Hex()
	directory.Id = directoryId

	// Create directory on disk
	if err := os.Mkdir(files.UploadDestination+directoryId, 0700); err != nil {
		log.Panic(err)
	}

	// Create and set access key to directory
	newDirectoryAccessKey, err := auth.GenerateFileAccessKey(directoryId, auth.AllDirectoryPermissions)
	if _, err := collection.UpdateByID(c, res.InsertedID, bson.D{{"$set", bson.M{"access_key": newDirectoryAccessKey}}}); err != nil {
		log.Panic(err)
	}

	directory.AccessKey = newDirectoryAccessKey

	// Update search database
	h.UpdateOrAddToSearchDatabase(&SearchDatabaseData{
		Id: directoryId,
		Name: directory.Name,
		Directory: parentDirectoryId,
		User: user.Id,
	})

	c.JSON(http.StatusCreated, directory)
}

func (h *Handler) ModifyDirectory(c *gin.Context) {
	directoryId := c.Param("id")
	dirAccessKey := c.GetHeader("DirectoryAccessKey")
	newDirAccessKey := c.GetHeader("NewDirectoryAccessKey")
	claims := auth.ExtractClaimsFromContext(c)

	// Validate permissions from access key
	isAuthorized := auth.ValidatePermissions(dirAccessKey, auth.PermissionModify)
	if isAuthorized == false {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "no modify permission",
		})
		return
	}

	var directory models.Directory

	if err := c.ShouldBindJSON(&directory); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "couldn't bind json data to object",
		})
		return
	}

	// Check if object matches requirements
	if err := directory.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err,
		})
		return
	}

	if directory.User != "" || directory.Id != "" || directory.AccessKey != "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "attempt to modify restricted fields",
		})
		return
	}

	accessKey, _ := auth.ValidateAccessKey(dirAccessKey)
	if accessKey.Id == directory.ParentDirectory.Hex() {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "can't set same id and parent_directory_id",
		})
		return
	}

	// Check if user wants to change parent directory (move directory) and if they provided access key
	// If there's no access key, we perform database check for directory ownership
	if !directory.ParentDirectory.IsZero() && newDirAccessKey != "" {
		if _, validAccessKey := auth.ValidateAccessKey(newDirAccessKey); validAccessKey == false {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "invalid new directory access key",
			})
			return
		}
	} else if !directory.ParentDirectory.IsZero() && newDirAccessKey == "" {
		// If user doesn't provide new directory access key, we perform database check for directory ownership
		var result bson.M

		directoryCollection := h.Db.Collection("directories")
		err := directoryCollection.FindOne(c, bson.D{{"_id", directory.ParentDirectory}}).Decode(&result)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "new parent directory not found",
			})

			return
		}

		if result["user"] != claims.Id {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "no access to new parent directory",
			})
			return
		}
	}

	collection := h.Db.Collection("directories")

	directoryObjectId, _ := primitive.ObjectIDFromHex(directoryId)

	_, err := collection.UpdateByID(c, directoryObjectId, bson.D{{"$set", directory.ToBsonNotEmpty()}})
	if err != nil {
		fmt.Println(err)
		c.Status(http.StatusNotFound)
		return
	}

	var parentDirectoryId string

	if directory.ParentDirectory.IsZero(){
		parentDirectoryId = ""
	} else {
		parentDirectoryId = directory.ParentDirectory.Hex()
	}


	// Update search database
	h.UpdateOrAddToSearchDatabase(&SearchDatabaseData{
		Id: directoryId,
		Name: directory.Name,
		Directory: parentDirectoryId,
		User: claims.Id,
	})

	c.Status(http.StatusNoContent)
	return

}

/*
	Return all the directories from directory tree

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

func (h *Handler) DeleteDirectory(c *gin.Context) {
	// Verify permissions from access key
	directoryAccessKey, _ := auth.ValidateAccessKey(c.GetHeader("DirectoryAccessKey"))
	if !helper.StringArrayContains(directoryAccessKey.Permissions, auth.PermissionDelete) {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "no permission to delete this directory",
		})
		return
	}

	claims := auth.ExtractClaimsFromContext(c)
	user := claims.Id
	directoryId := c.Param("id")

	collection := h.Db.Collection("directories")

	// Get all directories with user from claims, with existing parent_directory:
	// everything except trash, main directory and potential future directories that can't be deleted anyway
	cursor, err := collection.Find(context.TODO(), bson.D{{"user", user}, {"parent_directory", bson.D{{"$exists", true}}}})
	if err != nil {
		log.Panic(err)
	}

	var results []bson.M
	if err = cursor.All(context.TODO(), &results); err != nil {
		log.Panic(err)
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

	// Convert string id to ObjectID
	dirIdObjectId, _ := primitive.ObjectIDFromHex(directoryId)

	// Create list of directories to delete: directory to delete and all directories inside
	directoryList := filterDirectories(dict, dict[dirIdObjectId])
	directoryList = append(directoryList, dirIdObjectId)

	// Remove all file documents from DB
	collection = h.Db.Collection("files")

	_, err = collection.DeleteMany(context.TODO(), bson.D{{"parent_directory", bson.D{{"$in", directoryList}}}})
	if err != nil {
		log.Panic(err)
	}

	// Remove all directories documents from DB
	collection = h.Db.Collection("directories")

	_, err = collection.DeleteMany(context.TODO(), bson.D{{"_id", bson.D{{"$in", directoryList}}}})
	if err != nil {
		log.Panic(err)
	}

	// Remove all directories (with files) from disk
	for _, directory := range directoryList {
		if err = os.RemoveAll(files.UploadDestination + directory.Hex()); err != nil {
			log.Panic(err)
		}
	}

	c.Status(http.StatusNoContent)
}
