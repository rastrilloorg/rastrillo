package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// dataApp writes an app's templates/ with layout and pages, and
// returns the app's directory and its layout's path.
func dataApp(t *testing.T, layout string, pages map[string]string) (
	string, string,
) {
	t.Helper()
	dir := t.TempDir()
	templates := filepath.Join(dir, "templates")
	if err := os.MkdirAll(templates, 0o755); err != nil {
		t.Fatal(err)
	}
	pages["layout.html"] = layout
	for name, body := range pages {
		err := os.WriteFile(filepath.Join(templates, name), []byte(body),
			0o644)
		if err != nil {
			t.Fatal(err)
		}
	}
	return dir, filepath.Join(templates, "layout.html")
}

const (
	oldLayout = `{{define "layout"}}<link rel="stylesheet" ` +
		`href="{{path $ pageStylesheet}}">{{block "content" .}}{{end}}` +
		`{{end}}`
	dataLayout = `{{define "layout"}}{{range pageComponentStylesheets}}` +
		`<link rel="stylesheet" href="{{path $ .}}">{{end}}` +
		`{{block "content" .}}{{end}}{{end}}`
	// salesPage calls a shared partial of the app's own, which calls
	// ui's chart-line: the chart is two calls away from the page.
	salesPage   = `{{define "content"}}{{template "sales" .}}{{end}}`
	sharedChart = `{{define "sales"}}{{template "chart-line" ` +
		`(withL .Sales (localizer $))}}{{end}}`
)

// doctor reads which templates reach a table or a chart, as ui.Pages
// does, rather than looking for a partial's name in the files.
func TestDoctorNamesALayoutThatLeavesAChartUnstyled(t *testing.T) {
	for name, test := range map[string]struct {
		layout string
		pages  map[string]string
		advise bool
	}{
		"a chart two calls away": {oldLayout, map[string]string{
			"sales.html": salesPage, "shared.html": sharedChart}, true},
		"a table in the page": {oldLayout, map[string]string{
			"orders.html": `{{define "content"}}<table rst-table>` +
				`</table>{{end}}`}, true},
		"a layout that links it": {dataLayout, map[string]string{
			"sales.html": salesPage, "shared.html": sharedChart}, false},
		// An if with no else, and a template call with no data, have
		// nil lists and pipes (CI, f82e1345: doctor --fix panicked).
		"a branch with no else": {oldLayout, map[string]string{
			"if.html": `{{define "content"}}{{if .Rows}}<table rst-table>` +
				`</table>{{end}}{{template "none"}}{{end}}` +
				`{{define "none"}}{{end}}`}, true},
		"a comment": {oldLayout, map[string]string{
			"notes.html": `{{define "content"}}{{/* chart-line */}}` +
				`{{end}}`}, false},
		// The hook is a string in a template call's argument, as page
		// planning reads it (Astra, round 2).
		"a class passed in a dict": {oldLayout, map[string]string{
			"grid.html": `{{define "content"}}{{template "grid" ` +
				`(dict "Class" "rst-table")}}{{end}}` +
				`{{define "grid"}}<table class="{{.Class}}"></table>` +
				`{{end}}`}, true},
		"a partial never called": {oldLayout, map[string]string{
			"notes.html":  `{{define "content"}}<p>Notes</p>{{end}}`,
			"shared.html": sharedChart}, false},
	} {
		dir, layout := dataApp(t, test.layout, test.pages)
		rep := &report{}
		rep.adviseOnLayout(dir, layout)
		if advised := rep.dataAdvice != ""; advised != test.advise {
			t.Errorf("%s: advised %v, want %v", name, advised, test.advise)
		}
	}
}

func TestTheDataAdviceSaysWhatToAdd(t *testing.T) {
	dir, layout := dataApp(t, oldLayout, map[string]string{
		"sales.html": salesPage, "shared.html": sharedChart})
	rep := &report{}
	rep.adviseOnLayout(dir, layout)
	var out strings.Builder
	rep.printLayoutAdvice(&out)
	for _, want := range []string{"templates/layout.html: ",
		"{{range pageComponentStylesheets}}", "rastrillo doctor -fix"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the advice %q does not say %s", out.String(), want)
		}
	}
}
