// Package scaffold creates standalone Go projects from bundled templates.
package scaffold

import (
	"bytes"
	"embed"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	pfw "github.com/palma99/palma-framework"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

const FrameworkModule = "github.com/palma99/palma-framework"

//go:embed all:templates all:routers all:modules all:docker
var bundled embed.FS

type Template struct{ Name, Description, Entry string }

func Templates() []Template {
	return []Template{
		{"hello-world", "Minimal application that prints Hello, world!", "./cmd/app"},
		{"api", "HTTP API with PostgreSQL storage in every environment", "./cmd/api"},
	}
}

type Options struct {
	Template, Directory, Module, Environment string
	FrameworkVersion, FrameworkDir           string
	Router                                   string
	Auth                                     bool
	Docker                                   bool
}

type Dependency struct{ Module, Version string }

// Router describes an HTTP template overlay and its direct dependencies.
// Add a registry entry and routers/<name> files to support another transport.
type Router struct {
	Name         string
	Dependencies []Dependency
}

func Routers() []Router {
	return []Router{
		{Name: "stdlib"},
		{Name: "echo", Dependencies: []Dependency{{"github.com/labstack/echo/v5", "v5.4.0"}}},
	}
}

func routerByName(name string) (Router, error) {
	var names []string
	for _, router := range Routers() {
		if router.Name == name {
			return router, nil
		}
		names = append(names, router.Name)
	}
	return Router{}, fmt.Errorf("unknown router %q; supported routers: %s", name, strings.Join(names, ", "))
}

// Create renders everything before writing. The destination must not exist.
// Dependencies and generated DI code are prepared by the user's next Go commands.
func Create(options Options) (string, error) {
	known := false
	for _, t := range Templates() {
		known = known || t.Name == options.Template
	}
	if !known {
		return "", fmt.Errorf("unknown template %q; use pfw templates", options.Template)
	}
	if options.Environment == "" {
		options.Environment = "dev"
		if options.Template == "api" {
			options.Environment = "local"
		}
	}
	if options.Template == "api" && options.Environment != "local" && options.Environment != "staging" && options.Environment != "prod" {
		return "", fmt.Errorf("api initial environment must be local, staging or prod")
	}
	if options.Template == "api" {
		if options.Router == "" {
			options.Router = "stdlib"
		}
		if _, err := routerByName(options.Router); err != nil {
			return "", err
		}
	} else if options.Router != "" {
		return "", fmt.Errorf("-router is only available for the api template")
	}
	if options.Auth && options.Template != "api" {
		return "", fmt.Errorf("-auth is only available for the api template")
	}
	if err := module.CheckPath(options.Module); err != nil {
		return "", fmt.Errorf("invalid module path: %w", err)
	}
	if options.Module == FrameworkModule {
		return "", fmt.Errorf("application module must differ from the framework module")
	}
	if err := pfw.Environment(options.Environment).Validate(); err != nil {
		return "", err
	}
	if options.Directory == "" {
		return "", fmt.Errorf("destination directory is required")
	}
	dir, err := filepath.Abs(options.Directory)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		return "", fmt.Errorf("destination must not exist: %s", dir)
	}
	if options.FrameworkDir != "" {
		absolute, err := filepath.Abs(options.FrameworkDir)
		if err != nil {
			return "", err
		}
		options.FrameworkDir = absolute
	}
	goMod, err := moduleFile(options)
	if err != nil {
		return "", err
	}
	files := map[string][]byte{"go.mod": goMod}
	roots := []string{"templates/" + options.Template}
	if options.Template == "api" {
		roots = append(roots, "routers/"+options.Router)
	}
	if options.Docker {
		roots = append(roots, "docker")
	}
	for _, root := range roots {
		err = fs.WalkDir(bundled, root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil || entry.IsDir() {
				return walkErr
			}
			data, err := bundled.ReadFile(path)
			if err != nil {
				return err
			}
			tmpl, err := template.New(path).Option("missingkey=error").Parse(string(data))
			if err != nil {
				return err
			}
			var rendered bytes.Buffer
			if err := tmpl.Execute(&rendered, options); err != nil {
				return err
			}
			name := strings.TrimSuffix(strings.TrimPrefix(path, root+"/"), ".tmpl")
			content := rendered.Bytes()
			if strings.HasSuffix(name, ".go") {
				content, err = format.Source(content)
				if err != nil {
					return fmt.Errorf("template %s: %w", name, err)
				}
			}
			files[name] = content
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	if options.Auth {
		files, err = prepareAuth(files, options)
		if err != nil {
			return "", err
		}
	}
	if options.Docker {
		files[".gitignore"] = append(files[".gitignore"], []byte("\n/.env\n/tmp/\n")...)
		files["README.md"] = append(files["README.md"], []byte("\n## Docker development\n\nRun `docker compose up --build`. See [docker/README.md](docker/README.md) for Air, environment selection and development services.\n")...)
	}
	// Reserve the destination exclusively after every template has rendered.
	if err := os.Mkdir(dir, 0755); err != nil {
		return "", err
	}
	complete := false
	defer func() {
		if !complete {
			os.RemoveAll(dir)
		}
	}()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return "", err
		}
		if err := os.WriteFile(path, content, 0644); err != nil {
			return "", err
		}
	}
	complete = true
	return dir, nil
}

func moduleFile(options Options) ([]byte, error) {
	file := new(modfile.File)
	if err := file.AddModuleStmt(options.Module); err != nil {
		return nil, err
	}
	if err := file.AddGoStmt("1.26.0"); err != nil {
		return nil, err
	}
	version := options.FrameworkVersion
	if options.FrameworkDir != "" {
		if version != "" {
			return nil, fmt.Errorf("use either -framework-dir or -framework-version")
		}
		dir, err := filepath.Abs(options.FrameworkDir)
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err != nil {
			return nil, fmt.Errorf("read local framework module: %w", err)
		}
		local, err := modfile.Parse("go.mod", data, nil)
		if err != nil || local.Module == nil || local.Module.Mod.Path != FrameworkModule {
			return nil, fmt.Errorf("-framework-dir must contain module %s", FrameworkModule)
		}
		if err := file.AddReplace(FrameworkModule, "", dir, ""); err != nil {
			return nil, err
		}
		version = "v0.0.0"
	}
	if version == "" {
		return nil, fmt.Errorf("development CLI requires -framework-dir <checkout> or -framework-version <published-version>")
	}
	if err := module.Check(FrameworkModule, version); err != nil {
		return nil, fmt.Errorf("invalid framework version: %w", err)
	}
	if err := file.AddRequire(FrameworkModule, version); err != nil {
		return nil, err
	}
	if options.Template == "api" {
		if err := file.AddRequire("github.com/jackc/pgx/v5", "v5.11.0"); err != nil {
			return nil, err
		}
	}
	if options.Router != "" {
		router, err := routerByName(options.Router)
		if err != nil {
			return nil, err
		}
		for _, dependency := range router.Dependencies {
			if err := file.AddRequire(dependency.Module, dependency.Version); err != nil {
				return nil, err
			}
		}
	}
	if err := file.AddTool(FrameworkModule + "/cmd/pfw"); err != nil {
		return nil, err
	}
	return file.Format()
}
