package main

import (
	"errors"
	"testing"

	pfwconfig "github.com/palma99/palma-framework/config"
)

func TestInvalidEnvironmentFailsBeforeServerStartup(t *testing.T) {
	t.Setenv("PFW_ENV", "local")
	t.Setenv("HTTPAPI_HTTP_ADDRESS", ":8080")
	t.Setenv("HTTPAPI_SEED_USER_NAME", "Ada")
	t.Setenv("HTTPAPI_HTTP_READ_HEADER_TIMEOUT", "5s")
	t.Setenv("HTTPAPI_HTTP_SHUTDOWN_TIMEOUT", "-1s")
	var field *pfwconfig.FieldError
	if err := run(); !errors.As(err, &field) || field.Field != "HTTP.ShutdownTimeout" {
		t.Fatalf("startup validation = %v", err)
	}
}
