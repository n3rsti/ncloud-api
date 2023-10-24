package models

import (
	"context"

	"github.com/go-playground/validator/v10"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"ncloud-api/utils/helper"
)

type File struct {
	Id                      string `json:"id"                                  bson:"_id"`
	Name                    string `json:"name"                                                                 validate:"max=260"`
	ParentDirectory         string `json:"parent_directory,omitempty"          bson:"parent_directory"`
	PreviousParentDirectory string `json:"previous_parent_directory,omitempty" bson:"previous_parent_directory"`
	User                    string `json:"user,omitempty"`
	Type                    string `json:"type"`
	Size                    int64  `json:"size"`
	Created                 int64  `json:"created"`
}

func (f *File) ToBSON() bson.D {
	return bson.D{
		{Key: "name", Value: f.Name},
		{Key: "user", Value: f.User},
		{Key: "parent_directory", Value: f.ParentDirectory},
		{Key: "type", Value: f.Type},
		{Key: "size", Value: f.Size},
	}
}

// ToBSONnotEmpty
//
// Convert File struct to BSON ignoring empty fields
func (f *File) ToBSONnotEmpty() bson.D {
	var data bson.D

	if f.Id != "" {
		data = append(data, bson.E{Key: "_id", Value: f.Id})
	}
	if f.Name != "" {
		data = append(data, bson.E{Key: "name", Value: f.Name})
	}
	if f.User != "" {
		data = append(data, bson.E{Key: "user", Value: f.User})
	}
	if f.ParentDirectory != "" {
		data = append(data, bson.E{Key: "parent_directory", Value: f.ParentDirectory})
	}
	if f.PreviousParentDirectory != "" {
		data = append(
			data,
			bson.E{Key: "previous_parent_directory", Value: f.PreviousParentDirectory},
		)
	}
	if f.Type != "" {
		data = append(data, bson.E{Key: "type", Value: f.Type})
	}
	if f.Size != 0 {
		data = append(data, bson.E{Key: "size", Value: f.Size})
	}
	if f.Created != 0 {
		data = append(data, bson.E{Key: "created", Value: f.Created})
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

func FindFilesByFilter[T interface{}](
	db *mongo.Database,
	filter interface{},
	opts ...*options.FindOptions,
) ([]T, error) {
	cursor, err := db.Collection("files").Find(context.TODO(), filter, opts...)
	if err != nil {
		return nil, err
	}

	return helper.MapCursorToObject[T](cursor)
}

func FindFilesById[T interface{}](
	db *mongo.Database,
	idList []string,
	opts ...*options.FindOptions,
) ([]T, error) {
	filter := bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: idList}}}}

	return FindFilesByFilter[T](db, filter, opts...)
}

func FilesToBsonNotEmpty(files []File) []interface{} {
	result := make([]interface{}, 0, len(files))
	for _, file := range files {
		result = append(result, file.ToBSONnotEmpty())
	}

	return result
}

func FilesToMap(files []File) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(files))
	for _, file := range files {
		result = append(result, map[string]interface{}{
			"_id":              file.Id,
			"name":             file.Name,
			"parent_directory": file.ParentDirectory,
			"type":             file.Type,
			"user":             file.User,
		})
	}

	return result

}
