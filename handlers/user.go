package handlers

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/mongo"
	"gopkg.in/validator.v2"
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

	c.IndentedJSON(http.StatusCreated, user)
}
