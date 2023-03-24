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
	"ncloud-api/utils/helper"
	"net/http"
	"os"
)

const uploadDestination = "/var/ncloud_upload/"

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
	directory := c.Param("parentDirectoryId")
	claims := auth.ExtractClaimsFromContext(c)


	// Convert to ObjectId
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

	if err = c.SaveUploadedFile(file, uploadDestination+fileId); err != nil {
		// Remove file document if saving it wasn't successful
		_, _ = collection.DeleteOne(c, bson.D{{"_id", res.InsertedID}})
		log.Panic(err)
		return
	}

	permissions := auth.AllFilePermissions
	fileAccessKey, err := auth.GenerateFileAccessKey(fileId, permissions)

	collection.UpdateByID(c, res.InsertedID, bson.D{{"$set", bson.M{"access_key": fileAccessKey}}})

	type FileResponse struct {
		Id        string `json:"id"`
		Name      string `json:"name"`
		AccessKey string `json:"access_key"`
	}

	filesResponse := []FileResponse{
		{
			Id:        fileId,
			Name:      file.Filename,
			AccessKey: fileAccessKey,
		},
	}

	c.IndentedJSON(http.StatusCreated, filesResponse)
}

func (h *FileHandler) CreateDirectory(c *gin.Context) {
	parentDirectoryId := c.Param("parentDirectoryId")

	var data models.Directory

	if err := c.BindJSON(&data); err != nil {
		return
	}

	// Set parentDirectoryId from URL
	data.ParentDirectory = parentDirectoryId

	if data.Name == "" {
		c.IndentedJSON(http.StatusBadRequest, gin.H{
			"error": "empty name or parent directory",
		})
		return
	}

	user := auth.ExtractClaimsFromContext(c)

	data.User = user.Id

	collection := h.Db.Collection("directories")

	hexId, err := primitive.ObjectIDFromHex(data.ParentDirectory)
	if err != nil {
		return
	}

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

	data.ParentDirectoryObjectId = hexId

	res, err := collection.InsertOne(c, data.ToBSON())

	if err != nil {
		fmt.Println(err)
		c.Status(http.StatusBadRequest)
	}

	fileId := res.InsertedID.(primitive.ObjectID).Hex()
	data.Id = fileId

	// Create and set access key to directory
	newDirectoryAccessKey, err := auth.GenerateFileAccessKey(fileId, auth.AllDirectoryPermissions)
	collection.UpdateByID(c, res.InsertedID, bson.D{{"$set", bson.M{"access_key": newDirectoryAccessKey}}})

	data.AccessKey = newDirectoryAccessKey

	c.IndentedJSON(http.StatusCreated, data)
}

func (h *FileHandler) GetDirectoryWithFiles(c *gin.Context) {
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

	res, err := collection.DeleteOne(c, bson.D{{"_id", hexFileId}})

	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}

	fmt.Println(res.DeletedCount)

	c.Status(http.StatusNoContent)
}

func (h *FileHandler) UpdateFile(c *gin.Context) {
	// Bind request body to File model
	var file models.File

	if err := c.MustBindWith(&file, binding.JSON); err != nil{
		return
	}

	// These values can't be edited
	if file.Size != 0 || file.User != "" || file.Id != "" || file.Type != "" || file.AccessKey != "" {
		c.Status(http.StatusBadRequest)
		c.IndentedJSON(http.StatusUnauthorized, gin.H{
			"error": "attempt to edit restricted fields",
		})
		return
	}

	// Verify new parent directory access key (if user wants to change it)
	if !file.ParentDirectory.IsZero() {
		parentDirectoryAccessKey := c.GetHeader("DirectoryAccessKey")

		claims, validAccessKey := auth.ValidateAccessKey(parentDirectoryAccessKey)

		if validAccessKey == false {
			c.IndentedJSON(http.StatusForbidden, gin.H{
				"error": "invalid directory access key",
			})
			return
		}

		if helper.StringArrayContains(claims.Permissions, auth.PermissionModify) == false {
			c.IndentedJSON(http.StatusForbidden, gin.H{
				"error": "no permissions to modify",
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
