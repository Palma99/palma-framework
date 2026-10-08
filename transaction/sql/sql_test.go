package sql_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"sync"
	"testing"

	"github.com/palma99/palma-framework/transaction"
	sqltx "github.com/palma99/palma-framework/transaction/sql"
)

type state struct {
	mu                                sync.Mutex
	value, begins, commits, rollbacks int
	options                           driver.TxOptions
	commitErr                         error
}
type connector struct{ state *state }

func (c connector) Connect(context.Context) (driver.Conn, error) {
	return &connection{state: c.state}, nil
}
func (c connector) Driver() driver.Driver { return testDriver{c.state} }

type testDriver struct{ state *state }

func (d testDriver) Open(string) (driver.Conn, error) { return &connection{state: d.state}, nil }

type connection struct {
	state   *state
	active  bool
	pending int
}

func (c *connection) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not implemented") }
func (c *connection) Close() error                        { return nil }
func (c *connection) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *connection) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	c.state.begins++
	c.state.options = options
	c.active = true
	c.pending = 0
	return c, nil
}
func (c *connection) ExecContext(ctx context.Context, _ string, args []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	if c.active {
		c.pending += int(args[0].Value.(int64))
	} else {
		c.state.value += int(args[0].Value.(int64))
	}
	return driver.RowsAffected(1), nil
}
func (c *connection) Commit() error {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	c.state.commits++
	c.active = false
	if c.state.commitErr != nil {
		return c.state.commitErr
	}
	c.state.value += c.pending
	return nil
}
func (c *connection) Rollback() error {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	c.state.rollbacks++
	c.active = false
	c.pending = 0
	return nil
}
func database(t *testing.T) (*sql.DB, *state) {
	t.Helper()
	s := &state{}
	db := sql.OpenDB(connector{s})
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db, s
}
func write(ctx context.Context, db *sql.DB, n int) error {
	executor, err := sqltx.Executor(ctx, db)
	if err != nil {
		return err
	}
	_, err = executor.ExecContext(ctx, "write", n)
	return err
}

func TestOperationsShareTransactionAndOutsideQueriesUsePool(t *testing.T) {
	db, s := database(t)
	m, err := sqltx.New(db, sqltx.Options{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	var retained context.Context
	err = m.Within(context.Background(), func(ctx context.Context) error {
		retained = ctx
		first, err := sqltx.Executor(ctx, db)
		if err != nil {
			return err
		}
		if err := write(ctx, db, 2); err != nil {
			return err
		}
		return m.Within(ctx, func(ctx context.Context) error {
			second, err := sqltx.Executor(ctx, db)
			if err != nil {
				return err
			}
			if first != second {
				t.Fatal("different executors")
			}
			return write(ctx, db, 3)
		})
	})
	if err != nil || s.value != 5 || s.begins != 1 || s.commits != 1 || s.rollbacks != 0 {
		t.Fatalf("outcome: %+v %v", s, err)
	}
	if s.options.Isolation != driver.IsolationLevel(sql.LevelSerializable) || s.options.ReadOnly {
		t.Fatalf("options: %+v", s.options)
	}
	if _, err := sqltx.Executor(retained, db); !errors.Is(err, transaction.ErrClosed) {
		t.Fatalf("expired scope: %v", err)
	}
	if err := write(context.Background(), db, 4); err != nil || s.value != 9 {
		t.Fatalf("outside transaction: %v", err)
	}
}

func TestRollbackAndFailedCommitDoNotReportSuccess(t *testing.T) {
	for _, commitFail := range []bool{false, true} {
		t.Run(map[bool]string{false: "callback error", true: "commit error"}[commitFail], func(t *testing.T) {
			db, s := database(t)
			m, err := sqltx.New(db, sqltx.Options{})
			if err != nil {
				t.Fatal(err)
			}
			cause := errors.New("failure")
			if commitFail {
				s.commitErr = cause
			}
			err = m.Within(context.Background(), func(ctx context.Context) error {
				if err := write(ctx, db, 2); err != nil {
					return err
				}
				if err := write(ctx, db, 3); err != nil {
					return err
				}
				if !commitFail {
					return cause
				}
				return nil
			})
			if !errors.Is(err, cause) || s.value != 0 {
				t.Fatalf("failed transaction: %+v %v", s, err)
			}
			if !commitFail && s.rollbacks != 1 {
				t.Fatal("no rollback")
			}
		})
	}
}

func TestDifferentDatabaseIsRejected(t *testing.T) {
	db, s := database(t)
	other, otherState := database(t)
	m, err := sqltx.New(db, sqltx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	err = m.Within(context.Background(), func(ctx context.Context) error { return write(ctx, other, 1) })
	if !errors.Is(err, sqltx.ErrDifferentDatabase) || s.rollbacks != 1 || otherState.value != 0 {
		t.Fatalf("foreign database: %v", err)
	}
}
