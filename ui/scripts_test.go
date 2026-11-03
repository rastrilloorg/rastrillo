package ui

import (
	"fmt"
	"html/template"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"amadan.net/rastrillo/rastrillo/locales"
)

// scriptsLayout renders only the scripts a page loads. Its content sits
// in a branch that never runs: the partials it calls would need real
// data, and a partial in a branch that does not run still counts.
const scriptsLayout = `{{define "layout"}}{{range neededScripts}}` +
	`<script src="{{.}}"></script>{{end}}` +
	`{{if false}}{{template "content" .}}{{end}}{{end}}`

var (
	selectSrc   = "static/select.js"
	calendarSrc = "static/calendar.js"
	datetimeSrc = "static/datetime.js"
	allThree    = []string{selectSrc, calendarSrc, datetimeSrc}
)

// scriptPages builds a Pages over scriptsLayout with one page per
// entry of contents, each the body of its content define.
func scriptPages(
	t testing.TB, contents map[string]string, funcs template.FuncMap,
) (*Pages, error) {
	t.Helper()
	config, err := locales.CheckConfig("en", []string{"en"})
	if err != nil {
		t.Fatal(err)
	}
	app := fstest.MapFS{"layout.html": {Data: []byte(scriptsLayout)}}
	pages := map[string]PageFile{}
	for name, content := range contents {
		app[name+".html"] = &fstest.MapFile{Data: []byte(content)}
		pages[name] = PageFile{Path: name + ".html"}
	}
	return NewPages(PagesConfig{
		App: app, Layout: "layout.html", Pages: pages, Locale: config,
		Funcs: funcs,
	})
}

func content(body string) string {
	return `{{define "content"}}` + body + `{{end}}`
}

var scriptSrc = regexp.MustCompile(`<script src="([^"]*)">`)

// loadedScripts renders page and returns the srcs it loads, in order.
func loadedScripts(t testing.TB, pages *Pages, page string) []string {
	t.Helper()
	var out strings.Builder
	data := map[string]any{"L": pages.Localizer("en")}
	if err := pages.Execute(&out, page, data); err != nil {
		t.Fatalf("%s: %v", page, err)
	}
	var srcs []string
	for _, match := range scriptSrc.FindAllStringSubmatch(out.String(), -1) {
		srcs = append(srcs, match[1])
	}
	return srcs
}

