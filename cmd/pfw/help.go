package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
)

type helpRow struct{ label, description string }

type helpWriter struct {
	w     io.Writer
	color bool
	err   error
}

func (h *helpWriter) print(format string, values ...any) {
	if h.err == nil {
		_, h.err = fmt.Fprintf(h.w, format, values...)
	}
}
func (h *helpWriter) heading(text string) {
	if h.color {
		text = "\x1b[1;36m" + text + "\x1b[0m"
	}
	h.print("\n%s\n", text)
}

func (h *helpWriter) examples(examples []string) {
	for _, example := range examples {
		lines := wrapHelp(example, 72)
		for index, line := range lines {
			indent, continuation := "  ", ""
			if index > 0 {
				indent = "    "
			}
			if index < len(lines)-1 {
				continuation = " \\"
			}
			h.print("%s%s%s\n", indent, line, continuation)
		}
	}
}
func (h *helpWriter) rows(rows []helpRow) {
	width := 0
	for _, row := range rows {
		if len(row.label) > width {
			width = len(row.label)
		}
	}
	for _, row := range rows {
		lines := wrapHelp(row.description, 80-width-4)
		label := fmt.Sprintf("%-*s", width, row.label)
		if h.color {
			label = "\x1b[36m" + label + "\x1b[0m"
		}
		h.print("  %s  %s\n", label, lines[0])
		for _, line := range lines[1:] {
			h.print("  %-*s  %s\n", width, "", line)
		}
	}
}

func wrapHelp(text string, width int) []string {
	if width < 20 {
		width = 20
	}
	lines := []string{""}
	for _, word := range strings.Fields(text) {
		last := len(lines) - 1
		if lines[last] != "" && len(lines[last])+1+len(word) > width {
			lines = append(lines, word)
		} else {
			if lines[last] != "" {
				lines[last] += " "
			}
			lines[last] += word
		}
	}
	return lines
}

func writeHelp(w io.Writer) error {
	h := helpWriter{w: w, color: useColor("auto", w)}
	h.print("Palma Framework · %s\n", currentVersion())
	h.print("Build-time dependency injection and application tools for Go.\n")
	h.heading("Usage")
	h.print("  pfw <command> [options]\n  pfw help [command]\n")
	h.heading("Commands")
	var names []string
	for name := range commands {
		names = append(names, name)
	}
	sort.Strings(names)
	var rows []helpRow
	for _, name := range names {
		rows = append(rows, helpRow{name, commands[name].description})
	}
	rows = append(rows, helpRow{"help", "Show general help or help for a command"})
	h.rows(rows)
	h.heading("Options")
	h.rows([]helpRow{{"-h, --help", "Show help"}, {"--version", "Show the installed CLI version"}})
	h.heading("Examples")
	h.examples([]string{
		"pfw new -template api -router echo -module example.com/myapi ./myapi",
		"go tool pfw run -env dev ./cmd/api",
		"go tool pfw inspect -env dev ./internal/bootstrap",
	})
	h.print("\nRun 'pfw <command> --help' for options and examples.\n")
	return h.err
}

func writeCommandHelp(w io.Writer, name string, flags *flag.FlagSet) error {
	cmd := commands[name]
	h := helpWriter{w: w, color: useColor("auto", w)}
	h.print("pfw %s\n%s\n", name, cmd.description)
	h.heading("Usage")
	h.print("  pfw %s\n", cmd.usage)
	h.heading("Options")
	var rows []helpRow
	if flags != nil {
		flags.VisitAll(func(f *flag.Flag) {
			argument, description := flag.UnquoteUsage(f)
			label := "-" + f.Name
			if argument != "" {
				label += " " + argument
			}
			if f.DefValue != "" && f.DefValue != "false" {
				description += " (default: " + f.DefValue + ")"
			}
			rows = append(rows, helpRow{label, description})
		})
	}
	rows = append(rows, helpRow{"-h, --help", "Show help for this command"})
	h.rows(rows)
	if len(cmd.examples) > 0 {
		h.heading("Examples")
		h.examples(cmd.examples)
	}
	if len(cmd.notes) > 0 {
		h.heading("Notes")
		for _, note := range cmd.notes {
			for _, line := range wrapHelp(note, 78) {
				h.print("  %s\n", line)
			}
			h.print("\n")
		}
	} else {
		h.print("\n")
	}
	return h.err
}

func parseCommandFlags(name string, flags *flag.FlagSet, args []string, stdout io.Writer) error {
	// Flag parsing is silent; help is a successful stdout response, while errors
	// are returned to main for a single stderr diagnostic with a useful hint.
	flags.SetOutput(io.Discard)
	flags.Usage = func() {}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			if writeErr := writeCommandHelp(stdout, name, flags); writeErr != nil {
				return writeErr
			}
			return flag.ErrHelp
		}
		return fmt.Errorf("%w\n\nRun 'pfw %s --help' for usage", err, name)
	}
	return nil
}
