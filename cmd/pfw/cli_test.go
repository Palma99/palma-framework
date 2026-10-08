package main

import (
	"bytes"
	"testing"
)

func TestInvalidCommand(t *testing.T) {
	for _, args := range [][]string{{"unknown"}, {"generate", "-unknown"}} {
		var output bytes.Buffer
		if err := run(args, &output, &output); err == nil {
			t.Fatalf("run(%v) unexpectedly succeeded", args)
		}
	}
}
