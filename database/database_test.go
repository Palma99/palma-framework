package database_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/palma99/palma-framework/database"
	"github.com/palma99/palma-framework/transaction"
	sqltx "github.com/palma99/palma-framework/transaction/sql"
)

type recorder struct {
	mu     sync.Mutex
	events []string
}

func (r *recorder) add(event string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

type endpointState struct {
	name     string
	pingErr  error
	closeErr error
	wait     bool
	recorder *recorder
}

type testDriver struct{ states map[string]*endpointState }

func (d testDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }
func (d testDriver) OpenConnector(dsn string) (driver.Connector, error) {
	s, ok := d.states[dsn]
	if !ok {
		return nil, errors.New("invalid DSN: " + dsn)
	}
	return connector{state: s, driver: d}, nil
}

type connector struct {
	state  *endpointState
	driver testDriver
}

func (c connector) Driver() driver.Driver { return c.driver }
func (c connector) Connect(ctx context.Context) (driver.Conn, error) {
	if c.state.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	c.state.recorder.add("open:" + c.state.name)
	return conn{c.state}, nil
}

type conn struct{ state *endpointState }

func (c conn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unsupported") }
func (c conn) Begin() (driver.Tx, error) {
	c.state.recorder.add("begin:" + c.state.name)
	return tx{c.state}, nil
}
func (c conn) Ping(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.state.pingErr
}
func (c conn) Close() error {
	c.state.recorder.add("close:" + c.state.name)
	return c.state.closeErr
}

type tx struct{ state *endpointState }

func (t tx) Commit() error   { t.state.recorder.add("commit:" + t.state.name); return nil }
func (t tx) Rollback() error { t.state.recorder.add("rollback:" + t.state.name); return nil }

var driverID atomic.Uint64

func configuration(t *testing.T, states ...*endpointState) database.Config {
	t.Helper()
	name := fmt.Sprintf("palma-database-test-%s-%d", t.Name(), driverID.Add(1))
	d := testDriver{states: make(map[string]*endpointState)}
	for _, s := range states {
		d.states[s.name] = s
	}
	sql.Register(name, d)
	cfg := database.Config{Primary: database.Endpoint{Driver: name, DSN: states[0].name}, Replicas: map[string]database.Endpoint{}}
	for _, s := range states[1:] {
		cfg.Replicas[s.name] = database.Endpoint{Driver: name, DSN: s.name}
	}
	return cfg
}

func TestPoolsSelectionAndConcurrentCleanup(t *testing.T) {
	r := &recorder{}
	cfg := configuration(t,
		&endpointState{name: "primary", recorder: r},
		&endpointState{name: "z", recorder: r},
		&endpointState{name: "a", recorder: r},
	)
	cfg.Primary.Pool = &database.PoolConfig{MaxOpenConns: 4, MaxIdleConns: 2, ConnMaxLifetime: time.Hour, ConnMaxIdleTime: time.Minute}
	db, cleanup, err := database.Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cleanup() })
	if db.Primary().Stats().MaxOpenConnections != 4 || db.Primary().Stats().Idle != 1 {
		t.Fatalf("pool configuration: %+v", db.Primary().Stats())
	}
	a, err := db.Replica("a")
	if err != nil || a == db.Primary() {
		t.Fatalf("replica selection: %p %v", a, err)
	}
	again, _ := db.Replica("a")
	if a != again {
		t.Fatal("selection must share the same pool")
	}
	if missing, err := db.Replica("missing"); missing != nil || !errors.Is(err, database.ErrReplicaNotFound) {
		t.Fatalf("missing replica: %v %v", missing, err)
	}
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			if err := cleanup(); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	want := []string{"open:primary", "open:a", "open:z", "close:z", "close:a", "close:primary"}
	if !reflect.DeepEqual(r.events, want) {
		t.Fatalf("resource order: %v", r.events)
	}
	if err := a.Ping(); err == nil {
		t.Fatal("cleanup must close replica pools")
	}
}

func TestPartialFailureClosesEveryPoolAndPreservesCauses(t *testing.T) {
	r := &recorder{}
	pingErr, closeErr := errors.New("secret DSN"), errors.New("close failed")
	cfg := configuration(t,
		&endpointState{name: "primary", recorder: r, closeErr: closeErr},
		&endpointState{name: "a", recorder: r},
		&endpointState{name: "b", recorder: r, pingErr: pingErr},
	)
	db, cleanup, err := database.Open(context.Background(), cfg)
	if db != nil || cleanup != nil || !errors.Is(err, pingErr) || !errors.Is(err, closeErr) {
		t.Fatalf("partial failure: %v %v", db, err)
	}
	if strings.Contains(err.Error(), "secret DSN") {
		t.Fatal("driver message leaked into connection error")
	}
	want := []string{"open:primary", "open:a", "open:b", "close:b", "close:a", "close:primary"}
	if !reflect.DeepEqual(r.events, want) {
		t.Fatalf("rollback order: %v", r.events)
	}
}

