package config

import (
	"errors"
	"net"
	"strconv"
	"time"

	pfwconfig "github.com/palma99/palma-framework/config"
	"github.com/palma99/palma-framework/examples/httpapi/internal/user/domain"
)

// Config is loaded before DI construction and server startup.
type Config struct {
	HTTP HTTPConfig `envPrefix:"HTTP_"`
	Seed SeedConfig `envPrefix:"SEED_"`
}

type HTTPConfig struct {
	Address           string        `env:"ADDRESS" envDefault:":8080"`
	ReadHeaderTimeout time.Duration `env:"READ_HEADER_TIMEOUT" envDefault:"5s"`
	ShutdownTimeout   time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"5s"`
}

type SeedConfig struct {
	UserName string `env:"USER_NAME" envDefault:"Ada"`
}

func (c Config) Validate() error {
	var problems []error
	_, port, err := net.SplitHostPort(c.HTTP.Address)
	n, parseErr := strconv.ParseUint(port, 10, 16)
	if err != nil || parseErr != nil || n == 0 {
		problems = append(problems, pfwconfig.Invalid("HTTP.Address", "must contain a host and a port between 1 and 65535"))
	}
	if c.HTTP.ReadHeaderTimeout <= 0 {
		problems = append(problems, pfwconfig.Invalid("HTTP.ReadHeaderTimeout", "must be positive"))
	}
	if c.HTTP.ShutdownTimeout <= 0 {
		problems = append(problems, pfwconfig.Invalid("HTTP.ShutdownTimeout", "must be positive"))
	}
	if _, err := domain.New(c.Seed.UserName); err != nil {
		problems = append(problems, pfwconfig.Invalid("Seed.UserName", "must contain between 1 and 100 non-blank characters"))
	}
	return errors.Join(problems...)
}

func Load() (Config, error) {
	return pfwconfig.Load[Config](pfwconfig.Options{Prefix: "HTTPAPI_", EnvFiles: []string{".env"}, IgnoreMissingEnvFiles: true})
}
