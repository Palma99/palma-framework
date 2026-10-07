package generate

import (
	"context"
	"testing"
)

func TestGeneratedResourceCleanup(t *testing.T) {
	dir := fixture(t, map[string]string{
		"resources.go": `package app
import "errors"
var ErrOpenA = errors.New("open A")
var ErrOpenB = errors.New("open B")
var ErrApp = errors.New("app")
var ErrCloseA = errors.New("close A")
var ErrCloseB = errors.New("close B")
type State struct {
    FailAt string
    NilA, NilB, NoCloseErrors bool
    Events []string
    ACalls, BCalls int
}
type CloseFn func() error
type Port interface { State() *State }
type A struct { state *State }
func (a *A) State() *State { return a.state }
type B struct { a *A }
type App struct { b *B; label string }
func Scalar() int { return 42 }
func Label() string { return "test" }
func FailPlain() (string, error) { return "", ErrApp }
func OpenA(s *State) (*A, CloseFn, error) {
    s.ACalls++
    s.Events = append(s.Events, "openA")
    var close CloseFn = func() error {
        s.Events = append(s.Events, "closeA")
        if s.NoCloseErrors { return nil }
        return ErrCloseA
    }
    if s.NilA { close = nil }
    if s.FailAt == "A" { return nil, close, ErrOpenA }
    return &A{state: s}, close, nil
}
func OpenB(a *A, port Port) (*B, func() error, error) {
    if port != a { panic("dependency was constructed twice") }
    s := a.state
    s.BCalls++
    s.Events = append(s.Events, "openB")
    var close func() error = func() error {
        s.Events = append(s.Events, "closeB")
        if s.NoCloseErrors { return nil }
        return ErrCloseB
    }
    if s.NilB { close = nil }
    if s.FailAt == "B" { return nil, close, ErrOpenB }
    return &B{a: a}, close, nil
}
func Finish(b *B, label string) (App, error) {
    b.a.state.Events = append(b.a.state.Events, "app")
    if b.a.state.FailAt == "App" { return App{}, ErrApp }
    return App{b: b, label: label}, nil
}
`,
		"composition.go": `//go:build pfw_inject
package app
import p "github.com/palma99/palma-framework"
var Resources = p.Module(p.Constructors(OpenA, OpenB), p.Bind[Port, *A]())
func Initialize(s *State) (App, func() error, error) {
    return p.BuildWithCleanup[App](Resources, p.Constructors(Label, Finish))
}
func Bound(s *State) (Port, CloseFn, error) {
    return p.BuildWithCleanup[Port](Resources)
}
func Input(s *State) (*State, func() error, error) {
    return p.BuildWithCleanup[*State]()
}
func Plain() (int, func() error, error) {
    return p.BuildWithCleanup[int](p.Constructors(Scalar))
}
func FailingPlain() (string, func() error, error) {
    return p.BuildWithCleanup[string](p.Constructors(FailPlain))
}
func UnreachableResource() (int, error) {
    return p.Build[int](p.Constructors(Scalar, OpenA))
}
`,
		"app_test.go": `package app
import (
    "errors"
    "reflect"
    "strings"
    "sync"
    "testing"
)
func TestSuccess(t *testing.T) {
    s := &State{}
    app, close, err := Initialize(s)
    if err != nil || close == nil || app.b == nil || app.label != "test" { t.Fatalf("init: %+v %v", app, err) }
    if s.ACalls != 1 || s.BCalls != 1 { t.Fatalf("calls: %+v", s) }
    if !reflect.DeepEqual(s.Events, []string{"openA", "openB", "app"}) { t.Fatalf("early cleanup: %v", s.Events) }
    err = close()
    if !errors.Is(err, ErrCloseA) || !errors.Is(err, ErrCloseB) { t.Fatalf("lost cleanup errors: %v", err) }
    if !strings.Contains(err.Error(), "cleanup example.test/app.OpenA") { t.Fatalf("missing context: %v", err) }
    if !reflect.DeepEqual(s.Events, []string{"openA", "openB", "app", "closeB", "closeA"}) { t.Fatalf("order: %v", s.Events) }
    cached := close()
    if cached != err || len(s.Events) != 5 { t.Fatalf("cleanup ran again: %v %v", s.Events, cached) }
}
func TestConcurrentCleanup(t *testing.T) {
    s := &State{}
    _, close, err := Initialize(s)
    if err != nil { t.Fatal(err) }
    var wg sync.WaitGroup
    results := make(chan error, 8)
    for range 8 { wg.Add(1); go func() { defer wg.Done(); results <- close() }() }
    wg.Wait()
    closeChan(results)
    for err := range results {
        if !errors.Is(err, ErrCloseA) || !errors.Is(err, ErrCloseB) { t.Fatal(err) }
    }
    if len(s.Events) != 5 { t.Fatalf("duplicate cleanup: %v", s.Events) }
}
// Separate helper avoids shadowing the predeclared close with the cleanup value.
func closeChan(c chan error) { close(c) }
func TestRollback(t *testing.T) {
    cases := []struct { stage string; cause error; events []string; a, b bool }{
        {"A", ErrOpenA, []string{"openA", "closeA"}, true, false},
        {"B", ErrOpenB, []string{"openA", "openB", "closeB", "closeA"}, true, true},
        {"App", ErrApp, []string{"openA", "openB", "app", "closeB", "closeA"}, true, true},
    }
    for _, tc := range cases { t.Run(tc.stage, func(t *testing.T) {
        s := &State{FailAt: tc.stage}
        app, close, err := Initialize(s)
        if app != (App{}) || close != nil || !errors.Is(err, tc.cause) { t.Fatalf("rollback: %+v %v %v", app, close == nil, err) }
        if errors.Is(err, ErrCloseA) != tc.a || errors.Is(err, ErrCloseB) != tc.b { t.Fatalf("lost errors: %v", err) }
        if !reflect.DeepEqual(s.Events, tc.events) { t.Fatalf("events: %v", s.Events) }
    }) }
}
func TestNilCallbacksAndSuccessfulCleanup(t *testing.T) {
    for _, state := range []*State{
        {NilA: true, NilB: true},
        {NilA: true, NoCloseErrors: true},
        {NilB: true, NoCloseErrors: true},
        {NoCloseErrors: true},
    } {
        _, close, err := Initialize(state)
        if err != nil || close == nil { t.Fatalf("init: %v", err) }
        if err := close(); err != nil { t.Fatal(err) }
        want := []string{"openA", "openB", "app"}
        if !state.NilB { want = append(want, "closeB") }
        if !state.NilA { want = append(want, "closeA") }
        if !reflect.DeepEqual(state.Events, want) { t.Fatalf("events: %v", state.Events) }
        if err := close(); err != nil || !reflect.DeepEqual(state.Events, want) { t.Fatalf("repeat cleanup: %v %v", state.Events, err) }
    }
    s := &State{FailAt: "B", NilB: true, NoCloseErrors: true}
    _, close, err := Initialize(s)
    if close != nil || !errors.Is(err, ErrOpenB) || !reflect.DeepEqual(s.Events, []string{"openA", "openB", "closeA"}) { t.Fatalf("nil rollback: %v %v", s.Events, err) }
}
func TestRootsAndExternalInputOwnership(t *testing.T) {
    s := &State{NoCloseErrors: true}
    port, close, err := Bound(s)
    if err != nil || port.State() != s { t.Fatalf("bound root: %v", err) }
    if err := close(); err != nil || !reflect.DeepEqual(s.Events, []string{"openA", "closeA"}) { t.Fatalf("bound cleanup: %v %v", s.Events, err) }
    events := len(s.Events)
    input, noop, err := Input(s)
    if input != s || err != nil || noop == nil || noop() != nil || len(s.Events) != events { t.Fatalf("input ownership: %v", err) }
    value, noop, err := Plain()
    if value != 42 || err != nil || noop == nil || noop() != nil { t.Fatalf("plain: %v", err) }
    failed, close, err := FailingPlain()
    if failed != "" || close != nil || !errors.Is(err, ErrApp) { t.Fatalf("failing plain: %v", err) }
    value, err = UnreachableResource()
    if value != 42 || err != nil { t.Fatalf("unreachable: %v", err) }
}
`,
	})
	if _, err := Run(context.Background(), Config{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	compileAndRun(t, dir, "-race")
}
