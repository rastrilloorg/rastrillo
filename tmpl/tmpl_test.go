package tmpl_test

import (
	"bytes"
	"context"
	"errors"
	htmltemplate "html/template"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"

	"amadan.net/rastrillo/rastrillo/tmpl"
	"amadan.net/rastrillo/rastrillo/tmpl/tmplrt"
)

const paritySource = `{{define "row"}}<li title="{{.}}">{{up .}}</li>{{end}}` +
	`<ul>{{range .}}{{template "row" .}}{{end}}</ul>`

var parityFuncs = map[string]any{"up": strings.ToUpper}

var parityData = []string{"a", `<b>"&'`}

// outcome is what one call produced, compared across the two packages:
// the bytes, and whether it failed. Error text is not part of the
// contract, except where tmpl writes html/template's own error.
type outcome struct {
	out    string
	failed bool
}

func result(out string, err error) outcome { return outcome{out, err != nil} }

func TestParityWithHTMLTemplate(t *testing.T) {
	newStd := func() *htmltemplate.Template {
		return htmltemplate.Must(
			htmltemplate.New("page").Funcs(parityFuncs).Parse(paritySource),
		)
	}
	newOurs := func() *tmpl.Template {
		return tmpl.Must(
			tmpl.New("page").Funcs(parityFuncs).Parse(paritySource),
		)
	}
	cases := []struct {
		name string
		std  func() outcome
		ours func() outcome
	}{
		{"Execute",
			func() outcome {
				var buffer bytes.Buffer
				err := newStd().Execute(&buffer, parityData)
				return result(buffer.String(), err)
			},
			func() outcome {
				var buffer bytes.Buffer
				err := newOurs().Execute(&buffer, parityData)
				return result(buffer.String(), err)
			}},
		{"ExecuteTemplate",
			func() outcome {
				var builder strings.Builder
				err := newStd().ExecuteTemplate(&builder, "row", "x<y")
				return result(builder.String(), err)
			},
			func() outcome {
				var builder strings.Builder
				err := newOurs().ExecuteTemplate(&builder, "row", "x<y")
				return result(builder.String(), err)
			}},
		{"Lookup then Execute",
			func() outcome {
				var buffer bytes.Buffer
				err := newStd().Lookup("row").Execute(&buffer, "q")
				return result(buffer.String(), err)
			},
			func() outcome {
				var buffer bytes.Buffer
				err := newOurs().Lookup("row").Execute(&buffer, "q")
				return result(buffer.String(), err)
			}},
		{"Clone then override",
			func() outcome {
				base := newStd()
				clone := htmltemplate.Must(htmltemplate.Must(base.Clone()).
					Parse(`{{define "row"}}<b>{{.}}</b>{{end}}`))
				var buffer bytes.Buffer
				err := clone.Execute(&buffer, parityData)
				buffer.WriteString("|")
				err = errors.Join(err, base.Execute(&buffer, parityData))
				return result(buffer.String(), err)
			},
			func() outcome {
				base := newOurs()
				clone := tmpl.Must(tmpl.Must(base.Clone()).
					Parse(`{{define "row"}}<b>{{.}}</b>{{end}}`))
				var buffer bytes.Buffer
				err := clone.Execute(&buffer, parityData)
				buffer.WriteString("|")
				err = errors.Join(err, base.Execute(&buffer, parityData))
				return result(buffer.String(), err)
			}},
		{"missingkey=error",
			func() outcome {
				standard := htmltemplate.Must(
					htmltemplate.New("m").
						Option("missingkey=error").
						Parse(`{{.absent}}`),
				)
				var buffer bytes.Buffer
				err := standard.Execute(&buffer, map[string]string{})
				return result(buffer.String(), err)
			},
			func() outcome {
				wrapped := tmpl.Must(
					tmpl.New("m").
						Option("missingkey=error").
						Parse(`{{.absent}}`),
				)
				var buffer bytes.Buffer
				err := wrapped.Execute(&buffer, map[string]string{})
				return result(buffer.String(), err)
			}},
		{"Parse after Execute",
			func() outcome {
				standard := newStd()
				err := standard.Execute(&bytes.Buffer{}, parityData)
				_, perr := standard.Parse(`{{define "late"}}{{end}}`)
				return outcome{errText(perr), err != nil || perr != nil}
			},
			func() outcome {
				ours := newOurs()
				err := ours.Execute(&bytes.Buffer{}, parityData)
				_, perr := ours.Parse(`{{define "late"}}{{end}}`)
				return outcome{errText(perr), err != nil || perr != nil}
			}},
		{"undefined template",
			func() outcome {
				var buffer bytes.Buffer
				err := newStd().ExecuteTemplate(&buffer, "nope", nil)
				return outcome{errText(err), err != nil}
			},
			func() outcome {
				var buffer bytes.Buffer
				err := newOurs().ExecuteTemplate(&buffer, "nope", nil)
				return outcome{errText(err), err != nil}
			}},
		{"names and DefinedTemplates",
			func() outcome {
				standard := newStd()
				return outcome{
					standard.Name() + names(
						standard.Templates(),
					) + sorted(
						standard.DefinedTemplates(),
					),
					false,
				}
			},
			func() outcome {
				ours := newOurs()
				return outcome{
					ours.Name() + names(
						ours.Templates(),
					) + sorted(
						ours.DefinedTemplates(),
					),
					false,
				}
			}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			want, got := testCase.std(), testCase.ours()
			if got != want {
				t.Errorf("tmpl gave %+v\nhtml/template gave %+v", got, want)
			}
		})
	}
}

