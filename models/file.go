package models

import (
	"github.com/go-playground/validator/v10"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type File struct {
	Id                      string             `json:"id"`
	Name                    string             `json:"name" validate:"max=260"`
	ParentDirectory         primitive.ObjectID `json:"parent_directory,omitempty"`
	PreviousParentDirectory primitive.ObjectID `json:"previous_parent_directory,omitempty"`
	User                    string             `json:"user"`
	Type                    string             `json:"type"`
	Size                    int64              `json:"size"`
	AccessKey               string             `json:"access_key"`
}

func (f *File) ToBSON() bson.D {
	return bson.D{
		{Key: "name", Value: f.Name},
		{Key: "user", Value: f.User},
		{Key: "parent_directory", Value: f.ParentDirectory},
		{Key: "type", Value: f.Type},
		{Key: "size", Value: f.Size},
		{Key: "access_key", Value: f.AccessKey},
	}
}

// ToBSONnotEmpty
//
// Convert File struct to BSON ignoring empty fields
func (f *File) ToBSONnotEmpty() bson.D {
	var data bson.D

	if f.Name != "" {
		data = append(data, bson.E{Key: "name", Value: f.Name})
	}
	if f.User != "" {
		data = append(data, bson.E{Key: "user", Value: f.User})
	}
	if !f.ParentDirectory.IsZero() {
		data = append(data, bson.E{Key: "parent_directory", Value: f.ParentDirectory})
	}
	if !f.PreviousParentDirectory.IsZero() {
		data = append(data, bson.E{Key: "previous_parent_directory", Value: f.PreviousParentDirectory})
	}
	if f.Type != "" {
		data = append(data, bson.E{Key: "type", Value: f.Type})
	}
	if f.Size != 0 {
		data = append(data, bson.E{Key: "size", Value: f.Size})
	}
	if f.AccessKey != "" {
		data = append(data, bson.E{Key: "access_key", Value: f.AccessKey})
	}

	return data
}
func (f *File) Validate() error {
	validate := validator.New()
	if err := validate.Struct(f); err != nil {
		return err
	}
	return nil
}
