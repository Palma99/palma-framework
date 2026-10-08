package generate

import (
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/types"
	"strings"

	"github.com/palma99/palma-framework/internal/di"
	"golang.org/x/tools/go/packages"
)

const markerPath = "github.com/palma99/palma-framework"

type constructor struct {
	fn       *types.Func
	fallible bool
	cleanup  bool
}

type initializer struct {
	name         string
	root         types.Type
	graph        di.Graph
	constructors map[string]constructor
	plan         di.Plan
	withCleanup  bool
	cleanupType  types.Type
	manual       map[string]bool
	excluded     map[string]bool
	autoScopes   []map[string]bool
	registered   []di.Provider
	discovered   map[string]bool
	modules      []*registrationScope
	inputNames   []string
}

type frontend struct {
	pkg         *packages.Package
	modules     map[types.Object]ast.Expr
	active      map[types.Object]bool
	selected    map[types.Object]bool
	allowedUses map[*ast.Ident]bool
	discovery   *discovery
	scopes      []*registrationScope
	moduleName  string
}

type registrationScope struct {
	auto   bool
	names  map[string]bool
	name   string
	source string
}

func newFrontend(pkg *packages.Package) *frontend {
	f := frontend{pkg: pkg, modules: make(map[types.Object]ast.Expr), active: make(map[types.Object]bool), selected: make(map[types.Object]bool), allowedUses: make(map[*ast.Ident]bool)}
	// Collect declarations first, so modules need not precede their use.
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			if gen, ok := decl.(*ast.GenDecl); ok {
				for _, spec := range gen.Specs {
					if vs, ok := spec.(*ast.ValueSpec); ok && len(vs.Names) == 1 && len(vs.Values) == 1 {
						f.modules[pkg.TypesInfo.Defs[vs.Names[0]]] = vs.Values[0]
					}
				}
			}
		}
	}
	return &f
}

func readPackage(pkg *packages.Package, catalog *discovery) ([]initializer, error) {
	f := newFrontend(pkg)
	f.discovery = catalog
	var result []initializer
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			var builds []*ast.CallExpr
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					if name := f.marker(call.Fun); name == "Build" || name == "BuildWithCleanup" {
						builds = append(builds, call)
					}
				}
				return true
			})
			if len(builds) == 0 {
				continue
			}
			if !isTemplate(file) {
				return nil, f.errorAt(fn, "initializer must be in a file guarded only by //go:build pfw_inject")
			}
			if len(builds) != 1 || len(fn.Body.List) != 1 {
				return nil, f.errorAt(fn, "initializer body must be a single return pfw.Build[T](...) or pfw.BuildWithCleanup[T](...) statement")
			}
			ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
			if !ok || len(ret.Results) != 1 || ret.Results[0] != builds[0] {
				return nil, f.errorAt(fn, "initializer body must return pfw.Build[T](...) or pfw.BuildWithCleanup[T](...) directly")
			}
			obj := pkg.TypesInfo.Defs[fn.Name].(*types.Func)
			sig := obj.Type().(*types.Signature)
			withCleanup := f.marker(builds[0].Fun) == "BuildWithCleanup"
			resultCount := 2
			if withCleanup {
				resultCount = 3
			}
			if sig.Recv() != nil || sig.TypeParams().Len() != 0 || sig.Variadic() || sig.Results().Len() != resultCount || !isError(sig.Results().At(resultCount-1).Type()) || (withCleanup && !isCleanup(sig.Results().At(1).Type())) {
				return nil, f.errorAt(fn, "initializer must be non-generic and return (T, error) for Build or (T, func() error, error) for BuildWithCleanup")
			}
			init := initializer{name: fn.Name.Name, root: sig.Results().At(0).Type(), constructors: make(map[string]constructor), withCleanup: withCleanup, manual: make(map[string]bool), excluded: make(map[string]bool), discovered: make(map[string]bool)}
			if withCleanup {
				init.cleanupType = sig.Results().At(1).Type()
			}
			buildType := pkg.TypesInfo.TypeOf(builds[0])
			resultTuple, ok := buildType.(*types.Tuple)
			if !ok || resultTuple.Len() != resultCount || !types.Identical(resultTuple.At(0).Type(), init.root) {
				return nil, f.errorAt(fn, "pfw.%s type argument must match the initializer's root result exactly", f.marker(builds[0].Fun))
			}
			for i := 0; i < sig.Params().Len(); i++ {
				init.graph.Inputs = append(init.graph.Inputs, di.Input{Name: fmt.Sprintf("input%d", i), Type: sig.Params().At(i).Type()})
				name := sig.Params().At(i).Name()
				if name == "" || name == "_" {
					name = fmt.Sprintf("input%d", i)
				}
				init.inputNames = append(init.inputNames, name)
			}
			for _, arg := range builds[0].Args {
				if err := f.registration(arg, &init); err != nil {
					return nil, err
				}
			}
			var included []di.Provider
			init.registered = append([]di.Provider(nil), init.graph.Providers...)
			for _, provider := range init.graph.Providers {
				if init.manual[provider.Name] || !init.excluded[provider.Name] {
					included = append(included, provider)
				}
			}
			init.graph.Providers = included
			for _, scope := range init.autoScopes {
				var names []string
				for _, p := range included {
					if scope[p.Name] {
						names = append(names, p.Name)
					}
				}
				init.graph.RootAutoBind = append(init.graph.RootAutoBind, names...)
				for n := range init.graph.Providers {
					if scope[init.graph.Providers[n].Name] {
						init.graph.Providers[n].AutoBind = append(init.graph.Providers[n].AutoBind, names...)
					}
				}
			}
			plan, err := init.graph.Resolve(init.root)
			if err != nil {
				return nil, f.errorAt(fn, "%v", err)
			}
			init.plan = plan
			for _, p := range plan.Providers {
				if init.constructors[p.Name].cleanup && !withCleanup {
					return nil, f.errorAt(fn, "resource %s requires pfw.BuildWithCleanup[T] and initializer results (T, func() error, error)", p.Name)
				}
			}
			result = append(result, init)
		}
	}
	for id, obj := range pkg.TypesInfo.Uses {
		if f.selected[obj] && !f.allowedUses[id] {
			return nil, f.errorAt(id, "module %s may only be referenced in static pfw registrations", obj.Name())
		}
	}
	return result, nil
}

