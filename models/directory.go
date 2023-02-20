package models

import (
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Directory struct {
	Id                      string             `json:"id"`
	Name                    string             `json:"name"`
	ParentDirectory         string             `json:"parent_directory"`
	ParentDirectoryObjectId primitive.ObjectID `json:"parentDirectoryObjectId"`
	User                    string             `json:"user"`
}

func (d *Directory) ToBSON() bson.D {
	return bson.D{
		{"name", d.Name},
		{"parent_directory", d.ParentDirectoryObjectId},
		{"user", d.User},
	}
}
