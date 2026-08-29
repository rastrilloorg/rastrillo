package docsite

import (
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/carlosframework/rastrillo/ui"
)

var definePartial = regexp.MustCompile(`{{-?\s*define\s+"([^"]+)"`)

// docs/site/templates.md lists the shipped partials by name, in a
// column-formatted block a reader scans rather than a sentence they
// read. That is exactly the shape that goes stale without anyone
// noticing: a partial added to ui/partials and not to the block is one
// nobody browsing the docs will find, and a name left in the block
// after its file is deleted is a partial an app will be told to use and
// cannot. Four partials were missing from it before this gate existed.
//
// The names come out of ui.Templates() — the same embedded FS every app
// parses — so the docs are checked against what ships rather than
// against a second list somebody has to keep. Order and column layout
// are the block's business; this compares the two as sets.
func TestTemplatesPageListsEveryPartial(t *testing.T) {
	site, err := Load(root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	page := site.BySlug["templates"]
	if page == nil {
		t.Fatal("docs/site has no templates page")
	}

	// The block is the one plain-text fence on the page. Pinned rather
	// than searched for: a second text fence would make "the partial
	// list" ambiguous, and the failure should say so instead of
	// silently checking the wrong one.
	var listed []string
	fences := 0
	for _, f := range page.Fences {
		if f.Lang != "text" {
			continue
		}
		fences++
		listed = strings.Fields(f.Body)
	}
	if fences != 1 {
		t.Fatalf("templates.md has %d text fences; the partial list is meant to be the only one", fences)
	}

	names, err := fs.Glob(ui.Templates(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	var defined []string
	for _, name := range names {
		body, err := fs.ReadFile(ui.Templates(), name)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range definePartial.FindAllStringSubmatch(string(body), -1) {
			defined = append(defined, m[1])
		}
	}
	if len(defined) == 0 {
		t.Fatal("no {{define}} found in ui.Templates(); the gate would pass vacuously")
	}

	inDocs := make(map[string]bool, len(listed))
	for _, n := range listed {
		inDocs[n] = true
	}
	inCode := make(map[string]bool, len(defined))
	for _, n := range defined {
		inCode[n] = true
	}
	var missing, extra []string
	for n := range inCode {
		if !inDocs[n] {
			missing = append(missing, n)
		}
	}
	for n := range inDocs {
		if !inCode[n] {
			extra = append(extra, n)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("templates.md's partial list is missing %v; add them to the block under \"## The partials\"", missing)
	}
	if len(extra) > 0 {
		t.Errorf("templates.md's partial list names %v, which ui/partials does not define", extra)
	}
}
