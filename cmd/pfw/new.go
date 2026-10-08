package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/palma99/palma-framework/internal/scaffold"
)

const newUsage = "new -template name -module path [-router name] [-env dev] [-framework-dir path | -framework-version version] directory"

var newCommand = command{
	usage: newUsage,
	run:   runNew,
}

var templatesCommand = command{
	usage: "templates",
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
	name := flags.String("template", "hello-world", "project template (see pfw templates)")
	module := flags.String("module", "", "application Go module path")
	env := flags.String("env", "dev", "initial application environment")
	router := flags.String("router", "", "API HTTP router (default: stdlib; see pfw templates)")
	version := flags.String("framework-version", "", "published framework version")
	local := flags.String("framework-dir", "", "local framework checkout for development")
	if err := flags.Parse(args); err != nil {
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
