package ui

import (
	"bytes"
	"encoding/json"
	"html"
	"html/template"
	"io"
	"net/url"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/currency"
)

// Recorded from ui.Funcs before the locale entries became dispatchers,
// by executing legacySrc over edgeData: "unchanged" is checked against
// what the old function map really rendered, not a re-derivation of it.
const (
	wantLegacyEN = "[][][&lt;nil&gt;][1,234,567][1,234.5][1,234,567][1234567][18,446,744,073,709,551,615][Cancel][Reference: &lt;b&gt;x&lt;/b&gt;][Reference: <bdi>&lt;b&gt;x&lt;/b&gt;</bdi>]"
	wantLegacyES = "[][][&lt;nil&gt;][1.234.567][1.234,5][1.234.567][1234567][18.446.744.073.709.551.615][Cancel][Reference: &lt;b&gt;x&lt;/b&gt;][Reference: <bdi>&lt;b&gt;x&lt;/b&gt;</bdi>]"
)

const legacySrc = `[{{number .Nil}}][{{displayValue .Nil}}][{{number .NilP}}][{{number .N}}][{{number .F}}][{{number .S}}][{{displayValue .S}}][{{number .U}}][{{T "rastrillo.ui.cancel"}}][{{Tf "rastrillo.ui.error_ref" "ref" .Evil}}][{{Tbdi "rastrillo.ui.error_ref" "ref" .Evil}}]`

// The same calls, migrated, with no localizer in the data: what an app
// still rendering with ui.Funcs gets from a migrated partial.
const migratedSrc = `{{$L := localizer $}}[{{number $L .Nil}}][{{displayValue $L .Nil}}][{{number $L .NilP}}][{{number $L .N}}][{{number $L .F}}][{{number $L .S}}][{{displayValue $L .S}}][{{number $L .U}}][{{T $L "rastrillo.ui.cancel"}}][{{Tf $L "rastrillo.ui.error_ref" "ref" .Evil}}][{{Tbdi $L "rastrillo.ui.error_ref" "ref" .Evil}}]`

func edgeData() map[string]any {
	var nilp *int
	return map[string]any{"Nil": nil, "NilP": nilp, "N": 1234567, "F": 1234.5, "S": "1234567", "U": uint64(18446744073709551615), "Evil": "<b>x</b>"}
}

