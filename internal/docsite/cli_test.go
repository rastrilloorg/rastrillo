package docsite

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// cliDir is the CLI's package, relative to this one.
const cliDir = "../../cmd/rastrillo"

// cliPage is the page that must document all of it.
const cliPage = "cli"

// TestCLICommandsAreDocumented reads the CLI's own source for every
// subcommand it dispatches and every flag it declares, and requires
// each to appear on the CLI reference page.
//
// It reads the dispatch and the flag declarations rather than the usage
// text on purpose. Usage text is prose someone can forget to update, so
// gating against it would only prove the docs match a second copy of
// the docs. The switch statement and the flag set are the CLI.
func TestCLICommandsAreDocumented(t *testing.T) {
	site, err := Load(root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	page, ok := site.BySlug[cliPage]
	if !ok {
		t.Fatalf("no %s page", cliPage)
	}

	commands, flags, err := cliSurface(cliDir)
	if err != nil {
		t.Fatalf("read CLI surface: %v", err)
	}
	if len(commands) == 0 || len(flags) == 0 {
		t.Fatalf("read no CLI surface (%d commands, %d flags) — the scanner is broken, not the docs",
			len(commands), len(flags))
	}

	for _, cmd := range commands {
		if !strings.Contains(page.Body, "rastrillo "+cmd) {
			t.Errorf("%s.md does not document the %q command (looked for %q)",
				cliPage, cmd, "rastrillo "+cmd)
		}
	}
	for _, f := range flags {
		if !strings.Contains(page.Body, "--"+f) {
			t.Errorf("%s.md does not document the --%s flag", cliPage, f)
		}
	}
}

// cliSurface returns every dispatched subcommand and every declared
// flag name in the CLI's package.
func cliSurface(dir string) (commands, flags []string, err error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.SkipObjectResolution)
	if err != nil {
		return nil, nil, err
	}

	seenCmd, seenFlag := map[string]bool{}, map[string]bool{}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			base := filepath.Base(name)
			ast.Inspect(file, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.SwitchStmt:
					// Only the two dispatch switches name subcommands.
					// main.go's cases are commands; migration.go's are
					// subcommands of one, and documenting them as
					// "rastrillo check" rather than "rastrillo
					// migration check" would be wrong in a way a reader
					// would trip over.
					var prefix string
					switch base {
					case "main.go":
					case "migration.go":
						prefix = "migration "
					default:
						return true
					}
					for _, stmt := range node.Body.List {
						clause, ok := stmt.(*ast.CaseClause)
						if !ok {
							continue
						}
						for _, expr := range clause.List {
							s, ok := stringLit(expr)
							if !ok || !isCommandWord(s) {
								continue
							}
							cmd := prefix + s
							if !seenCmd[cmd] {
								seenCmd[cmd] = true
								commands = append(commands, cmd)
							}
						}
					}
				case *ast.CallExpr:
					sel, ok := node.Fun.(*ast.SelectorExpr)
					if !ok || !flagMethod(sel.Sel.Name) {
						return true
					}
					for _, arg := range node.Args {
						if s, ok := stringLit(arg); ok {
							if !seenFlag[s] {
								seenFlag[s] = true
								flags = append(flags, s)
							}
							break
						}
					}
				}
				return true
			})
		}
	}
	return commands, flags, nil
}

// flagMethod reports whether a selector names a flag.FlagSet
// declaration method.
func flagMethod(name string) bool {
	switch name {
	case "String", "Bool", "Int", "Int64", "Uint", "Uint64", "Float64", "Duration",
		"StringVar", "BoolVar", "IntVar", "Int64Var", "UintVar", "Uint64Var",
		"Float64Var", "DurationVar", "Func", "TextVar":
		return true
	}
	return false
}

// isCommandWord filters the dispatch switches' cases down to real
// subcommands: help aliases are documented as one thing, and a case
// arm is only a command if it looks like one.
func isCommandWord(s string) bool {
	if s == "" || strings.HasPrefix(s, "-") {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z') && r != '-' {
			return false
		}
	}
	return true
}

func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}
