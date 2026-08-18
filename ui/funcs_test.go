package ui

import (
	"html/template"
	"strings"
	"testing"
)

func TestDictBuildsAMap(t *testing.T) {
	m, err := dict("Title", "Posts", "Count", 3)
	if err != nil {
		t.Fatalf("dict: %v", err)
	}
	if m["Title"] != "Posts" {
		t.Errorf(`m["Title"] = %v, want "Posts"`, m["Title"])
	}
	if m["Count"] != 3 {
		t.Errorf(`m["Count"] = %v, want 3`, m["Count"])
	}
	if len(m) != 2 {
		t.Errorf("len(m) = %d, want 2", len(m))
	}
}

func TestDictWithNoPairsIsEmptyNotNil(t *testing.T) {
	m, err := dict()
	if err != nil {
		t.Fatalf("dict: %v", err)
	}
	if m == nil {
		t.Fatal("dict() returned a nil map; callers index into it")
	}
	if len(m) != 0 {
		t.Errorf("len(m) = %d, want 0", len(m))
	}
}

// An odd argument count must fail loudly at Execute rather than silently
// dropping the last key (spec §4.1).
func TestDictOddArgCountIsAnError(t *testing.T) {
	if _, err := dict("Title", "Posts", "Count"); err == nil {
		t.Fatal("dict with 3 args returned no error")
	}
}

func TestDictNonStringKeyIsAnError(t *testing.T) {
	if _, err := dict(7, "Posts"); err == nil {
		t.Fatal("dict with a non-string key returned no error")
	}
}

func TestListBuildsASlice(t *testing.T) {
	got := list("a", 2, true)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0] != "a" || got[1] != 2 || got[2] != true {
		t.Errorf("list(...) = %v, want [a 2 true]", got)
	}
}

func TestListWithNoItemsIsEmptyNotNil(t *testing.T) {
	if got := list(); got == nil {
		t.Fatal("list() returned nil; templates range over this")
	}
}

func TestFuncsRegistersDictListIconAndT(t *testing.T) {
	f := Funcs()
	for _, name := range []string{"dict", "list", "icon", "T"} {
		if _, ok := f[name]; !ok {
			t.Errorf("Funcs() is missing %q", name)
		}
	}
	if len(f) != 4 {
		t.Errorf("Funcs() has %d entries, want exactly 4", len(f))
	}
}

// defaultT (Funcs' T) resolves the framework base catalog and falls back
// to the key itself for anything the base catalog does not carry —
// exercised directly, since the partials only ever call it for keys the
// catalog does define.
func TestDefaultTResolvesBaseCatalogAndFallsBackToKey(t *testing.T) {
	if got := defaultT("rastrillo.ui.cancel"); got != "Cancel" {
		t.Errorf("defaultT(%q) = %q, want %q", "rastrillo.ui.cancel", got, "Cancel")
	}
	if got := defaultT("no.such.key"); got != "no.such.key" {
		t.Errorf("defaultT of a missing key = %q, want the key verbatim", got)
	}
}

// TestFuncsWithRebindsOnAClonedPristineTree proves the seam FuncsWith's
// doc comment now documents: html/template refuses to Clone a tree that
// has already executed ("cannot Clone ... after it has executed"), so
// the only way to rebind T per request is to keep one base tree pristine
// — parsed once, never itself passed to Execute/ExecuteTemplate — and
// Clone+rebind it fresh for every request. This exercises exactly that:
// base is never executed; only its clone is, with T rebound on the clone
// after Clone returns.
func TestFuncsWithRebindsOnAClonedPristineTree(t *testing.T) {
	base := template.Must(template.New("").Funcs(Funcs()).ParseFS(Templates(), "*.html"))

	perReq, err := base.Clone()
	if err != nil {
		t.Fatalf("Clone of a never-executed tree must succeed: %v", err)
	}
	perReq.Funcs(FuncsWith(func(key string, _ ...any) string {
		return "X-" + key
	}))

	var buf strings.Builder
	if err := perReq.ExecuteTemplate(&buf, "pagination", map[string]any{}); err != nil {
		t.Fatalf("ExecuteTemplate on the clone: %v", err)
	}
	if !strings.Contains(buf.String(), `aria-label="X-rastrillo.ui.pagination"`) {
		t.Errorf("rebound T did not take effect on the clone: %s", buf.String())
	}

	// The clone's rebind must not leak back into the pristine base tree —
	// base still resolves T through Funcs' own default. This is base's
	// first execution, which Clone (already called, above) permits.
	var baseBuf strings.Builder
	if err := base.ExecuteTemplate(&baseBuf, "pagination", map[string]any{}); err != nil {
		t.Fatalf("ExecuteTemplate on base: %v", err)
	}
	if !strings.Contains(baseBuf.String(), `aria-label="Pagination"`) {
		t.Errorf("Clone must not mutate the pristine base tree's own T: %s", baseBuf.String())
	}
}

// FuncsWith replaces only the T entry — dict/list/icon are unchanged.
func TestFuncsWithReplacesOnlyT(t *testing.T) {
	f := FuncsWith(func(key string, _ ...any) string { return "X-" + key })
	for _, name := range []string{"dict", "list", "icon", "T"} {
		if _, ok := f[name]; !ok {
			t.Errorf("FuncsWith(...) is missing %q", name)
		}
	}
	if len(f) != 4 {
		t.Errorf("FuncsWith(...) has %d entries, want exactly 4", len(f))
	}
	tFunc, ok := f["T"].(func(string, ...any) string)
	if !ok {
		t.Fatalf("T entry is %T, want func(string, ...any) string", f["T"])
	}
	if got := tFunc("rastrillo.ui.cancel"); got != "X-rastrillo.ui.cancel" {
		t.Errorf("rebound T = %q, want %q", got, "X-rastrillo.ui.cancel")
	}
}

// The value proposition: these have to work as real FuncMap entries end
// to end, the same standard icons_test.go's TestIconWorksAsTemplateFunc
// holds Icon to.
func TestFuncsWorkEndToEnd(t *testing.T) {
	tmpl := template.Must(template.New("t").Funcs(Funcs()).Parse(
		`{{$d := dict "Label" "Search"}}{{$d.Label}}|{{len (list 1 2 3)}}|{{icon "search"}}`,
	))
	var buf strings.Builder
	if err := tmpl.Execute(&buf, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := buf.String()
	if !strings.HasPrefix(got, "Search|3|") {
		t.Errorf("dict/list did not render as expected: %s", got)
	}
	if !strings.Contains(got, "<svg") {
		t.Errorf("icon did not resolve through Funcs(): %s", got)
	}
}

// An odd dict call must surface as an Execute error, not a silent render.
func TestDictErrorSurfacesAtExecute(t *testing.T) {
	tmpl := template.Must(template.New("t").Funcs(Funcs()).Parse(`{{$d := dict "A"}}{{$d.A}}`))
	var buf strings.Builder
	if err := tmpl.Execute(&buf, nil); err == nil {
		t.Fatal("Execute returned no error for an odd-argument dict call")
	}
}
