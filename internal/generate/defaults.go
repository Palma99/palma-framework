package generate

import (
	"fmt"
	"go/ast"
	"go/types"

	"github.com/palma99/palma-framework/internal/di"
)

// Framework defaults are a closed catalog of ordinary exported constructors.
// They use the same go/types universe as application imports and discovery.
func (f *frontend) frameworkPackage(path string) *types.Package {
	visited := make(map[*types.Package]bool)
	var visit func(*types.Package) *types.Package
	visit = func(pkg *types.Package) *types.Package {
		if pkg == nil || visited[pkg] {
			return nil
		}
		visited[pkg] = true
		if pkg.Path() == path {
			return pkg
		}
		for _, imported := range pkg.Imports() {
			if found := visit(imported); found != nil {
				return found
			}
		}
		return nil
	}
	if pkg := visit(f.pkg.Types); pkg != nil {
		return pkg
	}
	for _, pkg := range f.discovery.pkgs {
		if found := visit(pkg.Types); found != nil {
			return found
		}
	}
	return nil
}

func (f *frontend) addFrameworkProviders(init *initializer, node ast.Node) error {
	init.framework = make(map[string]bool)
	register := func(pkg *types.Package, name string) error {
		fn, ok := pkg.Scope().Lookup(name).(*types.Func)
		if !ok {
			return fmt.Errorf("framework default %s.%s is unavailable; use matching framework and CLI versions", pkg.Path(), name)
		}
		key := pkg.Path() + "." + name
		if _, exists := init.constructors[key]; exists {
			return nil
		}
		if err := f.addConstructor(fn, node, init, true); err != nil {
			return err
		}
		init.manual[key], init.discovered[key] = false, false
		init.framework[key] = true
		init.graph.Providers[len(init.graph.Providers)-1].Fallback = true
		return nil
	}
	if pkg := f.frameworkPackage(markerPath + "/logging"); pkg != nil {
		if err := register(pkg, "NewDefault"); err != nil {
			return err
		}
		init.graph.FallbackBindings = append(init.graph.FallbackBindings, di.Binding{
			Interface: pkg.Scope().Lookup("Logger").Type(),
			Concrete:  types.NewPointer(pkg.Scope().Lookup("SlogLogger").Type()),
		})
	}
	if pkg := f.frameworkPackage(markerPath + "/transport/httpserver"); pkg != nil {
		name := "NewWithLogger"
		if init.environmentIndex >= 0 {
			name = "NewForEnvironment"
		}
		if err := register(pkg, name); err != nil {
			return err
		}
	}
	return nil
}
