package ui

import (
	"html/template"
	"regexp"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/internal/vendored"
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

// An action that may write attributes the walk cannot read may put any
// class on an element: printed in a tag outside a value, a
// template.HTMLAttr writes whole attributes. So may a template called
// there.
func TestASliceKeepsEveryClassWhereAnActionMayWriteOne(t *testing.T) {
	mono := `<i rst-mono hidden></i>`
	for name, body := range map[string]string{
		"attributes from data": mono + `<code {{.Attrs}}>x</code>`,
		"after a value":        mono + `<code id="a" {{.Attrs}}>x</code>`,
		"after a bare name":    mono + `<code hidden {{.Attrs}}>x</code>`,
		"in a name":            mono + `<code data-{{.Name}}="a">x</code>`,
		"in an else, after a branch that left the tag": mono +
			`<p {{if .A}}>a{{else}} {{.Attrs}}>b{{end}}</p>`,
		"after an unquoted value from data": mono +
			`<code id={{.ID}} {{.Attrs}}>x</code>`,
		"after a quoted value from data": mono +
			`<code id="{{.ID}}" {{.Attrs}}>x</code>`,
		"after an unquoted value with text around data": mono +
			`<code id=x{{.ID}}y {{.Attrs}}>x</code>`,
		"after an attribute name from data": mono +
			`<code {{.Name}} {{.Attrs}}>x</code>`,
		"after a branch's unquoted value": mono +
			`<code id={{if .A}}{{.ID}}{{end}} {{.Attrs}}>x</code>`,
		"in a tag a template call leaves open": mono +
			`{{template "open" .}} {{.Attrs}}>x</code>` +
			`{{end}}{{define "open"}}<code id="a"`,
		"in a value a template call leaves open": mono +
			`<code title={{template "word" .}} {{.Attrs}}>x</code>` +
			`{{end}}{{define "word"}}{{.Word}}`,
		"a template called in a tag": mono +
			`<code {{template "attributes" .}}>x</code>` +
			`{{end}}{{define "attributes"}}id="a"`,
	} {
		slice := pageSlice(t, content(body))
		if !classSelector("rst-mono").MatchString(slice) {
			t.Errorf("%s: the slice drops .rst-mono", name)
		}
	}
}

// An action whose output cannot put a class on an element leaves the
// cut: a value of another attribute, and anything printed as text,
// which is read as text: markup built in Go declares what it shows
// with needs.
func TestAnActionThatCannotWriteAClassKeepsTheCut(t *testing.T) {
	button := `<button rst-btn>Go</button>`
	for name, body := range map[string]string{
		"another attribute's value": button + `<a href="{{.URL}}">a</a>`,
		"an app function's text":    button + `<p>{{upper "a"}}</p>`,
		"an app function's markup":  button + `<p>{{markup "a"}}</p>`,
		"data":                      button + `<p>{{.Body}}</p>`,
		"a bare attribute in a branch": button +
			`<input name="a"{{if .A}} checked{{end}}>`,
		"a value in a branch": button +
			`<input{{if .A}} value="{{.A}}"{{end}} name="a">`,
		"a translation":  button + `<p>{{T . "a"}}</p>`,
		"an icon":        button + `<p>{{icon "x"}}</p>`,
		"a variable set": button + `<p {{$a := .A}}>a</p>`,
	} {
		slice := pageSliceWith(t, content(body), template.FuncMap{
			"upper":  strings.ToUpper,
			"markup": func(string) template.HTML { return "" },
		})
		if classSelector("rst-btn").MatchString(slice) {
			t.Errorf("%s: the slice keeps .rst-btn", name)
		}
	}
}

// Markup built in Go that wears a class declares it with needs, and
// its slice keeps the class's rules.
func TestAClassMarkupBuiltInGoNeedsIsKept(t *testing.T) {
	body := content(`<button rst-btn>Go</button>{{needs "rst-mono"}}` +
		`<p>{{.Code}}</p>`)
	if !classSelector("rst-mono").MatchString(pageSlice(t, body)) {
		t.Errorf("the slice drops .rst-mono, which the page needs")
	}
}

// select.js splits an option's data-rst-class at JavaScript's \s,
// which takes a no-break space for a space, as HTML does not.
func TestADataRstClassSplitsAsSelectJSSplitsIt(t *testing.T) {
	body := "<option data-rst-class=\"rst-mono\u00a0extra\">a</option>"
	if !classSelector("rst-mono").MatchString(pageSlice(t, content(body))) {
		t.Errorf("the slice drops .rst-mono, which select.js copies")
	}
}

// pageSliceWith is pageSlice for an app with funcs.
func pageSliceWith(t *testing.T, body string, funcs template.FuncMap) string {
	t.Helper()
	pages, err := hookPagesWith(t, scriptsLayout,
		map[string]string{"page": body}, funcs)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planPage("page", pages.pages["page"], pages.entry,
		everyFile)
	if err != nil {
		t.Fatal(err)
	}
	hooks := map[string]bool{}
	for _, hook := range plan.hooks {
		hooks[hook] = true
	}
	whole, err := vendored.Minify("tokens.css", TokensCSS())
	if err != nil {
		t.Fatal(err)
	}
	slice, err := vendored.Slice(whole, hooks)
	if err != nil {
		t.Fatal(err)
	}
	return string(slice)
}

// A template that calls itself while it writes an attribute's name
// enters with a longer name each time; the walk still ends, and keeps
// every class, since the name is written by a template.
func TestARecursiveNameEndsAndKeepsEveryClass(t *testing.T) {
	body := content(`<i rst-mono hidden></i>`+
		`<div data-{{template "suffix" .}}></div>`) +
		`{{define "suffix"}}x{{if .}}{{template "suffix" .Next}}{{end}}` +
		`{{end}}`
	if !classSelector("rst-mono").MatchString(pageSlice(t, body)) {
		t.Errorf("the slice drops .rst-mono")
	}
}
