package generate

import (
	"fmt"
	"io"
	"regexp"
	"strings"
)

type TextOptions struct {
	FullNames bool
	Color     bool
}

// Match qualified names while preserving quoted literals in anonymous Go types.
var displayTokens = regexp.MustCompile("\"(?:\\\\.|[^\"\\\\])*\"|`[^`]*`|[\\pL\\pN_./-]+\\.[\\pL_][\\pL\\pN_]*")

func shortNames(init InitializerReport) func(string) string {
	packages := map[string]bool{init.Package: true}
	collect := func(s string) {
		for _, token := range displayTokens.FindAllString(s, -1) {
			if token[0] == '"' || token[0] == '`' {
				continue
			}
			packages[token[:strings.LastIndex(token, ".")]] = true
		}
	}
	collect(init.Root)
	for _, input := range init.Inputs {
		collect(input.Type)
	}
	for _, p := range init.Providers {
		collect(p.Name)
		collect(p.Output)
		for _, input := range p.Dependencies {
			collect(input)
		}
	}
	for _, binding := range init.Bindings {
		collect(binding.Consumer)
		collect(binding.Interface)
		collect(binding.SelectedType)
		collect(binding.Provider)
	}
	aliases := make(map[string]string)
	displayPaths := make(map[string]string)
	minimum := make(map[string]int)
	for path := range packages {
		parts := strings.Split(path, "/")
		last := parts[len(parts)-1]
		version := len(parts) > 1 && len(last) > 1 && last[0] == 'v'
		if version {
			for _, r := range last[1:] {
				version = version && r >= '0' && r <= '9'
			}
		}
		displayPaths[path] = path
		if version {
			base := strings.Join(parts[:len(parts)-1], "/")
			multiple := false
			for other := range packages {
				if other != path && (other == base || strings.HasPrefix(other, base+"/v")) {
					multiple = true
					break
				}
			}
			if multiple {
				minimum[path] = 2
			} else {
				displayPaths[path] = base
			}
		}
	}
	for path := range packages {
		parts := strings.Split(displayPaths[path], "/")
		start := minimum[path]
		if start == 0 {
			start = 1
		}
		for n := start; n <= len(parts); n++ {
			candidate := strings.Join(parts[len(parts)-n:], "/")
			collision := false
			for other := range packages {
				if other != path && (displayPaths[other] == candidate || strings.HasSuffix(displayPaths[other], "/"+candidate)) {
					collision = true
					break
				}
			}
			if !collision || n == len(parts) {
				aliases[path] = candidate
				break
			}
		}
	}
	return func(s string) string {
		return displayTokens.ReplaceAllStringFunc(s, func(token string) string {
			if token[0] == '"' || token[0] == '`' {
				return token
			}
			index := strings.LastIndex(token, ".")
			if alias, ok := aliases[token[:index]]; ok {
				return alias + token[index:]
			}
			return token
		})
	}
}

