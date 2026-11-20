package ui

import (
	"html"
	"html/template"
	"maps"
	"os"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/locales"
)

// The three shells with chrome, topbar, console and sidebar, write
// Fiesta 1.4.0's phone markup: one header row that starts with the up
// link, and an account card that holds the scheme control and the
// Language row. These tests read the rendered HTML. The browser half,
// phone_shells_browser_test.go, reads the laid-out page.

// phoneShells are the layouts with a header row on a phone.
var phoneShells = []string{"topbar", "console", "sidebar"}

// threeLocales is the Language row's choices on every test page:
// Gaeilge current, so the row's name is not the page's default.
var threeLocales = []rastrillo.LocaleItem{
	{Code: "en", Name: "English", Href: "/en/"},
	{Code: "ga", Name: "Gaeilge", Href: "/ga/", Current: true},
	{Code: "ar", Name: "العربية", Href: "/ar/"},
}

// phoneShellPage renders layout through ui.Pages in identity, every
// locale Rastrillo translates being offered. defines are the page's
// own templates beside its content; data's keys sit beside L. funcs
// are the app's own, the icon seams for one (upgrade_icons_test.go).
func phoneShellPage(
	t testing.TB, layout []byte, identity, defines string,
	data map[string]any, funcs ...template.FuncMap,
) string {
	t.Helper()
	config, err := locales.CheckConfig("en", rastrillo.BaseLocales())
	if err != nil {
		t.Fatal(err)
	}
	page := `{{define "content"}}<h1>Posts</h1><p>Content.</p>{{end}}` +
		defines
	app := fstest.MapFS{
		"layout.html": {Data: layout},
		"page.html":   {Data: []byte(page)},
	}
	appFuncs := template.FuncMap{"asset": func(name string) string {
		return "/" + strings.TrimPrefix(name, "static/")
	}}
	for _, extra := range funcs {
		maps.Copy(appFuncs, extra)
	}
	pages, err := NewPages(PagesConfig{
		App: app, Layout: "layout.html",
		Pages:  map[string]PageFile{"page": {Path: "page.html"}},
		Locale: config, LocaleFS: app,
		Funcs: appFuncs,
	})
	if err != nil {
		t.Fatal(err)
	}
	full := map[string]any{"L": pages.Localizer(identity)}
	for key, value := range data {
		full[key] = value
	}
	out, err := runPage(pages, "page", full)
	if err != nil {
		t.Fatalf("rendering %s: %v", identity, err)
	}
	return out
}

func shippedLayout(t testing.TB, name string) []byte {
	t.Helper()
	source, ok := Layout(name)
	if !ok {
		t.Fatalf("no %s layout", name)
	}
	return source
}

// oldLayout is a shell as 0.37 shipped it, before one header row: the
// topbar's and the console's Menu disclosure, the console's and the
// sidebar's back strip. Apps that wrote their own layout.html from it
// still have that markup, and Fiesta still draws it, so the drives of
// that behaviour run on these copies.
func oldLayout(t testing.TB, name string) []byte {
	t.Helper()
	source, err := os.ReadFile("testdata/layouts/" + name + "-0.37.html")
	if err != nil {
		t.Fatal(err)
	}
	return source
}

var (
	upLink      = regexp.MustCompile(`<a rst-shell-up [^>]*>(?s:.*?)</a>`)
	ariaLabel   = regexp.MustCompile(`aria-label="([^"]*)"`)
	hrefOf      = regexp.MustCompile(`href="([^"]*)"`)
	svgElement  = regexp.MustCompile(`(?s)<svg.*?</svg>`)
	anyTag      = regexp.MustCompile(`<[^>]*>`)
	oldShellTag = regexp.MustCompile(
		`<[^>]*\srst-shell-(menu|back|tail|chrome)[\s=>]`)
)

// visibleLabel is an up link's text outside its icon.
func visibleLabel(link string) string {
	text := anyTag.ReplaceAllString(svgElement.ReplaceAllString(link, ""), "")
	return strings.Join(strings.Fields(html.UnescapeString(text)), " ")
}

// TestPhoneShellsWriteOneUpLinkAndNoOldChrome: each shell, on its
// index and on a content page, writes one up link carrying the page's
// up and upLabel, names its view on its root, and writes none of the
// old phone chrome: no Menu disclosure, no tail, no back strip.
func TestPhoneShellsWriteOneUpLinkAndNoOldChrome(t *testing.T) {
	for _, shell := range phoneShells {
		for _, view := range []string{"index", "page"} {
			out := phoneShellPage(t, shippedLayout(t, shell), "en",
				`{{define "view"}}`+view+`{{end}}`+
					`{{define "up"}}/posts{{end}}`+
					`{{define "upLabel"}}Posts{{end}}`,
				map[string]any{"Locales": threeLocales})
			root := `<div rst-shell-` + shell + `="` + view + `">`
			if !strings.Contains(out, root) {
				t.Errorf("%s %s: no %s", shell, view, root)
			}
			links := upLink.FindAllString(out, -1)
			if len(links) != 1 {
				t.Errorf("%s %s: %d up links; want one",
					shell, view, len(links))
				continue
			}
			href := hrefOf.FindStringSubmatch(links[0])
			label := visibleLabel(links[0])
			if href == nil || href[1] != "/posts" || label != "Posts" ||
				!strings.Contains(links[0], `rel="up"`) {
				t.Errorf("%s %s: the up link is %s; want href /posts, "+
					"rel up and the label Posts", shell, view, links[0])
			}
			if old := oldShellTag.FindAllString(out, -1); len(old) > 0 {
				t.Errorf("%s %s: still writes the old phone chrome: %q",
					shell, view, old)
			}
		}
	}
}

