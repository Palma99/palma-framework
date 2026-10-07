package di

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"reflect"
	"strings"
	"testing"
)

func testTypes(t *testing.T) map[string]types.Type {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "services.go", `package services
type Config struct{}
type DB struct{}
type Repository interface { Find() string }
type SQLRepository struct{}
func (*SQLRepository) Find() string { return "" }
type Service struct{}
type App struct{}
type Alias = Config
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := new(types.Config).Check("example/services", fset, []*ast.File{file}, nil)
	if err != nil {
		t.Fatal(err)
	}
	result := make(map[string]types.Type)
	for _, name := range pkg.Scope().Names() {
		result[name] = pkg.Scope().Lookup(name).Type()
		result["*"+name] = types.NewPointer(result[name])
	}
	return result
}

func TestResolveSharedDependenciesAndInterfaceBinding(t *testing.T) {
	typesByName := testTypes(t)
	g := Graph{
		Providers: []Provider{
			{Name: "NewApp", Output: typesByName["*App"], Inputs: []types.Type{typesByName["*Service"], typesByName["*DB"]}},
			{Name: "NewService", Output: typesByName["*Service"], Inputs: []types.Type{typesByName["Repository"]}},
			{Name: "NewSQLRepository", Output: typesByName["*SQLRepository"], Inputs: []types.Type{typesByName["*DB"]}},
			{Name: "NewDB", Output: typesByName["*DB"], Inputs: []types.Type{typesByName["Config"]}},
			{Name: "LoadConfig", Output: typesByName["Config"]},
		},
		Bindings: []Binding{{Interface: typesByName["Repository"], Concrete: typesByName["*SQLRepository"]}},
	}
	for range 2 {
		plan, err := g.Resolve(typesByName["*App"], typesByName["*Service"])
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, p := range plan.Providers {
			names = append(names, p.Name)
		}
		want := []string{"LoadConfig", "NewDB", "NewSQLRepository", "NewService", "NewApp"}
		if !reflect.DeepEqual(names, want) {
			t.Fatalf("construction order = %v, want %v", names, want)
		}
	}
}

func TestResolveRejectsInvalidGraphs(t *testing.T) {
	ts := testTypes(t)
	tests := []struct {
		name string
		g    Graph
		root types.Type
		want string
	}{
		{"missing", Graph{Providers: []Provider{{Name: "NewApp", Output: ts["App"], Inputs: []types.Type{ts["Config"]}}}}, ts["App"], "NewApp -> example/services.Config: missing provider"},
		{"cycle", Graph{Providers: []Provider{{Name: "NewApp", Output: ts["App"], Inputs: []types.Type{ts["Config"]}}, {Name: "LoadConfig", Output: ts["Config"], Inputs: []types.Type{ts["App"]}}}}, ts["App"], "dependency cycle: NewApp -> LoadConfig -> NewApp"},
		{"duplicate", Graph{Providers: []Provider{{Name: "A", Output: ts["Config"]}, {Name: "B", Output: ts["Config"]}}}, ts["Config"], "ambiguous providers"},
		{"alias duplicate", Graph{Providers: []Provider{{Name: "A", Output: ts["Config"]}, {Name: "B", Output: ts["Alias"]}}}, ts["Config"], "ambiguous providers"},
		{"wrong implementation", Graph{Bindings: []Binding{{Interface: ts["Repository"], Concrete: ts["SQLRepository"]}}}, ts["Repository"], "does not implement"},
		{"non interface binding", Graph{Bindings: []Binding{{Interface: ts["Config"], Concrete: ts["Config"]}}}, ts["Config"], "must be a value interface"},
		{"interface as concrete", Graph{Bindings: []Binding{{Interface: ts["Repository"], Concrete: ts["Repository"]}}}, ts["Repository"], "must be concrete"},
		{"duplicate binding", Graph{Bindings: []Binding{{Interface: ts["Repository"], Concrete: ts["*SQLRepository"]}, {Interface: ts["Repository"], Concrete: ts["*SQLRepository"]}}}, ts["Repository"], "duplicate binding"},
		{"binding must use its concrete provider", Graph{Providers: []Provider{{Name: "NewRepo", Output: ts["Repository"]}}, Bindings: []Binding{{Interface: ts["Repository"], Concrete: ts["*SQLRepository"]}}}, ts["Repository"], "missing provider"},
		{"bound missing provider", Graph{Bindings: []Binding{{Interface: ts["Repository"], Concrete: ts["*SQLRepository"]}}}, ts["Repository"], "missing provider for *example/services.SQLRepository"},
		{"nil root", Graph{}, nil, "root type must not be nil"},
		{"nil provider output", Graph{Providers: []Provider{{Name: "Invalid"}}}, ts["Config"], "must have a name and output type"},
		{"nil provider input", Graph{Providers: []Provider{{Name: "Invalid", Output: ts["App"], Inputs: []types.Type{nil}}}}, ts["App"], "nil input type"},
		{"nil binding", Graph{Bindings: []Binding{{}}}, ts["App"], "must have interface and concrete types"},
		{"pointer mismatch", Graph{Providers: []Provider{{Name: "NewDB", Output: ts["DB"]}}}, ts["*DB"], "missing provider"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := tt.g.Resolve(tt.root)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Resolve error = %v, want %q", err, tt.want)
			}
			if len(plan.Providers) != 0 {
				t.Fatal("failed resolution returned a partial plan")
			}
		})
	}
}

func TestResolveAutomaticallyBindsUniqueImplementation(t *testing.T) {
	ts := testTypes(t)
	g := Graph{Providers: []Provider{{Name: "NewSQLRepository", Output: ts["*SQLRepository"]}}}
	if _, err := g.Resolve(ts["Repository"]); err == nil {
		t.Fatal("autobinding must be disabled by default")
	}
	g.RootAutoBind = []string{"NewSQLRepository"}
	plan, err := g.Resolve(ts["Repository"])
	if err != nil || len(plan.Providers) != 1 || len(plan.Selections) != 1 || !types.Identical(plan.Selections[0].Concrete, ts["*SQLRepository"]) {
		t.Fatalf("automatic binding = %+v, %v", plan, err)
	}
}

func TestAutomaticBindingAmbiguityAndManualPrecedence(t *testing.T) {
	ts := testTypes(t)
	// Two distinct concrete types with the same method set.
	sql := ts["*SQLRepository"]
	other := types.NewNamed(types.NewTypeName(token.NoPos, types.NewPackage("other", "other"), "Repository", nil), types.NewStruct(nil, nil), nil)
	method := sql.(*types.Pointer).Elem().(*types.Named).Method(0)
	sig := method.Type().(*types.Signature)
	other.AddMethod(types.NewFunc(token.NoPos, other.Obj().Pkg(), "Find", types.NewSignatureType(types.NewVar(token.NoPos, other.Obj().Pkg(), "", types.NewPointer(other)), nil, nil, sig.Params(), sig.Results(), false)))
	g := Graph{Providers: []Provider{{Name: "SQL", Output: sql}, {Name: "Other", Output: types.NewPointer(other)}}, RootAutoBind: []string{"SQL", "Other"}}
	if _, err := g.Resolve(ts["Repository"]); err == nil || !strings.Contains(err.Error(), "ambiguous implementations") || !strings.Contains(err.Error(), "SQL") || !strings.Contains(err.Error(), "Other") {
		t.Fatalf("ambiguity = %v", err)
	}
	g.Bindings = []Binding{{Interface: ts["Repository"], Concrete: sql}}
	plan, err := g.Resolve(ts["Repository"])
	if err != nil || len(plan.Providers) != 1 || plan.Providers[0].Name != "SQL" {
		t.Fatalf("manual precedence = %+v %v", plan, err)
	}
}

func TestAutomaticBindingInputPointerAndCycle(t *testing.T) {
	ts := testTypes(t)
	g := Graph{Inputs: []Input{{Name: "repo", Type: ts["*SQLRepository"]}}, Bindings: []Binding{{Interface: ts["Repository"], Concrete: ts["*SQLRepository"]}}}
	if plan, err := g.Resolve(ts["Repository"]); err != nil || len(plan.Providers) != 0 || len(plan.Selections) != 1 {
		t.Fatalf("input binding = %+v %v", plan, err)
	}
	g = Graph{Providers: []Provider{{Name: "Value", Output: ts["SQLRepository"]}}}
	if _, err := g.Resolve(ts["Repository"]); err == nil || !strings.Contains(err.Error(), "missing provider") {
		t.Fatalf("pointer method set = %v", err)
	}
	g = Graph{Providers: []Provider{{Name: "Cycle", Output: ts["*SQLRepository"], Inputs: []types.Type{ts["Repository"]}, AutoBind: []string{"Cycle"}}}, RootAutoBind: []string{"Cycle"}}
	if _, err := g.Resolve(ts["Repository"]); err == nil || !strings.Contains(err.Error(), "dependency cycle") {
		t.Fatalf("automatic cycle = %v", err)
	}
}

func TestResolveOmitsUnreachableProviders(t *testing.T) {
	ts := testTypes(t)
	g := Graph{Providers: []Provider{
		{Name: "LoadConfig", Output: ts["Config"]},
		{Name: "UnusedApp", Output: ts["App"], Inputs: []types.Type{ts["*DB"]}},
	}}
	plan, err := g.Resolve(ts["Alias"])
	if err != nil || len(plan.Providers) != 1 || plan.Providers[0].Name != "LoadConfig" {
		t.Fatalf("alias resolution = %+v, %v", plan, err)
	}
}

func TestResolveSeparatesTypesFromDifferentPackages(t *testing.T) {
	a := types.NewNamed(types.NewTypeName(token.NoPos, types.NewPackage("a", "same"), "Config", nil), types.NewStruct(nil, nil), nil)
	b := types.NewNamed(types.NewTypeName(token.NoPos, types.NewPackage("b", "same"), "Config", nil), types.NewStruct(nil, nil), nil)
	g := Graph{Providers: []Provider{{Name: "A", Output: a}, {Name: "B", Output: b}}}
	plan, err := g.Resolve(a, b)
	if err != nil || len(plan.Providers) != 2 {
		t.Fatalf("different package types should coexist: %+v, %v", plan, err)
	}
}

func TestResolveExternalInputs(t *testing.T) {
	ts := testTypes(t)
	g := Graph{
		Inputs:    []Input{{Name: "repository", Type: ts["*SQLRepository"]}},
		Bindings:  []Binding{{Interface: ts["Repository"], Concrete: ts["*SQLRepository"]}},
		Providers: []Provider{{Name: "NewService", Output: ts["*Service"], Inputs: []types.Type{ts["Repository"]}}},
	}
	plan, err := g.Resolve(ts["*Service"], ts["Repository"])
	if err != nil || len(plan.Providers) != 1 || plan.Providers[0].Name != "NewService" {
		t.Fatalf("external input resolution = %+v, %v", plan, err)
	}
	g.Inputs = append(g.Inputs, Input{Name: "interface", Type: ts["Repository"]})
	if plan, err := g.Resolve(ts["Repository"]); err != nil || len(plan.Selections) != 1 || !types.Identical(plan.Selections[0].Concrete, ts["*SQLRepository"]) {
		t.Fatalf("explicit binding must override direct interface input: %+v %v", plan, err)
	}
}
