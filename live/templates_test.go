package live

import (
	"bytes"
	"html/template"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"amadan.net/rastrillo/rastrillo/tmpl"
)

// bothKinds parses the same sources as html/template and as tmpl, so a
// test can hold the two to one answer.
func bothKinds(
	t *testing.T,
	page string,
) (std *template.Template, compiled *tmpl.Template) {
	t.Helper()
	const layout = `{{define "layout"}}<main>` +
		`{{template "region:notice" .}}{{template "body" .}}` +
		`</main>{{end}}` +
		`{{define "region:notice"}}<div id="notice">` +
		`{{if .Took}}saved{{end}}</div>{{end}}`
	std = template.Must(
		template.Must(template.New("layout").Parse(layout)).Parse(page),
	)
	compiled = tmpl.Must(
		tmpl.Must(tmpl.New("layout").Parse(layout)).Parse(page),
	)
	return std, compiled
}

// answer is everything a client can tell about a response.
type answer struct {
	Status int
	Header http.Header
	Body   string
}

func record(
	t *testing.T,
	method string,
	hdr []string,
	status int,
	tpl Templates,
) answer {
	t.Helper()
	request := httptest.NewRequest(method, "/index?x=1", nil)
	for index := 0; index+1 < len(hdr); index += 2 {
		request.Header.Add(hdr[index], hdr[index+1])
	}
	recorder := httptest.NewRecorder()
	err := Render(
		recorder,
		request,
		status,
		tpl,
		"layout",
		func(takeFlash bool) (any, error) {
			return renderData{Took: takeFlash}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	res := recorder.Result()
	return answer{res.StatusCode, res.Header, recorder.Body.String()}
}

// fragment is the header a live request sends for the regions named.
func fragment(regions string) []string {
	return []string{"Rastrillo-Fragment", regions}
}

func TestTemplatesAcceptsBoth(t *testing.T) {
	std, compiled := bothKinds(
		t,
		`{{define "body"}}{{template "region:list" .}}{{end}}`+
			`{{define "region:list"}}<ul id="list"></ul>{{end}}`,
	)

	for _, testCase := range []struct {
		name   string
		method string
		status int
		hdr    []string
	}{
		{name: "whole page", method: "GET"},
		{name: "whole page keeps its status", method: "POST", status: 422},
		{name: "fragment", method: "GET", hdr: fragment("list notice")},
		{
			name:   "fragment keeps its status",
			method: "POST", status: 422, hdr: fragment("list notice"),
		},
		{
			name:   "missing region navigates",
			method: "GET", hdr: fragment("nope"),
		},
		{
			name:   "missing region on a POST is the whole page",
			method: "POST", status: 422, hdr: fragment("nope"),
		},
		{
			name:   "one region of two is missing",
			method: "GET", hdr: fragment("nope list"),
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			want := record(
				t,
				testCase.method,
				testCase.hdr,
				testCase.status,
				std,
			)
			got := record(
				t,
				testCase.method,
				testCase.hdr,
				testCase.status,
				compiled,
			)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("tmpl answered %+v, html/template %+v", got, want)
			}
			if want.Body == "" && want.Status != http.StatusNoContent {
				t.Errorf("the case rendered nothing: %+v", want)
			}
		})
	}
}

func TestTemplatesAcceptsBothInExecuteRegions(t *testing.T) {
	std, compiled := bothKinds(
		t,
		`{{define "body"}}{{template "region:list" .}}{{end}}`+
			`{{define "region:list"}}<ul id="list"></ul>{{end}}`,
	)
	names := []string{"list", "nope", "notice"}

	var want, got bytes.Buffer
	wantNames, err := ExecuteRegions(&want, std, names, renderData{Took: true})
	if err != nil {
		t.Fatal(err)
	}
	gotNames, err := ExecuteRegions(
		&got,
		compiled,
		names,
		renderData{Took: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotNames, wantNames) ||
		got.String() != want.String() {
		t.Errorf(
			"tmpl rendered %v %q, html/template %v %q",
			gotNames,
			got.String(),
			wantNames,
			want.String(),
		)
	}
	if !reflect.DeepEqual(wantNames, []string{"list", "notice"}) {
		t.Errorf("rendered %v, want list and notice", wantNames)
	}
	for _, tpl := range []Templates{std, compiled} {
		if !Has(tpl, []string{"nope", "list"}) ||
			Has(tpl, []string{"nope", "notice"}) {
			t.Errorf("%T: Has disagrees with the page's regions", tpl)
		}
	}
}

// A template that New made and nothing parsed into is in the set by
// name. Both kinds find it and fail when asked to run it, so an app
// does not see a different answer for the same set after moving to tmpl.
func TestAnEmptyRegionErrorsInBothKinds(t *testing.T) {
	std := template.Must(
		template.New("layout").Parse(`{{define "region:list"}}x{{end}}`),
	)
	std.New("region:empty")
	compiled := tmpl.Must(
		tmpl.New("layout").Parse(`{{define "region:list"}}x{{end}}`),
	)
	compiled.New("region:empty")

	var errs []string
	for _, tpl := range []Templates{std, compiled} {
		if !Has(tpl, []string{"empty"}) {
			t.Errorf("%T: Has missed a region that is in the set", tpl)
		}
		var buffer bytes.Buffer
		names, err := ExecuteRegions(
			&buffer,
			tpl,
			[]string{"list", "empty"},
			nil,
		)
		if err == nil || names != nil || buffer.Len() != 0 {
			t.Errorf(
				"%T: rendered %v %q, %v; want an error and nothing written",
				tpl,
				names,
				buffer.String(),
				err,
			)
			continue
		}
		errs = append(errs, err.Error())
	}
	if len(errs) == 2 && errs[0] != errs[1] {
		t.Errorf("tmpl failed with %q, html/template with %q", errs[1], errs[0])
	}
}
