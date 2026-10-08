// Package database owns SQL pools and exposes explicit primary/replica selection.
// It does not execute queries or route operations or transactions.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrReplicaNotFound indicates that the requested replica is not configured.
var ErrReplicaNotFound = errors.New("pfw: database replica not configured")

// PoolConfig uses database/sql semantics: zero MaxOpenConns means unlimited,
// zero MaxIdleConns disables idle connections, and zero durations disable expiry.
// A nil Endpoint.Pool preserves database/sql defaults.
type PoolConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// Endpoint describes one physical SQL pool. The application registers its driver.
// ConnectTimeout defaults to five seconds. DSN follows the driver's format.
type Endpoint struct {
	Driver         string
	DSN            string
	ConnectTimeout time.Duration
	Pool           *PoolConfig
}

// Config describes one logical database. Replica names are application-defined.
type Config struct {
	Primary  Endpoint
	Replicas map[string]Endpoint
}

// Connection is an immutable group of pools shared by an application's consumers.
// Obtain one through Open; its cleanup owns all pools. Consumers must not close
// the pools returned by Primary or Replica themselves.
type Connection struct {
	primary  *sql.DB
	replicas map[string]*sql.DB
}

// Primary returns the primary pool without consulting request or transaction state.
func (c *Connection) Primary() *sql.DB { return c.primary }

// Replica returns exactly the named pool, with no implicit primary fallback.
func (c *Connection) Replica(name string) (*sql.DB, error) {
	db, ok := c.replicas[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrReplicaNotFound, name)
	}
	return db, nil
}

// endpointError keeps driver errors available to errors.Is/As without displaying
// potentially sensitive driver messages (such as connection strings).
type endpointError struct {
	name, operation string
	cause           error
}

func (e *endpointError) Error() string {
	return fmt.Sprintf("pfw: database %s: %s failed", e.name, e.operation)
}
func (e *endpointError) Unwrap() error { return e.cause }

// Open validates every endpoint, configures and pings each pool, and returns a
// cleanup suitable for Palma's BuildWithCleanup. Initialization is all-or-nothing:
// a failure closes all acquired pools and returns nil connection and cleanup.
// Cleanup closes pools in reverse order once, aggregates errors, and is safe for
// concurrent calls. Invoke it only after all consumers have stopped.
func Open(ctx context.Context, cfg Config) (*Connection, func() error, error) {
	if ctx == nil {
		return nil, nil, errors.New("pfw: database context is required")
	}
	names := make([]string, 0, len(cfg.Replicas))
	for name := range cfg.Replicas {
		names = append(names, name)
	}
	sort.Strings(names)
	if err := validate("primary", cfg.Primary); err != nil {
		return nil, nil, err
	}
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			return nil, nil, errors.New("pfw: database replica name must not be blank")
		}
		if err := validate(fmt.Sprintf("replica %q", name), cfg.Replicas[name]); err != nil {
			return nil, nil, err
		}
	}
	var pools []*sql.DB
	var once sync.Once
	var cleanupErr error
	cleanup := func() error {
		once.Do(func() {
			for i := len(pools) - 1; i >= 0; i-- {
				cleanupErr = errors.Join(cleanupErr, pools[i].Close())
			}
		})
		return cleanupErr
	}
	open := func(name string, endpoint Endpoint) (*sql.DB, error) {
		if err := ctx.Err(); err != nil {
			return nil, &endpointError{name, "connect", err}
		}
		db, err := sql.Open(endpoint.Driver, endpoint.DSN)
		if err != nil {
			return nil, &endpointError{name, "open", err}
		}
		pools = append(pools, db)
		if p := endpoint.Pool; p != nil {
			db.SetMaxOpenConns(p.MaxOpenConns)
			db.SetMaxIdleConns(p.MaxIdleConns)
			db.SetConnMaxLifetime(p.ConnMaxLifetime)
			db.SetConnMaxIdleTime(p.ConnMaxIdleTime)
		}
		timeout := endpoint.ConnectTimeout
		if timeout == 0 {
			timeout = 5 * time.Second
		}
		startup, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		if err := db.PingContext(startup); err != nil {
			return nil, &endpointError{name, "connect", err}
		}
		return db, nil
	}
	primary, err := open("primary", cfg.Primary)
	if err != nil {
		return nil, nil, errors.Join(err, cleanup())
	}
	connection := &Connection{primary: primary, replicas: make(map[string]*sql.DB, len(names))}
	for _, name := range names {
		db, err := open(fmt.Sprintf("replica %q", name), cfg.Replicas[name])
		if err != nil {
			return nil, nil, errors.Join(err, cleanup())
		}
		connection.replicas[name] = db
	}
	return connection, cleanup, nil
}

func validate(name string, endpoint Endpoint) error {
	invalid := func(message string) error {
		return fmt.Errorf("pfw: database %s: %s", name, message)
	}
	if strings.TrimSpace(endpoint.Driver) == "" {
		return invalid("driver is required")
	}
	if endpoint.ConnectTimeout < 0 {
		return invalid("connect timeout must not be negative")
	}
	if p := endpoint.Pool; p != nil {
		if p.MaxOpenConns < 0 || p.MaxIdleConns < 0 || p.ConnMaxLifetime < 0 || p.ConnMaxIdleTime < 0 {
			return invalid("pool limits and durations must not be negative")
		}
		if p.MaxOpenConns > 0 && p.MaxIdleConns > p.MaxOpenConns {
			return invalid("max idle connections must not exceed max open connections")
		}
	}
	return nil
}
