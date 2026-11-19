package designsystem

import (
	"bufio"
	"html/template"
	"os"
	"path"
	"slices"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/ui"
)

// fiestaManifest reads testdata/fiesta-pages.txt: the Fiesta pages this
// gallery may link, one <theme>/<locale>/<page> a line.
func fiestaManifest(t *testing.T) []string {
	t.Helper()
	file, err := os.Open("testdata/fiesta-pages.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return lines
}

// everyFiestaPage is every theme this gallery renders, times every
// locale, times the pages that replaced what left: what the manifest
// has to say, line for line.
func everyFiestaPage() []string {
	var out []string
	for _, theme := range ui.ThemeNames() {
		for _, locale := range rastrillo.BaseLocales() {
			for _, page := range fiestaPages {
				out = append(out, theme+"/"+locale+"/"+page)
			}
		}
	}
	slices.Sort(out)
	return out
}

// fiestaLinks is every href in the tree that goes to Fiesta's design
// system, as the manifest line it names, by the file that carries it.
func fiestaLinks(files map[string][]byte) map[string][]string {
	out := map[string][]string{}
	for name, body := range files {
		if !strings.HasSuffix(name, ".html") {
			continue
		}
		links := anchorHref.FindAllStringSubmatch(string(body), -1)
		for _, link := range links {
			if page, ok := strings.CutPrefix(link[1], fiestaDesignSystem); ok {
				out[name] = append(out[name], page)
			}
		}
	}
	return out
}

// Every Fiesta link the render writes names a page in the manifest,
// and the manifest is exactly the themes and locales this gallery
// renders times the pages that replaced what left, so FiestaPage's own
// check and the manifest cannot disagree. Every manifest line is
// linked somewhere, or it is a page nothing needs.
func TestFiestaLinksNameAPublishedPage(t *testing.T) {
	manifest := fiestaManifest(t)
	got := slices.Sorted(slices.Values(manifest))
	if want := everyFiestaPage(); !slices.Equal(got, want) {
		t.Errorf("the manifest is not every theme, locale and page:"+
			"\n got %v\nwant %v", got, want)
	}

	linked := map[string]int{}
	for name, pages := range fiestaLinks(render(t)) {
		for _, page := range pages {
			if !slices.Contains(manifest, page) {
				t.Errorf("%s links Fiesta's %s, which is not in "+
					"testdata/fiesta-pages.txt", name, page)
			}
			linked[page]++
		}
	}
	for _, page := range manifest {
		if linked[page] == 0 {
			t.Errorf("nothing in the tree links Fiesta's %s", page)
		}
	}

	for _, bad := range [][3]string{
		{"dusk", "en", FiestaTokensPage},
		{"day", "fr", FiestaTokensPage},
		{"day", "en", "colours.html"},
	} {
		if !panics(func() { FiestaPage(bad[0], bad[1], bad[2]) }) {
			t.Errorf("FiestaPage%q did not panic", bad)
		}
	}
}

// panics reports whether call panics.
func panics(call func()) (panicked bool) {
	defer func() { panicked = recover() != nil }()
	call()
	return false
}

// Tokens and UI primitives left for Fiesta: no page of either is in the
// tree, under any theme or locale, and nothing the primitives page
// framed is left behind as a preview document or a marker.
func TestTheGalleryHasNoTokensOrPrimitivesPage(t *testing.T) {
	gone := []string{"tokens.html", "primitives.html"}
	for name, body := range render(t) {
		switch {
		case slices.Contains(gone, path.Base(name)):
			t.Errorf("%s is still in the tree", name)
		case strings.Contains(name, "/primitives/"),
			strings.Contains(name, "/tokens/"):
			t.Errorf("%s is a preview of a page that left", name)
		case strings.Contains(string(body), "<!-- idiom: "):
			t.Errorf("%s still marks a markup idiom", name)
		}
	}
	for _, pk := range pageKinds() {
		if pk.Kind == "tokens" || pk.Kind == "primitives" {
			t.Errorf("pageKinds() still has %q", pk.Kind)
		}
	}
}

// themeOfPath reads the theme a page is for out of its path, as
// localeOfPath reads the locale: the root index is the default theme's.
func themeOfPath(name string) string {
	parts := strings.Split(path.Clean(name), "/")
	if len(parts) < 2 {
		return RootTheme()
	}
	return parts[0]
}

// Every link in every document of the tree, in every theme and locale,
// lands somewhere: under the mount, on a file the render wrote (the
// rail, prev/next, the Overview's routes, the filter's results, which
// are the rail's links, and both switchers); off it, on Lucide from the
// Icons page, or on a Fiesta page in the page's own theme and locale.
// A sample's own route has been deadened to "#".
func TestEveryLinkInTheTreeResolves(t *testing.T) {
	files := render(t)
	var inside, away int
	for name, body := range files {
		if !strings.HasSuffix(name, ".html") {
			continue
		}
		var fiesta []string
		for _, page := range fiestaPages {
			fiesta = append(fiesta,
				FiestaPage(themeOfPath(name), localeOfPath(name), page))
		}
		links := anchorHref.FindAllStringSubmatch(string(body), -1)
		for _, link := range links {
			href := link[1]
			switch {
			case strings.HasPrefix(href, "#"):
			case strings.HasPrefix(href, mountPrefix):
				inside++
				if !resolves(files, href) {
					t.Errorf("%s: %s names nothing in the tree", name, href)
				}
			case href == lucideHome && path.Base(name) == fileOf("icons"):
				away++
			case slices.Contains(fiesta, href):
				away++
			default:
				t.Errorf("%s: %s is not in the tree, nor Lucide from "+
					"the Icons page, nor a Fiesta page in this page's "+
					"theme and locale", name, href)
			}
		}
	}
	if inside == 0 || away == 0 {
		t.Fatalf("%d links into the tree and %d out of it; this gate is "+
			"checking nothing", inside, away)
	}
}

// fiestaPlace is one place the Overview links a Fiesta page from: the
// run of links it sits in, and the link itself.
type fiestaPlace struct{ where, run, link string }

// fiestaPlaces are the three places on one Overview that link a Fiesta
// page: the rail, the routes and the phone index.
func fiestaPlaces(overview, rail, href, title, blurb string) []fiestaPlace {
	_, routes, _ := strings.Cut(overview, `<ul class="ds-routes">`)
	_, index, _ := strings.Cut(overview, `<nav class="ds-index"`)
	away := `href="` + href + `" rel="external">` + title + `</a>`
	return []fiestaPlace{
		{"rail", rail, `<a class="ds-nav__page" ` + away},
		{"routes", routes, `<li><a ` + away + `<span>` + blurb + `</span>`},
		{"phone index", index, `<a ` + away},
	}
}

// Tokens and UI primitives take the places they had in the rail and
// the Overview's routes, as links to Fiesta's pages in the reader's
// theme and language, marked rel="external". The phone index, read off
// the rail, carries them too.
func TestTheOverviewAndRailLinkToFiesta(t *testing.T) {
	files := render(t)
	for _, theme := range ui.ThemeNames() {
		for _, locale := range rastrillo.BaseLocales() {
			where := theme + "/" + locale
			overview := galleryPage(t, files, theme, locale, "overview")
			rail := railOf(t, where, overview)
			for before, away := range fiestaEntries {
				href := FiestaPage(theme, locale, away.Page)
				title := proseIn(locale, away.Title)
				blurb := proseIn(locale, away.Blurb)
				places := fiestaPlaces(overview, rail, href,
					template.HTMLEscapeString(title),
					template.HTMLEscapeString(blurb))
				next := pageHref(mountPath, theme, locale, fileOf(before))
				for _, place := range places {
					at := strings.Index(place.run, place.link)
					if at < 0 {
						t.Errorf("%s: the %s has no %s",
							where, place.where, place.link)
						continue
					}
					// It stands right in front of its page: the next
					// link in the run is that page's.
					rest := place.run[at+len(place.link):]
					following := anchorHref.FindStringSubmatch(rest)
					if following == nil ||
						!strings.HasPrefix(following[1], next) {
						t.Errorf("%s: in the %s, %s is not followed by %s",
							where, place.where, href, next)
					}
				}
			}
		}
	}
}

// Getting started keeps how an app gets and keeps the files (rastrillo
// new, the pinned theme, upgrading, rastrillo doctor) and hands the
// files themselves to Fiesta: no row per file, no download, no weight.
func TestGettingStartedKeepsTheRastrilloHalf(t *testing.T) {
	const doctor = "rastrillo doctor will compare an app's frozen files " +
		"against the module's and offer to re-copy."
	gone := []string{
		`class="ds-files"`, `id="asset-`, ` download="`, "KiB",
	}
	files := render(t)
	for _, theme := range ui.ThemeNames() {
		for _, locale := range rastrillo.BaseLocales() {
			name := theme + "/" + locale + "/" + fileOf("getting-started")
			page := galleryPage(t, files, theme, locale, "getting-started")
			for _, want := range []string{
				"rastrillo new --theme=" + theme + " myapp",
				`const vendoredTheme = "` + theme + `"`,
				proseIn(locale, "Upgrading"),
				proseIn(locale, doctor),
			} {
				if !strings.Contains(page, template.HTMLEscapeString(want)) {
					t.Errorf("%s does not say %q", name, want)
				}
			}
			details := FiestaPage(theme, locale, FiestaFilesPage)
			link := `<a href="` + details + `" rel="external">`
			if !strings.Contains(page, link) {
				t.Errorf("%s does not link Fiesta's file details", name)
			}
			for _, row := range gone {
				if strings.Contains(page, row) {
					t.Errorf("%s still carries %s: the rows for each "+
						"file are Fiesta's", name, row)
				}
			}
		}
	}
}
