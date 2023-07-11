package user

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/meilisearch/meilisearch-go"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gopkg.in/validator.v2"

	"ncloud-api/handlers/files"
	"ncloud-api/middleware/auth"
	"ncloud-api/models"
	"ncloud-api/utils/crypto"
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
		log.Println("Error during DB operation")
		c.JSON(http.StatusConflict, gin.H{
			"error": "user already exists",
		})
		return
	}

	collection = h.Db.Collection("directories")

	// Create trash and main directory documents
	docs := []interface{}{
		bson.D{
			{Key: "user", Value: userInsertResult.InsertedID.(primitive.ObjectID).Hex()},
			{Key: "name", Value: "Main"},
		},
		bson.D{
			{Key: "user", Value: userInsertResult.InsertedID.(primitive.ObjectID).Hex()},
			{Key: "name", Value: "Trash"},
		},
	}

	opts := options.InsertMany().SetOrdered(true)
	res, _ := collection.InsertMany(c, docs, opts)

	// Generate access keys for created directories
	mainDirId := res.InsertedIDs[0].(primitive.ObjectID).Hex()
	trashId := res.InsertedIDs[1].(primitive.ObjectID).Hex()

	// TODO: do something on error
	if err := os.Mkdir(files.UploadDestination+mainDirId, 0700); err != nil {
		log.Println(err)
	}
	if err := os.Mkdir(files.UploadDestination+trashId, 0700); err != nil {
		log.Println(err)
	}

	permissions := []string{auth.PermissionRead, auth.PermissionUpload}
	mainDirAccessKey, _ := auth.GenerateFileAccessKey(mainDirId, permissions)
	trashAccessKey, _ := auth.GenerateFileAccessKey(trashId, permissions)

	collection.UpdateByID(
		c,
		res.InsertedIDs[0],
		bson.D{{Key: "$set", Value: bson.M{"access_key": mainDirAccessKey}}},
	)
	collection.UpdateByID(
		c,
		res.InsertedIDs[1],
		bson.D{{Key: "$set", Value: bson.M{"access_key": trashAccessKey}}},
	)

	collection = h.Db.Collection("user")
	collection.UpdateByID(c, userInsertResult.InsertedID,
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "trash_access_key", Value: trashAccessKey},
		}}},
	)

	// Remove password so it won't be included in response
	user.Password = ""

	// Add to search database
	if _, err := h.SearchDb.Index("directories").AddDocuments(&SearchDatabaseData{
		Id:   res.InsertedIDs[0].(primitive.ObjectID).Hex(),
		Name: "Main",
		User: userInsertResult.InsertedID.(primitive.ObjectID).Hex(),
	}); err != nil {
		log.Println(err)
	}

	if _, err := h.SearchDb.Index("directories").AddDocuments(&SearchDatabaseData{
		Id:   res.InsertedIDs[1].(primitive.ObjectID).Hex(),
		Name: "Trash",
		User: userInsertResult.InsertedID.(primitive.ObjectID).Hex(),
	}); err != nil {
		log.Println(err)
	}

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
	err := collection.FindOne(c, bson.D{{Key: "username", Value: loginData.Username}}).
		Decode(&result)
	if err != nil {
		fmt.Println(err)
		c.Status(http.StatusForbidden)
		return
	}

	passwordHash := result["password"].(string)

	isValidPassword := crypto.ComparePasswordAndHash(loginData.Password, passwordHash)
	if !isValidPassword {
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
		"username":         loginData.Username,
		"access_token":     accessToken,
		"refresh_token":    refreshToken,
		"trash_access_key": trashAccessKey,
	})
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
