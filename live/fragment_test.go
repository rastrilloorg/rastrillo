package live

import (
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func fragReq(method, target string, hdr ...string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Add(hdr[i], hdr[i+1])
	}
	return r
}

func TestWantedIgnoresTheJobsPollHeader(t *testing.T) {
	if got, ok := Wanted(fragReq("GET", "/", "Rastrillo-Fragment", "1")); ok {
		t.Errorf(`"1" gave %v, true; the jobs poll's header is not a region request`, got)
	}
	if got, ok := Wanted(fragReq("GET", "/")); ok {
		t.Errorf("no header gave %v, true", got)
	}
	got, ok := Wanted(fragReq("GET", "/", "Rastrillo-Fragment", "notes-list notice"))
	if !ok || !slices.Equal(got, []string{"notes-list", "notice"}) {
		t.Errorf("got %v, %v; want [notes-list notice], true", got, ok)
	}
}

func TestWantedRefusesBadNames(t *testing.T) {
	for _, v := range []string{"Notes", "a/b", "region:x", "ok Bad", "-x", strings.Repeat("a", 65), "   "} {
		if got, ok := Wanted(fragReq("GET", "/", "Rastrillo-Fragment", v)); ok {
			t.Errorf("%q gave %v, true; want false", v, got)
		}
	}
}

// A header of one name repeated tens of thousands of times would
// otherwise render that region that many times from one small request.
func TestWantedListsEachNameOnce(t *testing.T) {
	got, ok := Wanted(fragReq("GET", "/", "Rastrillo-Fragment", "a b a notice b"))
	if !ok || !slices.Equal(got, []string{"a", "b", "notice"}) {
		t.Errorf("got %v, %v; want [a b notice], true", got, ok)
	}
}

func TestElsewhereOnlyForAGetFromAnotherPage(t *testing.T) {
	w := httptest.NewRecorder()
	if !Elsewhere(w, fragReq("GET", "/notes/1?x=2", "Rastrillo-Page", "/")) {
		t.Fatal("GET from another page: want true")
	}
	if w.Code != 204 || w.Header().Get("Rastrillo-Location") != "/notes/1?x=2" {
		t.Errorf("got %d, Location %q; want 204, /notes/1?x=2", w.Code, w.Header().Get("Rastrillo-Location"))
	}
	for _, m := range []string{"POST", "PUT", "DELETE"} {
		w := httptest.NewRecorder()
		if Elsewhere(w, fragReq(m, "/notes/1", "Rastrillo-Page", "/")) || w.Code != 200 || w.Header().Get("Rastrillo-Location") != "" {
			t.Errorf("%s answered; Elsewhere must never fire for it", m)
		}
	}
	w = httptest.NewRecorder()
	if Elsewhere(w, fragReq("GET", "/", "Rastrillo-Page", "/")) {
		t.Error("GET on the same page: want false")
	}
	w = httptest.NewRecorder()
	if Elsewhere(w, fragReq("GET", "/notes/1")) {
		t.Error("GET without Rastrillo-Page: want false")
	}
}

func TestNoStoreKeepsExistingVary(t *testing.T) {
	w := httptest.NewRecorder()
	w.Header().Set("Vary", "Accept-Language")
	NoStore(w)
	NoStore(w)
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control %q", got)
	}
	if got := w.Header().Values("Vary"); strings.Join(got, ",") != "Accept-Language,Rastrillo-Fragment" {
		t.Errorf("Vary %q; want Accept-Language kept and Rastrillo-Fragment once", got)
	}
	w = httptest.NewRecorder()
	w.Header().Set("Vary", "accept-language, rastrillo-fragment")
	NoStore(w)
	if got := w.Header().Values("Vary"); len(got) != 1 {
		t.Errorf("Vary %q; already listed in another case, want no second entry", got)
	}
}

func TestTakesFlash(t *testing.T) {
	for _, c := range []struct {
		hdr  string
		want bool
	}{{"", true}, {"1", true}, {"list", false}, {"list notice", true}, {"notice", true}} {
		var r *http.Request
		if c.hdr == "" {
			r = fragReq("GET", "/")
		} else {
			r = fragReq("GET", "/", "Rastrillo-Fragment", c.hdr)
		}
		if got := TakesFlash(r); got != c.want {
			t.Errorf("%q: got %v, want %v", c.hdr, got, c.want)
		}
	}
}

func regionSet(t *testing.T, src string) *template.Template {
	t.Helper()
	return template.Must(template.New("page").Parse(src))
}

func TestRegionsRenderOnlyDefinedRegionTemplates(t *testing.T) {
	tpl := regionSet(t, `{{define "region:list"}}<ul id="list">{{.}}</ul>{{end}}{{define "secret"}}SECRET{{end}}`)
	var b strings.Builder
	got, err := ExecuteRegions(&b, tpl, []string{"list", "secret"}, "x")
	if err != nil || !slices.Equal(got, []string{"list"}) || b.String() != `<ul id="list">x</ul>` {
		t.Errorf("got %v, %v, %q; want [list], nil, the list only", got, err, b.String())
	}
}

