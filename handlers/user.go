package handlers

import (
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/mongo"
	"ncloud-api/models"
	"net/http"
)

type UserHandler struct {
	Db *mongo.Database
}

func (h *UserHandler) Register (c *gin.Context){
	var user models.User

	if err := c.BindJSON(&user); err != nil {
		return
	}


	

	c.IndentedJSON(http.StatusCreated, user)
}
