package generate

import (
	"context"
	"fmt"
	pfw "github.com/palma99/palma-framework"
	"go/types"
	"sort"
)

type Report struct {
	Version      int                 `json:"version"`
	Initializers []InitializerReport `json:"initializers"`
}

type InitializerReport struct {
	Package           string           `json:"package"`
	Name              string           `json:"name"`
	Environment       string           `json:"environment,omitempty"`
	Root              string           `json:"root"`
	WithCleanup       bool             `json:"with_cleanup"`
	Inputs            []InputReport    `json:"inputs"`
	Providers         []ProviderReport `json:"providers"`
	Modules           []ModuleReport   `json:"modules"`
	Bindings          []BindingReport  `json:"bindings"`
	ConstructionOrder []string         `json:"construction_order"`
	CleanupOrder      []string         `json:"cleanup_order"`
}
type InputReport struct {
	Name string `json:"name"`
	Type string `json:"type"`
}
type ProviderReport struct {
	Name               string   `json:"name"`
	Output             string   `json:"output"`
	Dependencies       []string `json:"dependencies"`
	Origins            []string `json:"origins"`
	Modules            []string `json:"modules"`
	AutoBindScopes     []string `json:"autobind_scopes"`
	Source             string   `json:"source"`
	Status             string   `json:"status"`
	ExclusionRequested bool     `json:"exclusion_requested"`
	Fallible           bool     `json:"fallible"`
	Cleanup            bool     `json:"cleanup"`
	Override           bool     `json:"override,omitempty"`
	Fallback           bool     `json:"fallback,omitempty"`
}
type ModuleReport struct {
	Name      string   `json:"name"`
	Source    string   `json:"source"`
	AutoBind  bool     `json:"autobind"`
	Providers []string `json:"providers"`
}
type BindingReport struct {
	Consumer       string   `json:"consumer"`
	Interface      string   `json:"interface"`
	SelectedType   string   `json:"selected_type"`
	Provider       string   `json:"provider"`
	Mode           string   `json:"mode"`
	AutoBindScopes []string `json:"autobind_scopes"`
}

