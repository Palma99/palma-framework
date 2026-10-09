package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
)

// Each command owns its flags and execution; the dispatcher only selects it.
type command struct {
	usage       string
	description string
	examples    []string
	notes       []string
	run         func(context.Context, []string, io.Writer, io.Writer) error
}

var commands map[string]command

func init() {
	commands = map[string]command{
		"version":   versionCommand,
		"new":       newCommand,
		"templates": templatesCommand,
		"generate":  generateCommand,
		"inspect":   inspectCommand,
		"run":       runCommand,
		"migrate":   migrateCommand,
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return writeHelp(stdout)
	}
	if args[0] == "-h" || args[0] == "--help" || args[0] == "-help" || args[0] == "help" {
		if len(args) == 1 {
			return writeHelp(stdout)
		}
		if args[0] != "help" || len(args) != 2 {
			return fmt.Errorf("usage: pfw help [command]")
		}
		args = []string{args[1], "--help"}
	}
	if args[0] == "--version" && len(args) == 1 {
		args = []string{"version"}
	}
	selected, ok := commands[args[0]]
	if !ok {
		return fmt.Errorf("unknown command %q\n\nRun 'pfw --help' to list available commands", args[0])
	}
	if (args[0] == "templates" || args[0] == "version") && len(args) == 2 && (args[1] == "-h" || args[1] == "--help" || args[1] == "-help") {
		return writeCommandHelp(stdout, args[0], nil)
	}
	err := selected.run(context.Background(), args[1:], stdout, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	return err
}