func (f *frontend) registration(expr ast.Expr, init *initializer) error {
	expr = unparen(expr)
	if id, ok := expr.(*ast.Ident); ok {
		obj := f.pkg.TypesInfo.Uses[id]
		value, exists := f.modules[obj]
		if !exists {
			return f.errorAt(expr, "module must be a package-level static declaration in this package")
		}
		if f.active[obj] {
			return f.errorAt(expr, "module cycle involving %s", id.Name)
		}
		call, ok := unparen(value).(*ast.CallExpr)
		if !ok || f.marker(call.Fun) != "Module" {
			return f.errorAt(expr, "module %s must be initialized with pfw.Module(...) directly", id.Name)
		}
		if !f.templateNode(value) {
			return f.errorAt(value, "module declarations must be in a pfw_inject template")
		}
		f.selected[obj] = true
		f.allowedUses[id] = true
		f.active[obj] = true
		defer delete(f.active, obj)
		previous := f.moduleName
		f.moduleName = id.Name
		defer func() { f.moduleName = previous }()
		return f.registration(value, init)
	}
	call, ok := expr.(*ast.CallExpr)
	if !ok || call.Ellipsis.IsValid() {
		return f.errorAt(expr, "expected a static pfw registration; dynamic expressions and slices are unsupported")
	}
	switch f.marker(call.Fun) {
	case "Constructors":
		for _, arg := range call.Args {
			fn, ok := f.object(arg).(*types.Func)
			if !ok || fn.Pkg() == nil {
				return f.errorAt(arg, "provider must be a named, non-generic function")
			}
			if err := f.addConstructor(fn, arg, init, true); err != nil {
				return err
			}
		}
	case "Discover":
		return f.discovery.register(f, call, init)
	case "Exclude":
		for _, arg := range call.Args {
			fn, ok := f.object(arg).(*types.Func)
			if !ok || fn.Pkg() == nil || fn.Type().(*types.Signature).Recv() != nil {
				return f.errorAt(arg, "pfw.Exclude requires named constructor functions")
			}
			init.excluded[fn.Pkg().Path()+"."+fn.Name()] = true
		}
	case "Implementation", "Bind":
		idx, ok := unparen(call.Fun).(*ast.IndexListExpr)
		if !ok || len(idx.Indices) != 2 || len(call.Args) != 0 {
			return f.errorAt(call, "expected pfw.%s[Interface, Concrete]()", f.marker(call.Fun))
		}
		init.graph.Bindings = append(init.graph.Bindings, di.Binding{Interface: f.pkg.TypesInfo.TypeOf(idx.Indices[0]), Concrete: f.pkg.TypesInfo.TypeOf(idx.Indices[1])})
	case "Module":
		source := f.pkg.Fset.Position(call.Pos()).String()
		name := f.moduleName
		if name == "" {
			name = "module@" + source
		}
		f.moduleName = ""
		scope := &registrationScope{names: make(map[string]bool), name: name, source: source}
		f.scopes = append(f.scopes, scope)
		defer func() { f.scopes = f.scopes[:len(f.scopes)-1] }()
		for _, arg := range call.Args {
			if err := f.registration(arg, init); err != nil {
				return err
			}
		}
		if scope.auto {
			init.autoScopes = append(init.autoScopes, scope.names)
		}
		init.modules = append(init.modules, scope)
	case "AutoBind":
		if len(f.scopes) == 0 || len(call.Args) != 0 {
			return f.errorAt(call, "pfw.AutoBind() must be declared inside pfw.Module")
		}
		f.scopes[len(f.scopes)-1].auto = true
	default:
		return f.errorAt(expr, "expected pfw.Constructors, pfw.Discover, pfw.Exclude, pfw.Implementation, pfw.Bind or pfw.Module; external modules and dynamic registrations are unsupported")
	}
	return nil
}

