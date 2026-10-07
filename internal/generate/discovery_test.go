package generate

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoveryMixedRegistrationAndExclusion(t *testing.T) {
	dir := fixture(t, map[string]string{
		"logic/logic.go": `package logic
type Config struct { Value int }
type Repository interface { Value() int }
type Client struct { N int }
func (c *Client) Value() int { return c.N }
var Closed int
// Open acquires a resource.
//pfw:coconut
func Open(cfg Config) (*Client, func() error, error) {
    return &Client{N: cfg.Value}, func() error { Closed++; return nil }, nil
}
// Not annotated: discovery must not register this competing constructor.
func Other(cfg Config) *Client { return &Client{N: cfg.Value + 1} }
`,
		"bootstrap/app.go": `package app
import l "example.test/app/logic"
type App struct { Repo l.Repository }
// pfw:coconut
func Assemble(repo l.Repository) *App { return &App{Repo: repo} }
func Replace(cfg l.Config) *l.Client { return &l.Client{N: 99} }
`,
		"bootstrap/template.go": `//go:build pfw_inject
package app
import (
    p "github.com/palma99/palma-framework"
    l "example.test/app/logic"
)
const scope = "../logic"
var Services = p.Module(p.Discover(scope, ".", "example.test/app/logic"), p.AutoBind())
func Initialize(cfg l.Config) (*App, func() error, error) {
    return p.BuildWithCleanup[*App](Services, p.Constructors(l.Open))
}
func Override(cfg l.Config) (*App, error) {
    return p.Build[*App](p.Exclude(l.Open), Services, p.Constructors(Replace), p.Implementation[l.Repository, *l.Client]())
}
`,
		"bootstrap/app_test.go": `package app
import (
    "testing"
    l "example.test/app/logic"
)
func TestDiscoveredGraph(t *testing.T) {
    app, cleanup, err := Initialize(l.Config{Value: 42})
    if err != nil || app.Repo.Value() != 42 || cleanup == nil { t.Fatalf("init: %v", err) }
    if err := cleanup(); err != nil || l.Closed != 1 { t.Fatalf("cleanup: %v calls=%d", err, l.Closed) }
    override, err := Override(l.Config{})
    if err != nil || override.Repo.Value() != 99 || l.Closed != 1 { t.Fatalf("override: %+v %v", override, err) }
}
`,
		"logic/ignored_test.go": `package logic
//pfw:coconut
func OnlyInTests() *Client { return &Client{} }
`,
		"logic/excluded.go": `//go:build pfw_never_enabled
package logic
//pfw:coconut
func Unavailable() *Client { return &Client{} }
`,
	})
	cfg := Config{Dir: dir, Patterns: []string{"./bootstrap"}}
	paths, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	compileAndRun(t, dir)
	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(paths[0])
	if !bytes.Equal(first, second) {
		t.Fatal("discovery was not deterministic")
	}
	cfg.Check = true
	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoveryDiagnostics(t *testing.T) {
	for _, tc := range []struct{ name, provider, registration, want string }{
		{"ambiguous", "//pfw:coconut\nfunc Other() int { return 2 }", `p.Discover(".")`, "ambiguous providers"},
		{"method", "//pfw:coconut\nfunc (Value) Method() int { return 2 }", `p.Discover(".")`, "must have signature"},
		{"generic", "//pfw:coconut\nfunc Generic[T any]() int { return 2 }", `p.Discover(".")`, "must have signature"},
		{"dynamic pattern", `var scope = "."`, `p.Discover(scope)`, "constant strings"},
		{"empty pattern", "", `p.Discover("")`, "must not be empty"},
		{"no patterns", "", `p.Discover()`, "requires constant"},
		{"no package", "", `p.Discover("./missing/...")`, "matched no packages"},
		{"excluded provider", "", `p.Discover("."), p.Exclude(Source)`, "missing provider"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := fixture(t, map[string]string{
				"missing/README.md": "No Go packages here.",
				"app.go":            "package app\ntype Value struct{}\n//pfw:coconut\nfunc Source() int { return 1 }\n" + tc.provider,
				"template.go":       "//go:build pfw_inject\npackage app\nimport p \"github.com/palma99/palma-framework\"\nfunc Initialize() (int, error) { return p.Build[int](" + tc.registration + ") }\n",
			})
			if _, err := Run(context.Background(), Config{Dir: dir}); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v want %q", err, tc.want)
			}
			if _, err := os.Stat(filepath.Join(dir, "pfw_gen.go")); !os.IsNotExist(err) {
				t.Fatal("invalid discovery wrote output")
			}
		})
	}
}

func TestDiscoveryRejectsPrivateExternalConstructor(t *testing.T) {
	dir := fixture(t, map[string]string{
		"logic/logic.go": "package logic\n//pfw:coconut\nfunc private() int { return 1 }\n",
		"template.go":    "//go:build pfw_inject\npackage app\nimport p \"github.com/palma99/palma-framework\"\nfunc Initialize() (int, error) { return p.Build[int](p.Discover(\"./logic\")) }\n",
	})
	if _, err := Run(context.Background(), Config{Dir: dir}); err == nil || !strings.Contains(err.Error(), "not exported") {
		t.Fatalf("private constructor = %v", err)
	}
}
