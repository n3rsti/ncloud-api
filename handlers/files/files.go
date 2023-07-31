package files

import (
	"context"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/meilisearch/meilisearch-go"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"ncloud-api/handlers/search"
	"ncloud-api/middleware/auth"
	"ncloud-api/models"
)

const UploadDestination = "/var/ncloud_upload/"

type Handler struct {
	Db       *mongo.Database
	SearchDb *meilisearch.Client
}

type SearchDatabaseData struct {
	Id        string `json:"_id"`
	Name      string `json:"name,omitempty"`
	Directory string `json:"parent_directory,omitempty"`
	User      string `json:"user,omitempty"`
	Type      string `json:"type,omitempty"`
}

func (h *Handler) UpdateOrAddToSearchDatabase(document interface{}) {
	if err := search.UpdateDocuments(h.SearchDb, "files", document); err != nil {
		log.Println(err)
	}
}

func (h *Handler) DeleteFromSearchDatabase(id []string) {
	if err := search.DeleteDocuments(h.SearchDb, "files", id); err != nil {
		log.Println(err)
	}
}

func getFileContentType(file *multipart.FileHeader) (contentType string, err error) {
	f, err := file.Open()
	if err != nil {
		return "", err
	}

	defer f.Close()

	buf := make([]byte, 512)

	_, err = f.Read(buf)

	return http.DetectContentType(buf), err
}

func (h *Handler) Upload(c *gin.Context) {
	form, _ := c.MultipartForm()

	fmt.Println(form.File["upload[]"])
	files := form.File["upload[]"]

	if len(files) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "no files",
		})
	}

	// These 2 arrays below are separate because:
	// 1) collection.InsertMany() argument type must be []interface{} and it doesn't work with neither []bson.D nor []models.File,
	//		so fileObjects: []interface{} is used
	// 2) files must be updated with ID and access key after InsertMany operation, and we can't update []interface{} with a new field and value
	//		so filesToReturn: []models.File is used
	//
	// TODO: fix if you find a better solution, because we are using 2 arrays = 2x space
	// (though it might not be a problem since no one will upload million files at once (probably :-D ))

	// Array of files in format for database insert
	fileObjects := make([]interface{}, 0, len(files))

	// Array of files to return. They need to be updated after DB insert with access key and ID ...
	// ... because they must be included in endpoint response
	filesToReturn := make([]models.File, 0, len(files))

	directory := c.Param("id")
	claims := auth.ExtractClaimsFromContext(c)

	// Create array of files based on form data
	for _, file := range files {
		fileContentType, _ := getFileContentType(file)

		newFile := models.File{
			Name:            file.Filename,
			ParentDirectory: directory,
			User:            claims.Id,
			Type:            fileContentType,
			Size:            file.Size,
		}

		filesToReturn = append(filesToReturn, newFile)
		fileObjects = append(fileObjects, newFile.ToBSON())

		if err := newFile.Validate(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": err,
			})
			return
		}
	}

	collection := h.Db.Collection("files")

	opts := options.InsertMany().SetOrdered(true)

	res, err := collection.InsertMany(c, fileObjects, opts)
	if err != nil {
		log.Panic(err)
	}

	// Create and update access keys for each file
	// Update filesToReturn with created ID and access key
	for index, file := range files {
		// Convert ID to string
		fileId := res.InsertedIDs[index].(primitive.ObjectID)

		if err = c.SaveUploadedFile(file, UploadDestination+directory+"/"+fileId.Hex()); err != nil {
			// Remove file document if saving it wasn't successful
			_, _ = collection.DeleteOne(c, bson.D{{Key: "_id", Value: res.InsertedIDs[index]}})
			log.Panic(err)
		}

		filesToReturn[index].Id = fileId

		fileContentType, _ := getFileContentType(file)

		// Update search database
		h.UpdateOrAddToSearchDatabase(&SearchDatabaseData{
			Id:        fileId.Hex(),
			Name:      file.Filename,
			Directory: directory,
			User:      claims.Id,
			Type:      fileContentType,
		})
	}

	c.JSON(http.StatusCreated, filesToReturn)
}

