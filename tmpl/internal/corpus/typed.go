package corpus

import (
	"fmt"
	"html/template"
	"reflect"
	"strconv"
)

// The typed cases are for the code generate writes for the package's
// own types, where Go's rules and text/template's part: a pointer method
// on a value that is not addressable, a named string type whose String
// method says something else, an embedded pointer, an interface field.
// Each runs over its types as values, as pointers, as nil pointers and
// as a struct of another type with the same fields, which only the
// dynamic code may render.

// box has Ptr only as a pointer method. text/template finds it on an
// addressable box: through a pointer, or as an element of a slice, but
// not as a map's value, a field of a value, or the data itself.
type box struct{ Name string }

func (box *box) Ptr() string {
	if box == nil {
		return "nil box"
	}
	return "ptr " + box.Name
}

type shelf struct {
	Title  string
	List   []box
	ByName map[string]box
	One    box
}

// lookalike has the field names of box and shelf both, and is neither.
type lookalike struct {
	Name   string
	Title  string
	List   []box
	ByName map[string]box
	One    box
}

// shown is a named string whose String method says something other
// than its value, as a filter type's might. An escaper prints it by the
// method; eq compares the string itself.
type shown string

func (shown shown) String() string { return "shown:" + string(shown) }

type tabs struct {
	Filter shown
	Count  uint
	Items  []shown
}

// base is embedded in withBase as a value and in withPtr as a pointer,
// which may be nil.
type base struct {
	ID   int
	Name string
}

func (base base) Label() string { return "base " + base.Name }

type withBase struct {
	base
	Title string
}

type withPtr struct {
	*base
	Title string
}

// described holds interfaces: only the data says what is in them.
type described struct {
	Name string
	Err  error
	Str  fmt.Stringer
	Any  any
}

// grid has arrays, a map with integer keys, and integers of several
// sizes to range over and print. An array's elements are addressable
// when the array is, so Boxes' have Ptr only through a pointer. count
// prints by its String method.
type grid struct {
	Cells [3]string
	Boxes [2]box
	Sizes map[int]uint
	N     int8
	U     uint16
	Count count
}

type count int

func (count count) String() string {
	return fmt.Sprintf("%d items", int(count))
}

// parent's methods change it while a template runs. text/template
// reads a ranged slice's length through the slice's own location on
// every iteration, so Trim ends the range early; it binds a method's
// receiver, and goes through a pointer, before it evaluates what comes
// after, so Swap does not change the child already chosen.
type parent struct {
	Child *kid
	Items []string
}

type kid struct{ Name string }

func (kid *kid) Say(text string) string { return kid.Name + " says " + text }

func (parent *parent) Swap() string {
	parent.Child = &kid{Name: "new"}
	return "swapped"
}

func (parent *parent) Trim() string {
	parent.Items = parent.Items[:1]
	return ""
}

// parents is a parent for each execution, which changes it, as a
// pointer and as the value under it, with a child and without one. A
// nil child is not bound: text/template keeps the field's location,
// and calls Say on the child Swap puts there.
func parents() []any {
	make := func() *parent {
		return &parent{Child: &kid{Name: "old"}, Items: []string{"a", "b", "c"}}
	}
	orphan := func() *parent { return &parent{Items: []string{"a", "b"}} }
	return []any{
		Fresh(func() any { return make() }),
		Fresh(func() any { return *make() }),
		Fresh(func() any { return reflect.ValueOf(make()).Elem() }),
		Fresh(func() any { return orphan() }),
		Fresh(func() any { return reflect.ValueOf(orphan()).Elem() }),
	}
}

// grower's Grow adds an item. text/template evaluates a call's
// arguments in order, each to a value before the next starts, so in
// {{printf "%d%s" (len .Items) (.Grow)}} len counts the items Grow has
// not yet added to.
type grower struct{ Items []string }

func (grower *grower) Grow() string {
	grower.Items = append(grower.Items, "grown")
	return ""
}

// ticker prints how often Tick has run, so a print of it that runs
// after a later Tick prints a different number.
type ticker struct{ n int }

func (ticker *ticker) String() string { return strconv.Itoa(ticker.n) }

func (ticker *ticker) Tick() string {
	ticker.n++
	return ""
}

type ticking struct{ T *ticker }

// tally is for a function's arguments, which text/template converts one
// at a time as it evaluates them: an addressable N is still read at the
// call, after Bump has changed it, and S fails to convert to an int
// before Boom can run.
type tally struct {
	N int
	S string
}

func (tally *tally) Bump() string {
	tally.N++
	return "bumped"
}

func (tally *tally) Boom() string {
	panic("Boom ran before the argument before it converted")
}

// pairFuncs has pair called directly and, under a result type declared
// here, by reflection.
func pairFuncs() []template.FuncMap {
	type local string
	return []template.FuncMap{
		{
			"pair": func(count int, text string) string {
				return fmt.Sprint(count, " ", text)
			},
		},
		{
			"pair": func(count int, text string) local {
				return local(fmt.Sprint(count, " ", text))
			},
		},
	}
}

// ordered are the cases that hold a call's arguments to the order
// text/template evaluates them in, on typed code and on the code that
// reads by reflection, which the reflect.Values and the map run.
func ordered() []Case {
	cases := []Case{
		fromText(
			"typed-nested-order",
			"typed-nested-order",
			`{{printf "%d%s" (len .Items) (.Grow)}}`,
			Fresh(func() any { return &grower{Items: []string{"a"}} }),
			Fresh(func() any {
				return reflect.ValueOf(&grower{Items: []string{"a"}})
			}),
			Fresh(func() any {
				return reflect.ValueOf(&grower{Items: []string{"a"}}).Elem()
			}),
			// Grow is a pointer method, which a value cannot call.
			grower{Items: []string{"a"}},
		),
		fromText(
			"nested-print-order",
			"nested-print-order",
			`{{print (print .T) (.T.Tick) (print .T)}}`,
			Fresh(func() any { return ticking{T: &ticker{}} }),
			Fresh(func() any { return &ticking{T: &ticker{}} }),
			Fresh(func() any { return map[string]any{"T": &ticker{}} }),
			Fresh(func() any { return reflect.ValueOf(ticking{T: &ticker{}}) }),
		),
	}
	for index, funcs := range pairFuncs() {
		path := []string{"direct", "reflected"}[index]
		cases = append(cases, callOrder(path, funcs)...)
	}
	return cases
}

// callOrder is the call-order cases with funcs, the path called path:
// an argument read after the one before it changed it, and one that
// fails to convert before the next runs.
func callOrder(path string, funcs template.FuncMap) []Case {
	name := "call-order-read-" + path
	read := fromSet(
		name,
		name,
		setup{funcs: funcs},
		`{{pair .N .Bump}}`,
		Fresh(func() any { return &tally{N: 1} }),
		Fresh(
			func() any { return reflect.ValueOf(&tally{N: 1}).Elem() },
		),
		tally{N: 1},
	)
	name = "call-order-convert-" + path
	convert := fromSet(name, name, setup{funcs: funcs}, `{{pair .S .Boom}}`,
		Fresh(func() any { return &tally{S: "s"} }),
		Fresh(func() any { return reflect.ValueOf(&tally{S: "s"}).Elem() }),
	)
	convert.errHas = "expected int"
	return []Case{read, convert}
}
