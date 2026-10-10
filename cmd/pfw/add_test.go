package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewAuthAndAddCommand(t *testing.T) {
	for _, atCreation := range []bool{true, false} {
		dir := filepath.Join(t.TempDir(), "api")
		var out bytes.Buffer
		args := []string{"new", "-template", "api", "-router", "echo", "-module", "example.test/app", "-framework-version", "v0.1.0"}
		if atCreation {
			args = append(args, "-auth")
		}
		args = append(args, dir)
		if err := run(args, &out, &out); err != nil {
			t.Fatal(err)
		}
		if !atCreation {
			out.Reset()
			if err := run([]string{"add", "auth", "-dir", dir}, &out, &out); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "Added auth module") {
				t.Fatalf("add: %s", out.String())
			}
		}
		out.Reset()
		if err := run([]string{"add", "auth", "-dir", dir}, &out, &out); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "already installed") {
			t.Fatalf("repeat: %s", out.String())
		}
		if _, err := os.Stat(filepath.Join(dir, "internal/auth/principal.go")); err != nil {
			t.Fatal(err)
		}
		settings, err := readProjectSettings(dir)
		if err != nil || len(settings.Modules) != 1 || settings.Modules[0] != "auth" {
			t.Fatalf("settings: %+v %v", settings, err)
		}
	}
}

func TestAddAuthCommandFromSubdirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "api")
	var out bytes.Buffer
	if err := run([]string{"new", "-template", "api", "-module", "example.test/app", "-framework-version", "v0.1.0", dir}, &out, &out); err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Join(dir, "internal/item"))
	if err := run([]string{"add", "auth"}, &out, &out); err != nil {
		t.Fatal(err)
	}
}

func TestAddCommandRejectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{{"add"}, {"add", "unknown"}, {"add", "auth", "extra"}, {"add", "auth", "-unknown"}, {"new", "-auth", "-module", "example.test/app", "-framework-version", "v0.1.0", filepath.Join(t.TempDir(), "app")}} {
		var out bytes.Buffer
		if err := run(args, &out, &out); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
