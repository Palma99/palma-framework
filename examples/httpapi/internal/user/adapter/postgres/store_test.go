package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/palma99/palma-framework/examples/httpapi/internal/user/domain"
	sqltx "github.com/palma99/palma-framework/transaction/sql"
)

// A database/sql test driver verifies query parameters and row handling without
// opening a real database or mutating an external service.
type testConnector struct{ state *queryState }
type queryState struct {
	name                       string
	query                      string
	args                       []driver.NamedValue
	begins, commits, rollbacks int
}

func (c testConnector) Connect(context.Context) (driver.Conn, error) {
	return testConnection{state: c.state}, nil
}
func (c testConnector) Driver() driver.Driver { return testDriver{state: c.state} }

type testDriver struct{ state *queryState }

func (d testDriver) Open(string) (driver.Conn, error) { return testConnection{state: d.state}, nil }

type testConnection struct{ state *queryState }

func (c testConnection) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not supported") }
func (c testConnection) Close() error                        { return nil }
func (c testConnection) Begin() (driver.Tx, error) {
	c.state.begins++
	return &testTransaction{state: c.state, previousName: c.state.name}, nil
}

type testTransaction struct {
	state        *queryState
	previousName string
}

func (tx *testTransaction) Commit() error { tx.state.commits++; return nil }
func (tx *testTransaction) Rollback() error {
	tx.state.rollbacks++
	tx.state.name = tx.previousName
	return nil
}
func (c testConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.state.query = query
	c.state.args = args
	rows := &testRows{}
	switch {
	case strings.HasPrefix(query, "INSERT"):
		c.state.name = args[0].Value.(string)
		rows.values = [][]driver.Value{{"2", c.state.name}}
	case strings.Contains(query, "WHERE"):
		if args[0].Value.(int64) == 2 {
			rows.values = [][]driver.Value{{"2", c.state.name}}
		}
	default:
		rows.values = [][]driver.Value{{"2", c.state.name}}
	}
	return rows, nil
}

type testRows struct {
	values [][]driver.Value
	index  int
}

func (r *testRows) Columns() []string { return []string{"id", "name"} }
func (r *testRows) Close() error      { return nil }
func (r *testRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}

func TestPostgresQueryMappingAndParameters(t *testing.T) {
	state := &queryState{}
	db := sql.OpenDB(testConnector{state: state})
	defer db.Close()
	store := NewPostgresStore(db)
	name := "Grace'; SELECT 1; --"
	created, err := store.Create(context.Background(), domain.User{Name: name})
	if err != nil || created.ID != "2" || created.Name != name || strings.Contains(state.query, name) || state.args[0].Value != name {
		t.Fatalf("create: %+v %v", created, err)
	}
	found, err := store.Get(context.Background(), "2")
	if err != nil || found != created {
		t.Fatalf("get: %+v %v", found, err)
	}
	for _, id := range []string{"999", "invalid", "-1"} {
		if _, err := store.Get(context.Background(), id); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("missing %s: %v", id, err)
		}
	}
	listed, err := store.List(context.Background())
	if err != nil || len(listed) != 1 || listed[0] != created {
		t.Fatalf("list: %+v %v", listed, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.List(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled query: %v", err)
	}
}

func TestStoresParticipateInSharedApplicationTransaction(t *testing.T) {
	state := &queryState{name: "original"}
	db := sql.OpenDB(testConnector{state: state})
	defer db.Close()
	first, second := NewPostgresStore(db), NewPostgresStore(db)
	manager, err := sqltx.New(db, sqltx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	cause := errors.New("application operation failed")
	err = manager.Within(context.Background(), func(ctx context.Context) error {
		if _, err := first.Create(ctx, domain.User{Name: "First"}); err != nil {
			return err
		}
		if _, err := second.Create(ctx, domain.User{Name: "Second"}); err != nil {
			return err
		}
		return cause
	})
	if !errors.Is(err, cause) || state.name != "original" || state.begins != 1 || state.rollbacks != 1 || state.commits != 0 {
		t.Fatalf("shared rollback: %+v %v", state, err)
	}
	err = manager.Within(context.Background(), func(ctx context.Context) error {
		if _, err := first.Create(ctx, domain.User{Name: "First"}); err != nil {
			return err
		}
		_, err := second.Create(ctx, domain.User{Name: "Second"})
		return err
	})
	if err != nil || state.name != "Second" || state.commits != 1 || state.begins != 2 {
		t.Fatalf("shared commit: %+v %v", state, err)
	}
}
