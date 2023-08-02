package directories

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/google/uuid"
	"github.com/meilisearch/meilisearch-go"
	cp "github.com/otiai10/copy"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"ncloud-api/handlers/files"
	"ncloud-api/handlers/search"
	"ncloud-api/middleware/auth"
	"ncloud-api/models"
	"ncloud-api/utils/helper"
)

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
		matchStage = bson.D{
			{Key: "$match", Value: bson.D{
				{Key: "_id", Value: directoryId},
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

	if directory.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "empty name or parent directory",
		})
		return
	}

	user := auth.ExtractClaimsFromContext(c)

	// Set parentDirectoryId from URL
	directory.ParentDirectory = parentDirectoryId
	directory.User = user.Id

	directoryId, _ := uuid.NewUUID()
	directory.Id = directoryId.String()

	// Create and set access key to directory
	newDirectoryAccessKey, _ := auth.GenerateDirectoryAccessKey(
		directoryId.String(),
		auth.AllDirectoryPermissions,
	)

	directory.AccessKey = newDirectoryAccessKey

	collection := h.Db.Collection("directories")

	_, err := collection.InsertOne(c, directory.ToBsonNotEmpty())
	if err != nil {
		fmt.Println(err)
		c.Status(http.StatusBadRequest)
		return
	}

	// Create directory on disk
	if err := os.Mkdir(files.UploadDestination+directoryId.String(), 0700); err != nil {
		log.Panic(err)
	}

	// Update search database
	h.UpdateOrAddToSearchDatabase(&SearchDatabaseData{
		Id:        directoryId.String(),
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

	if directory.User != "" || directory.Id != "" || directory.AccessKey != "" ||
		directory.PreviousParentDirectory != "" ||
		directory.ParentDirectory != "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "attempt to modify restricted fields",
		})
		return
	}

	collection := h.Db.Collection("directories")

	_, err := collection.UpdateByID(
		c,
		directoryId,
		bson.D{{Key: "$set", Value: directory.ToBsonNotEmpty()}},
	)
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

	Array of string elements

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
func GetDirectoriesFromParent(
	parentDirectory []string,
	data map[string][]string,
) []string {
	var allDirectories []string

	for _, childDirectory := range parentDirectory {
		// append directory
		allDirectories = append(allDirectories, childDirectory)

		// append directory children
		allDirectories = append(
			allDirectories,
			GetDirectoriesFromParent(data[childDirectory], data)...)
	}

	return allDirectories
}

func GetDirectoriesFromParentAsObject(
	parentDirectory []*models.Directory,
	data map[string][]*models.Directory,
) []*models.Directory {
	var allDirectories []*models.Directory

	for idx, childDirectory := range parentDirectory {
		// append directory
		allDirectories = append(allDirectories, parentDirectory[idx])

		// append directory children
		allDirectories = append(
			allDirectories,
			GetDirectoriesFromParentAsObject(data[childDirectory.Id], data)...)
	}

	return allDirectories
}

// Return a map with directories in format: parent_directory: [child_directory1, child_directory2, ...]
func (h *Handler) FindAndMapDirectories(user string) map[string][]string {
	collection := h.Db.Collection("directories")

	// Get all directories with user from claims, with existing parent_directory:
	// everything except trash, main directory and potential future directories that can't be deleted anyway
	cursor, err := collection.Find(
		context.TODO(),
		bson.D{
			{Key: "user", Value: user},
			{Key: "parent_directory", Value: bson.D{{Key: "$exists", Value: true}}},
		},
	)
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
	*/

	dict := make(map[string][]string, len(results))
	for _, result := range results {
		resId := result["parent_directory"].(string)

		if value, ok := dict[resId]; ok {
			dict[resId] = append(value, result["_id"].(string))
		} else {
			dict[resId] = []string{result["_id"].(string)}
		}
	}

	return dict
}