func (h *Handler) UpdateFile(c *gin.Context) {
	parentDirectoryAccessKey, _ := auth.ValidateAccessKey(c.GetHeader("DirectoryAccessKey"))
	parentDirectoryObjId, _ := primitive.ObjectIDFromHex(parentDirectoryAccessKey.Id)

	// Bind request body to File model
	var file models.File

	if err := c.MustBindWith(&file, binding.JSON); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "bad format",
		})
		return
	}

	if err := file.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err,
		})
		return
	}

	// Update file record
	fileCollection := h.Db.Collection("files")
	fileId := c.Param("id")

	fileObjId, err := primitive.ObjectIDFromHex(fileId)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}

	_, err = fileCollection.UpdateOne(
		context.TODO(),
		bson.D{
			{Key: "_id", Value: fileObjId},
			{Key: "parent_directory", Value: parentDirectoryObjId},
		},
		bson.D{{Key: "$set", Value: bson.M{"name": file.Name}}},
	)
	if err != nil {
		fmt.Println(err)
		c.Status(http.StatusBadRequest)
		return
	}

	h.UpdateOrAddToSearchDatabase(&SearchDatabaseData{
		Id:   fileId,
		Name: file.Name,
	})

	c.Status(http.StatusNoContent)
}

func (h *Handler) GetFile(c *gin.Context) {
	// Don't need to validate access key, because it is verified in FileAuth
	fileId := c.Param("id")
	_, err := primitive.ObjectIDFromHex(fileId)
	if err != nil {
		c.Status(http.StatusNotFound)
	}

	directoryAccessKey := c.GetHeader("DirectoryAccessKey")
	directory, _ := auth.ValidateAccessKey(directoryAccessKey)

	c.File(UploadDestination + directory.Id + "/" + fileId)
}

func (h *Handler) DeleteFiles(c *gin.Context) {
	type RequestData struct {
		DirectoryId primitive.ObjectID   `json:"id"`
		AccessKey   string               `json:"access_key"`
		Files       []primitive.ObjectID `json:"files"`
	}

	var data []RequestData

	if err := c.MustBindWith(&data, binding.JSON); err != nil {
		log.Println(err)
	}

	deleteQuery := make([]bson.D, 0, len(data))
	filesToDelete := make([]string, 0)

	for _, directory := range data {
		if isValid := auth.ValidateAccessKeyWithId(directory.AccessKey, directory.DirectoryId.Hex()); !isValid {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "invalid access key for directory: " + directory.DirectoryId.Hex(),
			})
			return
		}

		deleteQuery = append(deleteQuery, bson.D{
			{Key: "parent_directory", Value: directory.DirectoryId},
			{Key: "_id", Value: bson.D{
				{Key: "$in", Value: directory.Files},
			}},
		})
	}

	collection := h.Db.Collection("files")

	result, err := collection.DeleteMany(context.TODO(), bson.D{{Key: "$or", Value: deleteQuery}})
	if err != nil {
		log.Panic(err)
	}

	for _, directory := range data {
		for _, file := range directory.Files {
			if err := os.Remove(UploadDestination + directory.DirectoryId.Hex() + "/" + file.Hex()); err != nil {
				log.Println(err)
			}

			filesToDelete = append(filesToDelete, file.Hex())
		}
	}

	if _, err := h.SearchDb.Index("files").DeleteDocuments(filesToDelete); err != nil {
		log.Println(err)
	}

	c.JSON(http.StatusOK, gin.H{
		"deleted": result.DeletedCount,
	})
}

