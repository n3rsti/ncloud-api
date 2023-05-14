package files

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"log"
	"mime/multipart"
	"ncloud-api/middleware/auth"
	"ncloud-api/models"
	"net/http"
	"os"
)

const UploadDestination = "/var/ncloud_upload/"

type FileHandler struct {
	Db *mongo.Database
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

func (h *FileHandler) Upload(c *gin.Context) {
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

	collection := h.Db.Collection("files")

	res, err := collection.InsertOne(c, newFile.ToBSON())

	if err != nil {
		log.Panic(err)
		return
	}

	// Convert ID to string
	fileId := res.InsertedID.(primitive.ObjectID).Hex()

	if err = c.SaveUploadedFile(file, UploadDestination+ directory + "/" + fileId); err != nil {
		// Remove file document if saving it wasn't successful
		_, _ = collection.DeleteOne(c, bson.D{{"_id", res.InsertedID}})
		log.Panic(err)
		return
	}

	permissions := auth.AllFilePermissions
	// No need to verify directory, because it is verified by parsing it to primitive.ObjectID (parentDirObjectId)
	fileAccessKey, err := auth.GenerateFileAccessKey(fileId, permissions, directory)

	collection.UpdateByID(c, res.InsertedID, bson.D{{"$set", bson.M{"access_key": fileAccessKey}}})

	type FileResponse struct {
		Id        string `json:"id"`
		Name      string `json:"name"`
		AccessKey string `json:"access_key"`
		Directory string `json:"parent_directory"`
		Type      string `json:"type"`
		Size      int64  `json:"size"`
	}

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

	c.IndentedJSON(http.StatusCreated, filesResponse)
}

// DeleteFile
//
// # Deletes file from server storage and database
//
// To avoid confusion: user is already authenticated and authorized at this point from file_auth
func (h *FileHandler) DeleteFile(c *gin.Context) {
	fileId := c.Param("id")

	// Convert to ObjectID
	hexFileId, err := primitive.ObjectIDFromHex(fileId)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}

	// Remove file
	err = os.Remove("/var/ncloud_upload/" + fileId)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	collection := h.Db.Collection("files")

	_, err = collection.DeleteOne(c, bson.D{{"_id", hexFileId}})

	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}


	c.Status(http.StatusNoContent)
}

func (h *FileHandler) UpdateFile(c *gin.Context) {
	// Bind request body to File model
	var file models.File

	if err := c.MustBindWith(&file, binding.JSON); err != nil {
		return
	}

	// These values can't be modified
	if file.Size != 0 || file.User != "" || file.Id != "" || file.Type != "" || file.AccessKey != "" {
		c.IndentedJSON(http.StatusBadRequest, gin.H{
			"error": "attempt to modify restricted fields",
		})
		return
	}

	// Verify new parent directory access key (if user wants to change it)
	if !file.ParentDirectory.IsZero() && c.GetHeader("DirectoryAccessKey") != "" {
		parentDirectoryAccessKey := c.GetHeader("DirectoryAccessKey")

		_, validAccessKey := auth.ValidateAccessKey(parentDirectoryAccessKey)

		if validAccessKey == false {
			c.IndentedJSON(http.StatusForbidden, gin.H{
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
			c.IndentedJSON(http.StatusBadRequest, gin.H{
				"error": "new parent directory not found",
			})

			return
		}

		claims := auth.ExtractClaimsFromContext(c)

		if result["user"] != claims.Id {
			c.IndentedJSON(http.StatusForbidden, gin.H{
				"error": "no access to new parent directory",
			})
			return
		}

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

	c.Status(http.StatusNoContent)
}

func (h *FileHandler) GetFile(c *gin.Context){
	// Don't need to validate access key, because it is verified in FileAuth
	fileAccessKey := c.GetHeader("FileAccessKey")
	claims, _ := auth.ValidateAccessKey(fileAccessKey)

	c.File(UploadDestination + claims.ParentDirectory + "/" + claims.Id)
}
