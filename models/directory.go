package models

import (
	"github.com/go-playground/validator/v10"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Directory struct {
	Id                      string             `json:"id"`
	Name                    string             `json:"name" validate:"max=100"`
	ParentDirectory         primitive.ObjectID `json:"parent_directory"`
	PreviousParentDirectory primitive.ObjectID `json:"previous_parent_directory,omitempty"`
	User                    string             `json:"user"`
	AccessKey               string             `json:"access_key"`
}

func (d *Directory) ToBSON() bson.D {
	return bson.D{
		{Key: "name", Value: d.Name},
		{Key: "parent_directory", Value: d.ParentDirectory},
		{Key: "user", Value: d.User},
	}
}

func (d *Directory) ToBsonNotEmpty() bson.D {
	var data bson.D

	if d.Name != "" {
		data = append(data, bson.E{Key: "name", Value: d.Name})
	}
	if !d.ParentDirectory.IsZero() {
		data = append(data, bson.E{Key: "parent_directory", Value: d.ParentDirectory})
	}
	if !d.PreviousParentDirectory.IsZero() {
		data = append(data, bson.E{Key: "previous_parent_directory", Value: d.PreviousParentDirectory})
	}

	return data
}
func (d *Directory) Validate() error {
	validate := validator.New()
	if err := validate.Struct(d); err != nil {
		return err
	}
	return nil
}
