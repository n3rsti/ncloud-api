package config

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const DbName = "ncloud-api"

type Config struct {
	Db *mongo.Client
}

func LoadConfig() *Config {
	if err := godotenv.Load(); err != nil {
		log.Fatal("no .env found")
	}

	db, err := loadDb()
	if err != nil {
		log.Fatal(err)
	}

	return &Config{
		Db: db,
	}
}

func loadDb() (*mongo.Client, error) {
	user := os.Getenv("DB_USER")
	password := os.Getenv("DB_PASSWORD")
	host := os.Getenv("DB_HOST")

	uri := fmt.Sprintf("mongodb://%s:%s@%s", user, password, host)
	if uri == "" {
		return nil, fmt.Errorf("empty db uri")
	}

	client, err := mongo.Connect(context.TODO(), options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}

	fmt.Println(
		client.Ping(context.TODO(), nil),
	)

	return client, nil
}
