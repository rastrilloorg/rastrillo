package tmplescape_test

import (
	"bytes"
	"errors"
	htmltemplate "html/template"
	"io"
	"io/fs"
	"os"
	"path"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"text/template"
	"text/template/parse"

	"amadan.net/rastrillo/rastrillo/internal/tmplescape"
	"amadan.net/rastrillo/rastrillo/tmpl/tmplrt"
	"amadan.net/rastrillo/rastrillo/ui"
)

// The fork is a copy of one Go release's html/template. A toolchain from
// another minor release may escape differently, and nothing else would
// notice: the stdlib fallback would follow the new rules while compiled
// pages kept the old ones.
func TestForkMatchesToolchain(t *testing.T) {
	got := minorVersion(runtime.Version())
	want := minorVersion(tmplescape.GoVersion)
	if got != want {
		t.Fatalf("internal/tmplescape and internal/htmlctx are forked "+
			"from html/template in %s, "+
			"but this toolchain is %s. Re-sync them from the new "+
			"toolchain's src/html/template: "+
			"escape.go into internal/tmplescape; context.go, "+
			"transition.go, error.go, attr.go, "+
			"the contentType constants of content.go, the CSS and JS "+
			"helpers of css.go and js.go "+
			"that transition.go calls, stripTags from html.go, "+
			"delimEnds from escape.go, and "+
			"state_string.go, delim_string.go, "+
			"element_string.go, attr_string.go, urlpart_string.go and "+
			"jsctx_string.go into "+
			"internal/htmlctx. Then set GoVersion in "+
			"internal/tmplescape/version.go.",
			tmplescape.GoVersion, runtime.Version())
	}
}

// minorVersion turns "go1.26.10" (or "go1.26.10 X:boringcrypto") into
// "go1.26".
func minorVersion(version string) string {
	version, _, _ = strings.Cut(version, " ")
	if major := strings.IndexByte(version, '.'); major >= 0 {
		rest := version[major+1:]
		if minor := strings.IndexByte(rest, '.'); minor >= 0 {
			return version[:major+1+minor]
		}
	}
	return version
}

// escapeContexts are Task 1's contexts (tmpl/tmplrt/escape_test.go): one
// action in each place html/template picks a different escaper chain.
var escapeContexts = []struct{ name, src string }{
	{"text", `<p>{{.}}</p>`},
	{"attr", `<p title="{{.}}">`},
	{"attr unquoted", `<p title={{.}}>`},
	{"href", `<a href="{{.}}">`},
	{"href query", `<a href="/x?q={{.}}">`},
	{"srcset", `<img srcset="{{.}}">`},
	{"js value", `<script>var x = {{.}};</script>`},
	{"js string", `<script>var x = "{{.}}";</script>`},
	{"js template literal", "<script>var x = `{{.}}`;</script>"},
	{"js regexp", `<script>var r = /{{.}}/;</script>`},
	{"js in attr", `<p onclick="f({{.}})">`},
	{"css value", `<style>p { color: {{.}} }</style>`},
	{"css string", `<style>p { font-family: "{{.}}" }</style>`},
	{"css url", `<style>p { background: url("{{.}}") }</style>`},
	{"style attr", `<p style="color: {{.}}">`},
	{"title", `<title>{{.}}</title>`},
	{"textarea", `<textarea>{{.}}</textarea>`},
	{"attr name", `<p {{.}}="x">`},
	{"html comment", `<!-- {{.}} -->`},
}

type (
	greeter    struct{ name string }
	failure    struct{}
	plain      struct{ n int }
	filter     string
	namedStr   string
	namedInt   int
	htmlHolder struct{ h htmltemplate.HTML }
)

func (who greeter) String() string { return "hello <" + who.name + ">" }
func (failure) Error() string      { return `failed: "<x>"` }
func (text namedStr) String() string {
	return "named:" + string(text)
}
func (holder htmlHolder) String() string { return string(holder.h) }