func TestCleanupAggregatesErrorsAndRunsOnce(t *testing.T) {
	r := &recorder{}
	first, second := errors.New("primary close"), errors.New("replica close")
	cfg := configuration(t, &endpointState{name: "primary", recorder: r, closeErr: first}, &endpointState{name: "report", recorder: r, closeErr: second})
	_, cleanup, err := database.Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	err = cleanup()
	if !errors.Is(err, first) || !errors.Is(err, second) || cleanup() != err {
		t.Fatalf("cleanup errors: %v", err)
	}
	if len(r.events) != 4 {
		t.Fatalf("cleanup executed more than once: %v", r.events)
	}
}

func TestValidationBeforeOpeningAndSensitiveOpenError(t *testing.T) {
	for _, mutate := range []func(*database.Config){
		func(c *database.Config) { c.Primary.Driver = "" },
		func(c *database.Config) { c.Primary.ConnectTimeout = -1 },
		func(c *database.Config) { c.Replicas[" "] = c.Primary },
		func(c *database.Config) { c.Replicas["bad"] = database.Endpoint{} },
		func(c *database.Config) { c.Primary.Pool = &database.PoolConfig{MaxOpenConns: -1} },
		func(c *database.Config) { c.Primary.Pool = &database.PoolConfig{MaxIdleConns: -1} },
		func(c *database.Config) { c.Primary.Pool = &database.PoolConfig{ConnMaxLifetime: -1} },
		func(c *database.Config) { c.Primary.Pool = &database.PoolConfig{ConnMaxIdleTime: -1} },
		func(c *database.Config) { c.Primary.Pool = &database.PoolConfig{MaxOpenConns: 1, MaxIdleConns: 2} },
	} {
		r := &recorder{}
		cfg := configuration(t, &endpointState{name: "primary", recorder: r})
		mutate(&cfg)
		if db, cleanup, err := database.Open(context.Background(), cfg); err == nil || db != nil || cleanup != nil {
			t.Fatalf("invalid configuration accepted: %+v", cfg)
		}
		if len(r.events) != 0 {
			t.Fatalf("validation opened pools: %v", r.events)
		}
	}
	r := &recorder{}
	cfg := configuration(t, &endpointState{name: "primary", recorder: r})
	cfg.Primary.DSN = "password=secret"
	_, _, err := database.Open(context.Background(), cfg)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("open error exposes DSN: %v", err)
	}
}

func TestContextAndConnectTimeout(t *testing.T) {
	r := &recorder{}
	cfg := configuration(t, &endpointState{name: "primary", recorder: r, wait: true})
	cfg.Primary.ConnectTimeout = 10 * time.Millisecond
	if _, _, err := database.Open(context.Background(), cfg); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("connection timeout: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := database.Open(ctx, cfg); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled startup: %v", err)
	}
	if _, _, err := database.Open(nil, cfg); err == nil {
		t.Fatal("nil context accepted")
	}
}

func TestExplicitSelectionWithTransactionsAndIndependentDatabases(t *testing.T) {
	r := &recorder{}
	cfg := configuration(t, &endpointState{name: "primary", recorder: r}, &endpointState{name: "report", recorder: r})
	db, cleanup, err := database.Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cleanup() })
	other, closeOther, err := database.Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closeOther() })
	replica, _ := db.Replica("report")
	if executor, err := sqltx.Executor(context.Background(), replica); err != nil || executor != replica {
		t.Fatalf("outside transaction: %v", err)
	}
	manager, err := sqltx.New(db.Primary(), sqltx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var expired context.Context
	err = manager.Within(context.Background(), func(ctx context.Context) error {
		expired = ctx
		first, err := sqltx.Executor(ctx, db.Primary())
		if err != nil {
			return err
		}
		if _, ok := first.(*sql.Tx); !ok {
			t.Fatal("primary did not participate in transaction")
		}
		second, err := sqltx.Executor(ctx, db.Primary())
		if err != nil || first != second {
			t.Fatal("repositories must share the transaction")
		}
		for _, pool := range []*sql.DB{replica, other.Primary()} {
			if _, err := sqltx.Executor(ctx, pool); !errors.Is(err, sqltx.ErrDifferentDatabase) {
				t.Fatalf("cross-pool transaction accepted: %v", err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sqltx.Executor(expired, db.Primary()); !errors.Is(err, transaction.ErrClosed) {
		t.Fatalf("expired transaction: %v", err)
	}
}
