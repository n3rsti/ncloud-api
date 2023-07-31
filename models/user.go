package models

import (
	"go.mongodb.org/mongo-driver/bson"
)

type User struct {
	Id             string `json:"id"                         bson:"_id"`
	Username       string `json:"username"                              validate:"min=1"`
	Password       string `json:"password,omitempty"                    validate:"min=5"`
	TrashAccessKey string `json:"trash_access_key,omitempty"`
}

func (u *User) ToBSON() bson.D {
	return bson.D{
		{Key: "username", Value: u.Username},
		{Key: "password", Value: u.Password},
		{Key: "trash_access_key", Value: u.TrashAccessKey},
		{Key: "_id", Value: u.Id},
	}
}
