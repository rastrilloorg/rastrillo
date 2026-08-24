package docsite

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// referencePages maps a package directory (relative to the repo root)
// to the reference page that must document it.
//
// This map is the claim "everything you can do with Rastrillo is
// documented" made mechanical, in two halves: this test requires every
// symbol of a mapped package to appear on its page, and
// TestEveryPackageHasAReferencePage requires every package of the
// framework to be mapped. Neither half is enough alone — the first
// would let a package go undocumented by staying out of the map, and
// the second would accept an empty page.
var referencePages = map[string]string{
	"scope":    "reference/scope",
	"db":       "reference/db",
	"migrate":  "reference/migrate",
	"view":     "reference/view",
	"form":     "reference/form",
	"flash":    "reference/flash",
	"csrf":     "reference/csrf",
	"sessions": "reference/sessions",
	"password": "reference/password",
	"auth":     "reference/auth",
	"passkey":  "reference/passkey",
	"webauthn": "reference/webauthn",
	"jobs":     "reference/jobs",
	"ui":       "reference/ui",
	"tools":    "reference/tools",
	"crypto":   "reference/crypto",
	"keyring":  "reference/keyring",
	"blobs":    "reference/blobs",
	"carlos":   "reference/carlos",
	"eventlog": "reference/eventlog",
	"mail":     "reference/mail",
	"gormlite": "reference/gormlite",
	"vectors":  "reference/vectors",
	"harness":  "reference/harness",
	".":        "reference/rastrillo",
}

