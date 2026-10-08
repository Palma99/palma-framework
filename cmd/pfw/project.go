package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// projectPatterns resolves implicit targets at the module root. Explicit Go
// package patterns retain their usual meaning relative to the working directory.
func projectPatterns(kind string, explicit []string) (string, []string, error) {
	if len(explicit) > 0 {
		return "", explicit, nil
	}
	root, err := os.Getwd()
	if err != nil {
		return "", nil, err
	}
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return "", nil, err
		}
		parent := filepath.Dir(root)
		if parent == root {
			return "", nil, fmt.Errorf("cannot find module root (go.mod); specify a package explicitly")
		}
		root = parent
	}
	settings := struct {
		Bootstrap *string `toml:"bootstrap"`
		Main      *string `toml:"main"`
	}{}
	path := filepath.Join(root, "pfw.toml")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", nil, fmt.Errorf("read %s: %w", path, err)
	}
	if err == nil {
		if err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&settings); err != nil {
			return "", nil, fmt.Errorf("parse %s: %w", path, err)
		}
	}
	pattern, configured := "./internal/bootstrap", settings.Bootstrap
	if kind == "main" {
		pattern, configured = "./cmd/app", settings.Main
	}
	if configured != nil {
		pattern = *configured
		if strings.TrimSpace(pattern) == "" {
			return "", nil, fmt.Errorf("%s: %s must be a non-empty package path", path, kind)
		}
		// TOML paths describe directories relative to the module root, rather
		// than import paths. A leading ./ makes bare paths work with Go tools.
		if !filepath.IsAbs(pattern) && !strings.HasPrefix(pattern, "./") && !strings.HasPrefix(pattern, "../") {
			pattern = "./" + pattern
		}
	}
	return root, []string{pattern}, nil
}
