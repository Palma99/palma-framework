package generate

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"sort"

	pfw "github.com/palma99/palma-framework"
	"github.com/palma99/palma-framework/internal/di"
)

func isEnvironment(t types.Type) bool {
	named, ok := types.Unalias(t).(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == markerPath && named.Obj().Name() == "Environment"
}

func (f *frontend) environmentValue(arg ast.Expr) (string, error) {
	value := f.pkg.TypesInfo.Types[arg].Value
	if value == nil || value.Kind() != constant.String {
		return "", f.errorAt(arg, "environment names must be constant strings")
	}
	name := constant.StringVal(value)
	if err := pfw.Environment(name).Validate(); err != nil {
		return "", f.errorAt(arg, "%v", err)
	}
	return name, nil
}

func (f *frontend) environmentArgument(call *ast.CallExpr) (string, error) {
	if len(call.Args) == 0 || call.Ellipsis.IsValid() {
		return "", f.errorAt(call, "ForEnv requires an environment and static registrations")
	}
	return f.environmentValue(call.Args[0])
}

func (f *frontend) profiles(args []ast.Expr, enabled bool) ([]string, error) {
	mentioned, declared := make(map[string]bool), make(map[string]bool)
	explicit := false
	active := make(map[types.Object]bool)
	var walk func(ast.Expr) error
	walk = func(expr ast.Expr) error {
		if id, ok := unparen(expr).(*ast.Ident); ok {
			obj := f.pkg.TypesInfo.Uses[id]
			if value, found := f.modules[obj]; found && !active[obj] {
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
		case "ForEnv":
			if !enabled {
				return f.errorAt(call, "ForEnv requires a pfw.Environment parameter on the initializer")
			}
			name, err := f.environmentArgument(call)
			if err != nil {
				return err
			}
			mentioned[name] = true
			for _, arg := range call.Args[1:] {
				if err := walk(arg); err != nil {
					return err
				}
			}
		case "Environments":
			if !enabled || len(call.Args) == 0 || call.Ellipsis.IsValid() {
				return f.errorAt(call, "Environments requires a pfw.Environment parameter and constant names")
			}
			explicit = true
			for _, arg := range call.Args {
				name, err := f.environmentValue(arg)
				if err != nil {
					return err
				}
				declared[name] = true
			}
		case "Module", "Override":
			for _, arg := range call.Args {
				if err := walk(arg); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, arg := range args {
		if err := walk(arg); err != nil {
			return nil, err
		}
	}
	if !enabled {
		return []string{""}, nil
	}
	if !explicit {
		return nil, fmt.Errorf("initializer with a pfw.Environment parameter requires an explicit Environments declaration")
	}
	for name := range mentioned {
		if !declared[name] {
			return nil, fmt.Errorf("environment %q used by ForEnv is not declared in Environments", name)
		}
	}
	var profiles []string
	for name := range declared {
		profiles = append(profiles, name)
	}
	sort.Strings(profiles)
	return profiles, nil
}

func uniqueBindings(bindings []di.Binding) ([]di.Binding, error) {
	var result []di.Binding
	for _, b := range bindings {
		duplicate := false
		for _, old := range result {
			if types.Identical(b.Interface, old.Interface) {
				if !types.Identical(b.Concrete, old.Concrete) {
					return nil, fmt.Errorf("conflicting bindings for %s at the same environment level", b.Interface)
				}
				duplicate = true
			}
		}
		if !duplicate {
			result = append(result, b)
		}
	}
	return result, nil
}

func mergeEnvironmentBindings(init *initializer) error {
	if init.environment == "" {
		return nil
	}
	common, err := uniqueBindings(init.graph.Bindings)
	if err != nil {
		return err
	}
	specific, err := uniqueBindings(init.environmentBindings)
	if err != nil {
		return err
	}
	var merged []di.Binding
	for _, b := range common {
		shadowed := false
		for _, override := range specific {
			shadowed = shadowed || types.Identical(b.Interface, override.Interface)
		}
		if !shadowed {
			merged = append(merged, b)
		}
	}
	init.graph.Bindings = append(merged, specific...)
	return nil
}
