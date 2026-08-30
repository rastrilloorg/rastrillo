package designsystem

import (
	"fmt"
	"html"
	"html/template"
	"path"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/carlosframework/rastrillo"
	"github.com/carlosframework/rastrillo/ui"
)

// mountPath is the mount every gate in this package renders at: the one
// rastrillo.org publishes. Render takes it as an argument now, so a gate
// that spelled a different one would be testing a tree nobody serves.
const mountPath = DefaultMount

// maxPageBytes is what one page of this gallery may weigh.
//
// It replaces a 20 MB ceiling on the whole tree. That number was a proxy
// for a different question — whether a machine-generated artifact
// belonged in the repository at all — and now that the tree is generated
// at the website's build instead of committed, the question it stood in
// for has been answered and the ceiling has nothing left to hold. What
// is left is the thing a reader actually experiences, which is one page
// at a time.
//
// 128 KiB is a little under twice the heaviest page that is not
// components.html (primitives.html at 67,507 bytes, in the widest
// locale), so a new section has room to land without an argument, and it
// is small enough that the page is on screen rather than arriving.
//
// components.html DOES NOT PASS IT. It is 332,827 bytes in day/en and
// 387,029 in signal/hi — three times the budget, and 5.7 times the next
// heaviest page. It is the page the ruling was about. It is recorded in
// pageBudgetDebt below rather than fixed here, because fixing it is a
// change to what the page contains and this commit is a change to what
// is gated.
//
// ── Where the weight is, because the next page will be asked ─────────
//
// Each sample is written into the page twice: as the escaped document
// its preview frame carries, and as the escaped source its Code tab
// shows. Attribute-escaping a run of markup costs about 40% on top,
// because every quote in it becomes six characters. Add ~360 bytes of
// document preamble and ~450 of widget markup per example, and
// components.html carries most of the 110 examples in the gallery.
//
// ── The two dsCSS numbers, because there are two ─────────────────────
//
// len(dsCSS) is 11,380 bytes and what reaches a page is 7,765. Neither
// is a typo for the other: html/template's CSS sanitiser strips the
// comments out of <style> text, and better than a third of that constant
// is the comments explaining the preview widget's scaling and the
// scriptless story. Measure the constant and you get the first number;
// measure a rendered page and you get the second. The one a reader waits
// for is 7,765, on every page, inline — which is the lever to reach for
// before this ceiling if a page ever sits just over it.
const maxPageBytes = 128 << 10

// pageBudgetDebt names the pages over maxPageBytes, with the ceiling
// each is held to until it is fixed. Same convention as a11y_test.go's
// axeExempt and ui/contrast_test.go's colorMixSkip: a gate that is not
// enforcing something says so in a named table instead of quietly not
// enforcing it.
//
// Keyed by file name, so it applies to that page in every theme and
// locale — the weight is the page's content, and the widest locale is
// the honest number to hold.
//
// An entry that is no longer needed fails the gate, so this table
// shrinks on its own once the page it names is fixed. Empty is the goal.
var pageBudgetDebt = map[string]int{
	// 387,029 bytes at its worst (signal/hi). The fix is not a bigger
	// number: it is the Code tabs, which write every sample into the
	// page a second time for a tab most readers never open.
	"components.html": 400 << 10,
}

