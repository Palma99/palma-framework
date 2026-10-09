package main

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrSessionNotFound  = errors.New("session not found")
	ErrUserNotFound     = errors.New("user not found")
	ErrDocumentNotFound = errors.New("document not found")
)

type Session struct {
	UserID    string
	ExpiresAt time.Time
	Revoked   bool
}
type User struct {
	ID, OrganizationID string
	Roles              []string
	Disabled           bool
}
type Document struct{ ID, OwnerID, OrganizationID, Title string }
type SessionRepository interface {
	Find(context.Context, string) (Session, error)
}
type UserRepository interface {
	GetUser(context.Context, string) (User, error)
}
type DocumentRepository interface {
	GetDocument(context.Context, string) (Document, error)
	Save(context.Context, Document) error
}

// Store simulates the application's repositories. Session IDs below are public
// demo fixtures, not secrets suitable for use in a deployed application.
type Store struct {
	mu        sync.RWMutex
	sessions  map[string]Session
	users     map[string]User
	documents map[string]Document
}

func NewStore() *Store {
	expires := time.Now().Add(time.Hour)
	return &Store{
		sessions: map[string]Session{
			"demo-alice":    {UserID: "alice", ExpiresAt: expires},
			"demo-bob":      {UserID: "bob", ExpiresAt: expires},
			"demo-admin":    {UserID: "admin", ExpiresAt: expires},
			"demo-disabled": {UserID: "disabled", ExpiresAt: expires},
			"demo-expired":  {UserID: "alice", ExpiresAt: time.Now().Add(-time.Hour)},
			"demo-revoked":  {UserID: "alice", ExpiresAt: expires, Revoked: true},
			"demo-deleted":  {UserID: "deleted", ExpiresAt: expires},
		},
		users: map[string]User{
			"alice":    {ID: "alice", OrganizationID: "one", Roles: []string{"editor"}},
			"bob":      {ID: "bob", OrganizationID: "one", Roles: []string{"editor"}},
			"admin":    {ID: "admin", OrganizationID: "one", Roles: []string{"admin"}},
			"disabled": {ID: "disabled", OrganizationID: "one", Disabled: true},
		},
		documents: map[string]Document{
			"doc-1": {ID: "doc-1", OwnerID: "alice", OrganizationID: "one", Title: "Alice's document"},
			"doc-2": {ID: "doc-2", OwnerID: "bob", OrganizationID: "one", Title: "Bob's document"},
			"doc-3": {ID: "doc-3", OwnerID: "carol", OrganizationID: "two", Title: "Other organization"},
		},
	}
}

func (s *Store) Find(ctx context.Context, id string) (Session, error) {
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.sessions[id]
	if !ok {
		return Session{}, ErrSessionNotFound
	}
	return value, nil
}
func (s *Store) GetUser(ctx context.Context, id string) (User, error) {
	if err := ctx.Err(); err != nil {
		return User{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.users[id]
	if !ok {
		return User{}, ErrUserNotFound
	}
	value.Roles = append([]string(nil), value.Roles...)
	return value, nil
}
func (s *Store) GetDocument(ctx context.Context, id string) (Document, error) {
	if err := ctx.Err(); err != nil {
		return Document{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.documents[id]
	if !ok {
		return Document{}, ErrDocumentNotFound
	}
	return value, nil
}
func (s *Store) Save(ctx context.Context, document Document) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.documents[document.ID]; !ok {
		return ErrDocumentNotFound
	}
	s.documents[document.ID] = document
	return nil
}
