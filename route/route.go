// Package route registers an app's page routes on chi or a ServeMux and
// records each one with the operation it serves, or why an agent has no
// use for it, so paritytest can check every route has an agent twin.
package route

import (
	"net/http"
	"slices"
	"strings"

	"amadan.net/rastrillo/rastrillo/op"
	"github.com/go-chi/chi/v5"
)

// Register gives one route to the router underneath a Table.
type Register func(method, path string, handler http.Handler)

// Chi registers on a chi router or sub-router.
func Chi(router chi.Router) Register {
	return func(method, path string, handler http.Handler) {
		router.Method(method, path, handler)
	}
}

// Mux registers on a ServeMux with a method pattern, so the mux, like
// chi, answers 405 to another method on the same path.
func Mux(mux *http.ServeMux) Register {
	return func(method, path string, handler http.Handler) {
		mux.Handle(method+" "+path, handler)
	}
}

// Mark says what a route is for: the operations it serves, or why it
// serves none. The zero Mark says neither, and a Table records it as it
// is, because refusing it is paritytest's job and the table only lists.
type Mark struct {
	operations []string
	pageOnly   string
}

// Operation marks a route as the page door of these operations.
func Operation(descriptor op.Descriptor, more ...op.Descriptor) Mark {
	var operations []string
	for _, each := range append([]op.Descriptor{descriptor}, more...) {
		if each.ID == "" {
			panic(msgNoOperation)
		}
		operations = append(operations, each.ID)
	}
	return Mark{operations: operations}
}

// PageOnly marks a route an agent has no use for, such as a stylesheet
// or a CSRF-only redirect. The reason is for the person reviewing why
// the route has no operation.
func PageOnly(reason string) Mark {
	if strings.TrimSpace(reason) == "" {
		panic(msgNoReason)
	}
	return Mark{pageOnly: reason}
}

// Operations returns the IDs the mark names, in order, as a copy.
func (mark Mark) Operations() []string {
	return slices.Clone(mark.operations)
}

// Reason returns the page-only reason, or "" for an operation's mark.
func (mark Mark) Reason() string {
	return mark.pageOnly
}

// Route is one recorded route: its method, its full path as the app
// serves it, and its mark.
type Route struct {
	Method, Path string
	Operations   []string
	PageOnly     string
}
