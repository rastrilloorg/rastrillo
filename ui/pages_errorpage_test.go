package ui

import (
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
)

// A panic is recovered outside the locale middleware and the Pages
// middleware, and handlers call ErrorPage directly; both must still
// render the error page in the request's language, and strictly.
func TestPagesErrorPagesFindTheRequestLanguage(t *testing.T) {
	p := strictPages(t) // offers en and es; has an "errors" page using ui's error-page partial
	errorPage := func(w http.ResponseWriter, r *http.Request, status int, ref string) {
		w.WriteHeader(status)
		if err := p.Execute(w, "errors", map[string]any{"L": p.LocalizerFor(r), "Status": status, "Ref": ref}); err != nil {
			t.Errorf("error page: %v", err)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /boom", func(http.ResponseWriter, *http.Request) { panic("boom") })
	mux.HandleFunc("GET /fail", func(w http.ResponseWriter, r *http.Request) { errorPage(w, r, 500, "r1") })
	h, closeFn, err := rastrillo.Handler(rastrillo.Options{Locales: []string{"en", "es"}, Mux: mux, ErrorPage: errorPage,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer closeFn()
	es := rastrillo.BaseCatalogs()["es"]["rastrillo.ui.error_500_title"] // the title error-page renders for a 500
	if es == "" || es == rastrillo.BaseCatalog()["rastrillo.ui.error_500_title"] {
		t.Fatalf("the Spanish 500 title %q cannot tell Spanish from English", es)
	}
	for _, c := range []struct {
		path   string
		header map[string]string
	}{
		{"/es/boom", nil},
		{"/boom", map[string]string{"Accept-Language": "es"}},
		{"/boom", map[string]string{"Cookie": rastrillo.LocaleCookie + "=es"}},
		{"/es/fail", nil},
	} {
		r := httptest.NewRequest("GET", c.path, nil)
		for k, v := range c.header {
			r.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != 500 || !strings.Contains(rec.Body.String(), template.HTMLEscapeString(es)) {
			t.Errorf("%s %v: %d, body lacks the Spanish error title:\n%s", c.path, c.header, rec.Code, rec.Body.String())
		}
	}
}
