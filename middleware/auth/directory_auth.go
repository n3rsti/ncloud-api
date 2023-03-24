package auth

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

func DirectoryAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		directoryAccessKey := c.GetHeader("DirectoryAccessKey")
		directory := c.Param("parentDirectoryId")

		// Verify access key
		claims, isValidAccessKey := ValidateAccessKey(directoryAccessKey)
		if isValidAccessKey == false || directoryAccessKey == "" || claims.Id != directory {
			c.Status(http.StatusForbidden)
			c.Abort()
			return
		}
		c.Next()
	}
}