// Package transaction defines an application transaction boundary independent
// of a storage driver. Pass the callback context to every participating operation.
package transaction

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrNoTransaction    = errors.New("pfw: no transaction in context")
	ErrClosed           = errors.New("pfw: transaction scope is closed")
	ErrRollbackOnly     = errors.New("pfw: transaction marked for rollback")
	ErrDifferentManager = errors.New("pfw: context belongs to another transaction manager")
)

// Runner is the application-facing contract. Nested calls on the same manager
// join the existing transaction, rather than creating savepoints.
type Runner interface {
	Within(context.Context, func(context.Context) error) error
}

// Transaction is implemented by storage adapters. Commit must honor the caller
// context. Rollback receives a separate bounded context for cleanup.
type Transaction interface {
	Commit(context.Context) error
	Rollback(context.Context) error
}

type Beginner interface {
	Begin(context.Context) (Transaction, error)
}

type Options struct{ RollbackTimeout time.Duration }

type Manager struct {
	backend         Beginner
	rollbackTimeout time.Duration
}

func New(backend Beginner, options Options) (*Manager, error) {
	if backend == nil {
		return nil, errors.New("pfw: transaction backend is required")
	}
	timeout := options.RollbackTimeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	if timeout < 0 {
		return nil, errors.New("pfw: rollback timeout must be positive")
	}
	return &Manager{backend: backend, rollbackTimeout: timeout}, nil
}

type scopeKey struct{}
type scope struct {
	manager *Manager
	tx      Transaction
	mu      sync.Mutex
	closed  bool
	failure error
}

func contextError(ctx context.Context) error {
	err := ctx.Err()
	if err != nil {
		if cause := context.Cause(ctx); cause != nil && cause != err {
			return errors.Join(err, cause)
		}
	}
	return err
}

// Current is intended for storage adapters. A context retained after Within
// returns must fail, rather than silently running work outside the transaction.
func Current(ctx context.Context) (Transaction, error) {
	if ctx == nil {
		return nil, errors.New("pfw: transaction context is required")
	}
	s, _ := ctx.Value(scopeKey{}).(*scope)
	if s == nil {
		return nil, ErrNoTransaction
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrClosed
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	return s.tx, nil
}

func (s *scope) fail(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failure = errors.Join(s.failure, err)
}

// Within owns begin/commit/rollback at the outermost call. Inner failures mark
// the shared scope rollback-only, even if their caller handles the returned error.
// Panics roll back and propagate unchanged. Work must finish before fn returns;
// do not retain the context or launch unjoined goroutines from the callback.
func (m *Manager) Within(ctx context.Context, fn func(context.Context) error) (err error) {
	if ctx == nil || fn == nil {
		return errors.New("pfw: transaction context and callback are required")
	}
	if s, _ := ctx.Value(scopeKey{}).(*scope); s != nil {
		s.mu.Lock()
		closed := s.closed
		s.mu.Unlock()
		if closed {
			return ErrClosed
		}
		if s.manager != m {
			s.fail(ErrDifferentManager)
			return ErrDifferentManager
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				s.fail(ErrRollbackOnly)
				panic(recovered)
			}
			s.fail(err)
		}()
		if err = contextError(ctx); err != nil {
			return err
		}
		err = fn(ctx)
		if err == nil {
			err = contextError(ctx)
		}
		return err
	}
	if err = contextError(ctx); err != nil {
		return err
	}
	txctx, cancel := context.WithCancel(ctx)
	defer cancel()
	tx, err := m.backend.Begin(txctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	if tx == nil {
		return errors.New("pfw: transaction backend returned nil")
	}
	s := &scope{manager: m, tx: tx}
	txctx = context.WithValue(txctx, scopeKey{}, s)
	committed := false
	defer func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		if !committed {
			cleanup, stop := context.WithTimeout(context.WithoutCancel(txctx), m.rollbackTimeout)
			defer stop()
			if failure := tx.Rollback(cleanup); failure != nil {
				err = errors.Join(err, fmt.Errorf("rollback transaction: %w", failure))
			}
		}
	}()
	if err = contextError(txctx); err != nil {
		return err
	}
	err = fn(txctx)
	if err == nil {
		err = contextError(txctx)
	}
	s.mu.Lock()
	failure := s.failure
	s.closed = true
	s.mu.Unlock()
	if failure != nil {
		err = errors.Join(err, ErrRollbackOnly, failure)
	}
	if err != nil {
		return err
	}
	if err = tx.Commit(txctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	committed = true
	return nil
}

// Do returns a value only after a successful boundary. On any failure the result
// is its zero value, including when the callback succeeded but commit failed.
// In a nested call the value is provisional until the outer boundary commits.
func Do[T any](ctx context.Context, runner Runner, fn func(context.Context) (T, error)) (T, error) {
	var result T
	if runner == nil || fn == nil {
		return result, errors.New("pfw: transaction runner and callback are required")
	}
	err := runner.Within(ctx, func(ctx context.Context) error {
		var err error
		result, err = fn(ctx)
		return err
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return result, nil
}
