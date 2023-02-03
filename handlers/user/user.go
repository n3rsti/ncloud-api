package user

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"gopkg.in/validator.v2"
	"log"
	"ncloud-api/middleware/auth"
	"ncloud-api/models"
	"ncloud-api/utils/crypto"
	"net/http"
)

type UserHandler struct {
	Db *mongo.Database
}

func (h *UserHandler) Register(c *gin.Context) {
	var user models.User

	if err := c.BindJSON(&user); err != nil {
		return
	}

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

	_, err = collection.InsertOne(c, user.ToBSON())

	if err != nil {
		fmt.Println("Error during DB operation")
		return
	}

	collection = h.Db.Collection("directories")


	// Create main directory without name and parent directory
	_, _ = collection.InsertOne(c, bson.D{{"user", user.Username}})

	// Remove password so it won't be included in response
	user.Password = ""

	c.IndentedJSON(http.StatusCreated, user)
}

func (h *UserHandler) Login(c *gin.Context) {
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

	c.IndentedJSON(http.StatusOK, gin.H{
		"username": loginData.Username,
		"access_token":  accessToken,
		"refresh_token": refreshToken,
	})

	return
}

func (h *UserHandler) RefreshToken(c *gin.Context){
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

	c.IndentedJSON(http.StatusOK, gin.H{
		"access_token": accessToken,
	})
}