package files

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/meilisearch/meilisearch-go"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"log"
	"mime/multipart"
	"ncloud-api/handlers/search"
	"ncloud-api/middleware/auth"
	"ncloud-api/models"
	"net/http"
	"os"
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
	file, _ := c.FormFile("file")
	directory := c.Param("id")
	claims := auth.ExtractClaimsFromContext(c)

	// Convert directoryId to ObjectId
	// There is no need to verify it because it is verified in directoryAuth
	parentDirObjectId, err := primitive.ObjectIDFromHex(directory)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	fileContentType, err := getFileContentType(file)

	newFile := models.File{
		Name:            file.Filename,
		ParentDirectory: parentDirObjectId,
		User:            claims.Id,
		Type:            fileContentType,
		Size:            file.Size,
	}


	if err := newFile.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err,
		})
		return
	}

	collection := h.Db.Collection("files")

	res, err := collection.InsertOne(c, newFile.ToBSON())

	if err != nil {
		log.Panic(err)
	}

	// Convert ID to string
	fileId := res.InsertedID.(primitive.ObjectID).Hex()

	if err = c.SaveUploadedFile(file, UploadDestination+directory+"/"+fileId); err != nil {
		// Remove file document if saving it wasn't successful
		_, _ = collection.DeleteOne(c, bson.D{{"_id", res.InsertedID}})
		log.Panic(err)
	}

	permissions := auth.AllFilePermissions
	// No need to verify directory, because it is verified by parsing it to primitive.ObjectID (parentDirObjectId)
	fileAccessKey, err := auth.GenerateFileAccessKey(fileId, permissions, directory)

	if _, err = collection.UpdateByID(c, res.InsertedID, bson.D{{"$set", bson.M{"access_key": fileAccessKey}}}); err != nil {
		log.Panic(err)
	}

	type FileResponse struct {
		Id        string `json:"id"`
		Name      string `json:"name"`
		AccessKey string `json:"access_key"`
		Directory string `json:"parent_directory"`
		Type      string `json:"type"`
		Size      int64  `json:"size"`
	}

	// Update search database
	h.UpdateOrAddToSearchDatabase(&SearchDatabaseData{
		Id:        fileId,
		Name:      file.Filename,
		Directory: directory,
		User:      claims.Id,
	})

	filesResponse := []FileResponse{
		{
			Id:        fileId,
			Name:      file.Filename,
			AccessKey: fileAccessKey,
			Directory: directory,
			Type:      fileContentType,
			Size:      file.Size,
		},
	}

	c.JSON(http.StatusCreated, filesResponse)
}

// DeleteFile
//
// # Deletes file from server storage and database
//
// To avoid confusion: user is already authenticated and authorized at this point from file_auth
func (h *Handler) DeleteFile(c *gin.Context) {
	fileId := c.Param("id")
	fileAccessKey, _ := auth.ValidateAccessKey(c.GetHeader("FileAccessKey"))
	parentDirectory := fileAccessKey.ParentDirectory

	// Convert to ObjectID
	hexFileId, err := primitive.ObjectIDFromHex(fileId)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}

	// Remove file
	err = os.Remove(UploadDestination + parentDirectory + "/" + fileId)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	collection := h.Db.Collection("files")

	_, err = collection.DeleteOne(c, bson.D{{"_id", hexFileId}})

	// Update search database
	h.DeleteFromSearchDatabase([]string{fileId})

	c.Status(http.StatusNoContent)
}

func (h *Handler) UpdateFile(c *gin.Context) {
	claims := auth.ExtractClaimsFromContext(c)
	// Bind request body to File model
	var file models.File
	fileAccessKey, _ := auth.ValidateAccessKey(c.GetHeader("FileAccessKey"))

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

	// These values can't be modified
	if file.Size != 0 || file.User != "" || file.Id != "" || file.Type != "" || file.AccessKey != "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "attempt to modify restricted fields",
		})
		return
	}

	// Verify new parent directory access key (if user wants to change it)
	if !file.ParentDirectory.IsZero() && c.GetHeader("DirectoryAccessKey") != "" {
		parentDirectoryAccessKey := c.GetHeader("DirectoryAccessKey")

		_, validAccessKey := auth.ValidateAccessKey(parentDirectoryAccessKey)

		if validAccessKey == false {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "invalid directory access key",
			})
			return
		}
	} else if !file.ParentDirectory.IsZero() && c.GetHeader("DirectoryAccessKey") == "" {
		// If user don't provide directory access key, we perform database check for directory ownership
		var result bson.M

		directoryCollection := h.Db.Collection("directories")
		err := directoryCollection.FindOne(c, bson.D{{"_id", file.ParentDirectory}}).Decode(&result)
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

	// If parentDirectory is changed, move file to a new parentDirectory on disk
	if !file.ParentDirectory.IsZero() {
		if err := os.Rename(
			UploadDestination+fileAccessKey.ParentDirectory+"/"+fileAccessKey.Id,
			UploadDestination+file.ParentDirectory.Hex()+"/"+fileAccessKey.Id,
		); err != nil {
			log.Panic(err)
		}

		// Update access key: copy previous access key, but replace parentDirectory with a new one
		updatedAccessKey, err := auth.GenerateFileAccessKey(fileAccessKey.Id, fileAccessKey.Permissions, file.ParentDirectory.Hex())
		if err != nil {
			log.Print(err)
			c.Status(http.StatusInternalServerError)
			return
		}

		file.AccessKey = updatedAccessKey
	}

	// Update file record
	fileCollection := h.Db.Collection("files")
	fileId := c.Param("id")

	hexId, err := primitive.ObjectIDFromHex(fileId)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}

	_, err = fileCollection.UpdateByID(c, hexId, bson.D{{"$set", file.ToBSONnotEmpty()}})
	if err != nil {
		fmt.Println(err)
		c.Status(http.StatusBadRequest)
		return
	}

	// Update search database
	var parentDirectory string
	if file.ParentDirectory.IsZero() {
		parentDirectory = ""
	} else {
		parentDirectory = file.ParentDirectory.Hex()
	}

	h.UpdateOrAddToSearchDatabase(&SearchDatabaseData{
		Id:        fileId,
		Name:      file.Name,
		Directory: parentDirectory,
		User:      claims.Id,
	})

	c.Status(http.StatusNoContent)
}

func (h *Handler) GetFile(c *gin.Context) {
	// Don't need to validate access key, because it is verified in FileAuth
	fileAccessKey := c.GetHeader("FileAccessKey")
	claims, _ := auth.ValidateAccessKey(fileAccessKey)

	c.File(UploadDestination + claims.ParentDirectory + "/" + claims.Id)
}
