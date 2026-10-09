package migrate

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
)

type stored struct{ name, checksum string }
type testState struct {
	exists, locked                               bool
	history                                      map[int64]stored
	events                                       []string
	failScript, failCommit, failLock, failUnlock error
	closes                                       int
}
type fakeDriver struct{ s *testState }

func (d fakeDriver) Open(string) (driver.Conn, error) { return &fakeConnection{s: d.s}, nil }

type fakeConnection struct {
	s  *testState
	tx *fakeTransaction
}

func (c *fakeConnection) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unsupported") }
func (c *fakeConnection) Close() error                        { c.s.closes++; c.s.locked = false; return nil }
func (c *fakeConnection) Begin() (driver.Tx, error) {
	c.s.events = append(c.s.events, "begin")
	copy := map[int64]stored{}
	for v, r := range c.s.history {
		copy[v] = r
	}
	c.tx = &fakeTransaction{conn: c, history: copy}
	return c.tx, nil
}
func (c *fakeConnection) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch {
	case strings.Contains(q, "pg_advisory_unlock"):
		c.s.events = append(c.s.events, "unlock")
		if c.s.failUnlock != nil {
			return nil, c.s.failUnlock
		}
		c.s.locked = false
	case strings.Contains(q, "pg_advisory_lock"):
		c.s.events = append(c.s.events, "lock")
		c.s.locked = true
		if c.s.failLock != nil {
			return nil, c.s.failLock
		}
	case strings.HasPrefix(q, "CREATE TABLE public.pfw_schema_migrations"):
		c.s.exists = true
	case strings.HasPrefix(q, "INSERT INTO public.pfw_schema_migrations"):
		if c.tx == nil {
			return nil, errors.New("history write outside transaction")
		}
		c.tx.history[args[0].Value.(int64)] = stored{args[1].Value.(string), args[2].Value.(string)}
	case strings.HasPrefix(q, "DELETE FROM public.pfw_schema_migrations"):
		if c.tx == nil {
			return nil, errors.New("history delete outside transaction")
		}
		delete(c.tx.history, args[0].Value.(int64))
	default:
		if !c.s.locked || c.tx == nil {
			return nil, errors.New("script without locked transaction")
		}
		c.s.events = append(c.s.events, q)
		if q == "FAIL" {
			return nil, c.s.failScript
		}
	}
	return driver.RowsAffected(1), nil
}
func (c *fakeConnection) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(q, "to_regclass") {
		return &fakeRows{columns: []string{"exists"}, rows: [][]driver.Value{{c.s.exists}}}, nil
	}
	r := &fakeRows{columns: []string{"version", "name", "checksum"}}
	versions := []int64{}
	for v := range c.s.history {
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })
	for _, v := range versions {
		item := c.s.history[v]
		r.rows = append(r.rows, []driver.Value{v, item.name, item.checksum})
	}
	return r, nil
}

type fakeTransaction struct {
	conn    *fakeConnection
	history map[int64]stored
}

func (t *fakeTransaction) Commit() error {
	t.conn.s.events = append(t.conn.s.events, "commit")
	t.conn.tx = nil
	if t.conn.s.failCommit != nil {
		return t.conn.s.failCommit
	}
	t.conn.s.history = t.history
	return nil
}
func (t *fakeTransaction) Rollback() error {
	t.conn.s.events = append(t.conn.s.events, "rollback")
	t.conn.tx = nil
	return nil
}

type fakeRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
}

func (r *fakeRows) Columns() []string { return r.columns }
func (r *fakeRows) Close() error      { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.index])
	r.index++
	return nil
}

var driverCounter atomic.Uint64

func testDatabase(t *testing.T) (*sql.DB, *testState) {
	t.Helper()
	s := &testState{history: map[int64]stored{}}
	name := fmt.Sprintf("migrate-test-%d", driverCounter.Add(1))
	sql.Register(name, fakeDriver{s})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db, s
}
func testFiles() []Migration {
	return []Migration{{Version: 1, Name: "first", Up: "UP1", Down: "DOWN1", Checksum: "a"}, {Version: 2, Name: "second", Up: "UP2", Down: "DOWN2", Checksum: "b"}}
}

