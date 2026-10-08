package config_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pfwconfig "github.com/palma99/palma-framework/config"
)

func envFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDotenvNestedConfigurationAndPrecedence(t *testing.T) {
	type database struct {
		Host     string `env:"HOST" envDefault:"default-host"`
		Port     uint16 `env:"PORT" envDefault:"5432"`
		Password string `env:"PASSWORD" envRequired:"true"`
	}
	type config struct {
		DB   database `envPrefix:"DB_"`
		Name string   `env:"NAME" envDefault:"default-name"`
	}
	base := envFile(t, ".env", "# configuration\nexport APP_DB_HOST=base\nAPP_DB_PORT=15432\nAPP_DB_PASSWORD='secret#with=equals'\nAPP_NAME=from-file\n")
	local := envFile(t, ".env.local", "APP_DB_HOST=local\nAPP_DB_PORT=25432\n")
	lookup := func(key string) (string, bool) {
		if key == "APP_DB_HOST" {
			return "process", true
		}
		if key == "APP_NAME" {
			return "", true
		}
		return "", false
	}
	value, err := pfwconfig.Load[config](pfwconfig.Options{Prefix: "APP_", Lookup: lookup, EnvFiles: []string{base, local}})
	if err != nil || value.DB.Host != "process" || value.DB.Port != 25432 || value.DB.Password != "secret#with=equals" || value.Name != "" {
		t.Fatalf("nested configuration precedence failed: %v", err)
	}
	value, err = pfwconfig.Load[config](pfwconfig.Options{Prefix: "APP_", Lookup: func(string) (string, bool) { return "", false }, EnvFiles: []string{base, local}})
	if err != nil || value.DB.Host != "local" || value.Name != "from-file" {
		t.Fatalf("file precedence failed: %v", err)
	}
}

func TestDotenvDoesNotMutateProcess(t *testing.T) {
	const key = "PFW_DOTENV_TEST_VALUE"
	original, present := os.LookupEnv(key)
	file := envFile(t, ".env", key+"='from file'\n")
	type config struct {
		Value string `env:"PFW_DOTENV_TEST_VALUE"`
	}
	value, err := pfwconfig.Load[config](pfwconfig.Options{EnvFiles: []string{file}, Lookup: func(string) (string, bool) { return "", false }})
	if err != nil || value.Value != "from file" {
		t.Fatalf("file loading failed: %v", err)
	}
	if current, exists := os.LookupEnv(key); current != original || exists != present {
		t.Fatal("loader mutated the process environment")
	}
	t.Setenv(key, "process")
	value, err = pfwconfig.Load[config](pfwconfig.Options{EnvFiles: []string{file}})
	if err != nil || value.Value != "process" {
		t.Fatalf("real process precedence failed: %v", err)
	}
}

func TestDotenvFailureAndMissingFilePolicy(t *testing.T) {
	type config struct {
		Name string `env:"NAME" envDefault:"default"`
	}
	missing := filepath.Join(t.TempDir(), ".env")
	value, err := pfwconfig.Load[config](pfwconfig.Options{EnvFiles: []string{missing}})
	if !errors.Is(err, fs.ErrNotExist) || value != (config{}) {
		t.Fatalf("missing file: %+v %v", value, err)
	}
	value, err = pfwconfig.Load[config](pfwconfig.Options{EnvFiles: []string{missing}, IgnoreMissingEnvFiles: true, Lookup: func(string) (string, bool) { return "", false }})
	if err != nil || value.Name != "default" {
		t.Fatalf("optional file: %+v %v", value, err)
	}
	malformed := envFile(t, ".env", "NAME=valid\nSECRET='do-not-print-this\n")
	value, err = pfwconfig.Load[config](pfwconfig.Options{EnvFiles: []string{malformed}, IgnoreMissingEnvFiles: true})
	if value != (config{}) || err == nil || strings.Contains(err.Error(), "do-not-print-this") || !strings.Contains(err.Error(), "invalid dotenv syntax") {
		t.Fatalf("syntax error was not safely reported: %v", err)
	}
	if _, err := pfwconfig.Load[config](pfwconfig.Options{EnvFiles: []string{t.TempDir()}, IgnoreMissingEnvFiles: true}); err == nil {
		t.Fatal("directory read error was ignored")
	}
}
