package codeview

import (
	"errors"
	"html/template"
	"math"
	"strconv"
	"strings"
	"testing"
	ttemplate "text/template"
)

// run executes a call's source against a one-partial tree that prints
// its dot as Go syntax, so a test can see exactly the value the call
// hands the partial.
func run(t *testing.T, src string, dot map[string]any) string {
	t.Helper()
	tmpl := ttemplate.Must(ttemplate.New("").Funcs(ttemplate.FuncMap{
		"dict": func(kv ...any) map[string]any {
			m := map[string]any{}
			for i := 0; i < len(kv); i += 2 {
				m[kv[i].(string)] = kv[i+1]
			}
			return m
		},
		"list": func(v ...any) []any { return v },
	}).Parse(`{{define "p"}}{{printf "%#v" .}}{{end}}{{define "call"}}` + src + `{{end}}`))
	var b strings.Builder
	if err := tmpl.ExecuteTemplate(&b, "call", dot); err != nil {
		t.Fatalf("executing %s: %v", src, err)
	}
	return b.String()
}

func TestCallWritesEachKindOfValue(t *testing.T) {
	src, dot, err := Call("p", map[string]any{"Percent": 140, "Text": "700/500"}, []string{"Percent", "Text"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{{template "p" dict "Percent" 140 "Text" "700/500"}}`; src != want {
		t.Errorf("got %s, want %s", src, want)
	}
	if len(dot) != 0 {
		t.Errorf("an unbound call has dot %v, want empty", dot)
	}
	for _, c := range []struct {
		data any
		want string
	}{
		{"Post saved.", `{{template "p" "Post saved."}}`},
		{7, `{{template "p" 7}}`},
		{true, `{{template "p" true}}`},
		{map[string]any{"F": 1.5, "U": uint8(3), "B": false}, `{{template "p" dict "B" false "F" 1.5 "U" 3}}`},
		{map[string]any{"Hidden": [][2]string{{"csrf", "t"}, {"id", "1"}}}, `{{template "p" dict "Hidden" (list (list "csrf" "t") (list "id" "1"))}}`},
		{map[string]any{"M": map[string]any{"b": 1, "a": 2}}, `{{template "p" dict "M" (dict "a" 2 "b" 1)}}`},
		{map[string]any{}, `{{template "p" dict}}`},
	} {
		got, _, err := Call("p", c.data, nil, nil)
		if err != nil {
			t.Errorf("%#v: %v", c.data, err)
			continue
		}
		if got != c.want {
			t.Errorf("%#v:\n got %s\nwant %s", c.data, got, c.want)
		}
	}
}

func TestCallOrdersKeysByThePartialsOwnList(t *testing.T) {
	src, _, err := Call("p", map[string]any{"A": 1, "Z": 2, "Label": "x", "Name": "n"}, []string{"Name", "Label", "Missing"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{{template "p" dict "Name" "n" "Label" "x" "A" 1 "Z" 2}}`; src != want {
		t.Errorf("got %s, want %s", src, want)
	}
}

func TestCallBreaksALongCallOnePairPerLine(t *testing.T) {
	data := map[string]any{
		"Name": "digest", "Type": "radio",
		"Options": []any{
			map[string]any{"Value": "daily", "Title": "Daily digest"},
			map[string]any{"Value": "weekly", "Title": "Weekly digest"},
		},
	}
	src, _, err := Call("choice-field", data, []string{"Name", "Type", "Options"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := `{{template "choice-field" dict
    "Name" "digest"
    "Type" "radio"
    "Options" (list
        (dict "Title" "Daily digest" "Value" "daily")
        (dict "Title" "Weekly digest" "Value" "weekly"))}}`
	if src != want {
		t.Errorf("got\n%s\nwant\n%s", src, want)
	}
	if got, one := run(t, strings.Replace(src, `"choice-field"`, `"p"`, 1), nil), run(t, `{{template "p" dict "Name" "digest" "Type" "radio" "Options" (list (dict "Title" "Daily digest" "Value" "daily") (dict "Title" "Weekly digest" "Value" "weekly"))}}`, nil); got != one {
		t.Errorf("the broken layout executes differently from the one-line form:\n%s\n%s", got, one)
	}
}

func TestCallBindsAPlaceholder(t *testing.T) {
	items := []struct{ Code string }{{"en"}, {"ga"}}
	src, dot, err := Call("p", map[string]any{"Items": items, "Return": "/"}, []string{"Items", "Return"}, map[string]string{"Items": "Locales"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{{template "p" dict "Items" .Locales "Return" "/"}}`; src != want {
		t.Errorf("got %s, want %s", src, want)
	}
	if _, ok := dot["Locales"]; !ok || len(dot) != 1 {
		t.Errorf("dot is %v, want exactly Locales", dot)
	}
}

func TestCallRefusesWhatItCannotWriteTruthfully(t *testing.T) {
	var nilPtr *int
	for name, data := range map[string]any{
		"a struct":          map[string]any{"V": struct{ A int }{1}},
		"a pointer":         map[string]any{"V": new(int)},
		"nil":               map[string]any{"V": nil},
		"a nil pointer":     map[string]any{"V": nilPtr},
		"template.HTML":     map[string]any{"V": template.HTML("<b>x</b>")},
		"a func":            map[string]any{"V": func() {}},
		"int keys":          map[int]any{1: "x"},
		"a named int":       map[string]any{"V": level(2)},
		"a named bool":      map[string]any{"V": flag(true)},
		"a named int alone": level(2),
		"NaN":               map[string]any{"V": math.NaN()},
		"+Inf":              map[string]any{"V": math.Inf(1)},
		"-Inf":              map[string]any{"V": math.Inf(-1)},
	} {
		if _, _, err := Call("p", data, nil, nil); !errors.Is(err, ErrUnwritable) {
			t.Errorf("%s: err = %v, want ErrUnwritable", name, err)
		}
	}
}

// Strings that are awkward as template literals still
// round-trip through the call to the exact value.
func TestCallQuotesAwkwardStrings(t *testing.T) {
	for _, s := range []string{`say "hi"`, `back\slash`, "two\nlines", "{{not an action}}", "−6 … ✕", "tab\there", "`tick`"} {
		src, _, err := Call("p", map[string]any{"S": s}, nil, nil)
		if err != nil {
			t.Fatalf("%q: %v", s, err)
		}
		if got, want := run(t, src, nil), `map[string]interface {}{"S":`+strconv.Quote(s)+`}`; got != want {
			t.Errorf("%q: the call hands the partial %s, want %s", s, got, want)
		}
	}
}

// level is a named int with a String method: written as the plain int
// the call can spell, the partial would receive an int and print 2
// where the preview printed "high".
type level int

func (l level) String() string { return [...]string{"low", "mid", "high"}[l] }

type flag bool

// A float is written so the template parses it back as the same
// float64: an integral one written "3" would reach the partial as the
// int 3, and one past 2^64 written in full would not parse at all.
func TestCallWritesFloatsThatRoundTrip(t *testing.T) {
	tmpl := func(src string) string {
		var b strings.Builder
		tt, err := ttemplate.New("").Funcs(ttemplate.FuncMap{
			"dict": func(kv ...any) map[string]any { return map[string]any{kv[0].(string): kv[1]} },
			"bits": func(v any) string {
				f, ok := v.(float64)
				if !ok {
					return "not a float64"
				}
				return strconv.FormatUint(math.Float64bits(f), 16)
			},
		}).Parse(`{{define "p"}}{{printf "%T" .F}} {{bits .F}}{{end}}{{define "call"}}` + src + `{{end}}`)
		if err != nil {
			return "a parse error: " + err.Error()
		}
		if err := tt.ExecuteTemplate(&b, "call", nil); err != nil {
			return "an execution error: " + err.Error()
		}
		return b.String()
	}
	for _, f := range []float64{3, 0, math.Copysign(0, -1), -0.5, 1.5, 1e21, 1e-7, 1 << 53, 1<<53 + 2, math.MaxFloat64, math.SmallestNonzeroFloat64} {
		src, _, err := Call("p", map[string]any{"F": f}, nil, nil)
		if err != nil {
			t.Errorf("%v: %v", f, err)
			continue
		}
		if got, want := tmpl(src), "float64 "+strconv.FormatUint(math.Float64bits(f), 16); got != want {
			t.Errorf("%v: %s hands the partial %s, want %s", f, src, got, want)
		}
	}
}

// The one-line limit is a reader's columns, so a call in Bengali or
// Japanese is measured in characters, not in its UTF-8 bytes.
func TestCallMeasuresTheLineInCharacters(t *testing.T) {
	s := strings.Repeat("é", 50)
	src, _, err := Call("p", map[string]any{"S": s}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{{template "p" dict "S" "` + s + `"}}`; src != want {
		t.Errorf("a 78-character call was broken:\n%s", src)
	}
	src, _, err = Call("p", map[string]any{"S": s + "ééé"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(src, "\n") {
		t.Errorf("an 81-character call stayed on one line:\n%s", src)
	}
}
