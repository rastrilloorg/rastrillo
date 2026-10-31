package scaffoldcheck

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// clashRefusal is the start of rastrillo new's refusal of a name whose
// package the scaffold's own code already uses as a name.
const clashRefusal = "choose another name"

// Every name the scaffold's Go uses where the app's package is in scope
// is refused as an app's package name: main.go's `database` variable
// shadowed a package called database, and its `database.New` did not
// build. A name added to a template and not to the refusal list fails
// here, before an app called that name does.
func TestNewRefusesEveryNameTheScaffoldUses(t *testing.T) {
	t.Parallel()
	used := map[string]bool{}
	for _, variant := range everyShell() {
		directory := newApp(t, variant.name, variant.flags...)
		for _, identifier := range namesBesideTheApp(t, directory,
			variant.name) {
			used[identifier] = true
		}
	}
	parent := t.TempDir()
	for identifier := range used {
		output, err := runIn(parent, rastrilloBinary, "new", identifier)
		if err == nil || !strings.Contains(output, clashRefusal) {
			t.Errorf("rastrillo new %s: %v, want the clash refused:\n%s",
				identifier, err, output)
		}
	}
	if entries, _ := os.ReadDir(parent); len(entries) != 0 {
		t.Errorf("a refused name left %v behind", entries)
	}
}

// namesBesideTheApp is every identifier in the packages that import the
// app's package, cmd/<name> and internal/<name>test, in the scaffold
// at directory. Those are the files where a name can shadow the
// package, or clash with its import.
func namesBesideTheApp(t *testing.T, directory, pkg string) []string {
	t.Helper()
	var names []string
	for _, importer := range []string{
		filepath.Join(directory, "cmd", pkg),
		filepath.Join(directory, "internal", pkg+"test"),
	} {
		paths, err := filepath.Glob(filepath.Join(importer, "*.go"))
		if err != nil || len(paths) == 0 {
			t.Fatalf("no Go files in %s: %v", importer, err)
		}
		for _, path := range paths {
			names = append(names, namesIn(t, path)...)
		}
	}
	return slices.DeleteFunc(names, func(name string) bool {
		return name == pkg || name == "_"
	})
}

// namesIn is every identifier a file declares or uses, except the ones
// that are never in its scope: the package clause, a selector's field
// or method, a struct's fields and an interface's methods, the field
// keys of a composite literal and a method's own name.
func namesIn(t *testing.T, path string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	members := membersOf(file)
	var names []string
	var visit func(ast.Node) bool
	walk := func(nodes ...ast.Node) {
		for _, node := range nodes {
			ast.Inspect(node, visit)
		}
	}
	visit = func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.Ident:
			names = append(names, node.Name)
		case *ast.SelectorExpr:
			walk(node.X)
			return false
		case *ast.Field:
			if members[node] {
				walk(node.Type)
				return false
			}
		case *ast.KeyValueExpr:
			if _, isField := node.Key.(*ast.Ident); isField {
				walk(node.Value)
				return false
			}
		case *ast.FuncDecl:
			if node.Recv != nil && node.Body != nil {
				walk(node.Recv, node.Type, node.Body)
				return false
			}
		}
		return true
	}
	for _, spec := range file.Imports {
		walk(spec)
	}
	for _, declaration := range file.Decls {
		walk(declaration)
	}
	return names
}

// membersOf is every struct field and interface method in file, whose
// names are reached through a value and shadow nothing.
func membersOf(file *ast.File) map[*ast.Field]bool {
	members := map[*ast.Field]bool{}
	ast.Inspect(file, func(node ast.Node) bool {
		var list *ast.FieldList
		switch node := node.(type) {
		case *ast.StructType:
			list = node.Fields
		case *ast.InterfaceType:
			list = node.Methods
		}
		if list != nil {
			for _, field := range list.List {
				members[field] = true
			}
		}
		return true
	})
	return members
}

// A name the scaffold once shadowed is refused before anything is
// written, and one letter away from it an app is scaffolded and its
// code and tests build, plain and signing people in.
func TestNewRefusesAClashAndBuildsANearMiss(t *testing.T) {
	t.Parallel()
	for _, clash := range []string{"database", "options"} {
		parent := t.TempDir()
		output, err := runIn(parent, rastrilloBinary, "new", clash)
		if err == nil || !strings.Contains(output, clashRefusal) {
			t.Errorf("rastrillo new %s: %v, want the clash refused:\n%s",
				clash, err, output)
		}
		if entries, _ := os.ReadDir(parent); len(entries) != 0 {
			t.Errorf("refusing %s left %v behind", clash, entries)
		}
	}
	for _, each := range variants {
		directory := buildableApp(t, "databases", each.flags...)
		run(t, directory, "go", "vet", "./...")
	}
}
