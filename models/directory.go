package models

import "go.mongodb.org/mongo-driver/bson"

type Directory struct {
	Id              string `json:"id"`
	Name            string `json:"name"`
	ParentDirectory string `json:"parent_directory"`
	User            string `json:"user"`
}

func (d *Directory) ToBSON() bson.D {
	return bson.D{
		{"name", d.Name},
		{"parent_directory_id", d.ParentDirectory},
		{"user", d.User},
	}
}