// requireScripts builds one page from body and requires it to load
// want.
func requireScripts(t *testing.T, body string, want []string) {
	t.Helper()
	pages, err := scriptPages(t, map[string]string{"page": body}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := loadedScripts(t, pages, "page"); !slices.Equal(got, want) {
		t.Errorf("%s loads %v, want %v", body, got, want)
	}
}

func TestAPlainPageNeedsNoComponentScript(t *testing.T) {
	requireScripts(t, content(`<h1>Orders</h1>`), nil)
}

func TestASelectFieldLoadsSelectJS(t *testing.T) {
	requireScripts(t, content(`{{template "field-select" .}}`),
		[]string{selectSrc})
}

func TestADateFieldBringsTheCalendar(t *testing.T) {
	for _, partial := range []string{
		"field-date", "field-datetime", "field-time",
	} {
		requireScripts(t, content(`{{template "`+partial+`" .}}`),
			[]string{calendarSrc, datetimeSrc})
	}
}

func TestScriptsAreListedOnceInLayoutOrder(t *testing.T) {
	requireScripts(t, content(`{{template "field-date" .}}`+
		`{{template "field-select" .}}{{template "field-select" .}}`),
		allThree)
}

func TestAPartialReachedThroughAnotherCounts(t *testing.T) {
	requireScripts(t, content(`{{template "api-tokens" .}}`),
		[]string{selectSrc})
	requireScripts(t, content(`{{template "field-daterange" .}}`),
		[]string{calendarSrc, datetimeSrc})
}

func TestALivePageLoadsEveryComponentScript(t *testing.T) {
	requireScripts(t,
		content(`<h1>Jobs</h1>`)+`{{define "region:items"}}{{end}}`,
		allThree)
	requireScripts(t, content(`{{template "job-status" .}}`), allThree)
	requireScripts(t,
		content(`<h1>Jobs</h1>`)+`{{define "region:notice"}}{{end}}`,
		nil)
}

func TestEachPageKeepsItsOwnScripts(t *testing.T) {
	pages, err := scriptPages(t, map[string]string{
		"plain":  content(`<h1>Orders</h1>`),
		"select": content(`{{template "field-select" .}}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for worker := range 8 {
		group.Go(func() {
			for round := range 50 {
				page, want := "plain", []string(nil)
				if (worker+round)%2 == 0 {
					page, want = "select", []string{selectSrc}
				}
				got := loadedScripts(t, pages, page)
				if !slices.Equal(got, want) {
					t.Errorf("%s loads %v, want %v", page, got, want)
				}
			}
		})
	}
	group.Wait()
}

func TestNeedsInAPagesOwnMarkup(t *testing.T) {
	requireScripts(t, content(`{{needs "select.js"}}<select></select>`),
		[]string{selectSrc})
}

func TestNeedsRefusesAnUnknownScript(t *testing.T) {
	_, err := scriptPages(t,
		map[string]string{"orders": content(`{{needs "selct.js"}}`)}, nil)
	if err == nil || !strings.Contains(err.Error(), `"orders"`) ||
		!strings.Contains(err.Error(), "selct.js") {
		t.Errorf("NewPages: %v; want an error naming the page and selct.js",
			err)
	}
}

func TestNeedsTakesOnlyAQuotedName(t *testing.T) {
	_, err := scriptPages(t,
		map[string]string{"orders": content(`{{needs .Script}}`)}, nil)
	if err == nil || !strings.Contains(err.Error(), `"orders"`) {
		t.Errorf("NewPages: %v; want an error naming the page", err)
	}
}

func TestAnAppCannotReplaceTheScriptFuncs(t *testing.T) {
	for _, name := range []string{
		"needs", "neededScripts", "pageStylesheet", "pageScripts",
	} {
		_, err := scriptPages(t, map[string]string{"page": content(``)},
			template.FuncMap{name: func() string { return "" }})
		want := fmt.Sprintf(reservedFuncMsg, name)
		if err == nil || err.Error() != want {
			t.Errorf("Funcs %q: %v, want %q", name, err, want)
		}
	}
}

func TestFuncsOutsidePagesListEveryComponentScript(t *testing.T) {
	listed := Funcs()["neededScripts"].(func() []string)()
	if !slices.Equal(listed, allThree) {
		t.Errorf("neededScripts outside Pages lists %v, want %v",
			listed, allThree)
	}
	page := template.Must(template.New("page").Funcs(Funcs()).
		Parse(`[{{needs "select.js"}}]`))
	var out strings.Builder
	if err := page.Execute(&out, nil); err != nil || out.String() != "[]" {
		t.Errorf("needs rendered %q, %v; want nothing", out.String(), err)
	}
}

func TestAPollMarkerInAnyCaseLoadsEveryComponentScript(t *testing.T) {
	requireScripts(t, content(`<div DATA-POLL="/jobs/1"></div>`), allThree)
}

// An app's own field markup, or its own definition of a field partial,
// carries the hook but no needs: the hook alone loads the script, so
// following the changelog's layout edit never strips an enhancement.
func TestAHookInAnAppsOwnMarkupLoadsItsScript(t *testing.T) {
	requireScripts(t, content(`<select data-rst-select></select>`),
		[]string{selectSrc})
	requireScripts(t, content(`<input data-rst-date name="due">`),
		[]string{calendarSrc, datetimeSrc})
	requireScripts(t, content(`{{template "field-select" .}}`)+
		`{{define "field-select"}}<select data-rst-select></select>{{end}}`,
		[]string{selectSrc})
}

// Review round 2: a hook's name in text, or a longer attribute that
// starts with it, is not the hook.
func TestTextThatIsNotAHookLoadsNothing(t *testing.T) {
	requireScripts(t, content(`<pre>&lt;select data-rst-select&gt;</pre>`),
		nil)
	requireScripts(t,
		content(`<header data-rst-select-ceiling>Orders</header>`), nil)
}
