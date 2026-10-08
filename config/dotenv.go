package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

func fileLookup(options Options, primary func(string) (string, bool)) (func(string) (string, bool), error) {
	values := make(map[string]string)
	for _, path := range options.EnvFiles {
		if strings.TrimSpace(path) == "" {
			return nil, fmt.Errorf("config: environment file path must not be empty")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			if options.IgnoreMissingEnvFiles && os.IsNotExist(err) {
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
	return func(key string) (string, bool) {
		if value, exists := primary(key); exists {
			return value, true
		}
		value, exists := values[key]
		return value, exists
	}, nil
}
