package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTemplatesAndNewCommand(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"templates"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "hello-world") || !strings.Contains(out.String(), "api") {
		t.Fatalf("templates: %s", out.String())
	}
	dir := filepath.Join(t.TempDir(), "app")
	out.Reset()
	if err := run([]string{"new", "-template", "api", "-module", "example.com/newapp", "-env", "staging", "-framework-version", "v0.1.0", dir}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "go tool pfw run -env staging ./cmd/api") {
		t.Fatalf("next steps: %s", out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "internal/item/domain/item.go")); err != nil {
		t.Fatal(err)
	}
}

func TestNewCommandRejectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{{"new"}, {"new", "one", "two"}, {"new", "-unknown"}, {"templates", "extra"}} {
		var out bytes.Buffer
		if err := run(args, &out, &out); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestNewCommandEchoRouter(t *testing.T) {
	var out bytes.Buffer
	dir := filepath.Join(t.TempDir(), "echo-api")
	if err := run([]string{"new", "-template", "api", "-router", "echo", "-module", "example.com/echoapi", "-framework-version", "v0.1.0", dir}, &out, &out); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join(dir, "internal/platform/http/server.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), "*echo.Echo") {
		t.Fatalf("wrong router: %s", source)
	}
	if !strings.Contains(out.String(), "go tool pfw run -env local ./cmd/api") {
		t.Fatalf("default API environment: %s", out.String())
	}
}
