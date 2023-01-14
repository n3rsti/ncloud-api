package upload

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
)

const uploadDestination = "/var/ncloud_upload/"

func Upload(c *gin.Context){
	file, _ := c.FormFile("file")


	err := c.SaveUploadedFile(file, uploadDestination + file.Filename)
	if err != nil {
		log.Panic(err)
	}

	c.String(http.StatusOK, fmt.Sprintf("'%s' uploaded!", file.Filename))
}
