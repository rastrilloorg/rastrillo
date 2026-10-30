package ui

import (
	"errors"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"amadan.net/rastrillo/rastrillo/locales"
)

func regionPages(t testing.TB) *Pages {
	t.Helper()
	cfg, err := locales.CheckConfig("en", []string{"en"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewPages(PagesConfig{App: fstest.MapFS{
		"layout.html": {Data: []byte(`{{define "layout"}}<main>{{template "region:notice" .}}{{template "body" .}}</main>{{end}}{{define "region:notice"}}<div id="notice" role="status"></div>{{end}}`)},
		"index.html":  {Data: []byte(`{{define "body"}}{{template "region:list" .}}{{end}}{{define "region:list"}}<ul id="list">{{.Name}}</ul>{{end}}{{define "private"}}SECRET{{end}}`)},
		"show.html":   {Data: []byte(`{{define "body"}}one{{end}}`)},
	}, Layout: "layout.html", Pages: map[string]PageFile{"index": {Path: "index.html"}, "show": {Path: "show.html"}}, Locale: cfg})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

type regionData struct {
	Localized
	Name string
}

func TestPagesExecuteRegionsNeedsALocalizer(t *testing.T) {
	p := regionPages(t)
	var w strings.Builder
	got, err := p.ExecuteRegions(&w, "index", []string{"list"}, struct{ Name string }{"x"})
	if !errors.Is(err, ErrNoLocalizer) || w.Len() != 0 || len(got) != 0 {
		t.Errorf("got %v, %v, wrote %d bytes; want ErrNoLocalizer and nothing written", got, err, w.Len())
	}
	if _, err := p.ExecuteRegions(&w, "nope", nil, regionData{Localized{L: p.Localizer("en")}, "x"}); err == nil {
		t.Error("an unknown page: want an error")
	}
}

func TestPagesExecuteRegionsRendersFromThePagesOwnSet(t *testing.T) {
	p := regionPages(t)
	data := regionData{Localized{L: p.Localizer("en")}, "hi"}
	var w strings.Builder
	got, err := p.ExecuteRegions(&w, "index", []string{"list", "private", "layout"}, data)
	if err != nil || !slices.Equal(got, []string{"list"}) || w.String() != `<ul id="list">hi</ul>` {
		t.Errorf("index: got %v, %v, %q; want only the list region", got, err, w.String())
	}
	w.Reset()
	got, err = p.ExecuteRegions(&w, "show", []string{"list"}, data)
	if err != nil || len(got) != 0 || w.Len() != 0 {
		t.Errorf("show: got %v, %v, %q; want nothing rendered", got, err, w.String())
	}
}

func TestPagesHasRegions(t *testing.T) {
	p := regionPages(t)
	for _, c := range []struct {
		page  string
		names []string
		want  bool
	}{
		{"index", []string{"list"}, true}, {"index", []string{"private"}, false}, {"show", []string{"list"}, false}, {"nope", []string{"list"}, false},
		// Every page has the layout's notice, so it says nothing about
		// whether this is the page a form asked for.
		{"show", []string{"list", "notice"}, false}, {"show", []string{"notice"}, false}, {"index", []string{"list", "notice"}, true},
	} {
		if got := p.HasRegions(c.page, c.names); got != c.want {
			t.Errorf("%s %v: got %v, want %v", c.page, c.names, got, c.want)
		}
	}
}

func TestPagesWriteRegions(t *testing.T) {
	p := regionPages(t)
	data := regionData{Localized{L: p.Localizer("en")}, "hi"}

	w := httptest.NewRecorder()
	if err := p.WriteRegions(w, httptest.NewRequest("GET", "/notes", nil), "index", []string{"list", "private"}, data); err != nil {
		t.Fatal(err)
	}
	if got := w.Result().Header.Get("Rastrillo-Regions"); got != "list" || w.Body.String() != `<ul id="list">hi</ul>` {
		t.Errorf("Rastrillo-Regions %q, body %q; want list on the committed response", got, w.Body.String())
	}

	w = httptest.NewRecorder()
	if err := p.WriteRegions(w, httptest.NewRequest("GET", "/notes/1?x=2", nil), "show", []string{"list"}, data); err != nil {
		t.Fatal(err)
	}
	if w.Code != 204 || w.Header().Get("Rastrillo-Location") != "/notes/1?x=2" || w.Body.Len() != 0 {
		t.Errorf("empty GET: got %d, %q, body %q", w.Code, w.Header().Get("Rastrillo-Location"), w.Body.String())
	}

	w = httptest.NewRecorder()
	if err := p.WriteRegions(w, httptest.NewRequest("POST", "/notes", nil), "show", []string{"list"}, data); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || w.Header().Get("Rastrillo-Location") != "" {
		t.Errorf("empty POST: got %d, Location %q; want no 204", w.Code, w.Header().Get("Rastrillo-Location"))
	}

	w = httptest.NewRecorder()
	err := p.WriteRegions(w, httptest.NewRequest("GET", "/notes", nil), "index", []string{"list"}, struct{ Name string }{"x"})
	if !errors.Is(err, ErrNoLocalizer) || w.Body.Len() != 0 || w.Header().Get("Rastrillo-Regions") != "" {
		t.Errorf("no localizer: err %v, wrote %q", err, w.Body.String())
	}
}
