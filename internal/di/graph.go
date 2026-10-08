// Package di implements the build-time dependency graph used by pfw.
// It is not a runtime container.
package di

import (
	"fmt"
	"go/types"
	"sort"
	"strings"
)

// Provider describes a constructor. Type checking and code generation belong
// to the frontend; the resolver only needs its name, inputs and output type.
type Provider struct {
	Name   string
	Output types.Type
	Inputs []types.Type
	// AutoBind lists eligible provider names for this constructor's dependencies.
	// Empty means automatic interface binding is disabled.
	AutoBind []string
	Override bool
	// Fallback providers are shadowed by application inputs/providers of the same type.
	Fallback bool
}

// Binding explicitly selects a concrete type for an interface dependency.
type Binding struct {
	Interface types.Type
	Concrete  types.Type
}

// Input is a dependency supplied by the caller of an initializer.
type Input struct {
	Name string
	Type types.Type
}

// Graph is a set of constructor registrations and interface bindings.
type Graph struct {
	Providers    []Provider
	Bindings     []Binding
	Inputs       []Input
	RootAutoBind []string
	// FallbackBindings are used only after application bindings and autobinding.
	FallbackBindings []Binding
}

// Selection records an interface choice for a particular constructor or root.
type Selection struct {
	Consumer  string
	Interface types.Type
	Concrete  types.Type
}

// Plan contains reachable providers in dependency-first construction order.
// Each provider occurs once, giving singleton semantics per initialization.
type Plan struct {
	Providers []Provider
	// Selections are scoped by consumer, allowing modules to choose different types.
	Selections []Selection
}

// Resolve validates registrations and plans construction for the requested
// roots. It compares Go type identities rather than printed names.
func (g Graph) Resolve(roots ...types.Type) (Plan, error) {
	// Exact-type overrides shadow ordinary providers without deleting providers
	// of other concrete types, which may still be explicitly requested.
	var active []Provider
	for _, p := range g.Providers {
		shadowed := false
		if p.Fallback {
			for _, input := range g.Inputs {
				shadowed = shadowed || types.Identical(input.Type, p.Output)
			}
			for _, other := range g.Providers {
				shadowed = shadowed || (!other.Fallback && types.Identical(other.Output, p.Output))
			}
		}
		for _, other := range g.Providers {
			if other.Override && !p.Override && p.Output != nil && other.Output != nil && types.Identical(p.Output, other.Output) {
				shadowed = true
				break
			}
		}
		if !shadowed {
			active = append(active, p)
		}
	}
	g.Providers = active
	if err := g.validate(); err != nil {
		return Plan{}, err
	}
	state := make([]uint8, len(g.Providers))
	var plan Plan
	var visit func(types.Type, string, []string) error
	visit = func(t types.Type, consumer string, chain []string) error {
		eligible := g.RootAutoBind
		for _, p := range g.Providers {
			if p.Name == consumer {
				eligible = p.AutoBind
				if p.Fallback && len(eligible) == 0 {
					eligible = g.RootAutoBind
				}
				break
			}
		}
		resolved, err := g.dependencyType(t, eligible)
		if err != nil {
			return fmt.Errorf("%s: %w", strings.Join(append(chain, typeName(t)), " -> "), err)
		}
		if !types.Identical(t, resolved) {
			known := false
			for _, b := range plan.Selections {
				known = known || (b.Consumer == consumer && types.Identical(t, b.Interface))
			}
			if !known {
				plan.Selections = append(plan.Selections, Selection{Consumer: consumer, Interface: t, Concrete: resolved})
			}
		}
		for _, input := range g.Inputs {
			if types.Identical(resolved, input.Type) {
				return nil
			}
		}
		index, err := g.providerFor(resolved)
		if err != nil {
			return fmt.Errorf("%s: %w", strings.Join(append(chain, typeName(t)), " -> "), err)
		}
		p := g.Providers[index]
		if state[index] == 1 {
			return fmt.Errorf("dependency cycle: %s", strings.Join(append(chain, p.Name), " -> "))
		}
		if state[index] == 2 {
			return nil
		}
		state[index] = 1
		for _, input := range p.Inputs {
			if err := visit(input, p.Name, append(append([]string(nil), chain...), p.Name)); err != nil {
				return err
			}
		}
		state[index] = 2
		plan.Providers = append(plan.Providers, p)
		return nil
	}
	for _, root := range roots {
		if root == nil {
			return Plan{}, fmt.Errorf("root type must not be nil")
		}
		if err := visit(root, "", nil); err != nil {
			return Plan{}, err
		}
	}
	return plan, nil
}