// sorted puts DefinedTemplates' list, which comes from a map, in order.
func sorted(defined string) string {
	const prefix = "; defined templates are: "
	list := strings.Split(strings.TrimPrefix(defined, prefix), ", ")
	slices.Sort(list)
	return prefix + strings.Join(list, ", ")
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// names lists templates sorted, because Templates comes from a map.
func names[T interface{ Name() string }](ts []T) string {
	var out []string
	for _, template := range ts {
		out = append(out, template.Name())
	}
	slices.Sort(out)
	return strings.Join(out, ",")
}

func TestOwner(t *testing.T) {
	const here = "amadan.net/rastrillo/rastrillo/tmpl_test"
	set := tmpl.New("x")
	if got := tmpl.OwnerOf(set); got != here {
		t.Errorf("New from this package: owner %q, want %q", got, here)
	}
	if got := tmpl.OwnerOf(tmpl.NewViaHelper("y")); got != here {
		t.Errorf("New through a framework helper: owner %q, want %q", got, here)
	}
	tmpl.Must(set.Parse(`{{define "p"}}p{{end}}x`))
	if got := tmpl.OwnerOf(set.Lookup("p")); got != here {
		t.Errorf("Lookup: owner %q, want %q", got, here)
	}
	tmpl.SetOwner(set, "example.com/original")
	clone := tmpl.Must(set.Clone())
	if got := tmpl.OwnerOf(clone); got != "example.com/original" {
		t.Errorf("Clone: owner %q, want the original's", got)
	}

	for pkg, want := range map[string]bool{
		"amadan.net/rastrillo/rastrillo/tmpl":                 true,
		"amadan.net/rastrillo/rastrillo/ui":                   true,
		"amadan.net/rastrillo/rastrillo/live":                 true,
		"amadan.net/rastrillo/rastrillo/view":                 true,
		"amadan.net/rastrillo/rastrillo/tmpl/internal/corpus": false,
		"amadan.net/rastrillo/rastrillo/ui/testdata":          false,
		"amadan.net/rastrillo/rastrillo/tmpl_test":            false,
		"amadan.net/rastrillo/rastrillo":                      false,
	} {
		if got := tmpl.IsFramework(pkg); got != want {
			t.Errorf("IsFramework(%q) = %v, want %v", pkg, got, want)
		}
	}
	const module = "amadan.net/rastrillo/rastrillo"
	const generic = "example.com/app.render[go.shape.struct { a/b.T }]"
	for fn, want := range map[string]string{
		"main.main":                           "main",
		module + "/ui.(*Pages).build":         module + "/ui",
		module + "/tmpl_test.TestOwner.func1": module + "/tmpl_test",
		"gopkg.in/yaml%2ev3.Unmarshal":        "gopkg.in/yaml.v3",
		generic:                               "example.com/app",
	} {
		if got := tmpl.PackageOf(fn); got != want {
			t.Errorf("PackageOf(%q) = %q, want %q", fn, got, want)
		}
	}
}

// register compiles name in a fresh copy of the set build makes, by
// registering bind under its fingerprint. The fingerprint is read from a
// throwaway copy because reading it prepares that set, and prepare has
// to see the registration.
func register(
	t *testing.T,
	build func() *tmpl.Template,
	name string,
	bind tmplrt.Bind,
) {
	t.Helper()
	fp := tmpl.FingerprintOf(build(), name)
	if fp == "" {
		t.Fatalf("%q has no fingerprint", name)
	}
	tmplrt.Register(fp, bind)
}

func writes(text string) tmplrt.Bind {
	return func(map[string]any) (tmplrt.Exec, error) {
		return func(buffer *bytes.Buffer, data any) error {
			buffer.WriteString(text)
			return nil
		}, nil
	}
}

// sawFuncs is what the Bind in TestCompiledPathAndFallback was given. It
// is not local to the test because registration outlives a run: under
// -count, a later run's sets bind the first run's closure.
var sawFuncs map[string]any

func TestCompiledPathAndFallback(t *testing.T) {
	build := func() *tmpl.Template {
		return tmpl.Must(tmpl.New("page").Funcs(parityFuncs).
			Parse(`<p>{{up .}}</p>{{define "unregistered"}}{{.}}{{end}}`))
	}
	register(t, build, "page", func(funcs map[string]any) (tmplrt.Exec, error) {
		sawFuncs = funcs
		return func(buffer *bytes.Buffer, data any) error {
			buffer.WriteString("compiled " + data.(string))
			return nil
		}, nil
	})

	set := build()
	if !set.IsCompiled("page") {
		t.Fatal(`IsCompiled("page") = false with a Bind registered`)
	}
	if _, ok := sawFuncs["up"].(func(string) string); !ok {
		t.Errorf("Bind got funcs %v, want the set's FuncMap", sawFuncs)
	}
	var buf bytes.Buffer
	if err := set.Execute(&buf, "x"); err != nil ||
		buf.String() != "compiled x" {
		t.Errorf(
			"Execute into a Buffer = %q, %v; want the compiled output",
			buf.String(),
			err,
		)
	}
	var sb strings.Builder
	if err := set.ExecuteTemplate(&sb, "page", "y"); err != nil ||
		sb.String() != "compiled y" {
		t.Errorf(
			"ExecuteTemplate into a Builder = %q, %v; want the compiled output",
			sb.String(),
			err,
		)
	}
	// html/template refuses these once a template has executed; running
	// compiled code instead must not let them through. This is before
	// anything has run on html/template, which would refuse them itself.
	if _, err := set.Parse(`{{define "late"}}{{end}}`); err == nil ||
		err.Error() != "html/template: cannot Parse after Execute" {
		t.Errorf(
			"Parse after a compiled Execute: %v, want html/template's error",
			err,
		)
	}
	if _, err := set.Clone(); err == nil {
		t.Error(
			"Clone after a compiled Execute succeeded; " +
				"html/template refuses it",
		)
	}
	if set.IsCompiled("unregistered") {
		t.Error(`IsCompiled("unregistered") = true with nothing registered`)
	}
	sb.Reset()
	if err := set.ExecuteTemplate(&sb, "unregistered", "<z>"); err != nil ||
		sb.String() != "&lt;z&gt;" {
		t.Errorf(
			"unregistered template = %q, %v; want html/template's output",
			sb.String(),
			err,
		)
	}

	// The compiled code was bound to the old functions.
	set.Funcs(map[string]any{"up": strings.ToLower})
	if set.IsCompiled("page") {
		t.Error(
			"still compiled after Funcs changed the functions it was bound to",
		)
	}
	buf.Reset()
	if err := set.Execute(&buf, "X"); err != nil || buf.String() != "<p>x</p>" {
		t.Errorf(
			"after Funcs, Execute = %q, %v; "+
				"want html/template with the new function",
			buf.String(),
			err,
		)
	}

	t.Run("a change before first use is seen", func(t *testing.T) {
		set := build()
		if !set.IsCompiled("page") {
			t.Fatal("not compiled")
		}
		tmpl.Must(set.Parse(`<p>{{.}} changed</p>`))
		if set.IsCompiled("page") {
			t.Error("still compiled after Parse changed the template")
		}
		var buffer bytes.Buffer
		if err := set.Execute(&buffer, "x"); err != nil ||
			buffer.String() != "<p>x changed</p>" {
			t.Errorf(
				"Execute = %q, %v; want the changed template",
				buffer.String(),
				err,
			)
		}
	})

	t.Run("a Bind error falls back", func(t *testing.T) {
		build := func() *tmpl.Template {
			return tmpl.Must(tmpl.New("refused").Parse(`<i>{{.}}</i>`))
		}
		register(
			t,
			build,
			"refused",
			func(map[string]any) (tmplrt.Exec, error) {
				return nil, errors.New("up is not func(string) string")
			},
		)
		set := build()
		if set.IsCompiled("refused") {
			t.Error("compiled although Bind failed")
		}
		var buffer bytes.Buffer
		if err := set.Execute(&buffer, "<"); err != nil ||
			buffer.String() != "<i>&lt;</i>" {
			t.Errorf(
				"Execute = %q, %v; want html/template's output",
				buffer.String(),
				err,
			)
		}
	})

	compiledPage := build // the subtest below shadows build
	t.Run("Delims are never compiled", func(t *testing.T) {
		build := func() *tmpl.Template {
			return tmpl.Must(
				tmpl.New("delims").Delims("[[", "]]").Parse(`<s>[[.]]</s>`),
			)
		}
		register(t, build, "delims", writes("compiled"))
		set := build()
		if set.IsCompiled("delims") {
			t.Error("a set using Delims was compiled")
		}
		var buffer bytes.Buffer
		if err := set.Execute(&buffer, "d"); err != nil ||
			buffer.String() != "<s>d</s>" {
			t.Errorf(
				"Execute = %q, %v; want html/template's output",
				buffer.String(),
				err,
			)
		}

		// After first use too, although nothing can be parsed with them.
		compiled := compiledPage()
		if err := compiled.Execute(&bytes.Buffer{}, "x"); err != nil ||
			!compiled.IsCompiled("page") {
			t.Fatalf("not compiled before Delims: %v", err)
		}
		compiled.Delims("[[", "]]")
		if compiled.IsCompiled("page") {
			t.Error("still compiled after Delims")
		}
	})
}

// derivedSource calls p inside a <script>, where html/template escapes
// it as p$htmltemplate_stateJS_elementScript, and defines a template of
// that name, which html/template then runs instead of a copy of p.
func derivedSource(body string) string {
	return `<script>var x = {{template "p" .}};</script>` +
		`{{define "p"}}{{.}}{{end}}` +
		`{{define "p$htmltemplate_stateJS_elementScript"}}"` + body + `"{{end}}`
}

func TestDerivedNamesAreNeverCompiled(t *testing.T) {
	build := func(body string) func() *tmpl.Template {
		return func() *tmpl.Template {
			return tmpl.Must(tmpl.New("page").Parse(derivedSource(body)))
		}
	}
	register(t, build("one"), "page", writes(`<script>var x = "one";</script>`))

	// Editing only the derived-name definition leaves the call graph as
	// it was. html/template's output changes; compiled code must not run.
	for _, body := range []string{"one", "two"} {
		set := build(body)()
		if set.IsCompiled("page") {
			t.Errorf(
				"%s: a set defining a derived template name was compiled",
				body,
			)
		}
		var got bytes.Buffer
		if err := set.Execute(&got, "v"); err != nil {
			t.Fatal(err)
		}
		std := htmltemplate.Must(
			htmltemplate.New("page").Parse(derivedSource(body)),
		)
		var want bytes.Buffer
		if err := std.Execute(&want, "v"); err != nil {
			t.Fatal(err)
		}
		if got.String() != want.String() {
			t.Errorf(
				"%s: Execute = %q, html/template %q",
				body,
				got.String(),
				want.String(),
			)
		}
	}
}

// TestNewReplacingATemplate follows html/template's New, which empties a
// replaced template into a namespace of its own: a wrapper kept from
// before must run that, not the replacement's compiled code, and a set
// whose root was replaced must still see all its templates.
func TestNewReplacingATemplate(t *testing.T) {
	const page = `<p>{{template "row" .}}</p>` +
		`{{define "row"}}<i>{{.}}</i>{{end}}`
	const newRow = `<b>{{.}}</b>`

	t.Run("a kept wrapper", func(t *testing.T) {
		replaced := func() (*tmpl.Template, *tmpl.Template) {
			set := tmpl.Must(tmpl.New("page").Parse(page))
			kept := set.Lookup("row")
			tmpl.Must(set.New("row").Parse(newRow))
			return set, kept
		}
		register(
			t,
			func() *tmpl.Template { first, _ := replaced(); return first },
			"row",
			writes("compiled row"),
		)

		set, kept := replaced()
		std := htmltemplate.Must(htmltemplate.New("page").Parse(page))
		stdKept := std.Lookup("row")
		htmltemplate.Must(std.New("row").Parse(newRow))

		var buffer bytes.Buffer
		err := kept.Execute(&buffer, "x")
		var want bytes.Buffer
		stdErr := stdKept.Execute(&want, "x")
		if (err != nil) != (stdErr != nil) || buffer.String() != want.String() {
			t.Errorf(
				"kept wrapper: %q, %v; html/template: %q, %v",
				buffer.String(),
				err,
				want.String(),
				stdErr,
			)
		}
		if kept.IsCompiled("row") {
			t.Error("the kept wrapper runs the replacement's compiled code")
		}
		buffer.Reset()
		if err := set.ExecuteTemplate(&buffer, "row", "x"); err != nil ||
			buffer.String() != "compiled row" {
			t.Errorf(
				"the replacement: %q, %v; want its compiled code",
				buffer.String(),
				err,
			)
		}
	})

	t.Run("the root", func(t *testing.T) {
		replaced := func() (*tmpl.Template, *tmpl.Template) {
			old := tmpl.Must(
				tmpl.New("page").
					Parse(`old` + page[len(`<p>{{template "row" .}}</p>`):]),
			)
			return tmpl.Must(
				old.New("page").Parse(`<p>{{template "row" .}}</p>`),
			), old
		}
		set, _ := replaced()
		want := tmpl.FingerprintOf(
			tmpl.Must(tmpl.New("page").Parse(page)),
			"page",
		)
		if got := tmpl.FingerprintOf(set, "page"); got != want {
			t.Fatalf(
				"fingerprint after replacing the root = %q, "+
					"want the same set built directly, %q",
				got,
				want,
			)
		}
		if tmpl.FingerprintOf(set, "row") == "" {
			t.Error(
				"row is missing from the set's fingerprints " +
					"after replacing the root",
			)
		}

		// New alone, with no Parse after it, must keep the set whole.
		old := tmpl.Must(tmpl.New("page").Parse(page))
		if tmpl.FingerprintOf(old.New("page"), "row") == "" {
			t.Error(
				"row is missing from the set's fingerprints " +
					"after New replaced the root",
			)
		}

		register(
			t,
			func() *tmpl.Template { first, _ := replaced(); return first },
			"page",
			writes("compiled page"),
		)
		set, old = replaced()
		var buffer bytes.Buffer
		if err := set.Execute(&buffer, "x"); err != nil ||
			buffer.String() != "compiled page" {
			t.Errorf(
				"the new root: %q, %v; want its compiled code",
				buffer.String(),
				err,
			)
		}
		if old.IsCompiled("page") {
			t.Error("the replaced root runs the new root's compiled code")
		}
		if err := old.Execute(&bytes.Buffer{}, "x"); err == nil {
			t.Error("the replaced root executed; html/template leaves it empty")
		}
	})
}

// records keeps every log record, at every level.
type records struct {
	mu   sync.Mutex
	list []slog.Record
}

func (handler *records) Enabled(
	context.Context,
	slog.Level,
) bool {
	return true
}
func (handler *records) Handle(_ context.Context, rec slog.Record) error {
	handler.mu.Lock()
	defer handler.mu.Unlock()
	handler.list = append(handler.list, rec)
	return nil
}
func (handler *records) WithAttrs([]slog.Attr) slog.Handler { return handler }
func (handler *records) WithGroup(string) slog.Handler      { return handler }

func (handler *records) withMessage(msg string) []slog.Record {
	handler.mu.Lock()
	defer handler.mu.Unlock()
	var out []slog.Record
	for _, rec := range handler.list {
		if rec.Message == msg {
			out = append(out, rec)
		}
	}
	return out
}

func captureLogs(t *testing.T) *records {
	handler := &records{}
	old := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(old) })
	return handler
}

