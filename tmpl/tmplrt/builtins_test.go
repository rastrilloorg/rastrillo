package tmplrt

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"text/template"
)

type named string

// builtinValues are the arguments every builtin is called with. A
// template reads each from a map, so compiled code and text/template
// both see a value held in an interface, as a field of the data is.
func builtinValues() map[string]any {
	one := 1
	ch := make(chan int, 2)
	ch <- 1
	return map[string]any{
		"int": 1, "two": 2, "neg": -1, "uint": uint(1), "uint8": uint8(2),
		"big":   uint64(1 << 63),
		"float": 1.0, "half": 0.5, "complex": 1i,
		"str": "abc", "str2": "b", "named": named("abc"), "empty": "",
		"true": true, "false": false, "nil": nil,
		"slice": []int{10, 20, 30}, "nilslice": []int(nil),
		"anys":  []any{nil, 1, "x"},
		"array": [2]string{"x", "y"}, "map": map[string]int{"a": 1},
		"intmap": map[int]string{1: "one"},
		"nilmap": map[string]int(nil), "ptr": &one, "nilptr": (*int)(nil),
		"struct": struct{ A int }{1},
		"func":   func(text string) string { return "<" + text + ">" },
		"errfunc": func() (int, error) {
			return 0, errors.New("errfunc failed")
		},
		"panicfunc": func() int { panic("panicfunc panicked") }, "chan": ch,
		"rv": reflect.ValueOf(7),
	}
}

// fromMap is how compiled code holds the value of .name in data.
func fromMap(data map[string]any, name string) reflect.Value {
	return reflect.ValueOf(data).MapIndex(reflect.ValueOf(name))
}

// render is what text/template prints for a builtin's result, which may
// be a value it cannot print.
func render(value reflect.Value) (string, error) {
	var output strings.Builder
	printer := template.Must(template.New("").Parse("{{.}}"))
	err := printer.Execute(&output, Arg(value))
	return output.String(), err
}

// sameAs runs text, a call of one builtin on fields of data, through
// text/template, and wants got to print the same or both to fail.
func sameAs(
	t *testing.T, text string, data map[string]any,
	got reflect.Value, gotErr error,
) {
	t.Helper()
	var want strings.Builder
	wantErr := template.Must(template.New("").Parse(text)).
		Execute(&want, data)
	printed := ""
	if gotErr == nil {
		printed, gotErr = render(got)
	}
	switch {
	case wantErr != nil && gotErr == nil:
		t.Errorf("%s: text/template fails (%v), tmplrt gives %s",
			text, wantErr, printed)
	case wantErr == nil && gotErr != nil:
		t.Errorf("%s: tmplrt fails (%v), text/template gives %s",
			text, gotErr, want.String())
	case wantErr == nil && printed != want.String():
		t.Errorf("%s: tmplrt gives %s, text/template %s",
			text, printed, want.String())
	}
}

