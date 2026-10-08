package generate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvironmentGraphsBindingsOverridesAndCleanup(t *testing.T) {
	dir := fixture(t, map[string]string{
		"app.go": `package app
type Config struct{}
type Port interface{Value()int}
type Live struct{n int}
func(*Live)Value()int{return 2}
type Memory struct{n int}
func(*Memory)Value()int{return 1}
var LiveCalls,MemoryCalls,Closed int
//pfw:coconut
func NewLive(Config)(*Live,func()error,error){LiveCalls++;return &Live{},func()error{Closed++;return nil},nil}
//pfw:coconut
func NewMemory(Config)*Memory{MemoryCalls++;return &Memory{}}
`,
		"template.go": `//go:build pfw_inject
package app
import p "github.com/palma99/palma-framework"
var All=p.Module(p.Environments(p.Local,p.Staging,p.Production),p.Discover("."),p.Implementation[Port,*Live](),p.ForEnv("local",p.Bind[Port,*Memory]()))
func Initialize(env p.Environment,cfg Config)(Port,func()error,error){return p.BuildWithCleanup[Port](All)}
var Automatic=p.Module(p.Environments(p.Local,p.Staging,p.Production),p.Constructors(NewLive),p.ForEnv("local",p.Override(p.Constructors(NewMemory))),p.AutoBind())
func Auto(env p.Environment,cfg Config)(Port,func()error,error){return p.BuildWithCleanup[Port](Automatic)}

`,
		"app_test.go": `package app
import("testing";p "github.com/palma99/palma-framework")
func TestRuntimeEnvironments(t *testing.T){
 for _,initialize:=range []func(p.Environment,Config)(Port,func()error,error){Initialize,Auto}{
  LiveCalls,MemoryCalls,Closed=0,0,0
  local,close,err:=initialize(p.Local,Config{});if err!=nil||local.Value()!=1||LiveCalls!=0||MemoryCalls!=1{t.Fatalf("local: %v",err)};if err:=close();err!=nil||Closed!=0{t.Fatalf("local cleanup: %v",err)}
  live,close,err:=initialize(p.Staging,Config{});if err==nil||live!=nil||close!=nil{t.Fatal("other declared environment accepted")}
  if LiveCalls!=0||MemoryCalls!=1{t.Fatal("mismatched environment called constructors")}
  unknown,close,err:=initialize("unknown",Config{});if err==nil||unknown!=nil||close!=nil{t.Fatal("unknown environment accepted")}
 }

}
`,
	})
	if _, err := Run(context.Background(), Config{Dir: dir, Environment: "local"}); err != nil {
		t.Fatal(err)
	}
	compileAndRun(t, dir)
	for _, env := range []string{"staging", "production"} {
		runtimeTest := `package app
import("testing";p "github.com/palma99/palma-framework")
func TestRuntimeEnvironments(t *testing.T){
 for _,initialize:=range []func(p.Environment,Config)(Port,func()error,error){Initialize,Auto}{
  LiveCalls,MemoryCalls,Closed=0,0,0
  live,close,err:=initialize("` + env + `",Config{});if err!=nil||live.Value()!=2||LiveCalls!=1||MemoryCalls!=0{t.Fatalf("live: %v",err)}
  if err:=close();err!=nil||Closed!=1{t.Fatalf("cleanup: %v",err)};close();if Closed!=1{t.Fatal("cleanup repeated")}
  value,close,err:=initialize(p.Local,Config{});if err==nil||value!=nil||close!=nil{t.Fatal("mismatched environment accepted")}
  if LiveCalls!=1||MemoryCalls!=0{t.Fatal("mismatched environment called constructors")}
 }
}`
		if err := os.WriteFile(filepath.Join(dir, "app_test.go"), []byte(runtimeTest), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Run(context.Background(), Config{Dir: dir, Environment: env}); err != nil {
			t.Fatal(err)
		}
		compileAndRun(t, dir)
	}
	report, err := Inspect(context.Background(), Config{Dir: dir, Environment: "local"})
	if err != nil {
		t.Fatal(err)
	}
	for _, init := range report.Initializers {
		if init.Environment != "local" {
			t.Fatalf("unfiltered graph: %s", init.Environment)
		}
	}
}

func TestEnvironmentDiagnostics(t *testing.T) {
	for _, tc := range []struct{ name, template, want string }{
		{"missing declaration", `func Init(env p.Environment)(int,error){return p.Build[int](p.Constructors(Source))}`, "requires an explicit Environments declaration"},
		{"ForEnv does not declare names", `func Init(env p.Environment)(int,error){return p.Build[int](p.ForEnv("dev",p.Constructors(Source)))}`, "requires an explicit Environments declaration"},
		{"undeclared name", `func Init(env p.Environment)(int,error){return p.Build[int](p.Environments("dev"),p.ForEnv("preview",p.Constructors(Source)))}`, `environment "preview" used by ForEnv is not declared`},
		{"empty declaration", `func Init(env p.Environment)(int,error){return p.Build[int](p.Environments(),p.Constructors(Source))}`, "constant names"},
		{"missing selector", `func Init()(int,error){return p.Build[int](p.ForEnv("local",p.Constructors(Source)))}`, "requires a pfw.Environment parameter"},
		{"bad name", `func Init(env p.Environment)(int,error){return p.Build[int](p.ForEnv("../secret",p.Constructors(Source)))}`, "invalid environment name"},
		{"invalid other environment", `func Init(env p.Environment)(int,error){return p.Build[int](p.Environments("local","live"),p.ForEnv("local",p.Constructors(Source)))}`, "missing provider"},
		{"conflicting binding", `func Init(env p.Environment)(any,error){return p.Build[any](p.Environments("local"),p.Constructors(Source,Text),p.ForEnv("local",p.Bind[any,int](),p.Bind[any,string]()))}`, "conflicting bindings"},
		{"ordinary conditional is additive", `func Init(env p.Environment)(any,error){return p.Build[any](p.Module(p.Environments("local"),p.Constructors(Source),p.ForEnv("local",p.Constructors(Text)),p.AutoBind()))}`, "ambiguous implementations"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := fixture(t, map[string]string{"app.go": "package app\nfunc Source()int{return 1}\nfunc Text()string{return \"text\"}\n", "template.go": "//go:build pfw_inject\npackage app\nimport p \"github.com/palma99/palma-framework\"\n" + tc.template})
			if _, err := Inspect(context.Background(), Config{Dir: dir}); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error: %v", err)
			}
		})
	}
}