// attr is the value of one attribute of rec, as text.
func attr(rec slog.Record, key string) string {
	var value string
	rec.Attrs(func(attribute slog.Attr) bool {
		if attribute.Key == key {
			value = attribute.Value.String()
		}
		return true
	})
	return value
}

func TestStaleWarning(t *testing.T) {
	const owner = "example.com/staletest"
	source := `<main>{{template "edited" .}}</main>` +
		`{{define "edited"}}<h1>{{.}}</h1>{{end}}` +
		`{{define "unsupported"}}<h2>{{.}}</h2>{{end}}`
	build := func() *tmpl.Template {
		set := tmpl.New("page")
		tmpl.SetOwner(set, owner)
		return tmpl.Must(set.Parse(source))
	}
	register(t, build, "page", writes("compiled page"))
	tmplrt.RegisterOwner(
		owner,
		[]string{tmpl.FingerprintOf(build(), "unsupported")},
	)

	t.Run("built binary warns once", func(t *testing.T) {
		logs := captureLogs(t)
		set := build()
		for range 3 {
			if err := set.Execute(&bytes.Buffer{}, "x"); err != nil {
				t.Fatal(err)
			}
			err := set.ExecuteTemplate(&bytes.Buffer{}, "edited", "x")
			if err != nil {
				t.Fatal(err)
			}
		}
		got := logs.withMessage(tmpl.StaleMessage)
		if len(got) != 1 {
			t.Fatalf("%d warnings, want 1 per set", len(got))
		}
		if got[0].Level != slog.LevelWarn {
			t.Errorf("level %v, want warn", got[0].Level)
		}
		if got := attr(got[0], "templates"); got != "[edited]" {
			t.Errorf("templates %q: want only the stale one, [edited]", got)
		}
		if got := attr(got[0], "owner"); got != owner {
			t.Errorf("owner %q, want %q", got, owner)
		}
		if !strings.Contains(tmpl.StaleMessage, "rastrillo generate") {
			t.Errorf(
				"message %q does not say to run rastrillo generate",
				tmpl.StaleMessage,
			)
		}
	})

	// The owner left one version of "unsupported" out, by fingerprint;
	// another set's template of that name, edited, is stale.
	t.Run(
		"a name left out does not exempt another template of that name",
		func(t *testing.T) {
			logs := captureLogs(t)
			set := tmpl.New("page")
			tmpl.SetOwner(set, owner)
			edited := strings.Replace(source, "<h2>", "<h3>", 1)
			tmpl.Must(set.Parse(edited))
			if err := set.Execute(&bytes.Buffer{}, "x"); err != nil {
				t.Fatal(err)
			}
			got := logs.withMessage(tmpl.StaleMessage)
			if len(got) != 1 ||
				attr(got[0], "templates") != "[edited unsupported]" {
				t.Errorf(
					"got %v, want one warning naming edited and unsupported",
					got,
				)
			}
		},
	)

	t.Run("rastrillo dev logs at debug", func(t *testing.T) {
		t.Setenv(tmpl.DevEnv, "1")
		logs := captureLogs(t)
		if err := build().Execute(&bytes.Buffer{}, "x"); err != nil {
			t.Fatal(err)
		}
		got := logs.withMessage(tmpl.StaleMessage)
		if len(got) != 1 || got[0].Level != slog.LevelDebug {
			t.Errorf("got %v, want one debug record", got)
		}
	})

	t.Run("an owner without generated code is quiet", func(t *testing.T) {
		logs := captureLogs(t)
		set := tmpl.New("page")
		tmpl.SetOwner(set, "example.com/nevergenerated")
		tmpl.Must(set.Parse(source))
		if err := set.Execute(&bytes.Buffer{}, "x"); err != nil {
			t.Fatal(err)
		}
		if got := logs.withMessage(tmpl.StaleMessage); len(got) != 0 {
			t.Errorf(
				"%d warnings for an owner that never ran rastrillo generate",
				len(got),
			)
		}
	})
}