// escapeInputs are Task 1's input values: every kind of value an escaper
// treats differently.
var escapeInputs = func() []any {
	text := "<ptr> & 'quoted'"
	markup := htmltemplate.HTML("<b>ptr</b>")
	var nilText *string
	textPointer := &text
	return []any{
		"", "<a href=\"x\">&'</a>", "javascript:alert(1)", " \t\n", " ",
		"</script>",
		nil, 42, 3.5, true,
		htmltemplate.HTML("<b>x</b>"),
		htmltemplate.HTML(
			`<i title="a>b">&amp; ok</i> <script>x</script>`),
		htmltemplate.URL("javascript:x"), htmltemplate.JS("a+b"),
		htmltemplate.JSStr(`a\x20b`),
		htmltemplate.CSS("red"), htmltemplate.HTMLAttr("x=1"),
		htmltemplate.Srcset("a.png 1x"),
		greeter{"world"}, &greeter{"ptr"}, failure{},
		filter("expression(x)"),
		namedStr("n"), namedInt(7), plain{3}, []string{"a", "<b>"},
		map[string]int{"a": 1},
		&text, &textPointer, nilText, &markup, (*plain)(nil),
		(*greeter)(nil), htmlHolder{"<b>x</b>"},
		"café   ﷐ \xff",
	}
}()

type escapeCase struct {
	name string
	// src is parsed as the template "page", the entry.
	src string
	// inputs default to escapeInputs.
	inputs []any
	// code is the ErrorCode html/template fails "page" with, or OK.
	code htmltemplate.ErrorCode
}

func escapeCases() []escapeCase {
	var cases []escapeCase
	for _, context := range escapeContexts {
		cases = append(cases, escapeCase{
			name: "context " + context.name, src: context.src,
		})
	}
	return append(cases, escapeSetCases()...)
}

// escapeSetCases are the cases that need more than one action: templates
// that call each other, branches, loops and predefined escapers.
func escapeSetCases() []escapeCase {
	list := []any{
		[]string{"<a>", "b&", `"c"`}, []string{}, []string{"javascript:x"},
	}
	return []escapeCase{
		// topbar.html:2's shape: the block is a derived template, escaped
		// as RCDATA, which html/template's API cannot reach.
		{name: "block in title",
			src: `<title>{{block "title" .}}Hello {{.}}{{end}}</title>` +
				`<p>{{.}}</p>`},
		// "p" sorts before "page", so an escaper that escaped every template
		// on one shared set in name order would derive p's attribute and
		// script copies from the already-escaped p.
		{name: "partial in text, attr and script",
			src: `{{define "p"}}{{.}}{{end}}<p>{{template "p" .}}</p>` +
				`<a title="{{template "p" .}}" ` +
				`href="/x?q={{template "p" .}}">x</a>` +
				`<script>var x = {{template "p" .}};</script>`},
		{name: "partial in attr, alone",
			src: `{{define "z"}}{{.}}{{end}}` +
				`<a title="{{template "z" .}}">x</a>`},
		{name: "recursion", inputs: list,
			src: `{{define "list"}}{{if .}}<li>{{index . 0}}</li>` +
				`{{template "list" (slice . 1)}}{{end}}{{end}}` +
				`<ul>{{template "list" .}}</ul>`},
		{name: "recursion in attr", inputs: list,
			src: `{{define "r"}}{{if .}}{{index . 0}} ` +
				`{{template "r" (slice . 1)}}{{end}}{{end}}` +
				`<p title="{{template "r" .}}">{{template "r" .}}</p>` +
				`<a href="{{template "r" .}}">x</a>`},
		{name: "recursion changing context", inputs: list,
			code: htmltemplate.ErrOutputContext,
			src: `{{define "r"}}{{template "r" .}}<p title="{{end}}` +
				`{{template "r" .}}`},
		{name: "branches end in different contexts",
			code: htmltemplate.ErrBranchEnd,
			src:  `{{if .}}<p title="{{end}}x">`},
		{name: "branches join", src: `<p title={{if .}}{{.}}{{end}}>`},
		{name: "ends in attr", code: htmltemplate.ErrEndContext,
			src: `<a href="{{.}}`},
		{name: "no such template", code: htmltemplate.ErrNoSuchTemplate,
			src: `{{template "missing" .}}`},
		{name: "ambiguous url", code: htmltemplate.ErrAmbigContext,
			src: `<a href="{{if .}}/x?{{end}}{{.}}">`},
		{name: "bad html", code: htmltemplate.ErrBadHTML,
			src: `<a title=x'{{.}}>`},
		{name: "range break continue",
			inputs: []any{
				[]string{"a", "b", "<c>", "d", "e"}, []string{},
			},
			src: `{{range .}}{{if eq . "b"}}{{continue}}{{end}}` +
				`{{if eq . "d"}}{{break}}{{end}}` +
				`<i title="{{.}}">{{.}}</i>{{end}}`},
		{name: "range break in attr", inputs: []any{[]string{"a"}},
			code: htmltemplate.ErrBranchEnd,
			src: `{{range .}}<a href="{{if .}}{{break}}{{end}}x">` +
				`{{end}}`},
		{name: "range reentry", inputs: []any{[]string{"a"}},
			code: htmltemplate.ErrBranchEnd,
			src:  `{{range .}}<p title="{{.}}{{end}}">`},
		{name: "html in pipeline",
			src: `<p>{{. | html}}</p><a title="{{. | html}}">x</a>`},
		{name: "html with args",
			src: `<p>{{html . "&"}}</p><textarea>{{html .}}</textarea>`},
		{name: "urlquery in pipeline",
			src: `<a href="/x?q={{. | urlquery}}">x</a>`},
		{name: "urlquery with args",
			src: `<a href="/x?q={{urlquery . "&"}}">x</a>` +
				`<p>{{urlquery .}}</p>`},
		{name: "html in unquoted attr",
			code: htmltemplate.ErrPredefinedEscaper,
			src:  `<a title={{html .}}>x</a>`},
		{name: "html not last", code: htmltemplate.ErrPredefinedEscaper,
			src: `<p>{{html . | print}}</p>`},
		{name: "text rewritten",
			src: "<script>var a = 1; /* c {{/* x */}} */ // d\n" +
				"var b = {{.}}; '<!--' </script><p>a < b</p>"},
		// The app's own template has the name escaping would give p's
		// derived copy for <title>, so html/template uses it as that copy.
		{name: "template named like a derived one", src: collisionSrc},
		{name: "meta refresh url", src: metaRefreshSrc},
		{name: "local variables",
			src: `{{$x := .}}{{with $x}}<a href="{{.}}">{{$x}}</a>{{end}}`},
	}
}

