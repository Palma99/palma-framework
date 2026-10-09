package migrate

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"
)

const lockKey int64 = 5784384101434173255
const table = "public.pfw_schema_migrations"

type State struct {
	Version int64
	Name    string
	Applied bool
}

// Run serializes migration operations on a dedicated PostgreSQL session.
// Up steps=0 applies all pending files; down steps must be positive.
// Each file and its history record commit together; earlier successful files
// remain committed if a later file fails. Status never creates the history table.
func Run(ctx context.Context, db *sql.DB, files []Migration, action string, steps int) (states []State, err error) {
	if ctx == nil || db == nil {
		return nil, errors.New("migrations: context and database are required")
	}
	if action != "up" && action != "down" && action != "status" {
		return nil, fmt.Errorf("migrations: unknown action %q", action)
	}
	if steps < 0 || (action == "down" && steps == 0) {
		return nil, errors.New("migrations: down requires positive steps; up allows zero for all")
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		closeErr := conn.Close()
		if !errors.Is(closeErr, sql.ErrConnDone) {
			err = errors.Join(err, closeErr)
		}
	}()
	// If acquisition fails, the server may have acquired the session lock just
	// before cancellation reached the client. Discard that session unconditionally.
	if _, err = conn.ExecContext(ctx, "SELECT pg_catalog.pg_advisory_lock($1)", lockKey); err != nil {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		return nil, fmt.Errorf("migrations: acquire lock: %w", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, unlockErr := conn.ExecContext(cleanup, "SELECT pg_catalog.pg_advisory_unlock($1)", lockKey)
		if unlockErr != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			err = errors.Join(err, fmt.Errorf("migrations: release lock: %w", unlockErr))
		}
	}()
	var exists bool
	if err = conn.QueryRowContext(ctx, "SELECT pg_catalog.to_regclass('public.pfw_schema_migrations') IS NOT NULL").Scan(&exists); err != nil {
		return nil, err
	}
	if !exists && action == "up" {
		_, err = conn.ExecContext(ctx, "CREATE TABLE "+table+" (version BIGINT PRIMARY KEY, name TEXT NOT NULL, checksum TEXT NOT NULL, applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP)")
		if err != nil {
			return nil, fmt.Errorf("migrations: create history: %w", err)
		}
		exists = true
	}
	type record struct{ name, checksum string }
	history := map[int64]record{}
	if exists {
		rows, queryErr := conn.QueryContext(ctx, "SELECT version, name, checksum FROM "+table+" ORDER BY version")
		if queryErr != nil {
			return nil, queryErr
		}
		for rows.Next() {
			var version int64
			var r record
			if scanErr := rows.Scan(&version, &r.name, &r.checksum); scanErr != nil {
				_ = rows.Close()
				return nil, scanErr
			}
			history[version] = r
		}
		err = errors.Join(rows.Err(), rows.Close())
		if err != nil {
			return nil, err
		}
	}
	known := map[int64]bool{}
	var applied int
	pending := false
	for i, m := range files {
		if m.Version <= 0 || (i > 0 && files[i-1].Version >= m.Version) || m.Checksum == "" {
			return nil, errors.New("migrations: files must have unique ascending positive versions and checksums")
		}
		known[m.Version] = true
		r, ok := history[m.Version]
		if ok {
			if pending {
				return nil, fmt.Errorf("migrations: out-of-order history at version %d", m.Version)
			}
			if r.name != m.Name || r.checksum != m.Checksum {
				return nil, fmt.Errorf("migrations: applied version %d was modified", m.Version)
			}
			applied++
		} else {
			pending = true
		}
	}
	for version := range history {
		if !known[version] {
			return nil, fmt.Errorf("migrations: applied version %d is missing from disk", version)
		}
	}
	if action == "up" {
		end := len(files)
		if steps > 0 && steps < end-applied {
			end = applied + steps
		}
		for applied < end {
			if err = execute(ctx, conn, files[applied], false); err != nil {
				return nil, err
			}
			applied++
		}
	} else if action == "down" {
		if steps > applied {
			return nil, fmt.Errorf("migrations: requested %d rollbacks but only %d versions are applied", steps, applied)
		}
		for range steps {
			if err = execute(ctx, conn, files[applied-1], true); err != nil {
				return nil, err
			}
			applied--
		}
	}
	for i, m := range files {
		states = append(states, State{Version: m.Version, Name: m.Name, Applied: i < applied})
	}
	return states, nil
}

func execute(ctx context.Context, conn *sql.Conn, m Migration, down bool) (err error) {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return migrationError(m, err)
	}
	defer func() {
		rollbackErr := tx.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, migrationError(m, rollbackErr))
		}
	}()
	script := m.Up
	if down {
		script = m.Down
	}
	if _, err = tx.ExecContext(ctx, script); err != nil {
		return migrationError(m, err)
	}
	if down {
		_, err = tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE version = $1", m.Version)
	} else {
		_, err = tx.ExecContext(ctx, "INSERT INTO "+table+" (version, name, checksum) VALUES ($1, $2, $3)", m.Version, m.Name, m.Checksum)
	}
	if err != nil {
		return migrationError(m, err)
	}
	if err = ctx.Err(); err != nil {
		return migrationError(m, err)
	}
	if err = tx.Commit(); err != nil {
		return migrationError(m, err)
	}
	return nil
}

// Keep causes discoverable without printing SQL source or driver-supplied secrets.
type executionError struct {
	version int64
	cause   error
}

func (e *executionError) Error() string {
	return fmt.Sprintf("migrations: version %d failed", e.version)
}
func (e *executionError) Unwrap() error { return e.cause }
func migrationError(m Migration, err error) error {
	return &executionError{version: m.Version, cause: err}
}
