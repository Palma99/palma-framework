package main

import (
	"context"
	"fmt"
	"io"
	"runtime/debug"

	"github.com/palma99/palma-framework/internal/scaffold"
)

// Release binaries set version with -ldflags "-X main.version=<tag>".
var version = "dev"

func currentVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Path == scaffold.FrameworkModule && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

var versionCommand = command{
	usage:       "version",
	description: "Show the installed CLI version",
	examples:    []string{"pfw version", "go tool pfw version"},
	run: func(_ context.Context, args []string, stdout, _ io.Writer) error {
		if len(args) != 0 {
			return fmt.Errorf("usage: pfw version")
		}
		_, err := fmt.Fprintf(stdout, "pfw %s\n", currentVersion())
		return err
	},
}
