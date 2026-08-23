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

// parseGoSnippet tries the four shapes a docs fence legitimately takes:
// a whole file, a statement list, a declaration list, and a run of
// struct fields.
//
// The error it reports on total failure is the DECLARATION attempt's,
// not whichever framing happened to be tried last. Reporting the last
// one sent a reader chasing "expected '}', found 'type'" from the
// struct framing when the actual mistake was an illustrative
// `func(...)` that is not a parameter list.
func parseGoSnippet(src string) error {
	framings := []string{
		src,
		"package p\n\nfunc _snippet() {\n" + src + "\n}\n",
		"package p\n\n" + src,
		"package p\n\ntype _T struct {\n" + src + "\n}\n",
	}
	const reported = 2 // the declaration framing
	var reportedErr error
	for i, framing := range framings {
		_, err := parser.ParseFile(token.NewFileSet(), "snippet.go", framing, parser.SkipObjectResolution)
		if err == nil {
			return nil
		}
		if i == reported {
			reportedErr = err
		}
	}
	return reportedErr
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
