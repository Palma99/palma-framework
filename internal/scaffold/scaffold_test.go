package scaffold

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
)

func frameworkRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestInvalidOptionsDoNotCreateProject(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Options)
	}{
		{"unsupported router", func(o *Options) { o.Router = "gin" }},
		{"invalid router path", func(o *Options) { o.Router = "../echo" }},
		{"router on hello world", func(o *Options) { o.Template = "hello-world"; o.Router = "echo" }},
		{"unknown template", func(o *Options) { o.Template = "../api" }},
		{"invalid module", func(o *Options) { o.Module = "bad module" }},
		{"framework module", func(o *Options) { o.Module = FrameworkModule }},
		{"invalid environment", func(o *Options) { o.Environment = "../secret" }},
		{"missing version", func(o *Options) { o.FrameworkVersion = "" }},
		{"invalid version", func(o *Options) { o.FrameworkVersion = "latest" }},
		{"invalid local checkout", func(o *Options) { o.FrameworkVersion = ""; o.FrameworkDir = t.TempDir() }},
		{"both sources", func(o *Options) { o.FrameworkDir = frameworkRoot(t) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := Options{Template: "api", Directory: filepath.Join(t.TempDir(), "app"), Module: "example.com/app", Environment: "uat", FrameworkVersion: "v0.1.0"}
			tc.change(&o)
			if _, err := Create(o); err == nil {
				t.Fatal("invalid options accepted")
			}
			if _, err := os.Lstat(o.Directory); !os.IsNotExist(err) {
				t.Fatal("invalid options created destination")
			}
		})
	}
}

func TestNeverOverwriteExistingDestination(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(file, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(Options{Template: "api", Directory: dir, Module: "example.com/app", Environment: "dev", FrameworkVersion: "v0.1.0"}); err == nil {
		t.Fatal("existing destination accepted")
	}
	data, err := os.ReadFile(file)
	if err != nil || string(data) != "keep" {
		t.Fatal("existing content changed")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatal("wrote files into existing project")
	}
}

func TestPublishedVersionAndBundledFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "app")
	if _, err := Create(Options{Template: "api", Directory: dir, Module: "example.com/app", Environment: "uat", FrameworkVersion: "v0.1.0"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	file, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if file.Module.Mod.Path != "example.com/app" || file.Require[0].Mod.Version != "v0.1.0" || len(file.Replace) != 0 || len(file.Tool) != 1 {
		t.Fatalf("go.mod: %s", data)
	}
	for _, name := range []string{".gitignore", ".env.example", "README.md", "internal/bootstrap/compose.go", "internal/item/infrastructure/http/handler.go", "internal/item/infrastructure/memory/store.go"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), "{{.") {
			t.Fatalf("unexpanded template: %s", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestStandaloneTemplatesGenerateCompileAndRun(t *testing.T) {
	for _, tpl := range []struct{ Name, Template, Router, Entry string }{
		{"hello-world", "hello-world", "", "./cmd/app"},
		{"api/default", "api", "", "./cmd/api"},
		{"api/stdlib", "api", "stdlib", "./cmd/api"},
		{"api/echo", "api", "echo", "./cmd/api"},
	} {
		t.Run(tpl.Name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "project with spaces")
			if _, err := Create(Options{Template: tpl.Template, Router: tpl.Router, Directory: dir, Module: "example.test/starter", Environment: "uat", FrameworkDir: frameworkRoot(t)}); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
			if err != nil {
				t.Fatal(err)
			}
			module, err := modfile.Parse("go.mod", data, nil)
			if err != nil {
				t.Fatal(err)
			}
			hasEcho := false
			for _, require := range module.Require {
				hasEcho = hasEcho || require.Mod.Path == "github.com/labstack/echo/v5"
			}
			if hasEcho != (tpl.Router == "echo") {
				t.Fatalf("unexpected router dependency: %s", data)
			}
			if tpl.Template == "api" {
				handler, err := os.ReadFile(filepath.Join(dir, "internal/item/infrastructure/http/handler.go"))
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(handler), "github.com/labstack/echo/v5") != (tpl.Router == "echo") {
					t.Fatalf("wrong HTTP variant: %s", handler)
				}
			}
			goCommand := func(args ...string) string {
				t.Helper()
				cmd := exec.Command("go", args...)
				cmd.Dir = dir
				cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=", "PFW_ENV=uat")
				output, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("go %v: %v\n%s", args, err, output)
				}
				return string(output)
			}
			goCommand("mod", "tidy")
			goCommand("generate", "./internal/bootstrap")
			goCommand("tool", "pfw", "generate", "-env", "uat", "-check", "./internal/bootstrap")
			goCommand("test", "./...")
			if tpl.Template == "hello-world" {
				if output := goCommand("tool", "pfw", "run", "-env", "uat", tpl.Entry); strings.TrimSpace(output) != "Hello, world!" {
					t.Fatalf("hello output: %s", output)
				}
			} else {
				goCommand("build", "-o", "api", "./cmd/api")
			}
		})
	}
}
