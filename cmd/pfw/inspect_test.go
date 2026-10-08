package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectJSONAndText(t *testing.T) {
	pattern, err := filepath.Abs("../../examples/httpapi/internal/bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	var output, diagnostics bytes.Buffer
	if err := run([]string{"inspect", "-json", "-env", "local", pattern}, &output, &diagnostics); err != nil {
		t.Fatal(err)
	}
	var report struct {
		Version      int `json:"version"`
		Initializers []struct {
			Name string `json:"name"`
		} `json:"initializers"`
	}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil || report.Version != 1 || len(report.Initializers) != 1 || report.Initializers[0].Name != "Initialize" || diagnostics.Len() != 0 {
		t.Fatalf("JSON output: %s diagnostics: %s err: %v", output.String(), diagnostics.String(), err)
	}
	output.Reset()
	if err := run([]string{"inspect", "-env", "local", pattern}, &output, &diagnostics); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "AutoBind=true") || !strings.Contains(output.String(), "coconut") {
		t.Fatalf("text output: %s", output.String())
	}
}

func TestInspectColorOptions(t *testing.T) {
	pattern, err := filepath.Abs("../../examples/httpapi/internal/bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args []string
		ansi bool
		json bool
	}{
		{[]string{"inspect", "-color=always", pattern}, true, false},
		{[]string{"inspect", "-color=never", pattern}, false, false},
		{[]string{"inspect", "-json", "-color=always", pattern}, false, true},
	} {
		var out, diagnostics bytes.Buffer
		if err := run(tc.args, &out, &diagnostics); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "\x1b[") != tc.ansi {
			t.Fatalf("color mode: %v", tc.args)
		}
		if tc.json && !json.Valid(out.Bytes()) {
			t.Fatal("color options polluted JSON output")
		}
	}
	var out bytes.Buffer
	if err := run([]string{"inspect", "-color=invalid", pattern}, &out, &out); err == nil {
		t.Fatal("invalid color mode accepted")
	}
}
