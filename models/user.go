package models

import (
	"go.mongodb.org/mongo-driver/bson"
)

type User struct {
	Username string `json:"username" validate:"min=1"`
	Password string `json:"password,omitempty" validate:"min=5"`
}

func (u *User) ToBSON() bson.D {
	return bson.D{
		{"username", u.Username},
		{"password", u.Password},
	}
}
