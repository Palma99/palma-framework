// Package sql adapts application transactions to database/sql. Import as sqltx.
package sql

import (
	"context"
	stdsql "database/sql"
	"errors"
	"time"

	"github.com/palma99/palma-framework/transaction"
)

var ErrDifferentDatabase = errors.New("pfw: operation uses a different transaction database")

// DBTX is the query surface shared by *sql.DB and *sql.Tx.
type DBTX interface {
	ExecContext(context.Context, string, ...any) (stdsql.Result, error)
	QueryContext(context.Context, string, ...any) (*stdsql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *stdsql.Row
}

type Options struct {
	Isolation       stdsql.IsolationLevel
	ReadOnly        bool
	RollbackTimeout time.Duration
}

type backend struct {
	db      *stdsql.DB
	options stdsql.TxOptions
}

// New creates a manager to share across all use cases for this database.
func New(db *stdsql.DB, options Options) (*transaction.Manager, error) {
	if db == nil {
		return nil, errors.New("pfw: transaction database is required")
	}
	return transaction.New(
		&backend{db: db, options: stdsql.TxOptions{Isolation: options.Isolation, ReadOnly: options.ReadOnly}},
		transaction.Options{RollbackTimeout: options.RollbackTimeout},
	)
}

func (b *backend) Begin(ctx context.Context) (transaction.Transaction, error) {
	tx, err := b.db.BeginTx(ctx, &b.options)
	if err != nil {
		return nil, err
	}
	return &sqlTransaction{Tx: tx, db: b.db}, nil
}

type sqlTransaction struct {
	*stdsql.Tx
	db *stdsql.DB
}

func (t *sqlTransaction) Commit(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return t.Tx.Commit()
}
func (t *sqlTransaction) Rollback(context.Context) error {
	err := t.Tx.Rollback()
	if errors.Is(err, stdsql.ErrTxDone) {
		return nil
	}
	return err
}

// Executor returns the active transaction for db, or db outside a scope.
// It never silently switches databases or falls back from an expired scope.
// Pass ctx to the returned executor's context-aware query methods as well.
func Executor(ctx context.Context, db *stdsql.DB) (DBTX, error) {
	if ctx == nil || db == nil {
		return nil, errors.New("pfw: query context and database are required")
	}
	resource, err := transaction.Current(ctx)
	if errors.Is(err, transaction.ErrNoTransaction) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return db, nil
	}
	if err != nil {
		return nil, err
	}
	tx, ok := resource.(*sqlTransaction)
	if !ok || tx.db != db {
		return nil, ErrDifferentDatabase
	}
	return tx.Tx, nil
}
