package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	pfw "github.com/palma99/palma-framework"
	"github.com/palma99/palma-framework/config"
	"github.com/palma99/palma-framework/database"
	"github.com/palma99/palma-framework/internal/migrate"
)

var migrateCommand = command{
	usage:       "migrate <create|up|down|status> [options] [name]",
	description: "Create and manage versioned PostgreSQL migrations",
	examples:    []string{"go tool pfw migrate create add_users", "go tool pfw migrate up -env staging", "go tool pfw migrate status -env prod", "go tool pfw migrate down -env staging -steps 1"},
	notes:       []string{"SQL files use <version>_<name>.sql with -- +pfw Up and -- +pfw Down sections. Each migration runs in a transaction on the primary database.", "Directory defaults to pfw.toml migrations_dir or ./migrations. Configuration uses the same environment directory as run.", "Down defaults to one version. Up applies all pending versions unless -steps is set. Migration actions require a primary PostgreSQL DSN.", "Migration files cannot contain transaction control or statements that require execution outside a transaction."},
	run:         runMigrate,
}

func runMigrate(ctx context.Context, args []string, stdout, stderr io.Writer) (err error) {
	action := ""
	if len(args) > 0 && args[0] != "--help" && args[0] != "-h" && args[0] != "-help" {
		action, args = args[0], args[1:]
	}
	flags := flag.NewFlagSet("pfw migrate", flag.ContinueOnError)
	envFlag := flags.String("env", "", "environment `name` (or PFW_ENV)")
	envDir := flags.String("env-dir", "", "dotenv directory `path` (same precedence as run)")
	dirFlag := flags.String("dir", "", "migration directory `path` (overrides pfw.toml)")
	dsnKey := flags.String("dsn-env", "APP_DB_DSN", "environment `key` containing the primary PostgreSQL DSN")
	steps := flags.Int("steps", 0, "maximum versions to apply; down defaults to 1")
	timeout := flags.Duration("timeout", 5*time.Minute, "total operation `duration`, including lock acquisition")
	if err := parseCommandFlags("migrate", flags, args, stdout); err != nil {
		return err
	}
	if action != "create" && action != "up" && action != "down" && action != "status" {
		return fmt.Errorf("usage: pfw migrate <create|up|down|status> [options]")
	}
	if *steps < 0 || *timeout <= 0 || strings.TrimSpace(*dsnKey) == "" {
		return errors.New("migrate: steps must not be negative, timeout must be positive and dsn-env must not be blank")
	}
	explicitSteps, explicitEnv := false, false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "steps" {
			explicitSteps = true
		}
		if f.Name == "env" {
			explicitEnv = true
		}
	})
	if (action == "create" || action == "status") && explicitSteps {
		return errors.New("migrate: -steps is only valid for up/down")
	}
	if action == "down" {
		if !explicitSteps {
			*steps = 1
		}
		if *steps == 0 {
			return errors.New("migrate: down requires positive steps")
		}
	}
	if (action == "create" && flags.NArg() != 1) || (action != "create" && flags.NArg() != 0) {
		return errors.New("migrate: create requires one name; other actions accept no positional arguments")
	}
	root, _, err := projectPatterns("bootstrap", nil)
	if err != nil {
		return err
	}
	settings, err := readProjectSettings(root)
	if err != nil {
		return err
	}
	directory := *dirFlag
	if directory == "" {
		directory = filepath.Join(root, "migrations")
		if settings.MigrationsDir != nil {
			directory = *settings.MigrationsDir
			if !filepath.IsAbs(directory) {
				directory = filepath.Join(root, directory)
			}
		}
	}
	directory, err = filepath.Abs(directory)
	if err != nil {
		return err
	}
	if action == "create" {
		path, err := migrate.Create(directory, flags.Arg(0))
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, path)
		return err
	}
	files, err := migrate.Load(directory)
	if err != nil {
		return err
	}
	env := pfw.Environment(*envFlag)
	if !explicitEnv {
		env, err = pfw.EnvironmentFromEnv("")
		if err != nil {
			return fmt.Errorf("migrate: select an environment with -env or PFW_ENV: %w", err)
		}
	}
	if err := env.Validate(); err != nil {
		return err
	}
	configuration, err := projectEnvironmentDirectory(root, *envDir)
	if err != nil {
		return err
	}
	values, err := config.EnvironmentValues(env, configuration)
	if err != nil {
		return err
	}
	lookup := func(key string) string {
		if value, ok := os.LookupEnv(key); ok {
			return value
		}
		return values[key]
	}
	dsn := lookup(*dsnKey)
	if strings.TrimSpace(dsn) == "" {
		return fmt.Errorf("migrate: %s is required in environment %q", *dsnKey, env)
	}
	if selected := lookup("APP_DB_DRIVER"); selected != "" && selected != "pgx" {
		return errors.New("migrate: this backend supports PostgreSQL via pgx only")
	}
	connectTimeout := 5 * time.Second
	if value := lookup("APP_DB_CONNECT_TIMEOUT"); value != "" {
		connectTimeout, err = time.ParseDuration(value)
		if err != nil || connectTimeout <= 0 {
			return errors.New("migrate: invalid APP_DB_CONNECT_TIMEOUT")
		}
	}
	signals, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	operation, cancel := context.WithTimeout(signals, *timeout)
	defer cancel()
	connection, cleanup, err := database.Open(operation, database.Config{Primary: database.Endpoint{Driver: "pgx", DSN: dsn, ConnectTimeout: connectTimeout, Pool: &database.PoolConfig{MaxOpenConns: 1, MaxIdleConns: 1}}})
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, cleanup()) }()
	states, err := migrate.Run(operation, connection.Primary(), files, action, *steps)
	if err != nil {
		return err
	}
	for _, state := range states {
		status := "pending"
		if state.Applied {
			status = "applied"
		}
		if _, err := fmt.Fprintf(stdout, "%06d %-8s %s\n", state.Version, status, state.Name); err != nil {
			return err
		}
	}
	return nil
}
