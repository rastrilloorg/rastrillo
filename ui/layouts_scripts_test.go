package ui

import (
	"html/template"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/locales"
)

// shellFields is a shell page's data: the localizer, and the two
// fields the select and date pages draw.
type shellFields struct {
	Localized
	Select, Date map[string]any
}

// shellContents are the four pages every shell is rendered with: no
// field, an enhanced select, a date field, and a live region.
var shellContents = map[string]string{
	"plain": content(`<h1>Orders</h1>`),
	"select": content(`{{$L := localizer $}}` +
		`{{template "field-select" (withL .Select $L)}}`),
	"date": content(`{{$L := localizer $}}` +
		`{{template "field-date" (withL .Date $L)}}`),
	"live": content(`<h1>Jobs</h1>`) +
		`{{define "region:items"}}<p>Running</p>{{end}}`,
}

// shellWants is the optional scripts each of shellContents loads in a
// shell with menus, which load anchor.js on every page. Only the live
// page has a form of its own or a lit hook: a fragment may bring one.
var shellWants = map[string][]string{
	"plain":  {"/static/anchor.js"},
	"select": {"/static/anchor.js", "/" + selectSrc},
	"date":   {"/static/anchor.js", "/" + calendarSrc, "/" + datetimeSrc},
	"live": {"/static/busy.js", "/static/live.js", "/static/light.js",
		"/static/anchor.js", "/" + copySrc, "/" + selectSrc,
		"/" + calendarSrc, "/" + datetimeSrc},
}

var deferredSrc = regexp.MustCompile(`<script defer[^>]* src="([^"]*)">`)

// renderShellPages renders each of contents in the layout source and
// returns the HTML by page name.
func renderShellPages(
	t *testing.T, layout []byte, contents map[string]string,
) map[string]string {
	t.Helper()
	return renderPagesWith(t, layout, contents, shellSetup{})
}

// shellSetup is what renderPagesWith gives the pages beyond their files.
type shellSetup struct {
	assets *rastrillo.Assets // PagesConfig.Assets
	asset  any               // the asset func; nil puts "/" before a name
	base   string            // the base path the data carries
}