const metaRefreshSrc = `<meta http-equiv="refresh" content="0; url={{.}}">`

const collisionSrc = `{{define "p"}}{{.}}{{end}}` +
	`{{define "p$htmltemplate_stateRCDATA_elementTitle"}}[{{.}}]{{end}}` +
	`<title>{{template "p" .}}</title><p>{{template "p" .}}</p>`

// TestEscapeAllTemplateNamedLikeDerived is the regression for an app
// template whose name is one escaping derives. html/template escapes it
// as the derived copy for one entry and as itself for another, and both
// run. Merging every entry's trees into one map made this an error.
func TestEscapeAllTemplateNamedLikeDerived(t *testing.T) {
	const name = "p$htmltemplate_stateRCDATA_elementTitle"
	trees := parseSkippingFuncs(
		t, map[string]string{"page": collisionSrc}, []string{"page"})
	results := tmplescape.EscapeAll(trees, stdlibDefaults)
	for entry, result := range results {
		if result.Err != nil {
			t.Fatalf("%s: %v", entry, result.Err)
		}
	}
	asDerived := results["page"].Trees[name]
	asItself := results[name].Trees[name]
	if asDerived == nil || asItself == nil {
		t.Fatalf("%s missing: as derived %v, as itself %v",
			name, asDerived, asItself)
	}
	derivedIsRCDATA := strings.Contains(
		asDerived.Root.String(), "_html_template_rcdataescaper")
	itselfIsHTML := strings.Contains(
		asItself.Root.String(), "_html_template_htmlescaper")
	if !derivedIsRCDATA || !itselfIsHTML {
		t.Fatalf("%s escaped as\n derived %s\n itself %s",
			name, asDerived.Root, asItself.Root)
	}
}

