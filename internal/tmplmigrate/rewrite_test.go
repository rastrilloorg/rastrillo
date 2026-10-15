package tmplmigrate

import (
	"errors"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"text/template"
)

func TestRewriteCases(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{`{{T "k"}}`, Prelude + `{{T $L "k"}}`},
		{`{{.X | number}}`, Prelude + `{{.X | number $L}}`},
		{`<a title="{{Tf "k" "n" .N}}">`, Prelude + `<a title="{{Tf $L "k" "n" .N}}">`},
		{`{{template "p" dict "A" 1}}`, Prelude + `{{template "p" dict "L" $L "A" 1}}`},
		{`{{template "p" (dict "A" (T "k"))}}`, Prelude + `{{template "p" (dict "L" $L "A" (T $L "k"))}}`},
		{`{{define "x"}}{{range .Items}}{{template "p" .}}{{end}}{{end}}`, `{{define "x"}}` + Prelude + `{{range .Items}}{{template "p" (withL (.) $L)}}{{end}}{{end}}`},
		{`{{define "x"}}{{template "p" .}}{{block "b" .}}{{T "k"}}{{end}}{{end}}`, `{{define "x"}}{{template "p" .}}{{block "b" .}}` + Prelude + `{{T $L "k"}}{{end}}{{end}}`},
		{`{{/* T "k" */}}<p>T "k"</p>{{"T" | printf "%s"}}`, `{{/* T "k" */}}<p>T "k"</p>{{"T" | printf "%s"}}`},
		{`{{dateWords}}`, Prelude + `{{dateWords $L}}`},
		// Round eight, finding 4: the complete pipeline is wrapped, so its
		// grouping is kept, and a "}}" inside a string is not the end.
		{`{{template "p" makeData .Item}}`, Prelude + `{{template "p" (withL (makeData .Item) $L)}}`},
		{`{{template "p" .Item | pick}}`, Prelude + `{{template "p" (withL (.Item | pick) $L)}}`},
		{`{{template "p" printf "%s}}" .X -}}`, Prelude + `{{template "p" (withL (printf "%s}}" .X) $L) -}}`},
		{"{{template \"p\" printf `}}` .X}}", Prelude + "{{template \"p\" (withL (printf `}}` .X) $L)}}"},
		// Round nine, finding 1: the localizer may be added inside a
		// wrapped pipeline; validation normalises that insertion away.
		{`{{template "p" makeData (T "key")}}`, Prelude + `{{template "p" (withL (makeData (T $L "key")) $L)}}`},
		{`{{template "p" makeData (printf "%s" (Tf "k" "n" .N))}}`, Prelude + `{{template "p" (withL (makeData (printf "%s" (Tf $L "k" "n" .N))) $L)}}`},
		{`{{template "p" .X | pick (T "k")}}`, Prelude + `{{template "p" (withL (.X | pick (T $L "k")) $L)}}`},
		{`{{template "p" makeData (dict "A" (number .N))}}`, Prelude + `{{template "p" (withL (makeData (dict "A" (number $L .N))) $L)}}`},
		// An existing prelude is ours: nothing is added twice.
		{Prelude + `{{T "k"}}`, Prelude + `{{T $L "k"}}`},
	} {
		got, _, err := Rewrite("t", c.in)
		if err != nil || got != c.want {
			t.Errorf("Rewrite(%s)\n got %s, %v\nwant %s", c.in, got, err, c.want)
			continue
		}
		again, st, err := Rewrite("t", got)
		if err != nil || again != got || st.Calls+st.Dicts+st.Wrapped+st.Preludes != 0 {
			t.Errorf("not idempotent on %s: %s, %v", got, again, err)
		}
	}
}

// Round eight, finding 5: any other binding of $L, at any depth, makes
// the file refused and unchanged.
func TestRewriteRefusesConflictingL(t *testing.T) {
	for _, in := range []string{
		`{{if .Flag}}{{$L := "other"}}{{end}}{{T "key"}}`,
		`{{$L := .Other}}{{T "key"}}`,
		`{{range $i, $L := .Items}}{{T "key"}}{{end}}`,
		`{{with $L := .X}}{{.}}{{end}}{{T "k"}}`,
		Prelude + `{{$L = .Other}}{{T $L "k"}}`,
	} {
		out, _, err := Rewrite("t", in)
		var c *ConflictError
		if !errors.As(err, &c) || out != in {
			t.Errorf("Rewrite(%s) = %q, %v; want a ConflictError and the text unchanged", in, out, err)
		}
	}
}

// Round eight, finding 3: a template call with no data is reported.
func TestRewriteReportsNoData(t *testing.T) {
	_, st, err := Rewrite("t", `{{T "k"}}{{template "p"}}`)
	if err != nil || len(st.NoData) != 1 || !strings.Contains(st.NoData[0], `{{template "p"}}`) {
		t.Errorf("no-data report: %v, %v", st.NoData, err)
	}
}

