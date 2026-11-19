package action

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/flash"
	"amadan.net/rastrillo/rastrillo/op"
	"amadan.net/rastrillo/rastrillo/view"
	"github.com/go-chi/chi/v5"
)

// Visit is one request as an action sees it. Its fields are private so
// that an action reaches the response and the request through the
// methods below, which keep the common steps (render, flash, redirect)
// the same on every page.
type Visit struct {
	response http.ResponseWriter
	request  *http.Request
	ctx      *rastrillo.Ctx

	// bodyRead and bodyErr remember how reading the body went. The
	// standard library parses a body once and reports success on every
	// later call, so without them a second ReadBody would pass a body
	// the first call refused.
	bodyRead bool
	bodyErr  error
}

// NewVisit wraps the three values a handler is given. The route table
// builds the Ctx before the handler runs, so an action never builds one.
func NewVisit(
	response http.ResponseWriter,
	request *http.Request,
	ctx *rastrillo.Ctx,
) *Visit {
	return &Visit{response: response, request: request, ctx: ctx}
}

// Response is the escape hatch for what Visit has no word for, such as
// writing JSON or setting a header.
func (visit *Visit) Response() http.ResponseWriter { return visit.response }

// Request is the escape hatch for what Visit has no word for, such as
// reading a header or a query string.
func (visit *Visit) Request() *http.Request { return visit.request }

// Ctx is the app's per-request context: stores, session and actor.
func (visit *Visit) Ctx() *rastrillo.Ctx { return visit.ctx }

// Context is the request's context, so a store call is cancelled when
// the client goes away.
func (visit *Visit) Context() context.Context {
	return visit.request.Context()
}

// PathValue reads chi's URL parameter first, because the generated
// router is chi. It falls back to the request's own path value so that
// a handler mounted on a plain ServeMux pattern still finds its id.
//
// chi matches the escaped path when the request has one (RawPath: the
// URL came escaped, or basepath set it) and the decoded path when it
// does not. So the value is unescaped then and only then: unescaping a
// value chi read decoded would turn a literal "%2F" into "/". A value
// that does not unescape is returned as it came, and PathID finds no
// id in it.
func (visit *Visit) PathValue(name string) string {
	value := chi.URLParam(visit.request, name)
	if value == "" {
		return visit.request.PathValue(name)
	}
	if visit.request.URL.RawPath == "" {
		return value
	}
	unescaped, err := url.PathUnescape(value)
	if err != nil {
		return value
	}
	return unescaped
}

// PathID reads a row id from the path. A malformed or non-positive id is
// a URL that was never ours, so it answers op.NotFound (a 404) rather
// than a 400, as view.ParseID does.
func (visit *Visit) PathID(name string) (int64, error) {
	id, err := strconv.ParseInt(visit.PathValue(name), 10, 64)
	if err != nil || id < 1 {
		return 0, op.NotFound{}
	}
	return id, nil
}

// Render shows a page through the app's render seam, so an action from
// this package and a generated action draw with the same templates and
// the same answer when the app forgot to wire the seam.
func (visit *Visit) Render(page string, status int, data any) {
	view.Render(visit.ctx, visit.response, page, status, data)
}

// Flash queues a notice for the next page the person sees. Call it
// before Redirect: the cookie is a header, and a redirect sends them.
func (visit *Visit) Flash(message string) {
	flash.Set(visit.response, visit.request, "notice", message)
}

// Redirect answers 303 See Other, not 302, so the browser follows a
// POST with a GET and a reload does not submit the form twice.
func (visit *Visit) Redirect(path string) {
	http.Redirect(visit.response, visit.request, path, http.StatusSeeOther)
}