// TestEscapeAllMetaContentURLEscape holds Options.MetaContentURLEscape to
// html/template under each GODEBUG htmlmetacontenturlescape setting,
// which html/template reads when it escapes.
func TestEscapeAllMetaContentURLEscape(t *testing.T) {
	src := metaRefreshSrc
	outputs := map[bool]string{}
	for _, setting := range []struct {
		godebug string
		opts    tmplescape.Options
	}{
		{"", tmplescape.Options{MetaContentURLEscape: true}},
		{
			"htmlmetacontenturlescape=1",
			tmplescape.Options{MetaContentURLEscape: true},
		},
		{
			"htmlmetacontenturlescape=0",
			tmplescape.Options{MetaContentURLEscape: false},
		},
	} {
		t.Run("GODEBUG="+setting.godebug, func(t *testing.T) {
			t.Setenv("GODEBUG", setting.godebug)
			templates := &set{
				t: t,
				trees: parseSkippingFuncs(t,
					map[string]string{"page": src}, []string{"page"}),
				stdlib: func() *htmltemplate.Template {
					return htmltemplate.Must(
						htmltemplate.New("page").Parse(src))
				},
			}
			results := tmplescape.EscapeAll(templates.trees, setting.opts)
			for _, input := range escapeInputs {
				templates.compareOutput(results, "page", input)
			}
			templates.compareTrees(results)
			out, err := executeEscaped(
				results["page"].Trees, nil, "page", "javascript:alert(1)")
			if err != nil {
				t.Fatal(err)
			}
			outputs[setting.opts.MetaContentURLEscape] = out
		})
	}
	// Without this the cases above could all pass with GODEBUG ignored.
	if outputs[true] == outputs[false] {
		t.Fatalf("both settings wrote %q", outputs[true])
	}
}

// stdlibDefaults are the Options for a process with no GODEBUG settings.
var stdlibDefaults = tmplescape.Options{MetaContentURLEscape: true}

// TestEscapeAllMatchesStdlib holds the fork to html/template: the escaped
// trees, run by text/template with tmplrt's escapers, must write exactly
// what html/template writes, and fail where it fails.
func TestEscapeAllMatchesStdlib(t *testing.T) {
	for _, tc := range escapeCases() {
		t.Run(tc.name, func(t *testing.T) {
			templates := &set{
				t: t,
				trees: parseSkippingFuncs(t,
					map[string]string{"page": tc.src}, []string{"page"}),
				stdlib: func() *htmltemplate.Template {
					return htmltemplate.Must(
						htmltemplate.New("page").Parse(tc.src))
				},
			}
			results := tmplescape.EscapeAll(templates.trees, stdlibDefaults)
			if tc.code != htmltemplate.OK {
				requireErrorCode(t, results["page"].Err, tc.code)
			}
			inputs := tc.inputs
			if inputs == nil {
				inputs = escapeInputs
			}
			for _, input := range inputs {
				templates.compareOutput(results, "page", input)
			}
			templates.compareTrees(results)
		})
	}
}

func requireErrorCode(
	t *testing.T, err error, code htmltemplate.ErrorCode,
) {
	t.Helper()
	var escapeErr *tmplescape.Error
	if !errors.As(err, &escapeErr) || int(escapeErr.ErrorCode) != int(code) {
		t.Fatalf("EscapeAll error for page = %v, want code %d", err, code)
	}
}

// uiSources reads the shipped partials, layouts and fixture pages, keyed
// the way ui.Pages assembles them. It returns the sources, the layout
// names, the page names and the partial file names.
func uiSources(t *testing.T) (sources map[string]string, layouts,
	pages, partials []string) {
	t.Helper()
	pagesFS := os.DirFS("../../ui/testdata/pages")
	sources = map[string]string{}
	partials, err := fs.Glob(ui.Templates(), "*.html")
	if err != nil || len(partials) == 0 {
		t.Fatalf("ui partials: %v %v", partials, err)
	}
	for _, partial := range partials {
		content, err := fs.ReadFile(ui.Templates(), partial)
		if err != nil {
			t.Fatal(err)
		}
		sources["partials/"+partial] = string(content)
	}
	for _, name := range ui.LayoutNames() {
		content, _ := ui.Layout(name)
		sources["layouts/"+name+".html"] = string(content)
		layouts = append(layouts, "layouts/"+name+".html")
	}
	err = fs.WalkDir(pagesFS, ".",
		func(file string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || path.Ext(file) != ".html" {
				return err
			}
			content, err := fs.ReadFile(pagesFS, file)
			sources["pages/"+file] = string(content)
			switch {
			case path.Base(file) == "layout.html":
				layouts = append(layouts, "pages/"+file)
			case file != "who.html":
				pages = append(pages, "pages/"+file)
			}
			return err
		})
	if err != nil {
		t.Fatal(err)
	}
	return sources, layouts, pages, partials
}

