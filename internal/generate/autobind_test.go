package generate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutomaticBindingGeneratedAndManualSelection(t *testing.T) {
	files := map[string]string{
		"app.go": `package app
type Port interface { Value() int }
type A struct{ marker int }
func (*A) Value() int { return 1 }
type B struct{}
func (*B) Value() int { return 2 }
func MakeA() *A { return &A{} }
func MakeB() *B { return &B{} }
func Direct() Port { return &A{} }
func Consumer(port Port, a *A) int { if port != a { panic("duplicate instance") }; return port.Value() }
`,
		"template.go": `//go:build pfw_inject
package app
import p "github.com/palma99/palma-framework"
func Auto() (int, error) { return p.Build[int](p.Module(p.Constructors(MakeA,Consumer),p.AutoBind()),p.Module(p.Constructors(MakeB))) }
func Input(a *A) (Port, error) { return p.Build[Port](p.Bind[Port,*A]()) }
func Manual() (Port, error) { return p.Build[Port](p.Constructors(MakeA,MakeB),p.Bind[Port,*B]()) }
func InterfaceProvider() (Port, error) { return p.Build[Port](p.Constructors(Direct,MakeA,MakeB)) }
func ExplicitOverDirect() (Port, error) { return p.Build[Port](p.Constructors(Direct,MakeA,MakeB),p.Implementation[Port,*B]()) }
`,
		"app_test.go": `package app
import "testing"
func TestBindings(t *testing.T) {
    n,err:=Auto(); if n!=1 || err!=nil { t.Fatalf("auto %d %v",n,err) }
    a:=&A{}; input,err:=Input(a); if input!=a || err!=nil { t.Fatalf("input %v",err) }
    manual,err:=Manual(); if err!=nil || manual.Value()!=2 { t.Fatalf("manual %v",err) }
    direct,err:=InterfaceProvider(); if err!=nil || direct.Value()!=1 { t.Fatalf("direct %v",err) }
    selected,err:=ExplicitOverDirect(); if err!=nil || selected.Value()!=2 { t.Fatalf("explicit over direct %v",err) }
}



`,
	}
	dir := fixture(t, files)
	if _, err := Run(context.Background(), Config{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	compileAndRun(t, dir)
	files["template.go"] = "//go:build pfw_inject\npackage app\nimport p \"github.com/palma99/palma-framework\"\nfunc Auto() (Port,error) { return p.Build[Port](p.Module(p.Constructors(MakeA,MakeB),p.AutoBind())) }\n"
	delete(files, "app_test.go")
	dir = fixture(t, files)
	if _, err := Run(context.Background(), Config{Dir: dir}); err == nil || !strings.Contains(err.Error(), "ambiguous implementations") || !strings.Contains(err.Error(), "MakeA") || !strings.Contains(err.Error(), "MakeB") {
		t.Fatalf("ambiguity %v", err)
	}
}

func TestAutoBindModuleIsolationAndDefault(t *testing.T) {
	files := map[string]string{
		"app.go": `package app
type Port interface { Value() int }
type A struct { value int }
func (*A) Value() int { return 1 }
type B struct { value int }
func (*B) Value() int { return 2 }
func MakeA() *A { return &A{} }
func MakeB() *B { return &B{} }
type Left struct { Value int }
type Right struct { Value int }
type App struct { Left *Left; Right *Right }
func MakeLeft(port Port) *Left { return &Left{port.Value()} }
func MakeRight(port Port) *Right { return &Right{port.Value()} }
func Assemble(left *Left,right *Right) *App { return &App{left,right} }
`,
		"template.go": `//go:build pfw_inject
package app
import p "github.com/palma99/palma-framework"
var LeftModule = p.Module(p.Module(p.Constructors(MakeA)),p.Constructors(MakeLeft),p.AutoBind())
var RightModule = p.Module(p.AutoBind(),p.Constructors(MakeB,MakeRight))
func Initialize() (*App,error) { return p.Build[*App](LeftModule,RightModule,p.Constructors(Assemble)) }
`,
		"app_test.go": `package app
import "testing"
func TestDifferentBindings(t *testing.T) {
    app,err:=Initialize();if err!=nil || app.Left.Value!=1 || app.Right.Value!=2 { t.Fatalf("scoped bindings: %+v %v",app,err) }
}
`,
	}
	dir := fixture(t, files)
	if _, err := Run(context.Background(), Config{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	compileAndRun(t, dir)
	delete(files, "app_test.go")
	for _, tc := range []struct{ name, registration, want string }{
		{"default off", `p.Module(p.Constructors(MakeA,MakeLeft))`, "missing provider"},
		{"consumer outside", `p.Module(p.Constructors(MakeA),p.AutoBind()),p.Constructors(MakeLeft)`, "missing provider"},
		{"child does not enable parent", `p.Module(p.Module(p.Constructors(MakeA),p.AutoBind()),p.Constructors(MakeLeft))`, "missing provider"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files["template.go"] = "//go:build pfw_inject\npackage app\nimport p \"github.com/palma99/palma-framework\"\nfunc Initialize() (*Left,error) { return p.Build[*Left](" + tc.registration + ") }\n"
			dir := fixture(t, files)
			if _, err := Run(context.Background(), Config{Dir: dir}); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("scope error: %v", err)
			}
		})
	}
}

func TestAutoBindRootAndModuleInheritance(t *testing.T) {
	for _, tc := range []struct{ name, registrations, want string }{
		{"root enabled", `p.Constructors(MakeA,MakeLeft),p.AutoBind()`, ""},
		{"module inherits root", `p.Constructors(MakeA),p.Module(p.Constructors(MakeLeft)),p.AutoBind()`, ""},
		{"module overrides root off", `p.AutoBind(false),p.Module(p.Constructors(MakeA,MakeLeft),p.AutoBind())`, ""},
		{"child inherits parent", `p.AutoBind(false),p.Module(p.Constructors(MakeA),p.Module(p.Constructors(MakeLeft)),p.AutoBind())`, ""},
		{"child enables disabled parent", `p.AutoBind(),p.Module(p.AutoBind(false),p.Module(p.Constructors(MakeA,MakeLeft),p.AutoBind()))`, ""},
		{"manual binding wins over disabled inference", `p.AutoBind(),p.Module(p.Constructors(MakeA,MakeLeft),p.AutoBind(false)),p.Bind[Port,*A]()`, ""},
		{"explicit local scope isolates candidates", `p.AutoBind(),p.Module(p.Constructors(MakeA,MakeLeft),p.AutoBind()),p.Constructors(MakeB)`, ""},
		{"module disables root", `p.AutoBind(),p.Module(p.Constructors(MakeA,MakeLeft),p.AutoBind(false))`, "missing provider"},
		{"child inherits disabled parent", `p.AutoBind(),p.Module(p.Constructors(MakeA),p.Module(p.Constructors(MakeLeft)),p.AutoBind(false))`, "missing provider"},
		{"module inherits root off", `p.AutoBind(false),p.Constructors(MakeA),p.Module(p.Constructors(MakeLeft))`, "missing provider"},
		{"root registrations respect more local disable", `p.Constructors(MakeA,MakeLeft),p.AutoBind(),p.Module(p.Constructors(MakeLeft),p.AutoBind(false))`, "missing provider"},
		{"reversed registration order", `p.Module(p.Constructors(MakeLeft),p.AutoBind(false)),p.AutoBind(),p.Constructors(MakeA,MakeLeft)`, "missing provider"},
		{"inherited global candidates remain ambiguous", `p.AutoBind(),p.Constructors(MakeA,MakeB),p.Module(p.Constructors(MakeLeft))`, "ambiguous implementations"},
		{"nonconstant setting", `p.Constructors(MakeA,MakeLeft),p.AutoBind(enabled)`, "constant boolean"},
		{"too many settings", `p.AutoBind(true,false)`, "zero or one"},
		{"conflicting settings", `p.AutoBind(),p.AutoBind(false)`, "conflicting AutoBind"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := fixture(t, map[string]string{
				"app.go": `package app
type Port interface{Value()int}
type A struct{}
func(*A)Value()int{return 1}
type B struct{}
func(*B)Value()int{return 2}
type Left struct{Value int}
func MakeA()*A{return &A{}}
func MakeB()*B{return &B{}}
func MakeLeft(port Port)*Left{return &Left{port.Value()}}
var enabled=true
`,
				"template.go": "//go:build pfw_inject\npackage app\nimport p \"github.com/palma99/palma-framework\"\nfunc Initialize()(*Left,error){return p.Build[*Left](" + tc.registrations + ")}\n",
				"app_test.go": `package app
import "testing"
func TestValue(t *testing.T){value,err:=Initialize();if err!=nil||value.Value!=1{t.Fatalf("%v %v",value,err)}}
`,
			})
			_, err := Run(context.Background(), Config{Dir: dir})
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("error: %v, want %s", err, tc.want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			compileAndRun(t, dir)
		})
	}
}

func TestAutoBindEnvironmentSettingsAndInspection(t *testing.T) {
	dir := fixture(t, map[string]string{
		"app.go": `package app
type Port interface{Value()int}
type A struct{}
func(*A)Value()int{return 1}
type B struct{}
func(*B)Value()int{return 2}
type Left struct{Value int}
func MakeA()*A{return &A{}}
func MakeB()*B{return &B{}}
func MakeLeft(port Port)*Left{return &Left{port.Value()}}
`,
		"template.go": `//go:build pfw_inject
package app
import p "github.com/palma99/palma-framework"
func Initialize(env p.Environment)(*Left,error){return p.Build[*Left](
 p.Environments("dev","manual"),
 p.ForEnv("manual",p.AutoBind(false),p.Constructors(MakeB),p.Bind[Port,*B]()),
 p.Module(p.Constructors(MakeA,MakeLeft)),p.AutoBind(),
)}
`,
	})
	report, err := Inspect(context.Background(), Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	for _, init := range report.Initializers {
		if init.AutoBind != (init.Environment == "dev") {
			t.Fatalf("root policy: %+v", init)
		}
		for _, module := range init.Modules {
			if module.AutoBind != init.AutoBind {
				t.Fatalf("inherited module: %+v", module)
			}
		}
	}
	for _, env := range []string{"dev", "manual"} {
		if _, err := Run(context.Background(), Config{Dir: dir, Environment: env}); err != nil {
			t.Fatal(err)
		}
		want := "1"
		if env == "manual" {
			want = "2"
		}
		runtime := `package app
import "testing"
func TestValue(t *testing.T){value,err:=Initialize("` + env + `");if err!=nil||value.Value!=` + want + `{t.Fatalf("%v %v",value,err)}}
`
		if err := os.WriteFile(filepath.Join(dir, "app_test.go"), []byte(runtime), 0644); err != nil {
			t.Fatal(err)
		}
		compileAndRun(t, dir)
	}
}
