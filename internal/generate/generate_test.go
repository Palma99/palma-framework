package generate

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func fixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	files["go.mod"] = "module example.test/app\n\ngo 1.26.0\n\nrequire github.com/palma99/palma-framework v0.0.0\nreplace github.com/palma99/palma-framework => " + strconv.Quote(root) + "\n"
	for path, content := range files {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func compileAndRun(t *testing.T, dir string, flags ...string) {
	t.Helper()
	args := append([]string{"test"}, flags...)
	cmd := exec.Command("go", append(args, "./...")...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated application failed: %v\n%s", err, output)
	}
}

func TestGenerateCompileAndExecute(t *testing.T) {
	dir := fixture(t, map[string]string{
		"logic/logic.go": `package logic
import "errors"
type Config struct { Fail bool }
type Port interface { Answer() int }
type Store struct{ value int }
func (*Store) Answer() int { return 42 }
var Cause = errors.New("constructor failure")
var Calls int
func Create(cfg Config) (*Store, error) {
    Calls++
    if cfg.Fail { return nil, Cause }
    return &Store{}, nil
}
`,
		"app.go": `package app
import l "example.test/app/logic"
type App struct { Port l.Port; Store *l.Store }
// Deliberately collide with typical generated names.
func input0(port l.Port, store *l.Store) (App, error) { return App{port, store}, nil }
func value0() bool { return true }
func err() bool { return true }
func zero() bool { return true }
func pfwdep0() bool { return true }
func logic() bool { return true }
func fmt() bool { return true }
`,
		"anything.go": `//go:build pfw_inject
package app
import (
    p "github.com/palma99/palma-framework"
    arbitrary "example.test/app/logic"
)
var Nested = p.Module(p.Constructors(arbitrary.Create))
var Data = p.Module(Nested, p.Bind[arbitrary.Port, *arbitrary.Store]())
func Start(cfg arbitrary.Config) (App, error) {
    return p.Build[App](Data, p.Constructors(input0))
}
func Identity(cfg arbitrary.Config) (arbitrary.Config, error) {
    return p.Build[arbitrary.Config]()
}
func Port(cfg arbitrary.Config) (arbitrary.Port, error) {
    return p.Build[arbitrary.Port](Data)
}
`,
		"app_test.go": `package app
import (
    "errors"
    "testing"
    l "example.test/app/logic"
)
func TestWiring(t *testing.T) {
    l.Calls = 0
    a, e := Start(l.Config{})
    if e != nil || a.Port.Answer() != 42 || a.Port != a.Store || l.Calls != 1 { t.Fatalf("bad wiring: %+v, %v, calls=%d", a, e, l.Calls) }
    b, e := Start(l.Config{})
    if e != nil || b.Store == a.Store || l.Calls != 2 { t.Fatalf("bad lifetime: %+v %v", b, e) }
    failed, e := Start(l.Config{Fail: true})
    if !errors.Is(e, l.Cause) || failed != (App{}) { t.Fatalf("error propagation: %+v %v", failed, e) }
    cfg, e := Identity(l.Config{Fail: true})
    if e != nil || !cfg.Fail { t.Fatalf("bad input root: %+v %v", cfg, e) }
    port, e := Port(l.Config{})
    if e != nil || port.Answer() != 42 { t.Fatalf("bad interface root: %+v %v", port, e) }
}
`,
	})
	cfg := Config{Dir: dir, Output: "composition.go"}
	paths, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || filepath.Base(paths[0]) != "composition.go" {
		t.Fatalf("paths = %v", paths)
	}
	first, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(first), "github.com/palma99/palma-framework") || strings.Contains(string(first), "Build[") {
		t.Fatalf("generated wiring must not depend on markers:\n%s", first)
	}
	compileAndRun(t, dir)
	// Switching the public alias must leave generated calls byte-for-byte equal.
	templatePath := filepath.Join(dir, "anything.go")
	template, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(templatePath, bytes.ReplaceAll(template, []byte("p.Bind["), []byte("p.Implementation[")), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(paths[0])
	if !bytes.Equal(first, second) {
		t.Fatal("Bind and Implementation did not generate identical wiring")
	}
	cfg.Check = true
	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	stale := append(append([]byte(nil), first...), []byte("\n// stale\n")...)
	if err := os.WriteFile(paths[0], stale, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), cfg); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("check should detect stale output, got %v", err)
	}
	unchanged, _ := os.ReadFile(paths[0])
	if !bytes.Equal(stale, unchanged) {
		t.Fatal("check modified output")
	}
}

