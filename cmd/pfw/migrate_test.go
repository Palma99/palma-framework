package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationCLIHelpCreateAndEnvironment(t *testing.T) {
	root := runFixture(t, "package main\nfunc main(){}\n")
	if err := os.WriteFile(filepath.Join(root, "pfw.toml"), []byte("env_dir='./env'\nmigrations_dir='./db/migrations'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "env"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "env/.env.staging"), []byte("APP_DB_DRIVER=mysql\nAPP_DB_DSN=fake\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PFW_ENV_DIR", "")
	t.Setenv("APP_DB_DRIVER", "mysql")
	t.Setenv("APP_DB_DSN", "")
	t.Chdir(root)
	var out bytes.Buffer
	if err := run([]string{"migrate", "--help"}, &out, &out); err != nil || !strings.Contains(out.String(), "PostgreSQL") {
		t.Fatalf("help: %s %v", out.String(), err)
	}
	out.Reset()
	if err := run([]string{"migrate", "create", "first"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "db/migrations/000001_first.sql")
	if err := os.WriteFile(file, []byte("-- +pfw Up\nSELECT 1;\n-- +pfw Down\nSELECT 1;\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := run([]string{"migrate", "up", "-env", "staging"}, &out, &out); err == nil || !strings.Contains(err.Error(), "APP_DB_DSN") {
		t.Fatalf("process override must win: %v", err)
	}
	if err := os.Unsetenv("APP_DB_DSN"); err != nil {
		t.Fatal(err)
	}
	if err := os.Unsetenv("APP_DB_DRIVER"); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"migrate", "up", "-env", "staging"}, &out, &out); err == nil || !strings.Contains(err.Error(), "PostgreSQL") {
		t.Fatalf("environment directory not used: %v", err)
	}
}

func TestMigrationCLIRejectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{{"migrate"}, {"migrate", "unknown"}, {"migrate", "create", "../escape"}, {"migrate", "create"}, {"migrate", "down", "-steps", "0"}, {"migrate", "up", "-steps", "-1"}, {"migrate", "status", "-steps", "1"}, {"migrate", "up", "unexpected"}, {"migrate", "up", "-timeout", "0s"}} {
		var out bytes.Buffer
		if err := run(args, &out, &out); err == nil {
			t.Fatalf("invalid command accepted: %v", args)
		}
	}
}
