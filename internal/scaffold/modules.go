package scaffold

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/pelletier/go-toml/v2"
	"golang.org/x/mod/modfile"
)

// Module describes an optional scaffold feature. Auth is the first recipe.
type Module struct{ Name, Description string }

func Modules() []Module {
	return []Module{{"auth", "Application principal and dependency injection extension point"}}
}

type moduleSettings struct {
	Scaffold      string   `toml:"scaffold"`
	Router        string   `toml:"router"`
	Modules       []string `toml:"modules"`
	Bootstrap     string   `toml:"bootstrap"`
	Main          string   `toml:"main"`
	EnvDir        string   `toml:"env_dir"`
	MigrationsDir string   `toml:"migrations_dir"`
}

func settingsForModule(data []byte) (moduleSettings, error) {
	var settings moduleSettings
	if err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&settings); err != nil {
		return settings, fmt.Errorf("parse pfw.toml: %w", err)
	}
	if settings.Scaffold != "" && settings.Scaffold != "api" {
		return settings, errors.New("auth requires an API scaffold")
	}
	if settings.Bootstrap == "" {
		settings.Bootstrap = "./internal/bootstrap"
	}
	if settings.MigrationsDir == "" {
		settings.MigrationsDir = "./migrations"
	}
	return settings, nil
}

func projectRelative(path string) (string, error) {
	clean := filepath.Clean(path)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("module paths must stay inside the project: %s", path)
	}
	return filepath.ToSlash(clean), nil
}

func authIntegrationPaths(settings moduleSettings) ([]string, error) {
	bootstrap, err := projectRelative(settings.Bootstrap)
	if err != nil {
		return nil, err
	}
	return []string{"pfw.toml", bootstrap + "/compose.go"}, nil
}

// AddModule locates the module root, prepares all edits, then installs the recipe.
// Existing integration files are edited structurally. New files never overwrite
// existing files. Repeating an installed recipe is a no-op. A failed write rolls
// back completed writes; a process crash may still require manual recovery.
func AddModule(directory, name string) (string, bool, error) {
	if name != "auth" {
		return "", false, fmt.Errorf("unknown module %q; available modules: auth", name)
	}
	root, err := filepath.Abs(directory)
	if err != nil {
		return "", false, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", false, err
	}
	for {
		if info, statErr := os.Lstat(filepath.Join(root, "go.mod")); statErr == nil {
			if !info.Mode().IsRegular() {
				return root, false, errors.New("go.mod must be a regular file")
			}
			break
		} else if !os.IsNotExist(statErr) {
			return root, false, statErr
		}
		parent := filepath.Dir(root)
		if parent == root {
			return "", false, errors.New("cannot find module root (go.mod)")
		}
		root = parent
	}
	lockPath := filepath.Join(root, ".pfw-add.lock")
	lock, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return root, false, fmt.Errorf("reserve module installation: %w", err)
	}
	lock.Close()
	defer os.Remove(lockPath)
	manifest, err := readModuleFile(root, "pfw.toml")
	if err != nil {
		return root, false, fmt.Errorf("auth requires an API scaffold with pfw.toml: %w", err)
	}
	settings, err := settingsForModule(manifest)
	if err != nil {
		return root, false, err
	}
	for _, installed := range settings.Modules {
		if installed == name {
			return root, false, nil
		}
	}
	moduleData, err := readModuleFile(root, "go.mod")
	if err != nil {
		return root, false, err
	}
	module, err := modfile.Parse("go.mod", moduleData, nil)
	if err != nil || module.Module == nil {
		return root, false, errors.New("invalid application go.mod")
	}
	if module.Module.Mod.Path == FrameworkModule {
		return root, false, errors.New("auth requires an application module")
	}
	paths, err := authIntegrationPaths(settings)
	if err != nil {
		return root, false, err
	}
	files := map[string][]byte{"go.mod": moduleData}
	for _, path := range paths {
		data, err := readModuleFile(root, path)
		if err != nil {
			return root, false, fmt.Errorf("unsupported API scaffold integration at %s: %w", path, err)
		}
		files[path] = data
	}
	originals := make(map[string][]byte, len(files))
	for path, data := range files {
		originals[path] = data
	}
	prepared, err := prepareAuth(files, Options{Template: "api", Module: module.Module.Mod.Path, Router: settings.Router})
	if err != nil {
		return root, false, err
	}
	if err := installModuleFiles(root, prepared, originals); err != nil {
		return root, false, err
	}
	return root, true, nil
}

