package ui

import (
	"errors"
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
		"layout.html": {Data: []byte(`{{define "layout"}}<main>{{template "body" .}}</main>{{end}}`)},
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
	}{{"index", []string{"list"}, true}, {"index", []string{"private"}, false}, {"show", []string{"list"}, false}, {"nope", []string{"list"}, false}} {
		if got := p.HasRegions(c.page, c.names); got != c.want {
			t.Errorf("%s %v: got %v, want %v", c.page, c.names, got, c.want)
		}
	}
}
