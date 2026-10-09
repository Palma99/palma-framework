package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/palma99/palma-framework/internal/scaffold"
)

var addCommand = command{
	usage:       "add <module> [options]",
	description: "Add an optional module to an existing API scaffold",
	examples:    []string{"pfw add auth", "pfw add auth -dir ./myapi"},
	notes:       []string{"Available modules: auth. Run from an API scaffold or one of its subdirectories.", "Auth adds bearer JWT authentication, a principal from verified claims and /auth endpoints. Existing application routes keep their access rules.", "The command preserves custom code and rejects conflicting files or unsupported integration points before writing. Run generate for the environment you intend to use afterwards."},
	run:         runAdd,
}

func runAdd(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	name := ""
	if len(args) > 0 && args[0] != "--help" && args[0] != "-h" && args[0] != "-help" {
		name, args = args[0], args[1:]
	}
	flags := flag.NewFlagSet("pfw add", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dir := flags.String("dir", ".", "existing project directory `path`")
	if err := parseCommandFlags("add", flags, args, stdout); err != nil {
		return err
	}
	if name == "" || flags.NArg() != 0 {
		return fmt.Errorf("usage: pfw add <module> [options]")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root, added, err := scaffold.AddModule(*dir, name)
	if err != nil {
		return err
	}
	if !added {
		fmt.Fprintf(stdout, "Module %s is already installed in %s\n", name, root)
		return nil
	}
	fmt.Fprintf(stdout, "Added %s module in %s\n\nFrom the project directory:\n  go mod tidy\n  go tool pfw generate -env local\n  go test ./...\n\nSee internal/auth/README.md for JWT configuration and endpoints.\n", name, root)
	return nil
}
