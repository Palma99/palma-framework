package scaffold

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestDockerDevelopmentTemplates(t *testing.T) {
	for _, tc := range []struct{ template, router, environment string }{
		{"api", "stdlib", "local"}, {"api", "echo", "staging"}, {"hello-world", "", "dev"},
	} {
		t.Run(tc.template+"/"+tc.router, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "app")
			if _, err := Create(Options{Template: tc.template, Router: tc.router, Environment: tc.environment, Docker: true, Directory: dir, Module: "example.test/dockerapp", FrameworkDir: frameworkRoot(t)}); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(dir, ".air.toml"))
			if err != nil {
				t.Fatal(err)
			}
			var air struct {
				Build struct {
					Cmd          string
					Entrypoint   []string
					ExcludeRegex []string
					Poll         bool
					KillDelay    string
				}
			}
			if err := toml.Unmarshal(data, &air); err != nil {
				t.Fatal(err)
			}
			if air.Build.Cmd == "" || len(air.Build.Entrypoint) != 1 {
				t.Fatal("Air has no build/start command")
			}
			for _, script := range []string{"docker/build.sh", "docker/entrypoint.sh"} {
				cmd := exec.Command("sh", "-n", filepath.Join(dir, script))
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("shell syntax: %v %s", err, output)
				}
			}
			if _, err := exec.LookPath("docker"); err != nil {
				t.Log("Docker CLI unavailable; Compose validation skipped")
				return
			}
			for _, env := range []string{tc.environment, "staging"} {
				if tc.template != "api" && env != tc.environment {
					continue
				}
				// Use Compose's file-based environment selection, without terminal exports.
				if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("PFW_ENV="+env+"\nDEV_HTTP_PORT=18080\nDEV_DB_PORT=15432\n"), 0600); err != nil {
					t.Fatal(err)
				}
				cmd := exec.Command("docker", "compose", "config", "--format", "json")
				cmd.Dir = dir
				for _, variable := range os.Environ() {
					if !strings.HasPrefix(variable, "PFW_ENV=") && !strings.HasPrefix(variable, "DEV_HTTP_PORT=") && !strings.HasPrefix(variable, "DEV_DB_PORT=") && !strings.HasPrefix(variable, "COMPOSE_") {
						cmd.Env = append(cmd.Env, variable)
					}
				}
				output, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("compose validation: %v %s", err, output)
				}
				var config struct {
					Services map[string]struct {
						Environment map[string]string
						DependsOn   map[string]struct{ Condition string } `json:"depends_on"`
						Volumes     []struct {
							Source, Target string
							ReadOnly       bool `json:"read_only"`
						}
					}
				}
				if err := json.Unmarshal(output, &config); err != nil {
					t.Fatal(err)
				}
				app := config.Services["app"]
				if app.Environment["PFW_ENV"] != env {
					t.Fatalf("environment: %v", app.Environment)
				}
				mounted := false
				for _, volume := range app.Volumes {
					if volume.Target == frameworkRoot(t) && volume.Source == frameworkRoot(t) && volume.ReadOnly {
						mounted = true
					}
				}
				if !mounted {
					t.Fatal("local framework replace is not mounted into container")
				}
				if tc.template == "api" {
					if app.DependsOn["db"].Condition != "service_healthy" || app.Environment["APP_HTTP_ADDRESS"] != ":8080" || !strings.Contains(app.Environment["APP_DB_DSN"], "@db:5432/") {
						t.Fatalf("API development settings: %+v", app)
					}
					if _, exists := config.Services["db"]; !exists {
						t.Fatal("missing development database")
					}
				} else if _, exists := config.Services["db"]; exists {
					t.Fatal("hello-world does not need a database")
				}
			}
		})
	}
}

func TestDockerFilesAreOptIn(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "app")
	if _, err := Create(Options{Template: "api", Directory: dir, Module: "example.test/app", FrameworkVersion: "v0.1.0"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "compose.yaml")); !os.IsNotExist(err) {
		t.Fatal("Docker files generated without -docker")
	}
}