// Inspect validates and describes the same graph used by generation. It never
// emits/writes Go files or evaluates constructors/configuration at runtime.
func Inspect(ctx context.Context, cfg Config) (Report, error) {
	if cfg.Environment != "" {
		if err := pfw.Environment(cfg.Environment).Validate(); err != nil {
			return Report{}, err
		}
	}
	analyses, err := analyze(ctx, cfg, false)
	if err != nil {
		return Report{}, err
	}
	report := Report{Version: 1, Initializers: []InitializerReport{}}
	hasProfiles, matched := false, false
	for _, analysis := range analyses {
		for _, init := range analysis.initializers {
			if init.environment != "" {
				hasProfiles = true
				matched = matched || init.environment == cfg.Environment
			}
		}
	}
	if cfg.Environment != "" && hasProfiles && !matched {
		return Report{}, fmt.Errorf("no graph for environment %q", cfg.Environment)
	}
	for _, analysis := range analyses {
		for _, init := range analysis.initializers {
			if cfg.Environment != "" && init.environment != "" && init.environment != cfg.Environment {
				continue
			}
			r := InitializerReport{Environment: init.environment, Package: analysis.pkg.PkgPath, Name: init.name, Root: typeString(init.root), WithCleanup: init.withCleanup, Inputs: []InputReport{}, Providers: []ProviderReport{}, Modules: []ModuleReport{}, Bindings: []BindingReport{}, ConstructionOrder: []string{}, CleanupOrder: []string{}}
			for n, input := range init.graph.Inputs {
				r.Inputs = append(r.Inputs, InputReport{Name: init.inputNames[n], Type: typeString(input.Type)})
			}
			used, included := make(map[string]bool), make(map[string]bool)
			for _, p := range init.graph.Providers {
				included[p.Name] = true
			}
			for _, p := range init.plan.Providers {
				used[p.Name] = true
				r.ConstructionOrder = append(r.ConstructionOrder, p.Name)
			}
			for n := len(init.plan.Providers) - 1; n >= 0; n-- {
				p := init.plan.Providers[n]
				if init.constructors[p.Name].cleanup {
					r.CleanupOrder = append(r.CleanupOrder, p.Name)
				}
			}
			seenModules := make(map[string]bool)
			for _, scope := range init.modules {
				key := scope.name + "@" + scope.source
				if seenModules[key] {
					continue
				}
				seenModules[key] = true
				m := ModuleReport{Name: scope.name, Source: scope.source, AutoBind: scope.auto, Providers: []string{}}
				for name := range scope.names {
					if included[name] {
						m.Providers = append(m.Providers, name)
					}
				}
				sort.Strings(m.Providers)
				r.Modules = append(r.Modules, m)
			}
			sort.Slice(r.Modules, func(i, j int) bool {
				return r.Modules[i].Name+r.Modules[i].Source < r.Modules[j].Name+r.Modules[j].Source
			})
			for _, p := range init.registered {
				ctor := init.constructors[p.Name]
				status := "unused"
				if !included[p.Name] {
					status = "excluded"
				} else if used[p.Name] {
					status = "used"
				}
				item := ProviderReport{Name: p.Name, Output: typeString(p.Output), Dependencies: []string{}, Origins: []string{}, Modules: []string{}, AutoBindScopes: []string{}, Source: analysis.pkg.Fset.Position(ctor.fn.Pos()).String(), Status: status, ExclusionRequested: init.excluded[p.Name], Fallible: ctor.fallible, Cleanup: ctor.cleanup}
				item.Override = init.overrides[p.Name]
				item.Fallback = init.framework[p.Name]
				if item.Fallback {
					item.Origins = append(item.Origins, "framework_default")
				}
				if init.manual[p.Name] {
					item.Origins = append(item.Origins, "manual")
				}
				if init.discovered[p.Name] {
					item.Origins = append(item.Origins, "coconut")
				}
				for _, input := range p.Inputs {
					item.Dependencies = append(item.Dependencies, typeString(input))
				}
				for _, scope := range init.modules {
					if scope.names[p.Name] {
						item.Modules = append(item.Modules, scope.name)
						if scope.auto {
							item.AutoBindScopes = append(item.AutoBindScopes, scope.name)
						}
					}
				}
				item.Modules = unique(item.Modules)
				item.AutoBindScopes = unique(item.AutoBindScopes)
				r.Providers = append(r.Providers, item)
			}
			sort.Slice(r.Providers, func(i, j int) bool { return r.Providers[i].Name < r.Providers[j].Name })
			addBinding := func(consumer string, requested types.Type) {
				if _, ok := requested.Underlying().(*types.Interface); !ok {
					return
				}
				selected := requested
				mode := "direct"
				for _, s := range init.plan.Selections {
					if s.Consumer == consumer && types.Identical(s.Interface, requested) {
						selected = s.Concrete
						mode = "automatic"
						for _, b := range init.graph.Bindings {
							if types.Identical(b.Interface, requested) {
								mode = "manual"
							}
						}
						break
					}
				}
				b := BindingReport{Consumer: consumer, Interface: typeString(requested), SelectedType: typeString(selected), Mode: mode, AutoBindScopes: []string{}}
				for _, p := range init.plan.Providers {
					if types.Identical(p.Output, selected) {
						b.Provider = p.Name
					}
				}
				for n, input := range init.graph.Inputs {
					if types.Identical(input.Type, selected) {
						b.Provider = "input:" + init.inputNames[n]
					}
				}
				if mode == "automatic" {
					for _, scope := range init.modules {
						if scope.auto && (consumer == "" || scope.names[consumer]) {
							b.AutoBindScopes = append(b.AutoBindScopes, scope.name)
						}
					}
				}
				b.AutoBindScopes = unique(b.AutoBindScopes)
				for _, previous := range r.Bindings {
					if previous.Consumer == b.Consumer && previous.Interface == b.Interface {
						return
					}
				}
				r.Bindings = append(r.Bindings, b)
			}
			addBinding("", init.root)
			for _, p := range init.plan.Providers {
				for _, input := range p.Inputs {
					addBinding(p.Name, input)
				}
			}
			sort.Slice(r.Bindings, func(i, j int) bool {
				return r.Bindings[i].Consumer+r.Bindings[i].Interface < r.Bindings[j].Consumer+r.Bindings[j].Interface
			})
			report.Initializers = append(report.Initializers, r)
		}
	}
	sort.Slice(report.Initializers, func(i, j int) bool {
		return report.Initializers[i].Package+report.Initializers[i].Name+report.Initializers[i].Environment < report.Initializers[j].Package+report.Initializers[j].Name+report.Initializers[j].Environment
	})
	if len(report.Initializers) == 0 {
		return Report{}, fmt.Errorf("no graph for environment %q", cfg.Environment)
	}
	return report, nil
}

func typeString(t types.Type) string { return types.TypeString(t, nil) }
func unique(values []string) []string {
	sort.Strings(values)
	result := []string{}
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}