// TestPhoneShellsDefaultTheirWayUp: a page that defines neither up nor
// upLabel still has a way back. The topbar's goes to its Menu page,
// /menu, and says Menu. The console's and the sidebar's go to the
// index at / and say Sections, as their back strip did.
func TestPhoneShellsDefaultTheirWayUp(t *testing.T) {
	for shell, want := range map[string]struct{ href, label string }{
		"topbar":  {"/menu", defaultT("rastrillo.ui.shell_menu")},
		"console": {"/", defaultT("rastrillo.ui.shell_up_label")},
		"sidebar": {"/", defaultT("rastrillo.ui.shell_up_label")},
	} {
		out := phoneShellPage(t, shippedLayout(t, shell), "en", "", nil)
		link := upLink.FindString(out)
		href := hrefOf.FindStringSubmatch(link)
		if href == nil || href[1] != want.href ||
			visibleLabel(link) != want.label {
			t.Errorf("%s: the default up link is %q; "+
				"want href %s and the label %s",
				shell, link, want.href, want.label)
		}
		root := `<div rst-shell-` + shell + `="page">`
		if !strings.Contains(out, root) {
			t.Errorf("%s: a page with no view is not a content page (%s)",
				shell, root)
		}
	}
}

// TestEveryUpLinksNameHoldsItsLabel: the up link's accessible name
// says it goes back and contains the label it shows (WCAG 2.5.3, label
// in name), in every language Rastrillo translates, with the shell's
// default label and with an app's own. A translation that inflected
// the label inside the name would fail here ("Разделы" shown, "Назад к
// разделам" spoken), which no English-only test can see.
func TestEveryUpLinksNameHoldsItsLabel(t *testing.T) {
	words := func(text string) string {
		return strings.ToLower(strings.Join(strings.Fields(text), " "))
	}
	ownLabel := `{{define "upLabel"}}Posts{{end}}`
	for _, shell := range phoneShells {
		for _, identity := range rastrillo.BaseLocales() {
			for _, defines := range []string{"", ownLabel} {
				out := phoneShellPage(t, shippedLayout(t, shell),
					identity, defines, nil)
				link := upLink.FindString(out)
				named := ariaLabel.FindStringSubmatch(link)
				label := words(visibleLabel(link))
				spoken := ""
				if named != nil {
					spoken = words(html.UnescapeString(named[1]))
				}
				switch {
				case link == "" || label == "":
					t.Errorf("%s %s: no up link with a visible label "+
						"in\n%.600s", shell, identity, out)
				case named == nil:
					t.Errorf("%s %s: the up link %q has no aria-label "+
						"to say it goes back", shell, identity, link)
				case !strings.Contains(spoken, label):
					t.Errorf("%s %s: the up link shows %q and is named "+
						"%q; the name must contain the label",
						shell, identity, label, named[1])
				case spoken == label:
					t.Errorf("%s %s: the up link is named %q, its label "+
						"alone; the name says it goes back",
						shell, identity, named[1])
				}
			}
		}
	}
}

// TestAnOldLayoutGetsTheIconControl: an app's own layout from 0.37,
// which calls scheme-menu with no option, now shows the three icons
// where its dropdown of words was (the design's call 1: no Dropdown
// option). The 0.37 layouts are kept as they shipped.
func TestAnOldLayoutGetsTheIconControl(t *testing.T) {
	const dark = `<button type="submit" aria-label="Dark" aria-current="true">`
	for _, shell := range phoneShells {
		path := "testdata/layouts/" + shell + "-0.37.html"
		out := phoneShellPage(t, oldLayout(t, shell), "en", "",
			map[string]any{"Locales": threeLocales, "Scheme": "dark"})
		if strings.Contains(out, `<details rst-dropdown rst-scheme`) {
			t.Errorf("%s: still renders the scheme dropdown of words", path)
		}
		icons := strings.Index(out, `<div rst-scheme="icons" role="group"`)
		locale := strings.Index(out, `<details rst-dropdown rst-locale`)
		if icons < 0 || locale < 0 || icons < locale {
			t.Errorf("%s: the icon control is at %d, the language menu at "+
				"%d; want the icons after the language menu, where the "+
				"dropdown was", path, icons, locale)
		}
		if !strings.Contains(out, dark) {
			t.Errorf("%s: Dark is not the current icon", path)
		}
		if strings.Contains(out, `popovertarget`) {
			t.Errorf("%s: an old layout's language menu became the card's "+
				"sheet; it has no card", path)
		}
	}
}
