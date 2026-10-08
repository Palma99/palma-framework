package pfw

import (
	"fmt"
	"os"
	"regexp"
)

// Environment is an application-defined name, validated without predefined semantics.
type Environment string

// Conventional names are optional conveniences; applications declare their own
// supported names through Environments.
const (
	Local      Environment = "local"
	Staging    Environment = "staging"
	Production Environment = "production"
)

var environmentName = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

func (e Environment) Validate() error {
	if !environmentName.MatchString(string(e)) {
		return fmt.Errorf("pfw: invalid environment name %q", e)
	}
	return nil
}

// EnvironmentFromEnv resolves PFW_ENV, or an explicit application fallback.
// An explicitly empty PFW_ENV is invalid rather than silently using a fallback.
func EnvironmentFromEnv(fallback Environment) (Environment, error) {
	value, present := os.LookupEnv("PFW_ENV")
	env := fallback
	if present {
		env = Environment(value)
	}
	return env, env.Validate()
}
