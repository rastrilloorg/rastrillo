package ui

import (
	"encoding/json"
	"fmt"
	"html/template"
	"strings"
	"testing"

	"github.com/carlosframework/rastrillo"
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

// menuGroup is the reason the MenuGroup key can be optional at all.
// ui's partials take either a dict-built map or a Go struct (the package
// doc offers the struct precisely so a caller gets missing-field
// detection), and a template action reading .MenuGroup off a struct
// without that field is an Execute error — so a plain {{if .MenuGroup}}
// in the markup would 500 every existing struct caller's list screen.
// examples/blog's blog.Filter is exactly that caller, and did.
func TestMenuGroupIsOptionalForEveryDataShape(t *testing.T) {
	type noField struct {
		Label string
		Items []string
	}
	type withField struct {
		Label     string
		MenuGroup string
	}
	empty := ""
	own := "list-controls"

	for _, c := range []struct {
		name string
		data any
		want string
	}{
		{"nil", nil, MenuGroupDefault},
		{"empty dict", map[string]any{}, MenuGroupDefault},
		{"dict without the key", map[string]any{"Label": "Sort"}, MenuGroupDefault},
		{"dict with an empty value", map[string]any{"MenuGroup": ""}, MenuGroupDefault},
		{"dict with a non-string value", map[string]any{"MenuGroup": 7}, MenuGroupDefault},
		{"dict with a value", map[string]any{"MenuGroup": "list-controls"}, "list-controls"},
		{"map[string]string", map[string]string{"MenuGroup": "list-controls"}, "list-controls"},
		{"struct without the field", noField{Label: "Sort"}, MenuGroupDefault},
		{"pointer to a struct without the field", &noField{Label: "Sort"}, MenuGroupDefault},
		{"nil pointer", (*noField)(nil), MenuGroupDefault},
		{"struct with the field set", withField{MenuGroup: "list-controls"}, "list-controls"},
		{"struct with the field empty", withField{}, MenuGroupDefault},
		{"pointer value", map[string]any{"MenuGroup": &own}, "list-controls"},
		{"pointer to empty", map[string]any{"MenuGroup": &empty}, MenuGroupDefault},
		{"nil interface value", map[string]any{"MenuGroup": nil}, MenuGroupDefault},
	} {
		if got := menuGroup(c.data); got != c.want {
			t.Errorf("menuGroup(%s) = %q, want %q", c.name, got, c.want)
		}
	}
}

// The struct path end to end: a struct shaped to the dropdown partial's
// key contract but written before MenuGroup existed must still render,
// and must land in the shared group.
func TestDropdownRendersForAStructWithoutMenuGroup(t *testing.T) {
	type item struct {
		Href, Label string
		Current     bool
	}
	type filter struct {
		Label string
		Aria  string
		Items []item
	}
	tmpl := template.Must(template.New("").Funcs(Funcs()).ParseFS(Templates(), "*.html"))
	var b strings.Builder
	if err := tmpl.ExecuteTemplate(&b, "dropdown", filter{
		Label: "All", Aria: "Filter by status: All",
		Items: []item{{Href: "/posts", Label: "All", Current: true}},
	}); err != nil {
		t.Fatalf("a struct caller predating MenuGroup must still render: %v", err)
	}
	if !strings.Contains(b.String(), `name="`+MenuGroupDefault+`"`) {
		t.Errorf("struct caller landed outside the shared group: %s", b.String())
	}
}

func TestFuncsRegistersDictListMenuGroupSearchClearIconIconAssetsTTfAndDateWords(t *testing.T) {
	f := Funcs()
	for _, name := range []string{"dict", "list", "menuGroup", "searchClear", "icon", "iconAssets", "T", "Tf", "dateWords"} {
		if _, ok := f[name]; !ok {
			t.Errorf("Funcs() is missing %q", name)
		}
	}
	// Exactly these: an accidental extra is a helper the shipped partials
	// do not document and an app cannot rely on.
	if len(f) != 9 {
		t.Errorf("Funcs() has %d entries, want exactly 9", len(f))
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
	for _, name := range []string{"dict", "list", "menuGroup", "searchClear", "icon", "iconAssets", "T", "Tf", "dateWords"} {
		if _, ok := f[name]; !ok {
			t.Errorf("FuncsWith(...) is missing %q", name)
		}
	}
	if len(f) != 9 {
		t.Errorf("FuncsWith(...) has %d entries, want exactly 9", len(f))
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

// dateWords is the one helper that reads a whole family of keys rather
// than one: seventeen parser words, JSON-encoded into a single attribute
// so datetime.js gets its vocabulary without seventeen more attributes.
func TestDateWordsIsJSONOfTheWholeVocabulary(t *testing.T) {
	fn, ok := Funcs()["dateWords"].(func() string)
	if !ok {
		t.Fatalf("dateWords is %T, want func() string", Funcs()["dateWords"])
	}
	var words map[string]string
	if err := json.Unmarshal([]byte(fn()), &words); err != nil {
		t.Fatalf("dateWords() is not valid JSON (%v): %s", err, fn())
	}
	if len(words) != len(dateWordNames) {
		t.Errorf("dateWords() has %d entries, want %d", len(words), len(dateWordNames))
	}
	for _, short := range dateWordNames {
		want := rastrillo.BaseCatalog()["rastrillo.ui.date_"+short]
		if want == "" {
			t.Fatalf("base catalog has no rastrillo.ui.date_%s", short)
		}
		if words[short] != want {
			t.Errorf("dateWords()[%q] = %q, want %q", short, words[short], want)
		}
	}
	// The short names are the parser's, not the catalog's: no key prefix
	// travels to the browser.
	if _, leaked := words["rastrillo.ui.date_today"]; leaked {
		t.Error("dateWords() shipped catalog keys as names")
	}
}

// It resolves through the BOUND t, so WithT localises the vocabulary per
// request exactly the way it localises every other partial default.
func TestDateWordsFollowsTheBoundT(t *testing.T) {
	fn, ok := FuncsWith(func(key string, _ ...any) string { return "X-" + key })["dateWords"].(func() string)
	if !ok {
		t.Fatal("FuncsWith did not rebind dateWords")
	}
	var words map[string]string
	if err := json.Unmarshal([]byte(fn()), &words); err != nil {
		t.Fatalf("rebound dateWords() is not valid JSON (%v): %s", err, fn())
	}
	for _, short := range dateWordNames {
		if want := "X-rastrillo.ui.date_" + short; words[short] != want {
			t.Errorf("rebound dateWords()[%q] = %q, want %q", short, words[short], want)
		}
	}
}

// Same input, same bytes: json.Marshal sorts map keys, so a page's markup
// does not churn between renders (and neither do the docs goldens).
func TestDateWordsIsDeterministic(t *testing.T) {
	fn := Funcs()["dateWords"].(func() string)
	first := fn()
	for i := 0; i < 20; i++ {
		if got := fn(); got != first {
			t.Fatalf("dateWords() run %d differs:\n%s\n%s", i, first, got)
		}
	}
}

// searchClear is the whole of §6-v2.1b.6's default: the ✕ beside a
// search field is a LINK to the same screen with q dropped, because the
// browser's own ✕ only clears the input's value and leaves the results
// and the ?q= exactly where they were.
func TestSearchClearDropsOnlyTheQuery(t *testing.T) {
	cases := []struct {
		name string
		data map[string]any
		want string
	}{
		{
			name: "filters and sort survive, only q goes",
			data: map[string]any{"Action": "/posts", "Hidden": [][2]string{{"status", "paid"}, {"sort", "newest"}}},
			want: "/posts?status=paid&sort=newest",
		},
		{
			name: "nothing to carry is the bare action",
			data: map[string]any{"Action": "/posts"},
			want: "/posts",
		},
		{
			// "" would resolve to the current URL, ?q= and all, so the
			// link would look like an affordance and do nothing — which
			// is the exact bug this whole feature exists to fix.
			name: "no action at all still clears the query string",
			data: map[string]any{},
			want: "?",
		},
		{
			name: "an action that already has a query gains an ampersand",
			data: map[string]any{"Action": "/posts?view=grid", "Hidden": [][2]string{{"sort", "newest"}}},
			want: "/posts?view=grid&sort=newest",
		},
		{
			name: "an action ending in ? or & does not gain a second one",
			data: map[string]any{"Action": "/posts?", "Hidden": [][2]string{{"sort", "newest"}}},
			want: "/posts?sort=newest",
		},
		{
			name: "names and values are escaped",
			data: map[string]any{"Action": "/posts", "Hidden": [][2]string{{"tag name", "a&b=c d"}}},
			want: "/posts?tag+name=a%26b%3Dc+d",
		},
		{
			name: "ClearHref always wins — the hook an app resets its paging with",
			data: map[string]any{"Action": "/posts", "Hidden": [][2]string{{"page", "7"}}, "ClearHref": "/posts"},
			want: "/posts",
		},
		{
			// The trap the partial's doc names: nothing in Hidden says
			// which pair is the page, so the default carries it. This
			// asserts the documented behaviour, not the desirable one.
			name: "the default carries a page number, because it cannot know",
			data: map[string]any{"Action": "/posts", "Hidden": [][2]string{{"page", "7"}}},
			want: "/posts?page=7",
		},
		{
			// The one pair it CAN know about. An app that carries its
			// whole query string across the GET wholesale hands q back in
			// Hidden, and carrying it would give the reader a ✕ that puts
			// the search back — the exact lie this link replaced.
			name: "a q in Hidden is dropped too, whatever else it carries",
			data: map[string]any{"Action": "/posts", "Hidden": [][2]string{{"status", "paid"}, {"q", "sere"}, {"sort", "newest"}}},
			want: "/posts?status=paid&sort=newest",
		},
		{
			name: "a Hidden that is nothing but q leaves a bare action",
			data: map[string]any{"Action": "/posts", "Hidden": [][2]string{{"q", "sere"}}},
			want: "/posts",
		},
		{
			// list-bar names the same thing SearchAction, and hands the
			// computed default down to list-bar-search as ClearHref, so
			// searchClear runs twice over one search. It must be the
			// same answer both times.
			name: "SearchAction is read too, so list-bar can compute it once",
			data: map[string]any{"SearchAction": "/posts", "Hidden": [][2]string{{"sort", "newest"}}},
			want: "/posts?sort=newest",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := searchClear(c.data)
			if got != c.want {
				t.Errorf("searchClear = %q, want %q", got, c.want)
			}
			// Idempotence is load-bearing, not a nicety: list-bar
			// computes the href and passes it on as ClearHref, and a
			// second pass that changed the answer would silently give
			// the two partials different links for one search.
			if again := searchClear(map[string]any{"ClearHref": got}); again != c.want {
				t.Errorf("searchClear of its own result = %q, want %q", again, c.want)
			}
		})
	}
}

// The same trap menuGroup exists for: a struct caller that predates the
// key must keep rendering. .ClearHref written inline in the template
// would be an Execute error — a 500 on every list screen — so the key is
// read reflectively, and a struct with the field set is honoured.
func TestSearchClearReadsBothDataShapes(t *testing.T) {
	type filter struct {
		SearchAction string
		Query        string
		Placeholder  string
		Hidden       [][2]string
	}
	got := searchClear(filter{SearchAction: "/posts", Hidden: [][2]string{{"sort", "newest"}}})
	if want := "/posts?sort=newest"; got != want {
		t.Errorf("struct without ClearHref: searchClear = %q, want %q", got, want)
	}

	type withHook struct {
		Action    string
		ClearHref string
	}
	if got := searchClear(withHook{Action: "/posts", ClearHref: "/posts?page=1"}); got != "/posts?page=1" {
		t.Errorf("struct with ClearHref: searchClear = %q, want %q", got, "/posts?page=1")
	}

	// A caller passing a shape with none of the keys, and nil, are both
	// "no search to clear", not a panic.
	if got := searchClear(nil); got != "?" {
		t.Errorf("searchClear(nil) = %q, want %q", got, "?")
	}
	if got := searchClear(42); got != "?" {
		t.Errorf("searchClear(42) = %q, want %q", got, "?")
	}
}

// The rendered markup, end to end: the link is there when there is a
// query to clear and absent when there is not, and it carries an
// accessible name from the catalog because an icon-only link has no
// other one.
func TestListBarSearchRendersTheClearLink(t *testing.T) {
	tmpl := template.Must(template.New("").Funcs(Funcs()).ParseFS(Templates(), "*.html"))

	var with strings.Builder
	if err := tmpl.ExecuteTemplate(&with, "list-bar-search", map[string]any{
		"Action": "/posts", "Query": "sere", "Hidden": [][2]string{{"sort", "newest"}},
	}); err != nil {
		t.Fatalf("rendering the search form: %v", err)
	}
	// &amp; because this is an attribute in HTML, which is correct and
	// is what a browser reads back as a single &.
	if want := `href="/posts?sort=newest"`; !strings.Contains(with.String(), want) {
		t.Errorf("no clear link to %s in:\n%s", want, with.String())
	}
	if want := `aria-label="Clear search"`; !strings.Contains(with.String(), want) {
		t.Errorf("the clear link has no accessible name:\n%s", with.String())
	}

	var without strings.Builder
	if err := tmpl.ExecuteTemplate(&without, "list-bar-search", map[string]any{"Action": "/posts"}); err != nil {
		t.Fatalf("rendering the empty search form: %v", err)
	}
	if strings.Contains(without.String(), "rst-search__clear") {
		t.Errorf("an empty search offers a clear link:\n%s", without.String())
	}

	// A q carried in Hidden must not come back through the markup
	// either: the whole point of the link is that the search is gone.
	var carryingQ strings.Builder
	if err := tmpl.ExecuteTemplate(&carryingQ, "list-bar-search", map[string]any{
		"Action": "/posts", "Query": "sere", "Hidden": [][2]string{{"q", "sere"}, {"sort", "newest"}},
	}); err != nil {
		t.Fatalf("rendering a search that carries its own q: %v", err)
	}
	if want := `class="rst-search__clear" href="/posts?sort=newest"`; !strings.Contains(carryingQ.String(), want) {
		t.Errorf("the clear link is not %s — it is handing the query back:\n%s", want, carryingQ.String())
	}

	// list-bar renames Action to SearchAction and hands the whole search
	// down; the clear link has to survive that hop.
	var bar strings.Builder
	if err := tmpl.ExecuteTemplate(&bar, "list-bar", map[string]any{
		"SearchAction": "/posts", "Query": "sere", "Hidden": [][2]string{{"sort", "newest"}},
	}); err != nil {
		t.Fatalf("rendering the list bar: %v", err)
	}
	if want := `href="/posts?sort=newest"`; !strings.Contains(bar.String(), want) {
		t.Errorf("list-bar lost the clear link to %s:\n%s", want, bar.String())
	}
}
