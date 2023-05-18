package models

import (
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Directory struct {
	Id                      string             `json:"id"`
	Name                    string             `json:"name"`
	ParentDirectory         primitive.ObjectID `json:"parent_directory"`
	PreviousParentDirectory primitive.ObjectID `json:"previous_parent_directory,omitempty"`
	User                    string             `json:"user"`
	AccessKey               string             `json:"access_key"`
}

func (d *Directory) ToBSON() bson.D {
	return bson.D{
		{"name", d.Name},
		{"parent_directory", d.ParentDirectory},
		{"user", d.User},
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
