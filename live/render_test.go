package live

import (
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// renderData is what the test pages render. Fail makes the template
// fail partway, after it has produced some output.
type renderData struct {
	Took bool
	Fail bool
}

// renderPages are two pages over one layout: index has a list region,
// show has only the layout's notice, which every page has.
func renderPages(t *testing.T) map[string]*template.Template {
	t.Helper()
	funcs := template.FuncMap{"fail": func(f bool) (string, error) {
		if f {
			return "", errors.New("boom")
		}
		return "", nil
	}}
	layout := `{{define "layout"}}<main>{{template "region:notice" .}}{{template "body" .}}{{fail .Fail}}</main>{{end}}` +
		`{{define "region:notice"}}<div id="notice">{{if .Took}}saved{{end}}</div>{{end}}`
	pages := map[string]string{
		"index": `{{define "body"}}{{template "region:list" .}}{{end}}{{define "region:list"}}<ul id="list">{{fail .Fail}}</ul>{{end}}`,
		"show":  `{{define "body"}}one{{end}}`,
	}
	out := map[string]*template.Template{}
	for name, src := range pages {
		out[name] = template.Must(template.Must(template.New("layout").Funcs(funcs).Parse(layout)).Parse(src))
	}
	return out
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

func TestRenderFollowsTheRenderOrder(t *testing.T) {
	pages := renderPages(t)
	const notCalled = "not called"
	for _, c := range []struct {
		name     string
		method   string
		page     string
		status   int
		hdr      []string
		code     int
		regions  string // Rastrillo-Regions, "-" for absent
		location bool   // Rastrillo-Location set
		body     string // substring the body holds; "" for an empty body
		take     string // what data was called with
	}{
		{name: "plain GET is the whole page", method: "GET", page: "index", code: 200, regions: "-",
			body: `<main><div id="notice">saved</div><ul id="list"></ul></main>`, take: "true"},
		{name: "a whole page keeps its status", method: "POST", page: "index", status: 422, code: 422, regions: "-",
			body: "<main>", take: "true"},
		{name: "GET for regions the page has", method: "GET", page: "index", hdr: []string{"Rastrillo-Fragment", "list notice"},
			code: 200, regions: "list notice", body: `<ul id="list"></ul><div id="notice">saved</div>`, take: "true"},
		{name: "regions keep the status", method: "POST", page: "index", status: 422, hdr: []string{"Rastrillo-Fragment", "list notice"},
			code: 422, regions: "list notice", body: `<ul id="list"></ul>`, take: "true"},
		{name: "GET for regions the page lacks navigates", method: "GET", page: "show", hdr: []string{"Rastrillo-Fragment", "list notice"},
			code: 204, regions: "", location: true, take: notCalled},
		{name: "the notice alone counts as lacking", method: "GET", page: "index", hdr: []string{"Rastrillo-Fragment", "notice"},
			code: 204, regions: "", location: true, take: notCalled},
		{name: "HEAD for regions the page lacks navigates", method: "HEAD", page: "show", hdr: []string{"Rastrillo-Fragment", "list"},
			code: 204, regions: "", location: true, take: notCalled},
		{name: "POST for regions the page lacks gets the whole page", method: "POST", page: "show", status: 422,
			hdr: []string{"Rastrillo-Fragment", "list notice"}, code: 422, regions: "-", body: `<main><div id="notice">saved</div>one`, take: "true"},
		{name: "a stream refetch leaves the flash", method: "GET", page: "index", hdr: []string{"Rastrillo-Fragment", "list"},
			code: 200, regions: "list", body: `<ul id="list"></ul>`, take: "false"},
		{name: "a form fetch takes the flash", method: "POST", page: "index", hdr: []string{"Rastrillo-Fragment", "list notice"},
			code: 200, regions: "list notice", body: "saved", take: "true"},
		{name: "a redirect to another page navigates", method: "GET", page: "index",
			hdr: []string{"Rastrillo-Fragment", "list notice", "Rastrillo-Page", "/other"}, code: 204, regions: "-", location: true, take: notCalled},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(c.method, "/index?x=1", nil)
			for i := 0; i+1 < len(c.hdr); i += 2 {
				r.Header.Add(c.hdr[i], c.hdr[i+1])
			}
			w := httptest.NewRecorder()
			take := notCalled
			err := Render(w, r, c.status, pages[c.page], "layout", func(takeFlash bool) (any, error) {
				if take != notCalled {
					t.Error("data called twice")
				}
				if takeFlash {
					take = "true"
				} else {
					take = "false"
				}
				return renderData{Took: takeFlash}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			// Result's header is the one sent with the status line, so
			// this also checks Rastrillo-Regions went out before the body.
			res := w.Result()
			if res.StatusCode != c.code {
				t.Errorf("status %d, want %d", res.StatusCode, c.code)
			}
			if take != c.take {
				t.Errorf("data called with %s, want %s", take, c.take)
			}
			got, has := res.Header.Get(headerRegions), len(res.Header.Values(headerRegions)) > 0
			if c.regions == "-" && has || c.regions != "-" && got != c.regions {
				t.Errorf("Rastrillo-Regions %q (present %v), want %q", got, has, c.regions)
			}
			if loc := res.Header.Get(headerLocation); c.location != (loc == "/index?x=1") {
				t.Errorf("Rastrillo-Location %q, want set: %v", loc, c.location)
			}
			if cc := res.Header.Get("Cache-Control"); cc != "no-store" {
				t.Errorf("Cache-Control %q, want no-store", cc)
			}
			if body := w.Body.String(); c.body == "" && body != "" || !strings.Contains(body, c.body) {
				t.Errorf("body %q, want it to hold %q", body, c.body)
			}
		})
	}
}

func TestRenderStatusZeroIsOK(t *testing.T) {
	w := httptest.NewRecorder()
	if err := Render(w, httptest.NewRequest("GET", "/", nil), 0, renderPages(t)["index"], "layout",
		func(bool) (any, error) { return renderData{}, nil }); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Errorf("got %d %q; want 200 text/html", w.Code, w.Header().Get("Content-Type"))
	}
}

// An error from data or from a template comes back with nothing on the
// wire, so the app can still send its 500.
func TestRenderErrorsWriteNothing(t *testing.T) {
	pages := renderPages(t)
	boom := errors.New("no data")
	for _, c := range []struct {
		name string
		hdr  []string
		data func(bool) (any, error)
		want error
	}{
		{name: "data error, whole page", data: func(bool) (any, error) { return nil, boom }, want: boom},
		{name: "data error, regions", hdr: []string{"Rastrillo-Fragment", "list"}, data: func(bool) (any, error) { return nil, boom }, want: boom},
		{name: "template error, whole page", data: func(bool) (any, error) { return renderData{Fail: true}, nil }},
		{name: "template error, regions", hdr: []string{"Rastrillo-Fragment", "list notice"}, data: func(bool) (any, error) { return renderData{Fail: true}, nil }},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/index", nil)
			for i := 0; i+1 < len(c.hdr); i += 2 {
				r.Header.Add(c.hdr[i], c.hdr[i+1])
			}
			w := &tracked{ResponseRecorder: httptest.NewRecorder()}
			err := Render(w, r, 422, pages["index"], "layout", c.data)
			if err == nil || c.want != nil && !errors.Is(err, c.want) {
				t.Errorf("err %v, want %v", err, c.want)
			}
			if w.wrote || w.Body.Len() != 0 {
				t.Errorf("wrote %q; want nothing", w.Body.String())
			}
		})
	}
}
