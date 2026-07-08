package service

import (
	"context"
	"fmt"
)

// UserService handles user business logic.
type UserService struct {
	repo UserRepository
}

// NewUserService creates a new UserService.
func NewUserService(repo UserRepository) *UserService {
	return &UserService{repo: repo}
}

// GetUser retrieves a user by ID.
func (s *UserService) GetUser(ctx context.Context, id string) (*User, error) {
	user, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get user %s: %w", id, err)
	}
	return user, nil
}

// CreateUser creates a new user.
func (s *UserService) CreateUser(ctx context.Context, req CreateUserRequest) (*User, error) {
	if req.Email == "" {
		return nil, fmt.Errorf("email is required")
	}
	return s.repo.Create(ctx, req)
}

// User represents a user entity.
type User struct {
	ID    string
	Email string
	Name  string
}

// CreateUserRequest is the input for creating a user.
type CreateUserRequest struct {
	Email string
	Name  string
}
