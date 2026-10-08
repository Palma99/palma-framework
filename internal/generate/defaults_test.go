package generate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFrameworkLoggerDefaultsAndApplicationPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name, registrations, input, want string
		fallback                         bool
	}{
		{"default", "", "", "*logging.SlogLogger", true},
		{"interface provider", "p.Constructors(NewInterface)", "", "*Custom", false},
		{"concrete binding", "p.Constructors(NewCustom),p.Bind[logging.Logger,*Custom]()", "", "*Custom", false},
		{"configured standard", "p.Constructors(NewStandard)", "", "*logging.SlogLogger", false},
		{"autobinding", "p.Module(p.Constructors(NewCustom),p.AutoBind())", "", "*Custom", false},
		{"interface input", "", "logger logging.Logger", "*Custom", false},
		{"concrete input", "", "logger *logging.SlogLogger", "*logging.SlogLogger", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := fixture(t, map[string]string{
				"app.go": `package app
import("io";"github.com/palma99/palma-framework/logging")
type Custom struct{logging.Logger}
func NewCustom()*Custom{return &Custom{logging.New(logging.Options{Output:io.Discard})}}
func NewInterface()logging.Logger{return NewCustom()}
func NewStandard()*logging.SlogLogger{return logging.New(logging.Options{Output:io.Discard})}
`,
				"template.go": "//go:build pfw_inject\npackage app\nimport(p \"github.com/palma99/palma-framework\";\"github.com/palma99/palma-framework/logging\")\nfunc Initialize(" + tc.input + ")(logging.Logger,error){return p.Build[logging.Logger](" + tc.registrations + ")}\n",
			})
			if _, err := Run(context.Background(), Config{Dir: dir}); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(dir, "pfw_gen.go"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), ".NewDefault()") != tc.fallback {
				t.Fatalf("fallback selection: %s", data)
			}
			args := ""
			if tc.input == "logger logging.Logger" {
				args = "NewCustom()"
			}
			if tc.input == "logger *logging.SlogLogger" {
				args = "NewStandard()"
			}
			// All generated initializers retain the application's original signature.
			runtime := `package app
import("testing";"github.com/palma99/palma-framework/logging")
func TestSelected(t *testing.T){logger,err:=Initialize(` + args + `);if err!=nil{t.Fatal(err)};if _,ok:=logger.(` + tc.want + `);!ok{t.Fatalf("logger: %T",logger)};_ = logging.Options{}}
`
			if err := os.WriteFile(filepath.Join(dir, "app_test.go"), []byte(runtime), 0644); err != nil {
				t.Fatal(err)
			}
			compileAndRun(t, dir)
			report, err := Inspect(context.Background(), Config{Dir: dir})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, p := range report.Initializers[0].Providers {
				if p.Fallback && p.Status == "used" {
					found = true
					if len(p.Origins) != 1 || p.Origins[0] != "framework_default" {
						t.Fatalf("origins: %+v", p)
					}
				}
			}
			if found != tc.fallback {
				t.Fatalf("fallback inspection: %+v", report)
			}
		})
	}
}

func TestDefaultsDoNotHideApplicationErrors(t *testing.T) {
	for _, tc := range []struct{ name, registrations, want string }{
		{"ambiguous providers", "p.Constructors(One,Two)", "ambiguous providers"},
		{"missing explicit target", "p.Bind[logging.Logger,*Custom]()", "missing provider"},
		{"ambiguous autobinding", "p.Module(p.Constructors(One,NewCustom),p.AutoBind())", "ambiguous implementations"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := fixture(t, map[string]string{
				"app.go": `package app
import "github.com/palma99/palma-framework/logging"
type Custom struct{logging.Logger}
func NewCustom()*Custom{return &Custom{}}
func One()*logging.SlogLogger{return logging.New(logging.Options{})}
func Two()*logging.SlogLogger{return logging.New(logging.Options{})}
`,
				"template.go": "//go:build pfw_inject\npackage app\nimport(p \"github.com/palma99/palma-framework\";\"github.com/palma99/palma-framework/logging\")\nfunc Initialize()(logging.Logger,error){return p.Build[logging.Logger](" + tc.registrations + ")}\n",
			})
			if _, err := Run(context.Background(), Config{Dir: dir}); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error: %v", err)
			}
		})
	}
}

func TestFrameworkDefaultsFollowEnvironmentBindings(t *testing.T) {
	dir := fixture(t, map[string]string{
		"app.go": `package app
import("github.com/palma99/palma-framework/logging")
type Custom struct{logging.Logger}
func NewCustom()*Custom{return &Custom{logging.New(logging.Options{})}}
`,
		"template.go": `//go:build pfw_inject
package app
import(p "github.com/palma99/palma-framework";"github.com/palma99/palma-framework/logging")
func Initialize(env p.Environment)(logging.Logger,error){return p.Build[logging.Logger](p.Environments("dev","uat"),p.ForEnv("uat",p.Constructors(NewCustom),p.Bind[logging.Logger,*Custom]()))}
`,
	})
	for _, env := range []string{"dev", "uat"} {
		if _, err := Run(context.Background(), Config{Dir: dir, Environment: env}); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(dir, "pfw_gen.go"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), ".NewDefault()") != (env == "dev") {
			t.Fatalf("environment %s: %s", env, data)
		}
		compileAndRun(t, dir)
	}
}

func TestHTTPFrameworkProviderCanBeReplaced(t *testing.T) {
	for _, custom := range []bool{false, true} {
		dir := fixture(t, map[string]string{
			"app.go": `package app
import("net/http";"github.com/palma99/palma-framework/logging";"github.com/palma99/palma-framework/transport/httpserver")
var CustomCalls int
func CustomServer(server *http.Server,logger logging.Logger)*httpserver.Server{CustomCalls++;return httpserver.NewWithLogger(server,logger)}
`,
		})
		registrations := ""
		if custom {
			registrations = "p.Constructors(CustomServer)"
		}
		template := "//go:build pfw_inject\npackage app\nimport(\"net/http\";p \"github.com/palma99/palma-framework\";\"github.com/palma99/palma-framework/transport/httpserver\")\nfunc Initialize(server *http.Server)(*httpserver.Server,error){return p.Build[*httpserver.Server](" + registrations + ")}\n"
		if err := os.WriteFile(filepath.Join(dir, "template.go"), []byte(template), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := Run(context.Background(), Config{Dir: dir}); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(dir, "pfw_gen.go"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "httpserver.NewWithLogger(") == custom {
			t.Fatalf("HTTP fallback: %s", data)
		}
		want := "0"
		if custom {
			want = "1"
		}
		runtime := `package app
import("testing";"net/http")
func TestHTTPProvider(t *testing.T){httpServer:=&http.Server{};server,err:=Initialize(httpServer);if err!=nil||server.HTTPServer()!=httpServer||server.Logger()==nil||CustomCalls!=` + want + `{t.Fatalf("server: %v calls=%d",err,CustomCalls)}}
`
		if err := os.WriteFile(filepath.Join(dir, "app_test.go"), []byte(runtime), 0644); err != nil {
			t.Fatal(err)
		}
		compileAndRun(t, dir)
	}
}