// render is Render() with the error already fatal — every test here
// starts the same way.
func render(t *testing.T) map[string][]byte {
	t.Helper()
	files, err := Render(mountPath)
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

// galleryFiles is the five section pages of one theme × locale
// directory, in page order. Read off pageKinds() rather than spelled
// out: a sixth page kind is covered by every gate that walks this the
// day its row lands.
func galleryFiles(theme, locale string) []string {
	out := make([]string, 0, len(pageKinds()))
	for _, pk := range pageKinds() {
		out = append(out, theme+"/"+locale+"/"+pk.File)
	}
	return out
}

// galleryPage is one page of one directory, by kind, with a fatal if
// the renderer did not produce it.
func galleryPage(t *testing.T, files map[string][]byte, theme, locale, kind string) string {
	t.Helper()
	name := theme + "/" + locale + "/" + fileOf(kind)
	page := string(files[name])
	if page == "" {
		t.Fatalf("no %s in the rendered tree", name)
	}
	return page
}

// markerCounts counts each NAME a marker comment carries, over a set of
// pages: the union the two coverage gates below assert.
//
// The union is the whole point since the split. Before it, every
// partial and every idiom was on one page, and "is the marker on this
// page" was the same question as "is this partial documented at all".
// It is not any more: the partials are on components.html and the
// idioms on primitives.html, and a per-page gate would pass on a page
// that had never had them — a partial dropped from its section would
// satisfy the tokens page, the shells page and the overview, and the
// build would stay green with the component missing from the tree.
//
// So the gates count over the whole directory, and they insist on
// exactly one page rather than at least one: zero is a component that
// vanished, and two is a section rendered twice, which is how a page
// kind added by copying its neighbour goes wrong.
func markerCounts(files map[string][]byte, pages []string, re *regexp.Regexp) map[string]int {
	seen := map[string]int{}
	for _, name := range pages {
		for _, m := range re.FindAllStringSubmatch(string(files[name]), -1) {
			seen[m[1]]++
		}
	}
	return seen
}

// Every partial ui ships is rendered somewhere in each gallery, marked
// by a comment the renderer emits per partial section. The marker is
// the gate surface deliberately: greping rendered HTML for a class or a
// string would break every time a partial's markup was tidied, and this
// gate is about coverage, not markup.
func TestEveryPartialAppearsAcrossThePages(t *testing.T) {
	files := render(t)
	want := definedPartials(t)
	for _, theme := range ui.ThemeNames() {
		for _, locale := range rastrillo.BaseLocales() {
			where := theme + "/" + locale
			found := markerCounts(files, galleryFiles(theme, locale), partialMarker)
			for _, name := range want {
				switch found[name] {
				case 1:
				case 0:
					t.Errorf("%s: partial %q is on none of the %d pages (no marker comment anywhere in the directory)", where, name, len(pageKinds()))
				default:
					t.Errorf("%s: partial %q is marked on %d pages; a section is being rendered twice", where, name, found[name])
				}
			}
			for name := range found {
				if !slices.Contains(want, name) {
					t.Errorf("%s: a page marks partial %q, which ui does not define", where, name)
				}
			}
		}
	}
}

// Every class idiom ui.Styleguide ships is rendered somewhere in each
// gallery, same marker mechanism and the same union.
func TestEveryStyleguideSampleAppearsAcrossThePages(t *testing.T) {
	files := render(t)
	want := make([]string, 0, len(ui.Styleguide()))
	for name := range ui.Styleguide() {
		want = append(want, name)
	}
	sort.Strings(want)
	for _, theme := range ui.ThemeNames() {
		for _, locale := range rastrillo.BaseLocales() {
			where := theme + "/" + locale
			found := markerCounts(files, galleryFiles(theme, locale), idiomMarker)
			for _, name := range want {
				switch found[name] {
				case 1:
				case 0:
					t.Errorf("%s: class idiom %q is on none of the %d pages (no marker comment anywhere in the directory)", where, name, len(pageKinds()))
				default:
					t.Errorf("%s: class idiom %q is marked on %d pages; a section is being rendered twice", where, name, found[name])
				}
			}
			for name := range found {
				if !slices.Contains(want, name) {
					t.Errorf("%s: a page marks class idiom %q, which ui.Styleguide does not ship", where, name)
				}
			}
		}
	}
}

// A page's weight is what a reader waits for, so it is gated per page
// rather than in total. The tree's total is logged because it is worth
// knowing and worth nothing as a ceiling: it is the website's build
// output now, not the repository's contents.
func TestEveryPageStaysUnderItsBudget(t *testing.T) {
	files := render(t)
	var total, heaviest int
	var heaviestName string
	used := map[string]bool{}
	for name, body := range files {
		total += len(body)
		if !strings.HasSuffix(name, ".html") {
			continue
		}
		if len(body) > heaviest {
			heaviest, heaviestName = len(body), name
		}
		limit, what := maxPageBytes, "budget"
		if debt, ok := pageBudgetDebt[path.Base(name)]; ok {
			limit, what = debt, "recorded debt"
			used[path.Base(name)] = true
		}
		if len(body) > limit {
			t.Errorf("%s is %d bytes, over its %d-byte %s", name, len(body), limit, what)
		}
	}
	for name := range pageBudgetDebt {
		if !used[name] {
			t.Errorf("pageBudgetDebt names %q, and no page of that name is over %d bytes: delete the entry", name, maxPageBytes)
		}
	}
	t.Logf("heaviest page: %s at %d bytes; whole tree: %d files, %d bytes (%.2f MiB)",
		heaviestName, heaviest, len(files), total, float64(total)/(1<<20))
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

// srcdocAttr finds the documents the previews carry. Every example on
// an index page is framed rather than rendered inline, and the frame's
// whole document lives in a srcdoc attribute — HTML-escaped, which is
// what keeps it invisible to every other pattern in this file. The
// gates below unescape it and hold it to the same rules a file in the
// tree is held to, because it is a page: a reader looks at it, follows
// links in it, and reads it in the language they chose.
//
// The value cannot contain a bare " (html/template writes it as &#34;),
// so the attribute really does end at the first quote.
var srcdocAttr = regexp.MustCompile(`srcdoc="([^"]*)"`)

// srcdocs returns one page's preview documents, unescaped.
func srcdocs(page string) []string {
	var out []string
	for _, m := range srcdocAttr.FindAllStringSubmatch(page, -1) {
		out = append(out, html.UnescapeString(m[1]))
	}
	return out
}

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
	var framed int
	for _, name := range names {
		body := string(files[name])
		locale := localeOfPath(name)
		wholeDocument(t, files, name, locale, body)
		// The same rules over the documents the previews carry. A
		// srcdoc is a page too — a reader looks at it, reads it in the
		// language they chose and clicks the links in it — and it is
		// escaped, so nothing else in this file can see inside one.
		for i, doc := range srcdocs(body) {
			framed++
			wholeDocument(t, files, fmt.Sprintf("%s srcdoc %d", name, i), locale, doc)
		}
	}
	// Asserted rather than assumed: a srcdoc that stopped being emitted,
	// or an extractor that stopped matching, would leave the loop above
	// checking the outer pages only and passing.
	if framed == 0 {
		t.Error("no preview documents found at all — the srcdoc extractor has stopped working, and this gate with it")
	}
	t.Logf("%d pages, %d preview documents", len(names), framed)
}

// wholeDocument holds one document — a file in the tree, or the
// document a preview frame carries — to the whole contract: it is a
// single complete page, in the language its path says, with no
// unresolved catalog key, and every URL in it either goes nowhere on
// purpose or names a file this tree contains.
//
// The absolute half of that is the fix for the live bug: rastrillo.org
// served /design-system unstyled, because the edge returns the
// directory index at the slash-less URL with no redirect and every
// relative href on it then resolved one directory too high. The
// resolves half is coverage the relative scheme never had — it catches
// a switcher pointing at a locale the tree does not ship, or a shell
// demo link naming a file that moved.
func wholeDocument(t *testing.T, files map[string][]byte, name, locale, body string) {
	t.Helper()
	if !strings.HasPrefix(body, "<!doctype html>") {
		t.Errorf("%s: does not start with a doctype", name)
		return
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
		return
	}
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
	// Every link a reader can actually click, anywhere in this tree,
	// is one of two things: a page of this tree, or "#" — which is
	// where the sample routes go now. Nothing lands on a 404. The
	// samples keep their real routes in the Code tab beside them,
	// where they are text to copy rather than a link to follow; see
	// deaden.
	for _, m := range anchorHref.FindAllStringSubmatch(body, -1) {
		href := m[1]
		if strings.HasPrefix(href, "#") {
			continue
		}
		if !strings.HasPrefix(href, mountPrefix) {
			t.Errorf("%s: link %q goes outside this tree; a sample link is rewritten to \"#\" so following it cannot 404", name, href)
			continue
		}
		if !resolves(files, href) {
			t.Errorf("%s: link %q names nothing in the tree", name, href)
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

// No gallery page may render a live modal. `.rst-modal-overlay` is
// `position: fixed; inset: 0; z-index: 10` and
// `body:has(.rst-backdrop) { overflow: hidden }`, so the modal sample
// rendered inline did not sit in the gallery's flow at all: every index
// page loaded with a full-viewport modal over it, the content behind it
// unscrollable, and its Close link — the sample's own `/settings` —
// a 404. That was the live bug on rastrillo.org/design-system. The rule
// is the whole gallery's, not the overview's: five pages share one
// stylesheet and any of them could grow a sample that laid an overlay
// over it.
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
func TestNoGalleryPageOpensAModalOverTheGallery(t *testing.T) {
	files := render(t)
	for _, theme := range ui.ThemeNames() {
		for _, locale := range rastrillo.BaseLocales() {
			for _, name := range galleryFiles(theme, locale) {
				body := string(files[name])
				if body == "" {
					t.Errorf("%s is missing", name)
					continue
				}
				for _, live := range []string{`class="rst-modal-overlay"`, `class="rst-backdrop"`} {
					if strings.Contains(body, live) {
						t.Errorf("%s renders a live %s — the overlay is fixed to the viewport and covers the page", name, live)
					}
				}
			}
			// The second half, on the one page that owes it. The modal
			// idiom lives under UI primitives since the split, so that
			// is where the escaped source and the demo link have to be
			// — and naming the page rather than sweeping for it is what
			// makes this fail if the idiom is dropped altogether.
			page := galleryPage(t, files, theme, locale, "primitives")
			if !strings.Contains(page, `class=&#34;rst-modal-overlay&#34;`) {
				t.Errorf("%s/%s/%s does not show the modal sample as escaped source", theme, locale, fileOf("primitives"))
			}
			if href := modalHref(mountPath, theme, locale); !strings.Contains(page, `href="`+href+`"`) {
				t.Errorf("%s/%s/%s does not link its modal demo (%s)", theme, locale, fileOf("primitives"), href)
			}
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

// Every theme × locale × page kind × shell combination is present, plus
// the root index and the seven shared assets — the tree's shape is part
// of its contract with the website's sync script. The page kinds come
// off pageKinds(), so a sixth page is expected in every directory the
// moment its row lands and nothing here has to be remembered.
func TestTreeShapeIsComplete(t *testing.T) {
	files := render(t)
	want := []string{
		"index.html",
		"tokens.css", "rastrillo.js", "select.js", "datetime.js", "gallery.js",
	}
	for _, theme := range ui.ThemeNames() {
		want = append(want, "theme-"+theme+".css")
		for _, locale := range rastrillo.BaseLocales() {
			want = append(want, galleryFiles(theme, locale)...)
			want = append(want, fmt.Sprintf("%s/%s/modal.html", theme, locale))
			want = append(want, fmt.Sprintf("%s/%s/demo.html", theme, locale))
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
			for _, path := range galleryFiles(theme, locale) {
				if want := `href="` + mountPrefix + `theme-` + theme + `.css"`; !strings.Contains(string(files[path]), want) {
					t.Errorf("%s does not link its own theme (%s)", path, want)
				}
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
	page := galleryPage(t, render(t), RootTheme(), "en", "components")
	// Read out of the preview documents, which is where every sample
	// on this page now lives. Booting is the point of the assertion, so
	// it is not enough that the attribute is somewhere in the file: the
	// document carrying it has to be the one that loads the script that
	// looks for it.
	frames := srcdocs(page)
	if len(frames) == 0 {
		t.Fatal("no preview documents on the page at all")
	}
	for _, c := range []struct{ hook, script string }{
		{"data-rst-select", "select.js"},
		{"data-rst-date", "datetime.js"},
		{"data-rst-time", "datetime.js"},
		{"data-rst-range", "datetime.js"},
	} {
		var found bool
		for _, doc := range frames {
			if !strings.Contains(doc, c.hook) {
				continue
			}
			found = true
			if !strings.Contains(doc, mountPrefix+c.script) {
				t.Errorf("a preview carries %s and does not load %s — the enhancement has nothing to boot from", c.hook, c.script)
			}
		}
		if !found {
			t.Errorf("no %s on the page — the enhancement has nothing to boot on", c.hook)
		}
	}
	var optgroup bool
	for _, doc := range frames {
		optgroup = optgroup || strings.Contains(doc, "<optgroup")
	}
	if !optgroup {
		t.Error("no hand-written optgroup'd select on the page")
	}
}

// ── The preview widget ───────────────────────────────────────────────

// widgetsOf cuts a page into its preview widgets: everything from one
// `<div class="ds-view"` up to the next one (or to the end).
func widgetsOf(page string) []string {
	parts := strings.Split(page, `<div class="ds-view" style=`)
	if len(parts) < 2 {
		return nil
	}
	return parts[1:]
}

// Every example is shown three ways behind one control, and the control
// is the browser's own: three radios sharing a name, Desktop checked,
// and :has() switching the panels. Nothing here runs.
//
// The gate is worth more than it looks. A widget with two checked
// radios, or with a name shared across two examples, renders perfectly
// and behaves wrongly — picking Mobile in one example would silently
// deselect the tab in another — and neither shows up in a screenshot.
func TestEveryExampleIsFramedDesktopMobileAndCode(t *testing.T) {
	files := render(t)
	var total int
	for _, name := range galleryFiles(RootTheme(), "en") {
		page := string(files[name])
		widgets := widgetsOf(page)
		// One widget per sample state, and one per shell section — the
		// only example on the tree whose frame is a page of the tree
		// rather than a document written for it. Counted off the page's
		// own markup rather than off a per-page-kind table, so a page
		// that grows examples is covered without an entry here.
		// The examples whose frame is a page of this tree rather than a
		// document written for it: the three shell demos, and the demo
		// application on the Overview. Neither kind offers a Code tab —
		// a shell's source is a Go template, and an application is not
		// a snippet to paste.
		framedPages := strings.Count(page, `<section class="ds-shell"`) + strings.Count(page, `<section class="ds-demo"`)
		if n := strings.Count(page, `<div class="ds-sample">`) + framedPages; n != len(widgets) {
			t.Errorf("%s: %d preview widgets for %d examples", name, len(widgets), n)
		}
		total += len(widgets)
		groups := map[string]bool{}
		var withCode int
		for i, w := range widgets {
			radios := regexp.MustCompile(`<input type="radio" name="([^"]*)"( checked)?>`).FindAllStringSubmatch(w, -1)
			if len(radios) < 2 || len(radios) > 3 {
				t.Errorf("%s widget %d has %d tabs, want 2 (a framed page) or 3 (with its source)", name, i, len(radios))
				continue
			}
			var checked int
			for _, r := range radios {
				if r[1] != radios[0][1] {
					t.Errorf("%s widget %d: tabs in one widget carry two names (%q, %q), so they are not one group", name, i, radios[0][1], r[1])
				}
				if r[2] != "" {
					checked++
				}
			}
			if checked != 1 || radios[0][2] == "" {
				t.Errorf("%s widget %d: %d tabs start checked and the first is %q; Desktop is the view a page with no interaction has to open on", name, i, checked, radios[0][2])
			}
			if groups[radios[0][1]] {
				t.Errorf("%s widget %d: the radio name %q is already used by another widget on this page; choosing a tab in one would clear the other", name, i, radios[0][1])
			}
			groups[radios[0][1]] = true
			if n := strings.Count(w, "<iframe"); n != 1 {
				t.Errorf("%s widget %d frames %d documents, want 1", name, i, n)
			}
			if strings.Contains(w, "ds-view__tab--c") {
				withCode++
				if !strings.Contains(w, `<pre class="ds-src ds-view__code`) {
					t.Errorf("%s widget %d offers a Code tab with no source behind it", name, i)
				}
			}
		}
		if want := len(widgets) - framedPages; withCode != want {
			t.Errorf("%s: %d widgets show source, want %d (all but the framed pages)", name, withCode, want)
		}

		// The mechanism, asserted where it lives — on every page, because
		// every page carries the whole stylesheet and any of them can
		// frame an example. Without these four rules the tabs are three
		// radios that change nothing.
		for _, rule := range []string{
			`.ds-view:has(.ds-view__tab--m input:checked) .ds-view__box`,
			`.ds-view:has(.ds-view__tab--c input:checked) .ds-view__stage { display: none; }`,
			`.ds-view:has(.ds-view__tab--c input:checked) .ds-view__code { display: block; }`,
			`.ds-view__box { --ds-k: min(1, tan(atan2(100cqw, var(--ds-w)))); }`,
		} {
			if !strings.Contains(page, rule) {
				t.Errorf("%s carries no rule %q — the tabs would switch nothing", name, rule)
			}
		}
	}
	// Asserted rather than assumed: a split that quietly stopped
	// rendering a section would leave the per-page arithmetic above
	// agreeing with itself on an empty page.
	if total < 100 {
		t.Errorf("the whole gallery frames %d examples; it has well over a hundred to frame", total)
	}
	t.Logf("%d preview widgets across %d pages", total, len(galleryFiles(RootTheme(), "en")))

	// And nothing scripted, anywhere in the tree: an inline handler
	// here would be a widget that stops working with scripts off.
	for name, body := range files {
		if !strings.HasSuffix(name, ".html") {
			continue
		}
		for _, on := range []string{" onclick=", " onchange=", " oninput=", " onload=", " onsubmit="} {
			if strings.Contains(string(body), on) {
				t.Errorf("%s carries an inline%shandler", name, on)
			}
		}
	}
}

// A sample's links go nowhere and its forms go into a sink, so nothing
// a reader clicks in a preview can navigate the frame away from the
// example they were looking at — and rastrillo.js's busy rule skips a
// form whose target is not _self, so nothing spins on its way nowhere
// either. The source beside it keeps the real routes.
func TestSampleLinksAndFormsAreDeadInThePreviews(t *testing.T) {
	files := render(t)
	// Every preview document in the directory, not one page's: the
	// samples are spread over components.html, primitives.html and
	// shells.html since the split, and a sweep over one of them would
	// leave the other two unchecked.
	var docs []string
	for _, name := range galleryFiles(RootTheme(), "en") {
		docs = append(docs, srcdocs(string(files[name]))...)
	}
	if len(docs) == 0 {
		t.Fatal("no preview documents anywhere in the gallery")
	}
	// The link half. TestEveryPageIsAWholeLocalisedDocument holds the
	// whole tree to this, srcdocs included, and would fail first — but
	// a gate named for links that did not look at one is a gate that
	// can be quietly narrowed to nothing, so it looks.
	var links int
	for i, doc := range docs {
		for _, m := range anchorHref.FindAllStringSubmatch(doc, -1) {
			links++
			// "#" is what a dead route becomes; a fragment that names
			// something — the shell samples' own skip link, #main —
			// is a link inside the sample and stays one.
			if href := m[1]; !strings.HasPrefix(href, "#") && !strings.HasPrefix(href, mountPrefix) {
				t.Errorf("preview %d still links %q; a sample route is rewritten to \"#\" so following it cannot 404", i, href)
			}
		}
	}
	if links == 0 {
		t.Fatal("no links in any preview document — the samples have no link in them at all, and this half is checking nothing")
	}

	var forms, sinks int
	for i, doc := range docs {
		n := strings.Count(doc, "<form")
		if n == 0 {
			if strings.Contains(doc, `name="ds-void"`) {
				t.Errorf("preview %d carries a sink frame and no form to aim at it", i)
			}
			continue
		}
		forms += n
		if got := strings.Count(doc, `<form target="ds-void"`); got != n {
			t.Errorf("preview %d: %d of %d forms are aimed at the sink; the rest would navigate the frame away", i, got, n)
		}
		if strings.Count(doc, `<iframe name="ds-void" hidden>`) != 1 {
			t.Errorf("preview %d aims its forms at a sink that is not in the document", i)
			continue
		}
		sinks++
	}
	if forms == 0 || sinks == 0 {
		t.Fatalf("%d forms in %d preview documents with a sink — the sample set has no form in it at all, and this gate is checking nothing", forms, sinks)
	}
	// The other half: the Code tab is NOT deadened. A gallery that had
	// quietly rewritten the routes a reader copies would be teaching
	// the wrong markup.
	if !strings.Contains(galleryPage(t, files, RootTheme(), "en", "components"), `href=&#34;/posts/1/edit&#34;`) {
		t.Error("no sample source on the components page keeps a real route — the Code tab has been deadened with the preview")
	}
}

// Every link the renderer owns that leaves this page opens in a new
// tab: the demo pages in the rail, the two shell chrome idioms' links,
// the modal's, and the button under each shell demo. A reader is a
// long way down a long page with a filter they typed into the rail;
// losing that to look at a demo is a poor trade.
//
// The switchers are deliberately NOT in this set. Choosing a theme or
// a language is not a detour, it is the same page again, and it
// belongs in the tab you are reading.
func TestEveryDemoLinkOpensInANewTab(t *testing.T) {
	files := render(t)
	away := regexp.MustCompile(`<a[^>]*\shref="([^"]*)"[^>]*>`)
	for _, theme := range ui.ThemeNames() {
		for _, locale := range rastrillo.BaseLocales() {
			demos := map[string]bool{modalHref(mountPath, theme, locale): true, demoHref(mountPath, theme, locale): true}
			for _, shell := range ui.LayoutNames() {
				demos[shellHref(mountPath, theme, locale, shell)] = true
			}
			seen := map[string]int{}
			for _, name := range galleryFiles(theme, locale) {
				for _, m := range away.FindAllStringSubmatch(string(files[name]), -1) {
					if !demos[m[1]] {
						continue
					}
					seen[m[1]]++
					if !strings.Contains(m[0], `target="_blank"`) || !strings.Contains(m[0], `rel="noopener"`) {
						t.Errorf("%s: %s does not open in a new tab", name, m[0])
					}
				}
			}
			// The rail carries one link to each of them on all five
			// pages, so nothing in the directory can link a demo
			// without opening it in a tab, and nothing can drop one.
			for href := range demos {
				if want := len(pageKinds()); seen[href] < want {
					t.Errorf("%s/%s: %s is linked %d times, want at least %d (the rail, on every page)", theme, locale, href, seen[href], want)
				}
			}
		}
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
	if _, err := Render(mountPath); err != nil {
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
// for; today that is 156 of the 207 keys, and the other 51 — "Theme",
// "Tokens", "Sections", "Shells", "Required", "Type scale", "Pick one",
// every switcher label and every one-word state — are not swept for at
// all. Those three numbers are len([]rune(key)) >= proseLeakFloor over
// proseKeysRendered, which is the same arithmetic the sweep below does:
// count them again rather than believe this sentence, because they move
// every time the page gains or loses a line of prose.
//
// That is a smaller hole than it sounds, because a short label is the
// PARITY gate's job, not this one's. TestEveryProseKeyIsTranslated
// walks every key in all twelve locales, so a switcher label with no
// Japanese translation fails the build there. What the floor gives up
// is only the second, belt-and-braces proof that the translation
// reached the page — for the strings where that proof cannot be had.
//
// It cannot be had because a bare English word occurs on a translated
// page for reasons that are nothing to do with prose. Of the 51 short
// keys, eleven actually collide today — "Display", "Published", "On",
// "Draft", "Failed", "Full", "Filter", "Form", "Text", "Tokens",
// "Demos" — and they are the justification for the floor rather than a
// list to maintain. "Failed" and "Full" are status-pill labels a
// fixture chose, "Display" is inside the count line's "Displaying
// 1–20", and "On" is a substring of half the English in the page's
// sample data. Gating those would fail on fixtures, which is the
// opposite of what this gate is for. Twelve characters is the shortest
// key that is a phrase; below it, matching a bare word proves nothing
// either way. Count the collisions again too: set the floor to 0 and
// the sweep names them.
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
	// components page — so there an English "Write a post" is the
	// fixture doing its job, and on a shell demo it would be a leak.
	"Write a post": "components.html",
	// The modal idiom's sample and this package's own modal demo say
	// the same two sentences, because the demo was written FROM the
	// sample. The sample is a fixture — English markup a reader copies
	// — and it used to be visible only as escaped source, which this
	// sweep skips. It is now also rendered, inside its preview frame,
	// so the English reaches the primitives page's bytes, which is
	// where the modal idiom lives. On modal.html the same words are the
	// page speaking and must be translated, and the sweep still says so
	// there.
	"Close settings": "primitives.html",
	"Update the name and photo shown across the account.": "primitives.html",
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
	"An overview of everything the design system provides.",
	"Links here are inactive",
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
	// All five English pages of the root theme, joined: the three
	// sentinels are on three different pages since the split — the
	// opening sentence on every one of them, the dead-link callout on
	// the pages that frame samples, and the list-grid rule under UI
	// primitives — and the point of a sentinel is that it is somewhere
	// in English, not that it is on one named file.
	var en string
	for _, name := range galleryFiles(RootTheme(), "en") {
		if files[name] == nil {
			t.Fatalf("no %s in the tree", name)
		}
		en += string(files[name])
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
	// is the failure mode it was once extended to fix. Per theme, per
	// non-English locale: one page per kind, a modal demo, the demo
	// application and one page per shell.
	if want := len(ui.ThemeNames()) * (len(rastrillo.BaseLocales()) - 1) * (len(pageKinds()) + 2 + len(ui.LayoutNames())); len(names) != want {
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
//
// Walked over every page kind, not just the Overview. That is not
// thoroughness for its own sake: the switchers point at THIS page in
// another theme or another language, and on index.html a switcher that
// had forgotten which page it was on would emit the same URL as one
// that had not. Reading only the Overview is reading the one page where
// the bug is invisible.
func TestTheChromeCarriesTheThreeSwitchers(t *testing.T) {
	files := render(t)
	for _, theme := range ui.ThemeNames() {
		for _, locale := range rastrillo.BaseLocales() {
			for _, pk := range pageKinds() {
				name := theme + "/" + locale + "/" + pk.File
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
				// Both switchers keep the reader on the page they are
				// reading: choosing another theme from components.html
				// lands on that theme's components.html, not back at the
				// Overview. Asserted per page kind, because index.html is
				// the one page where doing this right and doing it wrong
				// produce the same URL — every switcher href there ends in
				// index.html either way, so a gate that only read the
				// Overview could not see the difference at all.
				for _, m := range anchorHref.FindAllStringSubmatch(chrome, -1) {
					if !strings.HasSuffix(m[1], "/"+pk.File) {
						t.Errorf("%s: the chrome links %q, which is not this page in another theme or language", name, m[1])
					}
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
	page := galleryPage(t, render(t), RootTheme(), "en", "overview")
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
	// 8 KiB was the budget when this file did one thing, and 10 KiB
	// when it did two — the toggle and the sidebar filter. It stays
	// 10 KiB, and the reason is worth a line, because the third entry
	// in the header nearly moved it.
	//
	// Every example is now drawn in an iframe of its own, and a colour
	// scheme is not propagated into an embedded document that declares
	// one — so a reader who chose Dark got a dark gallery full of
	// light previews, and the toggle had to reach the frames. That is
	// not a third feature; it is the first one following the page. It
	// cost 1,273 bytes, about 300 of them code (two short functions, a
	// load listener, one call in the click handler) and the rest the
	// paragraph saying why an iframe needs telling.
	//
	// It fit, with 32 bytes to spare. That is uncomfortably close, and
	// it is the honest number: the next thing here needs the ceiling
	// raised, and raising it should buy comments rather than code.
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
	page := galleryPage(t, render(t), RootTheme(), "en", "overview")
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
	// Every link in the rail. They are all absolute paths under the
	// mount since the split — a page of this directory with a fragment
	// on the end, or a demo page with none — and
	// TestEveryPageIsAWholeLocalisedDocument holds them all to files
	// the tree contains.
	navHref       = regexp.MustCompile(`<a[^>]*\shref="([^"]*)"`)
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
			for _, pk := range pageKinds() {
				name := theme + "/" + locale + "/" + pk.File
				page := string(files[name])
				rail := railOf(t, name, page)
				if rail == "" {
					continue
				}

				var anchors []string
				for _, m := range anchorMarker.FindAllStringSubmatch(page, -1) {
					anchors = append(anchors, m[1])
				}
				// The rail's entries for THIS page. Every entry in it is
				// an absolute page address with a fragment on the end —
				// the current page's included — so "the entries for this
				// page" is a prefix match, and everything else in the
				// rail belongs to one of the other four.
				prefix := mountPrefix + theme + "/" + locale + "/" + pk.File + "#"
				var fragments []string
				for _, m := range navHref.FindAllStringSubmatch(rail, -1) {
					if strings.HasPrefix(m[1], prefix) {
						fragments = append(fragments, strings.TrimPrefix(m[1], prefix))
					}
				}
				if len(fragments) != len(anchors) {
					t.Errorf("%s: the sidebar links %d fragments of this page, the page anchors %d", name, len(fragments), len(anchors))
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
					for _, m := range kind.re.FindAllStringSubmatch(page, -1) {
						if want := anchorID(kind.prefix, m[1]); !linked[want] {
							t.Errorf("%s: %s %q is on this page and not in the sidebar (no %s%s)", name, kind.prefix, m[1], prefix, want)
						}
					}
				}

				// Everything the rail links is a page of this tree — the
				// other four galleries, the demos, and this page itself.
				// Exactly one link per shell demo plus the modal leaves
				// the gallery altogether; they are the only entries in
				// the rail that do.
				gallery := map[string]bool{}
				for _, p := range galleryFiles(theme, locale) {
					gallery[mountPrefix+p] = true
				}
				var away int
				for _, m := range navHref.FindAllStringSubmatch(rail, -1) {
					if !strings.HasPrefix(m[1], mountPrefix) || !resolves(files, m[1]) {
						t.Errorf("%s: the sidebar links %q, which is not a page of this tree", name, m[1])
						continue
					}
					target, _, _ := strings.Cut(m[1], "#")
					if !gallery[target] {
						away++
					}
				}
				if want := len(ui.LayoutNames()) + 2; away != want {
					t.Errorf("%s: the sidebar has %d links out of the gallery, want %d (one per shell demo, plus the modal and the demo application)", name, away, want)
				}
			}
			// The union, which is the half a per-page reading cannot
			// see: every marker in the whole directory has a rail entry
			// somewhere, so a partial that landed on no page at all
			// fails here as well as in the coverage gates.
			rail := railOf(t, theme+"/"+locale, string(files[theme+"/"+locale+"/"+fileOf("overview")]))
			for _, kind := range []struct {
				prefix string
				re     *regexp.Regexp
			}{{"partial", partialMarker}, {"idiom", idiomMarker}} {
				found := markerCounts(files, galleryFiles(theme, locale), kind.re)
				if len(found) == 0 {
					t.Errorf("%s/%s: no %s markers anywhere in the directory", theme, locale, kind.prefix)
				}
				for marked := range found {
					if want := "#" + anchorID(kind.prefix, marked); !strings.Contains(rail, want) {
						t.Errorf("%s/%s: %s %q is somewhere in the directory and nowhere in the rail (no %s)", theme, locale, kind.prefix, marked, want)
					}
				}
			}
		}
	}
}

// The rail is the same on all five pages of a directory, byte for byte
// apart from which section carries `open aria-current="page"`.
//
// That is the promise the split makes to a reader: the whole vocabulary
// is one list, and moving between the pages does not move the list
// under them. It is easy to break by accident — a nav built per page
// out of that page's own data would come out shorter on four of the
// five, and every other gate here would still pass — so it is asserted
// literally rather than left to be noticed.
func TestTheRailIsTheSameOnEveryPage(t *testing.T) {
	files := render(t)
	current := strings.NewReplacer(` open aria-current="page"`, "", ` aria-current="page"`, "")
	for _, theme := range ui.ThemeNames() {
		for _, locale := range rastrillo.BaseLocales() {
			var first, firstName string
			for _, name := range galleryFiles(theme, locale) {
				rail := current.Replace(railOf(t, name, string(files[name])))
				if rail == "" {
					continue
				}
				if first == "" {
					first, firstName = rail, name
					continue
				}
				if rail != first {
					t.Errorf("%s and %s carry different rails", firstName, name)
				}
			}
			if first == "" {
				t.Errorf("%s/%s: no rail on any page", theme, locale)
			}
		}
	}
}

// Exactly one section of the rail is the page the reader is on, and it
// is the right one. aria-current sits on the section rather than on a
// link because the section IS the page: its entries are fragments of
// it, and there is no single link in the rail that means "this page".
func TestTheRailSaysWhichPageYouAreOn(t *testing.T) {
	files := render(t)
	for _, theme := range ui.ThemeNames() {
		for _, locale := range rastrillo.BaseLocales() {
			for _, pk := range pageKinds() {
				name := theme + "/" + locale + "/" + pk.File
				rail := railOf(t, name, string(files[name]))
				if n := strings.Count(rail, `aria-current="page"`); n != 1 {
					t.Errorf("%s: %d sections of the rail are marked current, want 1", name, n)
					continue
				}
				title := template.HTMLEscapeString(proseIn(locale, pk.Title))
				// The current section is either a disclosure holding
				// this page's entries, or — for a page with nothing
				// anchored on it yet — the plain link that stands in
				// for one. Either way it says this page's own name.
				open := regexp.MustCompile(`(?s)<details open aria-current="page"><summary><span class="rst-caret" aria-hidden="true"><svg.*?</svg></span>` + regexp.QuoteMeta(title) + `</summary>`)
				link := `<a class="ds-nav__page" href="` + mountPrefix + theme + "/" + locale + "/" + pk.File + `" aria-current="page">` + title + `</a>`
				if !strings.Contains(rail, link) && !open.MatchString(rail) {
					t.Errorf("%s: the current section of the rail is not %q", name, proseIn(locale, pk.Title))
				}
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
			for _, pk := range pageKinds() {
				name := theme + "/" + locale + "/" + pk.File
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
				rail := railOf(t, name, page)
				// One disclosure per page kind that has anything to
				// list, plus Demos, and exactly one of them open: the
				// section for the page you are reading. The rest are
				// folded, which is what makes a rail carrying the whole
				// vocabulary usable from any of the five pages.
				// A page kind with a Nav function has entries to list
				// and is therefore a disclosure; one without is a plain
				// link. Demos is always a disclosure. Derived rather
				// than counted so a sixth page kind needs nothing here.
				sections := 1
				for _, pk2 := range pageKinds() {
					if pk2.Nav != nil {
						sections++
					}
				}
				if n := strings.Count(rail, "<details"); n != sections {
					t.Errorf("%s: %d sidebar disclosures, want %d", name, n, sections)
				}
				want := 0
				if pk.Nav != nil {
					want = 1
				}
				if n := strings.Count(rail, "<details open"); n != want {
					t.Errorf("%s: %d sidebar sections arrive open, want %d (the one this page is)", name, n, want)
				}
				// Every section says its name in this page's language,
				// as a summary where it has entries and as a plain link
				// where it has none yet.
				for _, pk2 := range pageKinds() {
					title := template.HTMLEscapeString(proseIn(locale, pk2.Title))
					if pk2.Nav == nil {
						if !strings.Contains(rail, `class="ds-nav__page"`) || !strings.Contains(rail, `>`+title+`</a>`) {
							t.Errorf("%s: no sidebar link named %q for the page with nothing to list", name, proseIn(locale, pk2.Title))
						}
						continue
					}
					if !strings.Contains(rail, `</span>`+title+`</summary>`) {
						t.Errorf("%s: no sidebar section named %q", name, proseIn(locale, pk2.Title))
					}
				}
				if title := template.HTMLEscapeString(proseIn(locale, "Demos")); !strings.Contains(rail, `</span>`+title+`</summary>`) {
					t.Errorf("%s: no sidebar section named %q", name, proseIn(locale, "Demos"))
				}
				// The disclosure glyph is the framework's own chevron,
				// not a geometric-shape character in a ::before. It is
				// aria-hidden because the section's name is right beside
				// it, and it is the icon set's so it flips on [open]
				// through tokens.css's own rule.
				if n := strings.Count(rail, `<span class="rst-caret" aria-hidden="true"><svg`); n != sections {
					t.Errorf("%s: %d rail disclosures draw a chevron, want %d", name, n, sections)
				}
				for _, glyph := range []string{"▸", "▾", `content: "\25b8"`, `content: "\25be"`} {
					if strings.Contains(page, glyph) {
						t.Errorf("%s still draws a disclosure glyph as the character %q", name, glyph)
					}
				}
				if want := `aria-label="` + template.HTMLEscapeString(proseIn(locale, "Sections and demos")) + `"`; !strings.Contains(page, want) {
					t.Errorf("%s: the sidebar nav is not named in this page's language", name)
				}
			}
		}
	}
}

// The section tab strip over the page names every page of this
// directory, exactly one of them current, and the current one is the
// page you are on.
//
// It is the rail's job done again in a row, and it is not decoration:
// below 800px the sidebar shell folds the rail away behind a
// disclosure, so this strip is the only VISIBLE way between the five
// pages on a phone. It shipped with no gate at all — dropping the
// aria-current from it, and cutting it down to a single link, both left
// the suite green — which is why the two assertions below are stated
// separately rather than as one count.
func TestTheSectionTabsNameEveryPage(t *testing.T) {
	files := render(t)
	strip := regexp.MustCompile(`(?s)<div class="ds-switch">.*?</div>`)
	for _, theme := range ui.ThemeNames() {
		for _, locale := range rastrillo.BaseLocales() {
			for _, pk := range pageKinds() {
				name := theme + "/" + locale + "/" + pk.File
				found := strip.FindString(string(files[name]))
				if found == "" {
					t.Errorf("%s: no section tab strip", name)
					continue
				}
				var links [][]string
				for _, m := range regexp.MustCompile(`<a href="([^"]*)"([^>]*)>([^<]*)</a>`).FindAllStringSubmatch(found, -1) {
					links = append(links, m)
				}
				if len(links) != len(pageKinds()) {
					t.Errorf("%s: the section tabs have %d entries, want one per page kind (%d)", name, len(links), len(pageKinds()))
					continue
				}
				var current int
				for i, pk2 := range pageKinds() {
					if want := pageHref(mountPath, theme, locale, pk2.File); links[i][1] != want {
						t.Errorf("%s: section tab %d links %q, want %q", name, i, links[i][1], want)
					}
					if want := template.HTMLEscapeString(proseIn(locale, pk2.Title)); links[i][3] != want {
						t.Errorf("%s: section tab %d is labelled %q, want %q", name, i, links[i][3], want)
					}
					marked := strings.Contains(links[i][2], `aria-current="page"`)
					if marked {
						current++
					}
					if marked != (pk2.Kind == pk.Kind) {
						t.Errorf("%s: the %s tab is marked current=%v; the page you are on is %s", name, pk2.Kind, marked, pk.Kind)
					}
				}
				if current != 1 {
					t.Errorf("%s: %d section tabs marked current, want exactly 1", name, current)
				}
				if want := `aria-label="` + template.HTMLEscapeString(proseIn(locale, "Sections")) + `"`; !strings.Contains(found, want) {
					t.Errorf("%s: the section tabs are not named in this page's language", name)
				}
			}
		}
	}
}

// ── The three navigation surfaces ────────────────────────────────────
//
// The Overview's routes, the prev/next pair at the foot of every page,
// and the overview link at the head of every rail section. All three
// are read off pageKinds(), and all three are gated here for the reason
// the spec gives: the chrome switchers and the section tab strip were
// both built the same way, both shipped ungated, and both were caught
// by a reviewer mutating the renderer rather than by a test. Every gate
// below walks pageKinds(), so a sixth page kind joins each surface with
// no edit — and fails these three the moment it does not.

// railSection is one group of the rail as the page actually renders it:
// a <details> holding a title and a run of links, or the plain link a
// section with nothing to list renders as instead.
var railSection = regexp.MustCompile(`(?s)<details([^>]*)><summary>.*?</summary>(.*?)</details>|<a class="ds-nav__page" href="([^"]*)"[^>]*>(.*?)</a>`)

// railTitle reads the section's own name out of either shape: past the
// caret icon in a <summary>, or the whole text of the plain link.
var railTitle = regexp.MustCompile(`(?s)<summary>(?:<span class="rst-caret" aria-hidden="true">.*?</span>)?(.*?)</summary>`)

// railSections splits one rendered rail into its groups, in order.
type railGroup struct {
	Title string
	Body  string // the links inside a disclosure; empty for a plain link
	Href  string // set only for the plain-link shape
	Open  bool   // true for the disclosure shape
}

func railSections(rail string) []railGroup {
	var out []railGroup
	for _, m := range railSection.FindAllString(rail, -1) {
		if strings.HasPrefix(m, "<details") {
			sub := railSection.FindStringSubmatch(m)
			title := ""
			if t := railTitle.FindStringSubmatch(m); t != nil {
				title = t[1]
			}
			out = append(out, railGroup{Title: title, Body: sub[2], Open: true})
			continue
		}
		sub := railSection.FindStringSubmatch(m)
		out = append(out, railGroup{Title: sub[4], Href: sub[3]})
	}
	return out
}

// §12. Every section of the rail has a route to the top of its own
// page, and it is the first thing under the section's name.
//
// The gap it closes: a section that lists anything draws its title as a
// <summary>, which discloses rather than navigates, so expanding TOKENS
// showed nine fragments of tokens.html and no way to tokens.html
// itself. A section with nothing to list is already a plain link to its
// page, and does not need a second one under it — so the gate accepts
// either shape and demands exactly one route per page kind either way.
//
// The three things it proves, which are the three ways this can be
// built wrong and still look right on the page a reviewer opens:
//
//   - derived from pageKinds(): the loop is over the table, so a sixth
//     page kind is expected here the day its row lands;
//   - correct on EVERY page, not on the first one: the rail is the same
//     on all five, and a link pinned to a fixed page is invisible on
//     the page it was pinned to;
//   - exactly one per section, and none in Demos, which is a run of
//     links out of the gallery rather than a page of it.
//
// The accessible name is the other half. The visible label is Paul's
// word — Overview, the word the filter matches — and it is the same on
// every section on purpose, so the name a screen reader reads has to
// carry the section instead. WCAG 2.2 SC 2.5.3 Label in Name asks that
// the accessible name CONTAIN the visible label, and the gate asserts
// exactly that rather than trusting the prose table to keep the shape.
func TestEverySectionOfTheRailRoutesToItsOwnPage(t *testing.T) {
	files := render(t)
	for _, theme := range ui.ThemeNames() {
		for _, locale := range rastrillo.BaseLocales() {
			label := proseIn(locale, "Overview")
			for _, pk := range pageKinds() {
				name := theme + "/" + locale + "/" + pk.File
				rail := railOf(t, name, string(files[name]))
				if rail == "" {
					continue
				}
				groups := railSections(rail)
				// The page kinds, in table order, and Demos last.
				if want := len(pageKinds()) + 1; len(groups) != want {
					t.Errorf("%s: the rail has %d sections, want %d (one per page kind, plus Demos)", name, len(groups), want)
					continue
				}
				overviews := 0
				for i, pk2 := range pageKinds() {
					g := groups[i]
					want := pageHref(mountPath, theme, locale, pk2.File)
					title := template.HTMLEscapeString(proseIn(locale, pk2.Title))
					if g.Title != title {
						t.Errorf("%s: rail section %d is %q, want %q", name, i, g.Title, title)
						continue
					}
					// One route per page kind, wherever it is: a bare
					// page address, no fragment, once in the whole rail.
					if n := strings.Count(rail, `href="`+want+`"`); n != 1 {
						t.Errorf("%s: the rail carries %d links to %s, want exactly 1", name, n, want)
					}
					if !g.Open {
						// The plain-link shape IS the route.
						if g.Href != want {
							t.Errorf("%s: the %s section links %q, want %q", name, pk2.Kind, g.Href, want)
						}
						continue
					}
					first := anchorHref.FindStringIndex(g.Body)
					if first == nil {
						t.Errorf("%s: the %s section of the rail has no links at all", name, pk2.Kind)
						continue
					}
					item := g.Body[first[0]:]
					if i := strings.Index(item, "</a>"); i >= 0 {
						item = item[:i+len("</a>")]
					}
					aria := template.HTMLEscapeString(proseIn(locale, "{section} overview", "section", proseIn(locale, pk2.Title)))
					wantItem := `<a href="` + want + `" aria-label="` + aria + `">` + template.HTMLEscapeString(label) + `</a>`
					if item != wantItem {
						t.Errorf("%s: the first item under %s is\n  %s\nwant\n  %s", name, pk2.Kind, item, wantItem)
						continue
					}
					overviews++
					// SC 2.5.3 Label in Name, asserted rather than
					// assumed: the accessible name has to contain the
					// visible label, or a reader who says "Overview" to
					// a voice control cannot activate the link they can
					// see.
					visible, spoken := proseIn(locale, "Overview"), proseIn(locale, "{section} overview", "section", proseIn(locale, pk2.Title))
					if !strings.Contains(strings.ToLower(spoken), strings.ToLower(visible)) {
						t.Errorf("%s: the %s section's accessible name %q does not contain its visible label %q (WCAG 2.2 SC 2.5.3)", name, pk2.Kind, spoken, visible)
					}
				}
				// Demos is the one section that is not a page of this
				// gallery, so it gets no overview link — and the count
				// says so out loud rather than leaving it to the
				// per-section loop above, which never looks at Demos.
				var bodies string
				for _, g := range groups {
					bodies += g.Body
				}
				if n := strings.Count(bodies, ` aria-label="`); n != overviews {
					t.Errorf("%s: %d rail links carry an accessible name, want %d (one per section that discloses, and none in Demos)", name, n, overviews)
				}
				if demos := groups[len(groups)-1]; demos.Title != template.HTMLEscapeString(proseIn(locale, "Demos")) {
					t.Errorf("%s: the last rail section is %q, want Demos", name, demos.Title)
				} else if strings.Contains(demos.Body, ` aria-label="`) {
					t.Errorf("%s: the Demos section carries an overview link; it is not a page of this gallery", name)
				}
			}
		}
	}
}

// §10. Every page ends with its place in the sequence: the page before
// it on the inline start, the page after it on the inline end, each
// naming where it goes.
//
// Walked over every page kind for the same reason the chrome gate is:
// the ends of the sequence are the only two pages where a missing link
// is correct, so a surface built by hand would pass on any single page
// a reviewer happened to open. The gate reads the pair off pageKinds()
// and asserts the exact link, so pinning either side to a fixed page
// fails on four pages out of five, and dropping the pair fails on all
// of them.
func TestEveryPageEndsWithItsPlaceInTheSequence(t *testing.T) {
	files := render(t)
	pair := regexp.MustCompile(`(?s)<nav class="ds-updown"([^>]*)>(.*?)</nav>`)
	for _, theme := range ui.ThemeNames() {
		for _, locale := range rastrillo.BaseLocales() {
			for at, pk := range pageKinds() {
				name := theme + "/" + locale + "/" + pk.File
				page := string(files[name])
				found := pair.FindAllStringSubmatch(page, -1)
				if len(found) != 1 {
					t.Errorf("%s: %d prev/next strips, want exactly 1", name, len(found))
					continue
				}
				attrs, body := found[0][1], found[0][2]
				if want := ` aria-label="` + template.HTMLEscapeString(proseIn(locale, "Previous and next")) + `"`; attrs != want {
					t.Errorf("%s: the prev/next strip is named %q, want %q", name, attrs, want)
				}
				// At the foot: nothing of the page's own content comes
				// after it. The strip closes the content column.
				_, rest, _ := strings.Cut(page, `</nav>`+"\n\n</div>\n</main>")
				if rest == "" {
					t.Errorf("%s: the prev/next strip is not the last thing in the content column", name)
				}
				kinds := pageKinds()
				var want []string
				if at > 0 {
					prev := kinds[at-1]
					want = append(want, `<a class="ds-updown__prev" href="`+pageHref(mountPath, theme, locale, prev.File)+`">`+
						template.HTMLEscapeString(proseIn(locale, "Previous: {page}", "page", proseIn(locale, prev.Title)))+`</a>`)
				}
				if at < len(kinds)-1 {
					next := kinds[at+1]
					want = append(want, `<a class="ds-updown__next" href="`+pageHref(mountPath, theme, locale, next.File)+`">`+
						template.HTMLEscapeString(proseIn(locale, "Next: {page}", "page", proseIn(locale, next.Title)))+`</a>`)
				}
				if got := strings.Join(want, ""); body != got {
					t.Errorf("%s: the prev/next strip is\n  %s\nwant\n  %s", name, body, got)
				}
				// The ends of the sequence have one link and not two,
				// and it is the right one. Asserted separately because
				// a strip that always emitted both would still match a
				// comparison built from the same wrong assumption.
				links := anchorHref.FindAllString(body, -1)
				if wantN := len(want); len(links) != wantN {
					t.Errorf("%s: the prev/next strip has %d links, want %d", name, len(links), wantN)
				}
				if at == 0 && strings.Contains(body, "ds-updown__prev") {
					t.Errorf("%s: the first page in the sequence has a previous link", name)
				}
				if at == len(kinds)-1 && strings.Contains(body, "ds-updown__next") {
					t.Errorf("%s: the last page in the sequence has a next link", name)
				}
			}
		}
	}
}

// The Overview routes into every other page of the gallery, in table
// order, each with the one sentence that row carries about itself.
//
// This is the surface that stops the landing page being a heading with
// nothing under it, so the gate holds both halves: that the list is the
// whole of pageKinds() minus the page it is on — never a literal list,
// so a sixth page kind appears the day its row lands — and that every
// entry actually says something, because a route with an empty sentence
// under it is the blank page again in a smaller box.
func TestTheOverviewRoutesIntoEveryOtherPage(t *testing.T) {
	files := render(t)
	list := regexp.MustCompile(`(?s)<ul class="ds-routes">(.*?)</ul>`)
	entry := regexp.MustCompile(`<li><a href="([^"]*)">([^<]*)</a><span>([^<]*)</span></li>`)
	for _, theme := range ui.ThemeNames() {
		for _, locale := range rastrillo.BaseLocales() {
			name := theme + "/" + locale + "/" + fileOf("overview")
			page := string(files[name])
			found := list.FindAllStringSubmatch(page, -1)
			if len(found) != 1 {
				t.Errorf("%s: %d route lists, want exactly 1", name, len(found))
				continue
			}
			got := entry.FindAllStringSubmatch(found[0][1], -1)
			var want []pageKind
			for _, pk := range pageKinds() {
				if pk.Kind != "overview" {
					want = append(want, pk)
				}
			}
			if len(got) != len(want) {
				t.Errorf("%s: the Overview routes into %d pages, want one per other page kind (%d)", name, len(got), len(want))
				continue
			}
			for i, pk := range want {
				if href := pageHref(mountPath, theme, locale, pk.File); got[i][1] != href {
					t.Errorf("%s: route %d links %q, want %q", name, i, got[i][1], href)
				}
				if title := template.HTMLEscapeString(proseIn(locale, pk.Title)); got[i][2] != title {
					t.Errorf("%s: route %d is labelled %q, want %q", name, i, got[i][2], title)
				}
				if pk.Blurb == "" {
					t.Errorf("%s: page kind %q has no Blurb — the Overview cannot say what is on it", name, pk.Kind)
					continue
				}
				if blurb := template.HTMLEscapeString(proseIn(locale, pk.Blurb)); got[i][3] != blurb {
					t.Errorf("%s: route %d says %q, want %q", name, i, got[i][3], blurb)
				}
			}
			// The Overview does not route to itself, and no other page
			// carries the list at all.
			if strings.Contains(found[0][1], `href="`+pageHref(mountPath, theme, locale, fileOf("overview"))+`"`) {
				t.Errorf("%s: the Overview routes to itself", name)
			}
		}
	}
	for _, pk := range pageKinds() {
		if pk.Kind == "overview" {
			continue
		}
		name := RootTheme() + "/en/" + pk.File
		if list.MatchString(string(files[name])) {
			t.Errorf("%s: carries the Overview's route list", name)
		}
	}
}

// Paul's paragraph is on the Overview, word for word, in every theme
// and in English — and translated, not left in English, everywhere
// else. It is the one piece of copy on this site that was written by a
// person rather than derived from the code, and the one that went
// through his review, so it is asserted as a literal here rather than
// left to the generic prose gates.
func TestPaulsParagraphOpensTheOverview(t *testing.T) {
	const paragraph = `The Rastrillo design system aims to be a starter framework for any app to get a consistent, polished, accessible UI with no or minimal JavaScript dependence, available in multiple languages, and using clean, modern HTML and CSS. It's designed to be delightful to use with or without LLM assistance, and easily remixable.`
	files := render(t)
	if _, ok := prose[paragraph]; !ok {
		t.Fatalf("prose.go has no entry for Paul's paragraph — it has been reworded, and every translation of it is now of something else")
	}
	for _, theme := range ui.ThemeNames() {
		name := theme + "/en/" + fileOf("overview")
		want := `<p class="ds-intro">` + template.HTMLEscapeString(paragraph) + `</p>`
		if !strings.Contains(string(files[name]), want) {
			t.Errorf("%s does not open with Paul's paragraph, verbatim", name)
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
// not, so the gate became every id on the page — and then the samples
// moved into preview frames, where their ids are escaped and this gate
// could no longer see them.
//
// So it walks both: the 181 files, and the 3,959 documents the frames
// carry. Every one of them is a document in its own right, and a
// duplicate inside one is exactly as broken as a duplicate out here —
// with the difference that a reader who opens a Code tab is being
// shown it as markup to copy. They are all clean today; the point of
// checking is that a field id repeated across two states of one
// partial would have been invisible otherwise.
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
	// would actually build an element. That is also why the preview
	// documents have to be unescaped before they can be looked at.
	var framed int
	for _, name := range names {
		page := string(files[name])
		uniqueIDs(t, name, page, true)
		for i, doc := range srcdocs(page) {
			framed++
			uniqueIDs(t, fmt.Sprintf("%s srcdoc %d", name, i), doc, false)
		}
	}
	if framed == 0 {
		t.Error("no preview documents found at all — the srcdoc extractor has stopped working, and half this gate with it")
	}
}

// uniqueIDs holds one document to one id each. mustHaveOne is the
// difference between a page of this tree, which always carries at
// least main's, and a preview document, where a sample with no id in
// it is an ordinary thing for a sample to be.
func uniqueIDs(t *testing.T, name, doc string, mustHaveOne bool) {
	t.Helper()
	seen := map[string]int{}
	var order []string
	for _, m := range elementID.FindAllStringSubmatch(doc, -1) {
		if seen[m[1]] == 0 {
			order = append(order, m[1])
		}
		seen[m[1]]++
	}
	if len(order) == 0 && mustHaveOne {
		t.Errorf("%s: no ids at all — every page in this tree has at least main's", name)
	}
	for _, id := range order {
		if seen[id] != 1 {
			t.Errorf("%s: id %q appears %d times; a fragment can only ever reach the first, and the document is invalid", name, id, seen[id])
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
	// The demo application's records — the rows of its grids and the
	// facts of its one open request. Everything the APPLICATION says —
	// its screens, its controls, its statuses, and the HEADERS over
	// these rows — is a prose key and is translated; what is below is
	// what its database would hold. A person's name, a request's
	// subject, a date, a queue name and the app's own brand are
	// content, and translating them would suggest the framework ships
	// those words.
	//
	// "Status" stays here for the SHELL demos, whose sample screen
	// writes its column headers literally (shellTemplate, "Post" /
	// "Status"). The demo application's copy of that word is
	// {{P "Status"}} and is translated; both can be true, because the
	// shell demo is a screen framing a shell and the demo application
	// is an application.
	"Harbour":                            true,
	"Ada Lovelace":                       true,
	"ada@example.com":                    true,
	"A":                                  true,
	"Invoice #4471 never arrived":        true,
	"Card declined on renewal":           true,
	"Export takes twenty minutes":        true,
	"Seat count is wrong on the invoice": true,
	"Fiona Reid · 09:12":                 true,
	"Otto Neurath · 08:40":               true,
	"Hedy Lamarr · 11 August":            true,
	"Fiona Reid · Billing":               true,
	"Otto Neurath · Billing":             true,
	"Mary Sherman · Data":                true,
	"Hedy Lamarr · Billing":              true,
	"12 August":                          true,
	"11 August":                          true,
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
	// The demo application's one open record: the subject it is a
	// request about, and the queue it sits in.
	"Invoice #4471 never arrived": true,
	"Billing":                     true,
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
	"SearchAction": true, "CancelHref": true,
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
// pageTemplates is every template constant this gate sweeps: the frame,
// the preview widget, the five section bodies, and the two demo pages.
// Built off bodyTemplates() so a page kind added to that table is swept
// the day it lands rather than the day somebody remembers.
func pageTemplates() []struct{ name, src string } {
	out := []struct{ name, src string }{
		{"pageTemplate", pageTemplate},
		{"viewTemplate", viewTemplate},
		{"modalTemplate", modalTemplate},
		{"shellTemplate", shellTemplate},
		{"demoTemplate", demoTemplate},
	}
	for _, body := range bodyTemplates() {
		out = append(out, struct{ name, src string }{body.kind + "Body", body.src})
	}
	return out
}

func TestNoUnregisteredEnglishInThePageTemplates(t *testing.T) {
	var allText, allPairs int
	for _, tt := range pageTemplates() {
		found := literalText(tt.src)
		allText += len(found)
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
		for _, action := range templateAction.FindAllString(tt.src, -1) {
			for _, kv := range dictArguments(action) {
				allPairs++
				name, value := kv[0], kv[1]
				if dictMachineArgs[name] || dictFixtures[value] || !templateLetter.MatchString(value) {
					continue
				}
				t.Errorf("%s passes %q as %s in a dict. If the page is saying it, wrap it in (P …) and translate it in prose.go; "+
					"if it is a machine's value, its argument name belongs in dictMachineArgs; "+
					"if it is sample data, add it to dictFixtures and say what it is.", tt.name, value, name)
			}
		}
	}
	// The extractors' own self-check. It used to be made per template
	// and named the two that legitimately say nothing of their own;
	// since the split there are five bodies and three of them are all
	// {{P …}} and field references, which is what a well-behaved body
	// looks like. So the check is made over the whole set instead: a
	// tokeniser that has stopped tokenising returns nothing anywhere,
	// and that is what these two numbers catch.
	if allText == 0 {
		t.Error("no literal text found in any page template — the extractor has stopped working, and this gate with it")
	}
	if allPairs == 0 {
		t.Error("no literal dict arguments found in any page template — the tokeniser has stopped working, and this pass with it")
	}
	t.Logf("%d literal runs and %d dict arguments across %d templates", allText, allPairs, len(pageTemplates()))
}

// TestVendoredAxeStaysOutOfTheTree is the other half of the containment
// gate in ui/axe_test.go. That one holds the library's shipped assets;
// this one holds the 369 files the gallery renders. The scanner is
// read off disk by the browser-tagged accessibility drive and injected
// at run time, and that is the only place it is allowed to exist.
func TestVendoredAxeStaysOutOfTheTree(t *testing.T) {
	const marker = "Deque Systems"
	for name, body := range render(t) {
		if strings.Contains(string(body), marker) {
			t.Errorf("%s carries the vendored axe-core: the scanner must not ship with the thing it scans", name)
		}
	}
	for name := range render(t) {
		if strings.Contains(name, "axe") {
			t.Errorf("the tree renders %s, which looks like the vendored scanner", name)
		}
	}
}

// TestEveryFrameTitleIsUniqueOnThePage is WCAG 2.0 A 4.1.2 for the one
// element this page has a hundred and ten of. A frame's title is what a
// screen reader announces on entering it and what a frame list shows;
// eleven frames called "the field sample" is a list that cannot be used
// to get anywhere. The accessibility gate found it (as an incomplete —
// see previewTitle); this holds it without needing a browser.
func TestEveryFrameTitleIsUniqueOnThePage(t *testing.T) {
	titleAttr := regexp.MustCompile(`<iframe[^>]*\stitle="([^"]*)"`)
	for name, body := range render(t) {
		if !strings.HasSuffix(name, ".html") {
			continue
		}
		seen := map[string]int{}
		for _, m := range titleAttr.FindAllStringSubmatch(string(body), -1) {
			seen[m[1]]++
		}
		for title, n := range seen {
			if n > 1 {
				t.Errorf("%s has %d frames titled %q — a frame title is an accessible name and has to be unique on the page", name, n, html.UnescapeString(title))
			}
		}
	}
}
