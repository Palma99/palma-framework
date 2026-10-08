package generate

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

type discovery struct {
	matches map[string][]string
	pkgs    map[string]*packages.Package
}

func discoveryPatterns(pkgs []*packages.Package) ([]string, error) {
	found := make(map[string]bool)
	for _, pkg := range pkgs {
		f := newFrontend(pkg)
		active := make(map[types.Object]bool)
		var walk func(ast.Expr) error
		walk = func(expr ast.Expr) error {
			if id, ok := unparen(expr).(*ast.Ident); ok {
				obj := pkg.TypesInfo.Uses[id]
				if value, exists := f.modules[obj]; exists && !active[obj] {
					active[obj] = true
					defer delete(active, obj)
					return walk(value)
				}
				return nil
			}
			call, ok := unparen(expr).(*ast.CallExpr)
			if !ok {
				return nil
			}
			switch f.marker(call.Fun) {
			case "Discover":
				patterns, err := f.patterns(call)
				if err != nil {
					return err
				}
				for _, pattern := range patterns {
					found[pattern] = true
				}
			case "Module", "Build", "BuildWithCleanup", "Override":
				for _, arg := range call.Args {
					if err := walk(arg); err != nil {
						return err
					}
				}
			case "ForEnv":
				if _, err := f.environmentArgument(call); err != nil {
					return err
				}
				for _, arg := range call.Args[1:] {
					if err := walk(arg); err != nil {
						return err
					}
				}
			}
			return nil
		}
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				var walkErr error
				ast.Inspect(fn.Body, func(node ast.Node) bool {
					if walkErr != nil {
						return false
					}
					if call, ok := node.(*ast.CallExpr); ok {
						name := f.marker(call.Fun)
						if name == "Build" || name == "BuildWithCleanup" {
							walkErr = walk(call)
							return false
						}
					}
					return true
				})
				if walkErr != nil {
					return nil, walkErr
				}
			}
		}
	}
	var result []string
	for pattern := range found {
		result = append(result, pattern)
	}
	sort.Strings(result)
	return result, nil
}

func (f *frontend) patterns(call *ast.CallExpr) ([]string, error) {
	if len(call.Args) == 0 || call.Ellipsis.IsValid() {
		return nil, f.errorAt(call, "pfw.Discover requires constant, non-empty Go package patterns")
	}
	var patterns []string
	for _, arg := range call.Args {
		value := f.pkg.TypesInfo.Types[arg].Value
		if value == nil || value.Kind() != constant.String {
			return nil, f.errorAt(arg, "discovery patterns must be constant strings")
		}
		pattern := constant.StringVal(value)
		if strings.TrimSpace(pattern) == "" {
			return nil, f.errorAt(arg, "discovery pattern must not be empty")
		}
		if pattern == "." || pattern == ".." || strings.HasPrefix(pattern, "./") || strings.HasPrefix(pattern, "../") {
			pattern = filepath.Join(filepath.Dir(f.pkg.CompiledGoFiles[0]), pattern)
		}
		patterns = append(patterns, pattern)
	}
	return patterns, nil
}

func (d *discovery) register(f *frontend, call *ast.CallExpr, init *initializer) error {
	patterns, err := f.patterns(call)
	if err != nil {
		return err
	}
	for _, pattern := range patterns {
		paths, ok := d.matches[pattern]
		if !ok {
			return f.errorAt(call, "discovery pattern %s was not loaded", pattern)
		}
		for _, path := range paths {
			pkg := d.pkgs[path]
			for _, file := range pkg.Syntax {
				for _, decl := range file.Decls {
					fn, ok := decl.(*ast.FuncDecl)
					if !ok || !coconut(fn.Doc) {
						continue
					}
					if isTemplate(file) {
						return f.errorAt(fn, "discovered constructor must be available without the pfw_inject tag")
					}
					obj, ok := pkg.TypesInfo.Defs[fn.Name].(*types.Func)
					if !ok {
						return fmt.Errorf("missing type information for coconut %s", fn.Name.Name)
					}
					if err := f.addConstructor(obj, fn, init, false); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func coconut(doc *ast.CommentGroup) bool {
	if doc == nil {
		return false
	}
	for _, comment := range doc.List {
		if strings.HasPrefix(comment.Text, "//") && strings.TrimSpace(strings.TrimPrefix(comment.Text, "//")) == "pfw:coconut" {
			return true
		}
	}
	return false
}
