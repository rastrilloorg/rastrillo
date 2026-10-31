package tmplrt_test

import (
	"bytes"
	"html/template"
	"reflect"
	"sort"
	"strings"
	"testing"
	texttemplate "text/template"
	"text/template/parse"

	"amadan.net/rastrillo/rastrillo/tmpl/tmplrt"
)

// oracle is a one-action html/template together with the escaper chain
// html/template chose for the action and the literal text around it, as
// html/template rewrote it.
type oracle struct {
	tpl           *template.Template
	before, after string
	chain         []string
}

// newOracle parses src, which must hold exactly one {{.}}, runs it once
// so html/template escapes it, and reads the escaper chain off the tree.
func newOracle(tb testing.TB, src string) *oracle {
	tb.Helper()
	tpl, err := template.New("t").Parse(src)
	if err != nil {
		tb.Fatalf("parse %q: %v", src, err)
	}
	if err := tpl.Execute(new(bytes.Buffer), "x"); err != nil {
		tb.Fatalf("execute %q: %v", src, err)
	}
	made := &oracle{tpl: tpl}
	var before, after strings.Builder
	seen := false
	for _, node := range tpl.Tree.Root.Nodes {
		switch node := node.(type) {
		case *parse.TextNode:
			if seen {
				after.Write(node.Text)
			} else {
				before.Write(node.Text)
			}
		case *parse.ActionNode:
			if seen {
				tb.Fatalf("%q: more than one action", src)
			}
			seen = true
			for _, cmd := range node.Pipe.Cmds[1:] {
				escaper := cmd.Args[0].(*parse.IdentifierNode).Ident
				made.chain = append(made.chain, escaper)
			}
		default:
			tb.Fatalf("%q: unexpected node %T", src, node)
		}
	}
	if !seen || len(made.chain) == 0 {
		tb.Fatalf("%q: no escaper chain found", src)
	}
	made.before, made.after = before.String(), after.String()
	return made
}

// stdlib renders the html/template with v.
func (oracle *oracle) stdlib(tb testing.TB, value any) string {
	tb.Helper()
	out, err := oracle.stdlibErr(value)
	if err != nil {
		tb.Fatalf("html/template execute: %v", err)
	}
	return out
}

func (oracle *oracle) stdlibErr(value any) (string, error) {
	var buffer bytes.Buffer
	err := oracle.tpl.Execute(&buffer, value)
	return buffer.String(), err
}

// ported applies the ported chain to v, the way generated code does.
func (oracle *oracle) ported(value any) string {
	var cur any = value
	for _, name := range oracle.chain {
		cur = tmplrt.Escapers[name].(func(...any) string)(cur)
	}
	return oracle.before + cur.(string) + oracle.after
}

// panics reports whether the ported chain panics on v.
func (oracle *oracle) panics(value any) (panicked bool) {
	defer func() { panicked = recover() != nil }()
	oracle.ported(value)
	return false
}

// viaTextErr runs the escaped tree in text/template with the ported
// escapers as its funcs, which is how the oracle test of a compiled
// template will see them.
func (oracle *oracle) viaTextErr(value any) (string, error) {
	tt := texttemplate.New("t").Funcs(tmplrt.Escapers)
	if _, err := tt.AddParseTree("t", oracle.tpl.Tree); err != nil {
		return "", err
	}
	var buffer bytes.Buffer
	err := tt.Execute(&buffer, value)
	return buffer.String(), err
}

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
	htmlHolder struct{ h template.HTML }
)

func (greeting greeter) String() string {
	return "hello <" + greeting.name + ">"
}
func (failure) Error() string { return `failed: "<x>"` }
func (value namedStr) String() string {
	return "named:" + string(value)
}
func (holder htmlHolder) String() string { return string(holder.h) }