func (f *frontend) addConstructor(fn *types.Func, node ast.Node, init *initializer, manual bool) error {
	if fn.Pkg() == f.pkg.Types && f.templateNode(argForObject(f.pkg, fn)) {
		return f.errorAt(node, "provider %s must be available without the pfw_inject tag", fn.Name())
	}
	if fn.Pkg() != f.pkg.Types && !fn.Exported() {
		return f.errorAt(node, "provider %s is not exported; export it or place the initializer in its package", fn.Name())
	}
	sig := fn.Type().(*types.Signature)
	count := sig.Results().Len()
	if sig.Recv() != nil || sig.TypeParams().Len() != 0 || sig.Variadic() || count < 1 || count > 3 || (count >= 2 && !isError(sig.Results().At(count-1).Type())) || (count == 3 && !isCleanup(sig.Results().At(1).Type())) {
		return f.errorAt(node, "provider %s must have signature func(...) T, func(...) (T, error) or func(...) (T, func() error, error)", fn.Name())
	}
	name := fn.Pkg().Path() + "." + fn.Name()
	for _, scope := range f.scopes {
		scope.names[name] = true
	}
	init.manual[name] = init.manual[name] || manual
	init.discovered[name] = init.discovered[name] || !manual
	if _, exists := init.constructors[name]; exists {
		return nil
	}
	p := di.Provider{Name: name, Output: sig.Results().At(0).Type()}
	for i := 0; i < sig.Params().Len(); i++ {
		p.Inputs = append(p.Inputs, sig.Params().At(i).Type())
	}
	init.graph.Providers = append(init.graph.Providers, p)
	init.constructors[name] = constructor{fn: fn, fallible: count >= 2, cleanup: count == 3}
	return nil
}

func (f *frontend) marker(expr ast.Expr) string {
	switch e := unparen(expr).(type) {
	case *ast.IndexExpr:
		return f.marker(e.X)
	case *ast.IndexListExpr:
		return f.marker(e.X)
	}
	fn, ok := f.object(expr).(*types.Func)
	if ok && fn.Pkg() != nil && fn.Pkg().Path() == markerPath {
		return fn.Name()
	}
	return ""
}

func (f *frontend) object(expr ast.Expr) types.Object {
	switch e := unparen(expr).(type) {
	case *ast.Ident:
		return f.pkg.TypesInfo.Uses[e]
	case *ast.SelectorExpr:
		return f.pkg.TypesInfo.Uses[e.Sel]
	}
	return nil
}

func (f *frontend) templateNode(n ast.Node) bool {
	if n == nil {
		return false
	}
	for _, file := range f.pkg.Syntax {
		if n.Pos() >= file.Pos() && n.Pos() <= file.End() {
			return isTemplate(file)
		}
	}
	return false
}

func argForObject(pkg *packages.Package, obj types.Object) ast.Node {
	for id, def := range pkg.TypesInfo.Defs {
		if def == obj {
			return id
		}
	}
	return nil
}

func isTemplate(file *ast.File) bool {
	for _, group := range file.Comments {
		if group.Pos() > file.Package {
			break
		}
		for _, comment := range group.List {
			if constraint.IsGoBuild(comment.Text) {
				expr, err := constraint.Parse(comment.Text)
				tag, ok := expr.(*constraint.TagExpr)
				return err == nil && ok && tag.Tag == "pfw_inject"
			}
		}
	}
	return false
}

func (f *frontend) errorAt(n ast.Node, format string, args ...any) error {
	return fmt.Errorf("%s: %s", f.pkg.Fset.Position(n.Pos()), fmt.Sprintf(format, args...))
}

func unparen(expr ast.Expr) ast.Expr {
	for {
		p, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = p.X
	}
}

func isError(t types.Type) bool { return types.Identical(t, types.Universe.Lookup("error").Type()) }

func isCleanup(t types.Type) bool {
	sig, ok := t.Underlying().(*types.Signature)
	return ok && !sig.Variadic() && sig.Params().Len() == 0 && sig.Results().Len() == 1 && isError(sig.Results().At(0).Type())
}

func packageErrors(pkgs []*packages.Package) error {
	var messages []string
	packages.Visit(pkgs, nil, func(pkg *packages.Package) {
		for _, err := range pkg.Errors {
			messages = append(messages, err.Error())
		}
	})
	if len(messages) != 0 {
		return fmt.Errorf("package loading failed:\n%s", strings.Join(messages, "\n"))
	}
	return nil
}
