package user

import (
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gopkg.in/validator.v2"
	"log"
	"ncloud-api/handlers/files"
	"ncloud-api/middleware/auth"
	"ncloud-api/models"
	"ncloud-api/utils/crypto"
	"net/http"
	"os"
)

type Handler struct {
	Db *mongo.Database
}

func (h *Handler) Register(c *gin.Context) {
	var user models.User

	if err := c.BindJSON(&user); err != nil {
		return
	}

	user.TrashAccessKey = ""

	if err := validator.Validate(user); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Error in validation",
		})
		return
	}

	// Insert to DB
	collection := h.Db.Collection("user")

	// hash password
	passwordHash, err := crypto.GenerateHash(user.Password)

	if err != nil {
		fmt.Println("Error while creating password hash")
		return
	}

	user.Password = passwordHash

	userInsertResult, err := collection.InsertOne(c, user.ToBSON())

	if err != nil {
		fmt.Println("Error during DB operation")
		return
	}

	collection = h.Db.Collection("directories")

	// Create trash and main directory documents
	docs := []interface{}{
		bson.D{{"user", userInsertResult.InsertedID.(primitive.ObjectID).Hex()}, {"name", "Main"}},
		bson.D{{"user", userInsertResult.InsertedID.(primitive.ObjectID).Hex()}, {"name", "Trash"}},
	}

	opts := options.InsertMany().SetOrdered(true)
	res, _ := collection.InsertMany(c, docs, opts)

	// Generate access keys for created directories
	mainDirId := res.InsertedIDs[0].(primitive.ObjectID).Hex()
	trashId := res.InsertedIDs[1].(primitive.ObjectID).Hex()

	// TODO: do something on error
	if err := os.Mkdir(files.UploadDestination + mainDirId, 0700); err != nil {
		log.Println(err)
	}
	if err := os.Mkdir(files.UploadDestination + trashId, 0700); err != nil {
		log.Println(err)
	}

	permissions := []string{auth.PermissionRead, auth.PermissionUpload}
	mainDirAccessKey, err := auth.GenerateFileAccessKey(mainDirId, permissions)
	trashAccessKey, err := auth.GenerateFileAccessKey(trashId, permissions)

	collection.UpdateByID(c, res.InsertedIDs[0], bson.D{{"$set", bson.M{"access_key": mainDirAccessKey}}})

	collection = h.Db.Collection("user")
	collection.UpdateByID(c, userInsertResult.InsertedID,
		bson.D{{"$set", bson.D{
			{"trash_access_key", trashAccessKey},
		}}},
	)

	// Remove password so it won't be included in response
	user.Password = ""

	c.JSON(http.StatusCreated, user)
}

func (h *Handler) Login(c *gin.Context) {
	type LoginData struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	var loginData LoginData

	if err := c.BindJSON(&loginData); err != nil {
		return
	}

	if loginData.Username == "" || loginData.Password == "" {
		fmt.Println("Password or username empty")
		c.Status(http.StatusBadRequest)
		return
	}

	var result bson.M

	collection := h.Db.Collection("user")
	err := collection.FindOne(c, bson.D{{"username", loginData.Username}}).Decode(&result)

	if err != nil {
		fmt.Println(err)
		c.Status(http.StatusForbidden)
		return
	}

	passwordHash := result["password"].(string)

	isValidPassword := crypto.ComparePasswordAndHash(loginData.Password, passwordHash)
	if isValidPassword == false {
		fmt.Println(err)
		c.Status(http.StatusForbidden)
		return
	}

	userId := result["_id"].(primitive.ObjectID).Hex()

	accessToken, refreshToken, err := auth.GenerateTokens(userId)
	if err != nil {
		log.Panic(err)
		return
	}

	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}

	trashAccessKey := result["trash_access_key"].(string)

	c.JSON(http.StatusOK, gin.H{
		"username":      loginData.Username,
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"trash_access_key": trashAccessKey,
	})

	return
}

func (h *Handler) RefreshToken(c *gin.Context) {
	token := c.GetHeader("Authorization")

	if len(token) < len("Bearer ") {
		c.Status(http.StatusBadRequest)
		return
	}
	token = token[len("Bearer "):]

	accessToken, err := auth.GenerateAccessTokenFromRefreshToken(token)

	if err != nil {
		c.Status(http.StatusUnauthorized)
		c.Header("WWW-Authenticate", err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token": accessToken,
	})
}

func (h *Handler) getMainDirectoryAccessKey(c *gin.Context, userId string) (string, error) {
	collection := h.Db.Collection("directories")

	var result bson.M

	if err := collection.FindOne(c, bson.D{{"name", ""}, {"user", userId}}).Decode(&result); err != nil {
		return "", errors.New("error finding directory")
	}

	return result["access_key"].(string), nil

}
