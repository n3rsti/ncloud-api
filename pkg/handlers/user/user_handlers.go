package user

import (
	"log"
	"ncloud-api/pkg/models"
	"ncloud-api/pkg/services"
	"ncloud-api/pkg/utils"
	"net/http"
)

// Register creates a new user
func Register(userService *services.UserService) http.Handler {
	return http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			user, problems, err := utils.DecodeValid[models.User](r)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			if len(problems) > 0 {
				w.WriteHeader(http.StatusBadRequest)
				utils.Encode(w, r, http.StatusBadRequest, problems)
				return
			}

			if err = userService.CreateUser(r.Context(), user.Username, user.Password); err != nil {
				log.Println(err)
				w.WriteHeader(http.StatusConflict)
				return
			}

			w.WriteHeader(http.StatusCreated)
		},
	)
}