func (h *Handler) DeleteDirectories(c *gin.Context) {
	type RequestData struct {
		Id        string `json:"id"`
		AccessKey string `json:"access_key"`
	}
	directories := make([]RequestData, 0)

	if err := c.MustBindWith(&directories, binding.JSON); err != nil {
		fmt.Println(err)
	}

	directoriesToDelete := make([]string, 0, len(directories))

	for _, directory := range directories {
		if isValid := auth.ValidateAccessKeyWithId(directory.AccessKey, directory.Id); !isValid {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "invalid access key for directory: " + directory.Id,
			})
			return
		}

		directoriesToDelete = append(directoriesToDelete, directory.Id)
	}

	claims := auth.ExtractClaimsFromContext(c)
	user := claims.Id

	directoryMap := h.FindAndMapDirectories(user)

	var directoryList []string

	for _, val := range directoriesToDelete {
		directoryList = append(
			directoryList,
			GetDirectoriesFromParent(directoryMap[val], directoryMap)...)
		directoryList = append(directoryList, val)
	}

	// Remove all file documents from DB
	collection := h.Db.Collection("files")

	_, err := collection.DeleteMany(
		context.TODO(),
		bson.D{
			{Key: "parent_directory", Value: bson.D{{Key: "$in", Value: directoryList}}},
		},
	)
	if err != nil {
		log.Panic(err)
	}

	// Remove all directories documents from DB
	collection = h.Db.Collection("directories")

	_, err = collection.DeleteMany(
		context.TODO(),
		bson.D{
			{Key: "user", Value: claims.Id},
			{Key: "_id", Value: bson.D{{Key: "$in", Value: directoryList}}},
		},
	)
	if err != nil {
		log.Panic(err)
	}

	// Remove all directories (with files) from disk
	for _, directory := range directoryList {
		if err = os.RemoveAll(files.UploadDestination + directory); err != nil {
			log.Panic(err)
		}
	}

	if _, err = h.SearchDb.Index("directories").DeleteDocuments(directoriesToDelete); err != nil {
		log.Println(err)
	}

	fileDeleteQuery := make([]string, 0, len(directoryList))
	for _, dirId := range directoryList {
		fileDeleteQuery = append(fileDeleteQuery, "parent_directory = "+dirId)
	}

	if _, err := h.SearchDb.Index("files").DeleteDocumentsByFilter(strings.Join(fileDeleteQuery, " OR ")); err != nil {
		log.Println(err)
	}

	c.Status(http.StatusNoContent)
}

func validateDirectory(
	c *gin.Context,
	accessKey string,
	directoryId string,
	directoryToMove string,
	directoryTree map[string][]string,
) bool {
	// Validate access key and check if this access key is for that specific directory
	// Check if access key allows user to modify (check permissions)
	// Check if destination folder is not in source folder (can't move directory to itself)
	if accessKeyClaims, valid := auth.ValidateAccessKey(accessKey); !valid ||
		accessKeyClaims.Id != directoryId {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid access key for directory: " + directoryId,
		})
		return false
	} else if !auth.ValidatePermissionsFromClaims(accessKeyClaims, auth.PermissionModify) {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "no permission to modify this directory",
		})
		return false
	} else if helper.ArrayContains(GetDirectoriesFromParent(directoryTree[directoryId], directoryTree), directoryToMove) || directoryId == directoryToMove {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "cannot move directory " + directoryId + " to itself",
		})
		return false
	}
	return true
}

