package models

import "go.mongodb.org/mongo-driver/bson"

type File struct {
	Id              string `json:"id"`
	Name            string `json:"name"`
	ParentDirectory string `json:"parent_directory,omitempty"`
	User            string `json:"user"`
}

func (f *File) toBSON() bson.D {
	return bson.D{
		{"id", f.Id},
		{"name", f.Name},
		{"parent_directory", f.ParentDirectory},
	}
}
