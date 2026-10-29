package ui

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

// Pages.Render shares live.Render's order; these cases check it is
// wired to the page's own set and entry, and keeps Execute's checks.
func TestPagesRenderFollowsTheRenderOrder(t *testing.T) {
	p := regionPages(t)
	const notCalled = "not called"
	for _, c := range []struct {
		name    string
		method  string
		page    string
		status  int
		hdr     []string
		code    int
		regions string // "-" for absent
		body    string
		take    string
	}{
		{name: "plain GET", method: "GET", page: "index", code: 200, regions: "-", body: `<main><div id="notice" role="status"></div><ul id="list">hi</ul></main>`, take: "true"},
		{name: "a whole page keeps its status", method: "POST", page: "index", status: 422, code: 422, regions: "-", body: "<main>", take: "true"},
		{name: "regions the page has", method: "GET", page: "index", hdr: []string{"Rastrillo-Fragment", "list notice"}, code: 200, regions: "list notice", body: `<ul id="list">hi</ul>`, take: "true"},
		{name: "regions keep the status", method: "POST", page: "index", status: 422, hdr: []string{"Rastrillo-Fragment", "list notice"}, code: 422, regions: "list notice", body: `<ul id="list">hi</ul>`, take: "true"},
		{name: "GET for regions the page lacks", method: "GET", page: "show", hdr: []string{"Rastrillo-Fragment", "list notice"}, code: 204, regions: "", take: notCalled},
		{name: "the notice alone counts as lacking", method: "GET", page: "index", hdr: []string{"Rastrillo-Fragment", "notice"}, code: 204, regions: "", take: notCalled},
		{name: "POST for regions the page lacks", method: "POST", page: "show", status: 422, hdr: []string{"Rastrillo-Fragment", "list notice"}, code: 422, regions: "-", body: "<main>", take: "true"},
		{name: "stream refetch", method: "GET", page: "index", hdr: []string{"Rastrillo-Fragment", "list"}, code: 200, regions: "list", body: `<ul id="list">hi</ul>`, take: "false"},
		{name: "form fetch", method: "POST", page: "index", hdr: []string{"Rastrillo-Fragment", "list notice"}, code: 200, regions: "list notice", body: `<ul id="list">hi</ul><div id="notice" role="status"></div>`, take: "true"},
		{name: "a redirect to another page", method: "GET", page: "index", hdr: []string{"Rastrillo-Page", "/other"}, code: 204, regions: "-", take: notCalled},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(c.method, "/index", nil)
			for i := 0; i+1 < len(c.hdr); i += 2 {
				r.Header.Add(c.hdr[i], c.hdr[i+1])
			}
			w := httptest.NewRecorder()
			take := notCalled
			err := p.Render(w, r, c.status, c.page, func(takeFlash bool) (any, error) {
				take = map[bool]string{true: "true", false: "false"}[takeFlash]
				return regionData{Localized{L: p.Localizer("en")}, "hi"}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			res := w.Result()
			if res.StatusCode != c.code || take != c.take {
				t.Errorf("status %d, data(%s); want %d, data(%s)", res.StatusCode, take, c.code, c.take)
			}
			got, has := res.Header.Get("Rastrillo-Regions"), len(res.Header.Values("Rastrillo-Regions")) > 0
			if c.regions == "-" && has || c.regions != "-" && got != c.regions {
				t.Errorf("Rastrillo-Regions %q (present %v), want %q", got, has, c.regions)
			}
			if c.code == 204 && res.Header.Get("Rastrillo-Location") != "/index" {
				t.Errorf("Rastrillo-Location %q, want /index", res.Header.Get("Rastrillo-Location"))
			}
			if res.Header.Get("Cache-Control") != "no-store" {
				t.Errorf("Cache-Control %q, want no-store", res.Header.Get("Cache-Control"))
			}
			if !strings.Contains(w.Body.String(), c.body) || c.code == 204 && w.Body.Len() != 0 {
				t.Errorf("body %q, want it to hold %q", w.Body.String(), c.body)
			}
		})
	}
}

// tracked records whether anything reached the wire, which a recorder
// alone cannot say: its Code is 200 whether or not WriteHeader ran.
type tracked struct {
	*httptest.ResponseRecorder
	wrote bool
}

func (t *tracked) WriteHeader(code int) {
	t.wrote = true
	t.ResponseRecorder.WriteHeader(code)
}

func (t *tracked) Write(b []byte) (int, error) {
	t.wrote = true
	return t.ResponseRecorder.Write(b)
}

func TestPagesRenderErrorsWriteNothing(t *testing.T) {
	p := regionPages(t)
	boom := errors.New("no data")
	for _, c := range []struct {
		name string
		page string
		hdr  []string
		data func(bool) (any, error)
		want error
	}{
		{name: "missing localizer, whole page", page: "index", data: func(bool) (any, error) { return struct{ Name string }{"x"}, nil }, want: ErrNoLocalizer},
		{name: "missing localizer, regions", page: "index", hdr: []string{"Rastrillo-Fragment", "list"}, data: func(bool) (any, error) { return struct{ Name string }{"x"}, nil }, want: ErrNoLocalizer},
		{name: "data error", page: "index", data: func(bool) (any, error) { return nil, boom }, want: boom},
		// The page's list region reads .Name, which this data lacks.
		{name: "template error, whole page", page: "index", data: func(bool) (any, error) { return struct{ Localized }{Localized{L: p.Localizer("en")}}, nil }},
		{name: "template error, regions", page: "index", hdr: []string{"Rastrillo-Fragment", "list notice"}, data: func(bool) (any, error) { return struct{ Localized }{Localized{L: p.Localizer("en")}}, nil }},
		{name: "unknown page", page: "nope", data: func(bool) (any, error) { return regionData{Localized{L: p.Localizer("en")}, "x"}, nil }},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/index", nil)
			for i := 0; i+1 < len(c.hdr); i += 2 {
				r.Header.Add(c.hdr[i], c.hdr[i+1])
			}
			w := &tracked{ResponseRecorder: httptest.NewRecorder()}
			err := p.Render(w, r, 422, c.page, c.data)
			if err == nil || c.want != nil && !errors.Is(err, c.want) {
				t.Errorf("err %v, want %v", err, c.want)
			}
			if w.wrote || w.Body.Len() != 0 {
				t.Errorf("wrote %q; want nothing", w.Body.String())
			}
		})
	}
}
