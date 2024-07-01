package models

import "fmt"

const (
	MIN_USERNAME = 1
	MAX_USERNAME = 25

	MIN_PASSWORD = 1
)

type User struct {
	Username string `json:"username" bson:"_id"`
	Password string `json:"password"`
}

func (u User) Valid() map[string]string {
	problems := make(map[string]string)

	if len(u.Username) < MIN_USERNAME {
		problems["username"] = fmt.Sprintf("must be longer than %d character", MIN_USERNAME)
	} else if len(u.Username) > MAX_USERNAME {
		problems["username"] = fmt.Sprintf("must be shorter than %d characters", MIN_USERNAME)
	}

	if len(u.Password) < MIN_PASSWORD {
		problems["password"] = fmt.Sprintf("must be longer than %d characters", MIN_PASSWORD)
	}

	return problems
}
