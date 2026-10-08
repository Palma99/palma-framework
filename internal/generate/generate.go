// Package generate loads explicit pfw declarations and emits Go initializers.
package generate

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Config describes a generation run. Output is a filename within each target
// package; place templates in the package where initializers should be emitted.
type Config struct {
	Dir      string
	Patterns []string
	Output   string
	Check    bool
}

// Run validates all graphs and type-checks generated code before writing any
// files. Check reports missing or stale files without modifying them.
func Run(ctx context.Context, cfg Config) ([]string, error) {
	if cfg.Output == "" {
		cfg.Output = "pfw_gen.go"
	}
	if filepath.Base(cfg.Output) != cfg.Output || !strings.HasSuffix(cfg.Output, ".go") || strings.HasSuffix(cfg.Output, "_test.go") || strings.HasPrefix(cfg.Output, ".") || strings.HasPrefix(cfg.Output, "_") {
		return nil, fmt.Errorf("output must be a Go filename in the template package, without a _test.go suffix")
	}
	analyses, err := analyze(ctx, cfg)
	if err != nil {
		return nil, err
	}
	outputs := make(map[string][]byte)
	var targets []string
	for _, analysis := range analyses {
		pkg, initializers := analysis.pkg, analysis.initializers
		data, err := emit(pkg.Types, initializers)
		if err != nil {
			return nil, err
		}
		path := filepath.Join(filepath.Dir(pkg.CompiledGoFiles[0]), cfg.Output)
		old, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if err == nil && !bytes.HasPrefix(old, []byte(generatedHeader)) {
			return nil, fmt.Errorf("refusing to overwrite non-generated file %s", path)
		}
		outputs[path] = data
		targets = append(targets, pkg.PkgPath)
	}
	if len(outputs) == 0 {
		return nil, fmt.Errorf("no pfw.Build or pfw.BuildWithCleanup initializers found in the selected packages")
	}
	// Validate in the application's normal build context, without template tags.
	// The overlay also permits first generation when files do not exist yet.
	checked, err := packages.Load(&packages.Config{Context: ctx, Dir: cfg.Dir, Mode: packages.LoadSyntax, Overlay: outputs}, targets...)
	if err != nil {
		return nil, err
	}
	if err := packageErrors(checked); err != nil {
		return nil, fmt.Errorf("generated code validation: %w", err)
	}
	paths := make([]string, 0, len(outputs))
	for path := range outputs {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	if cfg.Check {
		var stale []string
		for _, path := range paths {
			old, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(old, outputs[path]) {
				stale = append(stale, path)
			}
		}
		if len(stale) > 0 {
			return nil, fmt.Errorf("generated files are missing or stale; run pfw generate:\n%s", strings.Join(stale, "\n"))
		}
		return paths, nil
	}
	for _, path := range paths {
		if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, outputs[path]) {
			continue
		}
		if err := writeAtomic(path, outputs[path]); err != nil {
			return nil, err
		}
	}
	return paths, nil
}

func writeAtomic(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".pfw-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Chmod(0644); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func loadPackages(ctx context.Context, cfg Config) ([]*packages.Package, *discovery, map[string]bool, error) {
	if len(cfg.Patterns) == 0 {
		cfg.Patterns = []string{"."}
	}
	mode := packages.LoadSyntax
	pkgs, err := packages.Load(&packages.Config{Context: ctx, Dir: cfg.Dir, Mode: mode, BuildFlags: []string{"-tags=pfw_inject"}}, cfg.Patterns...)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := packageErrors(pkgs); err != nil {
		return nil, nil, nil, err
	}
	selected := make(map[string]bool)
	for _, pkg := range pkgs {
		selected[pkg.PkgPath] = true
	}
	patterns, err := discoveryPatterns(pkgs)
	if err != nil {
		return nil, nil, nil, err
	}
	catalog := &discovery{matches: make(map[string][]string), pkgs: make(map[string]*packages.Package)}
	if len(patterns) > 0 {
		// Match paths first, then load all source/type information in one universe.
		// Mixing types from separate Load calls would break interface bindings.
		all := make(map[string]bool)
		for path := range selected {
			all[path] = true
		}
		for _, pattern := range patterns {
			matched, err := packages.Load(&packages.Config{Context: ctx, Dir: cfg.Dir, Mode: packages.LoadFiles, BuildFlags: []string{"-tags=pfw_inject"}}, pattern)
			if err != nil {
				return nil, nil, nil, err
			}
			if err := packageErrors(matched); err != nil {
				return nil, nil, nil, err
			}
			if len(matched) == 0 {
				return nil, nil, nil, fmt.Errorf("discovery pattern %s matched no packages", pattern)
			}
			for _, pkg := range matched {
				catalog.matches[pattern] = append(catalog.matches[pattern], pkg.PkgPath)
				all[pkg.PkgPath] = true
			}
			sort.Strings(catalog.matches[pattern])
		}
		var paths []string
		for path := range all {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		pkgs, err = packages.Load(&packages.Config{Context: ctx, Dir: cfg.Dir, Mode: mode, BuildFlags: []string{"-tags=pfw_inject"}}, paths...)
		if err != nil {
			return nil, nil, nil, err
		}
		if err := packageErrors(pkgs); err != nil {
			return nil, nil, nil, err
		}
	}
	for _, pkg := range pkgs {
		catalog.pkgs[pkg.PkgPath] = pkg
	}
	return pkgs, catalog, selected, nil
}

type analysis struct {
	pkg          *packages.Package
	initializers []initializer
}

func analyze(ctx context.Context, cfg Config) ([]analysis, error) {
	pkgs, catalog, selected, err := loadPackages(ctx, cfg)
	if err != nil {
		return nil, err
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].PkgPath < pkgs[j].PkgPath })
	var result []analysis
	for _, pkg := range pkgs {
		if !selected[pkg.PkgPath] {
			continue
		}
		initializers, err := readPackage(pkg, catalog)
		if err != nil {
			return nil, err
		}
		if len(initializers) > 0 {
			result = append(result, analysis{pkg: pkg, initializers: initializers})
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no pfw.Build or pfw.BuildWithCleanup initializers found in the selected packages")
	}
	return result, nil
}
