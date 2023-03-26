package models

import (
	"go.mongodb.org/mongo-driver/bson"
)

type User struct {
	Username         string `json:"username" validate:"min=1"`
	Password         string `json:"password,omitempty" validate:"min=5"`
	MainDirAccessKey string `json:"main_dir_access_key,omitempty"`
	TrashAccessKey   string `json:"trash_access_key,omitempty"`
}

func (u *User) ToBSON() bson.D {
	return bson.D{
		{"username", u.Username},
		{"password", u.Password},
		{"main_dir_access_key", u.MainDirAccessKey},
		{"trash_access_key", u.TrashAccessKey},
	}
}
