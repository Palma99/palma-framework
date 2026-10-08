package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelpIsSuccessfulAndDoesNotLoadProjects(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, args := range [][]string{nil, {"help"}, {"--help"}, {"-h"}, {"-help"}} {
		var stdout, stderr bytes.Buffer
		if err := run(args, &stdout, &stderr); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		for _, text := range []string{"Palma Framework", "Usage", "Commands", "Examples", "go tool pfw run"} {
			if !strings.Contains(stdout.String(), text) {
				t.Fatalf("missing %q: %s", text, stdout.String())
			}
		}
		if stderr.Len() != 0 || strings.Contains(stdout.String(), "\x1b[") {
			t.Fatalf("redirected help: stderr=%s stdout=%s", stderr.String(), stdout.String())
		}
	}
	for name := range commands {
		for _, args := range [][]string{{name, "--help"}, {name, "-h"}, {"help", name}} {
			var stdout, stderr bytes.Buffer
			if err := run(args, &stdout, &stderr); err != nil {
				t.Fatalf("%v: %v", args, err)
			}
			if !strings.Contains(stdout.String(), "pfw "+name) || !strings.Contains(stdout.String(), "Options") || stderr.Len() != 0 || strings.Contains(stdout.String(), "\x1b[") {
				t.Fatalf("%v: stdout=%s stderr=%s", args, stdout.String(), stderr.String())
			}
			if name == "generate" || name == "inspect" || name == "new" || name == "run" {
				if !strings.Contains(stdout.String(), "-env name") {
					t.Fatalf("missing env flag: %s", stdout.String())
				}
			}
		}
	}
}

func TestFlagErrorsAreSingleDiagnosticsWithHelpHint(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"run", "-unknown"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "pfw run --help") || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("error=%v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
	if err := run([]string{"help", "unknown"}, &stdout, &stderr); err == nil {
		t.Fatal("unknown help command accepted")
	}
}

func TestBinaryHelpExitStatus(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "pfw")
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, output)
	}
	dir := t.TempDir()
	for _, args := range [][]string{nil, {"--help"}, {"help", "new"}, {"run", "--help"}} {
		command := exec.Command(binary, args...)
		command.Dir = dir
		var stderr bytes.Buffer
		command.Stderr = &stderr
		if output, err := command.Output(); err != nil || len(output) == 0 || stderr.Len() != 0 {
			t.Fatalf("%v: %v stdout=%s stderr=%s", args, err, output, stderr.String())
		}
	}
	command := exec.Command(binary, "generate", "-unknown")
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err == nil || strings.Count(string(output), "pfw:") != 1 || !strings.Contains(string(output), "pfw generate --help") {
		t.Fatalf("invalid flag: %v %s", err, output)
	}
	command = exec.Command(binary, "--version")
	command.Env = append(os.Environ(), "NO_COLOR=1")
	if output, err := command.Output(); err != nil || !strings.HasPrefix(string(output), "pfw ") {
		t.Fatalf("version: %v %s", err, output)
	}
}
