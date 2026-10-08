package config_test

import (
	"strings"
	"testing"
	"time"

	pfwconfig "github.com/palma99/palma-framework/config"
	appconfig "github.com/palma99/palma-framework/examples/httpapi/internal/config"
)

func TestEnvironmentAndValidation(t *testing.T) {
	values := map[string]string{"HTTP_ADDRESS": "127.0.0.1:9090", "HTTP_READ_HEADER_TIMEOUT": "2s", "HTTP_SHUTDOWN_TIMEOUT": "10s"}
	lookup := func(key string) (string, bool) { v, ok := values[key]; return v, ok }
	cfg, err := pfwconfig.Load[appconfig.Config](pfwconfig.Options{Lookup: lookup})
	if err != nil || cfg.HTTP.Address != "127.0.0.1:9090" || cfg.HTTP.ReadHeaderTimeout != 2*time.Second || cfg.HTTP.ShutdownTimeout != 10*time.Second || cfg.Seed.UserName != "Ada" {
		t.Fatalf("config: %+v %v", cfg, err)
	}
	values["HTTP_SHUTDOWN_TIMEOUT"] = "-1s"
	values["HTTP_ADDRESS"] = "not-an-address"
	cfg, err = pfwconfig.Load[appconfig.Config](pfwconfig.Options{Lookup: lookup})
	if cfg != (appconfig.Config{}) || err == nil || !strings.Contains(err.Error(), "HTTP.Address") || !strings.Contains(err.Error(), "HTTP.ShutdownTimeout") {
		t.Fatalf("invalid config: %+v %v", cfg, err)
	}
}
