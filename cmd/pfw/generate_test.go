package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestInvalidOutput(t *testing.T) {
	var output bytes.Buffer
	if err := run([]string{"generate", "-output", "../file.go"}, &output, &output); err == nil || !strings.Contains(err.Error(), "output must") {
		t.Fatalf("invalid output error = %v", err)
	}
}