func (h *Handler) ChangeDirectory(c *gin.Context) {
	// List of operations because we want to update all files at once instead of query for each directory with files
	// Usually it will be update for files from 1 directory, but we allow possibility of need to move many files from many directories
	// for example when we want to move all files matching specific query (e.g name)
	var operations []mongo.WriteModel

	// List of maps in format {"_id": "ID of file we want to move", "parent_directory": "ID of directory we want to move the file into"}
	// Used for search database update
	searchDbFileList := make([]map[string]interface{}, 0)

	type RequestData struct {
		Id          primitive.ObjectID `json:"id"`
		AccessKey   string             `json:"access_key"`
		Directories []struct {
			Id        primitive.ObjectID   `json:"id"`
			AccessKey string               `json:"access_key"`
			Files     []primitive.ObjectID `json:"files"`
		} `json:"directories"`
	}

	var requestData RequestData

	if err := c.MustBindWith(&requestData, binding.JSON); err != nil {
		log.Println(err)
	}

	// Check if destination directory access key is valid and matches destination directory ID
	if directoryClaims, valid := auth.ValidateAccessKey(requestData.AccessKey); !valid ||
		directoryClaims.Id != requestData.Id.Hex() {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid access key for directory: " + requestData.Id.Hex(),
		})
		return
	}

	for _, directory := range requestData.Directories {
		// Check if directory access key is valid and matches directory ID
		if accessKeyClaims, valid := auth.ValidateAccessKey(directory.AccessKey); !valid ||
			accessKeyClaims.Id != directory.Id.Hex() {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "invalid access key for directory: " + directory.Id.Hex(),
			})
			return
		}

		for _, file := range directory.Files {
			dbOperation := mongo.NewUpdateOneModel()
			// File from list in request body AND having parent_directory as directory ID from list
			// This removes possibility of user providing valid access key, but for different directory and trying to modify file without access to it
			dbOperation.SetFilter(bson.M{
				"_id":              file,
				"parent_directory": directory.Id,
			},
			)

			dbOperation.SetUpdate(bson.M{
				"$set": bson.M{
					"parent_directory":          requestData.Id,
					"previous_parent_directory": directory.Id,
				},
			})

			operations = append(operations, dbOperation)

			searchDbFileList = append(searchDbFileList, map[string]interface{}{
				"_id":              file.Hex(),
				"parent_directory": requestData.Id.Hex(),
			})

			// move file to destination directory
			if err := os.Rename(
				UploadDestination+directory.Id.Hex()+"/"+file.Hex(),
				UploadDestination+requestData.Id.Hex()+"/"+file.Hex(),
			); err != nil {
				log.Panic(err)
			}
		}

	}

	// update primary database
	res, err := h.Db.Collection("files").BulkWrite(context.TODO(), operations)
	if err != nil {
		log.Panic(err)
	}

	// update search database
	if response, err := h.SearchDb.Index("files").UpdateDocuments(searchDbFileList); err != nil {
		log.Println(err)
	} else {
		log.Println(response)
	}

	c.JSON(http.StatusOK, gin.H{
		"updated": res.ModifiedCount,
	})
}

func (h *Handler) RestoreFiles(c *gin.Context) {
	userClaims := auth.ExtractClaimsFromContext(c)

	type RequestData struct {
		Files []primitive.ObjectID `json:"files"`
	}

	var requestData RequestData

	if err := c.MustBindWith(&requestData, binding.JSON); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "bad request format",
		})
	}

	// List for search db update operation
	searchDbQueryList := make([]map[string]interface{}, 0, len(requestData.Files))

	dbResult := make([]bson.M, 0, len(requestData.Files))

	// Find files from request body list
	cursor, err := h.Db.Collection("files").
		Find(context.TODO(), bson.M{"_id": bson.M{"$in": requestData.Files}})
	if err != nil {
		log.Panic(err)
	}

	// Map results to bson.M format
	if err = cursor.All(context.TODO(), &dbResult); err != nil {
		log.Panic(err)
	}

	dbUpdateOperations := make([]mongo.WriteModel, 0, len(requestData.Files))

	for _, file := range dbResult {
		// Check if user is the owner of the file
		if file["user"].(string) != userClaims.Id {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "no access for file: " + file["_id"].(primitive.ObjectID).Hex(),
			})
		}

		// Check if previous parent directory isn't empty
		if !file["previous_parent_directory"].(primitive.ObjectID).IsZero() {
			operation := mongo.NewUpdateOneModel()
			operation.SetFilter(bson.M{"_id": file["_id"]})
			operation.SetUpdate(bson.M{
				"$set": bson.M{
					"parent_directory":          file["previous_parent_directory"],
					"previous_parent_directory": "",
				},
			})

			dbUpdateOperations = append(dbUpdateOperations, operation)

			searchDbQueryList = append(searchDbQueryList, map[string]interface{}{
				"_id":              file["_id"].(primitive.ObjectID).Hex(),
				"parent_directory": file["previous_parent_directory"].(primitive.ObjectID).Hex(),
			})
		}
	}

	res, err := h.Db.Collection("files").BulkWrite(context.TODO(), dbUpdateOperations)
	if err != nil {
		log.Panic(err)
	}

	// Move on disk
	for _, file := range dbResult {
		if err := os.Rename(
			UploadDestination+file["parent_directory"].(primitive.ObjectID).Hex()+"/"+file["_id"].(primitive.ObjectID).Hex(),
			UploadDestination+file["previous_parent_directory"].(primitive.ObjectID).Hex()+"/"+file["_id"].(primitive.ObjectID).Hex(),
		); err != nil {
			log.Println(err)
		}
	}

	// Update search database
	if _, err := h.SearchDb.Index("files").UpdateDocuments(searchDbQueryList); err != nil {
		log.Println(err)
	}

	c.JSON(http.StatusOK, gin.H{
		"updated": res.ModifiedCount,
	})
}

