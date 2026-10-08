package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/palma99/palma-framework/internal/generate"
	"golang.org/x/term"
)

var inspectCommand = command{
	usage:       "inspect [options] [packages...]",
	description: "Inspect the dependency graph without writing files",
	examples: []string{
		"go tool pfw inspect -env dev ./internal/bootstrap",
		"go tool pfw inspect -json ./internal/bootstrap",
	},
	notes: []string{"Packages default to bootstrap in the module root's pfw.toml, or ./internal/bootstrap. Explicit packages override configuration.", "Without -env, all declared environments are shown. All graphs are validated even when the output is filtered."},
	run:   runInspect,
}

func runInspect(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("pfw inspect", flag.ContinueOnError)
	flags.SetOutput(stderr)
	asJSON := flags.Bool("json", false, "print the dependency report as JSON")
	full := flags.Bool("full", false, "show full package names and source locations")
	color := flags.String("color", "auto", "color `mode`: auto, always, never")
	env := flags.String("env", "", "inspect a specific environment `name`")
	if err := parseCommandFlags("inspect", flags, args, stdout); err != nil {
		return err
	}
	if *color != "auto" && *color != "always" && *color != "never" {
		return fmt.Errorf("color must be auto, always or never")
	}
	dir, patterns, err := projectPatterns("bootstrap", flags.Args())
	if err != nil {
		return err
	}
	report, err := generate.Inspect(ctx, generate.Config{Dir: dir, Patterns: patterns, Environment: *env})
	if err != nil {
		return err
	}
	if *asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	return report.WriteText(stdout, generate.TextOptions{FullNames: *full, Color: useColor(*color, stdout)})
}

func useColor(mode string, stdout io.Writer) bool {
	if mode != "auto" {
		return mode == "always"
	}
	file, ok := stdout.(*os.File)
	if !ok {
		return false
	}
	_, disabled := os.LookupEnv("NO_COLOR")
	return !disabled && os.Getenv("TERM") != "dumb" && term.IsTerminal(int(file.Fd()))
}