func names(data map[string]any) []string {
	out := make([]string, 0, len(data))
	for name := range data {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

func boolValue(truth bool, err error) (reflect.Value, error) {
	return reflect.ValueOf(truth), err
}

func TestComparisonsMatchTextTemplate(t *testing.T) {
	data := builtinValues()
	field := func(name string) reflect.Value { return fromMap(data, name) }
	compare := map[string]func(left, right reflect.Value) (bool, error){
		"eq": func(left, right reflect.Value) (bool, error) {
			return Eq(left, right)
		},
		"ne": Ne, "lt": Lt, "le": Le, "gt": Gt, "ge": Ge,
	}
	for _, fn := range slices.Sorted(func(yield func(string) bool) {
		for name := range compare {
			if !yield(name) {
				return
			}
		}
	}) {
		for _, left := range names(data) {
			for _, right := range names(data) {
				got, err := boolValue(compare[fn](field(left), field(right)))
				text := fmt.Sprintf("{{%s .%s .%s}}", fn, left, right)
				sameAs(t, text, data, got, err)
			}
		}
	}
	// eq's many-argument form, and with one argument.
	for _, left := range names(data) {
		got, err := boolValue(
			Eq(field(left), field("str"), field("int"), field(left)))
		text := fmt.Sprintf("{{eq .%s .str .int .%s}}", left, left)
		sameAs(t, text, data, got, err)
		got, err = boolValue(Eq(field(left)))
		sameAs(t, fmt.Sprintf("{{eq .%s}}", left), data, got, err)
	}
}

func TestNotAndLenMatchTextTemplate(t *testing.T) {
	data := builtinValues()
	for _, name := range names(data) {
		value := fromMap(data, name)
		sameAs(t, fmt.Sprintf("{{not .%s}}", name), data,
			reflect.ValueOf(Not(value)), nil)
		length, err := Len(value)
		sameAs(t, fmt.Sprintf("{{len .%s}}", name), data,
			reflect.ValueOf(length), err)
		text := fmt.Sprintf("{{if .%s}}true{{else}}false{{end}}", name)
		sameAs(t, text, data, reflect.ValueOf(Truth(value)), nil)
	}
}

func TestIndexAndSliceMatchTextTemplate(t *testing.T) {
	data := builtinValues()
	field := func(name string) reflect.Value { return fromMap(data, name) }
	indexes := []string{
		"int", "neg", "uint", "uint8", "two", "big", "str", "nil", "float",
		"rv",
	}
	for _, item := range names(data) {
		got, err := Index(field(item))
		sameAs(t, fmt.Sprintf("{{index .%s}}", item), data, got, err)
		got, err = Slice(field(item))
		sameAs(t, fmt.Sprintf("{{slice .%s}}", item), data, got, err)
		for _, first := range indexes {
			got, err := Index(field(item), field(first))
			text := fmt.Sprintf("{{index .%s .%s}}", item, first)
			sameAs(t, text, data, got, err)
			got, err = Slice(field(item), field(first))
			text = fmt.Sprintf("{{slice .%s .%s}}", item, first)
			sameAs(t, text, data, got, err)
			for _, second := range indexes {
				got, err := Slice(field(item), field(first), field(second))
				text := fmt.Sprintf("{{slice .%s .%s .%s}}",
					item, first, second)
				sameAs(t, text, data, got, err)
				got, err = Slice(field(item), field(first), field(second),
					field("two"))
				text = fmt.Sprintf("{{slice .%s .%s .%s .two}}",
					item, first, second)
				sameAs(t, text, data, got, err)
			}
		}
	}
	got, err := Index(field("map"), reflect.ValueOf("a"))
	sameAs(t, `{{index .map "a"}}`, data, got, err)
	got, err = Index(field("map"), reflect.ValueOf("missing"))
	sameAs(t, `{{index .map "missing"}}`, data, got, err)
	got, err = Index(field("intmap"), reflect.ValueOf(1))
	sameAs(t, `{{index .intmap 1}}`, data, got, err)
}

func TestCallMatchesTextTemplate(t *testing.T) {
	data := builtinValues()
	for _, fn := range names(data) {
		got, err := CallBuiltin("."+fn, fromMap(data, fn))
		sameAs(t, fmt.Sprintf("{{call .%s}}", fn), data, got, err)
		for _, arg := range names(data) {
			got, err := CallBuiltin("."+fn,
				fromMap(data, fn), fromMap(data, arg))
			text := fmt.Sprintf("{{call .%s .%s}}", fn, arg)
			sameAs(t, text, data, got, err)
		}
	}
}

func TestPrintAndEscapersMatchTextTemplate(t *testing.T) {
	data := builtinValues()
	for _, name := range []string{
		"str", "int", "nil", "named", "anys", "ptr", "nilptr", "func",
	} {
		value := Arg(fromMap(data, name))
		for fn, got := range map[string]string{
			"print":    Print(value, value),
			"println":  Println(value, 1),
			"html":     HTMLEscaperBuiltin(value, "<&>"),
			"js":       JSEscaper(value, `"'`),
			"urlquery": URLQueryEscaper(value, "a b"),
		} {
			argument := map[string]string{
				"print": "." + name, "println": "1", "html": `"<&>"`,
				"js": "`\"'`", "urlquery": `"a b"`,
			}[fn]
			text := fmt.Sprintf(`{{%s .%s %s}}`, fn, name, argument)
			sameAs(t, text, data, reflect.ValueOf(got), nil)
		}
		text := fmt.Sprintf(`{{printf "%%v|%%d" .%s .%s}}`, name, name)
		printed := Printf("%v|%d", value, value)
		sameAs(t, text, data, reflect.ValueOf(printed), nil)
	}
}