func TestUpStatusDownAndIdempotency(t *testing.T) {
	db, s := testDatabase(t)
	ctx := context.Background()
	files := testFiles()
	states, err := Run(ctx, db, files, "status", 0)
	if err != nil || s.exists || len(states) != 2 || states[0].Applied {
		t.Fatalf("status: %+v %v", states, err)
	}
	states, err = Run(ctx, db, files, "up", 0)
	if err != nil || !states[1].Applied || len(s.history) != 2 || s.locked {
		t.Fatalf("up: %+v %v", states, err)
	}
	before := len(s.events)
	if _, err := Run(ctx, db, files, "up", 0); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(s.events[before:], ","), "begin") {
		t.Fatal("repeated up executed a migration")
	}
	states, err = Run(ctx, db, files, "down", 1)
	if err != nil || states[1].Applied || !states[0].Applied || len(s.history) != 1 {
		t.Fatalf("down: %+v %v", states, err)
	}
	if _, err := Run(ctx, db, files, "down", 2); err == nil || len(s.history) != 1 {
		t.Fatal("excessive rollback changed history")
	}
	if _, err := Run(ctx, db, files, "up", 1); err != nil || len(s.history) != 2 {
		t.Fatalf("reapply: %v", err)
	}
}

func TestFailureRollsBackOnlyCurrentMigration(t *testing.T) {
	db, s := testDatabase(t)
	cause := errors.New("secret SQL")
	s.failScript = cause
	files := testFiles()
	files[1].Up = "FAIL"
	_, err := Run(context.Background(), db, files, "up", 0)
	if !errors.Is(err, cause) || strings.Contains(err.Error(), "secret SQL") || len(s.history) != 1 || s.locked {
		t.Fatalf("failure: history=%v err=%v", s.history, err)
	}
	if !strings.Contains(strings.Join(s.events, ","), "FAIL,rollback,unlock") {
		t.Fatalf("rollback/lock lifecycle: %v", s.events)
	}
}

func TestHistoryDriftIsRejected(t *testing.T) {
	for _, history := range []map[int64]stored{{1: {"first", "changed"}}, {3: {"missing", "c"}}, {2: {"second", "b"}}} {
		db, s := testDatabase(t)
		s.exists = true
		s.history = history
		if _, err := Run(context.Background(), db, testFiles(), "up", 0); err == nil || s.locked {
			t.Fatalf("drift accepted: %v", history)
		}
		if strings.Contains(strings.Join(s.events, ","), "begin") {
			t.Fatal("drift executed SQL")
		}
	}
}

func TestUncertainCommitAndLocksDiscardSessions(t *testing.T) {
	db, s := testDatabase(t)
	cause := errors.New("commit unavailable")
	s.failCommit = cause
	if _, err := Run(context.Background(), db, testFiles(), "up", 0); !errors.Is(err, cause) || len(s.history) != 0 || s.locked {
		t.Fatalf("commit failure: %v", err)
	}
	for _, acquire := range []bool{true, false} {
		db, s := testDatabase(t)
		cause := errors.New("lock connection lost")
		if acquire {
			s.failLock = cause
		} else {
			s.failUnlock = cause
		}
		if _, err := Run(context.Background(), db, testFiles(), "status", 0); !errors.Is(err, cause) || s.locked || s.closes != 1 {
			t.Fatalf("discard uncertain lock: closes=%d locked=%v err=%v", s.closes, s.locked, err)
		}
	}
}

func TestCancelledRunDoesNotAcquireResources(t *testing.T) {
	db, s := testDatabase(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, db, testFiles(), "up", 0); !errors.Is(err, context.Canceled) || s.exists || len(s.events) != 0 {
		t.Fatalf("cancelled run: %v %v", s.events, err)
	}
}

func TestDownOnFreshDatabaseDoesNotCreateHistory(t *testing.T) {
	db, s := testDatabase(t)
	if _, err := Run(context.Background(), db, testFiles(), "down", 1); err == nil || s.exists || s.locked {
		t.Fatalf("fresh rollback: exists=%v locked=%v err=%v", s.exists, s.locked, err)
	}
}
