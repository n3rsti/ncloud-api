package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type AccessKey struct {
	Id          string
	Permissions []string
}

func FileAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		parentDirectoryAccessKey := c.GetHeader("DirectoryAccessKey")
		_, isValidAccessKey := ValidateAccessKey(parentDirectoryAccessKey)
		if !isValidAccessKey {
			c.Status(http.StatusForbidden)
			c.Abort()
			return
		}

		c.Next()
	}
}
