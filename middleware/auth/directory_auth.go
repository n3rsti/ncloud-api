package auth

import (
	"github.com/gin-gonic/gin"
)

func DirectoryAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// TODO refactor files.go auth into DirectoryAuth module

		c.Next()
	}
}