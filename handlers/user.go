package handlers

import (
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/mongo"
	"gopkg.in/validator.v2"
	"ncloud-api/models"
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
		return
	}

	// Insert to DB
	collection := h.Db.Collection("user")

	_, err := collection.InsertOne(c, user.ToBSON())

	if err != nil {
		return
	}

	c.IndentedJSON(http.StatusCreated, user)
}
