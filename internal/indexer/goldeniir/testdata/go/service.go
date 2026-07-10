package service

import "fmt"

// FindUser looks up a user by id, returning an error when missing.
func FindUser(id string, limit int) (*User, error) {
	if id == "" {
		return nil, fmt.Errorf("empty id")
	}
	return &User{}, nil
}

func recordEvent(name string) {
	fmt.Println(name)
}

type User struct{ Name string }