var escapeInputs = func() []struct {
	name string
	v    any
} {
	text := "<ptr> & 'quoted'"
	markup := template.HTML("<b>ptr</b>")
	var ps *string
	pp := &text
	return []struct {
		name string
		v    any
	}{
		{"empty", ""},
		{"markup", "<a href=\"x\">&'</a>"},
		{"javascript url", "javascript:alert(1)"},
		{"whitespace", " \t\n"},
		{"space", " "},
		{"close script", "</script>"},
		{"nil", nil},
		{"int", 42},
		{"float", 3.5},
		{"bool", true},
		{"html", template.HTML("<b>x</b>")},
		{"html with attr and entity", template.HTML(
			`<i title="a>b">&amp; ok</i> <script>x</script>`)},
		{"url", template.URL("javascript:x")},
		{"js", template.JS("a+b")},
		{"jsstr", template.JSStr(`a\x20b`)},
		{"css", template.CSS("red")},
		{"htmlattr", template.HTMLAttr("x=1")},
		{"srcset", template.Srcset("a.png 1x")},
		{"stringer", greeter{"world"}},
		{"stringer pointer", &greeter{"ptr"}},
		{"error", failure{}},
		{"filter", filter("expression(x)")},
		{"named string with String", namedStr("n")},
		{"named int", namedInt(7)},
		{"struct", plain{3}},
		{"slice", []string{"a", "<b>"}},
		{"map", map[string]int{"a": 1}},
		{"*string", &text},
		{"**string", &pp},
		{"nil *string", ps},
		{"*template.HTML", &markup},
		{"nil *plain", (*plain)(nil)},
		{"nil *greeter", (*greeter)(nil)},
		{"stringer returning html", htmlHolder{"<b>x</b>"}},
		{"unicode", "café   ﷐ \xff"},
	}
}()

