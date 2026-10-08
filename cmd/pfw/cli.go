package main

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Each command owns its flags and execution; the dispatcher only selects it.
type command struct {
	usage string
	run   func(context.Context, []string, io.Writer, io.Writer) error
}

var commands = map[string]command{
	"version":   versionCommand,
	"new":       newCommand,
	"templates": templatesCommand,
	"generate":  generateCommand,
	"inspect":   inspectCommand,
	"run":       runCommand,
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return usageError()
	}
	selected, ok := commands[args[0]]
	if !ok {
		return usageError()
	}
	return selected.run(context.Background(), args[1:], stdout, stderr)
}

func usageError() error {
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	sort.Strings(names)
	usages := make([]string, 0, len(names))
	for _, name := range names {
		usages = append(usages, "pfw "+commands[name].usage)
	}
	return fmt.Errorf("usage: %s", strings.Join(usages, " | "))
}