func prepareAuth(files map[string][]byte, options Options) (map[string][]byte, error) {
	settings, err := settingsForModule(files["pfw.toml"])
	if err != nil {
		return nil, err
	}
	paths, err := authIntegrationPaths(settings)
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		if files[path] == nil {
			return nil, fmt.Errorf("auth requires integration file %s", path)
		}
	}
	data, err := integrateAuth(files[paths[1]])
	if err != nil {
		return nil, fmt.Errorf("auth integration in %s: %w", paths[1], err)
	}
	files[paths[1]] = data

	moduleFiles, err := renderModuleFiles(options)
	if err != nil {
		return nil, err
	}
	// Keep the module declaration with a relocated bootstrap package.
	if paths[1] != "internal/bootstrap/compose.go" {
		bootstrapDir := strings.TrimSuffix(paths[1], "/compose.go")
		parsed, err := parser.ParseFile(token.NewFileSet(), "compose.go", files[paths[1]], 0)
		if err != nil {
			return nil, err
		}
		for _, name := range []string{"auth_module.go"} {
			data := moduleFiles["internal/bootstrap/"+name]
			data = bytes.ReplaceAll(data, []byte("package bootstrap"), []byte("package "+parsed.Name.Name))
			data = bytes.ReplaceAll(data, []byte(options.Module+"/internal/bootstrap"), []byte(options.Module+"/"+bootstrapDir))
			moduleFiles[bootstrapDir+"/"+name] = data
			delete(moduleFiles, "internal/bootstrap/"+name)
		}
	}
	for path, data := range moduleFiles {
		if _, exists := files[path]; exists {
			return nil, fmt.Errorf("auth file already exists: %s", path)
		}
		files[path] = data
	}
	settings.Modules = append(settings.Modules, "auth")
	var names []string
	for _, name := range settings.Modules {
		names = append(names, strconv.Quote(name))
	}
	line := "modules = [" + strings.Join(names, ", ") + "]"
	manifest := string(files["pfw.toml"])
	pattern := regexp.MustCompile(`(?m)^modules\s*=.*$`)
	if pattern.MatchString(manifest) {
		// Only rewrite an ordinary single-line array, not a customized multiline value.
		if !strings.Contains(pattern.FindString(manifest), "]") {
			return nil, errors.New("unsupported multiline modules setting in pfw.toml")
		}
		manifest = pattern.ReplaceAllString(manifest, line)
	} else {
		manifest += "\n" + line + "\n"
	}
	files["pfw.toml"] = []byte(manifest)
	if _, err := settingsForModule(files["pfw.toml"]); err != nil {
		return nil, err
	}
	return files, nil
}