// TestExportedSymbolsAreDocumented is the completeness gate.
//
// For each package above, every exported top-level declaration must be
// named somewhere on its reference page. A symbol that genuinely does
// not merit prose opts out with a marker carrying a reason:
//
//	<!-- docs:ignore LegacyRPID superseded by Config.RPIDs, kept for hostname moves -->
//
// The marker is deliberately ugly and greppable, and the reason is
// required, so opting out is a decision someone reads in review rather
// than a silent gap.
func TestExportedSymbolsAreDocumented(t *testing.T) {
	site, err := Load(root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	dirs := make([]string, 0, len(referencePages))
	for dir := range referencePages {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	for _, dir := range dirs {
		slug := referencePages[dir]
		page, ok := site.BySlug[slug]
		if !ok {
			t.Errorf("package %s: no %s page", dir, slug)
			continue
		}
		symbols, err := exportedSymbols("../../" + dir)
		if err != nil {
			t.Errorf("package %s: %v", dir, err)
			continue
		}
		if len(symbols) == 0 {
			t.Errorf("package %s: found no exported symbols — the scanner is broken, not the docs", dir)
			continue
		}

		var missing []string
		for _, sym := range symbols {
			if reason, ignored := page.Ignores[sym]; ignored {
				if reason == "" {
					t.Errorf("%s.md: docs:ignore for %s gives no reason", slug, sym)
				}
				continue
			}
			if !documents(page.Body, sym) {
				missing = append(missing, sym)
			}
		}
		if len(missing) > 0 {
			t.Errorf("%s.md does not document %d of package %s's %d exported symbols:\n\t%s",
				slug, len(missing), dir, len(symbols), strings.Join(missing, ", "))
		}
	}
}

// TestIgnoreMarkersNameRealSymbols stops a docs:ignore outliving the
// symbol it excused — otherwise a page accumulates apologies for things
// that no longer exist, and the next reader cannot tell which are live.
func TestIgnoreMarkersNameRealSymbols(t *testing.T) {
	site, err := Load(root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for dir, slug := range referencePages {
		page, ok := site.BySlug[slug]
		if !ok || len(page.Ignores) == 0 {
			continue
		}
		symbols, err := exportedSymbols("../../" + dir)
		if err != nil {
			continue
		}
		have := map[string]bool{}
		for _, s := range symbols {
			have[s] = true
		}
		for name := range page.Ignores {
			if !have[name] {
				t.Errorf("%s.md: docs:ignore names %s, which package %s does not export", slug, name, dir)
			}
		}
	}
}

// documents reports whether a page documents a symbol.
//
// A plain symbol must appear as a whole word. A method — "Sessions.Require"
// — is satisfied either by that qualified spelling in prose or by its Go
// declaration with a matching receiver, since a page quoting
//
//	func (s *Sessions) Require(next http.Handler) http.Handler
//
// has plainly documented it. The receiver type has to match: accepting a
// bare ") Require(" would let a method on one type satisfy the same name
// on another, which is exactly the confusion a reference page exists to
// prevent.
func documents(body, symbol string) bool {
	if mentions(body, symbol) {
		return true
	}
	recv, method, isMethod := strings.Cut(symbol, ".")
	if !isMethod {
		return false
	}
	decl := regexp.MustCompile(
		`func\s*\(\s*\w+\s+\*?` + regexp.QuoteMeta(recv) + `\s*\)\s*` +
			regexp.QuoteMeta(method) + `\s*[(\[]`)
	return decl.MatchString(body)
}

// mentions reports whether a page names a symbol as a whole word.
// Substring matching would let "New" be satisfied by "NewHandlers".
func mentions(body, symbol string) bool {
	for i := 0; ; {
		j := strings.Index(body[i:], symbol)
		if j < 0 {
			return false
		}
		start := i + j
		end := start + len(symbol)
		if !identByte(byteAt(body, start-1)) && !identByte(byteAt(body, end)) {
			return true
		}
		i = start + 1
	}
}

func byteAt(s string, i int) byte {
	if i < 0 || i >= len(s) {
		return ' '
	}
	return s[i]
}

func identByte(b byte) bool {
	return b == '_' ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9')
}

// exportedSymbols returns every exported top-level declaration in a
// package directory: funcs (including methods, named Type.Method),
// types, constants and variables.
func exportedSymbols(dir string) ([]string, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if name == "" || !ast.IsExported(strings.TrimPrefix(name, "*")) || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}

	for name, pkg := range pkgs {
		if strings.HasSuffix(name, "_test") {
			continue
		}
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:
					if d.Recv == nil {
						add(d.Name.Name)
						continue
					}
					// A method counts only when its receiver is
					// exported: an exported method on an unexported
					// type is unreachable from outside the package.
					recv := receiverName(d.Recv)
					if ast.IsExported(recv) && ast.IsExported(d.Name.Name) {
						add(recv + "." + d.Name.Name)
					}
				case *ast.GenDecl:
					for _, spec := range d.Specs {
						switch s := spec.(type) {
						case *ast.TypeSpec:
							add(s.Name.Name)
						case *ast.ValueSpec:
							for _, n := range s.Names {
								add(n.Name)
							}
						}
					}
				}
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

func receiverName(recv *ast.FieldList) string {
	if recv == nil || len(recv.List) == 0 {
		return ""
	}
	switch t := recv.List[0].Type.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	case *ast.IndexExpr: // generic receiver, e.g. Set[T]
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	case *ast.IndexListExpr: // generic receiver with several params
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

// coveredElsewhere lists importable packages that deliberately have no
// reference page of their own, each with the page that covers them.
//
// Both are support packages for a parent whose page is the right place
// to describe them: splitting them out would give a reader two pages to
// hold in their head for one idea. The map exists so that decision is
// recorded and reviewable rather than an omission nobody notices.
var coveredElsewhere = map[string]string{
	"webauthn/authtest": "reference/webauthn",
	"migrate/dump":      "reference/migrate",
}

// TestEveryPackageHasAReferencePage is the other half of the
// completeness claim.
//
// TestExportedSymbolsAreDocumented proves every symbol of a MAPPED
// package appears on its page. On its own that is satisfiable by
// leaving a package out of the map, so this test walks the repository
// for importable packages and requires each to be either mapped or
// explicitly covered elsewhere. Together the two mean: no package goes
// undocumented, and no page is empty.
func TestEveryPackageHasAReferencePage(t *testing.T) {
	const repo = "../.."
	skip := map[string]bool{"internal": true, "cmd": true, "examples": true, "hack": true, "docs": true}

	err := filepath.WalkDir(repo, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(repo, p)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			base := d.Name()
			if strings.HasPrefix(base, ".") && rel != "." {
				return filepath.SkipDir
			}
			if skip[strings.SplitN(rel, "/", 2)[0]] && rel != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		dir := path.Dir(rel)
		if _, mapped := referencePages[dir]; mapped {
			return nil
		}
		if page, covered := coveredElsewhere[dir]; covered {
			if page == "" {
				t.Errorf("package %s is listed as covered elsewhere but names no page", dir)
			}
			return nil
		}
		t.Errorf("package %s has no reference page: add it to referencePages, or to coveredElsewhere with the page that covers it", dir)
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}
