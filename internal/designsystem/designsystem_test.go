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
// sentence and below which it is a word. Only the sentences are gated.
//
// The four keys under the floor — "On", "Full", "Failed", "Display" —
// all occur on the Japanese page as sample data rather than as the
// page's own voice: "Failed" and "Full" are status-pill labels a
// fixture chose, "Display" is inside the count line's "Displaying
// 1–20", and "On" is a substring of half the English in the escaped
// markup. Gating them would fail on fixtures, which is the opposite of
// what this gate is for. Twelve characters is the shortest key that is
// a phrase; below it, matching a bare word proves nothing either way.
const proseLeakFloor = 12

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

	for _, locale := range nonEnglish() {
		page := escapedSource(string(files[RootTheme()+"/"+locale+"/index.html"]))
		if page == "" {
			t.Errorf("no %s index page", locale)
			continue
		}
		for _, s := range proseSentinels {
			if strings.Contains(page, s) {
				t.Errorf("%s page still says %q", locale, s)
			}
		}
		for _, key := range keys {
			if len([]rune(key)) < proseLeakFloor {
				continue
			}
			if strings.Contains(page, key) {
				t.Errorf("%s page carries the English %q", locale, key)
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
			if !strings.HasPrefix(strings.TrimSpace(rest), `<main class="rst-page"`) {
				t.Errorf("%s: the gallery header is not the element immediately before main", name)
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
	if n := len(js); n > 8*1024 {
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
}
