// Package corpus is the equivalence corpus: template sets, each with the
// data to render it with, that the compiled path must render exactly as
// html/template does. rastrillo_templates_gen.go is generated from these
// sets by go run ./internal/tmplgen/cmd/tmplgen-corpus, and the tests
// check both that it is fresh and that every entry it compiles writes
// html/template's bytes.
package corpus

import (
	"errors"
	"html/template"
	"reflect"
	"slices"

	"amadan.net/rastrillo/rastrillo/tmpl"
)

// Case is one set and the data to execute its entry with.
type Case struct {
	Name string
	// Build makes the set. It runs in package corpus, so the set belongs
	// here and its compiled code is in this package's generated file.
	Build func() (*tmpl.Template, error)
	// Entry is the template executed.
	Entry string
	Data  []any

	// std builds the same set on html/template, which the compiled code
	// is compared with.
	std func() (*template.Template, error)
	// errHas, when set, is in both errors for every value of Data, all of
	// which fail: for a case where which error comes first is the point.
	errHas string
}

// Fresh is data made anew for each execution, for a value that one
// execution uses up, such as a channel.
type Fresh func() any

// Cases returns every case. Build each set afresh: executing one fixes
// it, and a test may execute it differently.
func Cases() []Case {
	cases := []Case{basics(), rangeFields(), pipelineEnds()}
	cases = append(cases, constructs()...)
	cases = append(cases, funcCases()...)
	cases = append(cases, todoMVC()...)
	cases = append(cases, typedCases()...)
	cases = append(cases, ordered()...)
	cases = append(cases, sharedPartial())
	cases = append(cases, uiLegacy())
	return cases
}

// fromText is a case whose set is one template, entry, parsed from text.
func fromText(name, entry, text string, data ...any) Case {
	return fromSet(name, entry, setup{}, text, data...)
}

// setup is what a set gets before it parses: functions and options.
type setup struct {
	funcs  template.FuncMap
	option string // such as "missingkey=error"; "" for none
}

// fromSet is a case whose set is made with config and parsed from text.
func fromSet(name, entry string, config setup, text string, data ...any) Case {
	return Case{
		Name:  name,
		Entry: entry,
		Data:  data,
		Build: func() (*tmpl.Template, error) {
			template := tmpl.New(entry).Funcs(config.funcs)
			if config.option != "" {
				template.Option(config.option)
			}
			return template.Parse(text)
		},
		std: func() (*template.Template, error) {
			standard := template.New(entry).Funcs(config.funcs)
			if config.option != "" {
				standard.Option(config.option)
			}
			return standard.Parse(text)
		},
	}
}

type person struct{ Name string }

type page struct {
	Title string
	Owner *person
	Items []string
}

// methods has Title, Owner and Items as methods, which a field lookup
// calls. Owner has a pointer receiver, so it is found only when the data
// is a pointer.
type methods struct{ title string }

func (methods methods) Title() string { return methods.title }

func (methods *methods) Owner() *person {
	return &person{Name: "pointer receiver"}
}

func (methods methods) Items() []string {
	return []string{"from", "a", "method"}
}

// ownedBy's Owner has Name only as a pointer method. It is found when
// the data is a pointer, which makes the field addressable, and not
// when the data is a value.
type ownedBy struct {
	Title string
	Owner namer
	Items []string
}

type namer struct{}

func (*namer) Name() string { return "pointer method on a field" }

// failing's Title returns an error, which execution must return.
type failing struct{}

func (failing) Title() (string, error) {
	return "", errors.New("no title today")
}

// panicking's Title panics, which execution turns into an error.
type panicking struct{}

func (panicking) Title() string { panic("title panicked") }

// specials is in every string a context must escape.
const specials = `<script>alert("x&y")</script> 'quoted' a=b`

// basics is text, {{.}} in text and in a quoted attribute, a field
// chain, {{if}} with {{else}}, and {{range}}, with data of every shape
// a field lookup and a range can meet.
func basics() Case {
	const text = `<h1 title="{{.Title}}">{{.Title}}</h1>
<p>by {{.Owner.Name}}</p>
{{if .Items}}<ul>
{{range .Items}}	<li class="{{.}}">{{.}}</li>
{{end}}</ul>{{else}}<p>Nothing yet.</p>{{end}}
`
	owner := &person{Name: "Ana"}
	data := slices.Concat(
		basicPages(owner), basicMaps(), basicOthers(owner),
	)
	return fromText("basics", "page", text, data...)
}

// basicPages is the basics data that is a page, or no page at all.
func basicPages(owner *person) []any {
	return []any{
		page{Title: "Groceries", Owner: owner, Items: []string{"milk", "eggs"}},
		&page{Title: "Pointer", Owner: owner, Items: []string{"one"}},
		page{Owner: owner},
		page{
			Title: specials,
			Owner: &person{Name: specials},
			Items: []string{specials, "", "&amp;", `a"b`},
		},
		page{Title: "No owner", Items: []string{"x"}},
		(*page)(nil),
		nil,
	}
}

