package auth

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
)

func DirectoryAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		directoryAccessKey := c.GetHeader("DirectoryAccessKey")
		directory := c.Param("parentDirectoryId")

		// Verify access key
		if directoryAccessKey == "" || VerifyHMAC(directory, directoryAccessKey) == false {
			fmt.Println(directoryAccessKey, directory)
			c.Status(http.StatusForbidden)
			c.Abort()
			return
		}

		c.Next()
	}
}