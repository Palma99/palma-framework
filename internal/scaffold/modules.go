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
	"golang.org/x/tools/go/ast/astutil"
)

// Module describes an optional scaffold feature. Auth is the first recipe.
type Module struct{ Name, Description string }

func Modules() []Module {
	return []Module{{"auth", "Typed session authentication with local and PostgreSQL repositories"}}
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
	return []string{"pfw.toml", bootstrap + "/compose.go", "internal/config/config.go", "internal/platform/http/server.go", "internal/item/infrastructure/http/error_mapper.go"}, nil
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
	files := map[string][]byte{}
	for _, path := range paths {
		data, err := readModuleFile(root, path)
		if err != nil {
			return root, false, fmt.Errorf("unsupported API scaffold integration at %s: %w", path, err)
		}
		files[path] = data
	}
	migrations, err := projectRelative(settings.MigrationsDir)
	if err != nil {
		return root, false, err
	}
	if err := checkModulePath(root, migrations); err != nil {
		return root, false, err
	}
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(migrations)))
	if err != nil && !os.IsNotExist(err) {
		return root, false, err
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			files[migrations+"/"+entry.Name()] = nil
		}
	}
	originals := make(map[string][]byte, len(files))
	for path, data := range files {
		originals[path] = data
	}
	prepared, err := prepareAuth(files, Options{Template: "api", Module: module.Module.Mod.Path, Router: settings.Router})
	if err != nil {
		return root, false, err
	}
	// Nil migration entries are version reservations, never files to rewrite.
	for path, data := range prepared {
		if data == nil {
			delete(prepared, path)
			delete(originals, path)
		}
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
	router, err := detectAuthRouter(files["internal/platform/http/server.go"])
	if err != nil {
		return nil, err
	}
	if options.Router != "" && options.Router != router {
		return nil, errors.New("configured router does not match the API source")
	}
	options.Router = router
	for _, change := range []struct{ path, kind string }{
		{paths[1], "bootstrap"}, {paths[2], "config"}, {paths[3], "routes"}, {paths[4], "mapper"},
	} {
		data, err := integrateAuth(files[change.path], options.Module, router, change.kind)
		if err != nil {
			return nil, fmt.Errorf("auth integration in %s: %w", change.path, err)
		}
		files[change.path] = data
	}
	moduleFiles, err := renderModuleFiles(options)
	if err != nil {
		return nil, err
	}
	// Keep the endpoint test with a relocated bootstrap package.
	if paths[1] != "internal/bootstrap/compose.go" {
		bootstrapDir := strings.TrimSuffix(paths[1], "/compose.go")
		parsed, err := parser.ParseFile(token.NewFileSet(), "compose.go", files[paths[1]], 0)
		if err != nil {
			return nil, err
		}
		for _, name := range []string{"auth_test.go", "auth_module.go"} {
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
	migrations, err := projectRelative(settings.MigrationsDir)
	if err != nil {
		return nil, err
	}
	var next int64 = 1
	for path := range files {
		if filepath.ToSlash(filepath.Dir(path)) != migrations || !strings.HasSuffix(path, ".sql") {
			continue
		}
		prefix, _, ok := strings.Cut(filepath.Base(path), "_")
		version, err := strconv.ParseInt(prefix, 10, 64)
		if !ok || err != nil || version < 1 || version == int64(^uint64(0)>>1) {
			return nil, fmt.Errorf("invalid migration filename %s", path)
		}
		if version >= next {
			next = version + 1
		}
	}
	files[fmt.Sprintf("%s/%06d_create_auth.sql", migrations, next)] = authMigration
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
	for _, root := range []string{"modules/auth/common", "modules/auth/" + options.Router} {
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

func detectAuthRouter(source []byte) (string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), "server.go", source, parser.ParseComments)
	if err != nil {
		return "", err
	}
	for _, declaration := range file.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "Routes" || fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
			continue
		}
		pointer, ok := fn.Type.Results.List[0].Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		selector, ok := pointer.X.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		if selector.Sel.Name == "Echo" && qualifiedImport(file, selector.X, "github.com/labstack/echo/v5", "echo") {
			return "echo", nil
		}
		if selector.Sel.Name == "ServeMux" && qualifiedImport(file, selector.X, "net/http", "http") {
			return "stdlib", nil
		}
	}
	return "", errors.New("auth requires Routes returning *http.ServeMux or *echo.Echo")
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
func integrateAuth(source []byte, module, router, kind string) ([]byte, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "integration.go", source, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	offset := func(pos token.Pos) int { return fset.Position(pos).Offset }
	var edits []sourceEdit
	alias, path := "", ""
	matched := 0
	switch kind {
	case "config":
		alias, path = "authconfig", module+"/internal/auth/config"
		ast.Inspect(file, func(node ast.Node) bool {
			spec, ok := node.(*ast.TypeSpec)
			if !ok || spec.Name.Name != "Config" {
				return true
			}
			structure, ok := spec.Type.(*ast.StructType)
			if !ok {
				return false
			}
			for _, field := range structure.Fields.List {
				for _, name := range field.Names {
					if name.Name == "Auth" {
						return false
					}
				}
			}
			edits = append(edits, sourceEdit{offset(structure.Fields.Opening) + 1, offset(structure.Fields.Opening) + 1, "\nAuth authconfig.Config `envPrefix:\"AUTH_\"`\n"})
			matched++
			return false
		})
	case "bootstrap":
		if file.Scope.Lookup("AuthModule") != nil {
			return nil, errors.New("AuthModule declaration already exists")
		}
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
			edits = append(edits, sourceEdit{offset(call.Lparen) + 1, offset(call.Lparen) + 1, "\nAuthModule,\n"})
			matched++
			return false
		})
	case "routes":
		alias, path = "authhttp", module+"/internal/auth/infrastructure/http"
		for _, declaration := range file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "Routes" || fn.Body == nil || fn.Recv != nil {
				continue
			}
			for _, field := range fn.Type.Params.List {
				for _, name := range field.Names {
					if name.Name == "authRoutes" {
						return nil, errors.New("authRoutes parameter already exists")
					}
				}
			}
			if len(fn.Body.List) == 0 {
				continue
			}
			ret, ok := fn.Body.List[len(fn.Body.List)-1].(*ast.ReturnStmt)
			if !ok || len(ret.Results) != 1 {
				continue
			}
			variable, ok := ret.Results[0].(*ast.Ident)
			if !ok {
				continue
			}
			prefix := ", "
			if len(fn.Type.Params.List) == 0 {
				prefix = ""
			}
			edits = append(edits,
				sourceEdit{offset(fn.Type.Params.Closing), offset(fn.Type.Params.Closing), prefix + "authRoutes *authhttp.Controller"},
				sourceEdit{offset(ret.Pos()), offset(ret.Pos()), "authRoutes.Register(" + variable.Name + ")\n"},
			)
			matched++
		}
	case "mapper":
		alias, path = "securityhttp", FrameworkModule+"/security/http"
		for _, declaration := range file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "NewErrorMapper" || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "NewMapper" || call.Ellipsis.IsValid() || !qualifiedImport(file, selector.X, FrameworkModule+"/http", "http") {
					return true
				}
				qualifier, ok := selector.X.(*ast.Ident)
				if !ok {
					return true
				}
				arguments := string(source[offset(call.Lparen)+1 : offset(call.Rparen)])
				text := "append(securityhttp.ErrorRules(), []" + qualifier.Name + ".Rule{" + arguments + "}...)..."
				edits = append(edits, sourceEdit{offset(call.Lparen) + 1, offset(call.Rparen), text})
				matched++
				return false
			})
		}
	}
	if matched != 1 {
		return nil, fmt.Errorf("expected one supported %s integration point, found %d; no files changed", kind, matched)
	}
	// Refuse alias collisions rather than changing application identifiers.
	for _, imp := range file.Imports {
		if imp.Name != nil && imp.Name.Name == alias {
			return nil, fmt.Errorf("import alias %s already exists", alias)
		}
	}
	if alias != "" && file.Scope.Lookup(alias) != nil {
		return nil, fmt.Errorf("identifier %s already exists", alias)
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, edit := range edits {
		source = append(append(append([]byte(nil), source[:edit.start]...), []byte(edit.text)...), source[edit.end:]...)
	}
	fset = token.NewFileSet()
	file, err = parser.ParseFile(fset, "integration.go", source, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	if path != "" {
		astutil.AddNamedImport(fset, file, alias, path)
	}
	var result bytes.Buffer
	if err := format.Node(&result, fset, file); err != nil {
		return nil, err
	}
	return format.Source(result.Bytes())
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

var authMigration = []byte(`-- +pfw Up
CREATE TABLE pfw_auth_users (
 id TEXT PRIMARY KEY,
 roles JSONB NOT NULL DEFAULT '[]'::jsonb,
 disabled BOOLEAN NOT NULL DEFAULT false
);
CREATE TABLE pfw_auth_sessions (
 session_hash CHAR(64) PRIMARY KEY,
 user_id TEXT NOT NULL REFERENCES pfw_auth_users(id),
 expires_at TIMESTAMPTZ NOT NULL,
 revoked BOOLEAN NOT NULL DEFAULT false
);

-- +pfw Down
DROP TABLE pfw_auth_sessions;
DROP TABLE pfw_auth_users;
`)
