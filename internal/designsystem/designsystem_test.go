package designsystem

import (
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/carlosframework/rastrillo"
	"github.com/carlosframework/rastrillo/ui"
)

// treeCommitted flips true the day docs/design-system lands in the
// repository (task 3 of the design-system-tree plan). Until then
// TestDesignSystemIsCurrent skips loudly rather than failing on a
// directory nobody has generated yet; from then on it is a hard gate —
// a hand-edited page, or a partial changed without re-running
// `go generate ./...`, fails the build.
//
// Same shape as ui/datetime_test.go's fixturesComplete, for the same
// reason: a gate that is temporarily unenforceable says so in one
// named constant instead of quietly not running.
const treeCommitted = true

// treeDir is docs/design-system, relative to this package.
const treeDir = "../../docs/design-system"

// maxTreeBytes is the ceiling the whole rendered tree has to stay
// under. The spec budgets ~15 MB; 20 MB is the line where "committed
// static HTML" stops being reviewable and starts being a binary blob.
const maxTreeBytes = 20 << 20

// render is Render() with the error already fatal — every test here
// starts the same way.
func render(t *testing.T) map[string][]byte {
	t.Helper()
	files, err := Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("Render returned no files")
	}
	return files
}

// Two renders must be byte-identical: every map in the pipeline
// (Styleguide's samples, BaseCatalogs, the parsed token blocks) is
// sorted before it can reach output. Without this gate a Go map's
// randomised iteration order would show up as a churning diff in a
// committed tree of 180 pages.
func TestRenderIsDeterministic(t *testing.T) {
	first, second := render(t), render(t)
	if !reflect.DeepEqual(first, second) {
		var diffs []string
		for name, a := range first {
			b, ok := second[name]
			if !ok {
				diffs = append(diffs, "only in first: "+name)
				continue
			}
			if string(a) != string(b) {
				diffs = append(diffs, "differs: "+name)
			}
		}
		for name := range second {
			if _, ok := first[name]; !ok {
				diffs = append(diffs, "only in second: "+name)
			}
		}
		sort.Strings(diffs)
		t.Fatalf("Render is not deterministic:\n%s", strings.Join(diffs, "\n"))
	}
}

// definedPartials enumerates ui's partials the way an app does —
// parse the whole embedded set, list the templates that carry a
// {{define}} rather than a file body. The gate below reads this list
// independently of the renderer's own family table, so a partial added
// to ui and forgotten in samples.go fails here.
func definedPartials(t *testing.T) []string {
	t.Helper()
	tmpl, err := template.New("").Funcs(ui.Funcs()).ParseFS(ui.Templates(), "*.html")
	if err != nil {
		t.Fatalf("parsing ui.Templates: %v", err)
	}
	var names []string
	for _, tt := range tmpl.Templates() {
		name := tt.Name()
		if name == "" || strings.HasSuffix(name, ".html") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatal("no partials found")
	}
	return names
}

// Every partial ui ships is rendered on the page, marked by a comment
// the renderer emits per partial section. The marker is the gate
// surface deliberately: greping rendered HTML for a class or a string
// would break every time a partial's markup is tidied, and this gate
// is about coverage, not markup.
func TestEveryPartialAppearsOnThePage(t *testing.T) {
	page := string(render(t)[RootTheme()+"/en/index.html"])
	if page == "" {
		t.Fatalf("no %s/en/index.html in the rendered tree", RootTheme())
	}
	for _, name := range definedPartials(t) {
		if !strings.Contains(page, "<!-- partial: "+name+" -->") {
			t.Errorf("partial %q is not on the page (no marker comment)", name)
		}
	}
}

// Every class idiom ui.Styleguide ships is rendered on the page, same
// marker mechanism.
func TestEveryStyleguideSampleAppears(t *testing.T) {
	page := string(render(t)[RootTheme()+"/en/index.html"])
	if page == "" {
		t.Fatalf("no %s/en/index.html in the rendered tree", RootTheme())
	}
	names := make([]string, 0, len(ui.Styleguide()))
	for name := range ui.Styleguide() {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !strings.Contains(page, "<!-- idiom: "+name+" -->") {
			t.Errorf("class idiom %q is not on the page (no marker comment)", name)
		}
	}
}

// The whole tree is committed, so its size is a review cost, not just a
// disk cost.
func TestTreeStaysUnderTheSizeGate(t *testing.T) {
	var total int
	for _, b := range render(t) {
		total += len(b)
	}
	if total > maxTreeBytes {
		t.Errorf("rendered tree is %d bytes, over the %d-byte gate", total, maxTreeBytes)
	}
	t.Logf("rendered tree: %d files, %d bytes (%.2f MiB)", len(render(t)), total, float64(total)/(1<<20))
}