// renderPagesWith is renderShellPages with setup.
func renderPagesWith(
	t *testing.T, layout []byte, contents map[string]string,
	setup shellSetup,
) map[string]string {
	t.Helper()
	config, err := locales.CheckConfig("en", []string{"en"})
	if err != nil {
		t.Fatal(err)
	}
	app := fstest.MapFS{"layout.html": {Data: layout}}
	pages := map[string]PageFile{}
	for name, body := range contents {
		app[name+".html"] = &fstest.MapFile{Data: []byte(body)}
		pages[name] = PageFile{Path: name + ".html"}
	}
	var asset any = func(name string) string { return "/" + name }
	if setup.asset != nil {
		asset = setup.asset
	}
	built, err := NewPages(PagesConfig{
		App: app, Layout: "layout.html", Pages: pages,
		Locale: config, LocaleFS: app, Assets: setup.assets,
		Funcs: template.FuncMap{
			"asset":      asset,
			"iconAssets": func() template.HTML { return "" },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	data := shellFields{
		Localized: Localized{L: built.Localizer("en"), Base: setup.base},
		Select:    selectData(12),
		Date:      map[string]any{"ID": "due", "Name": "due", "Label": "Due"},
	}
	out := map[string]string{}
	for name := range contents {
		var page strings.Builder
		if err := built.Execute(&page, name, data); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out[name] = page.String()
	}
	return out
}

// deferredScripts is every deferred script a page loads, in order.
func deferredScripts(page string) []string {
	var srcs []string
	for _, match := range deferredSrc.FindAllStringSubmatch(page, -1) {
		srcs = append(srcs, match[1])
	}
	return srcs
}

func componentsIn(srcs []string) []string {
	var components []string
	for _, src := range srcs {
		name := strings.TrimPrefix(src, "/")
		if slices.Contains(allComponentScripts(), name) {
			components = append(components, src)
		}
	}
	return components
}

func shellPages(t *testing.T) map[string]map[string]string {
	t.Helper()
	out := map[string]map[string]string{}
	for _, shell := range []string{"column", "topbar", "sidebar", "console"} {
		layout, ok := Layout(shell)
		if !ok {
			t.Fatalf("no %s layout", shell)
		}
		out[shell] = renderShellPages(t, layout, shellContents)
	}
	return out
}

// optionalIn is the srcs of the scripts a page loads only where used.
func optionalIn(srcs []string) []string {
	optional := static(optionalScripts()...)
	return slices.DeleteFunc(slices.Clone(srcs), func(src string) bool {
		return !slices.Contains(optional, strings.TrimPrefix(src, "/"))
	})
}

func TestEachShellLoadsOnlyTheScriptsItsPageUses(t *testing.T) {
	always := []string{"rastrillo.js"}
	for shell, pages := range shellPages(t) {
		for name, page := range pages {
			srcs := deferredScripts(page)
			got := optionalIn(srcs)
			if !slices.Equal(got, shellWants[name]) {
				t.Errorf("%s %s loads %v, want %v",
					shell, name, got, shellWants[name])
			}
			want := always
			if shell == "sidebar" || shell == "console" {
				want = append(slices.Clone(always), "shell.js")
			}
			for _, script := range want {
				if !slices.Contains(srcs, "/static/"+script) {
					t.Errorf("%s %s does not load %s", shell, name, script)
				}
			}
		}
	}
}

func TestAPagesHooksAlwaysHaveTheirScript(t *testing.T) {
	hooks := map[string][]string{
		"data-rst-select": {selectSrc},
		"data-rst-date":   {calendarSrc, datetimeSrc},
		"data-rst-time":   {calendarSrc, datetimeSrc},
		"data-rst-range":  {calendarSrc, datetimeSrc},
	}
	for shell, pages := range shellPages(t) {
		for name, page := range pages {
			srcs := deferredScripts(page)
			for hook, scripts := range hooks {
				if !strings.Contains(page, hook) {
					continue
				}
				for _, script := range scripts {
					if !slices.Contains(srcs, "/"+script) {
						t.Errorf("%s %s carries %s but does not load %s",
							shell, name, hook, script)
					}
				}
			}
		}
	}
	selectPage := shellPages(t)["column"]["select"]
	if !strings.Contains(selectPage, "data-rst-select") {
		t.Error("the select page has no data-rst-select: the check is vacuous")
	}
}

func TestStageStillLoadsNoComponentScript(t *testing.T) {
	layout, ok := Layout("stage")
	if !ok {
		t.Fatal("no stage layout")
	}
	pages := renderShellPages(t, layout, map[string]string{
		"plain": shellContents["plain"],
	})
	if got := componentsIn(deferredScripts(pages["plain"])); got != nil {
		t.Errorf("stage loads %v, want no component script", got)
	}
}

func TestAnUneditedLayoutStillLoadsEveryComponentScript(t *testing.T) {
	layout, err := os.ReadFile("testdata/pages/scaffold/layout.html")
	if err != nil {
		t.Fatal(err)
	}
	pages := renderShellPages(t, layout, map[string]string{
		"plain": shellContents["plain"],
	})
	got := componentsIn(deferredScripts(pages["plain"]))
	want := []string{"/" + selectSrc, "/" + calendarSrc, "/" + datetimeSrc}
	if !slices.Equal(got, want) {
		t.Errorf("an unedited layout loads %v, want all three", got)
	}
}

func TestNonTransitionLayoutsObserveBeforeStylesLoad(t *testing.T) {
	for _, shell := range []string{
		"column", "topbar", "stage",
	} {
		page := renderShellLayout(t, shell)
		script := `<script src="/static/transition.js"></script>`
		scriptAt := strings.Index(page, script)
		stylesAt := strings.Index(page, `<link rel="stylesheet"`)
		if scriptAt < 0 || stylesAt < 0 || scriptAt > stylesAt {
			t.Errorf("%s: load transition.js before styles without defer",
				shell)
		}
		if strings.Count(page, "/static/transition.js") != 1 {
			t.Errorf("%s: load transition.js exactly once", shell)
		}
	}
}

func TestNonTransitionLayoutsDoNotLoadShellJS(t *testing.T) {
	for _, shell := range []string{"column", "topbar", "stage"} {
		if strings.Contains(renderShellLayout(t, shell), "/static/shell.js") {
			t.Errorf("%s loads shell.js without phone shell navigation", shell)
		}
	}
}
