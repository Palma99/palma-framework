package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/palma99/palma-framework/internal/scaffold"
)

const newUsage = "new [options] directory"

var newCommand = command{
	usage:       newUsage,
	description: "Create a Go project from a bundled template",
	examples: []string{
		"pfw new -template api -router echo -module example.com/myapi ./myapi",
		"pfw new -template hello-world -module example.com/hello ./hello",
		"go run ./cmd/pfw new -template api -module example.com/api -framework-dir . /tmp/palma-api",
	},
	notes: []string{"Run from your workspace. The destination must not exist; new creates go.mod and the project files for you.", "-module is required and sets the Go module path used by imports. The directory is where files are written; it can have a different name.", "Templates: hello-world, api. API routers: stdlib (default), echo. Run pfw templates to list the available choices.", "A release CLI pins its own version. A development CLI requires -framework-dir or -framework-version."},
	run:   runNew,
}

var templatesCommand = command{
	usage:       "templates",
	description: "List project templates and available API routers",
	examples:    []string{"pfw templates"},
	run: func(_ context.Context, args []string, stdout, _ io.Writer) error {
		if len(args) != 0 {
			return fmt.Errorf("usage: pfw templates")
		}
		for _, t := range scaffold.Templates() {
			fmt.Fprintf(stdout, "%s\t%s\n", t.Name, t.Description)
		}
		var routers []string
		for _, router := range scaffold.Routers() {
			routers = append(routers, router.Name)
		}
		fmt.Fprintf(stdout, "\nAPI routers: %s (default: stdlib)\n", strings.Join(routers, ", "))
		return nil
	},
}

func runNew(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("pfw new", flag.ContinueOnError)
	flags.SetOutput(stderr)
	name := flags.String("template", "hello-world", "project template `name` (see pfw templates)")
	module := flags.String("module", "", "Go module `path` used by imports (required)")
	env := flags.String("env", "dev", "initial environment `name`")
	router := flags.String("router", "", "API router `name` (default: stdlib; see pfw templates)")
	version := flags.String("framework-version", "", "published framework `version`")
	local := flags.String("framework-dir", "", "local framework checkout `path` for development")
	if err := parseCommandFlags("new", flags, args, stdout); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: pfw %s", newUsage)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if *version == "" && *local == "" {
		if release := currentVersion(); release != "dev" {
			*version = release
		}
	}
	dir, err := scaffold.Create(scaffold.Options{Template: *name, Router: *router, Module: *module, Environment: *env, Directory: flags.Arg(0), FrameworkVersion: *version, FrameworkDir: *local})
	if err != nil {
		return err
	}
	entry := ""
	for _, t := range scaffold.Templates() {
		if t.Name == *name {
			entry = t.Entry
		}
	}
	fmt.Fprintf(stdout, "Created %s project in %s\n\nFrom the project directory:\n  go mod tidy\n  go tool pfw run -env %s %s\n", *name, dir, *env, entry)
	return nil
}
