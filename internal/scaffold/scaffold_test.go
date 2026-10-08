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
		{"api/stdlib/custom-logger", "api", "stdlib", "./cmd/api"},
		{"api/echo/custom-logger", "api", "echo", "./cmd/api"},
	} {
		t.Run(tpl.Name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "project with spaces")
			if _, err := Create(Options{Template: tpl.Template, Router: tpl.Router, Directory: dir, Module: "example.test/starter", Environment: "uat", FrameworkDir: frameworkRoot(t)}); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(tpl.Name, "custom-logger") {
				installCustomLogger(t, dir)
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

func installCustomLogger(t *testing.T, dir string) {
	t.Helper()
	compose := filepath.Join(dir, "internal/bootstrap/compose.go")
	data, err := os.ReadFile(compose)
	if err != nil {
		t.Fatal(err)
	}
	source := strings.Replace(string(data), `"github.com/palma99/palma-framework/logging"`, `"github.com/palma99/palma-framework/logging"`+"\n"+`"example.test/starter/internal/platform/customlogger"`, 1)
	source = strings.Replace(source, "pfw.Bind[logging.Logger, *logging.SlogLogger]()", "pfw.Bind[logging.Logger, *customlogger.Logger]()", 1)
	if err := os.WriteFile(compose, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"internal/platform/customlogger/logger.go": `package customlogger
import("context";"io";"sync/atomic";"github.com/palma99/palma-framework/logging")
type Counts struct{Info,Error atomic.Int32}
type Logger struct{logging.Logger;Counts *Counts}
//pfw:coconut
func NewLogger()*Logger{return &Logger{Logger:logging.New(logging.Options{Output:io.Discard}),Counts:&Counts{}}}
func(l *Logger)Info(ctx context.Context,msg string,args ...any){l.Counts.Info.Add(1);l.Logger.Info(ctx,msg,args...)}
func(l *Logger)Error(ctx context.Context,msg string,args ...any){l.Counts.Error.Add(1);l.Logger.Error(ctx,msg,args...)}
func(l *Logger)With(args ...any)logging.Logger{return &Logger{Logger:l.Logger.With(args...),Counts:l.Counts}}
`,
		"internal/bootstrap/custom_logger_test.go": `package bootstrap_test
import("context";"net/http/httptest";"testing";"time";"example.test/starter/internal/bootstrap";"example.test/starter/internal/config";"example.test/starter/internal/platform/customlogger")
func TestLoggerBindingReplacesDefault(t *testing.T){
 server,cleanup,err:=bootstrap.Initialize("uat",config.Config{HTTP:config.HTTPConfig{Address:"127.0.0.1:0",ReadHeaderTimeout:time.Second,ShutdownTimeout:time.Second}})
 if err!=nil{t.Fatal(err)};defer cleanup()
 logger,ok:=server.Logger.(*customlogger.Logger);if !ok{t.Fatalf("logger: %T",server.Logger)}
 if err:=server.Start(context.Background());err!=nil{t.Fatal(err)}
 t.Cleanup(func(){ctx,cancel:=context.WithTimeout(context.Background(),time.Second);defer cancel();if err:=server.Stop(ctx);err!=nil{t.Error(err)};if err:=server.Wait();err!=nil{t.Error(err)}})
 if logger.Counts.Info.Load()!=1{t.Fatal("readiness did not use custom logger")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 request:=httptest.NewRequest("GET","/items",nil).WithContext(ctx)
 response:=httptest.NewRecorder();server.HTTP.Handler.ServeHTTP(response,request)
 if response.Code!=500||logger.Counts.Error.Load()!=1{t.Fatalf("HTTP did not use same custom logger: status=%d errors=%d",response.Code,logger.Counts.Error.Load())}
}
`,
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}
