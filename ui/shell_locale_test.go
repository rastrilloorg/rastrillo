package ui

import (
	"html/template"
	"strings"
	"testing"
	"testing/fstest"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/locales"
)

// cardSchemeBlock and cardLocaleBlock are the scheme and locale blocks
// in the account card of topbar, console and sidebar, Appearance
// first: each partial writes its card row.
const (
	cardSchemeBlock = `{{block "scheme" .}}{{$L := localizer $}}` +
		`{{template "scheme-menu" dict "L" $L "Scheme" (opt $ "Scheme") ` +
		`"Return" (opt $ "Return") "Action" (path $ "/_theme") ` +
		`"Card" true}}{{end}}`
	cardLocaleBlock = `{{block "locale" .}}{{$L := localizer $}}` +
		`{{template "locale-menu" dict "L" $L "Items" (opt $ "Locales") ` +
		`"Return" (opt $ "Return") "Action" (path $ "/_locale") ` +
		`"Card" true}}{{end}}`
)

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

// The column shell's menus sit in a footer row after the content, as
// Fiesta's layout-column example has them: a one-column app has no bar
// or rail, and a dropdown on a line of its own above the title is what
// the Row Rule forbids.
func TestTheColumnMenusSitInTheFooterRow(t *testing.T) {
	two := []rastrillo.LocaleItem{
		{Code: "en", Name: "English", Href: "/en/"},
		{Code: "es", Name: "Español", Href: "/es/", Current: true},
	}
	out := renderShell(t, "column", "es", map[string]any{"Locales": two})
	inOrder := []string{
		`<main rst-page id="main">`, "<p>x</p>", "<footer rst-page-foot>",
		"<details rst-dropdown rst-locale", `<div rst-scheme="icons"`,
		"</footer>", "</main>",
	}
	from := 0
	for _, want := range inOrder {
		at := strings.Index(out[from:], want)
		if at < 0 {
			t.Fatalf("the column shell has no %q after %q:\n%s",
				want, out[:from], out)
		}
		from += at + len(want)
	}
}

// In an account card the language menu is Fiesta's Language row
// (task-A8): the label, a button that opens the list as a popover
// sheet, a <details> fallback for an engine without popover, then the
// sheet, in that order and adjacent, because Fiesta's CSS joins them
// with +. Both faces name the current language in its own lang.
func TestTheLocaleMenuInACardIsTheLanguageRow(t *testing.T) {
	out := render(t, "locale-menu", map[string]any{
		"Items": threeLocales, "Return": "/x", "Card": true,
	})
	chevron := string(rastrillo.Icon("chevron-down"))
	caret := `<span rst-caret aria-hidden="true">` + chevron + `</span>`
	face := `<span lang="ga">Gaeilge</span>` + caret
	inOrder := []string{
		`<div rst-menu-row><span rst-menu-row-label>Language</span>`,
		`<button type="button" popovertarget="rst-language">` + face +
			`</button>`,
		`<details rst-dropdown name="rst-menus"><summary>` + face +
			`</summary></details>`,
		`<div rst-locale popover id="rst-language">`,
		`<div rst-menu-row><h2>Language</h2><button type="button" ` +
			`popovertarget="rst-language" popovertargetaction="hide">` +
			`Close</button></div>`,
		`<form method="post" action="/_locale">` +
			`<input type="hidden" name="locale" value="en">` +
			`<input type="hidden" name="return" value="/x">` +
			`<button type="submit" lang="en">English</button></form>`,
		`<button type="submit" lang="ga" aria-current="true">` +
			`Gaeilge</button>`,
		`</div></div>`,
	}
	squeeze := func(text string) string {
		return strings.Join(strings.Fields(text), " ")
	}
	joined := squeeze(out)
	from := 0
	for index, want := range inOrder {
		at := strings.Index(joined[from:], squeeze(want))
		// The button, the fallback and the sheet touch: Fiesta joins
		// them with +.
		adjacent := index > 0 && index < 4
		if at < 0 || (adjacent && at != 0) {
			t.Fatalf("the Language row has no %s right after %q:\n%s",
				want, joined[:from], out)
		}
		from += at + len(squeeze(want))
	}
	if ids := strings.Count(out, `id="rst-language"`); ids != 1 {
		t.Errorf("the sheet's id is written %d times; want one", ids)
	}
	alone := render(t, "locale-menu", map[string]any{"Card": true})
	if alone != "" {
		t.Errorf("a one-locale app's card has a Language row: %q", alone)
	}
}

// Outside a card (column's footer, an old layout) the language menu is
// the dropdown it always was: nothing there is nested in a menu.
func TestTheLocaleMenuOutsideACardIsADropdown(t *testing.T) {
	out := render(t, "locale-menu", map[string]any{"Items": threeLocales})
	const dropdown = `<details rst-dropdown rst-locale name="rst-menus">`
	if !strings.HasPrefix(out, dropdown) ||
		strings.Contains(out, "popover") {
		t.Errorf("the plain language menu is\n%s", out)
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