// basicMaps is the basics data that is a map, or a struct shaped as
// one, with every kind of value a range can meet under Items.
func basicMaps() []any {
	return []any{
		map[string]any{
			"Title": "Map",
			"Owner": map[string]string{"Name": "Bo"},
			"Items": []string{"a", "b"},
		},
		map[string]any{},
		// A nil interface value fails .Owner.Name; a missing key does not.
		map[string]any{"Owner": nil},
		struct {
			Title string
			Owner any
			Items []string
		}{Title: "nil interface field"},
		map[string]string{"Title": "strings only"},
		map[string]any{
			"Title": template.HTML("<b>trusted</b>"),
			"Items": []any{
				nil,
				1,
				2.5,
				true,
				template.HTML("<i>x</i>"),
				template.URL("javascript:x"),
				specials,
			},
		},
		map[string]any{"Items": map[string]int{"b": 2, "a": 1, "c": 3}},
		map[string]any{"Items": map[int]string{3: "three", 1: "one", 2: "two"}},
		map[string]any{"Items": 3},
		map[string]any{"Items": 0},
		map[string]any{"Items": &[]string{"through", "a pointer"}},
		map[string]any{"Items": (*[]string)(nil)},
		map[string]any{
			"Items": slices.Values([]string{"from", "an", "iterator"}),
		},
		map[string]any{"Items": "a string cannot be ranged over"},
		map[string]any{"Items": [2]string{"an", "array"}},
		map[string]any{"Items": []string{}},
	}
}

// basicOthers is the basics data whose fields are methods, reached
// through reflect, or missing.
func basicOthers(owner *person) []any {
	return []any{
		methods{title: "methods"},
		&methods{title: "methods through a pointer"},
		&ownedBy{Title: "addressable"},
		ownedBy{Title: "not addressable"},
		// A reflect.Value is used as the value it holds, addressable
		// or not.
		reflect.ValueOf(
			page{Title: "reflect.Value", Owner: owner, Items: []string{"rv"}},
		),
		reflect.ValueOf(&ownedBy{Title: "addressable through reflect"}).Elem(),
		reflect.Value{},
		failing{},
		panicking{},
		"a string has no fields",
		struct{ Name string }{"no Title field"},
		struct{ title string }{"unexported"},
	}
}

// named has Name only as a pointer method, which handles a nil
// receiver.
type named struct{ n string }

func (named *named) Name() string {
	if named == nil {
		return "nil receiver"
	}
	return named.n
}

// rangeFields reads a field of each element: what {{range}} hands its
// body keeps the element's addressability and its interface type.
func rangeFields() Case {
	const text = `<ul>{{range .}}<li>{{.Name}}</li>{{end}}</ul>`
	return fromText("range-fields", "list", text,
		// Slice elements are addressable, so Name is found.
		[]named{{"a"}, {"b"}},
		[]*named{{"pointer"}, nil},
		reflect.ValueOf([]named{{"through reflect"}}),
		reflect.ValueOf(&[]named{{"addressable slice"}}).Elem(),
		// Map values and interface contents are not.
		map[string]named{"k": {"map value"}},
		[]any{named{"in an interface"}},
		[]any{&named{"pointer in an interface"}},
		// A nil interface element has no fields.
		[]any{nil},
		[]person{{Name: "a field"}},
		[]map[string]string{{"Name": "a key"}, {}},
		reflect.Value{},
		nil,
	)
}

// pipelineEnds sends values held in interfaces to each construct. At the
// end of every command text/template takes the value out of an empty
// interface, so a nil one is no value: {{range}} over it runs {{else}},
// where ranging over the interface itself would fail. An interface with
// methods, such as error, is left as it is.
func pipelineEnds() Case {
	const text = `{{range .Items}}<i>{{.}}</i>{{else}}empty{{end}}
{{if .Items}}yes{{else}}no{{end}}
<b title="{{.Items}}">{{.Items}}</b>
{{range .Each}}[{{range .}}{{.}}{{else}}none{{end}}]{{end}}
`
	type anyItems struct {
		Items any
		Each  []any
	}
	type errorItems struct {
		Items error
		Each  []any
	}
	return fromText("pipeline-ends", "ends", text,
		map[string]any{"Items": nil, "Each": []any{nil, []string{"x"}, 2}},
		map[string]any{"Items": []string{"a", "<b>"}},
		map[string]any{"Items": []any{nil}},
		map[string]any{"Items": map[string]any{"k": nil}},
		anyItems{},
		anyItems{Items: []int{1}, Each: []any{map[string]int{"k": 1}, nil}},
		errorItems{},
		errorItems{Items: errors.New("<an error>")},
	)
}