func TestShadow(t *testing.T) {
	t.Setenv(tmpl.ShadowEnv, "1")
	source := `{{define "row"}}<td>{{.}}</td>{{end}}` +
		`<tr>{{template "row" .}}</tr>`
	build := func(name string) func() *tmpl.Template {
		return func() *tmpl.Template {
			return tmpl.Must(tmpl.New(name).Parse(source))
		}
	}

	register(t, build("shadowpage"), "row", writes("<td>wrong</td>"))
	var buffer bytes.Buffer
	err := build("shadowpage")().ExecuteTemplate(&buffer, "row", "right")
	if err == nil {
		t.Fatal(
			"a compiled Exec that differs from html/template " +
				"passed shadow mode",
		)
	}
	for _, want := range []string{`"shadowpage"`, `"row"`, "byte 4"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %s", err, want)
		}
	}

	register(
		t,
		build("matchingpage"),
		"matchingpage",
		writes("<tr><td>same</td></tr>"),
	)
	buffer.Reset()
	if err := build("matchingpage")().Execute(&buffer, "same"); err != nil ||
		buffer.String() != "<tr><td>same</td></tr>" {
		t.Errorf(
			"matching compiled output: %q, %v; want it to pass",
			buffer.String(),
			err,
		)
	}
}

// Defined is Lookup(name) != nil, so code that asks either type whether
// a template exists, such as live's regions, gets one answer, and a
// template made with New but never parsed errors on execute in both.
func TestDefinedIsLookup(t *testing.T) {
	std := htmltemplate.Must(
		htmltemplate.New("root").Parse(`{{define "body"}}x{{end}}`),
	)
	std.New("empty")
	set := tmpl.Must(tmpl.New("root").Parse(`{{define "body"}}x{{end}}`))
	set.New("empty")

	for _, name := range []string{"root", "body", "empty", "missing"} {
		got, want := set.Defined(name), std.Lookup(name) != nil
		if got != want {
			t.Errorf(
				"Defined(%q) = %v, html/template's Lookup finds it: %v",
				name,
				got,
				want,
			)
		}
	}
	var want, got bytes.Buffer
	stdErr := std.ExecuteTemplate(&want, "empty", nil)
	err := set.ExecuteTemplate(&got, "empty", nil)
	if stdErr == nil || err == nil || err.Error() != stdErr.Error() {
		t.Errorf(
			"executing a template never parsed: tmpl %v, html/template %v",
			err,
			stdErr,
		)
	}
}