func TestGenerationSelectsEnvironment(t *testing.T) {
	dir := fixture(t, map[string]string{
		"app.go": `package app
var Calls int
func Dev()int{Calls++;return 1}
func Live()int{Calls++;return 2}
`,
		"template.go": `//go:build pfw_inject
package app
import p "github.com/palma99/palma-framework"
const DevEnv p.Environment="dev"
var App=p.Module(p.Environments(DevEnv,"live","broken"),p.ForEnv(DevEnv,p.Constructors(Dev)),p.ForEnv("live",p.Constructors(Live)))
func Initialize(env p.Environment)(int,error){return p.Build[int](App)}
func Plain()(int,error){return p.Build[int](p.Constructors(Dev))}
`,
	})
	output := filepath.Join(dir, "pfw_gen.go")
	for _, tc := range []struct{ env, want string }{{"", "requires generate -env"}, {"unknown", "not declared"}, {"../dev", "invalid environment name"}, {"broken", "missing provider"}} {
		if _, err := Run(context.Background(), Config{Dir: dir, Environment: tc.env}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%q: %v", tc.env, err)
		}
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatal("failed generation wrote a file")
		}
	}
	for _, tc := range []struct{ env, ctor, want string }{{"dev", "Dev", "1"}, {"live", "Live", "2"}} {
		cfg := Config{Dir: dir, Environment: tc.env}
		if _, err := Run(context.Background(), cfg); err != nil {
			t.Fatal(err)
		}
		source, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		text := string(source)
		if strings.Count(text, "func Initialize(") != 1 || strings.Contains(text, "_pfwInitialize") || strings.Contains(text, "switch ") {
			t.Fatalf("unexpected dispatch: %s", text)
		}
		// Plain always needs Dev; Live must only appear in live generation.
		if strings.Contains(text, "Live()") != (tc.env == "live") {
			t.Fatalf("wrong graph: %s", text)
		}
		if !strings.Contains(text, tc.ctor+"()") {
			t.Fatalf("missing selected provider: %s", text)
		}
		runtimeTest := `package app
import("testing";p "github.com/palma99/palma-framework")
func TestSelected(t *testing.T){
 Calls=0
 value,err:=Initialize("` + tc.env + `");if err!=nil||value!=` + tc.want + `||Calls!=1{t.Fatalf("selected: %d %v",value,err)}
 for _,env:=range []p.Environment{"local","staging","production","unknown","broken"}{value,err:=Initialize(env);if err==nil||value!=0{t.Fatalf("accepted %s",env)}}
 other:=p.Environment("dev");if other=="` + tc.env + `"{other="live"};if _,err:=Initialize(other);err==nil{t.Fatal("other declared environment accepted")}
 if Calls!=1{t.Fatal("mismatch called providers")}
 if _,err:=Plain();err!=nil{t.Fatal(err)}
}`
		if err := os.WriteFile(filepath.Join(dir, "app_test.go"), []byte(runtimeTest), 0600); err != nil {
			t.Fatal(err)
		}
		compileAndRun(t, dir)
		cfg.Check = true
		if _, err := Run(context.Background(), cfg); err != nil {
			t.Fatal(err)
		}
		cfg.Environment = "dev"
		if tc.env == "dev" {
			cfg.Environment = "live"
		}
		if _, err := Run(context.Background(), cfg); err == nil || !strings.Contains(err.Error(), "stale") {
			t.Fatalf("check accepted different environment: %v", err)
		}
		unchanged, err := os.ReadFile(output)
		if err != nil || string(unchanged) != text {
			t.Fatal("check changed output")
		}
	}
	// Full inspection still catches the broken graph that selected generation skips.
	if _, err := Inspect(context.Background(), Config{Dir: dir}); err == nil || !strings.Contains(err.Error(), "missing provider") {
		t.Fatalf("inspection: %v", err)
	}
}
