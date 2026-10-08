package scaffold

import (
	"encoding/json"
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
		{"unsupported API environment", func(o *Options) { o.Environment = "uat" }},
		{"invalid environment", func(o *Options) { o.Environment = "../secret" }},
		{"missing version", func(o *Options) { o.FrameworkVersion = "" }},
		{"invalid version", func(o *Options) { o.FrameworkVersion = "latest" }},
		{"invalid local checkout", func(o *Options) { o.FrameworkVersion = ""; o.FrameworkDir = t.TempDir() }},
		{"both sources", func(o *Options) { o.FrameworkDir = frameworkRoot(t) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := Options{Template: "api", Directory: filepath.Join(t.TempDir(), "app"), Module: "example.com/app", Environment: "local", FrameworkVersion: "v0.1.0"}
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
	if _, err := Create(Options{Template: "api", Directory: dir, Module: "example.com/app", Environment: "local", FrameworkVersion: "v0.1.0"}); err == nil {
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
	if _, err := Create(Options{Template: "api", Directory: dir, Module: "example.com/app", Environment: "local", FrameworkVersion: "v0.1.0"}); err != nil {
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
	frameworkVersion, driverVersion := "", ""
	for _, requirement := range file.Require {
		switch requirement.Mod.Path {
		case FrameworkModule:
			frameworkVersion = requirement.Mod.Version
		case "github.com/jackc/pgx/v5":
			driverVersion = requirement.Mod.Version
		}
	}
	if file.Module.Mod.Path != "example.com/app" || frameworkVersion != "v0.1.0" || driverVersion != "v5.11.0" || len(file.Replace) != 0 || len(file.Tool) != 1 {
		t.Fatalf("go.mod: %s", data)
	}
	for _, name := range []string{"pfw.toml", ".gitignore", "env/.env.example", "README.md", "internal/bootstrap/compose.go", "internal/item/infrastructure/http/controller.go", "internal/item/infrastructure/memory/store.go", "internal/platform/database/database.go", "internal/item/infrastructure/postgres/store.go", "schema.sql", "env/.env.local", "env/.env.staging", "env/.env.prod"} {
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
		{"api/stdlib/custom-mapper", "api", "stdlib", "./cmd/api"},
		{"api/echo/custom-mapper", "api", "echo", "./cmd/api"},
	} {
		t.Run(tpl.Name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "project with spaces")
			env := "local"
			if tpl.Template == "hello-world" {
				env = "uat"
			}
			if _, err := Create(Options{Template: tpl.Template, Router: tpl.Router, Directory: dir, Module: "example.test/starter", Environment: env, FrameworkDir: frameworkRoot(t)}); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(tpl.Name, "custom-logger") {
				installCustomLogger(t, dir)
			}
			if strings.Contains(tpl.Name, "custom-mapper") {
				installCustomMapper(t, dir)
			}
			data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
			if err != nil {
				t.Fatal(err)
			}
			module, err := modfile.Parse("go.mod", data, nil)
			if err != nil {
				t.Fatal(err)
			}
			hasEcho, hasPGX := false, false
			for _, require := range module.Require {
				hasEcho = hasEcho || require.Mod.Path == "github.com/labstack/echo/v5"
				hasPGX = hasPGX || require.Mod.Path == "github.com/jackc/pgx/v5"
			}
			if hasEcho != (tpl.Router == "echo") {
				t.Fatalf("unexpected router dependency: %s", data)
			}
			if hasPGX != (tpl.Template == "api") {
				t.Fatalf("unexpected SQL driver dependency: %s", data)
			}
			if tpl.Template == "api" {
				handler, err := os.ReadFile(filepath.Join(dir, "internal/item/infrastructure/http/controller.go"))
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(handler), "github.com/labstack/echo/v5") != (tpl.Router == "echo") {
					t.Fatalf("wrong HTTP variant: %s", handler)
				}
			}
			commandDir := dir
			goCommand := func(args ...string) string {
				t.Helper()
				cmd := exec.Command("go", args...)
				cmd.Dir = commandDir
				cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=", "PFW_ENV="+env)
				output, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("go %v: %v\n%s", args, err, output)
				}
				return string(output)
			}
			goCommand("mod", "tidy")
			goCommand("generate", "./internal/bootstrap")
			commandDir = filepath.Join(dir, "internal", "bootstrap")
			goCommand("tool", "pfw", "generate", "-env", env)
			goCommand("tool", "pfw", "generate", "-env", env, "-check")
			plan := goCommand("tool", "pfw", "inspect", "-env", env, "-json")
			if tpl.Template == "api" {
				assertStorageGraph(t, plan, env)
			}
			commandDir = dir
			goCommand("test", "./...")
			if tpl.Template == "hello-world" {
				if output := goCommand("tool", "pfw", "run", "-env", env); strings.TrimSpace(output) != "Hello, world!" {
					t.Fatalf("hello output: %s", output)
				}
			} else {
				goCommand("build", "-o", "api", "./cmd/api")
				if tpl.Name == "api/stdlib" || tpl.Name == "api/echo" {
					for _, target := range []string{"staging", "prod"} {
						env = target
						goCommand("tool", "pfw", "generate", "-env", env)
						goCommand("tool", "pfw", "generate", "-env", env, "-check")
						plan := goCommand("tool", "pfw", "inspect", "-env", env, "-json")
						assertStorageGraph(t, plan, env)
						goCommand("test", "./...")
						goCommand("build", "-o", "api", "./cmd/api")
					}
				}
			}
		})
	}
}

