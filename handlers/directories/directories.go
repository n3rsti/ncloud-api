package directories

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
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
				{Key: "user", Value: claims.Id},
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

	c.JSON(http.StatusOK, results)
}

func (h *Handler) CreateDirectory(c *gin.Context) {
	parentDirectoryId := c.Param("id")

	// Attempt to bind JSON directory to Directory model
	var directory models.Directory
	if err := c.BindJSON(&directory); err != nil {
		return
	}

	// Check if object matches requirements
	if err := directory.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err,
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
		log.Panic(err)
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

	if err := c.BindJSON(&directory); err != nil {
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
		log.Println(err)
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
func GetDirectoriesFromParents(
	parentDirectories []string,
	children map[string][]string,
) []string {
	var allDirectories []string

	for _, parentDirectory := range parentDirectories {
		// append directory
		allDirectories = append(allDirectories, parentDirectory)

		// append directory children
		allDirectories = append(
			allDirectories,
			GetDirectoriesFromParents(children[parentDirectory], children)...)
	}

	return allDirectories
}

func GetDirectoriesFromParentsAsPointers(
	parentDirectories []*models.Directory,
	children map[string][]*models.Directory,
) []*models.Directory {
	var allDirectories []*models.Directory

	for idx, parentDirectory := range parentDirectories {
		// append directory
		allDirectories = append(allDirectories, parentDirectories[idx])

		// append directory children
		allDirectories = append(
			allDirectories,
			GetDirectoriesFromParentsAsPointers(children[parentDirectory.Id], children)...)
	}

	return allDirectories
}

// Return a children map from user with all directories in format: parent_directory: [child_directory1, child_directory2, ...]
func (h *Handler) FindAndMapDirectories(user string) map[string][]string {
	// Get all directories with user from claims, with existing parent_directory:
	// everything except trash, main directory and potential future directories that can't be deleted anyway
	filter := bson.D{
		{Key: "user", Value: user},
		{Key: "parent_directory", Value: bson.D{{Key: "$exists", Value: true}}},
	}
	directories, err := models.FindDirectoriesByFilter[models.Directory](h.Db, filter)
	if err != nil {
		log.Panic(err)
	}

	/*
		Map folders into hash map in format:
		parent_directory: [child_directory1, child_directory2, ...]
	*/

	childrenMap := make(map[string][]string, len(directories))
	for _, directory := range directories {
		directoryParentId := directory.ParentDirectory

		if value, ok := childrenMap[directoryParentId]; ok {
			childrenMap[directoryParentId] = append(value, directory.Id)
		} else {
			childrenMap[directoryParentId] = []string{directory.Id}
		}
	}

	return childrenMap
}

func (h *Handler) DeleteDirectories(c *gin.Context) {
	type RequestData struct {
		Id        string `json:"id"`
		AccessKey string `json:"access_key"`
	}
	directories := make([]RequestData, 0)

	if err := c.BindJSON(&directories); err != nil {
		return
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

	// We use len(directoryMap), even though it's not exactly accurate, but this is the highest amount we can estimate
	// It will reduce a little bit of work caused by appending to already full slice
	directoryList := make([]string, 0, len(directoryMap))

	for _, val := range directoriesToDelete {
		directoryList = append(
			directoryList,
			GetDirectoriesFromParents(directoryMap[val], directoryMap)...)
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
	accessKeyClaims, IS_INVALID_ACCESS_KEY := auth.ValidateAccessKey(accessKey)
	IS_FOR_THIS_DIRECTORY := accessKeyClaims.Id == directoryId

	// Check if access key allows user to modify (check permissions)
	VALID_PERMISSIONS := auth.ValidatePermissionsFromClaims(accessKeyClaims, auth.PermissionModify)

	// Check if destination folder is not in source folder (can't move directory to itself)
	IS_INSIDE_OF_ITSELF := helper.ArrayContains(
		GetDirectoriesFromParents(directoryTree[directoryId], directoryTree),
		directoryToMove,
	) ||
		directoryId == directoryToMove

	if IS_INVALID_ACCESS_KEY || !VALID_PERMISSIONS || IS_INSIDE_OF_ITSELF ||
		!IS_FOR_THIS_DIRECTORY {
		c.Status(http.StatusBadRequest)
		return false
	}
	return true
}

func (h *Handler) ChangeDirectory(c *gin.Context) {
	var operations []mongo.WriteModel

	claims := auth.ExtractClaimsFromContext(c)
	directoryTree := h.FindAndMapDirectories(claims.Id)

	type RequestData struct {
		DestinationId        string `json:"id"`
		DestinationAccessKey string `json:"access_key"`
		Items                []struct {
			Id              string `json:"id"`
			AccessKey       string `json:"access_key"`
			ParentDirectory string `json:"parent_directory"` // this is optional, this value will be set as previous_parent_directory, useful for restoring from trash
		}
	}

	var data RequestData

	if err := c.BindJSON(&data); err != nil {
		return
	}

	// Validate access key and check if the access key is for that specific directory
	directoryClaims, valid := auth.ValidateAccessKey(data.DestinationAccessKey)
	if !valid || directoryClaims.Id != data.DestinationId {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "invalid access key for directory: " + data.DestinationId,
		})
		return
	}

	// map in format {"_id": "directoryId", "parent_directory": "ID of destination directory"}
	// used to construct search database update query
	searchDbQueryList := make([]map[string]interface{}, 0, len(data.Items))

	directoryIdList := make([]string, 0, len(data.Items))

	// Validate each file and add them to searchDbQueryList and directoryIdList
	for _, directory := range data.Items {
		if isValidDirectory := validateDirectory(c, directory.AccessKey, directory.Id, data.DestinationId, directoryTree); !isValidDirectory {
			return
		}

		searchDbQueryList = append(searchDbQueryList, map[string]interface{}{
			"_id":              directory.Id,
			"parent_directory": data.DestinationId,
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
					"parent_directory":          data.DestinationId,
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
				{Key: "parent_directory", Value: data.DestinationId},
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

	var data RequestData

	if err := c.BindJSON(&data); err != nil {
		return
	}

	// List for search db update operation
	searchDbQueryList := make([]map[string]interface{}, 0, len(data.Directories))

	filter := bson.D{
		{Key: "_id", Value: bson.D{{Key: "$in", Value: data.Directories}}},
		{Key: "user", Value: userClaims.Id},
	}
	directories, err := models.FindDirectoriesByFilter[models.Directory](h.Db, filter)
	if err != nil {
		log.Panic(err)
	}

	if len(directories) == 0 {
		c.Status(http.StatusNotFound)
		return
	}

	var dbUpdateOperations []mongo.WriteModel

	for _, directory := range directories {
		if directory.PreviousParentDirectory != "" {
			dbOperation := mongo.NewUpdateOneModel()
			dbOperation.SetFilter(bson.M{"_id": directory.Id})
			dbOperation.SetUpdate(bson.M{
				"$set": bson.M{
					"parent_directory":          directory.PreviousParentDirectory,
					"previous_parent_directory": "",
				},
			})

			dbUpdateOperations = append(dbUpdateOperations, dbOperation)

			searchDbQueryList = append(searchDbQueryList, map[string]interface{}{
				"_id":              directory.Id,
				"parent_directory": directory.PreviousParentDirectory,
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

	if err := c.BindJSON(&data); err != nil {
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
	}

	topDirectories := make([]*models.Directory, 0, len(data.Directories))

	childrenMap := make(map[string][]*models.Directory, len(directories))
	for idx, directory := range directories {
		if helper.ArrayContains(data.Directories, directory.Id) {
			topDirectories = append(topDirectories, &directories[idx])
		}
		directoryParentId := directory.ParentDirectory

		if value, ok := childrenMap[directoryParentId]; ok {
			childrenMap[directoryParentId] = append(value, &directories[idx])
		} else {
			childrenMap[directoryParentId] = []*models.Directory{&directories[idx]}
		}

	}

	directoriesToCopy := GetDirectoriesFromParentsAsPointers(topDirectories, childrenMap)

	// Used to delete files by parent directory
	filesParentList := make([]string, 0, len(directoriesToCopy))

	// Store pair NEW -> OLD and OLD -> NEW for directory ID
	directoryIdMap := make(map[string]string, len(directoriesToCopy))

	for _, directory := range directoriesToCopy {
		filesParentList = append(filesParentList, directory.Id)

		newId := uuid.New()

		directoryIdMap[newId.String()] = directory.Id
		directoryIdMap[directory.Id] = newId.String()

		newAccessKey, _ := auth.GenerateDirectoryAccessKey(
			newId.String(),
			auth.AllDirectoryPermissions,
		)

		children, exists := childrenMap[directory.Id]
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
		{Key: "parent_directory", Value: bson.D{{Key: "$in", Value: filesParentList}}},
	}

	if _, err := h.Db.Collection("directories").InsertMany(context.TODO(), models.DirectoryPointersToBsonNotEmpty(directoriesToCopy)); err != nil {
		log.Panic(err)
	}

	for _, dir := range directoriesToCopy {
		sourceDirectory := files.UploadDestination + directoryIdMap[dir.Id]
		destinationDirectory := files.UploadDestination + dir.Id
		if err := cp.Copy(sourceDirectory, destinationDirectory); err != nil {
			log.Panic(err)
		}
	}

	filesToCopy, err := models.FindFilesByFilter[models.File](h.Db, filter)
	if err != nil {
		log.Panic(err)
	}

	if len(filesToCopy) > 0 {
		fileIdMap := make(map[string]string, len(filesToCopy))
		for idx, file := range filesToCopy {
			newId := uuid.New()

			fileIdMap[newId.String()] = file.Id

			filesToCopy[idx].Id = newId.String()
			filesToCopy[idx].ParentDirectory = directoryIdMap[file.ParentDirectory]
			filesToCopy[idx].PreviousParentDirectory = ""
		}

		if _, err := h.Db.Collection("files").InsertMany(context.TODO(), models.FilesToBsonNotEmpty(filesToCopy)); err != nil {
			log.Panic(err)
		}

		for _, file := range filesToCopy {
			source := files.UploadDestination + file.ParentDirectory + "/" + fileIdMap[file.Id]
			destination := files.UploadDestination + file.ParentDirectory + "/" + file.Id

			if err := os.Rename(source, destination); err != nil {
				log.Panic(err)
			}
		}
	}
	c.JSON(http.StatusOK, topDirectories)
}