func (h *Handler) ChangeDirectory(c *gin.Context) {
	var operations []mongo.WriteModel

	claims := auth.ExtractClaimsFromContext(c)
	directoryTree := h.FindAndMapDirectories(claims.Id)

	type RequestData struct {
		Id        string `json:"id"`
		AccessKey string `json:"access_key"`
		Items     []struct {
			Id              string `json:"id"`
			AccessKey       string `json:"access_key"`
			ParentDirectory string `json:"parent_directory"` // this is optional, this value will be set as previous_parent_directory, useful for restoring from trash
		}
	}

	var requestData RequestData

	if err := c.MustBindWith(&requestData, binding.JSON); err != nil {
		log.Println(err)
	}

	// Validate access key and check if the access key is for that specific directory
	directoryClaims, valid := auth.ValidateAccessKey(requestData.AccessKey)
	if !valid || directoryClaims.Id != requestData.Id {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid access key for directory: " + requestData.Id,
		})
		return
	}

	// map in format {"_id": "directoryId", "parent_directory": "ID of destination directory"}
	// used to construct search database update query
	searchDbQueryList := make([]map[string]interface{}, 0, len(requestData.Items))

	directoryIdList := make([]string, 0, len(requestData.Items))

	// Validate each file and add them to searchDbQueryList and directoryIdList
	for _, directory := range requestData.Items {
		if isValidDirectory := validateDirectory(c, directory.AccessKey, directory.Id, requestData.Id, directoryTree); !isValidDirectory {
			return
		}

		searchDbQueryList = append(searchDbQueryList, map[string]interface{}{
			"_id":              directory.Id,
			"parent_directory": requestData.Id,
		})

		// Set parentDirectory value if it's provided in RequestData
		if directory.ParentDirectory != "" {
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
		} else {
			directoryIdList = append(directoryIdList, directory.Id)
		}

	}

	if len(directoryIdList) > 0 {
		updateOperation := mongo.NewUpdateManyModel()

		updateOperation.SetFilter(bson.D{
			{Key: "_id", Value: bson.D{
				{Key: "$in", Value: directoryIdList},
			}},
		})

		updateOperation.SetUpdate(bson.D{
			{Key: "$set", Value: bson.D{
				{Key: "parent_directory", Value: requestData.Id},
			}},
		})

		operations = append(operations, updateOperation)
	}

	res, err := h.Db.Collection("directories").BulkWrite(context.TODO(), operations)
	if err != nil {
		log.Panic(err)
	}

	if _, err := h.SearchDb.Index("directories").UpdateDocuments(searchDbQueryList); err != nil {
		log.Println(err)
	}

	c.JSON(http.StatusOK, gin.H{
		"updated": res.ModifiedCount,
	})
}

func (h *Handler) RestoreDirectories(c *gin.Context) {
	userClaims := auth.ExtractClaimsFromContext(c)

	type RequestData struct {
		Directories []string `json:"directories"`
	}

	var requestData RequestData

	if err := c.MustBindWith(&requestData, binding.JSON); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "bad request format",
		})
	}

	// List for search db update operation
	searchDbQueryList := make([]map[string]interface{}, 0, len(requestData.Directories))

	dbFindResult := make([]bson.M, 0, len(requestData.Directories))

	cursor, err := h.Db.Collection("directories").
		Find(context.TODO(), bson.M{"_id": bson.M{"$in": requestData.Directories}})
	if err != nil {
		log.Panic(err)
	}

	if err = cursor.All(context.TODO(), &dbFindResult); err != nil {
		log.Panic(err)
	}

	var dbUpdateOperations []mongo.WriteModel

	for _, directory := range dbFindResult {
		if directory["user"].(string) != userClaims.Id {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "no access for directory: " + directory["_id"].(string),
			})
		}

		if directory["previous_parent_directory"].(string) != "" {
			dbOperation := mongo.NewUpdateOneModel()
			dbOperation.SetFilter(bson.M{"_id": directory["_id"]})
			dbOperation.SetUpdate(bson.M{
				"$set": bson.M{
					"parent_directory":          directory["previous_parent_directory"],
					"previous_parent_directory": "",
				},
			})

			dbUpdateOperations = append(dbUpdateOperations, dbOperation)

			searchDbQueryList = append(searchDbQueryList, map[string]interface{}{
				"_id":              directory["_id"].(string),
				"parent_directory": directory["previous_parent_directory"].(string),
			})
		}

	}

	res, err := h.Db.Collection("directories").BulkWrite(context.TODO(), dbUpdateOperations)
	if err != nil {
		log.Panic(err)
	}

	if _, err := h.SearchDb.Index("directories").UpdateDocuments(searchDbQueryList); err != nil {
		log.Println(err)
	}

	c.JSON(http.StatusOK, gin.H{
		"updated": res.ModifiedCount,
	})
}

