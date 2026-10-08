package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
)

func TestReleaseBinaryVersionAndDefaultDependency(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "pfw")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-trimpath", "-ldflags=-X main.version=v0.1.0", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	command := exec.Command(binary, "version")
	if output, err := command.CombinedOutput(); err != nil || string(output) != "pfw v0.1.0\n" {
		t.Fatalf("version: %v %s", err, output)
	}
	app := filepath.Join(dir, "app")
	command = exec.Command(binary, "new", "-template", "api", "-router", "echo", "-module", "example.com/releaseapp", app)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("release new: %v\n%s", err, output)
	}
	data, err := os.ReadFile(filepath.Join(app, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	module, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, requirement := range module.Require {
		if requirement.Mod.Path == "github.com/palma99/palma-framework" && requirement.Mod.Version == "v0.1.0" {
			found = true
		}
	}
	if !found || len(module.Replace) != 0 {
		t.Fatalf("release dependency: %s", data)
	}
	for _, name := range []string{"internal/item/infrastructure/http/handler.go", "internal/platform/http/server.go"} {
		data, err := os.ReadFile(filepath.Join(app, name))
		if err != nil || !strings.Contains(string(data), "echo") {
			t.Fatalf("bundled Echo template: %v %s", err, data)
		}
	}
}

func TestVersionCommandArguments(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"version", "extra"}, &out, &out); err == nil {
		t.Fatal("version accepted arguments")
	}
}
