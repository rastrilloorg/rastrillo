package ui

import (
	"fmt"
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

func TestFuncsRegistersDictListIconIconAssetsTAndTf(t *testing.T) {
	f := Funcs()
	for _, name := range []string{"dict", "list", "icon", "iconAssets", "T", "Tf"} {
		if _, ok := f[name]; !ok {
			t.Errorf("Funcs() is missing %q", name)
		}
	}
	// Exactly these: an accidental extra is a helper the shipped partials
	// do not document and an app cannot rely on.
	if len(f) != 6 {
		t.Errorf("Funcs() has %d entries, want exactly 6", len(f))
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
func TestFuncsWithReplacesOnlyTAndTf(t *testing.T) {
	f := FuncsWith(func(key string, _ ...any) string { return "X-" + key })
	for _, name := range []string{"dict", "list", "icon", "iconAssets", "T", "Tf"} {
		if _, ok := f[name]; !ok {
			t.Errorf("FuncsWith(...) is missing %q", name)
		}
	}
	if len(f) != 6 {
		t.Errorf("FuncsWith(...) has %d entries, want exactly 6", len(f))
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

// The seam exists even with no options, so a layout can call
// {{iconAssets}} unconditionally and switching delivery later needs no
// template change.
func TestFuncsAlwaysRegistersIconAssets(t *testing.T) {
	fn, ok := Funcs()["iconAssets"].(func() template.HTML)
	if !ok {
		t.Fatalf("iconAssets is %T, want func() template.HTML", Funcs()["iconAssets"])
	}
	if got := fn(); got != "" {
		t.Errorf("default iconAssets() = %q, want empty (inline needs no head markup)", got)
	}
}

func TestWithIconsOverridesBothSeams(t *testing.T) {
	myIcon := func(slug string) template.HTML { return template.HTML(`<i data-slug="` + slug + `"></i>`) }
	myAssets := func() template.HTML { return `<link rel="stylesheet" href="/x.css">` }
	fm := Funcs(WithIcons(myIcon, myAssets))

	if got := fm["icon"].(func(string) template.HTML)("plus"); !strings.Contains(string(got), `data-slug="plus"`) {
		t.Errorf("icon seam not overridden: %s", got)
	}
	if got := fm["iconAssets"].(func() template.HTML)(); !strings.Contains(string(got), "/x.css") {
		t.Errorf("iconAssets seam not overridden: %s", got)
	}
}

// The point of the seam: shipped partials resolve through the app's own
// icon package without any partial changing.
func TestPartialsRenderThroughOverriddenIcons(t *testing.T) {
	myIcon := func(slug string) template.HTML { return template.HTML(`<i data-slug="` + slug + `"></i>`) }
	tmpl, err := template.New("").
		Funcs(Funcs(WithIcons(myIcon, nil))).
		ParseFS(Templates(), "*.html")
	if err != nil {
		t.Fatalf("ParseFS: %v", err)
	}
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "list-bar-search", map[string]any{"Action": "/posts"}); err != nil {
		t.Fatalf("ExecuteTemplate: %v", err)
	}
	if !strings.Contains(buf.String(), `data-slug="search"`) {
		t.Errorf("partial did not resolve through the overridden icon func: %s", buf.String())
	}
}

// A nil for either seam leaves the framework default, rather than
// installing a nil func that panics at render time.
func TestWithIconsToleratesNil(t *testing.T) {
	fm := Funcs(WithIcons(nil, nil))
	if got := fm["icon"].(func(string) template.HTML)("check"); !strings.Contains(string(got), "<svg") {
		t.Error("nil icon func should leave the framework default in place")
	}
	if got := fm["iconAssets"].(func() template.HTML)(); got != "" {
		t.Errorf("nil assets func should leave the empty default, got %q", got)
	}
}

// Both seams compose in one call — the form an app with its own icons
// AND its own catalog must use on the per-request rebind.
func TestOptionsCompose(t *testing.T) {
	myIcon := func(slug string) template.HTML { return template.HTML("<i></i>") }
	fm := Funcs(WithT(func(key string, _ ...any) string { return "translated:" + key }),
		WithIcons(myIcon, nil))
	if got := fm["T"].(func(string, ...any) string)("k"); got != "translated:k" {
		t.Errorf("T not applied alongside WithIcons: %q", got)
	}
	if got := fm["icon"].(func(string) template.HTML)("check"); string(got) != "<i></i>" {
		t.Errorf("WithIcons not applied alongside WithT: %s", got)
	}
}

// The documented trap, pinned so it cannot regress into a surprise:
// FuncsWith rebinds icon back to the framework default. An app with its
// own icons must pass both options rather than reach for FuncsWith.
func TestFuncsWithResetsTheIconSeam(t *testing.T) {
	myIcon := func(slug string) template.HTML { return template.HTML("<i></i>") }
	custom := Funcs(WithIcons(myIcon, nil))
	if string(custom["icon"].(func(string) template.HTML)("check")) != "<i></i>" {
		t.Fatal("precondition: WithIcons did not take effect")
	}

	reverted := FuncsWith(func(key string, _ ...any) string { return key })
	if !strings.Contains(string(reverted["icon"].(func(string) template.HTML)("check")), "<svg") {
		t.Error("FuncsWith no longer resets icon to the framework default; update its doc comment, which warns that it does")
	}
}

// Tf is T plus {name} interpolation — the root package's Locales.Tf
// semantics, available to a partial that has a value to place inside a
// sentence (the error page's reference line is the first caller).
func TestTfInterpolatesTheBaseCatalog(t *testing.T) {
	fn, ok := Funcs()["Tf"].(func(string, ...any) string)
	if !ok {
		t.Fatalf("Funcs has no Tf entry of the T signature: %T", Funcs()["Tf"])
	}
	if got, want := fn("rastrillo.ui.error_ref", "ref", "k3f9tq"), "Reference: k3f9tq"; got != want {
		t.Errorf("Tf = %q, want %q", got, want)
	}
	// A placeholder with no argument stays verbatim: a translator's typo
	// shows up in the page rather than silently deleting a sentence.
	if got, want := fn("rastrillo.ui.error_ref"), "Reference: {ref}"; got != want {
		t.Errorf("Tf with no args = %q, want %q", got, want)
	}
	// An unknown key is its own name, exactly as T's miss behaves.
	if got := fn("rastrillo.ui.nope", "ref", "x"); got != "rastrillo.ui.nope" {
		t.Errorf("Tf on a miss = %q, want the key back", got)
	}
}

// WithT rebinds Tf too — the request-scoped translator an app passes
// takes args already, so a page rendered per request interpolates
// through the app's lookup rather than the framework's English.
func TestWithTRebindsTf(t *testing.T) {
	fn, ok := FuncsWith(func(key string, args ...any) string {
		return "app:" + key + ":" + fmt.Sprint(args...)
	})["Tf"].(func(string, ...any) string)
	if !ok {
		t.Fatalf("FuncsWith left Tf unbound")
	}
	if got, want := fn("k", "ref", "v"), "app:k:refv"; got != want {
		t.Errorf("Tf = %q, want %q", got, want)
	}
}