// WriteText renders a compact execution view; JSON retains full report metadata.
func (report Report) WriteText(w io.Writer, settings ...TextOptions) error {
	options := TextOptions{}
	if len(settings) > 0 {
		options = settings[0]
	}
	paint := func(code, s string) string {
		if !options.Color {
			return s
		}
		return "\x1b[" + code + "m" + s + "\x1b[0m"
	}
	var out strings.Builder
	for _, init := range report.Initializers {
		short := shortNames(init)
		if options.FullNames {
			short = func(s string) string { return s }
		}
		modules := make(map[string]string)
		reserved := make(map[string]bool)
		for _, m := range init.Modules {
			if !strings.HasPrefix(m.Name, "module@") {
				reserved[m.Name] = true
			}
		}
		counter := 0
		for _, m := range init.Modules {
			label := m.Name
			if !options.FullNames && strings.HasPrefix(label, "module@") {
				for {
					counter++
					label = fmt.Sprintf("module%d", counter)
					if !reserved[label] {
						reserved[label] = true
						break
					}
				}
			}
			modules[m.Name] = label
		}
		moduleList := func(names []string) string {
			var labels []string
			for _, name := range names {
				label, ok := modules[name]
				if !ok {
					label = name
				}
				labels = append(labels, label)
			}
			return strings.Join(labels, ", ")
		}
		providers := make(map[string]ProviderReport)
		unused, excluded := 0, 0
		for _, p := range init.Providers {
			providers[p.Name] = p
			if p.Status == "unused" {
				unused++
			}
			if p.Status == "excluded" {
				excluded++
			}
		}
		fmt.Fprintf(&out, "%s\n", paint("1;36", short(init.Package+"."+init.Name)))
		if init.Environment != "" {
			fmt.Fprintf(&out, "  Environment: %s\n", paint("1;33", init.Environment))
		}
		fmt.Fprintf(&out, "  Root: %s\n  Global AutoBind: %t\n  %d used · %d unused · %d excluded\n\n", short(init.Root), init.AutoBind, len(init.ConstructionOrder), unused, excluded)
		if len(init.Inputs) > 0 {
			out.WriteString(paint("1;36", "Inputs") + "\n")
			for _, input := range init.Inputs {
				fmt.Fprintf(&out, "  %s: %s\n", input.Name, short(input.Type))
			}
			out.WriteString("\n")
		}
		out.WriteString(paint("1;36", "Construction order") + "\n")
		for n, name := range init.ConstructionOrder {
			p := providers[name]
			fmt.Fprintf(&out, "  %s %s → %s\n", paint("32", fmt.Sprintf("%d.", n+1)), short(name), short(p.Output))
			var badges []string
			for _, origin := range p.Origins {
				badges = append(badges, origin)
			}
			if len(p.Modules) > 0 {
				badges = append(badges, "module: "+moduleList(p.Modules))
			}
			if p.Fallible {
				badges = append(badges, "may fail")
			}
			if p.Cleanup {
				badges = append(badges, "cleanup")
			}
			if p.Override {
				badges = append(badges, "override")
			}
			if len(badges) > 0 {
				fmt.Fprintf(&out, "     %s\n", paint("2", strings.Join(badges, " · ")))
			}
			for _, dependency := range p.Dependencies {
				fmt.Fprintf(&out, "     ← %s\n", short(dependency))
			}
			if options.FullNames {
				fmt.Fprintf(&out, "     source: %s\n", p.Source)
			}
		}
		if len(init.ConstructionOrder) == 0 {
			out.WriteString("  (root supplied by caller)\n")
		}
		out.WriteString("\n")
		if len(init.Bindings) > 0 {
			out.WriteString(paint("1;36", "Interface bindings") + "\n")
			for _, binding := range init.Bindings {
				consumer := binding.Consumer
				if consumer == "" {
					consumer = "root"
				}
				mode := binding.Mode
				code := "34"
				if mode == "automatic" {
					code = "33"
				}
				fmt.Fprintf(&out, "  %s → %s  %s\n", short(binding.Interface), short(binding.SelectedType), paint(code, "["+mode+"]"))
				fmt.Fprintf(&out, "     for %s\n", short(consumer))
				if len(binding.AutoBindScopes) > 0 {
					fmt.Fprintf(&out, "     AutoBind: %s\n", moduleList(binding.AutoBindScopes))
				}
			}
			out.WriteString("\n")
		}
		if len(init.Modules) > 0 {
			out.WriteString(paint("1;36", "Modules") + "\n")
			for _, module := range init.Modules {
				policy := "explicit bindings"
				if module.AutoBind {
					policy = "AutoBind=true"
				}
				fmt.Fprintf(&out, "  %s · %s · %d providers\n", modules[module.Name], policy, len(module.Providers))
			}
			out.WriteString("\n")
		}
		if unused+excluded > 0 {
			out.WriteString(paint("1;36", "Not constructed") + "\n")
			for _, p := range init.Providers {
				if p.Status == "unused" || p.Status == "excluded" {
					code := "33"
					if p.Status == "excluded" {
						code = "31"
					}
					fmt.Fprintf(&out, "  %s %s → %s\n", paint(code, "["+p.Status+"]"), short(p.Name), short(p.Output))
				}
			}
			out.WriteString("\n")
		}
		out.WriteString(paint("1;36", "Cleanup order (reverse)") + "\n")
		if len(init.CleanupOrder) == 0 {
			out.WriteString("  (none)\n")
		}
		for n, name := range init.CleanupOrder {
			fmt.Fprintf(&out, "  %d. %s\n", n+1, short(name))
		}
		out.WriteString("\n")
	}
	_, err := io.WriteString(w, out.String())
	return err
}
