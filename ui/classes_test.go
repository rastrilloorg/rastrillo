package ui

import (
	"regexp"
	"strings"
	"testing"
)

// classSelector finds a selector for a hook written as a class.
func classSelector(hook string) *regexp.Regexp {
	return regexp.MustCompile(`\.` + hook + `($|[^a-z0-9_-])`)
}

// A page that writes a hook only as an attribute cannot wear it as a
// class, so its slice keeps the attribute's selector and drops the
// class's from the rules they share.
func TestASliceDropsTheClassesAPageNeverWrites(t *testing.T) {
	slice := pageSlice(t, content(`<button rst-btn>Go</button>`))
	if !strings.Contains(slice, `[rst-btn]{`) {
		t.Errorf("the slice drops [rst-btn]{…}, which the page writes")
	}
	if classSelector("rst-btn").MatchString(slice) {
		t.Errorf("the slice keeps .rst-btn, which the page never writes")
	}
}

// A class the page's markup writes, and one a script it loads adds at
// run time, keep their selectors.
func TestASliceKeepsTheClassesAPageWears(t *testing.T) {
	for name, test := range map[string]struct{ body, hook string }{
		"written": {`<code class="rst-mono">a1</code>`, "rst-mono"},
		"written beside others": {
			`<p class="lead rst-mono" id="a">a1</p>`, "rst-mono"},
		"an option's data-rst-class": {
			`<option data-rst-class="rst-mono">a1</option>`, "rst-mono"},
		"added by select.js": {`{{template "field-select" .}}`,
			"rst-sr-only"},
	} {
		slice := pageSlice(t, content(test.body))
		if !classSelector(test.hook).MatchString(slice) {
			t.Errorf("%s: the slice drops .%s", name, test.hook)
		}
	}
}

// A class written from data, a template, a string argument or a needs
// could be any class, and a page that runs code of its own may add
// one, so such a page keeps every class's selectors.
func TestASliceKeepsEveryClassWhereThePageMayWearAny(t *testing.T) {
	button := `<button rst-btn>Go</button>`
	for name, body := range map[string]string{
		"a class from data": content(button +
			`<p class="{{.Class}}">a</p>`),
		"part of a class from data": content(button +
			`<p class="lead {{.Class}}">a</p>`),
		"a class from a branch": content(button +
			`<p class="{{if .A}}lead{{end}}">a</p>`),
		"an unquoted class from data": content(button +
			`<p class={{.Class}}>a</p>`),
		"a needs": content(button + `{{needs "rst-mono"}}`),
		"a string": content(button+`{{template "x" "rst-mono"}}`) +
			`{{define "x"}}{{end}}`,
		"an app script": content(button +
			`<script src="/static/app.js"></script>`),
		"inline code": content(button +
			`<script>document.body.className = "rst-btn"</script>`),
	} {
		if !classSelector("rst-btn").MatchString(pageSlice(t, body)) {
			t.Errorf("%s: the slice drops .rst-btn", name)
		}
	}
}

// A live page links the slice of every page's hooks, and a fragment can
// carry any of the app's templates, so it keeps every class's
// selectors.
func TestALivePageKeepsEveryClass(t *testing.T) {
	layout, _ := Layout("column")
	assets := appAssets()
	pages := renderPagesWith(t, layout, map[string]string{
		"live": content(`<button rst-btn>Go</button>`) +
			`{{define "region:items"}}<p>Running</p>{{end}}`,
	}, sliced(assets, ""))
	served := sliceServed(t, assets, pages["live"])
	if !classSelector("rst-btn").MatchString(served) {
		t.Errorf("the live page's slice drops .rst-btn")
	}
}
