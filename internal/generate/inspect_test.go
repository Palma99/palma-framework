package generate

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestInspectProvenanceScopesAndOrdersWithoutWrites(t *testing.T) {
	dir := fixture(t, map[string]string{
		"app.go": `package app
type Config struct{}
type Port interface{ Value() int }
type A struct{}
func (*A) Value() int { return 1 }
type Service struct{}
//pfw:coconut
func MakeA(Config) (*A,func() error,error) { panic("constructors must not be executed") }
//pfw:coconut
func MakeService(Port) *Service { panic("constructors must not be executed") }
//pfw:coconut
func Unwanted() bool { panic("excluded") }
func Unused() string { panic("unused") }
func Direct(Config) Port { panic("direct") }
`,
		"template.go": `//go:build pfw_inject
package app
import p "github.com/palma99/palma-framework"
var Services=p.Module(p.Discover("."),p.Constructors(MakeA,Unused),p.Exclude(Unwanted),p.AutoBind())
func Initialize(cfg Config) (*Service,func() error,error) { return p.BuildWithCleanup[*Service](Services) }
func Manual(cfg Config) (*Service,func() error,error) { return p.BuildWithCleanup[*Service](Services,p.Bind[Port,*A]()) }
func DirectRoot(cfg Config) (Port,error) { return p.Build[Port](p.Constructors(Direct)) }
`,
		"pfw_gen.go": "package app\n// A user file: inspection must not attempt to overwrite it.\n",
	})
	path := filepath.Join(dir, "pfw_gen.go")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{Dir: dir}
	report, err := Inspect(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if report.Version != 1 || len(report.Initializers) != 3 {
		t.Fatalf("report: %+v", report)
	}
	var automatic, manual, direct InitializerReport
	for _, init := range report.Initializers {
		switch init.Name {
		case "Initialize":
			automatic = init
		case "Manual":
			manual = init
		case "DirectRoot":
			direct = init
		}
	}
	if len(automatic.Inputs) != 1 || automatic.Inputs[0].Name != "cfg" {
		t.Fatalf("inputs: %+v", automatic.Inputs)
	}
	providers := make(map[string]ProviderReport)
	for _, provider := range automatic.Providers {
		providers[provider.Name] = provider
	}
	a := providers["example.test/app.MakeA"]
	if a.Status != "used" || !a.Cleanup || !reflect.DeepEqual(a.Origins, []string{"manual", "coconut"}) || !reflect.DeepEqual(a.AutoBindScopes, []string{"Services"}) {
		t.Fatalf("provider metadata: %+v", a)
	}
	if providers["example.test/app.Unused"].Status != "unused" || providers["example.test/app.Unwanted"].Status != "excluded" {
		t.Fatalf("provider states: %+v", providers)
	}
	if !reflect.DeepEqual(automatic.ConstructionOrder, []string{"example.test/app.MakeA", "example.test/app.MakeService"}) || !reflect.DeepEqual(automatic.CleanupOrder, []string{"example.test/app.MakeA"}) {
		t.Fatalf("orders: %+v", automatic)
	}
	if len(automatic.Bindings) != 1 || automatic.Bindings[0].Mode != "automatic" || automatic.Bindings[0].Provider != "example.test/app.MakeA" || !reflect.DeepEqual(automatic.Bindings[0].AutoBindScopes, []string{"Services"}) {
		t.Fatalf("automatic selection: %+v", automatic.Bindings)
	}
	if len(manual.Bindings) != 1 || manual.Bindings[0].Mode != "manual" {
		t.Fatalf("manual selection: %+v", manual.Bindings)
	}
	if len(direct.Bindings) != 1 || direct.Bindings[0].Mode != "direct" {
		t.Fatalf("direct selection: %+v", direct.Bindings)
	}
	var text bytes.Buffer
	if err := report.WriteText(&text); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"AutoBind=true", "[excluded]", "[unused]", "Construction order", "Cleanup order"} {
		if !strings.Contains(text.String(), expected) {
			t.Fatalf("missing %q in %s", expected, text.String())
		}
	}
	first, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Inspect(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(again)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("inspection was not deterministic")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("inspection modified the user file")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("inspection created generated output")
	}
}
