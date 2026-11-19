package corpus

import (
	"errors"
	"html/template"
	"slices"
	"strings"
)

// doc is the data the construct cases read. Each case runs over every
// value variants returns, so that each construct meets a zero value, a
// nil pointer, empty and missing values, a map in place of the struct,
// HTML specials, and every kind of typed content.
type doc struct {
	Title string
	Count int
	Ratio float64
	Done  bool
	Kind  kind  // a named string type without String
	Label label // a named string type with String
	Owner *person
	Items []entry
	Tags  map[string]string
	Any   any
	Fn    func(string) string
}

type entry struct {
	Name string
	Size int
}

// kind has no String method; label has one.
type (
	kind  string
	label string
)

func (label label) String() string { return "label:" + string(label) }

// stringer prints HTML specials through its String method.
type stringer struct{}

func (stringer) String() string { return `<stringer & "co">` }

func (doc doc) Greet(
	who string,
) string {
	return doc.Title + " greets " + who
}
func (doc doc) Scale(number float64) float64 { return number * doc.Ratio }

func (doc doc) Join(
	sep string,
	parts ...string,
) string {
	return strings.Join(parts, sep)
}
func (doc doc) Is(want kind) bool { return doc.Kind == want }

func (doc *doc) Shout(
	text string,
	times int,
) string {
	return strings.Repeat(strings.ToUpper(text), times)
}

// Lookup fails for a name the doc has no tag for, which a chain on its
// result must not hide.
func (doc doc) Lookup(name string) (*person, error) {
	value, ok := doc.Tags[name]
	if !ok {
		return nil, errors.New("no tag " + name)
	}
	return &person{Name: value}, nil
}

func (person *person) Initial() string {
	if person == nil || person.Name == "" {
		return "?"
	}
	return person.Name[:1]
}

// typedValues is a value of every kind an escaper treats specially.
func typedValues() []any {
	return []any{
		template.HTML(`<b>bold</b> & "q"`),
		template.HTMLAttr(`title="attr"`),
		template.URL("javascript:alert(1)"),
		template.URL("/a?b=c&d=<e>"),
		template.JS(`alert("js")`),
		template.JSStr(`a"b<c`),
		template.CSS("color: red"),
		template.Srcset("a.png 1x, b.png 2x"),
		stringer{},
		kind("named<kind>"),
		label("named<label>"),
		errors.New("<an error>"),
		(*person)(nil),
		&person{Name: "pointer <person>"},
		1.5,
		uint8(3),
		-4,
		true,
		[]string{"a", "<b>"},
		map[string]int{"b": 2, "a": 1},
	}
}

// variants is the data every construct case runs over.
func variants() []any {
	return slices.Concat(docVariants(), otherTypeVariants(), typedVariants())
}

// groceries is a doc with every field set.
func groceries() doc {
	return doc{
		Title: "Groceries", Count: 3, Ratio: 0.5, Done: true,
		Kind: "a", Label: "b",
		Owner: &person{Name: "Ana"},
		Items: []entry{
			{"milk", 1},
			{"eggs", 12},
			{"", 0},
			{"bread", 2},
			{"jam", 5},
			{"tea", 7},
		},
		Tags: map[string]string{"b": "two", "a": "one"},
		Any:  7,
		Fn:   strings.ToUpper,
	}
}

// specialDoc is a doc whose every string is specials.
func specialDoc() doc {
	return doc{
		Title: specials, Count: 1, Ratio: 2,
		Kind: kind(specials), Label: label(specials),
		Owner: &person{Name: specials},
		Items: []entry{{specials, 1}},
		Tags:  map[string]string{"a": specials, specials: specials},
		Any:   specials,
		Fn:    func(text string) string { return text + specials },
	}
}

// docVariants are docs, full, special, empty and missing, and maps and
// values in a doc's place.
func docVariants() []any {
	full, special := groceries(), specialDoc()
	return []any{
		doc{},
		(*doc)(nil),
		nil,
		full,
		&full,
		special,
		&special,
		doc{
			Title: "empty",
			Items: []entry{},
			Tags:  map[string]string{},
			Owner: &person{},
		},
		map[string]any{
			"Title": "from a map", "Count": 2, "Ratio": 1.0, "Done": false,
			"Kind":  "a",
			"Owner": map[string]string{"Name": "Bo"},
			"Items": []any{entry{"m", 1}, map[string]any{"Name": "in a map"}},
			"Tags":  map[string]string{"a": "map tag"},
			"Any":   nil,
		},
		map[string]any{},
		map[string]any{"Owner": nil, "Items": nil},
		"a string",
		42,
	}
}

// otherTypeVariants are values of the package's other types: a
// construct that reads only names they have too gets typed code for
// them.
func otherTypeVariants() []any {
	return []any{
		item{
			ID:        7,
			Title:     specials,
			EditHref:  "javascript:alert(1)",
			Completed: true,
		},
		&item{Title: "pointer item"},
		failing{},
		&failing{},
		lookalike{
			Name:  "lookalike",
			Title: specials,
			One:   box{"one"},
			List:  []box{{"b"}},
		},
		&lookalike{Title: "pointer lookalike"},
		page{
			Title: "page",
			Owner: &person{Name: "Pa"},
			Items: []string{"x", specials},
		},
		&page{Title: "pointer page"},
		methods{title: "methods"},
		&methods{title: "pointer methods"},
		described{Name: "described", Err: errors.New("<an error>"), Any: 3},
		&base{ID: 4, Name: "base"},
		box{"box"},
		&box{specials},
		ownedBy{Title: "owned"},
		&ownedBy{Title: "pointer owned"},
	}
}

// typedVariants put every typed value in a doc's Any and in every key
// of a map.
func typedVariants() []any {
	var out []any
	for _, value := range typedValues() {
		out = append(
			out,
			doc{
				Title: "typed",
				Count: 1,
				Any:   value,
				Owner: &person{Name: "Ana"},
				Items: []entry{{"x", 1}},
			},
			map[string]any{
				"Title": value,
				"Any":   value,
				"Kind":  value,
				"Count": value,
				"Owner": value,
				"Items": value,
				"Tags":  value,
			},
		)
	}
	return out
}

// construct is one construct case: a template and the data variants.
func construct(name, text string) Case {
	return fromText(name, name, text, variants()...)
}
