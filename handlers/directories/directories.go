package directories

import (
	"context"
	"fmt"
	"log"
	"ncloud-api/handlers/files"
	"ncloud-api/handlers/search"
	"ncloud-api/middleware/auth"
	"ncloud-api/models"
	"ncloud-api/utils/helper"
	"net/http"
	"os"
	"strings"

	"github.com/meilisearch/meilisearch-go"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type PatchRequestData struct {
	DirectoryId primitive.ObjectID `json:"id"`
	AccessKey   string             `json:"access_key"`
}

type Handler struct {
	Db       *mongo.Database
	SearchDb *meilisearch.Client
}

type SearchDatabaseData struct {
	Id        string `json:"_id"`
	Name      string `json:"name,omitempty"`
	Directory string `json:"parent_directory,omitempty"`
	User      string `json:"user,omitempty"`
}

func (h *Handler) UpdateOrAddToSearchDatabase(document interface{}) {
	if err := search.UpdateDocuments(h.SearchDb, "directories", &document); err != nil {
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
			{Key: "$match", Value: bson.D{
				{Key: "parent_directory", Value: nil},
				{Key: "user", Value: claims.Id},
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
			{Key: "$match", Value: bson.D{
				{Key: "_id", Value: directoryObjectId},
			}},
		}
	}

	// Join files
	lookupStage := bson.D{{Key: "$lookup", Value: bson.D{
		{Key: "from", Value: "files"},
		{Key: "localField", Value: "_id"},
		{Key: "foreignField", Value: "parent_directory"},
		{Key: "as", Value: "files"},
	}}}

	// Join directories
	lookupStage2 := bson.D{{Key: "$lookup", Value: bson.D{
		{Key: "from", Value: "directories"},
		{Key: "localField", Value: "_id"},
		{Key: "foreignField", Value: "parent_directory"},
		{Key: "as", Value: "directories"},
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

	if err = collection.FindOne(c, bson.D{{Key: "_id", Value: hexId}}).Decode(&dbResult); err != nil {
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
	newDirectoryAccessKey, _ := auth.GenerateFileAccessKey(directoryId, auth.AllDirectoryPermissions)
	if _, err := collection.UpdateByID(c, res.InsertedID, bson.D{{Key: "$set", Value: bson.M{"access_key": newDirectoryAccessKey}}}); err != nil {
		log.Panic(err)
	}

	directory.AccessKey = newDirectoryAccessKey

	// Update search database
	h.UpdateOrAddToSearchDatabase(&SearchDatabaseData{
		Id:        directoryId,
		Name:      directory.Name,
		Directory: parentDirectoryId,
		User:      user.Id,
	})

	c.JSON(http.StatusCreated, directory)
}

func (h *Handler) ModifyDirectory(c *gin.Context) {
	directoryId := c.Param("id")
	dirAccessKey := c.GetHeader("DirectoryAccessKey")
	claims := auth.ExtractClaimsFromContext(c)

	// Validate permissions from access key
	isAuthorized := auth.ValidatePermissions(dirAccessKey, auth.PermissionModify)
	if !isAuthorized {
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

	if directory.User != "" || directory.Id != "" || directory.AccessKey != "" || !directory.PreviousParentDirectory.IsZero() || !directory.ParentDirectory.IsZero() {
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

	collection := h.Db.Collection("directories")

	directoryObjectId, _ := primitive.ObjectIDFromHex(directoryId)

	_, err := collection.UpdateByID(c, directoryObjectId, bson.D{{Key: "$set", Value: directory.ToBsonNotEmpty()}})
	if err != nil {
		fmt.Println(err)
		c.Status(http.StatusNotFound)
		return
	}

	// Update search database
	h.UpdateOrAddToSearchDatabase(&SearchDatabaseData{
		Id:   directoryId,
		Name: directory.Name,
		User: claims.Id,
	})

	c.Status(http.StatusNoContent)
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
func FilterDirectories(data map[primitive.ObjectID][]primitive.ObjectID, parentDirectory []primitive.ObjectID) []primitive.ObjectID {
	var allDirectories []primitive.ObjectID

	for _, childDirectory := range parentDirectory {
		// append directory
		allDirectories = append(allDirectories, childDirectory)

		// append directory children
		allDirectories = append(allDirectories, FilterDirectories(data, data[childDirectory])...)
	}

	return allDirectories

}

// Return a map with directories in format: parent_directory: [child_directory1, child_directory2, ...]
func (h *Handler) FindAndMapDirectories(user string) map[primitive.ObjectID][]primitive.ObjectID {
	collection := h.Db.Collection("directories")

	// Get all directories with user from claims, with existing parent_directory:
	// everything except trash, main directory and potential future directories that can't be deleted anyway
	cursor, err := collection.Find(context.TODO(), bson.D{{Key: "user", Value: user}, {Key: "parent_directory", Value: bson.D{{Key: "$exists", Value: true}}}})
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

		if value, ok := dict[resId]; ok {
			dict[resId] = append(value, result["_id"].(primitive.ObjectID))
		} else {
			dict[resId] = []primitive.ObjectID{result["_id"].(primitive.ObjectID)}
		}
	}

	return dict
}

func (h *Handler) DeleteDirectories(c *gin.Context) {
	type RequestData struct {
		Id        primitive.ObjectID `json:"id"`
		AccessKey string             `json:"access_key"`
	}
	directories := make([]RequestData, 0)

	if err := c.MustBindWith(&directories, binding.JSON); err != nil {
		fmt.Println(err)
	}

	directoriesToDelete := make([]primitive.ObjectID, 0)
	directoryStringList := make([]string, 0)
	fileDeleteQuery := make([]string, 0)

	for _, directory := range directories {
		if isValid := auth.ValidateAccessKeyWithId(directory.AccessKey, directory.Id.Hex()); !isValid {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "invalid access key for directory: " + directory.Id.Hex(),
			})
			return
		}

		directoriesToDelete = append(directoriesToDelete, directory.Id)
		directoryStringList = append(directoryStringList, directory.Id.Hex())
		fileDeleteQuery = append(fileDeleteQuery, "parent_directory = "+directory.Id.Hex())
	}

	claims := auth.ExtractClaimsFromContext(c)
	user := claims.Id

	directoryMap := h.FindAndMapDirectories(user)

	var directoryList []primitive.ObjectID

	for _, val := range directoriesToDelete {
		directoryList = append(directoryList, FilterDirectories(directoryMap, directoryMap[val])...)
		directoryList = append(directoryList, val)
	}

	// Remove all file documents from DB
	collection := h.Db.Collection("files")

	_, err := collection.DeleteMany(context.TODO(), bson.D{{Key: "user", Value: claims.Id}, {Key: "parent_directory", Value: bson.D{{Key: "$in", Value: directoryList}}}})
	if err != nil {
		log.Panic(err)
	}

	// Remove all directories documents from DB
	collection = h.Db.Collection("directories")

	_, err = collection.DeleteMany(context.TODO(), bson.D{{Key: "user", Value: claims.Id}, {Key: "_id", Value: bson.D{{Key: "$in", Value: directoryList}}}})
	if err != nil {
		log.Panic(err)
	}

	// Remove all directories (with files) from disk
	for _, directory := range directoryList {
		if err = os.RemoveAll(files.UploadDestination + directory.Hex()); err != nil {
			log.Panic(err)
		}
	}

	if _, err = h.SearchDb.Index("directories").DeleteDocuments(directoryStringList); err != nil {
		log.Println(err)
	}

	if _, err := h.SearchDb.Index("files").DeleteDocumentsByFilter(strings.Join(fileDeleteQuery, " OR ")); err != nil {
		log.Println(err)
	}

	c.Status(http.StatusNoContent)
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
	cursor, err := collection.Find(context.TODO(), bson.D{{Key: "user", Value: user}, {Key: "parent_directory", Value: bson.D{{Key: "$exists", Value: true}}}})
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

		if value, ok := dict[resId]; ok {
			dict[resId] = append(value, result["_id"].(primitive.ObjectID))
		} else {
			dict[resId] = []primitive.ObjectID{result["_id"].(primitive.ObjectID)}
		}
	}

	// Convert string id to ObjectID
	dirIdObjectId, _ := primitive.ObjectIDFromHex(directoryId)

	// Create list of directories to delete: directory to delete and all directories inside
	directoryList := FilterDirectories(dict, dict[dirIdObjectId])
	directoryList = append(directoryList, dirIdObjectId)

	// Remove all file documents from DB
	collection = h.Db.Collection("files")

	_, err = collection.DeleteMany(context.TODO(), bson.D{{Key: "parent_directory", Value: bson.D{{Key: "$in", Value: directoryList}}}})
	if err != nil {
		log.Panic(err)
	}

	// Remove all directories documents from DB
	collection = h.Db.Collection("directories")

	_, err = collection.DeleteMany(context.TODO(), bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: directoryList}}}})
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
func (h *Handler) ChangeDirectory(c *gin.Context) {
	var operations []mongo.WriteModel
	directoryObjectIdList := make([]primitive.ObjectID, 0)

	// map in format {"_id": "directoryId", "parent_directory": "ID of destination directory"}
	// used to contstruct search database update query
	directoryMap := make([]map[string]interface{}, 0)

	claims := auth.ExtractClaimsFromContext(c)
	directoryTree := h.FindAndMapDirectories(claims.Id)

	type RequestData struct {
		Id        primitive.ObjectID `json:"id"`
		AccessKey string             `json:"access_key"`
		Items     []struct {
			Id              primitive.ObjectID `json:"id"`
			AccessKey       string             `json:"access_key"`
			ParentDirectory primitive.ObjectID `json:"parent_directory"` // this is optional, this value will be set as previous_parent_directory, useful for restoring from trash
		}
	}

	var requestData RequestData

	if err := c.MustBindWith(&requestData, binding.JSON); err != nil {
		log.Println(err)
	}

	// Validate access key and check if the access key is for that specific directory
	directoryClaims, valid := auth.ValidateAccessKey(requestData.AccessKey)
	if !valid || directoryClaims.Id != requestData.Id.Hex() {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid access key for directory: " + requestData.Id.Hex(),
		})
		return
	}

	// Validate each file and add them to directoryMap and directoryObjectIdList
	for _, directory := range requestData.Items {
		// Validate access key and check if this access key is for that specific directory
		// Check if access key allows user to modify (check permissions)
		// Check if destinaion folder is not in source folder (can't move directory to itself)
		if accessKeyClaims, valid := auth.ValidateAccessKey(directory.AccessKey); !valid || accessKeyClaims.Id != directory.Id.Hex() {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "invalid access key for directory: " + directory.Id.Hex(),
			})
			return
		} else if !auth.ValidatePermissionsFromClaims(accessKeyClaims, auth.PermissionModify) {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "no permission to modify this directory",
			})
			return
		} else if helper.ObjectIArrayContains(FilterDirectories(directoryTree, directoryTree[directory.Id]), requestData.Id) || directory.Id == requestData.Id {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "cannot move directory " + directory.Id.Hex() + " to itself",
			})
			return
		}

		directoryMap = append(directoryMap, map[string]interface{}{
			"_id":              directory.Id.Hex(),
			"parent_directory": requestData.Id.Hex(),
		})

		// If there is no value to set as previous_parent_directory, add directory ID to UpdateMany operation list
		// Else add UpdateOne operation and set parent_directory as previous_parent_directory (use for trash)
		if directory.ParentDirectory.IsZero() {
			directoryObjectIdList = append(directoryObjectIdList, directory.Id)

		} else {
			dbOperation := mongo.NewUpdateOneModel()
			// Directories from list in request body AND having parent_directory as directory ID from list
			// This removes possibility of user providing invalid parent directory
			dbOperation.SetFilter(bson.M{
				"_id":              directory.Id,
				"parent_directory": directory.ParentDirectory,
			})

			dbOperation.SetUpdate(bson.M{
				"$set": bson.M{
					"parent_directory":          requestData.Id,
					"previous_parent_directory": directory.ParentDirectory,
				},
			})

			operations = append(operations, dbOperation)
		}

	}

	if len(directoryObjectIdList) > 0 {
		updateOperation := mongo.NewUpdateManyModel()

		updateOperation.SetFilter(bson.D{
			{Key: "_id", Value: bson.D{
				{Key: "$in", Value: directoryObjectIdList},
			}},
		})

		updateOperation.SetUpdate(bson.D{
			{Key: "$set", Value: bson.D{
				{Key: "parent_directory", Value: requestData.Id},
			}},
		})

		operations = append(operations, updateOperation)
	}

	// Update parent directory of directories with IDs from directoryObjectIdList
	res, err := h.Db.Collection("directories").BulkWrite(context.TODO(), operations)
	if err != nil {
		log.Panic(err)
	}

	// Update search database
	if _, err := h.SearchDb.Index("directories").UpdateDocuments(directoryMap); err != nil {
		log.Println(err)
	}

	c.JSON(http.StatusOK, gin.H{
		"updated": res.ModifiedCount,
	})
}

