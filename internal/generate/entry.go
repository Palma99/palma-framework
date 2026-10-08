package generate

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"

	"golang.org/x/tools/go/packages"
)

type Entry struct {
	Package, Root string
	Initializers  []string
}

// FindEntry locates initializer packages reachable from a single main package,
// within its module. It does not assume a bootstrap folder or template filename.
func FindEntry(ctx context.Context, dir, pattern string) (Entry, error) {
	pkgs, err := packages.Load(&packages.Config{Context: ctx, Dir: dir, Mode: packages.LoadFiles | packages.NeedImports | packages.NeedDeps | packages.NeedModule, BuildFlags: []string{"-tags=pfw_inject"}}, pattern)
	if err != nil {
		return Entry{}, err
	}
	if err := packageErrors(pkgs); err != nil {
		return Entry{}, err
	}
	if len(pkgs) != 1 || pkgs[0].Name != "main" || pkgs[0].Module == nil {
		return Entry{}, fmt.Errorf("run requires exactly one main package in a Go module")
	}
	entry := Entry{Package: pkgs[0].PkgPath, Root: pkgs[0].Module.Dir}
	var scanErr error
	packages.Visit(pkgs, func(pkg *packages.Package) bool {
		if scanErr != nil || pkg.Module == nil || pkg.Module.Dir != entry.Root {
			return false
		}
		for _, path := range pkg.CompiledGoFiles {
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				scanErr = err
				return false
			}
			aliases := make(map[string]bool)
			for _, spec := range file.Imports {
				p, _ := strconv.Unquote(spec.Path.Value)
				if p == markerPath {
					alias := "pfw"
					if spec.Name != nil {
						alias = spec.Name.Name
					}
					aliases[alias] = true
				}
			}
			found := false
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				fun := unparen(call.Fun)
				switch index := fun.(type) {
				case *ast.IndexExpr:
					fun = index.X
				case *ast.IndexListExpr:
					fun = index.X
				}
				if selector, ok := fun.(*ast.SelectorExpr); ok {
					if id, ok := selector.X.(*ast.Ident); ok && aliases[id.Name] && (selector.Sel.Name == "Build" || selector.Sel.Name == "BuildWithCleanup") {
						found = true
					}
				}
				if id, ok := fun.(*ast.Ident); ok && aliases["."] && (id.Name == "Build" || id.Name == "BuildWithCleanup") {
					found = true
				}
				return true
			})
			if found {
				entry.Initializers = append(entry.Initializers, pkg.PkgPath)
				break
			}
		}
		return true
	}, nil)
	if scanErr != nil {
		return Entry{}, scanErr
	}
	sort.Strings(entry.Initializers)
	return entry, nil
}
