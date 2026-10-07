package domain

import (
	"errors"
	"strings"
	"unicode/utf8"
)

type User struct {
	ID   string
	Name string
}

var ErrNotFound = errors.New("user not found")

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string { return e.Field + ": " + e.Message }

// New validates and normalizes a user's name independently of transport/storage.
func New(name string) (User, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return User{}, &ValidationError{Field: "name", Message: "name is required"}
	}
	if utf8.RuneCountInString(name) > 100 {
		return User{}, &ValidationError{Field: "name", Message: "name must contain at most 100 characters"}
	}
	return User{Name: name}, nil
}