func TestGenerateDiagnostics(t *testing.T) {
	tests := []struct {
		name, source, want string
	}{
		{"missing provider", `func Start() (*App, error) { return pfw.Build[*App](pfw.Constructors(MakeApp)) }`, "missing provider"},
		{"cycle", `func Start() (*App, error) { return pfw.Build[*App](pfw.Constructors(MakeApp, Cycle)) }`, "dependency cycle"},
		{"duplicate", `func Start() (*App, error) { return pfw.Build[*App](pfw.Constructors(Source, OtherSource)) }`, "ambiguous providers"},
		{"duplicate input", `func Start(a Config, b Config) (Config, error) { return pfw.Build[Config]() }`, "ambiguous inputs"},
		{"input and provider", `func Start(cfg Config) (*App, error) { return pfw.Build[*App](pfw.Constructors(Source, MakeApp)) }`, "ambiguous input and provider"},
		{"closure", `func Start() (Config, error) { return pfw.Build[Config](pfw.Constructors(func() Config { return Config{} })) }`, "named, non-generic"},
		{"generic", `func Start() (Config, error) { return pfw.Build[Config](pfw.Constructors(Generic[int])) }`, "named, non-generic"},
		{"method", `func Start() (Config, error) { return pfw.Build[Config](pfw.Constructors(Config.Get)) }`, "must have signature"},
		{"variadic", `func Start() (Config, error) { return pfw.Build[Config](pfw.Constructors(Variadic)) }`, "must have signature"},
		{"multiple outputs", `func Start() (Config, error) { return pfw.Build[Config](pfw.Constructors(Multiple)) }`, "must have signature"},
		{"resource without ownership", `func Start() (*App, error) { return pfw.Build[*App](pfw.Constructors(Resource)) }`, "requires pfw.BuildWithCleanup"},
		{"invalid cleanup", `func Start() (*App, func() error, error) { return pfw.BuildWithCleanup[*App](pfw.Constructors(BadCleanup)) }`, "must have signature"},
		{"cleanup arguments", `func Start() (*App, func() error, error) { return pfw.BuildWithCleanup[*App](pfw.Constructors(CleanupWithArgs)) }`, "must have signature"},
		{"four results", `func Start() (*App, func() error, error) { return pfw.BuildWithCleanup[*App](pfw.Constructors(Four)) }`, "must have signature"},
		{"invalid binding", `func Start() (Port, error) { return pfw.Build[Port](pfw.Bind[Port, Config]()) }`, "does not implement"},
		{"invalid implementation", `func Start() (Port, error) { return pfw.Build[Port](pfw.Implementation[Port, Config]()) }`, "does not implement"},
		{"extra statements", `func Start() (Config, error) { Source(); return pfw.Build[Config](pfw.Constructors(Source)) }`, "single return"},
		{"dynamic registration", `func Start() (Config, error) { return pfw.Build[Config](Dynamic()) }`, "dynamic registrations"},
		{"mutable module", `var Set = pfw.Module(pfw.Constructors(Source)); func init() { Set = pfw.Module() }; func Start() (Config, error) { return pfw.Build[Config](Set) }`, "only be referenced in static"},
		{"template provider", `func Hidden() Config { return Config{} }; func Start() (Config, error) { return pfw.Build[Config](pfw.Constructors(Hidden)) }`, "available without"},
		{"wrong root", `func Start() (any, error) { return pfw.Build[Config](pfw.Constructors(Source)) }`, "root result exactly"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := fixture(t, map[string]string{
				"app.go": `package app
type Config struct{}
type App struct{}
type Port interface { Act() }
func Source() Config { return Config{} }
func OtherSource() Config { return Config{} }
func MakeApp(Config) *App { return &App{} }
func Cycle(*App) Config { return Config{} }
func Generic[T any]() Config { return Config{} }
func (Config) Get() Config { return Config{} }
func Variadic(...int) Config { return Config{} }
func Multiple() (Config, bool) { return Config{}, true }
func Resource() (*App, func() error, error) { return &App{}, nil, nil }
func BadCleanup() (*App, func(), error) { return &App{}, nil, nil }
func CleanupWithArgs() (*App, func(int) error, error) { return &App{}, nil, nil }
func Four() (*App, func() error, bool, error) { return &App{}, nil, false, nil }
`,
				"declarations.go": "//go:build pfw_inject\npackage app\nimport \"github.com/palma99/palma-framework\"\nfunc Dynamic() pfw.Registration { return pfw.Module() }\n" + tt.source,
			})
			if _, err := Run(context.Background(), Config{Dir: dir}); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
			if _, err := os.Stat(filepath.Join(dir, "pfw_gen.go")); !os.IsNotExist(err) {
				t.Fatal("invalid graph produced an output file")
			}
		})
	}
}

func TestProtectUserFilesAndMissingCheck(t *testing.T) {
	dir := fixture(t, map[string]string{
		"app.go":    "package app\nfunc Source() int { return 1 }\n",
		"inject.go": "//go:build pfw_inject\npackage app\nimport \"github.com/palma99/palma-framework\"\nfunc Start() (int, error) { return pfw.Build[int](pfw.Constructors(Source)) }\n",
	})
	if _, err := Run(context.Background(), Config{Dir: dir, Check: true}); err == nil || !strings.Contains(err.Error(), "missing or stale") {
		t.Fatalf("missing output check = %v", err)
	}
	path := filepath.Join(dir, "pfw_gen.go")
	userContent := []byte("package app\n// written by user\n")
	if err := os.WriteFile(path, userContent, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), Config{Dir: dir}); err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("overwrite check = %v", err)
	}
	data, _ := os.ReadFile(path)
	if !bytes.Equal(data, userContent) {
		t.Fatal("user file was modified")
	}
}

func TestRejectUntaggedInitializer(t *testing.T) {
	dir := fixture(t, map[string]string{
		"app.go": "package app\nimport \"github.com/palma99/palma-framework\"\nfunc Start(x int) (int, error) { return pfw.Build[int]() }\n",
	})
	if _, err := Run(context.Background(), Config{Dir: dir}); err == nil || !strings.Contains(err.Error(), "guarded only") {
		t.Fatalf("untagged initializer = %v", err)
	}
}