// TestEscapeAllMatchesStdlibOnUI runs the same comparison over ui's own
// sets: the shipped partials with each layout and each fixture page, as
// ui.Pages assembles them. These are the sets the compiler meets first,
// and their layouts call blocks from <title> and attributes.
func TestEscapeAllMatchesStdlibOnUI(t *testing.T) {
	sources, layouts, pages, partials := uiSources(t)
	funcs := ui.Funcs()
	funcs["asset"] = func(name string) string { return "/static/" + name }

	// A set whose layout fails on nil data compares only errors. Most must
	// render, or this test proves nothing about output.
	var ran, rendered atomic.Int32
	t.Cleanup(func() {
		count, total := rendered.Load(), ran.Load()
		if !t.Failed() && count < total/2 {
			t.Errorf("only %d of %d sets rendered layout", count, total)
		}
	})
	for _, layout := range layouts {
		for _, page := range pages {
			files := []string{}
			for _, partial := range partials {
				files = append(files, "partials/"+partial)
			}
			files = append(files, layout, "pages/who.html", page)
			t.Run(layout+"+"+page, func(t *testing.T) {
				t.Parallel()
				ran.Add(1)
				stdlib := parseStdlib(t, sources, files, funcs)
				templates := &set{
					t: t, funcs: funcs,
					stdlib: func() *htmltemplate.Template {
						return htmltemplate.Must(stdlib.Clone())
					},
				}
				templates.trees = parseSkippingFuncs(t, sources, files)
				templates.compareParse(stdlib)
				results := tmplescape.EscapeAll(
					templates.trees, stdlibDefaults)
				if templates.compareOutput(results, "layout", nil) {
					rendered.Add(1)
				}
				templates.compareTrees(results)
			})
		}
	}
}

// set is one template set under test, parsed twice: as parse trees for
// the fork, and as an html/template for the oracle.
type set struct {
	t     *testing.T
	trees map[string]*parse.Tree
	funcs template.FuncMap
	// stdlib returns a fresh, unexecuted html/template of the set: escaping
	// changes a set, so each comparison needs its own.
	stdlib func() *htmltemplate.Template
}

// compareOutput executes entry with data both ways and requires the same
// bytes, or an error from both. An escape error must carry the same
// ErrorCode. Error text and the partial output before an execution error
// are not compared: the compiled path does not promise them. It reports
// whether html/template rendered without error.
func (templates *set) compareOutput(
	results map[string]tmplescape.Result, entry string, data any,
) bool {
	templates.t.Helper()
	var want bytes.Buffer
	wantErr := templates.stdlib().ExecuteTemplate(&want, entry, data)

	if err := results[entry].Err; err != nil {
		if wantErr == nil {
			templates.t.Fatalf(
				"%s: EscapeAll failed (%v) where html/template wrote %q",
				entry, err, want.String())
		}
		templates.sameEscapeError(entry, err, wantErr)
		return false
	}
	got, gotErr := executeEscaped(
		results[entry].Trees, templates.funcs, entry, data)
	var htmlErr *htmltemplate.Error
	switch {
	case errors.As(wantErr, &htmlErr):
		templates.t.Fatalf(
			"%s(%#v): html/template failed to escape (%v); EscapeAll did not",
			entry, data, wantErr)
	case wantErr != nil && gotErr == nil:
		templates.t.Fatalf(
			"%s(%#v): html/template failed (%v); escaped trees wrote %q",
			entry, data, wantErr, got)
	case wantErr == nil && gotErr != nil:
		templates.t.Fatalf(
			"%s(%#v): escaped trees failed (%v); html/template wrote %q",
			entry, data, gotErr, want.String())
	case wantErr == nil && got != want.String():
		templates.t.Fatalf("%s(%#v):\n got %q\nwant %q",
			entry, data, got, want.String())
	}
	return wantErr == nil
}

