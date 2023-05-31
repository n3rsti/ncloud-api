package search

import (
	"github.com/gin-gonic/gin"
	"github.com/meilisearch/meilisearch-go"
	"log"
	"ncloud-api/middleware/auth"
	"net/http"
)

type Handler struct {
	Db *meilisearch.Client
}

func (h *Handler) FindDirectoriesAndFiles(c *gin.Context) {
	claims := auth.ExtractClaimsFromContext(c)
	name := c.Query("name")
	parentDirectory := c.Query("parent_directory")

	filter := [][]string{
		{"user = '" + claims.Id + "'"},
	}

	if parentDirectory != "" {
		filter = append(filter, []string{"parent_directory = '" + parentDirectory + "'"})
	}



	resp, err := h.Db.Index("directories").Search(name, &meilisearch.SearchRequest{
		Filter: filter,
	})

	resp2, err := h.Db.Index("files").Search(name, &meilisearch.SearchRequest{
		Filter: filter,
	})


	if err != nil {
		log.Println(err)
		c.Status(http.StatusInternalServerError)
		return
	}

	type Response struct {
		Directories []interface{}
		Files []interface{}
	}

	c.IndentedJSON(http.StatusOK, &Response{
		Directories: resp.Hits,
		Files: resp2.Hits,
	})
}