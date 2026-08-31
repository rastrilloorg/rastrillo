package manifest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"amadan.net/rastrillo/rastrillo"
)

// rastrilloImportPath is the import path a Go manifest must use to
// declare a rastrillo.Resource var — fixed, since the type lives here.
const rastrilloImportPath = "amadan.net/rastrillo/rastrillo"

// goVar names one exported package-level rastrillo.Resource var found
// by goEval, paired with the .go file it was declared in.
type goVar struct {
	name string
	file string
}

// goEval discovers exported package-level vars of type
// rastrillo.Resource in dir's *.go files (skipping _test.go, via
// go/ast — the type must be spelled rastrillo.Resource, selector
// resolved from each file's own import of rastrilloImportPath),
// generates a driver program that imports dir's package and prints
// the vars' evaluated values as JSON, runs it with `go run` in
// moduleRoot, and decodes the result. Each decoded resource is paired
// with the file its var was declared in, so Load can attribute
// duplicate/validation errors to the precise .go file rather than the
// directory. A compile error in the driver run is returned verbatim —
// a typo'd manifest IS that compile error. No matching vars returns
// nil without running anything.
func goEval(moduleRoot, dir string) ([]source, error) {
	vars, err := findResourceVars(dir)
	if err != nil {
		return nil, err
	}
	if len(vars) == 0 {
		return nil, nil
	}

	modulePath, err := readModulePath(moduleRoot)
	if err != nil {
		return nil, err
	}
	importPath, err := packageImportPath(moduleRoot, dir, modulePath)
	if err != nil {
		return nil, err
	}

	out, err := runDriver(moduleRoot, importPath, vars)
	if err != nil {
		return nil, err
	}

	var resources []rastrillo.Resource
	if err := json.Unmarshal(out, &resources); err != nil {
		return nil, fmt.Errorf("decode Go manifest driver output: %w", err)
	}
	if len(resources) != len(vars) {
		return nil, fmt.Errorf("Go manifest driver returned %d resource(s), expected %d", len(resources), len(vars))
	}

	sources := make([]source, len(resources))
	for i, r := range resources {
		sources[i] = source{r, vars[i].file}
	}
	return sources, nil
}

// findResourceVars parses dir's *.go files (excluding _test.go) and
// returns every exported package-level var whose type is
// rastrillo.Resource, in file-then-declaration order. A missing dir is
// not an error: manifests are optional, per source.
func findResourceVars(dir string) ([]goVar, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var paths []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		paths = append(paths, filepath.Join(dir, name))
	}
	sort.Strings(paths)

	fset := token.NewFileSet()
	var vars []goVar
	for _, path := range paths {
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}

		alias := rastrilloAlias(f)
		if alias == "" {
			continue
		}

		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range resourceNamesInSpec(vs, alias) {
					vars = append(vars, goVar{name: name, file: path})
				}
			}
		}
	}
	return vars, nil
}

// rastrilloAlias returns the local name f's imports use for
// rastrilloImportPath (its declared alias, or "rastrillo" for an
// unaliased import), or "" if the file doesn't import it.
func rastrilloAlias(f *ast.File) string {
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || path != rastrilloImportPath {
			continue
		}
		if imp.Name == nil {
			return "rastrillo"
		}
		switch imp.Name.Name {
		case "_", ".":
			return "" // not a form that lets us spell alias.Resource
		default:
			return imp.Name.Name
		}
	}
	return ""
}

// resourceNamesInSpec returns the exported names in vs whose type is
// alias.Resource — either declared directly (`var X rastrillo.Resource`)
// or via a composite literal value (`var X = rastrillo.Resource{...}`).
func resourceNamesInSpec(vs *ast.ValueSpec, alias string) []string {
	var names []string
	if vs.Type != nil {
		if !isResourceSelector(vs.Type, alias) {
			return nil
		}
		for _, n := range vs.Names {
			if ast.IsExported(n.Name) {
				names = append(names, n.Name)
			}
		}
		return names
	}

	for i, n := range vs.Names {
		if !ast.IsExported(n.Name) || i >= len(vs.Values) {
			continue
		}
		cl, ok := vs.Values[i].(*ast.CompositeLit)
		if !ok || !isResourceSelector(cl.Type, alias) {
			continue
		}
		names = append(names, n.Name)
	}
	return names
}

// isResourceSelector reports whether expr is the selector
// alias.Resource.
func isResourceSelector(expr ast.Expr, alias string) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Resource" {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == alias
}

// readModulePath reads the module directive from root's go.mod.
// Hand-rolled rather than depending on golang.org/x/mod/modfile: the
// module line is one fixed shape, not worth a dependency.
func readModulePath(root string) (string, error) {
	f, err := os.Open(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("read go.mod: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("go.mod: no module directive found")
}

// packageImportPath computes dir's import path given moduleRoot's own
// module path.
func packageImportPath(moduleRoot, dir, modulePath string) (string, error) {
	rel, err := filepath.Rel(moduleRoot, dir)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return modulePath, nil
	}
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("manifest dir %s is outside module root %s", dir, moduleRoot)
	}
	return modulePath + "/" + rel, nil
}

// runDriver writes a small main package referencing every var in
// vars, runs it with `go run` in moduleRoot, and returns its stdout
// (the JSON-encoded []rastrillo.Resource). A failing run's stderr is
// returned verbatim as the error — a typo'd manifest IS that compile
// error.
func runDriver(moduleRoot, importPath string, vars []goVar) ([]byte, error) {
	tmp, err := os.MkdirTemp("", "rastrillo-manifest-driver-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	refs := make([]string, len(vars))
	for i, v := range vars {
		refs[i] = "m." + v.name
	}

	src := fmt.Sprintf(`package main

import (
	"encoding/json"
	"os"

	m %q
	"amadan.net/rastrillo/rastrillo"
)

func main() {
	rs := []rastrillo.Resource{%s}
	if err := json.NewEncoder(os.Stdout).Encode(rs); err != nil {
		panic(err)
	}
}
`, importPath, strings.Join(refs, ", "))

	driverPath := filepath.Join(tmp, "main.go")
	if err := os.WriteFile(driverPath, []byte(src), 0o644); err != nil {
		return nil, err
	}

	cmd := exec.Command("go", "run", driverPath)
	cmd.Dir = moduleRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("go run %s: %s", driverPath, msg)
	}
	return stdout.Bytes(), nil
}

// Artifact renders the full resource set as the stable JSON artifact —
// gen/manifest.json's exact contract: resources sorted by Name,
// two-space indent, trailing newline. Evolution of the shape is
// additive only (see rastrillo.Resource's doc).
func Artifact(rs []rastrillo.Resource) []byte {
	sorted := make([]rastrillo.Resource, len(rs))
	copy(sorted, rs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	b, err := json.MarshalIndent(sorted, "", "  ")
	if err != nil {
		panic(fmt.Errorf("marshal artifact: %w", err)) // Resource is JSON-safe by construction
	}
	return append(b, '\n')
}