// compareTrees checks every template html/template's API can reach: run
// as its own entry in a fresh set, its escaped tree must print as the
// fork's, and it must fail to escape where the fork's does. Derived
// templates are not reachable this way; compareOutput covers them.
func (templates *set) compareTrees(results map[string]tmplescape.Result) {
	templates.t.Helper()
	for _, name := range sortedNames(templates.trees) {
		oracle := templates.stdlib()
		err := oracle.ExecuteTemplate(io.Discard, name, nil)
		var htmlErr *htmltemplate.Error
		isEscapeErr := errors.As(err, &htmlErr) ||
			(err != nil && strings.Contains(err.Error(), "incomplete"))
		if result := results[name]; result.Err != nil {
			if err == nil {
				templates.t.Fatalf(
					"%s: EscapeAll failed (%v); html/template escaped it",
					name, result.Err)
			}
			templates.sameEscapeError(name, result.Err, err)
			continue
		}
		if isEscapeErr {
			templates.t.Fatalf(
				"%s: html/template failed to escape (%v); EscapeAll did not",
				name, err)
		}
		tree := results[name].Trees[name]
		if tree == nil {
			templates.t.Fatalf(
				"%s: missing from EscapeAll's result", name)
		}
		got := tree.Root.String()
		want := oracle.Lookup(name).Tree.Root.String()
		if got != want {
			templates.t.Fatalf("%s: escaped tree\n got %s\nwant %s",
				name, got, want)
		}
	}
}

func (templates *set) sameEscapeError(name string, got, want error) {
	templates.t.Helper()
	var gotErr *tmplescape.Error
	var wantErr *htmltemplate.Error
	gotCode, wantCode := -1, -1
	if errors.As(got, &gotErr) {
		gotCode = int(gotErr.ErrorCode)
	}
	if errors.As(want, &wantErr) {
		wantCode = int(wantErr.ErrorCode)
	}
	if gotCode != wantCode {
		templates.t.Fatalf(
			"%s: EscapeAll error %v (code %d), html/template %v (code %d)",
			name, got, gotCode, want, wantCode)
	}
}

// compareParse checks that parsing with SkipFuncCheck and text/template's
// redefinition rule gave the trees html/template holds before executing.
func (templates *set) compareParse(oracle *htmltemplate.Template) {
	templates.t.Helper()
	for _, expected := range oracle.Templates() {
		if expected.Tree == nil {
			continue
		}
		tree := templates.trees[expected.Name()]
		if tree == nil ||
			tree.Root.String() != expected.Tree.Root.String() {
			templates.t.Fatalf(
				"%s: parsed tree differs from html/template's",
				expected.Name())
		}
	}
	if len(oracle.Templates()) < len(templates.trees) {
		templates.t.Fatalf("parsed %d templates, html/template has %d",
			len(templates.trees), len(oracle.Templates()))
	}
}

// executeEscaped runs the fork's trees the way the compiler's oracle sees
// them: plain text/template, with tmplrt's escapers standing in for
// html/template's.
func executeEscaped(
	escaped map[string]*parse.Tree, funcs template.FuncMap, entry string,
	data any,
) (string, error) {
	text := template.New(entry).Funcs(tmplrt.Escapers).Funcs(funcs)
	for _, name := range sortedNames(escaped) {
		if _, err := text.AddParseTree(name, escaped[name]); err != nil {
			return "", err
		}
	}
	var output bytes.Buffer
	err := text.ExecuteTemplate(&output, entry, data)
	return output.String(), err
}

// parseSkippingFuncs parses files in order into one set of trees, as
// html/template's ParseFS would, but without checking that the functions
// exist. A later definition replaces an earlier one unless it is empty
// (text/template's associate).
func parseSkippingFuncs(
	t *testing.T, sources map[string]string, files []string,
) map[string]*parse.Tree {
	t.Helper()
	trees := map[string]*parse.Tree{}
	for _, file := range files {
		fileTrees := map[string]*parse.Tree{}
		parser := parse.New(path.Base(file))
		parser.Mode = parse.SkipFuncCheck
		_, err := parser.Parse(sources[file], "", "", fileTrees)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for name, tree := range fileTrees {
			if old := trees[name]; old != nil &&
				parse.IsEmptyTree(tree.Root) {
				continue
			}
			trees[name] = tree
		}
	}
	return trees
}

func parseStdlib(
	t *testing.T, sources map[string]string, files []string,
	funcs template.FuncMap,
) *htmltemplate.Template {
	t.Helper()
	oracle := htmltemplate.New("layout").Funcs(htmltemplate.FuncMap(funcs))
	for _, file := range files {
		_, err := oracle.New(path.Base(file)).Parse(sources[file])
		if err != nil {
			t.Fatalf("html/template parse %s: %v", file, err)
		}
	}
	return oracle
}

func sortedNames(trees map[string]*parse.Tree) []string {
	names := make([]string, 0, len(trees))
	for name := range trees {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
