package services

import (
	"context"
	"fmt"
	"ncloud-api/pkg/repositories"
)

type UserService struct {
	repository *repositories.UserRepository
}

// NewUserService creates and returns UserService with db
func NewUserService(repository *repositories.UserRepository) *UserService {
	return &UserService{
		repository: repository,
	}
}

// CreateUser inserts new user to database with username and encoded password
func (s *UserService) CreateUser(ctx context.Context, username, password string) error {
	if err := s.repository.CreateUser(ctx, username, password); err != nil {
		return fmt.Errorf("create user: %w", err)
	}

	return nil
}
