package auth

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

type AccessKey struct {
	Id string
	Permissions []string
}

func FileAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		fileAccessKey := c.GetHeader("FileAccessKey")

		idParam := c.Param("id")
		if idParam == "" {
			idParam = c.Request.RequestURI[len("/files/"):]
		}


		claims, isValidAccessKey := ValidateAccessKey(fileAccessKey)
		if isValidAccessKey == false || claims.Id != idParam {
			c.Status(http.StatusForbidden)
			c.Abort()
			return
		}

		c.Next()
	}
}