func execTmpl(t *testing.T, funcs template.FuncMap, src string, data any) string {
	t.Helper()
	var b bytes.Buffer
	if err := template.Must(template.New("x").Funcs(funcs).Parse(src)).Execute(&b, data); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestLegacyAndNilLocalizerUnchanged(t *testing.T) {
	for _, c := range []struct {
		opts []Option
		want string
	}{{nil, wantLegacyEN}, {[]Option{WithLocale("es")}, wantLegacyES}} {
		if got := execTmpl(t, Funcs(c.opts...), legacySrc, edgeData()); got != c.want {
			t.Errorf("legacy form changed:\n got %q\nwant %q", got, c.want)
		}
		if got := execTmpl(t, Funcs(c.opts...), migratedSrc, edgeData()); got != c.want {
			t.Errorf("migrated form with no L differs from today's output:\n got %q\nwant %q", got, c.want)
		}
	}
}

func TestLocalizerFormUsesTheLocalizer(t *testing.T) {
	es := NewLocalizer("es", "es", func(k string, args ...any) string {
		if k == "app.hello" {
			return "Hola"
		}
		return k
	})
	src := `{{$L := localizer $}}{{T $L "app.hello"}}|{{number $L .N}}|{{money $L .P .C}}|{{money $L .Nil .C}}|{{template "p" (withL (dict "X" 1) $L)}}{{define "p"}}{{$L := localizer $}}{{T $L "app.hello"}}{{end}}`
	var nilAmount *int64
	got := execTmpl(t, Funcs(), src, map[string]any{"L": es, "N": 1234567, "P": int64(128450), "C": "EUR", "Nil": nilAmount})
	if want := "Hola|1.234.567|1284,50 €||Hola"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	got = execTmpl(t, Funcs(), `{{$L := localizer $}}{{T $L "app.hello"}}`, struct{ Localized }{Localized{L: es}})
	if got != "Hola" {
		t.Errorf("struct path: %q", got)
	}
}

func TestMoneyArgumentErrorsStopTheTemplate(t *testing.T) {
	es := NewLocalizer("es", "es", func(k string, _ ...any) string { return k })
	for name, src := range map[string]string{
		"legacy form":   `{{money .P .C}}`,
		"missing code":  `{{$L := localizer $}}{{money $L .P}}`,
		"float":         `{{$L := localizer $}}{{money $L .F .C}}`,
		"string":        `{{$L := localizer $}}{{money $L "12" .C}}`,
		"huge unsigned": `{{$L := localizer $}}{{money $L .U .C}}`,
		"unsupported":   `{{$L := localizer $}}{{money $L .P "KDW"}}`,
	} {
		tmpl := template.Must(template.New("x").Funcs(Funcs()).Parse(src))
		err := tmpl.Execute(io.Discard, map[string]any{"L": es, "P": int64(1), "C": "EUR", "F": 1.5, "U": uint64(1 << 63)})
		if err == nil {
			t.Errorf("%s: executed without error", name)
		}
	}
}

// Review Focus 4: money's output in every html/template context. The
// formatter's output carries no-break spaces and bidi marks, never
// markup; the escaper must still treat it as text everywhere.
func TestMoneyEscapesInEveryContext(t *testing.T) {
	ar := NewLocalizer("ar", "ar", func(k string, _ ...any) string { return k })
	want, _ := currency.Format("ar", 128450, "SAR")
	src := `{{$L := localizer $}}<p>{{money $L .P .C}}</p><a title="{{money $L .P .C}}" href="/pay?amount={{money $L .P .C}}">x</a><script>var v = {{money $L .P .C}};</script>`
	out := execTmpl(t, Funcs(), src, map[string]any{"L": ar, "P": int64(128450), "C": "SAR"})
	if !strings.Contains(out, "<p>"+template.HTMLEscapeString(want)+"</p>") {
		t.Errorf("text context: %q", out)
	}
	if !strings.Contains(out, `title="`+template.HTMLEscapeString(want)+`"`) {
		t.Errorf("attribute context: %q", out)
	}
	js, _ := json.Marshal(want)
	if !strings.Contains(out, "var v = "+string(js)+";") {
		t.Errorf("script context: %q, want a JSON string %s", out, js)
	}
	i := strings.Index(out, `href="`) + len(`href="`)
	u, err := url.Parse(html.UnescapeString(out[i : i+strings.Index(out[i:], `"`)]))
	if err != nil || u.Query().Get("amount") != want {
		t.Errorf("URL context: %q decodes to %q, %v", out, u.Query().Get("amount"), err)
	}
}

func TestFuncsKeepsEveryEntry(t *testing.T) {
	m := Funcs()
	for _, name := range append(append([]string{}, LocaleFuncs...), "localizer", "withL", "dict", "list", "menuGroup",
		"searchClear", "icon", "iconAssets", "opt", "stageArt", "displayURL", "safeHref", "rowMenuItems") {
		if m[name] == nil {
			t.Errorf("Funcs lost %q: shipped partials stop parsing without it", name)
		}
	}
}

// html/template checked the old fixed signatures at Execute; a variadic
// any checks nothing, so each dispatcher must refuse a malformed call
// itself, naming the function, rather than render a key or a number
// from the wrong argument.
func TestLocaleFuncArgumentErrorsNameTheFunction(t *testing.T) {
	es := NewLocalizer("es", "es", func(k string, _ ...any) string { return k })
	for src, want := range map[string]string{
		`{{T}}`:                              "T: 0 arguments, want 1",
		`{{T 5}}`:                            "T: argument 1 is int, want string",
		`{{$L := localizer $}}{{T $L}}`:      "T: 1 arguments, want 2",
		`{{$L := localizer $}}{{Tf $L 5}}`:   "Tf: argument 2 is int, want string",
		`{{Tbdi .Nil}}`:                      "Tbdi: argument 1 is <nil>, want string",
		`{{number}}`:                         "number: 0 arguments, want 1",
		`{{number 1 2}}`:                     "number: 2 arguments, want 1",
		`{{$L := localizer $}}{{number $L}}`: "number: 1 arguments, want 2",
		`{{$L := localizer $}}{{displayValue $L 1 2}}`: "displayValue: 3 arguments, want 2",
		`{{dateWords 1}}`:                              "dateWords: 1 arguments, want 0",
		`{{$L := localizer $}}{{dateWords $L 1}}`:      "dateWords: 2 arguments, want 1",
		`{{money}}`:                                    "money: the first argument must be $L (money has no legacy form)",
		`{{$L := localizer $}}{{money $L 1}}`:          "money: want $L, an amount and a currency; got 1 arguments after $L",
		`{{$L := localizer $}}{{money $L 1 2}}`:        "money: argument 3 is int, want string",
		`{{$L := localizer $}}{{money $L .F "EUR"}}`:   "amount is not an integer count of minor units: got float64",
		`{{$L := localizer $}}{{money $L .Big "EUR"}}`: "amount does not fit in int64",
		`{{$L := localizer $}}{{money $L 1 "KDW"}}`:    "currency code is not in the storage set",
	} {
		tmpl := template.Must(template.New("x").Funcs(Funcs()).Parse(src))
		err := tmpl.Execute(io.Discard, map[string]any{"L": es, "Nil": nil, "F": 1.5, "Big": uint64(1 << 63)})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want it to contain %q", src, err, want)
		}
	}
}

// The nil-localizer money falls back to the options' locale: a migrated
// partial reached through an app that still renders with
// ui.Funcs(WithLocale("es")) formats Spanish money, not the default's.
func TestMoneyWithANilLocalizerUsesWithLocale(t *testing.T) {
	want, err := currency.Format("es", 128450, "EUR")
	if err != nil {
		t.Fatal(err)
	}
	got := execTmpl(t, Funcs(WithLocale("es")), `{{$L := localizer $}}{{money $L .P .C}}`, map[string]any{"P": int64(128450), "C": "EUR"})
	if got != template.HTMLEscapeString(want) {
		t.Errorf("got %q, want %q", got, want)
	}
}
