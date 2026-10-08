package generate

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestCompactNamesDisambiguateAndPreserveJSON(t *testing.T) {
	init := InitializerReport{
		Package: "example.com/project/bootstrap", Name: "Initialize", Root: "*net/http.Server",
		Providers: []ProviderReport{
			{Name: "example.com/project/transport/http.Router", Output: "*github.com/labstack/echo/v5.Echo", Status: "used", Origins: []string{"manual"}, Dependencies: []string{"example.com/project/http.ErrorMapper"}},
			{Name: "example.com/project/user/http.Controller", Output: "*example.com/project/user/http.Controller", Status: "used", Origins: []string{"coconut"}},
			{Name: "example.com/project/unused.Ctor", Output: "string", Status: "unused"},
			{Name: "example.com/project/excluded.Ctor", Output: "bool", Status: "excluded"},
		}, ConstructionOrder: []string{"example.com/project/transport/http.Router", "example.com/project/user/http.Controller"},
	}
	report := Report{Version: 1, Initializers: []InitializerReport{init}}
	before, _ := json.Marshal(report)
	var out bytes.Buffer
	if err := report.WriteText(&out); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, expected := range []string{"bootstrap.Initialize", "transport/http.Router", "user/http.Controller", "echo.Echo", "[unused]", "[excluded]", "2 used · 1 unused · 1 excluded"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %q in %s", expected, text)
		}
	}
	if strings.Contains(text, "example.com/") || strings.Contains(text, "github.com/") || strings.Contains(text, "\x1b[") {
		t.Fatalf("default output is not compact/plain: %s", text)
	}
	after, _ := json.Marshal(report)
	if !bytes.Equal(before, after) {
		t.Fatal("renderer mutated JSON metadata")
	}
	out.Reset()
	if err := report.WriteText(&out, TextOptions{FullNames: true, Color: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "example.com/project/transport/http.Router") || !strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("full/color options: %s", out.String())
	}
}

func TestShortNamesPreserveTypeLiteralsAndVersions(t *testing.T) {
	init := InitializerReport{Package: "example.com/app", Inputs: []InputReport{
		{Type: "github.com/library/widget/v4.Type"}, {Type: "github.com/library/widget/v5.Type"},
		{Type: "struct { Value example.com/project/model.Value `envDefault:\"example.com/project/secret.Token\"` }"},
	}}
	short := shortNames(init)
	if short(init.Inputs[0].Type) == short(init.Inputs[1].Type) {
		t.Fatal("versioned packages have identical display names")
	}
	if got := short(init.Inputs[2].Type); !strings.Contains(got, "model.Value") || !strings.Contains(got, "`envDefault:\"example.com/project/secret.Token\"`") {
		t.Fatalf("type literal changed: %s", got)
	}
}
