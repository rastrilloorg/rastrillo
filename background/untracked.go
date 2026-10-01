package background

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
)

// Untracked is the fence: it lists every go statement and every
// time.AfterFunc call in the non-test Go files of dir, as
// "file:line:col: what". Work started that way is work no Stop can
// find, which is exactly how a fuse outlived its test's database. Call
// it from a test and fail on anything it returns:
//
//	offences, err := background.Untracked(".", "serve.go")
//
// allow names files (base names) to skip. The one launch that usually
// belongs there is the listener goroutine: it IS the server, it ends
// when http.Server.Shutdown says so, and waiting for it in Stop would
// be a wait for the process's whole life.
//
// It reads syntax, not types: a time.AfterFunc reached through a
// renamed import, or a go statement hidden in a helper package, gets
// past it. It is a fence against the obvious mistake, which is the one
// that keeps being made.
func Untracked(dir string, allow ...string) ([]string, error) {
	skip := map[string]bool{}
	for _, a := range allow {
		skip[a] = true
	}
	names, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	fset := token.NewFileSet()
	var out []string
	for _, name := range names {
		base := filepath.Base(name)
		if strings.HasSuffix(base, "_test.go") || skip[base] {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.GoStmt:
				out = append(out, fmt.Sprintf("%s: go statement (use Group.Go)", fset.Position(v.Pos())))
			case *ast.CallExpr:
				if sel, ok := v.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "AfterFunc" {
					if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "time" {
						out = append(out, fmt.Sprintf("%s: time.AfterFunc (use Group.After)", fset.Position(v.Pos())))
					}
				}
			}
			return true
		})
	}
	return out, nil
}
