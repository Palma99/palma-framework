package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestInvalidCommand(t *testing.T) {
	for _, args := range [][]string{nil, {"unknown"}, {"generate", "-unknown"}} {
		var output bytes.Buffer
		if err := run(args, &output, &output); err == nil {
			t.Fatalf("run(%v) unexpectedly succeeded", args)
		}
	}
}

func TestInvalidOutput(t *testing.T) {
	var output bytes.Buffer
	if err := run([]string{"generate", "-output", "../file.go"}, &output, &output); err == nil || !strings.Contains(err.Error(), "output must") {
		t.Fatalf("invalid output error = %v", err)
	}
}
