package corpus

import (
	"errors"
	"fmt"
	"html/template"
	"strings"
)

// The function cases run each template twice: once with functions whose
// types the generated code can spell, which it calls directly, and once
// with functions whose result type is declared inside a function, which
// it cannot name and calls by reflection. Both must fail where
// html/template does, for an error and for a panic alike.

// directFuncs are called directly: every type they mention is the
// package's or predeclared.
func directFuncs() template.FuncMap {
	return template.FuncMap{
		"upper": strings.ToUpper,
		// check fails for an empty string.
		"check": func(text string) (string, error) {
			if text == "" {
				return "", errors.New("check: empty <string>")
			}
			return "checked " + text, nil
		},
		// boom panics for an empty string.
		"boom": func(text string) string {
			if text == "" {
				panic("boom: empty string")
			}
			return "no boom " + text
		},
		"join": func(sep string, parts ...any) string {
			return joined(sep, parts)
		},
		"kindOf": func(value kind) string { return "kind " + string(value) },
		"count": func(count int, fraction float64, ok bool) string {
			return fmt.Sprint(count, fraction, ok)
		},
		"html2": func(text string) template.HTML {
			return template.HTML("<b>" + text + "</b>")
		},
		"self":    func(value any) any { return value },
		"person":  func(name string) *person { return &person{Name: name} },
		"err":     func() error { return errors.New("<returned error>") },
		"labelOf": func(text string) label { return label(text) },
		"nothing": func() any { return nil },
		"len":     func(value any) int { return 42 }, // shadows the builtin
	}
}

// joined is what join returns on both paths: parts printed, with sep
// between them.
func joined(sep string, parts []any) string {
	var builder strings.Builder
	for index, part := range parts {
		if index > 0 {
			builder.WriteString(sep)
		}
		fmt.Fprint(&builder, part)
	}
	return builder.String()
}

// reflectedFuncs has the same names as directFuncs, but each mentions a
// type declared in this function, where generated code cannot name it.
func reflectedFuncs() template.FuncMap {
	type local string
	// anything takes any argument, as any does, under a name of its own.
	type anything interface{}
	type wrapped struct{ V any }
	return template.FuncMap{
		"upper": func(text string) local {
			return local(strings.ToUpper(text))
		},
		"check": func(text string) (local, error) {
			if text == "" {
				return "", errors.New("check: empty <string>")
			}
			return local("checked " + text), nil
		},
		"boom": func(text string) local {
			if text == "" {
				panic(errors.New("boom: empty string"))
			}
			return local("no boom " + text)
		},
		"join": func(sep string, parts ...any) local {
			return local(joined(sep, parts))
		},
		"kindOf": func(value kind) local { return local("kind " + value) },
		"count": func(count int, fraction float64, ok bool) local {
			return local(fmt.Sprint(count, fraction, ok))
		},
		"html2": func(text anything) template.HTML {
			return template.HTML(fmt.Sprint("<b>", text, "</b>"))
		},
		"self": func(value anything) wrapped { return wrapped{value} },
		"person": func(name anything) *person {
			return &person{Name: fmt.Sprint(name)}
		},
		"err":     func() (local, error) { return "", nil },
		"labelOf": func(text string) local { return local(text) },
		"nothing": func() local { return "" },
		"len":     func(value any) local { return "42" },
	}
}

// funcCases are the function cases on both paths, direct first.
func funcCases() []Case {
	var cases []Case
	for _, path := range []struct {
		name  string
		funcs template.FuncMap
	}{
		{"direct", directFuncs()},
		{"reflected", reflectedFuncs()},
	} {
		cases = append(cases, funcPathCases(path.name, path.funcs)...)
	}
	return cases
}

// callsText calls every function, piped and not.
const callsText = `{{upper .Title}}|{{.Title | upper}}|` +
	`{{join "," 1 2.5 "s" true nil .Count}}|{{join "-"}}|` +
	`{{kindOf "a"}}|{{kindOf .Kind}}|{{count 1 2 true}}|` +
	`{{count 3 1.5 false}}|` +
	`<b title="{{html2 .Title}}">{{html2 .Title}}</b>|{{self .Any}}|` +
	`{{(person .Title).Name}}|{{(person "x").Initial}}|` +
	`{{labelOf "l"}}|{{nothing}}|{{len .Items}}`

// funcPathCases are the function cases with funcs, the path called
// pathName.
func funcPathCases(pathName string, funcs template.FuncMap) []Case {
	config := setup{funcs: funcs}
	newCase := func(name, text string, data ...any) Case {
		if data == nil {
			data = variants()
		}
		name = "func-" + name + "-" + pathName
		return fromSet(name, name, config, text, data...)
	}
	cases := []Case{
		newCase("calls", callsText),
		newCase("args", `{{upper (print .Count)}}|{{upper .Kind}}`),
		newCase("error", `<p>{{check .Title}}</p>`),
		newCase("error-in-if", `{{if check .Title}}yes{{end}}`),
		newCase("returned-error", `{{err}}`, nil),
		newCase("panic", `<p>{{boom .Title}}</p>`),
		newCase("panic-in-arg", `{{upper (boom .Title)}}`),
		newCase("panic-in-range", `{{range .Items}}{{boom .Name}},{{end}}`),
	}
	// A command that fails as it is written, with the wrong number
	// of arguments or as a variable given one, still runs after the
	// command piped into it, whose error comes first.
	empty := []any{doc{}, map[string]any{"Title": ""}}
	for index, text := range []string{
		`{{check .Title | upper "y"}}`,
		`{{check .Title | not "y"}}`,
		`{{check .Title | $}}`,
		`{{check .Title | ne "a" "b"}}`,
	} {
		stage := newCase(
			fmt.Sprintf("stage-before-failure-%d", index+1),
			text,
			empty...)
		stage.errHas = "error calling check"
		cases = append(cases, stage)
	}
	return cases
}
