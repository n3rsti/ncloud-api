package repositories

import (
	"context"
	"fmt"
	"ncloud-api/pkg/crypto"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// UserRepository handles the operations related to user data in the MongoDB database.
type UserRepository struct {
	db *mongo.Database
}

// NewUserRepository creates a new instance of UserRepository with the provided MongoDB database.
// It returns a pointer to the newly created UserRepository.
func NewUserRepository(db *mongo.Database) *UserRepository {
	return &UserRepository{
		db: db,
	}
}

// CreateUser inserts a new user document into the MongoDB "user" collection.
// It takes a context, username, password.
// The password is hashed before being stored. The username is used as the document's _id.
// It returns an error if there is any issue during the password hashing or document insertion.
func (r UserRepository) CreateUser(ctx context.Context, username, password string) error {
	// Get the "user" collection from the database
	collection := r.db.Collection("user")

	// Generate a hash for the password
	encodedPassword, err := crypto.GenerateHash(password)
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}

	// Create a document with the username as the _id and the hashed password
	document := bson.M{
		"_id":      username,
		"password": encodedPassword,
	}

	// Insert the document into the collection
	if _, err := collection.InsertOne(ctx, document); err != nil {
		return fmt.Errorf("create user: %w", err)
	}

	return nil
}
