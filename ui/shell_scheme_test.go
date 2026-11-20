package ui

import (
	"html/template"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/locales"
)

// The server paints the first frame: data-theme on <html> comes from
// the page data's Scheme, which ForRequest reads off the cookie, and
// is absent for System or no cookie so the OS decides. Every shell,
// stage included — a sign-in card is the first thing a returning
// reader sees, and it must not flash the other scheme.
func TestShellsWriteDataThemeFromTheScheme(t *testing.T) {
	for _, shell := range LayoutNames() {
		for name, c := range map[string]struct {
			data map[string]any
			html string
		}{
			"dark":      {map[string]any{"Scheme": "dark"}, `<html lang="en" dir="ltr" data-theme="dark">`},
			"light":     {map[string]any{"Scheme": "light"}, `<html lang="en" dir="ltr" data-theme="light">`},
			"system":    {map[string]any{"Scheme": ""}, `<html lang="en" dir="ltr">`},
			"no scheme": {nil, `<html lang="en" dir="ltr">`},
		} {
			out := renderShell(t, shell, "en", c.data)
			if !strings.Contains(out, c.html) {
				t.Errorf("%s/%s: want %s in\n%.300s", shell, name, c.html, out)
			}
		}
	}
}

// The scheme control sits beside the language menu in every shell that
// has one, in the same block pattern, so an app that moved or
// replaced its locale block finds the scheme block in the same place.
// Unlike the language menu it always renders: there are always three
// schemes to choose from. In an account card it comes first, as
// Appearance then Language (Fiesta's card); in column's footer it
// follows the language menu (Fiesta's layout-column).
func TestShellsOfferTheSchemeMenuBesideTheLanguageMenu(t *testing.T) {
	two := []rastrillo.LocaleItem{{Code: "en", Name: "English", Href: "/en/"}, {Code: "ga", Name: "Gaeilge", Href: "/ga/", Current: true}}
	for _, shell := range LayoutNames() {
		out := renderShell(t, shell, "en", map[string]any{"Locales": two, "Return": "/x?y=1", "Scheme": "dark"})
		has := strings.Contains(out, "rst-scheme")
		if shell == "stage" {
			if has {
				t.Errorf("stage has no chrome for a switcher; it rendered one")
			}
			continue
		}
		if !has {
			t.Errorf("%s: no scheme menu in\n%s", shell, out)
			continue
		}
		language := strings.Index(out, "rst-locale")
		scheme := strings.Index(out, "rst-scheme")
		schemeFirst := shell != "column"
		if language < 0 || (scheme < language) != schemeFirst {
			t.Errorf("%s: the scheme control is at %d and the language "+
				"menu at %d; want the scheme first %v",
				shell, scheme, language, schemeFirst)
		}
		dark := `name="scheme" value="dark">` +
			`<input type="hidden" name="return" value="/x?y=1">` +
			`<button type="submit" aria-label="Dark" aria-current="true">`
		if !strings.Contains(out, dark) {
			t.Errorf("%s: Dark is not the current choice carrying "+
				"Return:\n%s", shell, out)
		}
	}
}

// The partial itself: Fiesta's icon control, a group named Appearance
// holding one form per scheme posting to /_theme, Light, Dark and
// System in that order, each button an icon named by its choice,
// aria-current on the choice in force, and System current when no
// cookie has chosen. No dropdown, and no script anywhere.
func TestSchemeMenuPartial(t *testing.T) {
	out := render(t, "scheme-menu", map[string]any{"Return": "/orders"})
	for _, want := range []string{
		`<div rst-scheme="icons" role="group" aria-label="Appearance">` +
			"\n  <form",
		`<form method="post" action="/_theme">` +
			`<input type="hidden" name="scheme" value="light">` +
			`<input type="hidden" name="return" value="/orders">` +
			`<button type="submit" aria-label="Light"><svg`,
		`<input type="hidden" name="scheme" value="dark">` +
			`<input type="hidden" name="return" value="/orders">` +
			`<button type="submit" aria-label="Dark"><svg`,
		`<input type="hidden" name="scheme" value="system">` +
			`<input type="hidden" name="return" value="/orders">` +
			`<button type="submit" aria-label="System" ` +
			`aria-current="true"><svg`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in\n%s", want, out)
		}
	}
	gones := []string{"<details", "<summary", "rst-menu-row", "<script"}
	for _, gone := range gones {
		if strings.Contains(out, gone) {
			t.Errorf("the control carries %s; it is three buttons, works "+
				"with scripts off and sits in a row only in a card:\n%s",
				gone, out)
		}
	}
	if i, j := strings.Index(out, `value="light"`), strings.Index(out, `value="dark"`); i < 0 || j < i || strings.Index(out, `value="system"`) < j {
		t.Errorf("order is not Light, Dark, System:\n%s", out)
	}
	light := render(t, "scheme-menu", map[string]any{"Scheme": "light"})
	lightCurrent := `value="light">` +
		`<button type="submit" aria-label="Light" aria-current="true">`
	if !strings.Contains(light, lightCurrent) ||
		strings.Contains(light, `name="return"`) {
		t.Errorf("Scheme light without Return:\n%s", light)
	}
	moved := render(t, "scheme-menu", map[string]any{"Action": "/x/_theme"})
	if !strings.Contains(moved, `action="/x/_theme"`) {
		t.Errorf("Action is not honoured:\n%s", moved)
	}
}