func (h *Handler) RestoreDirectories(c *gin.Context) {
	claims := auth.ExtractClaimsFromContext(c)
	searchDbList := make([]map[string]interface{}, 0)

	type RequestData struct {
		Directories []primitive.ObjectID `json:"directories"`
	}

	var requestData RequestData

	if err := c.MustBindWith(&requestData, binding.JSON); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "bad request format",
		})
	}

	var dbResult []bson.M

	// Find directories from request body list
	cursor, err := h.Db.Collection("directories").Find(context.TODO(), bson.M{"_id": bson.M{"$in": requestData.Directories}})
	if err != nil {
		log.Panic(err)
	}

	if err = cursor.All(context.TODO(), &dbResult); err != nil {
		log.Panic(err)
	}

	var operations []mongo.WriteModel

	for _, directory := range dbResult {
		// Check if user is the owner of directories
		if directory["user"].(string) != claims.Id {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "no access for directory: " + directory["_id"].(primitive.ObjectID).Hex(),
			})
		}

		// Check if previous parent directory isn't empty
		if !directory["previous_parent_directory"].(primitive.ObjectID).IsZero() {
			dbOperation := mongo.NewUpdateOneModel()
			dbOperation.SetFilter(bson.M{"_id": directory["_id"]})
			dbOperation.SetUpdate(bson.M{
				"$set": bson.M{
					"parent_directory":          directory["previous_parent_directory"],
					"previous_parent_directory": "",
				},
			})

			operations = append(operations, dbOperation)

			searchDbList = append(searchDbList, map[string]interface{}{
				"_id":              directory["_id"].(primitive.ObjectID).Hex(),
				"parent_directory": directory["previous_parent_directory"].(primitive.ObjectID).Hex(),
			})
		}

	}

	res, err := h.Db.Collection("directories").BulkWrite(context.TODO(), operations)
	if err != nil {
		log.Panic(err)
	}

	if _, err := h.SearchDb.Index("directories").UpdateDocuments(searchDbList); err != nil {
		log.Println(err)
	}

	c.JSON(http.StatusOK, gin.H{
		"updated": res.ModifiedCount,
	})
}
