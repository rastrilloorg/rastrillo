package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/vendored"
)

const (
	primaryRule = `[rst-btn~=primary]`
	ghostRule   = `[rst-btn~=ghost]`
)

// pageSlice is the shipped tokens.css, minified as it is served, cut
// for the one page body over scriptsLayout.
func pageSlice(t *testing.T, body string) string {
	t.Helper()
	plan := planOne(t, scriptsLayout, body, everyFile)
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
	if !strings.Contains(string(whole), ghostRule) {
		t.Fatalf("tokens.css has no %s rule; pick another variant", ghostRule)
	}
	return string(slice)
}

// A page that writes only primary buttons keeps the primary variant's
// rules and drops the ghost variant's.
func TestASliceKeepsOnlyTheVariantsThePageWrites(t *testing.T) {
	slice := pageSlice(t, content(`<a rst-btn="primary" href="/new">New</a>`+
		`<summary rst-btn>Filter</summary>`+
		`<summary rst-btn{{if .Aria}} aria-label="Filter"{{end}}>F</summary>`))
	if !strings.Contains(slice, primaryRule) {
		t.Errorf("the slice drops %s, which the page writes", primaryRule)
	}
	if strings.Contains(slice, ghostRule) {
		t.Errorf("the slice keeps %s, which the page never writes",
			ghostRule)
	}
}

// A value written from data, a hook in a string argument or a needs,
// and a variant a script may write could be any value, so every
// variant's rules stay.
func TestASliceKeepsEveryVariantOfAValueItCannotRead(t *testing.T) {
	primary := `<a rst-btn="primary">New</a>`
	primary = content(primary)
	for name, body := range map[string]string{
		"from data": content(`<a rst-btn="primary">New</a>` +
			`<a rst-btn="{{.Variant}}">Edit</a>`),
		"part from data": content(`<a rst-btn="primary">New</a>` +
			`<a rst-btn="sm {{.Variant}}">Edit</a>`),
		"unquoted": content(`<a rst-btn="primary">New</a>` +
			`<a rst-btn={{.Variant}}>Edit</a>`),
		"a needs": content(`<a rst-btn="primary">New</a>` +
			`{{needs "rst-btn"}}`),
		"a string": primary[:len(primary)-len("{{end}}")] +
			`{{template "x" "rst-btn"}}{{end}}{{define "x"}}{{end}}`,
		"an entity": content(`<a rst-btn="primary&#32;ghost">New</a>`),
		"after a branch": content(`<a rst-btn="primary">New</a>` +
			`<a rst-btn{{if .Ghost}}="ghost"{{end}}>Edit</a>`),
		"from data after a branch": content(`<a rst-btn="primary">New</a>` +
			`<a rst-btn{{if .Some}}={{.Variant}}{{end}}>Edit</a>`),
	} {
		if !strings.Contains(pageSlice(t, body), ghostRule) {
			t.Errorf("%s: the slice drops %s", name, ghostRule)
		}
	}
}

// A live page links the slice of every page's hooks, and a fragment can
// carry any of the app's templates, so it keeps every variant.
func TestALivePageKeepsEveryVariant(t *testing.T) {
	layout, _ := Layout("column")
	assets := appAssets()
	pages := renderPagesWith(t, layout, map[string]string{
		"plain": content(`<a rst-btn="primary">New</a>`),
		"live": content(`<a rst-btn="primary">New</a>`) +
			`{{define "region:items"}}<p>Running</p>{{end}}`,
	}, sliced(assets, ""))
	if served := sliceServed(t, assets, pages["live"]); !strings.Contains(
		served, ghostRule) {
		t.Errorf("the live page's slice drops %s", ghostRule)
	}
}

// sliceServed is the tokens.css slice page links, as assets serve it.
func sliceServed(
	t *testing.T, assets *rastrillo.Assets, page string,
) string {
	t.Helper()
	link := linkedTokens(page)
	recorder := httptest.NewRecorder()
	assets.Handler().ServeHTTP(recorder,
		httptest.NewRequest(http.MethodGet, link, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET %s: %d", link, recorder.Code)
	}
	return recorder.Body.String()
}

// A script the page's templates load that is not Rastrillo's, inline
// code, or an event handler attribute may write any value, so such a
// page keeps every variant of every hook.
func TestAPageWithItsOwnScriptKeepsEveryVariant(t *testing.T) {
	primary := `<button rst-btn="primary">Go</button>`
	for name, script := range map[string]string{
		"an app script": `<script src="/static/app.js"></script>`,
		"an app script through asset": `<script defer ` +
			`src="{{asset "static/app.js"}}"></script>`,
		"an app script from data": `<script src="{{.Script}}"></script>`,
		"inline code": `<script>document.querySelector("button")` +
			`.setAttribute("rst-btn", "ghost")</script>`,
		"a module": `<script type="module">import "/app.js"</script>`,
		"a handler": `<button onclick="this.setAttribute('rst-btn', ` +
			`'ghost')">Swap</button>`,
		"a javascript: link": `<a href="javascript:swap()">Swap</a>`,
	} {
		if !strings.Contains(pageSlice(t, content(primary+script)),
			ghostRule) {
			t.Errorf("%s: the slice drops %s", name, ghostRule)
		}
	}
}

// The shells' own script tags, written through asset and pageScripts,
// are Rastrillo's, and data that runs no code is not a script: a page
// in a shell still loses the variants it never writes.
func TestTheShellsOwnScriptsKeepTheCut(t *testing.T) {
	body := content(`<a rst-btn="primary">New</a>` +
		`<script type="application/ld+json">{"a": 1}</script>`)
	for _, shell := range everyShell {
		layout, _ := Layout(shell)
		assets := appAssets()
		page := renderPagesWith(t, layout, map[string]string{"page": body},
			sliced(assets, ""))["page"]
		if slice := sliceServed(t, assets, page); strings.Contains(
			slice, ghostRule) || !strings.Contains(slice, primaryRule) {
			t.Errorf("%s: the slice keeps %s or drops %s", shell,
				ghostRule, primaryRule)
		}
	}
}
