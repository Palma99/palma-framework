package config_test

import (
	"os"
	"path/filepath"
	"testing"

	pfw "github.com/palma99/palma-framework"
	pfwconfig "github.com/palma99/palma-framework/config"
)

type profiled struct {
	Value string `env:"VALUE" envDefault:"default"`
}

func (c profiled) ValidateEnvironment(env pfw.Environment) error {
	if env == pfw.Production && c.Value == "default" {
		return pfwconfig.Invalid("Value", "required in production")
	}
	return nil
}

func TestEnvironmentFilesAndConditionalValidation(t *testing.T) {
	dir := t.TempDir()
	for name, data := range map[string]string{".env": "VALUE=common\n", ".env.staging": "VALUE=staging\n", ".env.staging.local": "VALUE=personal\nPFW_ENV=wrong\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	missing := func(string) (string, bool) { return "", false }
	value, err := pfwconfig.Load[profiled](pfwconfig.Options{Environment: pfw.Staging, EnvDir: dir, Lookup: missing})
	if err != nil || value.Value != "personal" {
		t.Fatalf("profile configuration: %+v %v", value, err)
	}
	values, err := pfwconfig.EnvironmentValues(pfw.Staging, dir)
	if err != nil || values["PFW_ENV"] != "" {
		t.Fatalf("reserved environment selector: %v", err)
	}
	if _, err := pfwconfig.Load[profiled](pfwconfig.Options{Environment: pfw.Production, EnvDir: t.TempDir(), Lookup: missing}); err == nil {
		t.Fatal("production validation was skipped")
	}
	if _, err := pfwconfig.EnvironmentValues("../other", dir); err == nil {
		t.Fatal("path traversal in environment name accepted")
	}
}