// validate catches a rewrite that lost $L or regrouped a pipeline.
func TestValidateRejectsBadRewrites(t *testing.T) {
	if err := validate("t", `{{T "k"}}`, nil); err == nil {
		t.Error("a locale call without $L passed validation")
	}
	if err := validate("t", Prelude+`{{template "p" (withL makeData $L)}}`, []string{"makeData .Item"}); err == nil {
		t.Error("a regrouped pipeline passed validation")
	}
	if err := validate("t", Prelude+`{{template "p" (withL (makeData (T $L "other")) $L)}}`, []string{`makeData (T "key")`}); err == nil {
		t.Error("a changed argument inside a wrapped pipeline passed validation")
	}
	if err := validate("t", `{{template "p" dict "A" 1}}`, nil); err == nil {
		t.Error("a dict without L passed validation")
	}
}

// Review Focus 5: the rewrite only ever inserts, and only the texts
// it is allowed to insert. A real app's file may have CRLF line
// endings, a byte-order mark, trim markers, comments, and literal text
// that looks like an insertion; none of it may change.
func TestRewriteOnlyInserts(t *testing.T) {
	const (
		lookAlikeText    = `<p>T $L "L" $L (withL (x) $L)</p>`
		lookAlikeLiteral = `{{printf "%s" " $L"}}`
	)
	for name, in := range map[string]string{
		"crlf":       "{{define \"a\"}}\r\n<p>{{T \"k\"}}</p>\r\n{{template \"p\" dict \"A\" 1}}\r\n{{end}}\r\n",
		"bom":        "\ufeff<p>{{T \"k\"}}</p>\n",
		"trim":       "{{- T \"k\" -}}\n{{- template \"p\" dict \"A\" 1 -}}\n{{- .X | number -}}",
		"comment":    "{{/* {{T \"k\"}} dict \"A\" */}}{{- /* T $L withL */ -}}<p>{{T \"k\"}}</p>",
		"two defs":   "{{define \"a\"}}{{T \"k\"}}{{end}}\n{{define \"b\"}}{{Tf \"k\" \"n\" 1}}{{end}}",
		"look-alike": lookAlikeText + lookAlikeLiteral + "{{T \"k\"}}",
		"nested":     "{{template \"p\" makeData (printf \"%s\" (Tf \"k\" \"n\" (number .N)))}}",
		"multiline":  "{{template \"p\"\n  makeData\n  .Item}}\n{{T\n \"k\"}}",
	} {
		out, st, err := Rewrite(name, in)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if extra, ok := onlyAllowedInsertions(in, out); !ok {
			t.Errorf("%s: the rewrite changed input bytes or inserted something else (%q):\n in %q\nout %q", name, extra, in, out)
		}
		if got, want := comments(out), comments(in); !slices.Equal(got, want) {
			t.Errorf("%s: comments changed:\n got %q\nwant %q", name, got, want)
		}
		if strings.HasPrefix(in, "\ufeff") && !strings.HasPrefix(out, "\ufeff") {
			t.Errorf("%s: the byte-order mark moved: %q", name, out)
		}
		if strings.Count(out, "\r\n") != strings.Count(in, "\r\n") {
			t.Errorf("%s: line endings changed", name)
		}
		switch name {
		case "look-alike":
			// The alignment cannot tell where an insertion landed; a
			// " $L" inside the paragraph or the string literal would
			// pass it, so both must survive byte for byte.
			if !strings.Contains(out, lookAlikeText) || !strings.Contains(out, lookAlikeLiteral) {
				t.Errorf("%s: literal text changed: %q", name, out)
			}
		case "comment":
			if st.Calls != 1 {
				t.Errorf("%s: %d calls rewritten, want 1 (only the call outside the comments)", name, st.Calls)
			}
		}
	}
}

// allowed are the only texts Rewrite may insert.
var allowed = []string{Prelude, ` "L" $L`, ` $L`, `(withL (`, `) $L)`}

// onlyAllowedInsertions reports whether out is in with only allowed
// insertions added. It searches every alignment (memoised), because a
// greedy one fails on the commonest insertion: " $L" begins with the
// same space as the input it is inserted before. On failure it returns
// out from the furthest position any alignment reached, for the message.
func onlyAllowedInsertions(in, out string) (extra string, ok bool) {
	type state struct{ i, j int }
	dead := map[state]bool{}
	far := 0
	var walk func(i, j int) bool
	walk = func(i, j int) bool {
		if j > far {
			far = j
		}
		if j == len(out) {
			return i == len(in)
		}
		if dead[state{i, j}] {
			return false
		}
		if i < len(in) && in[i] == out[j] && walk(i+1, j+1) {
			return true
		}
		for _, a := range allowed {
			if strings.HasPrefix(out[j:], a) && walk(i, j+len(a)) {
				return true
			}
		}
		dead[state{i, j}] = true
		return false
	}
	if walk(0, 0) {
		return "", true
	}
	return out[far:], false
}

var commentRE = regexp.MustCompile(`(?s)\{\{-?\s*/\*.*?\*/\s*-?\}\}`)

