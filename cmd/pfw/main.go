package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/palma99/palma-framework/internal/generate"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "pfw:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] != "generate" {
		return fmt.Errorf("usage: pfw generate [-check] [-output pfw_gen.go] [packages...]")
	}
	flags := flag.NewFlagSet("pfw generate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	check := flags.Bool("check", false, "verify generated files without writing")
	output := flags.String("output", "pfw_gen.go", "output filename within each template package")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	paths, err := generate.Run(context.Background(), generate.Config{Patterns: flags.Args(), Output: *output, Check: *check})
	if err != nil {
		return err
	}
	for _, path := range paths {
		fmt.Fprintln(stdout, path)
	}
	return nil
}