// With TraceEnv set, every execution logs which path it ran: the only
// way a test outside the framework, such as scaffold-smoke's served
// page, can tell compiled code ran, since ui.Pages does not hand out
// its sets. Off, nothing is logged.
func TestTrace(t *testing.T) {
	build := func() *tmpl.Template {
		return tmpl.Must(
			tmpl.New("traced").
				Parse(`<p>{{.}}</p>{{define "plain"}}{{.}}{{end}}`),
		)
	}
	register(t, build, "traced", writes("compiled"))
	logs := captureLogs(t)
	set := build()
	if err := set.Execute(io.Discard, "x"); err != nil {
		t.Fatal(err)
	}
	if got := logs.withMessage(tmpl.TraceMessage); len(got) != 0 {
		t.Fatalf("logged %d trace lines with tracing off", len(got))
	}

	t.Cleanup(tmpl.SetTrace(true))
	if err := set.Execute(io.Discard, "x"); err != nil {
		t.Fatal(err)
	}
	if err := set.ExecuteTemplate(io.Discard, "plain", "x"); err != nil {
		t.Fatal(err)
	}
	got := logs.withMessage(tmpl.TraceMessage)
	if len(got) != 2 {
		t.Fatalf("logged %d trace lines, want one per execution", len(got))
	}
	wants := [][2]string{{"traced", "true"}, {"plain", "false"}}
	for index, want := range wants {
		if attr(got[index], "template") != want[0] ||
			attr(got[index], "compiled") != want[1] {
			t.Errorf(
				"trace line %d: template=%s compiled=%s, want %s %s",
				index,
				attr(got[index], "template"),
				attr(got[index], "compiled"),
				want[0],
				want[1],
			)
		}
	}
}
