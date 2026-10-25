package ui

import (
	"html/template"
	"strings"
	"testing"
	"testing/fstest"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/locales"
)

// localeBlock is the locale block every shell but stage declares: its
// default is the switcher over the page data's Locales and Return,
// posting under the base path.
const localeBlock = `{{block "locale" .}}{{$L := localizer $}}{{template "locale-menu" dict "L" $L "Items" (opt $ "Locales") "Return" (opt $ "Return") "Action" (path $ "/_locale")}}{{end}}`

// schemeBlock follows it in the same shells: the colour-scheme switch
// over the page data's Scheme and Return, posting under the base path.
const schemeBlock = `{{block "scheme" .}}{{$L := localizer $}}{{template "scheme-menu" dict "L" $L "Scheme" (opt $ "Scheme") "Return" (opt $ "Return") "Action" (path $ "/_theme")}}{{end}}`

// A second locale turns on the /xx/ prefixes, so a shell that still
// said lang="en" dir="ltr" and rendered no switcher would leave a
// reader with no way to change language and lay an Arabic page out
// left to right. Every shell takes both from the request. stage, the
// sign-in screen's card on a backdrop, gets lang and dir but no locale
// block: its grid has rows for the card and the foot only, so a
// switcher there needs a place in tokens.css first.
func TestShellsTakeLanguageDirectionAndSwitcherFromTheRequest(t *testing.T) {
	two := []rastrillo.LocaleItem{{Code: "en", Name: "English", Href: "/en/", Current: false}, {Code: "ar", Name: "العربية", Href: "/ar/", Current: true}}
	for _, shell := range LayoutNames() {
		for name, c := range map[string]struct {
			identity string
			items    []rastrillo.LocaleItem
			html     string
			switcher bool
		}{
			"one locale":  {"en", nil, `<html lang="en" dir="ltr">`, false},
			"two locales": {"es", two, `<html lang="es" dir="ltr">`, true},
			"Arabic":      {"ar", two, `<html lang="ar" dir="rtl">`, true},
		} {
			out := renderShell(t, shell, c.identity, map[string]any{"Locales": c.items, "Return": "/x"})
			if !strings.Contains(out, c.html) {
				t.Errorf("%s/%s: want %s in\n%.300s", shell, name, c.html, out)
			}
			if got := strings.Contains(out, "rst-locale"); got != c.switcher && shell != "stage" {
				t.Errorf("%s/%s: switcher present = %v, want %v", shell, name, got, c.switcher)
			}
			if c.switcher && shell != "stage" && !strings.Contains(out, `name="return" value="/x"`) {
				t.Errorf("%s/%s: the switcher does not carry Return, so a switch would land on the locale's home", shell, name)
			}
		}
	}
}

// An app that still renders through ui.Funcs passes no localizer; its
// shell must say what it said before the shells read one.
func TestShellsWithoutALocalizerKeepEnglishLeftToRight(t *testing.T) {
	for _, shell := range LayoutNames() {
		if out := renderShellLayout(t, shell); !strings.Contains(out, `<html lang="en" dir="ltr">`) {
			t.Errorf("%s: want the old <html lang=\"en\" dir=\"ltr\"> without a localizer in\n%.300s", shell, out)
		}
	}
}

// The column shell's switcher sits at the top of the page, since a
// one-column app has no bar or rail to put it in.
func TestTheColumnSwitcherOpensThePage(t *testing.T) {
	two := []rastrillo.LocaleItem{{Code: "en", Name: "English", Href: "/en/"}, {Code: "es", Name: "Español", Href: "/es/", Current: true}}
	out := renderShell(t, "column", "es", map[string]any{"Locales": two})
	if !strings.Contains(out, "<main rst-page id=\"main\">\n<details rst-dropdown rst-locale") {
		t.Errorf("the column shell's switcher is not the first child of <main rst-page>:\n%s", out)
	}
}

// renderShell executes one shipped shell through ui.Pages, with "L"
// set to the localizer for identity, as a request would.
func renderShell(t *testing.T, shell, identity string, data map[string]any) string {
	t.Helper()
	src, ok := Layout(shell)
	if !ok {
		t.Fatalf("no %s layout", shell)
	}
	cfg, err := locales.CheckConfig("en", []string{"en", "es", "ar"})
	if err != nil {
		t.Fatal(err)
	}
	app := fstest.MapFS{
		"layout.html": {Data: src},
		"page.html":   {Data: []byte(`{{define "content"}}<p>x</p>{{end}}`)},
	}
	p, err := NewPages(PagesConfig{
		App: app, Layout: "layout.html", Pages: map[string]PageFile{"page": {Path: "page.html"}},
		Locale: cfg, LocaleFS: app,
		Funcs: template.FuncMap{
			"asset":      func(p string) string { return "/" + p },
			"icon":       func(n string) template.HTML { return template.HTML("<i>" + n + "</i>") },
			"iconAssets": func() template.HTML { return "" },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	d := map[string]any{"L": p.Localizer(identity)}
	for k, v := range data {
		d[k] = v
	}
	out, err := runPage(p, "page", d)
	if err != nil {
		t.Fatalf("%s: %v", shell, err)
	}
	return out
}
