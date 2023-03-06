package models

import (
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type File struct {
	Id              string             `json:"id"`
	Name            string             `json:"name"`
	ParentDirectory primitive.ObjectID `json:"parent_directory,omitempty"`
	User            string             `json:"user"`
	Type            string             `json:"type"`
	Size            int64              `json:"size"`
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

// ToBSONnotEmpty
//
// Convert File struct to BSON ignoring empty fields
func (f *File) ToBSONnotEmpty() bson.D {
	var data bson.D

	if f.Name != ""{
		data = append(data, bson.E{Key: "name", Value: f.Name})
	}
	if f.User != "" {
		data = append(data, bson.E{Key: "user", Value: f.User})
	}
	if !f.ParentDirectory.IsZero() {
		data = append(data, bson.E{Key: "parent_directory", Value: f.ParentDirectory})
	}
	if f.Type != "" {
		data = append(data, bson.E{Key: "type", Value: f.Type})
	}
	if f.Size != 0 {
		data = append(data, bson.E{Key: "size", Value: f.Size})
	}

	return data
}