func renderModuleFiles(options Options) (map[string][]byte, error) {
	files := map[string][]byte{}
	for _, root := range []string{"modules/auth/common"} {
		err := fs.WalkDir(bundled, root, func(path string, entry fs.DirEntry, walkErr error) error {
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
					return fmt.Errorf("auth template %s: %w", name, err)
				}
			}
			files[name] = content
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}

type sourceEdit struct {
	start, end int
	text       string
}

func qualifiedImport(file *ast.File, expression ast.Expr, path, defaultName string) bool {
	id, ok := expression.(*ast.Ident)
	if !ok {
		return false
	}
	for _, imp := range file.Imports {
		importPath, err := strconv.Unquote(imp.Path.Value)
		if err != nil || importPath != path {
			continue
		}
		name := defaultName
		if imp.Name != nil {
			name = imp.Name.Name
		}
		return id.Name == name
	}
	return false
}

// Edit only named integration points; preserve surrounding application code.
func integrateAuth(source []byte) ([]byte, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "compose.go", source, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	if file.Scope.Lookup("AuthModule") != nil {
		return nil, errors.New("AuthModule declaration already exists")
	}
	var edits []sourceEdit
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		fun := call.Fun
		if index, ok := fun.(*ast.IndexExpr); ok {
			fun = index.X
		}
		selector, ok := fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "BuildWithCleanup" || !qualifiedImport(file, selector.X, FrameworkModule, "pfw") {
			return true
		}
		position := fset.Position(call.Lparen).Offset + 1
		edits = append(edits, sourceEdit{position, position, "\nAuthModule,\n"})
		return false
	})
	if len(edits) != 1 {
		return nil, fmt.Errorf("expected one supported bootstrap integration point, found %d; no files changed", len(edits))
	}
	edit := edits[0]
	source = append(append(append([]byte(nil), source[:edit.start]...), []byte(edit.text)...), source[edit.end:]...)
	return format.Source(source)
}

func checkModulePath(root, relative string) error {
	clean, err := projectRelative(relative)
	if err != nil {
		return err
	}
	path := root
	for _, part := range strings.Split(filepath.FromSlash(clean), string(filepath.Separator)) {
		path = filepath.Join(path, part)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("auth refuses symlink path %s", path)
		}
	}
	return nil
}

func readModuleFile(root, relative string) ([]byte, error) {
	if err := checkModulePath(root, relative); err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
}

func installModuleFiles(root string, files, originals map[string][]byte) (err error) {
	var paths []string
	for path, data := range files {
		if old, exists := originals[path]; exists && bytes.Equal(old, data) {
			continue
		}
		if err := checkModulePath(root, path); err != nil {
			return err
		}
		current, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if old, exists := originals[path]; exists {
			if readErr != nil || !bytes.Equal(old, current) {
				return fmt.Errorf("integration file changed during preparation: %s", path)
			}
		} else if !os.IsNotExist(readErr) {
			return fmt.Errorf("auth file already exists or is unreadable: %s", path)
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var written, directories []string
	defer func() {
		if err == nil {
			return
		}
		for i := len(written) - 1; i >= 0; i-- {
			path := written[i]
			full := filepath.Join(root, filepath.FromSlash(path))
			if old, exists := originals[path]; exists {
				err = errors.Join(err, os.WriteFile(full, old, 0644))
			} else {
				err = errors.Join(err, os.Remove(full))
			}
		}
		for i := len(directories) - 1; i >= 0; i-- {
			_ = os.Remove(directories[i])
		}
	}()
	for _, path := range paths {
		full := filepath.Join(root, filepath.FromSlash(path))
		var missing []string
		for dir := filepath.Dir(full); dir != root; dir = filepath.Dir(dir) {
			if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
				missing = append(missing, dir)
			} else {
				break
			}
		}
		for i := len(missing) - 1; i >= 0; i-- {
			if err = os.Mkdir(missing[i], 0755); err != nil {
				return err
			}
			directories = append(directories, missing[i])
		}
		if _, exists := originals[path]; exists {
			if err = replaceModuleFile(full, files[path]); err != nil {
				return err
			}
			written = append(written, path)
		} else {
			var file *os.File
			file, err = os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
			if err != nil {
				return err
			}
			written = append(written, path)
			_, writeErr := file.Write(files[path])
			err = errors.Join(writeErr, file.Close())
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func replaceModuleFile(path string, content []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".pfw-add-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(content)
	err = errors.Join(writeErr, file.Chmod(info.Mode().Perm()), file.Close())
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
