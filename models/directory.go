package models

import (
	"context"

	"github.com/go-playground/validator/v10"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"ncloud-api/utils/helper"
)

type Directory struct {
	Id                      string `json:"id"`
	Name                    string `json:"name"                                validate:"max=100"`
	ParentDirectory         string `json:"parent_directory"`
	PreviousParentDirectory string `json:"previous_parent_directory,omitempty"`
	User                    string `json:"user"`
	AccessKey               string `json:"access_key"`
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
	if d.ParentDirectory != "" {
		data = append(data, bson.E{Key: "parent_directory", Value: d.ParentDirectory})
	}
	if d.PreviousParentDirectory != "" {
		data = append(
			data,
			bson.E{Key: "previous_parent_directory", Value: d.PreviousParentDirectory},
		)
	}
	if d.Id != "" {
		data = append(data, bson.E{Key: "_id", Value: d.Id})
	}
	if d.User != "" {
		data = append(data, bson.E{Key: "user", Value: d.User})
	}
	if d.AccessKey != "" {
		data = append(data, bson.E{Key: "access_key", Value: d.AccessKey})
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

func FindDirectoriesById[T interface{}](
	db *mongo.Database,
	idList []primitive.ObjectID,
	opts ...*options.FindOptions,
) ([]T, error) {
	filter := bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: idList}}}}

	return FindDirectoriesByFilter[T](db, filter, opts...)
}

func FindDirectoriesByFilter[T interface{}](
	db *mongo.Database,
	filter interface{},
	opts ...*options.FindOptions,
) ([]T, error) {
	cursor, err := db.Collection("directories").Find(context.TODO(), filter, opts...)
	if err != nil {
		return nil, err
	}

	return helper.MapCursorToObject[T](cursor)
}

func DirectoriesToBsonNotEmpty(directories []Directory) []interface{} {
	result := make([]interface{}, 0, len(directories))
	for _, directory := range directories {
		result = append(result, directory.ToBsonNotEmpty())
	}

	return result
}
