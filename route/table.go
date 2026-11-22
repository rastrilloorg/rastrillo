package route

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
)

// Table registers routes through a Register and records each route with
// a mark. A table returned by On shares its record with the table it
// came from, so List includes routes from the whole app.
//
// Register every page route through a Table. A route registered
// directly on the router has no mark. paritytest finds it on a chi
// router, but cannot find it on a ServeMux.
type Table struct {
	register Register
	// prefix is where register's router is mounted, without a trailing
	// slash, so the recorded path is the one a client requests.
	prefix string
	shared *record
}

type record struct {
	routes []Route
	// registered holds each method and full path as twiceKey shapes it.
	registered map[string]bool
}

// New starts a table at the root of register's router.
func New(register Register) *Table {
	return &Table{
		register: register,
		shared:   &record{registered: map[string]bool{}},
	}
}

// On returns a table for a sub-router mounted at prefix, such as a chi
// sub-router created with Route("/admin", ...). Use it where you would
// use Mount, then register the sub-router's routes on the table On
// returns. For nested sub-routers, call On on the parent table.
func (table *Table) On(register Register, prefix string) *Table {
	return &Table{
		register: register,
		prefix:   table.prefix + strings.TrimSuffix(prefix, "/"),
		shared:   table.shared,
	}
}

// Handle registers handler at pattern, "<METHOD> <path>", and records
// it with mark under the full path.
func (table *Table) Handle(pattern string, handler http.Handler, mark Mark) {
	method, path := parse(pattern)
	full := table.prefix + path
	table.refuseTwice(method, full)
	table.register(method, path, handler)
	table.shared.routes = append(table.shared.routes, Route{
		Method:     method,
		Path:       full,
		Operations: mark.Operations(),
		PageOnly:   mark.Reason(),
	})
}

// HandleFunc is Handle for a handler function.
func (table *Table) HandleFunc(pattern string,
	handler func(http.ResponseWriter, *http.Request), mark Mark) {
	table.Handle(pattern, http.HandlerFunc(handler), mark)
}

// List returns every route recorded so far, in the order registered.
// Each call returns its own copy, so a caller that edits it cannot
// change what the next call reports.
func (table *Table) List() []Route {
	listing := slices.Clone(table.shared.routes)
	for position := range listing {
		listing[position].Operations = slices.Clone(
			listing[position].Operations)
	}
	return listing
}

func parse(pattern string) (method, path string) {
	method, path, found := strings.Cut(pattern, " ")
	if !found {
		panic(fmt.Sprintf(msgNoMethod, pattern))
	}
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete:
		return method, path
	}
	panic(fmt.Sprintf(msgUnknownMethod, pattern))
}

// refuseTwice panics before the router sees a route it already has.
// chi keeps the later handler without a word, so the earlier route
// would be listed and never run, and a ServeMux's own panic would not
// say which table line to delete.
func (table *Table) refuseTwice(method, path string) {
	key := method + " " + twiceKey(path)
	if table.shared.registered[key] {
		panic(fmt.Sprintf(msgTwice, method, path))
	}
	table.shared.registered[key] = true
}

// twiceKey writes each {name} in a path as {}, because a router matches
// /notes/{id} and /notes/{key} alike. What does change the match stays:
// chi's regexp in {id:[0-9]+}, and ServeMux's {$} and {name...}. Braces
// nest, so {code:[0-9]{3}} is one parameter.
func twiceKey(path string) string {
	var key, inside strings.Builder
	depth := 0
	for _, character := range path {
		switch {
		case character == '{':
			if depth > 0 {
				inside.WriteRune(character)
			}
			depth++
		case character == '}' && depth == 1:
			depth--
			key.WriteString(parameterShape(inside.String()))
			inside.Reset()
		case depth > 0:
			if character == '}' {
				depth--
			}
			inside.WriteRune(character)
		default:
			key.WriteRune(character)
		}
	}
	return key.String()
}

func parameterShape(inside string) string {
	_, pattern, found := strings.Cut(inside, ":")
	switch {
	case found:
		return "{:" + pattern + "}"
	case inside == "$":
		return "{$}"
	case strings.HasSuffix(inside, "..."):
		return "{...}"
	}
	return "{}"
}