func TestEscapersMatchStdlib(t *testing.T) {
	used := map[string]bool{}
	for _, escapeContext := range escapeContexts {
		t.Run(escapeContext.name, func(t *testing.T) {
			oracle := newOracle(t, escapeContext.src)
			for _, name := range oracle.chain {
				used[name] = true
				if _, ok := tmplrt.Escapers[name]; !ok {
					t.Fatalf("html/template uses %s, "+
						"which Escapers does not have", name)
				}
			}
			for _, in := range escapeInputs {
				want, err := oracle.stdlibErr(in.v)
				if err != nil {
					// A String method that cannot run on a nil pointer
					// panics inside html/template's escaper, and
					// text/template turns that into an error. The port must
					// fail the same way: the panic itself, and the error
					// through text/template.
					if !oracle.panics(in.v) {
						t.Errorf("%s: html/template fails (%v) "+
							"but the port does not panic", in.name, err)
					}
					if _, err := oracle.viaTextErr(in.v); err == nil {
						t.Errorf("%s: html/template fails "+
							"but text/template with the port does not",
							in.name)
					}
					continue
				}
				if got := oracle.ported(in.v); got != want {
					t.Errorf("%s: chain %v\n direct: %q\n stdlib: %q",
						in.name, oracle.chain, got, want)
				}
				got, err := oracle.viaTextErr(in.v)
				if err != nil || got != want {
					t.Errorf("%s: chain %v\n text/template: %q (err %v)\n"+
						" stdlib: %q", in.name, oracle.chain, got, err, want)
				}
			}
		})
	}
	// Every escaper must be exercised by some context above, or the
	// port of that escaper is untested. _eval_args_ is not an escaper in
	// a chain; TestEvalArgsMatchesStdlib covers it.
	var missing []string
	for name := range tmplrt.Escapers {
		if !used[name] && name != "_eval_args_" {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("no context exercises %v", missing)
	}
}

func TestEscapersMap(t *testing.T) {
	if len(tmplrt.Escapers) != 17 {
		t.Errorf("len(Escapers) = %d, want 17", len(tmplrt.Escapers))
	}
	for name, fn := range tmplrt.Escapers {
		if !strings.HasPrefix(name, "_html_template_") &&
			name != "_eval_args_" {
			t.Errorf("%s: not an html/template escaper name", name)
		}
		if _, ok := fn.(func(...any) string); !ok {
			t.Errorf("%s: is %T, want func(...any) string", name, fn)
		}
	}
}

// TestEvalArgsMatchesStdlib covers _eval_args_, which escaping puts in
// front of a predefined escaper called with arguments. An escaped tree
// for {{html .}} calls it, so without it in Escapers that tree cannot
// run at all.
func TestEvalArgsMatchesStdlib(t *testing.T) {
	for _, src := range []string{
		`<p>{{html .}}</p>`,
		`<p title="{{html . "&" .}}">`,
		`<a href="/x?q={{urlquery . 1}}">`,
		`<textarea>{{html "<" .}}</textarea>`,
	} {
		for _, in := range escapeInputs {
			tpl := template.Must(template.New("t").Parse(src))
			var want bytes.Buffer
			wantErr := tpl.Execute(&want, in.v)
			if tpl.Tree == nil {
				t.Fatalf("%s: html/template failed to escape: %v", src, wantErr)
			}
			if !strings.Contains(tpl.Tree.Root.String(), "_eval_args_") {
				t.Fatalf("%s: html/template did not use _eval_args_: %s",
					src, tpl.Tree.Root)
			}
			tt := texttemplate.New("t").Funcs(tmplrt.Escapers)
			if _, err := tt.AddParseTree("t", tpl.Tree); err != nil {
				t.Fatal(err)
			}
			var got bytes.Buffer
			gotErr := tt.Execute(&got, in.v)
			if (wantErr != nil) != (gotErr != nil) ||
				wantErr == nil && got.String() != want.String() {
				t.Errorf("%s with %s:\n text/template %q (err %v)\n"+
					" stdlib %q (err %v)",
					src, in.name, got.String(), gotErr, want.String(), wantErr)
			}
		}
	}
}

// TestEscapersMultipleArgs covers the fmt.Sprint branch, which a
// one-action template with a single argument never takes.
func TestEscapersMultipleArgs(t *testing.T) {
	// html/template gives escapers one argument per pipeline stage, so
	// the multi-argument form is only reachable by calling the funcs
	// directly. It must match fmt.Sprint with pointers dereferenced and
	// nils skipped, then escape that.
	text := "x<"
	got := tmplrt.HTMLEscaper(nil, &text, 1, template.HTML("<b>"))
	if want := "x&lt;1&lt;b&gt;"; got != want {
		t.Errorf("HTMLEscaper multi-arg = %q, want %q", got, want)
	}
}

var fastCorpus = []string{
	"", "plain", "hello world", "<a href=\"x\">&'</a>", "javascript:alert(1)",
	" \t\n", "a b%41%zz%4", "%", "\x00", "\xff\xfe", "http://x/?q=a b&c=d#f",
	"  ", "café", "﷐�", "a+b=c`d", "mailto:a@b.c",
	"HTTPS://X", "/rel/path:colon", "a/b:c", "</script><!--", "it's", "100%",
	"~-._ok", "[x]!$*,;@", "\\\"", "</SCRIPT", "a\rb\fc\vd",
}

func TestFastPathsMatchGeneric(t *testing.T) {
	fast := []struct {
		name    string
		fast    func(string) string
		generic func(...any) string
	}{
		{"HTMLEscaperString", tmplrt.HTMLEscaperString, tmplrt.HTMLEscaper},
		{"AttrEscaperString", tmplrt.AttrEscaperString, tmplrt.AttrEscaper},
		{
			"RCDATAEscaperString",
			tmplrt.RCDATAEscaperString, tmplrt.RCDATAEscaper,
		},
		{"URLFilterString", tmplrt.URLFilterString, tmplrt.URLFilter},
		{
			"URLNormalizerString",
			tmplrt.URLNormalizerString, tmplrt.URLNormalizer,
		},
		{"URLEscaperString", tmplrt.URLEscaperString, tmplrt.URLEscaper},
		{"JSValEscaperString", tmplrt.JSValEscaperString, tmplrt.JSValEscaper},
		{"JSStrEscaperString", tmplrt.JSStrEscaperString, tmplrt.JSStrEscaper},
	}
	for _, pair := range fast {
		for _, text := range fastCorpus {
			if got, want := pair.fast(text), pair.generic(text); got != want {
				t.Errorf("%s(%q) = %q, generic gives %q",
					pair.name, text, got, want)
			}
		}
	}
	writers := []struct {
		name    string
		write   func(*bytes.Buffer, string)
		generic func(...any) string
	}{
		{"WriteHTMLEscaped", tmplrt.WriteHTMLEscaped, tmplrt.HTMLEscaper},
		{"WriteAttrEscaped", tmplrt.WriteAttrEscaped, tmplrt.AttrEscaper},
	}
	for _, writer := range writers {
		for _, text := range fastCorpus {
			var buffer bytes.Buffer
			buffer.WriteString("pre:")
			writer.write(&buffer, text)
			got, want := buffer.String(), "pre:"+writer.generic(text)
			if got != want {
				t.Errorf("%s(%q) wrote %q, generic gives %q",
					writer.name, text, got, want)
			}
		}
	}
}

// TestFastPathsDoNotAllocate pins the point of the fast paths: input
// that needs no escaping comes back as is, without allocating.
func TestFastPathsDoNotAllocate(t *testing.T) {
	const clean = "A perfectly ordinary sentence, with a comma."
	const cleanURL = "/path/to/page-1_2.html?x=1&y=2"
	cases := []struct {
		name string
		in   string
		fn   func(string) string
	}{
		{"HTMLEscaperString", clean, tmplrt.HTMLEscaperString},
		{"AttrEscaperString", clean, tmplrt.AttrEscaperString},
		{"RCDATAEscaperString", clean, tmplrt.RCDATAEscaperString},
		{"URLFilterString", cleanURL, tmplrt.URLFilterString},
		{"URLNormalizerString", cleanURL, tmplrt.URLNormalizerString},
		{"URLEscaperString", "path-to_page.1~2", tmplrt.URLEscaperString},
		{"JSStrEscaperString", clean, tmplrt.JSStrEscaperString},
	}
	for _, testCase := range cases {
		var out string
		allocations := testing.AllocsPerRun(100, func() {
			out = testCase.fn(testCase.in)
		})
		if allocations != 0 {
			t.Errorf("%s allocates %v times for clean input",
				testCase.name, allocations)
		}
		if out != testCase.in {
			t.Errorf("%s(%q) = %q, want the input back",
				testCase.name, testCase.in, out)
		}
	}
	var buffer bytes.Buffer
	buffer.Grow(1 << 10)
	for name, fn := range map[string]func(*bytes.Buffer, string){
		"WriteHTMLEscaped": tmplrt.WriteHTMLEscaped,
		"WriteAttrEscaped": tmplrt.WriteAttrEscaped,
	} {
		allocations := testing.AllocsPerRun(100, func() {
			buffer.Reset()
			fn(&buffer, clean)
			fn(&buffer, "<b>&</b>")
		})
		if allocations != 0 {
			t.Errorf("%s allocates %v times with a roomy buffer",
				name, allocations)
		}
	}
}

// FuzzHTMLEscaperString checks the text-context fast path, the generic
// port and html/template against each other.
func FuzzHTMLEscaperString(f *testing.F) {
	oracle := newOracle(f, `<p>{{.}}</p>`)
	for _, text := range fastCorpus {
		f.Add(text)
	}
	f.Fuzz(func(t *testing.T, text string) {
		want := oracle.stdlib(t, text)
		got := oracle.before + tmplrt.HTMLEscaperString(text) + oracle.after
		if got != want {
			t.Fatalf("fast path %q, stdlib %q", got, want)
		}
		if got := oracle.ported(text); got != want {
			t.Fatalf("generic %q, stdlib %q", got, want)
		}
		var buffer bytes.Buffer
		buffer.WriteString(oracle.before)
		tmplrt.WriteHTMLEscaped(&buffer, text)
		buffer.WriteString(oracle.after)
		if buffer.String() != want {
			t.Fatalf("writer %q, stdlib %q", buffer.String(), want)
		}
	})
}

// FuzzAttrEscaperString is the same check for a quoted attribute value.
func FuzzAttrEscaperString(f *testing.F) {
	oracle := newOracle(f, `<p title="{{.}}">`)
	for _, text := range fastCorpus {
		f.Add(text)
	}
	f.Fuzz(func(t *testing.T, text string) {
		want := oracle.stdlib(t, text)
		got := oracle.before + tmplrt.AttrEscaperString(text) + oracle.after
		if got != want {
			t.Fatalf("fast path %q, stdlib %q", got, want)
		}
		if got := oracle.ported(text); got != want {
			t.Fatalf("generic %q, stdlib %q", got, want)
		}
		var buffer bytes.Buffer
		buffer.WriteString(oracle.before)
		tmplrt.WriteAttrEscaped(&buffer, text)
		buffer.WriteString(oracle.after)
		if buffer.String() != want {
			t.Fatalf("writer %q, stdlib %q", buffer.String(), want)
		}
	})
}

// FuzzURLChainString checks the href chain, urlfilter then urlnormalizer
// then attrescaper, through its three fast paths.
func FuzzURLChainString(f *testing.F) {
	oracle := newOracle(f, `<a href="{{.}}">`)
	want := []string{
		"_html_template_urlfilter",
		"_html_template_urlnormalizer",
		"_html_template_attrescaper",
	}
	if !reflect.DeepEqual(oracle.chain, want) {
		f.Fatalf("html/template chain is %v, want %v", oracle.chain, want)
	}
	for _, text := range fastCorpus {
		f.Add(text)
	}
	f.Fuzz(func(t *testing.T, text string) {
		want := oracle.stdlib(t, text)
		fast := tmplrt.AttrEscaperString(
			tmplrt.URLNormalizerString(tmplrt.URLFilterString(text)))
		if got := oracle.before + fast + oracle.after; got != want {
			t.Fatalf("fast path %q, stdlib %q", got, want)
		}
		if got := oracle.ported(text); got != want {
			t.Fatalf("generic %q, stdlib %q", got, want)
		}
	})
}

// fuzzFast is the shared body of the fuzz targets below. It checks the
// html/template chain for src is exactly names, then, for each input,
// that the fast path, the generic ports and html/template agree.
func fuzzFast(
	f *testing.F, src string, names []string, fast func(string) string,
) {
	oracle := newOracle(f, src)
	if !reflect.DeepEqual(oracle.chain, names) {
		f.Fatalf("html/template chain for %s is %v, want %v",
			src, oracle.chain, names)
	}
	for _, text := range fastCorpus {
		f.Add(text)
	}
	f.Fuzz(func(t *testing.T, text string) {
		want := oracle.stdlib(t, text)
		if got := oracle.before + fast(text) + oracle.after; got != want {
			t.Fatalf("fast path %q, stdlib %q", got, want)
		}
		if got := oracle.ported(text); got != want {
			t.Fatalf("generic %q, stdlib %q", got, want)
		}
	})
}

// FuzzURLEscaperString covers a value in a URL query: urlescaper, then
// attrescaper.
func FuzzURLEscaperString(f *testing.F) {
	fuzzFast(f, `<a href="/x?q={{.}}">`,
		[]string{"_html_template_urlescaper", "_html_template_attrescaper"},
		func(text string) string {
			return tmplrt.AttrEscaperString(tmplrt.URLEscaperString(text))
		})
}

// FuzzJSValEscaperString covers a value in JavaScript code.
func FuzzJSValEscaperString(f *testing.F) {
	fuzzFast(f, `<script>var x = {{.}};</script>`,
		[]string{"_html_template_jsvalescaper"}, tmplrt.JSValEscaperString)
}

// FuzzJSStrEscaperString covers a value inside a JavaScript string.
func FuzzJSStrEscaperString(f *testing.F) {
	fuzzFast(f, `<script>var x = "{{.}}";</script>`,
		[]string{"_html_template_jsstrescaper"}, tmplrt.JSStrEscaperString)
}
