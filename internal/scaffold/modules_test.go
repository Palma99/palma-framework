package scaffold

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
)

func projectSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			result[relative] = "symlink:" + target
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result[relative] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestAuthModuleNewAndAddGenerateCompileAndRun(t *testing.T) {
	for _, router := range []string{"stdlib", "echo"} {
		for _, atCreation := range []bool{true, false} {
			name := router + "/add"
			if atCreation {
				name = router + "/new"
			}
			t.Run(name, func(t *testing.T) {
				dir := filepath.Join(t.TempDir(), "app")
				_, err := Create(Options{Template: "api", Router: router, Auth: atCreation, Directory: dir, Module: "example.test/authapp", FrameworkDir: frameworkRoot(t)})
				if err != nil {
					t.Fatal(err)
				}
				if !atCreation {
					// Preserve an application customization in a file the recipe edits.
					path := filepath.Join(dir, "internal/platform/http/server.go")
					data, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					data = append(data, []byte("\n// application customization\nfunc CustomValue() string { return \"kept\" }\n")...)
					if err := os.WriteFile(path, data, 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.Chmod(path, 0600); err != nil {
						t.Fatal(err)
					}
					baseline := projectSnapshot(t, dir)
					root, added, err := AddModule(filepath.Join(dir, "internal/item"), "auth")
					if err != nil || !added {
						t.Fatalf("add: %s %v %v", root, added, err)
					}
					installed := projectSnapshot(t, dir)
					for path, original := range baseline {
						if path == "pfw.toml" || path == "internal/bootstrap/compose.go" {
							continue
						}
						if installed[path] != original {
							t.Fatalf("auth changed existing file %s", path)
						}
					}
					data, err = os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Contains(data, []byte("application customization")) || !bytes.Contains(data, []byte("CustomValue")) {
						t.Fatal("custom code lost")
					}
					info, err := os.Stat(path)
					if err != nil || info.Mode().Perm() != 0600 {
						t.Fatal("file permissions changed")
					}
				}
				for _, path := range []string{"internal/auth/principal.go", "internal/auth/README.md", "internal/bootstrap/auth_module.go"} {
					if _, err := os.Stat(filepath.Join(dir, path)); err != nil {
						t.Fatal(err)
					}
				}
				authFiles := map[string]bool{}
				for path := range projectSnapshot(t, dir) {
					if strings.HasPrefix(path, "internal/auth/") || strings.Contains(path, "auth_module.go") {
						authFiles[path] = true
					}
				}
				if len(authFiles) != 3 {
					t.Fatalf("unexpected auth files: %v", authFiles)
				}

				before := projectSnapshot(t, dir)
				if _, added, err := AddModule(dir, "auth"); err != nil || added {
					t.Fatalf("repeat: %v %v", added, err)
				}
				if !reflect.DeepEqual(before, projectSnapshot(t, dir)) {
					t.Fatal("repeat changed files")
				}
				if _, err := os.Stat(filepath.Join(dir, "migrations/000002_create_auth.sql")); !os.IsNotExist(err) {
					t.Fatal("auth generated a migration")
				}
				if _, err := os.Stat(filepath.Join(dir, "internal/auth/infrastructure/postgres")); !os.IsNotExist(err) {
					t.Fatal("auth generated a database repository")
				}
				if _, err := os.Stat(filepath.Join(dir, "cmd/auth-token")); !os.IsNotExist(err) {
					t.Fatalf("auth must not generate a token command: %v", err)
				}
				modData, err := os.ReadFile(filepath.Join(dir, "go.mod"))
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(modData, []byte("github.com/golang-jwt/jwt")) {
					t.Fatal("auth added a JWT dependency")
				}
				env := "local"
				command := func(args ...string) string {
					t.Helper()
					cmd := exec.Command("go", args...)
					cmd.Dir = dir
					cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=", "PFW_ENV="+env)
					output, err := cmd.CombinedOutput()
					if err != nil {
						t.Fatalf("go %v: %v\n%s", args, err, output)
					}
					return string(output)
				}
				command("mod", "tidy")
				for _, target := range []string{"local", "staging", "prod"} {
					env = target
					command("tool", "pfw", "generate", "-env", env)
					command("tool", "pfw", "generate", "-env", env, "-check")
					plan := command("tool", "pfw", "inspect", "-env", env, "-json")
					var report struct {
						Initializers []struct {
							ConstructionOrder []string `json:"construction_order"`
						} `json:"initializers"`
					}
					if err := json.Unmarshal([]byte(plan), &report); err != nil || len(report.Initializers) != 1 {
						t.Fatalf("inspect: %v", err)
					}
					order := strings.Join(report.Initializers[0].ConstructionOrder, "\n")
					if strings.Contains(order, "security/http.NewMiddleware") || strings.Contains(order, "internal/auth.") {
						t.Fatalf("base setup must not construct an authentication implementation: %s", order)
					}

					command("test", "-race", "./...")
					command("build", "./cmd/api")
				}
			})
		}
	}
}

func TestAddAuthConflictsDoNotChangeProject(t *testing.T) {
	for _, kind := range []string{"existing file", "symlink directory", "unsupported bootstrap", "auth module exists", "lock"} {
		t.Run(kind, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "app")
			if _, err := Create(Options{Template: "api", Directory: dir, Module: "example.test/app", FrameworkVersion: "v0.1.0"}); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "existing file":
				if err := os.MkdirAll(filepath.Join(dir, "internal/auth"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "internal/auth/principal.go"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink directory":
				if err := os.Symlink(t.TempDir(), filepath.Join(dir, "internal/auth")); err != nil {
					t.Fatal(err)
				}
			case "unsupported bootstrap":
				if err := os.WriteFile(filepath.Join(dir, "internal/bootstrap/compose.go"), []byte("package bootstrap\n"), 0644); err != nil {
					t.Fatal(err)
				}
			case "auth module exists":
				path := filepath.Join(dir, "internal/bootstrap/compose.go")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				data = append(data, []byte("\nvar AuthModule = pfw.Module()\n")...)
				if err := os.WriteFile(path, data, 0644); err != nil {
					t.Fatal(err)
				}

			case "lock":
				if err := os.WriteFile(filepath.Join(dir, ".pfw-add.lock"), []byte("reserved"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := projectSnapshot(t, dir)
			if _, _, err := AddModule(dir, "auth"); err == nil {
				t.Fatal("conflict accepted")
			}
			if !reflect.DeepEqual(before, projectSnapshot(t, dir)) {
				t.Fatal("failed installation changed project")
			}
		})
	}
}

func TestAddAuthLegacyManifestPreservesMigrations(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "app")
	if _, err := Create(Options{Template: "api", Directory: dir, Module: "example.test/app", FrameworkVersion: "v0.1.0"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "pfw.toml")
	if err := os.WriteFile(path, []byte("# legacy settings\nbootstrap = './internal/bootstrap'\nmain = './cmd/api'\nenv_dir = './env'\nmigrations_dir = './migrations'\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "migrations/000010_custom.sql"), []byte("keep migration"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, added, err := AddModule(dir, "auth"); err != nil || !added {
		t.Fatalf("legacy: %v %v", added, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "migrations/000011_create_auth.sql")); !os.IsNotExist(err) {
		t.Fatal("auth changed migrations")
	}
	dataMigration, err := os.ReadFile(filepath.Join(dir, "migrations/000010_custom.sql"))
	if err != nil || string(dataMigration) != "keep migration" {
		t.Fatal("existing migration changed")
	}
	data, _ := os.ReadFile(path)
	if !bytes.Contains(data, []byte("# legacy settings")) {
		t.Fatal("manifest comment lost")
	}
}

func TestAuthRejectsNonAPIAndUnknownModules(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "app")
	if _, err := Create(Options{Template: "hello-world", Auth: true, Directory: dir, Module: "example.test/app", FrameworkVersion: "v0.1.0"}); err == nil {
		t.Fatal("hello-world auth accepted")
	}
	if _, _, err := AddModule(t.TempDir(), "unknown"); err == nil {
		t.Fatal("unknown module accepted")
	}
}

func TestAddAuthPreservesExistingDependencies(t *testing.T) {
	for _, version := range []string{"v5.0.0", "v5.3.1", "v5.4.0"} {
		t.Run(version, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "app")
			if _, err := Create(Options{Template: "api", Directory: dir, Module: "example.test/app", FrameworkVersion: "v0.1.0"}); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "go.mod")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			module, err := modfile.Parse("go.mod", data, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := module.AddRequire("github.com/golang-jwt/jwt/v5", version); err != nil {
				t.Fatal(err)
			}
			data, err = module.Format()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0644); err != nil {
				t.Fatal(err)
			}
			if _, _, err := AddModule(dir, "auth"); err != nil {
				t.Fatal(err)
			}
			installed, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(data, installed) {
				t.Fatal("auth changed existing dependencies")
			}

		})
	}
}
