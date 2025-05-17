package files

import (
	"archive/zip"
	"context"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/google/uuid"
	"github.com/meilisearch/meilisearch-go"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"ncloud-api/config"
	"ncloud-api/handlers/search"
	"ncloud-api/internal/fsutil"
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

func (h *Handler) InsertDocumentsToSearchDatabase(documents interface{}) {
	if err := search.InsertDocuments(h.SearchDb, "files", documents); err != nil {
		log.Println(err)
	}
}

func (h *Handler) Upload(c *gin.Context) {
	form, _ := c.MultipartForm()

	files := form.File["upload[]"]

	if len(files) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "no files",
		})
		return
	}

	directory := c.Param("id")
	user := auth.ExtractClaimsFromContext(c).Id

	path := user + "/" + directory

	for _, f := range files {
		fsutil.SaveFile(f, path)
	}

	// TODO: return files (check if it's needed on frontend)
}

func (h *Handler) UpdateFile(c *gin.Context) {
	oldpath := ""
	newpath := ""

	err := fsutil.Move(oldpath, newpath)
	if err != nil {
		log.Println(err)
		c.Status(http.StatusBadRequest)
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *Handler) GetFile(c *gin.Context) {
	path := c.Param("path")

	c.File(config.UploadDestination + path)
}

func (h *Handler) GetFiles(c *gin.Context) {
	type RequestData struct {
		Id        string   `json:"id"`
		AccessKey string   `json:"access_key"`
		Files     []string `json:"files"`
	}

	var data []RequestData

	if err := c.MustBindWith(&data, binding.JSON); err != nil {
		log.Println(err)
	}

	zipFile, err := os.CreateTemp("", "files.zip")
	if err != nil {
		log.Panic(err)
	}
	defer os.Remove(zipFile.Name())

	zipWriter := zip.NewWriter(zipFile)

	for _, directory := range data {
		err := helper.CreateZip(zipWriter, directory.Id, directory.Files)
		if err != nil {
			log.Println(err)
		}
	}

	zipWriter.Close()

	c.File(zipFile.Name())
}

func (h *Handler) DeleteFiles(c *gin.Context) {
	type RequestData struct {
		DirectoryId string   `json:"id"`
		AccessKey   string   `json:"access_key"`
		Files       []string `json:"files"`
	}

	var data []RequestData

	if err := c.MustBindWith(&data, binding.JSON); err != nil {
		log.Println(err)
	}

	deleteQuery := make([]bson.D, 0, len(data))
	filesToDelete := make([]string, 0)

	for _, directory := range data {
		if isValid := auth.ValidateAccessKeyWithId(directory.AccessKey, directory.DirectoryId); !isValid {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "invalid access key for directory: " + directory.DirectoryId,
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
			if err := os.Remove(config.UploadDestination + directory.DirectoryId + "/" + file); err != nil {
				log.Println(err)
			}

			filesToDelete = append(filesToDelete, file)
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
		Id          string `json:"id"`
		AccessKey   string `json:"access_key"`
		Directories []struct {
			Id        string   `json:"id"`
			AccessKey string   `json:"access_key"`
			Files     []string `json:"files"`
		} `json:"directories"`
	}

	var data RequestData

	if err := c.MustBindWith(&data, binding.JSON); err != nil {
		return
	}

	// Check if destination directory access key is valid and matches destination directory ID
	if directoryClaims, valid := auth.ValidateAccessKey(data.AccessKey); !valid ||
		directoryClaims.Id != data.Id {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid access key for directory: " + data.Id,
		})
		return
	}

	for _, directory := range data.Directories {
		// Check if directory access key is valid and matches directory ID
		if accessKeyClaims, valid := auth.ValidateAccessKey(directory.AccessKey); !valid ||
			accessKeyClaims.Id != directory.Id {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "invalid access key for directory: " + directory.Id,
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
					"parent_directory":          data.Id,
					"previous_parent_directory": directory.Id,
				},
			})

			operations = append(operations, dbOperation)

			searchDbFileList = append(searchDbFileList, map[string]interface{}{
				"_id":              file,
				"parent_directory": data.Id,
			})

			// move file to destination directory
			if err := os.Rename(
				config.UploadDestination+directory.Id+"/"+file,
				config.UploadDestination+data.Id+"/"+file,
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
	if _, err := h.SearchDb.Index("files").UpdateDocuments(searchDbFileList); err != nil {
		log.Println(err)
	}
	c.JSON(http.StatusOK, gin.H{
		"updated": res.ModifiedCount,
	})
}