func TestRegionsWriteNothingOnError(t *testing.T) {
	tpl := template.Must(template.New("page").Funcs(template.FuncMap{
		"boom": func() (string, error) { return "", errors.New("boom") },
	}).Parse(`{{define "region:a"}}A{{end}}{{define "region:b"}}{{boom}}{{end}}`))
	var b strings.Builder
	got, err := ExecuteRegions(&b, tpl, []string{"a", "b"}, nil)
	if err == nil || b.Len() != 0 || len(got) != 0 {
		t.Errorf("got %v, %v, wrote %q; want an error and nothing written", got, err, b.String())
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestRegionsReportAWriteError(t *testing.T) {
	tpl := regionSet(t, `{{define "region:a"}}A{{end}}`)
	if _, err := ExecuteRegions(failWriter{}, tpl, []string{"a"}, nil); err == nil {
		t.Error("want the write error")
	}
}

func TestHasFindsOnlyRegionTemplates(t *testing.T) {
	tpl := regionSet(t, `{{define "region:list"}}x{{end}}{{define "secret"}}y{{end}}`)
	for _, c := range []struct {
		names []string
		want  bool
	}{{[]string{"list"}, true}, {[]string{"secret"}, false}, {[]string{"secret", "list"}, true}, {nil, false}, {[]string{"page"}, false}} {
		if got := Has(tpl, c.names); got != c.want {
			t.Errorf("%v: got %v, want %v", c.names, got, c.want)
		}
	}
}

func TestAnswerNavigatesWhenNothingRendered(t *testing.T) {
	w := httptest.NewRecorder()
	Answer(w, fragReq("GET", "/notes?q=1"), nil)
	if w.Code != 204 || w.Header().Get("Rastrillo-Location") != "/notes?q=1" {
		t.Errorf("GET: got %d, Location %q; want 204 and /notes?q=1", w.Code, w.Header().Get("Rastrillo-Location"))
	}
	w = httptest.NewRecorder()
	Answer(w, fragReq("POST", "/notes"), nil)
	if w.Code != 200 || w.Header().Get("Rastrillo-Location") != "" {
		t.Errorf("POST: got %d, Location %q; want no navigation", w.Code, w.Header().Get("Rastrillo-Location"))
	}
	if v, ok := w.Header()["Rastrillo-Regions"]; !ok || v[0] != "" {
		t.Errorf("POST: Rastrillo-Regions %q, %v; want present and empty", v, ok)
	}
	w = httptest.NewRecorder()
	Answer(w, fragReq("GET", "/notes"), []string{"list", "notice"})
	if w.Code != 200 || w.Header().Get("Rastrillo-Regions") != "list notice" {
		t.Errorf("rendered: got %d, Regions %q", w.Code, w.Header().Get("Rastrillo-Regions"))
	}
}

func TestWriteRegionsSetsTheHeaderBeforeTheBody(t *testing.T) {
	tpl := regionSet(t, `{{define "region:list"}}<ul id="list"></ul>{{end}}{{define "region:notice"}}<div id="notice"></div>{{end}}`)
	w := httptest.NewRecorder()
	if err := WriteRegions(w, fragReq("GET", "/notes"), tpl, []string{"list", "gone", "notice"}, nil); err != nil {
		t.Fatal(err)
	}
	if got := w.Result().Header.Get("Rastrillo-Regions"); got != "list notice" {
		t.Errorf("Rastrillo-Regions %q; want it on the committed response", got)
	}
	if w.Code != 200 || w.Body.String() != `<ul id="list"></ul><div id="notice"></div>` {
		t.Errorf("got %d %q", w.Code, w.Body.String())
	}
}

func TestWriteRegionsNavigatesAnEmptyGet(t *testing.T) {
	tpl := regionSet(t, `{{define "region:list"}}x{{end}}`)
	w := httptest.NewRecorder()
	if err := WriteRegions(w, fragReq("GET", "/notes?q=1"), tpl, []string{"other"}, nil); err != nil {
		t.Fatal(err)
	}
	if w.Code != 204 || w.Header().Get("Rastrillo-Location") != "/notes?q=1" || w.Body.Len() != 0 {
		t.Errorf("got %d, %q, body %q; want 204, /notes?q=1, no body", w.Code, w.Header().Get("Rastrillo-Location"), w.Body.String())
	}
}

func TestWriteRegionsDoesNotNavigateAnEmptyPost(t *testing.T) {
	tpl := regionSet(t, `{{define "region:list"}}x{{end}}`)
	w := httptest.NewRecorder()
	if err := WriteRegions(w, fragReq("POST", "/notes"), tpl, []string{"other"}, nil); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || w.Header().Get("Rastrillo-Location") != "" {
		t.Errorf("got %d, Location %q; want no 204", w.Code, w.Header().Get("Rastrillo-Location"))
	}
}

func TestWriteRegionsSendsNothingOnError(t *testing.T) {
	tpl := template.Must(template.New("page").Funcs(template.FuncMap{
		"boom": func() (string, error) { return "", errors.New("boom") },
	}).Parse(`{{define "region:a"}}A{{end}}{{define "region:b"}}{{boom}}{{end}}`))
	w := httptest.NewRecorder()
	if err := WriteRegions(w, fragReq("GET", "/"), tpl, []string{"a", "b"}, nil); err == nil {
		t.Fatal("want the error")
	}
	if w.Body.Len() != 0 || w.Header().Get("Rastrillo-Regions") != "" || w.Code != 200 || w.Flushed {
		t.Errorf("wrote %q, header %q; want nothing so the caller can send a 500", w.Body.String(), w.Header().Get("Rastrillo-Regions"))
	}
}
