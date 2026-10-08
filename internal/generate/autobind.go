package generate

import (
	"fmt"
	"go/ast"
	"go/constant"
	"sort"
)

func (f *frontend) setAutoBinding(call *ast.CallExpr) error {
	if len(call.Args) > 1 {
		return f.errorAt(call, "AutoBind accepts zero or one constant boolean")
	}
	enabled := true
	if len(call.Args) == 1 {
		value := f.pkg.TypesInfo.Types[call.Args[0]].Value
		if value == nil || value.Kind() != constant.Bool {
			return f.errorAt(call, "AutoBind requires a constant boolean")
		}
		enabled = constant.BoolVal(value)
	}
	scope := f.scopes[len(f.scopes)-1]
	// Environment-specific declarations override common settings independent of order.
	if scope.setting != nil {
		if scope.conditional && !f.conditional {
			return nil
		}
		if scope.conditional == f.conditional && *scope.setting != enabled {
			return f.errorAt(call, "conflicting AutoBind settings in the same scope")
		}
	}
	scope.setting = &enabled
	scope.conditional = f.conditional
	return nil
}

func autoPolicy(scope *registrationScope) *registrationScope {
	for scope != nil {
		if scope.setting != nil {
			return scope
		}
		scope = scope.parent
	}
	return nil
}

func configureAutoBinding(init *initializer) error {
	init.providerAutoScopes = make(map[string][]*registrationScope)
	for _, scope := range init.modules {
		policy := autoPolicy(scope)
		scope.auto = policy != nil && *policy.setting
	}
	root := init.rootScope
	root.auto = root.setting != nil && *root.setting
	if root.setting != nil {
		if root.auto {
			init.rootAutoScopes = []*registrationScope{root}
		}
	} else {
		// Preserve the original module-only behavior for interface roots.
		for _, scope := range init.modules {
			if scope.setting != nil && *scope.setting {
				init.rootAutoScopes = append(init.rootAutoScopes, scope)
			}
		}
	}
	candidates := func(scopes []*registrationScope) []string {
		names := make(map[string]bool)
		for _, p := range init.graph.Providers {
			if p.Fallback {
				continue
			}
			for _, scope := range scopes {
				if scope.names[p.Name] {
					names[p.Name] = true
				}
			}
		}
		var result []string
		for name := range names {
			result = append(result, name)
		}
		sort.Strings(result)
		return result
	}
	init.graph.RootAutoBind = candidates(init.rootAutoScopes)
	for index, p := range init.graph.Providers {
		depth := -1
		var policies []*registrationScope
		for _, scope := range init.providerScopes[p.Name] {
			policy := autoPolicy(scope)
			if policy == nil {
				continue
			}
			policies = append(policies, policy)
			if policy.depth > depth {
				depth = policy.depth
			}
		}
		var selected []*registrationScope
		for _, policy := range policies {
			if policy.depth != depth {
				continue
			}
			duplicate := false
			for _, previous := range selected {
				if *previous.setting != *policy.setting {
					return fmt.Errorf("conflicting AutoBind scopes for provider %s", p.Name)
				}
				duplicate = duplicate || previous == policy
			}
			if !duplicate {
				selected = append(selected, policy)
			}
		}
		if len(selected) > 0 && *selected[0].setting {
			init.providerAutoScopes[p.Name] = selected
			init.graph.Providers[index].AutoBind = candidates(selected)
		}
	}
	return nil
}
