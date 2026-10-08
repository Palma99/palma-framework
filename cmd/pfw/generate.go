package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/palma99/palma-framework/internal/generate"
)

var generateCommand = command{
	usage:       "generate [options] [packages...]",
	description: "Generate Go dependency wiring",
	examples: []string{
		"go tool pfw generate -env dev ./internal/bootstrap",
		"go tool pfw generate -env dev -check ./internal/bootstrap",
	},
	notes: []string{"Packages default to bootstrap in the module root's pfw.toml, or ./internal/bootstrap. Explicit packages override configuration.", "Select -env when the initializer receives pfw.Environment. Only the selected graph is emitted; -check never writes files."},
	run:   runGenerate,
}

func runGenerate(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("pfw generate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	check := flags.Bool("check", false, "verify generated files without writing")
	output := flags.String("output", "pfw_gen.go", "output `file` within each template package")
	env := flags.String("env", "", "environment `name` (required for environment-aware initializers)")
	if err := parseCommandFlags("generate", flags, args, stdout); err != nil {
		return err
	}
	dir, patterns, err := projectPatterns("bootstrap", flags.Args())
	if err != nil {
		return err
	}
	paths, err := generate.Run(ctx, generate.Config{Dir: dir, Patterns: patterns, Output: *output, Check: *check, Environment: *env})
	if err != nil {
		return err
	}
	for _, path := range paths {
		fmt.Fprintln(stdout, path)
	}
	return nil
}
