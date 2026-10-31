//rastrillo:data template texts of the equivalence corpus, kept as written

package corpus

import (
	"errors"
	"reflect"
	"slices"
)

// typedCases are the cases for typed code: a template whose dot has one
// type is compiled for it, and must still render each other value as
// html/template does.
func typedCases() []Case {
	shelves := shelfVariants()
	gridFull := fullGrid()
	return slices.Concat(
		pointerMethodCases(shelves),
		stringerCases(),
		embeddedCases(),
		gridCases(&gridFull),
	)
}

// shelfVariants are a shelf, as a value, a pointer, empty and nil, and
// values of other types with the same fields.
func shelfVariants() []any {
	full := shelf{
		Title:  "shelf",
		List:   []box{{"a"}, {specials}},
		ByName: map[string]box{"k": {"in a map"}},
		One:    box{"one"},
	}
	return []any{
		full,
		&full,
		shelf{},
		(*shelf)(nil),
		lookalike{
			Title:  "lookalike",
			List:   full.List,
			ByName: full.ByName,
			One:    full.One,
		},
		struct {
			Title  string
			List   []box
			ByName map[string]box
			One    box
		}{Title: "unnamed", List: full.List, ByName: full.ByName, One: full.One},
		reflect.ValueOf(&full).Elem(),
	}
}

// fullGrid is a grid with every field set.
func fullGrid() grid {
	return grid{
		Cells: [3]string{"x", specials, ""},
		Boxes: [2]box{{"b0"}, {"b1"}},
		Sizes: map[int]uint{3: 30, -1: 1, 0: 0},
		N:     3,
		U:     2,
		Count: 12,
	}
}

// pointerMethodCases call a pointer method on what shelves holds.
func pointerMethodCases(shelves []any) []Case {
	return []Case{
		// Review focus: a pointer method on the data itself, a map's
		// value, a field of a value, and a slice's element.
		fromText(
			"typed-pointer-method-data",
			"typed-pointer-method-data",
			`{{.Ptr}}`,
			box{
				"value",
			},
			&box{"pointer"},
			(*box)(nil),
			lookalike{Name: "lookalike"},
			reflect.ValueOf(&box{"addressable"}).
				Elem(),
			reflect.ValueOf(box{"not addressable"}),
		),
		fromText(
			"typed-pointer-method-map",
			"typed-pointer-method-map",
			`{{range .ByName}}{{.Ptr}}{{end}}`,
			shelves...),
		fromText(
			"typed-pointer-method-field",
			"typed-pointer-method-field",
			`{{.One.Ptr}}`,
			shelves...),
		fromText(
			"typed-pointer-method-slice",
			"typed-pointer-method-slice",
			`<h1>{{.Title}}</h1>{{range .List}}<i title="{{.Name}}">{{.Ptr}}</i>{{end}}`,
			shelves...),
	}
}

// stringerCases print a named string with a String method.
func stringerCases() []Case {
	return []Case{
		// Review focus: a named string with a String method, printed in
		// text, in attributes and in script, compared with eq, and given
		// to a template, which is specialised for it.
		fromText(
			"typed-stringer",
			"typed-stringer",
			`{{define "label"}}<b title="{{.}}">{{.}}</b>{{end}}`+
				`<p>{{.Filter}}</p><a title="{{.Filter}}" href="/{{.Filter}}?c={{.Count}}">{{.Count}}</a>
{{if eq .Filter "all"}}all{{else}}other{{end}}{{$f := .Filter}}{{if eq $f "shown:all"}}by its method{{end}}
<script>var f = {{.Filter}}; var s = "{{.Filter}}";</script>
{{template "label" .Filter}}{{range .Items}}[{{.}}]{{end}}`,
			tabs{Filter: "all", Count: 7, Items: []shown{"a", specials}},
			&tabs{Filter: specials, Count: 18446744073709551615},
			tabs{},
			(*tabs)(nil),
			struct {
				Filter string
				Count  uint
				Items  []shown
			}{Filter: "all"},
		),
	}
}

// embeddedCases read through embedded structs and interfaces.
func embeddedCases() []Case {
	return []Case{
		fromText(
			"typed-embedded",
			"typed-embedded",
			`{{.Title}} {{.ID}} {{.Name}} {{.Label}}`,
			withBase{base: base{ID: 1, Name: "embedded"}, Title: "t"},
			&withBase{base: base{ID: -2, Name: specials}},
			(*withBase)(nil),
			withPtr{base: &base{ID: 3, Name: "through a pointer"}, Title: "p"},
			withPtr{Title: "nil embedded pointer"},
			&withPtr{base: &base{}},
		),
		fromText(
			"typed-interfaces",
			"typed-interfaces",
			`{{.Name}}|{{.Err}}|{{.Str}}|{{if .Err}}err{{end}}|{{with .Str}}{{.}}{{end}}|<a title="{{.Err}}" href="/{{.Str}}">{{.Any}}</a>|{{len .Name}}|{{range .Any}}{{.}}{{end}}`,
			described{},
			described{
				Name: "full",
				Err:  errors.New("<an error>"),
				Str:  stringer{},
				Any:  []int{1, 2},
			},
			&described{Name: "pointer", Any: 3},
			described{Any: "a string cannot be ranged over"},
			(*described)(nil),
		),
	}
}

// gridCases range over arrays and maps, and over data a method
// changes while the template runs. full is the grid both of its
// grid cases run over.
func gridCases(full *grid) []Case {
	return []Case{
		fromText(
			"typed-grid",
			"typed-grid",
			`{{range $i, $c := .Cells}}{{$i}}={{$c}};{{end}}|{{range $k, $v := .Sizes}}{{$k}}:{{$v}},{{else}}no sizes{{end}}|{{range .N}}{{.}}{{end}}|{{range .U}}{{.}}{{else}}none{{end}}|<a href="/{{.U}}?n={{.N}}&c={{.Count}}" title="{{.N}} {{.Count}}">{{len .Cells}} {{.Count}}</a>`,
			*full,
			full,
			grid{N: -2},
			(*grid)(nil),
		),
		fromText(
			"typed-range-length",
			"typed-range-length",
			`{{range .Items}}{{.}}{{$.Trim}}{{end}}|{{range $i, $e := .Items}}{{$i}}{{end}}`,
			parents()...),
		fromText(
			"typed-receiver-binding",
			"typed-receiver-binding",
			`{{.Child.Say (.Swap)}}`,
			parents()...),
		fromText(
			"typed-pointer-binding",
			"typed-pointer-binding",
			`{{print .Child.Name (.Swap) .Child.Name}}`,
			parents()...),
		fromText(
			"typed-array-pointer-method",
			"typed-array-pointer-method",
			`{{range .Boxes}}{{.Ptr}}{{end}}`,
			*full,
			full,
			(*grid)(nil),
		),
	}
}
