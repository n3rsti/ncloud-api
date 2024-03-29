package auth

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

func DirectoryAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		directoryAccessKey := c.GetHeader("DirectoryAccessKey")
		directory := c.Param("id")

		// Verify access key
		claims, isValidAccessKey := ValidateAccessKey(directoryAccessKey)
		if !isValidAccessKey || directoryAccessKey == "" || claims.Id != directory {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "invalid access key",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}