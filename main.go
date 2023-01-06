package main

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

func health(c *gin.Context) {
	c.IndentedJSON(http.StatusOK, map[string]string{"ok": "true"})
}

func main() {
	router := gin.Default()

	router.GET("/api/health", health)

	router.Run("localhost:8080")
}
