package corpus

import (
	"fmt"
	"slices"
)

// missingKeyMethod calls a method with arguments on no value, which is
// no value, without evaluating the arguments, unless missingkey=error.
// A {{template}} with no pipeline gives its callee no value as dot.
func missingKeyMethod(option string) Case {
	const text = `{{define "m"}}[{{.Greet "x"}}]{{end}}{{template "m"}}`
	name := "missingkey-method-" + option
	return fromSet(
		name,
		name,
		setup{option: "missingkey=" + option},
		text,
		nil,
		doc{Title: "t"},
	)
}

// lazyArgs: on no value, a method's arguments are not evaluated, so
// one that would fail does not.
func lazyArgs() Case {
	const text = `{{.Greet (index .Bad 1)}}|{{.Missing.Greet (index .Bad 1)}}`
	return fromText(
		"lazy-args",
		"lazy-args",
		text,
		nil,
		doc{},
		map[string]any{},
		map[string]any{"Missing": doc{}},
	)
}

// missingKeyPrint prints a missing key of maps of several element types,
// where the zero value and no value print differently.
func missingKeyPrint(option string) Case {
	const text = `{{.Missing}}|{{printf "%T %v" .Missing .Missing}}|` +
		`{{if .Missing}}y{{else}}n{{end}}|{{with .Missing}}w{{end}}|` +
		`{{range .Missing}}r{{end}}`
	name := "missingkey-print-" + option
	return fromSet(name, name, setup{option: "missingkey=" + option}, text,
		map[string]int{"Present": 1},
		map[string]string{},
		map[string][]int{},
		map[string]*person{},
		map[string]any{},
		map[kind]int{},
		map[string]int{"Missing": 2},
	)
}

// pipelineOrder pipes a failing command into one that would not need
// it: text/template evaluates the pipeline's commands in order, so the
// failure comes first, though or, and, or a method on no value would
// otherwise not evaluate the value piped in.
func pipelineOrder() []Case {
	ten := doc{Title: "ten", Items: make([]entry, 10)}
	ten.Items[9] = entry{Name: "ninth"}
	data := append(
		variants(),
		ten,
		&ten,
		map[string]any{"Items": []entry{}},
		map[string]any{"Items": ten.Items, "Title": "t"},
	)
	var cases []Case
	for index, text := range []string{
		`{{index .Items 9 | or "ok"}}`,
		`{{index .Items 9 | and false}}`,
		`{{index .Items 9 | .Missing.Greet}}`,
		`{{index .Items 9 | printf "%v %v" (len .Title)}}`,
	} {
		name := fmt.Sprintf("pipeline-order-%d", index+1)
		cases = append(cases, fromText(name, name, text, data...))
	}
	return cases
}

// node is a tree for recursive templates.
type node struct {
	Name string
	Kids []node
}

func recursion() Case {
	const text = `{{define "tree"}}<li>{{.Name}}{{if .Kids}}<ul>` +
		`{{range .Kids}}{{template "tree" .}}{{end}}</ul>{{end}}</li>` +
		`{{end}}<ul>{{template "tree" .}}</ul>`
	return fromText(
		"recursion",
		"recursion",
		text,
		node{
			Name: "root",
			Kids: []node{
				{Name: "a", Kids: []node{{Name: "a1"}, {Name: specials}}},
				{Name: "b"},
			},
		},
		node{},
		&node{Name: "pointer"},
		nil,
		map[string]any{
			"Name": "map",
			"Kids": []any{map[string]any{"Name": "kid"}},
		},
	)
}

// depth recurses until html/template's depth limit stops it with an
// error, which compiled code must return too, not overflow its stack.
func depth() Case {
	const text = `{{define "down"}}{{if .}}{{template "down" .}}{{end}}` +
		`{{end}}{{template "down" .}}`
	return fromText("depth", "depth", text, true, false, nil)
}

// channels ranges over channels, which each execution drains, so the
// data is made afresh for each.
func channels() Case {
	filled := func(vals ...string) Fresh {
		return func() any {
			channel := make(chan string, len(vals))
			for _, value := range vals {
				channel <- value
			}
			close(channel)
			return map[string]any{"C": channel}
		}
	}
	const text = `{{range .C}}[{{.}}]{{else}}none{{end}}|` +
		`{{range $i, $v := .C}}{{$i}}{{$v}}{{end}}`
	return fromText("channels", "channels", text,
		filled("a", "<b>", specials),
		filled(),
		map[string]any{"C": (chan string)(nil)},
		map[string]any{"C": make(chan<- string)},
		Fresh(func() any {
			return map[string]any{
				"C": slices.Values([]string{"from", "a", "Seq"}),
			}
		}),
		Fresh(func() any {
			return map[string]any{
				"C": slices.All([]string{"from", "a", "Seq2"}),
			}
		}),
	)
}

// missingKey reads present and missing map keys under a set's
// missingkey option.
func missingKey(option string) Case {
	const text = `{{.Title}}|{{.Owner.Name}}|{{index .Tags "a"}}|` +
		`{{range .Items}}{{.Name}}{{end}}|{{with .Any}}{{.}}{{end}}|` +
		`{{if .Count}}c{{end}}`
	name := "missingkey-" + option
	return fromSet(name, name, setup{option: "missingkey=" + option}, text,
		append(
			variants(),
			map[string]any{"Title": "t", "Owner": map[string]any{}},
			map[string]any{
				"Title": "t",
				"Owner": map[string]any{"Name": "n"},
				"Tags":  map[string]string{},
				"Items": nil,
				"Any":   1,
				"Count": 0,
			},
			map[string]int{"Title": 1},
		)...)
}