func assertStorageGraph(t *testing.T, plan, env string) {
	t.Helper()
	var report struct {
		Initializers []struct {
			ConstructionOrder []string `json:"construction_order"`
		} `json:"initializers"`
	}
	if err := json.Unmarshal([]byte(plan), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Initializers) != 1 {
		t.Fatalf("initializers: %s", plan)
	}
	order := strings.Join(report.Initializers[0].ConstructionOrder, "\n")
	memory := strings.Contains(order, "memory.NewStore")
	postgres := strings.Contains(order, "postgres.NewStore")
	database := strings.Contains(order, "OpenMainDB")
	if (env == "local" && (!memory || postgres || database)) || (env != "local" && (memory || !postgres || !database)) {
		t.Fatalf("storage graph for %s: %s", env, order)
	}
}

func installCustomMapper(t *testing.T, dir string) {
	t.Helper()
	compose := filepath.Join(dir, "internal/bootstrap/compose.go")
	data, err := os.ReadFile(compose)
	if err != nil {
		t.Fatal(err)
	}
	source := strings.Replace(string(data), `"github.com/palma99/palma-framework/transport/httpserver"`, `"github.com/palma99/palma-framework/transport/httpserver"`+"\n"+`pfwhttp "github.com/palma99/palma-framework/http"`+"\n"+`"example.test/starter/internal/platform/custommapper"`, 1)
	source = strings.Replace(source, `pfw.Discover("../..."),`, `pfw.Discover("../..."), pfw.Bind[pfwhttp.ErrorMapper, *custommapper.Mapper](),`, 1)
	if err := os.WriteFile(compose, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"internal/platform/custommapper/mapper.go": `package custommapper
import("net/http";"sync/atomic";pfwhttp "github.com/palma99/palma-framework/http";itemhttp "example.test/starter/internal/item/infrastructure/http")
var Calls atomic.Int32
type Mapper struct{pfwhttp.ErrorMapper}
//pfw:coconut
func NewMapper()*Mapper{return &Mapper{ErrorMapper:itemhttp.NewErrorMapper()}}
func(m *Mapper)Map(err error)*pfwhttp.Error{Calls.Add(1);return m.ErrorMapper.Map(err)}
func(m *Mapper)Write(w http.ResponseWriter,err error)error{mapped:=m.Map(err);if mapped==nil{return nil};return mapped.Write(w)}
`,
		"internal/bootstrap/custom_mapper_test.go": `package bootstrap_test
import("context";"net/http/httptest";"testing";"example.test/starter/internal/bootstrap";"example.test/starter/internal/config";"example.test/starter/internal/platform/custommapper")
func TestCustomMapperIsInjected(t *testing.T){
 custommapper.Calls.Store(0)
 server,cleanup,err:=bootstrap.Initialize(context.Background(),"local",config.Config{});if err!=nil{t.Fatal(err)};defer cleanup()
 response:=httptest.NewRecorder();server.HTTPServer().Handler.ServeHTTP(response,httptest.NewRequest("GET","/items/missing",nil))
 if response.Code!=404||custommapper.Calls.Load()!=1{t.Fatalf("custom mapper was not used: status=%d calls=%d",response.Code,custommapper.Calls.Load())}
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

func installCustomLogger(t *testing.T, dir string) {
	t.Helper()
	compose := filepath.Join(dir, "internal/bootstrap/compose.go")
	data, err := os.ReadFile(compose)
	if err != nil {
		t.Fatal(err)
	}
	source := strings.Replace(string(data), `"github.com/palma99/palma-framework/transport/httpserver"`, `"github.com/palma99/palma-framework/transport/httpserver"`+"\n"+`"github.com/palma99/palma-framework/logging"`+"\n"+`"example.test/starter/internal/platform/customlogger"`, 1)
	source = strings.Replace(source, `pfw.Discover("../..."),`, `pfw.Discover("../..."), pfw.Bind[logging.Logger, *customlogger.Logger](),`, 1)
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
 server,cleanup,err:=bootstrap.Initialize(context.Background(),"local",config.Config{HTTP:config.HTTPConfig{Address:"127.0.0.1:0",ReadHeaderTimeout:time.Second,ShutdownTimeout:time.Second}})
 if err!=nil{t.Fatal(err)};defer cleanup()
 logger,ok:=server.Logger().(*customlogger.Logger);if !ok{t.Fatalf("logger: %T",server.Logger())}
 if err:=server.Start(context.Background());err!=nil{t.Fatal(err)}
 t.Cleanup(func(){ctx,cancel:=context.WithTimeout(context.Background(),time.Second);defer cancel();if err:=server.Stop(ctx);err!=nil{t.Error(err)};if err:=server.Wait();err!=nil{t.Error(err)}})
 if logger.Counts.Info.Load()!=1{t.Fatal("readiness did not use custom logger")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 request:=httptest.NewRequest("GET","/items",nil).WithContext(ctx)
 response:=httptest.NewRecorder();server.HTTPServer().Handler.ServeHTTP(response,request)
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
