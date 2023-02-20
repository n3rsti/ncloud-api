package models

import (
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type File struct {
	Id              string `json:"id"`
	Name            string `json:"name"`
	ParentDirectory primitive.ObjectID `json:"parent_directory,omitempty"`
	User            string `json:"user"`
	Type            string `json:"type"`
	Size            int64  `json:"size"`
}

func (f *File) ToBSON() bson.D {
	return bson.D{
		{"name", f.Name},
		{"user", f.User},
		{"parent_directory", f.ParentDirectory},
		{"type", f.Type},
		{"size", f.Size},
	}
}
