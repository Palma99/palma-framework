package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/palma99/palma-framework/internal/generate"
)

var generateCommand = command{
	usage: "generate [-env name] [-check] [-output pfw_gen.go] [packages...]",
	run:   runGenerate,
}

func runGenerate(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("pfw generate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	check := flags.Bool("check", false, "verify generated files without writing")
	output := flags.String("output", "pfw_gen.go", "output filename within each template package")
	env := flags.String("env", "", "environment to generate (required for environment-aware initializers)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	paths, err := generate.Run(ctx, generate.Config{Patterns: flags.Args(), Output: *output, Check: *check, Environment: *env})
	if err != nil {
		return err
	}
	for _, path := range paths {
		fmt.Fprintln(stdout, path)
	}
	return nil
}
