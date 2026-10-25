package ui_test

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"
	"testing/fstest"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/basepath"
	"amadan.net/rastrillo/rastrillo/basepath/basepathtest"
	"amadan.net/rastrillo/rastrillo/locales"
	"amadan.net/rastrillo/rastrillo/ui"
)

// The template func is Join with the data's Base, so a shell links
// under the base wherever its data came from, and an absolute or
// scheme-relative URL is never turned into a path on the hub.
func TestPathFunc(t *testing.T) {
	fn, ok := ui.Funcs()["path"].(func(any, string) string)
	if !ok {
		t.Fatalf("Funcs()[\"path\"] is %T, want func(any, string) string", ui.Funcs()["path"])
	}
	under := struct{ ui.Localized }{ui.Localized{Base: "/acme"}}
	for _, c := range []struct {
		name string
		data any
		p    string
		want string
	}{
		{"a path", under, "/x", "/acme/x"},
		{"the root", under, "/", "/acme"},
		{"a fragment", under, "/#nav", "/acme#nav"},
		{"a map's Base", map[string]any{"Base": "/acme"}, "/x", "/acme/x"},
		{"a map without one", map[string]any{}, "/x", "/x"},
		{"no data", nil, "/x", "/x"},
		{"an absolute URL", under, "https://h/x", "https://h/x"},
		{"empty", under, "", ""},
	} {
		if got := fn(c.data, c.p); got != c.want {
			t.Errorf("%s: path(%q) = %q, want %q", c.name, c.p, got, c.want)
		}
	}
	// basepath's own Path table, the refusals included: a copy of the
	// rule here would drift from Join's.
	for _, p := range []string{"/x", "/", "/?q", "/#a", "x", "", "https://h/x", "//h/x", "/\\h"} {
		if got, want := fn(under, p), basepath.Join("/acme", p); got != want {
			t.Errorf("path(under, %q) = %q, Join(/acme, %q) = %q", p, got, p, want)
		}
	}
}

func TestForRequestCarriesTheBase(t *testing.T) {
	var got ui.Localized
	h := basepath.Config{Origin: "https://example.com", Slug: "acme"}.Handler(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = ui.ForRequest(r)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/acme/orders", nil))
	// Return stays the app's path: the switchers hand it to a redirect,
	// and the middleware adds the base to the Location itself.
	if got.Base != "/acme" || got.Return != "/orders" {
		t.Errorf("under /acme: Base %q, Return %q; want /acme and /orders", got.Base, got.Return)
	}
	if b := ui.ForRequest(httptest.NewRequest("GET", "/orders", nil)).Base; b != "" {
		t.Errorf("without the middleware: Base %q, want \"\"", b)
	}
}

type shellPage struct {
	ui.Localized
}

// Every URL a shell emits carries the base: a stylesheet or script left
// at the root is the hub's 404, and the page renders unstyled.
func TestShellsUnderABasePathLeakNothing(t *testing.T) {
	cfg, err := locales.CheckConfig("en", []string{"en", "es"})
	if err != nil {
		t.Fatal(err)
	}
	items := []rastrillo.LocaleItem{{Code: "en", Name: "English", Href: "/acme/en/x", Current: true}, {Code: "es", Name: "Español", Href: "/acme/es/x"}}
	for _, shell := range ui.LayoutNames() {
		src, _ := ui.Layout(shell)
		app := fstest.MapFS{
			"layout.html": {Data: src},
			"page.html":   {Data: []byte(`{{define "content"}}<p>x</p>{{end}}`)},
		}
		p, err := ui.NewPages(ui.PagesConfig{
			App: app, Layout: "layout.html", Pages: map[string]ui.PageFile{"page": {Path: "page.html"}},
			Locale: cfg, LocaleFS: app,
			Funcs: template.FuncMap{
				"asset": func(name string) string {
					ext := path.Ext(name)
					return "/" + strings.TrimSuffix(name, ext) + ".0123456789abcdef" + ext
				},
				"icon":       func(n string) template.HTML { return template.HTML("<i>" + n + "</i>") },
				"iconAssets": func() template.HTML { return "" },
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		render := func(base string) string {
			var b strings.Builder
			data := shellPage{ui.Localized{L: p.Localizer("en"), Locales: items, Return: "/x", Base: base}}
			if err := p.Execute(&b, "page", data); err != nil {
				t.Fatalf("%s: %v", shell, err)
			}
			return b.String()
		}
		body := render("/acme")
		if leaks := basepathtest.Leaks([]byte(body), "/acme"); len(leaks) > 0 {
			t.Errorf("%s under /acme leaks %q", shell, leaks)
		}
		if shell == "topbar" || shell == "sidebar" || shell == "console" {
			for _, want := range []string{`href="/acme"`, `action="/acme/_theme"`} {
				if !strings.Contains(body, want) {
					t.Errorf("%s under /acme has no %s", shell, want)
				}
			}
		}
		if root := render(""); strings.Contains(root, "/acme") {
			t.Errorf("%s at the root mentions /acme:\n%s", shell, root)
		}
	}
}
