package user

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/meilisearch/meilisearch-go"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gopkg.in/validator.v2"

	"ncloud-api/config"
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

	userId, _ := uuid.NewUUID()
	user.Id = userId.String()

	permissions := []string{auth.PermissionRead, auth.PermissionUpload}

	mainId, _ := uuid.NewUUID()
	accessKey, _ := auth.GenerateDirectoryAccessKey(mainId.String(), permissions)
	mainDir := models.Directory{
		Name:      "Main",
		User:      userId.String(),
		Id:        mainId.String(),
		AccessKey: accessKey,
	}

	trashId, _ := uuid.NewUUID()
	trashAccessKey, _ := auth.GenerateDirectoryAccessKey(trashId.String(), permissions)
	trashDir := models.Directory{
		Name:      "Trash",
		User:      userId.String(),
		Id:        trashId.String(),
		AccessKey: trashAccessKey,
	}

	user.TrashAccessKey = trashAccessKey

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
		c.Status(http.StatusInternalServerError)
		log.Println("Error while creating password hash")
		return
	}

	user.Password = passwordHash

	_, err = collection.InsertOne(c, user.ToBSON())
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{
			"error": "user already exists",
		})
		return
	}

	collection = h.Db.Collection("directories")

	_, _ = collection.InsertMany(
		c,
		models.DirectoriesToBsonNotEmpty([]models.Directory{mainDir, trashDir}),
	)

	// TODO: do something on error
	if err := os.Mkdir(config.UploadDestination+mainId.String(), 0700); err != nil {
		log.Println(err)
	}
	if err := os.Mkdir(config.UploadDestination+trashId.String(), 0700); err != nil {
		log.Println(err)
	}

	// Remove password so it won't be included in response
	user.Password = ""

	// Add to search database
	if _, err := h.SearchDb.Index("directories").AddDocuments(&SearchDatabaseData{
		Id:   mainId.String(),
		Name: "Main",
		User: userId.String(),
	}); err != nil {
		log.Println(err)
	}

	if _, err := h.SearchDb.Index("directories").AddDocuments(&SearchDatabaseData{
		Id:   trashId.String(),
		Name: "Trash",
		User: userId.String(),
	}); err != nil {
		log.Println(err)
	}

	c.JSON(http.StatusCreated, user)
}

func (h *Handler) Login(c *gin.Context) {
	type RequestData struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	var data RequestData

	if err := c.BindJSON(&data); err != nil {
		return
	}

	if data.Username == "" || data.Password == "" {
		log.Println("Password or username empty")
		c.Status(http.StatusBadRequest)
		return
	}

	var result bson.M

	collection := h.Db.Collection("user")
	err := collection.FindOne(c, bson.D{{Key: "username", Value: data.Username}}).
		Decode(&result)
	if err != nil {
		log.Println(err)
		c.Status(http.StatusForbidden)
		return
	}

	passwordHash := result["password"].(string)

	isValidPassword := crypto.ComparePasswordAndHash(data.Password, passwordHash)
	if !isValidPassword {
		log.Println(err)
		c.Status(http.StatusForbidden)
		return
	}

	userId := result["_id"].(string)

	accessToken, refreshToken, err := auth.GenerateTokens(userId)
	if err != nil {
		log.Panic(err)
	}

	trashAccessKey := result["trash_access_key"].(string)

	c.JSON(http.StatusOK, gin.H{
		"username":         data.Username,
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

func (h *Handler) DeleteUser(c *gin.Context) {
	userId := c.Param("id")
	claims := auth.ExtractClaimsFromContext(c)

	if userId != claims.Id {
		c.Status(http.StatusBadRequest)
		return
	}

	filter := bson.D{{Key: "_id", Value: claims.Id}}
	res, err := h.Db.Collection("user").DeleteOne(context.TODO(), filter)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	if res.DeletedCount != 1 {
		c.Status(http.StatusNotFound)
		return
	}

	opts := options.Find().SetProjection(
		bson.D{
			{Key: "_id", Value: 1},
		},
	)

	filter = bson.D{{Key: "user", Value: claims.Id}}
	directoriesToDelete, err := models.FindDirectoriesByFilter[models.Directory](h.Db, filter, opts)
	if err != nil {
		log.Println(err)
	}

	for _, directory := range directoriesToDelete {
		if err = os.RemoveAll(config.UploadDestination + directory.Id); err != nil {
			log.Println(err)
		}
	}

	_, err = h.Db.Collection("directories").DeleteMany(context.TODO(), filter)
	if err != nil {
		log.Println(err)
	}
	_, err = h.Db.Collection("files").DeleteMany(context.TODO(), filter)
	if err != nil {
		log.Println(err)
	}

	h.SearchDb.Index("directories").DeleteDocumentsByFilter("user = " + claims.Id)
	h.SearchDb.Index("files").DeleteDocumentsByFilter("user = " + claims.Id)

	c.Status(http.StatusNoContent)
}