func (h *Handler) CopyDirectories(c *gin.Context) {
	type RequestData struct {
		Destination string   `json:"destination"`
		Directories []string `json:"directories"`
	}

	var data RequestData

	if err := c.MustBindWith(&data, binding.JSON); err != nil {
		return
	}

	user := auth.ExtractClaimsFromContext(c).Id

	filter := bson.D{
		{Key: "user", Value: user},
		{Key: "parent_directory", Value: bson.D{{Key: "$exists", Value: true}}},
	}

	directories, err := models.FindDirectoriesByFilter[models.Directory](h.Db, filter)
	if err != nil {
		log.Panic(err)
		return
	}

	topDirectories := make([]*models.Directory, 0, len(data.Directories))

	dict := make(map[string][]*models.Directory, len(directories))
	for idx, directory := range directories {
		if helper.ArrayContains(data.Directories, directory.Id) {
			topDirectories = append(topDirectories, &directories[idx])
		}
		dirId := directory.ParentDirectory

		if value, ok := dict[dirId]; ok {
			dict[dirId] = append(value, &directories[idx])
		} else {
			dict[dirId] = []*models.Directory{&directories[idx]}
		}

	}

	directoriesToCopy := GetDirectoriesFromParentAsObject(topDirectories, dict)
	directoryIdList := make([]string, 0, len(directoriesToCopy))

	directoryIdMap := make(map[string]string, len(directoriesToCopy))

	for _, directory := range directoriesToCopy {
		directoryIdList = append(directoryIdList, directory.Id)

		newId := uuid.New()

		directoryIdMap[newId.String()] = directory.Id
		directoryIdMap[directory.Id] = newId.String()

		newAccessKey, _ := auth.GenerateDirectoryAccessKey(
			newId.String(),
			auth.AllDirectoryPermissions,
		)

		children, exists := dict[directory.Id]
		if exists {
			for _, child := range children {
				child.ParentDirectory = newId.String()
			}
		}

		directory.Id = newId.String()
		directory.AccessKey = newAccessKey
	}

	for _, directory := range topDirectories {
		directory.ParentDirectory = data.Destination
	}

	filter = bson.D{
		{Key: "parent_directory", Value: bson.D{{Key: "$in", Value: directoryIdList}}},
	}

	filesToCopy, err := models.FindFilesByFilter[models.File](h.Db, filter)
	if err != nil {
		log.Panic(err)
		return
	}

	fileIdMap := make(map[string]string, len(filesToCopy))
	for idx, file := range filesToCopy {
		newId := uuid.New()

		fileIdMap[newId.String()] = file.Id

		filesToCopy[idx].Id = newId.String()
		filesToCopy[idx].ParentDirectory = directoryIdMap[file.ParentDirectory]
		filesToCopy[idx].PreviousParentDirectory = ""
	}

	if _, err := h.Db.Collection("directories").InsertMany(context.TODO(), models.DirectoryPointersToBsonNotEmpty(directoriesToCopy)); err != nil {
		log.Panic(err)
		return
	}

	if _, err := h.Db.Collection("files").InsertMany(context.TODO(), models.FilesToBsonNotEmpty(filesToCopy)); err != nil {
		log.Panic(err)
		return
	}

	for _, dir := range directoriesToCopy {
		sourceDirectory := files.UploadDestination + directoryIdMap[dir.Id]
		destinationDirectory := files.UploadDestination + dir.Id
		if err := cp.Copy(sourceDirectory, destinationDirectory); err != nil {
			log.Panic(err)
			return
		}
	}

	for _, file := range filesToCopy {
		source := files.UploadDestination + file.ParentDirectory + "/" + fileIdMap[file.Id]
		destination := files.UploadDestination + file.ParentDirectory + "/" + file.Id

		if err := os.Rename(source, destination); err != nil {
			log.Panic(err)
		}
	}

	c.JSON(http.StatusOK, topDirectories)
}
