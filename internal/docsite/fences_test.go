package docsite

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// TestGoFencesParse stops a broken snippet shipping.
//
// A docs snippet is usually a fragment rather than a file, so each
// fence is tried as a whole file first and then wrapped in a function
// body. A fence that is neither is a genuine syntax error — the kind
// that comes from an edit that dropped a brace.
func TestGoFencesParse(t *testing.T) {
	site, err := Load(root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, p := range site.Pages() {
		for _, f := range p.Fences {
			if f.Lang != "go" {
				continue
			}
			if err := parseGoSnippet(f.Body); err != nil {
				t.Errorf("%s.md:%d: go fence does not parse: %v", p.Slug, f.Line, err)
			}
		}
	}
}

func parseGoSnippet(src string) error {
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "snippet.go", src, parser.SkipObjectResolution); err == nil {
		return nil
	}
	wrapped := "package p\n\nfunc _snippet() {\n" + src + "\n}\n"
	if _, err := parser.ParseFile(fset, "snippet.go", wrapped, parser.SkipObjectResolution); err == nil {
		return nil
	}
	// Declaration fragments (a lone type or func) parse as a file only
	// with a package clause.
	decls := "package p\n\n" + src
	_, err := parser.ParseFile(token.NewFileSet(), "snippet.go", decls, parser.SkipObjectResolution)
	return err
}

// TestFenceLanguagesAreDeclared keeps Go snippets inside the gate above.
// An undeclared fence is invisible to TestGoFencesParse, so a fence that
// looks like Go and says nothing is a way to smuggle broken code past
// review.
func TestFenceLanguagesAreDeclared(t *testing.T) {
	known := map[string]bool{
		"go": true, "sh": true, "bash": true, "json": true, "toml": true,
		"sql": true, "html": true, "css": true, "js": true, "text": true,
		"": false,
	}
	site, err := Load(root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, p := range site.Pages() {
		for _, f := range p.Fences {
			if !known[f.Lang] {
				if f.Lang == "" && looksLikeGo(f.Body) {
					t.Errorf("%s.md:%d: fence looks like Go but declares no language, so the parse gate skips it",
						p.Slug, f.Line)
					continue
				}
				if f.Lang != "" {
					t.Errorf("%s.md:%d: unknown fence language %q", p.Slug, f.Line, f.Lang)
				}
			}
		}
	}
}

func looksLikeGo(body string) bool {
	for _, sig := range []string{"func ", ":= ", "package ", "import ("} {
		if strings.Contains(body, sig) {
			return true
		}
	}
	return false
}