func (g Graph) validate() error {
	for i, input := range g.Inputs {
		if input.Name == "" || input.Type == nil {
			return fmt.Errorf("input %d must have a name and type", i)
		}
		for _, other := range g.Inputs[:i] {
			if types.Identical(input.Type, other.Type) {
				return fmt.Errorf("ambiguous inputs for %s: %s and %s", typeName(input.Type), other.Name, input.Name)
			}
		}
		for _, p := range g.Providers {
			if p.Output != nil && types.Identical(input.Type, p.Output) {
				return fmt.Errorf("ambiguous input and provider for %s: %s and %s", typeName(input.Type), input.Name, p.Name)
			}
		}
	}
	for i, p := range g.Providers {
		if p.Name == "" || p.Output == nil {
			return fmt.Errorf("provider %d must have a name and output type", i)
		}
		for _, input := range p.Inputs {
			if input == nil {
				return fmt.Errorf("provider %s has a nil input type", p.Name)
			}
		}
		for _, other := range g.Providers[:i] {
			if types.Identical(p.Output, other.Output) {
				return fmt.Errorf("ambiguous providers for %s: %s and %s", typeName(p.Output), other.Name, p.Name)
			}
		}
	}
	for i, b := range g.Bindings {
		if b.Interface == nil || b.Concrete == nil {
			return fmt.Errorf("binding %d must have interface and concrete types", i)
		}
		iface, ok := b.Interface.Underlying().(*types.Interface)
		if !ok || !iface.IsMethodSet() {
			return fmt.Errorf("binding target %s must be a value interface", typeName(b.Interface))
		}
		if _, ok := b.Concrete.Underlying().(*types.Interface); ok {
			return fmt.Errorf("binding source %s must be concrete", typeName(b.Concrete))
		}
		if !types.Implements(b.Concrete, iface) {
			return fmt.Errorf("%s does not implement %s", typeName(b.Concrete), typeName(b.Interface))
		}
		for _, other := range g.Bindings[:i] {
			if types.Identical(b.Interface, other.Interface) {
				return fmt.Errorf("duplicate binding for %s", typeName(b.Interface))
			}
		}
	}
	return nil
}

func (g Graph) providerFor(t types.Type) (int, error) {
	for i, p := range g.Providers {
		if types.Identical(t, p.Output) {
			return i, nil
		}
	}
	return -1, fmt.Errorf("missing provider for %s", typeName(t))
}

func (g Graph) dependencyType(t types.Type, eligible []string) (types.Type, error) {
	for _, b := range g.Bindings {
		if types.Identical(t, b.Interface) {
			return b.Concrete, nil
		}
	}
	// A registration returning the interface directly already supplies its value.
	for _, input := range g.Inputs {
		if types.Identical(t, input.Type) {
			return t, nil
		}
	}
	// Prefer compatible overrides within the consumer's opt-in scope.
	if iface, ok := t.Underlying().(*types.Interface); ok && iface.IsMethodSet() {
		var choices []Provider
		for _, p := range g.Providers {
			if p.Override && types.AssignableTo(p.Output, t) {
				for _, name := range eligible {
					if name == p.Name {
						choices = append(choices, p)
						break
					}
				}
			}
		}
		if len(choices) > 1 {
			var names []string
			for _, p := range choices {
				names = append(names, p.Name)
			}
			sort.Strings(names)
			return nil, fmt.Errorf("ambiguous override implementations for %s: %s; specify pfw.Implementation or pfw.Bind", typeName(t), strings.Join(names, ", "))
		}
		if len(choices) == 1 {
			return choices[0].Output, nil
		}
	}
	for _, p := range g.Providers {
		if types.Identical(t, p.Output) {
			return t, nil
		}
	}
	iface, ok := t.Underlying().(*types.Interface)
	if !ok || !iface.IsMethodSet() {
		return t, nil
	}
	var candidates []string
	var selected types.Type
	consider := func(name string, candidate types.Type) {
		if _, isInterface := candidate.Underlying().(*types.Interface); !isInterface && types.Implements(candidate, iface) {
			candidates = append(candidates, name+" ("+typeName(candidate)+")")
			selected = candidate
		}
	}
	for _, p := range g.Providers {
		for _, name := range eligible {
			if p.Name == name {
				consider(p.Name, p.Output)
				break
			}
		}
	}
	if len(candidates) > 1 {
		sort.Strings(candidates)
		return nil, fmt.Errorf("ambiguous implementations for %s: %s; specify pfw.Implementation or pfw.Bind", typeName(t), strings.Join(candidates, ", "))
	}
	if selected != nil {
		return selected, nil
	}
	for _, binding := range g.FallbackBindings {
		if types.Identical(t, binding.Interface) {
			return binding.Concrete, nil
		}
	}
	return t, nil
}

func typeName(t types.Type) string {
	return types.TypeString(t, nil)
}