// The tree the repository carries must be exactly what Render()
// produces: `go generate ./...` is the only way it changes.
func TestDesignSystemIsCurrent(t *testing.T) {
	if !treeCommitted {
		t.Skip("docs/design-system is not committed yet — task 3 generates it " +
			"and flips treeCommitted in internal/designsystem/designsystem_test.go; " +
			"until then this gate cannot run and the tree cannot drift, " +
			"because there is no tree")
	}
	want := render(t)
	for name, body := range want {
		got, err := os.ReadFile(filepath.Join(treeDir, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("%s: %v (run `go generate ./...`)", name, err)
		}
		if string(got) != string(body) {
			t.Fatalf("%s differs from the committed tree (run `go generate ./...`)", name)
		}
	}
	root := os.DirFS(treeDir)
	err := fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if _, ok := want[p]; !ok {
			t.Errorf("%s is committed but nothing renders it (run `go generate ./...`)", p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", treeDir, err)
	}
}

var (
	langAttr      = regexp.MustCompile(`<html lang="([^"]*)" dir="([^"]*)"`)
	unresolvedKey = "rastrillo.ui."
)

// chromeLinks finds the URLs the renderer itself emits into a page's
// chrome: the stylesheets, the scripts and the shell iframes. Every one
// of them has to be an absolute path under /design-system/, because the
// static edge serves this tree's directory indexes at their slash-less
// URL without redirecting and a relative path resolves against the
// wrong base there.
//
// The escaped shell source in the <pre> blocks is invisible to these:
// it reaches the page as &lt;link …, not as markup.
var chromeLinks = []struct {
	what string
	re   *regexp.Regexp
}{
	{"stylesheet link", regexp.MustCompile(`<link[^>]*\shref="([^"]*)"`)},
	{"script src", regexp.MustCompile(`<script[^>]*\ssrc="([^"]*)"`)},
	{"iframe src", regexp.MustCompile(`<iframe[^>]*\ssrc="([^"]*)"`)},
}

// anchorHref finds every <a href>. Anchors are mixed: the switchers,
// the shell demo links and the back-links are the renderer's own and
// must resolve, while the hrefs inside the samples are content —
// /orders/AB3PX and friends, sample data pointing at routes no static
// site serves. The rule that separates them without marking up either
// is the mount prefix: an href under /design-system/ is a link into
// this tree and must hit a file, and an href that is not is a sample.
var anchorHref = regexp.MustCompile(`<a[^>]*\shref="([^"]*)"`)

// mountPrefix is where every internal link starts.
const mountPrefix = mountPath + "/"

// resolves reports whether an absolute in-tree URL names a file the
// renderer actually produces.
func resolves(files map[string][]byte, href string) bool {
	target := strings.TrimPrefix(href, mountPrefix)
	if i := strings.IndexAny(target, "#?"); i >= 0 {
		target = target[:i]
	}
	_, ok := files[target]
	return ok
}

// Every page is a whole document in the right language, with no
// unresolved catalog key; every asset it loads and every link it makes
// into this tree is an absolute path under /design-system/ that names a
// file the tree contains.
//
// The absolute half of that is the fix for the live bug: rastrillo.org
// served /design-system unstyled, because the edge returns the
// directory index at the slash-less URL with no redirect and every
// relative href on it then resolved one directory too high. The
// resolves half is new coverage the relative scheme never had — it
// catches a switcher pointing at a locale the tree does not ship, or a
// shell demo link naming a file that moved.
func TestEveryPageIsAWholeLocalisedDocument(t *testing.T) {
	files := render(t)
	names := make([]string, 0, len(files))
	for name := range files {
		if strings.HasSuffix(name, ".html") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatal("no HTML pages rendered")
	}
	for _, name := range names {
		body := string(files[name])
		if !strings.HasPrefix(body, "<!doctype html>") {
			t.Errorf("%s: does not start with a doctype", name)
			continue
		}
		// One document per file. A second doctype means a whole page
		// got rendered inside itself, which is exactly what happened
		// the first time the orphan sweep ran over the page's own tree
		// and found the page template sitting in it.
		if n := strings.Count(body, "<!doctype html>"); n != 1 {
			t.Errorf("%s: %d doctypes — a document is nested inside another", name, n)
		}
		if !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
			t.Errorf("%s: does not end with </html>", name)
		}
		m := langAttr.FindStringSubmatch(body)
		if m == nil {
			t.Errorf("%s: no <html lang=… dir=…>", name)
			continue
		}
		locale := localeOfPath(name)
		if m[1] != locale {
			t.Errorf("%s: lang=%q, want %q", name, m[1], locale)
		}
		if want := rastrillo.Dir(locale); m[2] != want {
			t.Errorf("%s: dir=%q, want %q", name, m[2], want)
		}
		if strings.Contains(body, unresolvedKey) {
			t.Errorf("%s: an unresolved catalog key leaked into the page", name)
		}
		for _, kind := range chromeLinks {
			for _, m := range kind.re.FindAllStringSubmatch(body, -1) {
				href := m[1]
				if !strings.HasPrefix(href, mountPrefix) {
					t.Errorf("%s: %s %q is not an absolute path under %s", name, kind.what, href, mountPrefix)
					continue
				}
				if !resolves(files, href) {
					t.Errorf("%s: %s %q names nothing in the tree", name, kind.what, href)
				}
			}
		}
		for _, m := range anchorHref.FindAllStringSubmatch(body, -1) {
			href := m[1]
			if !strings.HasPrefix(href, mountPrefix) {
				continue // sample content, or a fragment
			}
			if !resolves(files, href) {
				t.Errorf("%s: link %q names nothing in the tree", name, href)
			}
		}
	}
}

// localeOfPath reads the locale a page is for out of its path:
// "<theme>/<locale>/…", and the root index is the default theme's
// English page by definition.
func localeOfPath(p string) string {
	parts := strings.Split(path.Clean(p), "/")
	if len(parts) < 2 {
		return "en"
	}
	return parts[1]
}

// No index page may render a live modal. `.rst-modal-overlay` is
// `position: fixed; inset: 0; z-index: 10` and
// `body:has(.rst-backdrop) { overflow: hidden }`, so the modal sample
// rendered inline did not sit in the gallery's flow at all: every index
// page loaded with a full-viewport modal over it, the content behind it
// unscrollable, and its Close link — the sample's own `/settings` —
// a 404. That was the live bug on rastrillo.org/design-system.
//
// The cure is the shells': escaped source in a <pre>, with the markup
// live at its own URL. Escaped source cannot trip this gate, which is
// what makes a plain string match the right instrument —
// html/template writes the sample's quotes as &#34; inside the <code>
// element, so `class="rst-modal-overlay"` occurs only where a browser
// would actually lay the overlay out.
//
// The second half of the gate is the one that matters a year from now:
// the source and the demo link have to be there. A gate that only
// forbade the live markup would pass just as happily on a page that had
// dropped the modal idiom altogether.
func TestNoIndexPageOpensAModalOverTheGallery(t *testing.T) {
	files := render(t)
	names := make([]string, 0, len(files))
	for name := range files {
		if strings.HasSuffix(name, "index.html") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatal("no index pages rendered")
	}
	for _, name := range names {
		body := string(files[name])
		for _, live := range []string{`class="rst-modal-overlay"`, `class="rst-backdrop"`} {
			if strings.Contains(body, live) {
				t.Errorf("%s renders a live %s — the overlay is fixed to the viewport and covers the page", name, live)
			}
		}
		if !strings.Contains(body, `class=&#34;rst-modal-overlay&#34;`) {
			t.Errorf("%s does not show the modal sample as escaped source", name)
		}
		theme, locale := themeLocaleOfPath(name)
		if href := modalHref(theme, locale); !strings.Contains(body, `href="`+href+`"`) {
			t.Errorf("%s does not link its modal demo (%s)", name, href)
		}
	}
}

// themeLocaleOfPath reads a page's theme and locale out of its path.
// The tree root is the default theme in English by definition, the same
// way localeOfPath treats it.
func themeLocaleOfPath(p string) (theme, locale string) {
	parts := strings.Split(path.Clean(p), "/")
	if len(parts) < 3 {
		return RootTheme(), "en"
	}
	return parts[0], parts[1]
}

// Every theme × locale × shell combination is present, plus the root
// index and the seven shared assets — the tree's shape is part of its
// contract with the website's sync script.
func TestTreeShapeIsComplete(t *testing.T) {
	files := render(t)
	want := []string{
		"index.html",
		"tokens.css", "rastrillo.js", "select.js", "datetime.js", "gallery.js",
	}
	for _, theme := range ui.ThemeNames() {
		want = append(want, "theme-"+theme+".css")
		for _, locale := range rastrillo.BaseLocales() {
			want = append(want, fmt.Sprintf("%s/%s/index.html", theme, locale))
			want = append(want, fmt.Sprintf("%s/%s/modal.html", theme, locale))
			for _, shell := range ui.LayoutNames() {
				want = append(want, fmt.Sprintf("%s/%s/shells/%s.html", theme, locale, shell))
			}
		}
	}
	for _, name := range want {
		if _, ok := files[name]; !ok {
			t.Errorf("missing from the tree: %s", name)
		}
	}
	// The theme half of the path/stylesheet wiring: a page under
	// <theme>/ must link that theme's stylesheet, not another one's.
	// Nothing else notices a swapped variable here — every page would
	// still be a valid document, just painted in the wrong palette.
	for _, theme := range ui.ThemeNames() {
		for _, locale := range rastrillo.BaseLocales() {
			path := fmt.Sprintf("%s/%s/index.html", theme, locale)
			if want := `href="` + mountPrefix + `theme-` + theme + `.css"`; !strings.Contains(string(files[path]), want) {
				t.Errorf("%s does not link its own theme (%s)", path, want)
			}
		}
	}
	if len(files) != len(want) {
		t.Errorf("tree has %d files, expected exactly %d", len(files), len(want))
	}
}

// The root index is the default theme's English page, byte for byte. It
// used to be a second render at a shallower depth whose every path came
// out different, and this gate blanked the hrefs to compare the rest;
// with absolute paths there is no difference left to allow for, so the
// gate asserts the stronger thing directly.
func TestRootIndexIsTheDefaultThemeInEnglishAtTheTreeRoot(t *testing.T) {
	files := render(t)
	nestedPath := RootTheme() + "/en/index.html"
	root, nested := string(files["index.html"]), string(files[nestedPath])
	if root == "" || nested == "" {
		t.Fatalf("root or %s index missing", nestedPath)
	}
	if !strings.Contains(root, `href="`+mountPrefix+`tokens.css"`) {
		t.Errorf("root index does not link %stokens.css", mountPrefix)
	}
	if root != nested {
		t.Errorf("the root index is not byte-identical to %s", nestedPath)
	}
	// No page in the tree may climb with a relative path, wherever it
	// sits: that is the whole of the bug this shape fixes.
	for name, body := range files {
		if strings.HasSuffix(name, ".html") && strings.Contains(string(body), `="../`) {
			t.Errorf("%s carries a relative up-path", name)
		}
	}
}

// Both enhanced controls are on the page: the filterable select and the
// natural-language date combobox both boot from the JavaScript the tree
// ships, so the page has to give them something to boot on.
func TestEnhancedControlsAreOnThePage(t *testing.T) {
	page := string(render(t)[RootTheme()+"/en/index.html"])
	for _, want := range []string{"data-rst-select", "data-rst-date", "data-rst-time", "data-rst-range"} {
		if !strings.Contains(page, want) {
			t.Errorf("no %s on the page — the enhancement has nothing to boot on", want)
		}
	}
	if !strings.Contains(page, "<optgroup") {
		t.Error("no hand-written optgroup'd select on the page")
	}
}

// ── The gallery's own words ──────────────────────────────────────────

// nonEnglish is the eleven locales prose.go has to carry a translation
// for. en is the twelfth and is the key itself, so it is not in the
// table — see prose.go's header.
func nonEnglish() []string {
	out := make([]string, 0, len(rastrillo.BaseLocales())-1)
	for _, code := range rastrillo.BaseLocales() {
		if code != "en" {
			out = append(out, code)
		}
	}
	return out
}

// placeholderNames pulls the {name} placeholders out of a string.
var placeholderNames = regexp.MustCompile(`\{([a-z]+)\}`)

// proseKeysRendered is every prose key one full Render actually asks
// for. Read off the renderer rather than off a list beside prose.go: a
// list would go stale the first time a sample gained a note, and this
// cannot.
func proseKeysRendered(t *testing.T) []string {
	t.Helper()
	asked := map[string]bool{}
	stop := setProseTrace(func(en string) { asked[en] = true })
	defer stop()
	if _, err := Render(); err != nil {
		t.Fatalf("Render: %v", err)
	}
	keys := make([]string, 0, len(asked))
	for k := range asked {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		t.Fatal("the renderer asked for no prose at all — the tracer is not wired up")
	}
	return keys
}

// Every string the page says in its own voice exists in all twelve
// shipped locales, non-empty, with its placeholders intact — the same
// promise TestBaseCatalogsShareOneKeySet makes for the framework's own
// catalog, made for the gallery's own words.
//
// The key set is the one a real Render asks for, so a sample added to
// samples.go with an English note fails here on the day it lands, not
// on the day somebody notices the Japanese page still reading English.
// The gate runs the other way too: an entry nothing renders is a
// translation of a sentence the page no longer says, and it goes.
func TestEveryProseKeyIsTranslated(t *testing.T) {
	keys := proseKeysRendered(t)
	shipped := map[string]bool{}
	for _, code := range rastrillo.BaseLocales() {
		shipped[code] = true
	}

	for _, key := range keys {
		row, ok := prose[key]
		if !ok {
			t.Errorf("prose.go has no entry for %q — every string the page says has to be translated", key)
			continue
		}
		for _, locale := range rastrillo.BaseLocales() {
			if strings.TrimSpace(proseIn(locale, key)) == "" {
				t.Errorf("%s renders empty for %q", locale, key)
			}
		}
		for _, locale := range nonEnglish() {
			if strings.TrimSpace(row[locale]) == "" {
				t.Errorf("prose.go: %q has no %s translation", key, locale)
			}
		}
		for code := range row {
			if !shipped[code] {
				t.Errorf("prose.go: %q carries %q, which is not a shipped locale", key, code)
			}
			if code == "en" {
				t.Errorf("prose.go: %q carries an en value; en is the key", key)
			}
		}
		// A placeholder that a translator dropped or misspelled is the
		// one kind of damage the page cannot show honestly: interpolate
		// leaves an unmatched {name} on the page, and a missing one
		// silently deletes the value the sentence was built around.
		for _, m := range placeholderNames.FindAllStringSubmatch(key, -1) {
			for _, locale := range nonEnglish() {
				if !strings.Contains(row[locale], m[0]) {
					t.Errorf("prose.go: the %s translation of %q lost the %s placeholder", locale, key, m[0])
				}
			}
		}
	}

	rendered := map[string]bool{}
	for _, k := range keys {
		rendered[k] = true
	}
	stale := make([]string, 0)
	for k := range prose {
		if !rendered[k] {
			stale = append(stale, k)
		}
	}
	sort.Strings(stale)
	for _, k := range stale {
		t.Errorf("prose.go carries %q, which nothing on the page renders any more", k)
	}
	t.Logf("%d prose keys × %d locales", len(keys), len(rastrillo.BaseLocales()))
}

// proseLeakFloor is the length above which an English prose string is a
// sentence and below which it is a word. Only the sentences are swept
// for; at the time of writing that is 148 of the 190 keys, and the
// other 42 — "Theme", "Tokens", "Sections", "Shells", "Required",
// "Type scale", "Pick one", every switcher label and every one-word
// state — are not swept for at all.
//
// That is a smaller hole than it sounds, because a short label is the
// PARITY gate's job, not this one's. TestEveryProseKeyIsTranslated
// walks every key in all twelve locales, so a switcher label with no
// Japanese translation fails the build there. What the floor gives up
// is only the second, belt-and-braces proof that the translation
// reached the page — for the strings where that proof cannot be had.
//
// It cannot be had because a bare English word occurs on a translated
// page for reasons that are nothing to do with prose. Four keys
// actually collide today, and they are the justification for the floor
// rather than an exhaustive list of what it exempts: "Failed" and
// "Full" are status-pill labels a fixture chose, "Display" is inside
// the count line's "Displaying 1–20", and "On" is a substring of half
// the English in the page's sample data. Gating those would fail on
// fixtures, which is the opposite of what this gate is for. Twelve
// characters is the shortest key that is a phrase; below it, matching a
// bare word proves nothing either way.
const proseLeakFloor = 12

// proseFixtureCollisions names the prose keys that are ALSO sample data
// somewhere in the tree, against the page kind the fixture lives on.
// The sweep skips such a key on those pages and keeps checking it
// everywhere else, so the guard stays where the prose actually is
// rather than being dropped for the whole tree.
//
// ── The boundary this map sits on ────────────────────────────────────
//
// Sample content stays English on every page: the names, the routes and
// the labels in the component samples are stand-ins, and translating
// them would suggest the framework ships those words. The shell and
// modal demos are the other way round — they impersonate a real
// application, so their chrome speaks the language the reader chose.
// The page says this out loud too, under Partials, in all twelve
// languages; this comment is the same sentence where a maintainer meets
// it rather than a reader.
//
// So this is not a bug to be tidied away by translating the fixture.
// The shell demos and the gallery genuinely draw the same words in two
// different roles, and only the page they are on tells the two apart.
// Prefer this to widening proseLeakFloor: a floor exempts every short
// key at once, and this exempts one key on one kind of page, in
// writing.
var proseFixtureCollisions = map[string]string{
	// The shell demos' sample screen says this as its own chrome and
	// translates it (page.go's shellTemplate). samples.go passes the
	// same words as page-header's ActionLabel, as fixture, on every
	// gallery index — so on an index page an English "Write a post" is
	// the fixture doing its job, and on a shell demo it would be a leak.
	"Write a post": "index.html",
}

// escapedSource strips the <pre class="ds-src"> blocks out of a page.
// They hold ui.Styleguide's samples verbatim — English markup a reader
// is meant to copy — so English inside them is the point, not a leak.
// Two of prose.go's own keys appear in there, because the modal sample
// and this package's own modal demo say the same sentences.
func escapedSource(page string) string {
	for {
		i := strings.Index(page, `<pre class="ds-src`)
		if i < 0 {
			return page
		}
		j := strings.Index(page[i:], "</pre>")
		if j < 0 {
			return page[:i]
		}
		page = page[:i] + page[i+j+len("</pre>"):]
	}
}

// proseSentinels are three English strings that must be on the English
// page and must not be on any other. They are named here, rather than
// left to the sweep below, because a sweep can be weakened by accident
// — drop a key from prose.go and it stops being checked — and these
// three cannot be: the gate asserts they are present in English first.
var proseSentinels = []string{
	"Every partial, class idiom and design token the framework ships, on one page.",
	"The links in these samples go nowhere",
	"Screens stack vertically",
}

// No English the page says in its own voice reaches a translated page.
// This is the gate the whole of prose.go exists to pass, and it is
// strong precisely because prose.go's keys ARE the English: every
// sentence the renderer can emit is a sentinel, not just the three
// named above.
//
// Sample data is exempt and deliberately so — the fixtures are English
// names, English routes and English record titles, and translating
// "Grace Hopper" would be a different and worse kind of dishonesty. So
// is the escaped source, which is markup to copy.
func TestNoEnglishProseReachesATranslatedPage(t *testing.T) {
	files := render(t)
	keys := proseKeysRendered(t)
	en := string(files[RootTheme()+"/en/index.html"])
	if en == "" {
		t.Fatal("no English index page")
	}
	for _, s := range proseSentinels {
		if !strings.Contains(en, s) {
			t.Errorf("sentinel %q is not on the English page — it has been reworded, and this gate has been checking nothing", s)
		}
	}

	// Every page in a language that is not English, not just the
	// galleries: the modal demo and the three shell demos are 144 of
	// the tree's 181 documents, and they say prose of their own. A
	// sweep over the index pages alone let a brand-new English sentence
	// in modalTemplate through every gate in this file.
	names := make([]string, 0, len(files))
	for name := range files {
		if strings.HasSuffix(name, ".html") && localeOfPath(name) != "en" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	// Asserted, not assumed: a refactor that quietly narrowed what this
	// loop walks would leave the gate passing over fewer pages, which
	// is the failure mode it was just extended to fix. Per theme, per
	// non-English locale: an index, a modal demo and one page per shell.
	if want := len(ui.ThemeNames()) * (len(rastrillo.BaseLocales()) - 1) * (2 + len(ui.LayoutNames())); len(names) != want {
		t.Errorf("sweeping %d translated pages, want %d", len(names), want)
	}

	sentences := make([]string, 0, len(keys))
	for _, key := range keys {
		if len([]rune(key)) >= proseLeakFloor {
			sentences = append(sentences, key)
		}
	}

	for _, name := range names {
		page := escapedSource(string(files[name]))
		if page == "" {
			t.Errorf("%s is empty", name)
			continue
		}
		for _, s := range proseSentinels {
			if strings.Contains(page, s) {
				t.Errorf("%s still says %q", name, s)
			}
		}
		for _, key := range sentences {
			if on, ok := proseFixtureCollisions[key]; ok && strings.HasSuffix(name, on) {
				continue
			}
			if strings.Contains(page, key) {
				t.Errorf("%s carries the English %q", name, key)
			}
		}
	}
}

// ── The chrome ───────────────────────────────────────────────────────

// The three switchers sit in the gallery's own <header>, above main,
// and each says which of its options is the current one. The scheme
// toggle's server-rendered answer is System, because System is the only
// state a page with no JavaScript can be in.
func TestTheChromeCarriesTheThreeSwitchers(t *testing.T) {
	files := render(t)
	for _, theme := range ui.ThemeNames() {
		for _, locale := range rastrillo.BaseLocales() {
			name := theme + "/" + locale + "/index.html"
			page := string(files[name])
			// The chrome is read out of the page by its own element,
			// not by cutting at main: the <style> block in the head
			// mentions aria-pressed in a selector, and a looser slice
			// counted the stylesheet as a fourth button.
			_, after, ok := strings.Cut(page, `<header class="ds-chrome">`)
			if !ok {
				t.Errorf("%s: no gallery header", name)
				continue
			}
			chrome, rest, ok := strings.Cut(after, "</header>")
			if !ok {
				t.Errorf("%s: the gallery header never closes", name)
				continue
			}
			// The chrome moved inside the shell's main column when the
			// sidebar landed, so what follows it is the content
			// column rather than main itself. It is still the first
			// thing in the reading order of the page's own content.
			if !strings.HasPrefix(strings.TrimSpace(rest), `<div class="rst-page">`) {
				t.Errorf("%s: the gallery header is not the element immediately before the content column", name)
			}
			if _, chromeStart, _ := strings.Cut(page, `<main class="rst-shell__main" id="main">`); !strings.HasPrefix(strings.TrimSpace(chromeStart), `<header class="ds-chrome">`) {
				t.Errorf("%s: the gallery header is not the first thing inside main", name)
			}
			// The theme switcher: one link per theme, exactly one current.
			if n := strings.Count(chrome, `aria-current="page"`); n != 1 {
				t.Errorf("%s: %d themes marked current, want 1", name, n)
			}
			// The language switcher: one link per locale, exactly one current.
			if n := strings.Count(chrome, `aria-current="true"`); n != 1 {
				t.Errorf("%s: %d locales marked current, want 1", name, n)
			}
			if n := strings.Count(chrome, `<a href="`+mountPrefix); n != len(ui.ThemeNames())+len(rastrillo.BaseLocales()) {
				t.Errorf("%s: the chrome has %d in-tree links, want one per theme and one per locale", name, n)
			}
			// The scheme toggle: three buttons, System pressed.
			for _, value := range []string{"system", "light", "dark"} {
				if !strings.Contains(chrome, `data-ds-scheme="`+value+`"`) {
					t.Errorf("%s: the scheme toggle has no %s button", name, value)
				}
			}
			if n := strings.Count(chrome, `aria-pressed="true"`); n != 1 {
				t.Errorf("%s: %d scheme buttons pressed, want exactly 1 (System)", name, n)
			}
			if n := strings.Count(chrome, `aria-pressed="false"`); n != 2 {
				t.Errorf("%s: %d scheme buttons unpressed, want 2", name, n)
			}
			if !strings.Contains(chrome, `data-ds-scheme="system" aria-pressed="true"`) {
				t.Errorf("%s: System is not the pressed scheme with no JavaScript", name)
			}
			// Every one of the three is named, in this page's language.
			for _, label := range []string{
				proseIn(locale, "Theme"),
				proseIn(locale, "Colour scheme"),
			} {
				if !strings.Contains(chrome, `aria-label="`+template.HTMLEscapeString(label)+`"`) {
					t.Errorf("%s: the chrome has no group labelled %q", name, label)
				}
			}
		}
	}
}

// gallery.js is loaded before the body so the remembered scheme is
// applied and the toggle revealed in the same parse — a deferred script
// would flash the system scheme at a reader who chose Dark, and pop the
// control into a bar they are already looking at.
func TestGalleryScriptLoadsBeforeTheBody(t *testing.T) {
	page := string(render(t)[RootTheme()+"/en/index.html"])
	tag := `<script src="` + mountPrefix + `gallery.js"></script>`
	i := strings.Index(page, tag)
	if i < 0 {
		t.Fatalf("no blocking gallery.js tag on the page (want %s)", tag)
	}
	if body := strings.Index(page, "<body>"); i > body {
		t.Error("gallery.js loads after <body> — the scheme it restores will flash")
	}
	if strings.Contains(page, `<script defer src="`+mountPrefix+`gallery.js"></script>`) {
		t.Error("gallery.js is deferred; see its header comment for why it is not")
	}
}

// gallery.js is first-party on the same terms as the three framework
// scripts: no network, no dependency, no build step. TestScriptsAreSelfContained
// in ui/ holds the other three; this is the fourth, which does not live
// in ui/ because no app is ever given it.
func TestGalleryScriptStaysInertAndFirstParty(t *testing.T) {
	js := string(GalleryJS())
	for _, bad := range []string{"http://", "https://", "import ", "require(", "//cdn"} {
		if strings.Contains(js, bad) {
			t.Errorf("gallery.js reaches outside the page (%q)", bad)
		}
	}
	if strings.Contains(js, "\t") {
		t.Error("gallery.js uses two-space indentation, not tabs")
	}
	// 8 KiB was the budget when this file did one thing. It does two
	// now — the toggle and the sidebar filter — and the ceiling moved
	// once, with the second feature, rather than being shaved off the
	// comments that are this file's documentation. It is still a
	// ceiling: a third feature is a conversation, not a bump.
	if n := len(js); n > 10*1024 {
		t.Errorf("gallery.js is %d bytes; it is the gallery's own furniture and should stay readable in one sitting", n)
	}
	// The two halves of the scriptless story: the toggle is hidden
	// until this file says otherwise, and System removes the attribute
	// rather than setting a third value.
	if !strings.Contains(js, `setAttribute("data-rst-js"`) {
		t.Error("gallery.js does not set data-rst-js — the toggle stays display:none and nothing works")
	}
	if !strings.Contains(js, `removeAttribute("data-theme")`) {
		t.Error("gallery.js never removes data-theme — System would not be reachable")
	}
	page := string(render(t)[RootTheme()+"/en/index.html"])
	if !strings.Contains(page, ".ds-scheme { display: none; }") {
		t.Error("the page does not hide the scheme toggle by default — with scripts off it would look like a control that works")
	}
	if !strings.Contains(page, `:root[data-rst-js] .ds-scheme`) {
		t.Error("the page never reveals the scheme toggle for a reader who has JavaScript")
	}
	// The filter tells the same story, in the same two rules. The nav
	// under it is a complete list of every anchor on the page either
	// way; the box that filters it is the part that needs a script.
	if !strings.Contains(page, ".ds-search { display: none; }") {
		t.Error("the page does not hide the filter box by default — with scripts off it would look like a control that works")
	}
	if !strings.Contains(page, `:root[data-rst-js] .ds-search`) {
		t.Error("the page never reveals the filter box for a reader who has JavaScript")
	}
	if !strings.Contains(js, `querySelector("[data-ds-filter]")`) {
		t.Error("gallery.js does not look for the filter box — the seam is empty")
	}
}

// ── The sidebar ──────────────────────────────────────────────────────

var (
	// An element the sidebar is allowed to link. The attribute is the
	// marker, exactly as the coverage gates use a comment: an element
	// that grew an id for some other reason (main, the four section
	// headings, a sample's own form field) is not a nav target, and a
	// section that lost its id is not one either.
	anchorMarker = regexp.MustCompile(`id="([^"]*)" data-ds-anchor`)
	// A link into this page. The rail's other links — the demo pages —
	// are absolute paths, and TestEveryPageIsAWholeLocalisedDocument
	// already holds those to files the tree contains.
	navFragment   = regexp.MustCompile(`<a href="#([^"]*)"`)
	navHref       = regexp.MustCompile(`<a href="([^"]*)"`)
	elementID     = regexp.MustCompile(`\bid="([^"]*)"`)
	partialMarker = regexp.MustCompile(`<!-- partial: (\S+) -->`)
	idiomMarker   = regexp.MustCompile(`<!-- idiom: (\S+) -->`)
)

// railOf cuts the sidebar's nav out of a page. Everything below reads
// this slice rather than the whole document, so a stray anchor
// somewhere in a sample cannot make the rail look complete.
func railOf(t *testing.T, name, page string) string {
	t.Helper()
	_, after, ok := strings.Cut(page, `<nav class="rst-shell__nav ds-nav" id="ds-nav"`)
	if !ok {
		t.Errorf("%s: no sidebar nav", name)
		return ""
	}
	rail, _, ok := strings.Cut(after, "</nav>")
	if !ok {
		t.Errorf("%s: the sidebar nav never closes", name)
		return ""
	}
	return rail
}

// The rail is a reading of the page, not a second copy of it.
//
// The gate is a sequence comparison, which is stronger than it looks
// and is the reason the nav is derived in Go rather than written out in
// the template: the fragments the rail links, in rail order, must be
// exactly the anchored elements on the page, in page order. A partial
// added to ui appears in both lists or in neither — appearing in one is
// the failure. So is a rail entry pointing at a fragment nothing on the
// page carries, a section rendered with no way to reach it, and a
// reordering of one side and not the other.
//
// The two things it cannot see on its own, checked beside it: the
// marker comments the two coverage gates use (so a partial's anchor is
// derived from the same name they are), and the rail's out-of-page
// links. The third — that a fragment resolves to one element and not
// two — is TestNoPageCarriesTheSameIdTwice below, which is the whole
// tree's business and not only this page's.
func TestTheSidebarLinksEverythingOnThePageExactlyOnce(t *testing.T) {
	files := render(t)
	for _, theme := range ui.ThemeNames() {
		for _, locale := range rastrillo.BaseLocales() {
			name := theme + "/" + locale + "/index.html"
			page := string(files[name])
			rail := railOf(t, name, page)
			if rail == "" {
				continue
			}

			var anchors []string
			for _, m := range anchorMarker.FindAllStringSubmatch(page, -1) {
				anchors = append(anchors, m[1])
			}
			var fragments []string
			for _, m := range navFragment.FindAllStringSubmatch(rail, -1) {
				fragments = append(fragments, m[1])
			}
			if len(anchors) == 0 {
				t.Errorf("%s: nothing on the page is anchored at all — the marker has stopped working, and this gate with it", name)
				continue
			}
			if len(fragments) != len(anchors) {
				t.Errorf("%s: the sidebar links %d fragments, the page anchors %d", name, len(fragments), len(anchors))
			}
			for i := range anchors {
				if i >= len(fragments) {
					t.Errorf("%s: nothing in the sidebar links #%s", name, anchors[i])
					continue
				}
				if fragments[i] != anchors[i] {
					t.Errorf("%s: sidebar entry %d links #%s, the page's %dth anchor is #%s", name, i, fragments[i], i, anchors[i])
				}
			}

			// The anchors are the marker comments' own names, so the
			// two coverage gates and this one are looking at one list.
			linked := map[string]bool{}
			for _, f := range fragments {
				linked[f] = true
			}
			for _, kind := range []struct {
				prefix string
				re     *regexp.Regexp
			}{{"partial", partialMarker}, {"idiom", idiomMarker}} {
				found := kind.re.FindAllStringSubmatch(page, -1)
				if len(found) == 0 {
					t.Errorf("%s: no %s markers on the page", name, kind.prefix)
				}
				for _, m := range found {
					if want := anchorID(kind.prefix, m[1]); !linked[want] {
						t.Errorf("%s: %s %q is on the page and not in the sidebar (no #%s)", name, kind.prefix, m[1], want)
					}
				}
			}

			// Everything in the rail that is not a fragment leaves this
			// document, and there is exactly one such link per shell
			// demo plus the modal. They are the only reason the rail
			// links off-page at all.
			var away int
			for _, m := range navHref.FindAllStringSubmatch(rail, -1) {
				if strings.HasPrefix(m[1], "#") {
					continue
				}
				away++
				if !strings.HasPrefix(m[1], mountPrefix) || !resolves(files, m[1]) {
					t.Errorf("%s: the sidebar links %q, which is not a page of this tree", name, m[1])
				}
			}
			if want := len(ui.LayoutNames()) + 1; away != want {
				t.Errorf("%s: the sidebar has %d links off this page, want %d (one per shell demo, plus the modal)", name, away, want)
			}
		}
	}
}

// The gallery is laid out in the vocabulary it documents. Not a style
// preference: the sidebar shell is one of the three things this page
// exists to show, and a gallery that documented rst-shell-sidebar while
// being built out of something else would be advertising, not
// documentation. The mobile collapse comes with it — the <details>
// chrome strip is the shell's own, so the rail folds away below 800px
// with no JavaScript and nothing here to write.
func TestTheSidebarIsTheShellTheGalleryDocuments(t *testing.T) {
	files := render(t)
	for _, theme := range ui.ThemeNames() {
		for _, locale := range rastrillo.BaseLocales() {
			name := theme + "/" + locale + "/index.html"
			page := string(files[name])
			for _, want := range []string{
				`<div class="rst-shell-sidebar">`,
				`<details class="rst-shell__chrome">`,
				`<aside class="rst-shell__rail ds-rail">`,
				`<nav class="rst-shell__nav ds-nav" id="ds-nav"`,
				`<main class="rst-shell__main" id="main">`,
			} {
				if !strings.Contains(page, want) {
					t.Errorf("%s: the page is not laid out in the sidebar shell (no %s)", name, want)
				}
			}
			// The filter: a real search input, in a search landmark,
			// naming the nav it filters, with the "no matches" line
			// rendered by the page and hidden until something needs it.
			for _, want := range []string{
				`<search class="ds-search">`,
				`<input id="ds-filter" type="search"`,
				`aria-controls="ds-nav"`,
				`data-ds-filter`,
				`data-ds-filter-empty role="status" hidden`,
			} {
				if !strings.Contains(page, want) {
					t.Errorf("%s: the filter box is not there as specified (no %s)", name, want)
				}
			}
			// Five sections, every one of them open on arrival: the
			// rail is a table of contents first and a set of
			// disclosures second.
			rail := railOf(t, name, page)
			if n := strings.Count(rail, "<details open>"); n != 5 {
				t.Errorf("%s: %d open sidebar sections, want 5 (tokens, partials, idioms, shells, demos)", name, n)
			}
			if n := strings.Count(rail, "<details"); n != 5 {
				t.Errorf("%s: %d sidebar sections, and not all of them start open", name, n)
			}
			// Each of them says its name in this page's language, and
			// the nav says what it is.
			for _, key := range []string{"Tokens", "Partials", "Class idioms", "Shells", "Demos"} {
				want := "<summary>" + template.HTMLEscapeString(proseIn(locale, key)) + "</summary>"
				if !strings.Contains(rail, want) {
					t.Errorf("%s: no sidebar section named %q", name, proseIn(locale, key))
				}
			}
			if want := `aria-label="` + template.HTMLEscapeString(proseIn(locale, "Sections and demos")) + `"`; !strings.Contains(page, want) {
				t.Errorf("%s: the sidebar nav is not named in this page's language", name)
			}
		}
	}
}

// An id is unique in the document that carries it. Every page in the
// tree, not just the galleries: a duplicate id makes a fragment link
// silently unreachable — both entries scroll to the first element —
// and it is invalid HTML besides, which is the kind of thing that
// reads fine and behaves badly for years.
//
// Scoped to anchored ids when the sidebar first landed, on a guess that
// the component samples repeated form-field ids across states. They do
// not: all 181 documents in the tree carry no duplicate at all, so the
// gate is the whole page, which is the gate worth having.
func TestNoPageCarriesTheSameIdTwice(t *testing.T) {
	files := render(t)
	names := make([]string, 0, len(files))
	for name := range files {
		if strings.HasSuffix(name, ".html") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatal("no HTML pages rendered")
	}
	// The escaped source blocks cannot trip this: html/template writes
	// a sample's quotes as &#34;, so `id="` occurs only where a browser
	// would actually build an element.
	for _, name := range names {
		page := string(files[name])
		seen := map[string]int{}
		var order []string
		for _, m := range elementID.FindAllStringSubmatch(page, -1) {
			if seen[m[1]] == 0 {
				order = append(order, m[1])
			}
			seen[m[1]]++
		}
		if len(order) == 0 {
			t.Errorf("%s: no ids at all — every page in this tree has at least main's", name)
		}
		for _, id := range order {
			if seen[id] != 1 {
				t.Errorf("%s: id %q appears %d times; a fragment can only ever reach the first, and the document is invalid", name, id, seen[id])
			}
		}
	}
}

// ── English that never reaches a gate at all ─────────────────────────
//
// The two gates above both work off the prose TABLE: one checks every
// key is translated, the other checks no key's English reaches a
// translated page. Neither can see a sentence that was never a key —
// an author who writes <p>Some new English.</p> straight into a
// template has added a string the page says in one language and
// renders on a hundred and sixty-five pages that are in another, and
// nothing notices. Extending the leak sweep to the modal and shell
// demos does not help: there is no key to sweep for.
//
// That is the hole the gate below closes, at the source rather than in
// the output. What it covers, exactly — the first version of this
// comment claimed more than the code did, and the difference was two
// strings shipping untranslated on 99 pages:
//
//   - text between tags;
//   - the values of the four attributes a person reads or hears
//     (title, aria-label, alt, placeholder);
//   - the literal string values of dict arguments inside a template
//     invocation — {{template "status-pill" dict "Label" "Published"}}
//     was invisible to the first two passes, because the whole action
//     is nulled before either of them runs, and "Published" is what a
//     reader of that page reads.
//
// What it does NOT cover, stated so nobody has to rediscover it:
// anything inside a parenthesised sub-expression in an action (the
// paren reader takes it as one opaque token), any string a Go
// identifier in this package hands to the template rather than the
// template writing it (samples.go's data, idiomBlurbs and friends —
// those reach the page through proseIn, which the parity gate covers),
// and any template outside the three named in the test.

var (
	// A template action. Non-greedy over any character, because the
	// argument to P routinely contains a single brace: {language} in
	// the switcher's screen-reader text, {theme} in the tokens lead.
	templateAction = regexp.MustCompile(`(?s)\{\{.*?\}\}`)
	// A style or script element, taken whole. Stripped first: dsCSS is
	// concatenated into indexTemplate, and CSS is full of the > and {
	// characters the passes below are looking for.
	templateBlock = regexp.MustCompile(`(?s)<(style|script)\b.*?</(style|script)>`)
	templateTag   = regexp.MustCompile(`<[^>]*>`)
	// The attributes whose values a person reads or hears. Everything
	// else in a tag is machinery.
	templateSpokenAttr = regexp.MustCompile(`\b(title|aria-label|alt|placeholder)="([^"]*)"`)
	templateLetter     = regexp.MustCompile(`\p{L}`)
)

// templateFixtures is every run of English the three page templates are
// allowed to write between their tags: the sample screens' own record
// data, the product's name, and the type specimen. Seventeen entries,
// and worth reading as documentation rather than only as an allowlist
// — with dictFixtures below, it is the inventory of English that stays
// English on a Japanese page, as far as these three templates go.
//
// Adding to it is a decision — this string is data, not the page
// speaking — and it should be a rare one. "Draft" came OUT of it when
// the status pill beside it was translated and left the row's meta
// line saying the same word in English. Everything the page says in
// its own voice goes through P instead.
var templateFixtures = map[string]bool{
	// The product's name, in the shell demos' brand slot.
	"rastrillo": true,
	// The type specimen beside each font-size token.
	"Ag": true,
	// The shell demos' sample screen: its nav, its section headings,
	// its column headers, its rows, and its count line.
	"Posts":                           true,
	"Comments":                        true,
	"Settings":                        true,
	"Recent":                          true,
	"Post":                            true,
	"Status":                          true,
	"Release notes, August":           true,
	"Published 2 August":              true,
	"Why we moved off the old runner": true,
	"Displaying":                      true,
	"of":                              true,
	// The modal demo's sample screen: the section it is settings for,
	// and the three tabs in the panel's rail.
	"Account":       true,
	"Profile":       true,
	"Billing":       true,
	"Notifications": true,
}

// dictFixtures is the literal English a dict argument is allowed to
// carry, on the same terms as templateFixtures and kept apart from it
// on purpose. The two positions are not interchangeable: "Draft" is
// legitimate prose in the row's meta line and would have been a leak
// as a status-pill Label, and one shared allowlist would have let the
// second through on the strength of the first. Both entries here are
// the name of a fictional screen.
var dictFixtures = map[string]bool{
	"Posts":    true, // the shell demos' sample screen
	"Settings": true, // the modal demo's
}

// dictMachineArgs are the argument names whose value is a machine's,
// never a reader's: a tone identifier, an icon slug, a URL, a form
// field's name, a CSS class, an element id. Their values are checked
// by nothing, which is the point — "positive" and "plus" are the
// strings the code wants, and translating either would break the
// component.
//
// A name allowlist rather than a value heuristic because the two are
// genuinely indistinguishable as strings: "plus" is an icon slug and
// "Draft" is a label, and only the argument they arrive under says
// which. Adding a name here is therefore a claim — that this argument
// can never carry something a person reads — so keep it to arguments
// whose partial actually uses them as identifiers.
var dictMachineArgs = map[string]bool{
	"Tone": true, "Icon": true, "ActionIcon": true,
	"Href": true, "ActionHref": true, "HomeHref": true, "BackHref": true,
	"Name": true, "ID": true, "Class": true, "Value": true,
}

// dictArguments returns the name/value pairs of every dict call in a
// template whose value is a bare literal string. A value that is a
// parenthesised expression — (P "…"), which is what prose looks like
// here — or a field reference is not a literal and is not returned.
//
// Hand-tokenised rather than regexped because a dict argument list is
// a sequence of three different shapes (quoted string, parenthesised
// expression, bare word) and their alternation is the whole signal:
// the name is always the odd token, the value always the even one, and
// a regex that just grabbed every quoted string in the action would
// report every argument NAME as English too.
func dictArguments(action string) [][2]string {
	i := strings.Index(action, "dict ")
	if i < 0 {
		return nil
	}
	rest := action[i+len("dict "):]

	type token struct {
		text    string
		literal bool
	}
	var tokens []token
	for p := 0; p < len(rest); {
		switch c := rest[p]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			p++
		case c == '"':
			q := p + 1
			for q < len(rest) && rest[q] != '"' {
				if rest[q] == '\\' {
					q++
				}
				q++
			}
			if q >= len(rest) {
				return nil // unterminated: not something to guess about
			}
			tokens = append(tokens, token{rest[p+1 : q], true})
			p = q + 1
		case c == '(':
			depth, q := 0, p
			for ; q < len(rest); q++ {
				if rest[q] == '(' {
					depth++
				} else if rest[q] == ')' {
					if depth--; depth == 0 {
						break
					}
				}
			}
			if q >= len(rest) {
				return nil
			}
			tokens = append(tokens, token{rest[p : q+1], false})
			p = q + 1
		default:
			q := p
			for q < len(rest) && rest[q] != ' ' && rest[q] != '\t' && rest[q] != '\n' && rest[q] != '\r' {
				q++
			}
			tokens = append(tokens, token{rest[p:q], false})
			p = q
		}
	}

	var out [][2]string
	for j := 0; j+1 < len(tokens); j += 2 {
		name, value := tokens[j], tokens[j+1]
		if !name.literal {
			break // not a dict argument list after all; stop guessing
		}
		if value.literal {
			out = append(out, [2]string{name.text, value.text})
		}
	}
	return out
}

// literalText pulls every run of visible English out of one template:
// the text between tags, and the values of the attributes a person
// reads or hears. Punctuation and digits are dropped — an em dash, a
// full stop and "1–2" are not English.
func literalText(src string) []string {
	s := templateBlock.ReplaceAllString(src, "\x00")
	s = templateAction.ReplaceAllString(s, "\x00")
	var out []string
	keep := func(chunk string) {
		if c := strings.TrimSpace(chunk); c != "" && templateLetter.MatchString(c) {
			out = append(out, strings.Join(strings.Fields(c), " "))
		}
	}
	for _, m := range templateSpokenAttr.FindAllStringSubmatch(s, -1) {
		keep(m[2])
	}
	for _, chunk := range strings.Split(templateTag.ReplaceAllString(s, "\x00"), "\x00") {
		keep(chunk)
	}
	return out
}

// Every word the page templates say out loud goes through P, or is
// named as fixture. This is the gate that catches a new English
// sentence written straight into a template — the one thing the prose
// table's own gates cannot see, because a string nobody registered is
// not a key.
func TestNoUnregisteredEnglishInThePageTemplates(t *testing.T) {
	for _, tt := range []struct{ name, src string }{
		{"indexTemplate", indexTemplate},
		{"modalTemplate", modalTemplate},
		{"shellTemplate", shellTemplate},
	} {
		found := literalText(tt.src)
		if len(found) == 0 {
			t.Errorf("%s: no literal text found at all — the extractor has stopped working, and this gate with it", tt.name)
		}
		for _, s := range found {
			if templateFixtures[s] {
				continue
			}
			t.Errorf("%s writes %q literally. If the page is saying it, wrap it in {{P …}} and translate it in prose.go; "+
				"if it is sample data, add it to templateFixtures and say what it is.", tt.name, s)
		}

		// The pass literalText cannot make: the action is nulled before
		// it runs, so a component's label handed over as a dict
		// argument reaches the page without passing anything.
		var pairs int
		for _, action := range templateAction.FindAllString(tt.src, -1) {
			for _, kv := range dictArguments(action) {
				pairs++
				name, value := kv[0], kv[1]
				if dictMachineArgs[name] || dictFixtures[value] || !templateLetter.MatchString(value) {
					continue
				}
				t.Errorf("%s passes %q as %s in a dict. If the page is saying it, wrap it in (P …) and translate it in prose.go; "+
					"if it is a machine's value, its argument name belongs in dictMachineArgs; "+
					"if it is sample data, add it to dictFixtures and say what it is.", tt.name, value, name)
			}
		}
		if pairs == 0 {
			t.Errorf("%s: no literal dict arguments found at all — the tokeniser has stopped working, and this pass with it", tt.name)
		}
	}
}