func createFile(destinationPath string) *os.File {
	destination, err := os.Create(destinationPath)
	if err != nil {
		log.Panic(err)
	}

	return destination
}

func openFile(sourcePath string) *os.File {
	source, err := os.Open(sourcePath)
	if err != nil {
		log.Panic(err)
	}

	return source
}

func (h *Handler) CopyFiles(c *gin.Context) {
	type RequestData struct {
		Files                []primitive.ObjectID `json:"files"`
		SourceAccessKey      string               `json:"source_access_key"`
		DestinationAccessKey string               `json:"destination_access_key"`
	}

	var requestData RequestData
	if err := c.MustBindWith(&requestData, binding.JSON); err != nil {
		return
	}

	sourceDirectory, isValid := auth.ValidateAccessKey(requestData.SourceAccessKey)
	if !isValid {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "invalid access key for source directory",
		})
		return
	}

	destinationDirectory, isValid := auth.ValidateAccessKey(requestData.DestinationAccessKey)
	if !isValid {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "invalid access key for destination directory",
		})
		return
	}

	SOURCE_DIRECTORY_ID := sourceDirectory.Id
	DESTINATION_DIRECTORY_ID := destinationDirectory.Id

	opts := options.Find().SetProjection(bson.D{
		{Key: "_id", Value: 1},
		{Key: "name", Value: 1},
		{Key: "parent_directory", Value: 1},
		{Key: "user", Value: 1},
		{Key: "type", Value: 1},
		{Key: "size", Value: 1},
	},
	)

	cursor, err := h.Db.Collection("files").
		Find(context.TODO(), bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: requestData.Files}}}}, opts)
	if err != nil {
		log.Panic(err)
		return
	}

	var files []models.File

	if err := cursor.All(context.TODO(), &files); err != nil {
		log.Panic(err)
		return
	}

	sourceFileIdList := make([]string, 0, len(files))
	for idx, file := range files {
		sourceFileIdList = append(sourceFileIdList, file.Id.Hex())
		file.Id = primitive.NilObjectID
		file.ParentDirectory = DESTINATION_DIRECTORY_ID
		files[idx] = file
	}

	insertOpts := options.InsertMany().SetOrdered(true)
	insertResult, err := h.Db.Collection("files").
		InsertMany(context.TODO(), models.FilesToBsonNotEmpty(files), insertOpts)
	if err != nil {
		log.Panic(err)
		return
	}

	if len(insertResult.InsertedIDs) != len(sourceFileIdList) {
		log.Panic("Error during insert")
	}

	for i := 0; i < len(insertResult.InsertedIDs); i++ {
		source := openFile(UploadDestination + SOURCE_DIRECTORY_ID + "/" + sourceFileIdList[i])
		defer source.Close()

		destination := createFile(
			UploadDestination + DESTINATION_DIRECTORY_ID + "/" + insertResult.InsertedIDs[i].(primitive.ObjectID).Hex(),
		)
		defer destination.Close()

		if _, err := io.Copy(destination, source); err != nil {
			log.Panic(err)
		}

		files[i].Id = insertResult.InsertedIDs[i].(primitive.ObjectID)
	}

	c.JSON(http.StatusOK, files)
}
