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
const treeCommitted = false

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
// committed tree of 144 pages.
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
	page := string(render(t)["ink/en/index.html"])
	if page == "" {
		t.Fatal("no ink/en/index.html in the rendered tree")
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
	page := string(render(t)["ink/en/index.html"])
	if page == "" {
		t.Fatal("no ink/en/index.html in the rendered tree")
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

// linkTarget matches an href or src attribute value, so two pages can
// be compared with their paths blanked out.
var linkTarget = regexp.MustCompile(`(href|src)="[^"]*"`)

func pathless(page string) string {
	return linkTarget.ReplaceAllString(page, `$1=""`)
}

var (
	langAttr      = regexp.MustCompile(`<html lang="([^"]*)" dir="([^"]*)"`)
	scriptSrcAbs  = regexp.MustCompile(`src="/`)
	styleHrefAbs  = regexp.MustCompile(`<link[^>]+href="/`)
	unresolvedKey = "rastrillo.ui."
)

// Every page is a whole document in the right language, with no
// unresolved catalog key and no absolute asset path — the tree is
// served from a subdirectory of rastrillo.org, so an asset link
// starting with "/" would resolve to the site root and 404.
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
		if styleHrefAbs.MatchString(body) {
			t.Errorf("%s: an absolute stylesheet link", name)
		}
		if scriptSrcAbs.MatchString(body) {
			t.Errorf("%s: an absolute script src", name)
		}
	}
}

// localeOfPath reads the locale a page is for out of its path:
// "<theme>/<locale>/…", and the root index is ink/en by definition.
func localeOfPath(p string) string {
	parts := strings.Split(path.Clean(p), "/")
	if len(parts) < 2 {
		return "en"
	}
	return parts[1]
}

// Every theme × locale × shell combination is present, plus the root
// index and the seven shared assets — the tree's shape is part of its
// contract with the website's sync script.
func TestTreeShapeIsComplete(t *testing.T) {
	files := render(t)
	want := []string{
		"index.html",
		"tokens.css", "rastrillo.js", "select.js", "datetime.js",
	}
	for _, theme := range ui.ThemeNames() {
		want = append(want, "theme-"+theme+".css")
		for _, locale := range rastrillo.BaseLocales() {
			want = append(want, fmt.Sprintf("%s/%s/index.html", theme, locale))
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
			if want := `href="../../theme-` + theme + `.css"`; !strings.Contains(string(files[path]), want) {
				t.Errorf("%s does not link its own theme (%s)", path, want)
			}
		}
	}
	if len(files) != len(want) {
		t.Errorf("tree has %d files, expected exactly %d", len(files), len(want))
	}
}

// The root index is ink/en with every relative path rewritten for the
// tree root — same page, different depth, generated rather than copied.
func TestRootIndexIsInkEnglishAtTheTreeRoot(t *testing.T) {
	files := render(t)
	root, nested := string(files["index.html"]), string(files["ink/en/index.html"])
	if root == "" || nested == "" {
		t.Fatal("root or ink/en index missing")
	}
	if !strings.Contains(root, `href="tokens.css"`) {
		t.Error("root index does not link tokens.css at the tree root")
	}
	if !strings.Contains(nested, `href="../../tokens.css"`) {
		t.Error("ink/en index does not link tokens.css two levels up")
	}
	if strings.Contains(root, "../../") {
		t.Error("root index still carries a two-levels-up path")
	}
	// Same page otherwise. Blanking every href and src leaves exactly
	// what the two copies must share — a prefix parameter is allowed to
	// change where a link points and nothing else.
	if pathless(nested) != pathless(root) {
		t.Error("the root index differs from ink/en by more than its paths")
	}
}

// Both enhanced controls are on the page: the filterable select and the
// natural-language date combobox both boot from the JavaScript the tree
// ships, so the page has to give them something to boot on.
func TestEnhancedControlsAreOnThePage(t *testing.T) {
	page := string(render(t)["ink/en/index.html"])
	for _, want := range []string{"data-rst-select", "data-rst-date", "data-rst-time", "data-rst-range"} {
		if !strings.Contains(page, want) {
			t.Errorf("no %s on the page — the enhancement has nothing to boot on", want)
		}
	}
	if !strings.Contains(page, "<optgroup") {
		t.Error("no hand-written optgroup'd select on the page")
	}
}
