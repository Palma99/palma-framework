package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type projectSettings struct {
	Bootstrap     *string `toml:"bootstrap"`
	Main          *string `toml:"main"`
	EnvDir        *string `toml:"env_dir"`
	MigrationsDir *string `toml:"migrations_dir"`
}

func readProjectSettings(root string) (projectSettings, error) {
	var settings projectSettings
	path := filepath.Join(root, "pfw.toml")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return settings, nil
	}
	if err != nil {
		return settings, fmt.Errorf("read %s: %w", path, err)
	}
	if err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&settings); err != nil {
		return settings, fmt.Errorf("parse %s: %w", path, err)
	}
	if settings.EnvDir != nil && strings.TrimSpace(*settings.EnvDir) == "" {
		return settings, fmt.Errorf("%s: env_dir must be a non-empty directory path", path)
	}
	if settings.MigrationsDir != nil && strings.TrimSpace(*settings.MigrationsDir) == "" {
		return settings, fmt.Errorf("%s: migrations_dir must be a non-empty directory path", path)
	}
	return settings, nil
}

// Project paths are relative to the module; explicit flag/environment paths
// retain the usual meaning relative to the caller's working directory.
func projectEnvironmentDirectory(root, explicit string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}
	if directory := os.Getenv("PFW_ENV_DIR"); directory != "" {
		return filepath.Abs(directory)
	}
	settings, err := readProjectSettings(root)
	if err != nil {
		return "", err
	}
	directory := root
	if settings.EnvDir != nil {
		directory = *settings.EnvDir
		if !filepath.IsAbs(directory) {
			directory = filepath.Join(root, directory)
		}
	}
	return filepath.Abs(directory)
}

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
	settings, err := readProjectSettings(root)
	if err != nil {
		return "", nil, err
	}
	path := filepath.Join(root, "pfw.toml")
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
