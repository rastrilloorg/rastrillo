package ui

import (
	"errors"
	"html/template"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"amadan.net/rastrillo/rastrillo"
)

// appAssets is an app's static directory as rastrillo new writes it:
// every vendored file the shells link but theme.css, which is never
// sliced and which no test here reads, less each of deleted.
func appAssets(deleted ...string) *rastrillo.Assets {
	files := fstest.MapFS{}
	for name, body := range map[string][]byte{
		"tokens.css": TokensCSS(), "shell.css": ShellCSS(),
		"rastrillo.js": ShimJS(), "busy.js": BusyJS(), "live.js": LiveJS(),
		"light.js": LightJS(), "anchor.js": AnchorJS(),
		"select.js": SelectJS(), "calendar.js": CalendarJS(),
		"datetime.js": DatetimeJS(), "shell.js": ShellJS(),
	} {
		if !slices.Contains(deleted, name) {
			files["static/"+name] = &fstest.MapFile{Data: body}
		}
	}
	return rastrillo.NewAssets(files)
}

// sliced is the setup of an app that passes its assets to PagesConfig
// and its asset func, as a new app's render.go does, served at base.
func sliced(assets *rastrillo.Assets, base string) shellSetup {
	return shellSetup{assets: assets, asset: assets.Path, base: base}
}

var tokensLink = regexp.MustCompile(
	`<link rel="stylesheet" href="([^"]*/static/tokens\.[^"]*)">`)

// linkedTokens is the URL of the tokens stylesheet a page links, "" for
// none.
func linkedTokens(page string) string {
	match := tokensLink.FindStringSubmatch(page)
	if match == nil {
		return ""
	}
	return match[1]
}

func TestEachShellLinksItsPagesSlice(t *testing.T) {
	contents := maps.Clone(shellContents)
	// A second live page, showing a field the first does not: each
	// links the union of every page's hooks, so the two share a URL.
	contents["live-select"] = shellContents["select"] +
		`{{define "region:items"}}<p>Running</p>{{end}}`
	for _, shell := range LayoutNames() {
		layout, _ := Layout(shell)
		assets := appAssets()
		whole := assets.Path("static/tokens.css")
		links := map[string]string{}
		pages := renderPagesWith(t, layout, contents, sliced(assets, ""))
		for name, page := range pages {
			links[name] = linkedTokens(page)
		}
		plain := links["plain"]
		if !strings.HasPrefix(plain, "/static/tokens.") || plain == whole {
			t.Errorf("%s: the plain page links %q, want a slice, not %q",
				shell, plain, whole)
		}
		if plain == links["select"] {
			t.Errorf("%s: the plain and select pages share %q", shell, plain)
		}
		if links["live"] != links["live-select"] || links["live"] == plain {
			t.Errorf("%s: the live pages link %q and %q, want one "+
				"slice, not the plain page's %q", shell, links["live"],
				links["live-select"], plain)
		}
	}
}

func TestARenderOutsidePagesLinksTheWholeFile(t *testing.T) {
	layout, _ := Layout("column")
	set := template.Must(template.New("layout").Funcs(Funcs()).Funcs(
		template.FuncMap{"asset": func(name string) string {
			return "/" + name
		}}).ParseFS(Templates(), "*.html"))
	template.Must(set.Parse(string(layout)))
	template.Must(set.Parse(shellContents["plain"]))
	var page strings.Builder
	if err := set.ExecuteTemplate(&page, "layout", nil); err != nil {
		t.Fatal(err)
	}
	if got := linkedTokens(page.String()); got != "/static/tokens.css" {
		t.Errorf("outside Pages the layout links %q, want the whole file",
			got)
	}
	var want []string
	for _, script := range static(optionalScripts()...) {
		want = append(want, "/"+script)
	}
	got := optionalIn(deferredScripts(page.String()))
	if !slices.Equal(got, want) {
		t.Errorf("outside Pages the layout loads %v, want %v", got, want)
	}
}

// The pages of an app that serves the shipped layout and has not added
// Assets to its PagesConfig link the whole file through its own asset
// func, and every script a page uses, as the app has them all.
func TestPagesWithoutAssetsLinkTheWholeFile(t *testing.T) {
	layout, _ := Layout("column")
	assets := appAssets()
	pages := renderPagesWith(t, layout, shellContents,
		shellSetup{asset: assets.Path})
	whole := assets.Path("static/tokens.css")
	for name, page := range pages {
		if got := linkedTokens(page); got != whole {
			t.Errorf("%s links %q, want the whole file, %q", name, got, whole)
		}
	}
	for _, script := range static(optionalScripts()...) {
		if !slices.Contains(deferredScripts(pages["live"]),
			assets.Path(script)) {
			t.Errorf("the live page does not load %s", script)
		}
	}
}

// Review Focus 5: under a base path the slice resolves like every other
// asset, through path.
func TestTheSliceURLHonoursTheBasePath(t *testing.T) {
	layout, _ := Layout("column")
	assets := appAssets()
	pages := renderPagesWith(t, layout, shellContents,
		sliced(assets, "/acme"))
	link := linkedTokens(pages["plain"])
	slice, under := strings.CutPrefix(link, "/acme")
	if !under || !strings.HasPrefix(slice, "/static/tokens.") ||
		slice == assets.Path("static/tokens.css") {
		t.Fatalf("under /acme the plain page links %q, want "+
			"/acme/static/tokens.<slice>.css", link)
	}
	recorder := httptest.NewRecorder()
	assets.Handler().ServeHTTP(recorder,
		httptest.NewRequest(http.MethodGet, slice, nil))
	if recorder.Code != http.StatusOK {
		t.Errorf("GET %s: %d, want 200", slice, recorder.Code)
	}
}

