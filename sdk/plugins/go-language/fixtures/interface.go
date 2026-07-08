package service

import "context"

// UserRepository defines persistence operations for users.
type UserRepository interface {
	FindByID(ctx context.Context, id string) (*User, error)
	Create(ctx context.Context, req CreateUserRequest) (*User, error)
	Update(ctx context.Context, user *User) error
	Delete(ctx context.Context, id string) error
}

// Cache defines a generic caching interface.
type Cache interface {
	Get(key string) ([]byte, bool)
	Set(key string, value []byte) error
	Delete(key string) error
}

// Middleware is a function that wraps an HTTP handler.
type Middleware func(Handler) Handler

// Handler is a function that handles HTTP requests.
type Handler func(ctx context.Context, req Request) Response

// Request represents an incoming HTTP request.
type Request struct {
	Method  string
	Path    string
	Headers map[string]string
	Body    []byte
}

// Response represents an HTTP response.
type Response struct {
	StatusCode int
	Body       []byte
}
