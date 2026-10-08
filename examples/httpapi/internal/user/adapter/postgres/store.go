package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/palma99/palma-framework/examples/httpapi/internal/config"
	"github.com/palma99/palma-framework/examples/httpapi/internal/user/domain"
	sqltx "github.com/palma99/palma-framework/transaction/sql"
)

type connectionError struct {
	message string
	cause   error
}

func (e *connectionError) Error() string { return e.message }
func (e *connectionError) Unwrap() error { return e.cause }

//pfw:coconut
func OpenDatabase(ctx context.Context, cfg config.Config) (*sql.DB, func() error, error) {
	db, err := sql.Open("pgx", cfg.DB.DSN)
	if err != nil {
		return nil, nil, &connectionError{message: "invalid PostgreSQL configuration", cause: err}
	}
	startup, cancel := context.WithTimeout(ctx, cfg.DB.ConnectTimeout)
	defer cancel()
	if err := db.PingContext(startup); err != nil {
		return nil, db.Close, &connectionError{message: "cannot connect to PostgreSQL", cause: err}
	}
	return db, db.Close, nil
}

type Store struct{ db *sql.DB }

//pfw:coconut
func NewPostgresStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) List(ctx context.Context) ([]domain.User, error) {
	db, err := sqltx.Executor(ctx, s.db)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, "SELECT id::text, name FROM pfw_users ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []domain.User{}
	for rows.Next() {
		var user domain.User
		if err := rows.Scan(&user.ID, &user.Name); err != nil {
			return nil, err
		}
		result = append(result, user)
	}
	return result, rows.Err()
}
func (s *Store) Get(ctx context.Context, id string) (domain.User, error) {
	var user domain.User
	key, err := strconv.ParseInt(id, 10, 64)
	if err != nil || key <= 0 {
		return user, domain.ErrNotFound
	}
	db, err := sqltx.Executor(ctx, s.db)
	if err != nil {
		return user, err
	}
	err = db.QueryRowContext(ctx, "SELECT id::text, name FROM pfw_users WHERE id = $1", key).Scan(&user.ID, &user.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	return user, err
}
func (s *Store) Create(ctx context.Context, user domain.User) (domain.User, error) {
	var created domain.User
	db, err := sqltx.Executor(ctx, s.db)
	if err != nil {
		return created, err
	}
	err = db.QueryRowContext(ctx, "INSERT INTO pfw_users (name) VALUES ($1) RETURNING id::text, name", user.Name).Scan(&created.ID, &created.Name)
	return created, err
}
