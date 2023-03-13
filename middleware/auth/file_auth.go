package auth

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

func FileAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		fileAccessKey := c.GetHeader("FileAccessKey")

		idParam := c.Param("id")
		if idParam == "" {
			idParam = c.Request.RequestURI[len("/files/"):]
		}

		if VerifyHMAC(idParam, fileAccessKey) == false {
			c.Status(http.StatusForbidden)
			c.Abort()
			return
		}

		c.Next()
	}
}
