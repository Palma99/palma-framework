package main

import (
	"errors"
	"fmt"
	"log"
)

//go:generate go run github.com/palma99/palma-framework/cmd/pfw generate

// Database is a simulated resource; this example needs no external database.
type Database struct{ closed bool }

func OpenDatabase() (*Database, func() error, error) {
	db := &Database{}
	log.Print("database opened")
	return db, func() error {
		db.closed = true
		log.Print("database closed")
		return nil
	}, nil
}

type Service struct{ db *Database }

func NewService(db *Database) *Service { return &Service{db: db} }

func (s *Service) Run() error {
	if s.db.closed {
		return fmt.Errorf("database is closed")
	}
	log.Print("service using database")
	return nil
}

func run() (err error) {
	service, cleanup, err := Initialize()
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, cleanup()) }()
	return service.Run()
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