// In an account card the control is a labelled row, as Fiesta's
// icons-in-a-card example writes it: Appearance, then the icons.
func TestTheSchemeMenuInACardIsARow(t *testing.T) {
	out := render(t, "scheme-menu",
		map[string]any{"Scheme": "dark", "Card": true})
	want := `<div rst-menu-row><span rst-menu-row-label>Appearance</span>` +
		`<div rst-scheme="icons" role="group" aria-label="Appearance">`
	if !strings.HasPrefix(out, want) ||
		!strings.HasSuffix(out, "</div></div>") {
		t.Errorf("the card's scheme row is\n%s\nwant it to open %s and "+
			"close the row after the group", out, want)
	}
	if !strings.Contains(out, `aria-label="Dark" aria-current="true"`) {
		t.Errorf("Dark is not current in the card's row:\n%s", out)
	}
}

// ForRequest carries the scheme the cookie chose, and a Return that is
// the page's real address: the locale prefix the request came in with
// is put back, so a switch that redirects back lands where the reader
// was.
func TestForRequestCarriesTheSchemeAndThePrefix(t *testing.T) {
	r := httptest.NewRequest("GET", "/items?page=2", nil)
	r.AddCookie(&http.Cookie{Name: rastrillo.SchemeCookie, Value: "dark"})
	if got := ForRequest(r); got.Scheme != "dark" || got.Return != "/items?page=2" {
		t.Errorf("ForRequest = %+v", got)
	}
	l, err := rastrillo.NewLocales([]string{"en", "fr"}, "en", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got Localized
	l.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = ForRequest(r) })).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/fr/items?page=2", nil))
	if got.Return != "/fr/items?page=2" || got.Scheme != "" {
		t.Errorf("under /fr/: ForRequest = %+v", got)
	}
}

// The whole thing with scripts off, through a real client with a
// cookie jar: the page offers the menu, the form posts, the server
// stores the choice and redirects back, and the next render paints
// the chosen scheme and marks it current. Then System clears it.
func TestTheSchemeSwitchRoundTripsWithoutScripts(t *testing.T) {
	src, _ := Layout("column")
	cfg, err := locales.CheckConfig("en", []string{"en"})
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
	mux := http.NewServeMux()
	mux.Handle("POST "+rastrillo.SchemeSwitchPath, rastrillo.SchemeSwitchHandler())
	mux.Handle("GET /", p.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := p.Execute(w, "page", ForRequest(r)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	get := func() string {
		res, err := client.Get(srv.URL + "/orders?page=2")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET: %d\n%s", res.StatusCode, b)
		}
		return string(b)
	}
	// Post exactly the form the page rendered, as a browser would.
	form := regexp.MustCompile(`<form method="post" action="([^"]+)"><input type="hidden" name="scheme" value="([^"]+)"><input type="hidden" name="return" value="([^"]+)">`)
	post := func(page, scheme string) {
		t.Helper()
		for _, m := range form.FindAllStringSubmatch(page, -1) {
			if m[2] != scheme {
				continue
			}
			req, _ := http.NewRequest("POST", srv.URL+m[1], strings.NewReader(url.Values{"scheme": {m[2]}, "return": {m[3]}}.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Sec-Fetch-Site", "same-origin")
			res, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			res.Body.Close()
			if res.Request.URL.Path != "/orders" || res.Request.URL.RawQuery != "page=2" {
				t.Fatalf("after %s the client is on %s, not back on /orders?page=2", scheme, res.Request.URL)
			}
			return
		}
		t.Fatalf("the page offers no %s form:\n%s", scheme, page)
	}
	// current is the form for scheme, carrying Return, marked current.
	current := func(scheme, label string) string {
		return `value="` + scheme + `">` +
			`<input type="hidden" name="return" value="/orders?page=2">` +
			`<button type="submit" aria-label="` + label +
			`" aria-current="true">`
	}
	first := get()
	if strings.Contains(first, "data-theme") ||
		!strings.Contains(first, current("system", "System")) {
		t.Fatalf("first visit: want no data-theme and System current:\n%s",
			first)
	}
	post(first, "dark")
	dark := get()
	if !strings.Contains(dark, `data-theme="dark"`) ||
		!strings.Contains(dark, current("dark", "Dark")) {
		t.Fatalf("after choosing Dark:\n%s", dark)
	}
	post(dark, "system")
	again := get()
	if strings.Contains(again, "data-theme") ||
		!strings.Contains(again, current("system", "System")) {
		t.Fatalf("after choosing System:\n%s", again)
	}
}
