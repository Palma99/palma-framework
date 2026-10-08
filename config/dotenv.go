package config

import (
	"fmt"
	pfw "github.com/palma99/palma-framework"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
)

func readFiles(paths []string, ignoreMissing bool) (map[string]string, error) {
	values := make(map[string]string)
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			return nil, fmt.Errorf("config: environment file path must not be empty")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			if ignoreMissing && os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("config: read environment file %q: %w", path, err)
		}
		parsed, err := godotenv.Unmarshal(string(data))
		if err != nil {
			// Parser errors may include source lines containing secrets.
			return nil, fmt.Errorf("config: invalid dotenv syntax in %q", path)
		}
		for key, value := range parsed {
			values[key] = value
		}
	}
	return values, nil
}

// EnvironmentValues reads optional layered files without changing process state.
// Reserved PFW_ENV/PFW_ENV_DIR selectors are never supplied by dotenv files.
func EnvironmentValues(env pfw.Environment, dir string) (map[string]string, error) {
	if err := env.Validate(); err != nil {
		return nil, err
	}
	paths := []string{filepath.Join(dir, ".env"), filepath.Join(dir, ".env."+string(env)), filepath.Join(dir, ".env."+string(env)+".local")}
	values, err := readFiles(paths, true)
	delete(values, "PFW_ENV")
	delete(values, "PFW_ENV_DIR")
	return values, err
}

func fileLookup(options Options, primary func(string) (string, bool)) (func(string) (string, bool), error) {
	var values map[string]string
	var err error
	if options.Environment != "" {
		if err := options.Environment.Validate(); err != nil {
			return nil, err
		}
	}
	if options.Environment != "" && len(options.EnvFiles) == 0 {
		values, err = EnvironmentValues(options.Environment, options.EnvDir)
	} else {
		values, err = readFiles(options.EnvFiles, options.IgnoreMissingEnvFiles)
	}
	if err != nil {
		return nil, err
	}
	return func(key string) (string, bool) {
		if key == "PFW_ENV" && options.Environment != "" {
			return string(options.Environment), true
		}
		if value, exists := primary(key); exists {
			return value, true
		}
		value, exists := values[key]
		return value, exists
	}, nil
}
