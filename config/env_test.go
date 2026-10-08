package config_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	pfwconfig "github.com/palma99/palma-framework/config"
)

func options(values map[string]string) pfwconfig.Options {
	return pfwconfig.Options{Prefix: "APP_", Lookup: func(key string) (string, bool) { v, ok := values[key]; return v, ok }}
}

type mode string

func (m *mode) UnmarshalText(text []byte) error {
	if string(text) != "production" {
		return fmt.Errorf("secret value %s is invalid", text)
	}
	*m = mode(text)
	return nil
}

type database struct {
	Host string `env:"HOST"`
	Port uint16 `env:"PORT" envDefault:"5432"`
}
type settings struct {
	Name     string          `env:"NAME" envDefault:"default"`
	Enabled  bool            `env:"ENABLED" envDefault:"true"`
	Timeout  time.Duration   `env:"TIMEOUT" envDefault:"5s"`
	Count    int8            `env:"COUNT"`
	Ratio    float32         `env:"RATIO"`
	Origins  []string        `env:"ORIGINS"`
	Delays   []time.Duration `env:"DELAYS" envSeparator:";"`
	Optional *int            `env:"OPTIONAL"`
	Database database        `envPrefix:"DB_"`
	Mode     mode            `env:"MODE"`
}

func TestTypedConfiguration(t *testing.T) {
	value, err := pfwconfig.Load[settings](options(map[string]string{
		"APP_NAME": "", "APP_COUNT": "127", "APP_RATIO": "0.5", "APP_ORIGINS": "https://a.test, https://b.test",
		"APP_DELAYS": "1s; 2m", "APP_OPTIONAL": "42", "APP_DB_HOST": "localhost", "APP_MODE": "production",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if value.Name != "" || !value.Enabled || value.Timeout != 5*time.Second || value.Count != 127 || value.Ratio != 0.5 || value.Database.Host != "localhost" || value.Database.Port != 5432 || value.Mode != "production" || value.Optional == nil || *value.Optional != 42 || len(value.Origins) != 2 || value.Origins[1] != "https://b.test" || len(value.Delays) != 2 || value.Delays[1] != 2*time.Minute {
		t.Fatalf("config: %+v", value)
	}
	absent, err := pfwconfig.Load[settings](options(nil))
	if err != nil || absent.Name != "default" || absent.Optional != nil {
		t.Fatalf("defaults: %+v %v", absent, err)
	}
}

func TestErrorsAggregateWithoutValuesOrPartialConfig(t *testing.T) {
	type config struct {
		Token string  `env:"TOKEN" envRequired:"true"`
		Count int8    `env:"COUNT"`
		Mode  mode    `env:"MODE"`
		Ratio float64 `env:"RATIO"`
	}
	value, err := pfwconfig.Load[config](options(map[string]string{"APP_COUNT": "999999", "APP_MODE": "top-secret", "APP_RATIO": "NaN"}))
	var aggregate *pfwconfig.Error
	var field *pfwconfig.FieldError
	if !errors.As(err, &aggregate) || len(aggregate.Fields) != 4 || !errors.As(err, &field) || value != (config{}) {
		t.Fatalf("error: %+v %v", value, err)
	}
	for _, secret := range []string{"999999", "top-secret", "NaN"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("leaked value in %v", err)
		}
	}
	if !strings.Contains(err.Error(), "APP_TOKEN") || !strings.Contains(err.Error(), "Mode") {
		t.Fatalf("missing field context: %v", err)
	}
}

type validated struct {
	Port int `env:"PORT" envDefault:"8080"`
}

func (v *validated) Validate() error {
	if v.Port <= 0 {
		return pfwconfig.Invalid("Port", "must be positive")
	}
	return nil
}

func TestValidationAndProcessEnvironment(t *testing.T) {
	value, err := pfwconfig.Load[validated](options(map[string]string{"APP_PORT": "0"}))
	if value != (validated{}) || err == nil || !strings.Contains(err.Error(), "must be positive") {
		t.Fatalf("validation: %+v %v", value, err)
	}
	t.Setenv("PFW_ENV_TEST_PORT", "9090")
	value, err = pfwconfig.Load[validated](pfwconfig.Options{Prefix: "PFW_ENV_TEST_"})
	if err != nil || value.Port != 9090 {
		t.Fatalf("process environment: %+v %v", value, err)
	}
	if _, err := pfwconfig.Load[int](options(nil)); err == nil {
		t.Fatal("non-struct config accepted")
	}
}

func TestSchemaErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		load func() error
		want string
	}{
		{"duplicate keys", func() error {
			type c struct {
				A string `env:"KEY"`
				B int    `env:"KEY"`
			}
			_, err := pfwconfig.Load[c](options(nil))
			return err
		}, "already mapped"},
		{"required default", func() error {
			type c struct {
				A string `env:"KEY" envRequired:"true" envDefault:"value"`
			}
			_, err := pfwconfig.Load[c](options(nil))
			return err
		}, "cannot be combined"},
		{"bad required", func() error {
			type c struct {
				A string `env:"KEY" envRequired:"yes"`
			}
			_, err := pfwconfig.Load[c](options(nil))
			return err
		}, "true or false"},
		{"unexported", func() error {
			type c struct {
				a string `env:"KEY"`
			}
			_, err := pfwconfig.Load[c](options(nil))
			return err
		}, "exported field"},
		{"unsupported", func() error {
			type c struct {
				A map[string]int `env:"KEY"`
			}
			_, err := pfwconfig.Load[c](options(nil))
			return err
		}, "unsupported"},
		{"missing tag", func() error {
			type c struct {
				A string `envDefault:"value"`
			}
			_, err := pfwconfig.Load[c](options(nil))
			return err
		}, "env tag is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.load(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("schema error: %v", err)
			}
		})
	}
}

func TestNestedPointersAndRequiredEmpty(t *testing.T) {
	type nested struct {
		Value string `env:"VALUE"`
	}
	type c struct {
		Nested *nested `envPrefix:"NESTED_"`
		Skip   string  `env:"-"`
	}
	value, err := pfwconfig.Load[c](options(nil))
	if err != nil || value.Nested != nil {
		t.Fatalf("optional nested: %+v %v", value, err)
	}
	value, err = pfwconfig.Load[c](options(map[string]string{"APP_NESTED_VALUE": "", "APP_SKIP": "ignored"}))
	if err != nil || value.Nested == nil || value.Nested.Value != "" || value.Skip != "" {
		t.Fatalf("present empty nested: %+v %v", value, err)
	}
	type required struct {
		Token string `env:"TOKEN" envRequired:"true"`
	}
	if _, err := pfwconfig.Load[required](options(map[string]string{"APP_TOKEN": ""})); err == nil {
		t.Fatal("empty required value accepted")
	}
	type recursive struct{ Next *recursive }
	if _, err := pfwconfig.Load[recursive](options(nil)); err == nil {
		t.Fatal("recursive schema accepted")
	}
}