// Review Focus 4: a layout from before slicing keeps its own tags, and
// with them the whole stylesheet and each script once, though the app
// now passes its assets.
func TestOlderLayoutsLoadEachScriptOnce(t *testing.T) {
	assets := appAssets()
	every := []string{assets.Path("static/rastrillo.js")}
	for _, script := range static(optionalScripts()...) {
		every = append(every, assets.Path(script))
	}
	// Before iteration 1 every tag was fixed; in it, the four fixed
	// tags stayed and neededScripts added the component scripts.
	for _, file := range []string{"fixed-tags.html", "needed-scripts.html"} {
		layout, err := os.ReadFile("testdata/older/" + file)
		if err != nil {
			t.Fatal(err)
		}
		pages := renderPagesWith(t, layout, shellContents,
			sliced(assets, ""))
		for name, page := range pages {
			if got := linkedTokens(page); got !=
				assets.Path("static/tokens.css") {
				t.Errorf("%s %s links %q, want the whole file",
					file, name, got)
			}
			srcs := deferredScripts(page)
			if len(slices.Compact(slices.Sorted(slices.Values(srcs)))) !=
				len(srcs) {
				t.Errorf("%s %s loads a script twice: %v", file, name, srcs)
			}
		}
		for _, script := range every {
			if !slices.Contains(deferredScripts(pages["live"]), script) {
				t.Errorf("%s: the live page does not load %s", file, script)
			}
		}
	}
}

// Review Focus 2 on the first screen a signed-out visitor meets: the
// sign-in card's button is lit, and an app that deleted light.js, as
// the layouts' comments allow, must not link it there.
func TestADeletedLightJSIsLeftOffAStagePage(t *testing.T) {
	layout, _ := Layout("stage")
	assets := appAssets("light.js")
	card := map[string]string{
		"signin": content(`<button rst-btn>Sign in</button>`),
	}
	page := renderPagesWith(t, layout, card, sliced(assets, ""))["signin"]
	for _, src := range deferredScripts(page) {
		if strings.HasPrefix(src, "/static/light.") {
			t.Errorf("the stage page links the deleted %s", src)
		}
	}
	whole := assets.Path("static/tokens.css")
	if got := linkedTokens(page); got == whole || got == "" {
		t.Errorf("the stage page links %q, want its slice", got)
	}
}

// A live page can be sent any of its regions on its own, as a fragment
// (live.ExecuteRegions), so a region the page's first render never
// calls still shows its hooks there: the whole app's slice must keep
// their rules.
func TestTheLiveSliceKeepsARegionThePageNeverCalls(t *testing.T) {
	layout, _ := Layout("column")
	assets := appAssets()
	contents := map[string]string{
		"plain": shellContents["plain"],
		"live": content(`<h1>Jobs</h1><div id="items"></div>`) +
			`{{define "region:items"}}<article rst-card>Running</article>` +
			`{{end}}`,
	}
	pages := renderPagesWith(t, layout, contents, sliced(assets, ""))
	slice := func(name string) string {
		recorder := httptest.NewRecorder()
		link := linkedTokens(pages[name])
		assets.Handler().ServeHTTP(recorder,
			httptest.NewRequest(http.MethodGet, link, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET %s: %d, want 200", link, recorder.Code)
		}
		return recorder.Body.String()
	}
	if strings.Contains(slice("plain"), "[rst-card]") {
		t.Fatal("the plain page's slice keeps rst-card: this proves nothing")
	}
	if !strings.Contains(slice("live"), "[rst-card]") {
		t.Error("the live page's slice has no rules for rst-card, " +
			"which its region:items shows when it is sent alone")
	}
}

// assetMount is an app's own type for its asset func.
type assetMount func(name string) string

// An app that takes the new layout without passing its Assets links
// the whole file through its own asset func, in either shape the
// layout's other links accept, and not a /static path its mount may
// not serve. (A func returning template.URL fails those links, as path
// takes a string.)
func TestTheWholeFileGoesThroughTheAppsAssetFunc(t *testing.T) {
	layout, _ := Layout("column")
	moved := func(name string) string {
		return "/assets/" + strings.TrimPrefix(name, "static/")
	}
	for shape, asset := range map[string]any{
		"a named func type": assetMount(moved),
		"a string and an error": func(name string) (string, error) {
			return moved(name), nil
		},
	} {
		page := renderPagesWith(t, layout, shellContents,
			shellSetup{asset: asset})["plain"]
		if !strings.Contains(page, `href="/assets/tokens.css"`) {
			t.Errorf("an asset func as %s: the page links no "+
				"/assets/tokens.css:\n%s", shape, page)
		}
	}
}

// An asset func that fails for tokens.css leaves the page the bare
// path, and says so once, not on every render.
func TestAFailingAssetFuncLinksTheBarePathAndSaysSo(t *testing.T) {
	var logged strings.Builder
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	defer slog.SetDefault(previous)
	layout, _ := Layout("column")
	failing := func(name string) (string, error) {
		if name == "static/tokens.css" {
			return "", errors.New("no such asset")
		}
		return "/" + name, nil
	}
	pages := renderPagesWith(t, layout, shellContents,
		shellSetup{asset: failing})
	for name, page := range pages {
		if got := linkedTokens(page); got != "/static/tokens.css" {
			t.Errorf("%s links %q, want /static/tokens.css", name, got)
		}
	}
	said := logged.String()
	if strings.Count(said, "\n") != 1 ||
		!strings.Contains(said, "no such asset") {
		t.Errorf("logged %q, want the asset func's error once", said)
	}
}
