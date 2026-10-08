package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestProjectPatterns(t *testing.T) {
	for _, tc := range []struct {
		name, config, bootstrap, main string
		missing                       bool
	}{
		{name: "no file", missing: true, bootstrap: "./internal/bootstrap", main: "./cmd/app"},
		{name: "empty file", bootstrap: "./internal/bootstrap", main: "./cmd/app"},
		{name: "custom paths", config: "bootstrap = 'private/assembly'\nmain = \"./cmd/my app\"\n", bootstrap: "./private/assembly", main: "./cmd/my app"},
		{name: "partial config", config: "main = './cmd/api'\n", bootstrap: "./internal/bootstrap", main: "./cmd/api"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := runFixture(t, "package main\nfunc main(){}\n")
			if !tc.missing {
				if err := os.WriteFile(filepath.Join(root, "pfw.toml"), []byte(tc.config), 0600); err != nil {
					t.Fatal(err)
				}
			}
			subdir := filepath.Join(root, "nested", "directory")
			if err := os.MkdirAll(subdir, 0700); err != nil {
				t.Fatal(err)
			}
			t.Chdir(subdir)
			// Getwd may resolve /var to /private/var on macOS.
			resolved, err := filepath.EvalSymlinks(root)
			if err != nil {
				t.Fatal(err)
			}
			for kind, want := range map[string]string{"bootstrap": tc.bootstrap, "main": tc.main} {
				dir, patterns, err := projectPatterns(kind, nil)
				actual, resolveErr := filepath.EvalSymlinks(dir)
				if err != nil || resolveErr != nil || actual != resolved || !reflect.DeepEqual(patterns, []string{want}) {
					t.Fatalf("%s: dir=%q patterns=%v error=%v", kind, dir, patterns, err)
				}
			}
		})
	}
}

func TestProjectConfigErrorsAndExplicitOverride(t *testing.T) {
	for _, config := range []string{"main = [", "main = 42", "main = ''", "bootstrap = '  '", "bootstarp = './wrong'"} {
		t.Run(config, func(t *testing.T) {
			t.Chdir(runFixture(t, "package main\nfunc main(){}\n"))
			if err := os.WriteFile("pfw.toml", []byte(config), 0600); err != nil {
				t.Fatal(err)
			}
			kind := "main"
			if strings.HasPrefix(config, "bootstrap") {
				kind = "bootstrap"
			}
			if _, _, err := projectPatterns(kind, nil); err == nil || !strings.Contains(err.Error(), "pfw.toml") {
				t.Fatalf("configuration error: %v", err)
			}
			explicit := []string{".", "./other/..."}
			dir, patterns, err := projectPatterns(kind, explicit)
			if err != nil || dir != "" || !reflect.DeepEqual(patterns, explicit) {
				t.Fatalf("explicit override: %q %v %v", dir, patterns, err)
			}
		})
	}
}

func TestProjectRootStopsAtNestedModule(t *testing.T) {
	root := runFixture(t, "package main\nfunc main(){}\n")
	if err := os.WriteFile(filepath.Join(root, "pfw.toml"), []byte("main = './outer'"), 0600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "go.mod"), []byte("module example.test/nested\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)
	if _, patterns, err := projectPatterns("main", nil); err != nil || patterns[0] != "./cmd/app" {
		t.Fatalf("nested module used parent config: %v %v", patterns, err)
	}
}

func TestRunConfiguredMainFromSubdirectoryWithAppArguments(t *testing.T) {
	root := runFixture(t, `package main
import("fmt";"os")
func main(){fmt.Print(os.Args[1], "|", os.Args[2])}
`)
	if err := os.WriteFile(filepath.Join(root, "pfw.toml"), []byte("main = '.'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Join(root, "nested")
	if err := os.Mkdir(subdir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(subdir)
	var out, diagnostics bytes.Buffer
	if err := runApplication(context.Background(), []string{"-env", "dev", "--", "-verbose", "hello world"}, &out, &diagnostics); err != nil {
		t.Fatalf("run: %v %s", err, diagnostics.String())
	}
	if out.String() != "-verbose|hello world" {
		t.Fatalf("app arguments: %q", out.String())
	}
}