func comments(s string) []string { return commentRE.FindAllString(s, -1) }

// The prelude goes where the body starts as rendered. A leading
// comment's parsed position is inside its delimiters, a leading "{{-"
// has trimmed the space before it, and a file's top-level body may
// begin after a {{define}}, so a prelude at the parsed position would
// break the parse, panic, or make trimmed space render. Each result
// must also render exactly as its input does.
func TestRewritePreludePlacement(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{`{{define "x"}}{{/* c */}}{{T "k"}}{{end}}`, `{{define "x"}}` + Prelude + `{{/* c */}}{{T $L "k"}}{{end}}`},
		{"{{define \"x\"}}\n  {{- T \"k\"}}{{end}}", "{{define \"x\"}}" + Prelude + "\n  {{- T $L \"k\"}}{{end}}"},
		{"{{define \"x\"}}\r\n{{- /* c */ -}}\r\n{{T \"k\"}}{{end}}", "{{define \"x\"}}" + Prelude + "\r\n{{- /* c */ -}}\r\n{{T $L \"k\"}}{{end}}"},
		{"{{define \"x\" -}}\n  <p>{{T \"k\"}}</p>{{end}}", "{{define \"x\" -}}\n  " + Prelude + "<p>{{T $L \"k\"}}</p>{{end}}"},
		{"{{define \"x\"}}{{block \"b\" .}}\n  {{- T \"k\"}}{{end}}{{end}}", "{{define \"x\"}}{{block \"b\" .}}" + Prelude + "\n  {{- T $L \"k\"}}{{end}}{{end}}"},
		{"\ufeff\n{{- /* c */}}{{T \"k\"}}", "\ufeff" + Prelude + "\n{{- /* c */}}{{T $L \"k\"}}"},
		// A file's top-level body that begins after a {{define}}: its
		// parsed position is the define's, not its first node's.
		{"{{define \"a\"}}x{{end}}\n{{- /* top */}}\n{{T \"k\"}}", Prelude + "{{define \"a\"}}x{{end}}\n{{- /* top */}}\n{{T $L \"k\"}}"},
		{`{{define "a"}}x{{end}}{{/* top */}}{{T "k"}}`, Prelude + `{{define "a"}}x{{end}}{{/* top */}}{{T $L "k"}}`},
		{"\n{{- define \"a\"}}x{{end}}{{/* top */}}{{T \"k\"}}", Prelude + "\n{{- define \"a\"}}x{{end}}{{/* top */}}{{T $L \"k\"}}"},
		{"    \n\n{{- define \"a\"}}x{{end}}{{/* top */}}{{T \"k\"}}", Prelude + "    \n\n{{- define \"a\"}}x{{end}}{{/* top */}}{{T $L \"k\"}}"},
		{"{{define \"a\"}}{{T \"k\"}}{{end}}\n<p>{{T \"k\"}}</p>", Prelude + "{{define \"a\"}}" + Prelude + "{{T $L \"k\"}}{{end}}\n<p>{{T $L \"k\"}}</p>"},
	} {
		got, _, err := Rewrite("t", c.in)
		if err != nil || got != c.want {
			t.Errorf("Rewrite(%q)\n got %q, %v\nwant %q", c.in, got, err, c.want)
			continue
		}
		if in, out := render(t, c.in), render(t, got); !maps.Equal(in, out) {
			t.Errorf("Rewrite(%q) renders differently:\n in %q\nout %q", c.in, in, out)
		}
	}
}

// render executes every template src defines, with stand-ins for the
// functions the rewrite touches, so a rewrite that makes trimmed space
// render shows up as different output.
func render(t *testing.T, src string) map[string]string {
	t.Helper()
	stub := func(args ...any) string { return "k" }
	set, err := template.New("t").Funcs(template.FuncMap{
		"T": stub, "localizer": func(any) any { return nil },
	}).Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	out := map[string]string{}
	for _, tm := range set.Templates() {
		var b strings.Builder
		if err := tm.Execute(&b, nil); err != nil {
			t.Fatalf("execute %s of %q: %v", tm.Name(), src, err)
		}
		out[tm.Name()] = b.String()
	}
	return out
}

// One file that makes the rewrite panic is that file's error, not the
// end of the whole run: a migrate-templates run over an app reports it
// like any refused file.
func TestRewriteFSContainsPanics(t *testing.T) {
	defer func(orig func(string, string) (string, Stats, error)) { rewriteFile = orig }(rewriteFile)
	rewriteFile = func(name, src string) (string, Stats, error) { panic("boom") }
	fsys := fstest.MapFS{"tpl/a.html": {Data: []byte(`{{T "k"}}`)}}
	_, _, err := RewriteFS(fsys, "tpl")
	if err == nil || !strings.Contains(err.Error(), "tpl/a.html") || !strings.Contains(err.Error(), "boom") {
		t.Errorf("RewriteFS with a panicking rewrite: %v; want an error naming the file and the panic", err)
	}
}