func (h *Handler) RestoreFiles(c *gin.Context) {
	userClaims := auth.ExtractClaimsFromContext(c)

	type RequestData struct {
		Files []string `json:"files"`
	}

	var data RequestData

	if err := c.BindJSON(&data); err != nil {
		return
	}

	// List for search db update operation
	searchDbQueryList := make([]map[string]interface{}, 0, len(data.Files))

	filter := bson.D{
		{Key: "_id", Value: bson.D{{Key: "$in", Value: data.Files}}},
		{Key: "user", Value: userClaims.Id},
	}
	filesToRestore, err := models.FindFilesByFilter[models.File](h.Db, filter)
	if err != nil {
		log.Panic(err)
	}

	dbUpdateOperations := make([]mongo.WriteModel, 0, len(data.Files))

	for _, file := range filesToRestore {
		// Check if previous parent directory isn't empty
		if file.PreviousParentDirectory != "" {
			operation := mongo.NewUpdateOneModel()
			operation.SetFilter(bson.M{"_id": file.Id})
			operation.SetUpdate(bson.M{
				"$set": bson.M{
					"parent_directory":          file.PreviousParentDirectory,
					"previous_parent_directory": "",
				},
			})

			dbUpdateOperations = append(dbUpdateOperations, operation)

			searchDbQueryList = append(searchDbQueryList, map[string]interface{}{
				"_id":              file.Id,
				"parent_directory": file.PreviousParentDirectory,
			})
		}
	}

	res, err := h.Db.Collection("files").BulkWrite(context.TODO(), dbUpdateOperations)
	if err != nil {
		log.Panic(err)
	}

	// Move on disk
	for _, file := range filesToRestore {
		if err := os.Rename(
			config.UploadDestination+file.ParentDirectory+"/"+file.Id,
			config.UploadDestination+file.PreviousParentDirectory+"/"+file.Id,
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
		Files                []string `json:"files"`
		SourceAccessKey      string   `json:"source_access_key"`
		DestinationAccessKey string   `json:"destination_access_key"`
	}

	var data RequestData
	if err := c.BindJSON(&data); err != nil {
		return
	}

	sourceDirectory, isValid := auth.ValidateAccessKey(data.SourceAccessKey)
	if !isValid {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "invalid access key for source directory",
		})
		return
	}

	destinationDirectory, isValid := auth.ValidateAccessKey(data.DestinationAccessKey)
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

	filter := bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: data.Files}}}}
	files, err := models.FindFilesByFilter[models.File](h.Db, filter, opts)
	if err != nil {
		log.Panic(err)
	}

	if len(files) != len(data.Files) {
		c.Status(http.StatusBadRequest)
		return
	}

	for idx, file := range files {
		fileId, _ := uuid.NewUUID()
		file.Id = fileId.String()
		file.ParentDirectory = DESTINATION_DIRECTORY_ID
		files[idx] = file
	}

	insertOpts := options.InsertMany().SetOrdered(true)
	_, err = h.Db.Collection("files").
		InsertMany(context.TODO(), models.FilesToBsonNotEmpty(files), insertOpts)
	if err != nil {
		log.Panic(err)
	}

	for idx, file := range files {
		source := openFile(config.UploadDestination + SOURCE_DIRECTORY_ID + "/" + data.Files[idx])
		defer source.Close()

		destination := createFile(
			config.UploadDestination + DESTINATION_DIRECTORY_ID + "/" + file.Id,
		)
		defer destination.Close()

		if _, err := io.Copy(destination, source); err != nil {
			log.Panic(err)
		}

	}

	c.JSON(http.StatusOK, files)
}
