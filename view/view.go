// Package view holds the plain HTTP-response helpers a generated action
// needs against a *rastrillo.Ctx: rendering a page, failing loudly but
// safely, and reading the {id} path value. These used to be stamped
// privately into every generated action file; they live here once now,
// serving generated and hand-written handlers alike.
package view

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"amadan.net/rastrillo/rastrillo"
)

// Fail logs through Ctx.Logger (when set) and answers a 500 — the app's
// own error page when Ctx.ErrorPage is wired, JSON when the client asked
// for JSON, and plain text otherwise.
//
// It mints a reference (rastrillo.NewRef) and puts it in both places:
// the log line, under "ref", and the response. That is the only thing
// the reference is for — a person quotes six characters, and an operator
// greps for them.
//
// The response never carries the error itself. An error string is
// written for an operator and routinely names a table, a path or a
// query; none of that belongs in a reply to someone who may have caused
// the error deliberately.
//
// r may be nil, for a caller with no request in scope: the page callback
// takes a request, so a nil one falls back to plain text (and there is
// no Accept header to sniff either).
func Fail(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request, what string, err error) {
	ref := rastrillo.NewRef()
	if ctx.Logger != nil {
		ctx.Logger.Error(what, "err", err, "ref", ref)
	}
	if writeJSON(w, r, http.StatusInternalServerError, ref) {
		return
	}
	if ctx.ErrorPage != nil && r != nil {
		ctx.ErrorPage(w, r, http.StatusInternalServerError, ref)
		return
	}
	http.Error(w, "Something went wrong.", http.StatusInternalServerError)
}

// NotFound answers 404 the same three ways Fail answers 500 — the app's
// page, JSON, or net/http's own text. No reference and no log line: a
// 404 is a URL that was never ours, not a failure to look up later.
func NotFound(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
	if writeJSON(w, r, http.StatusNotFound, "") {
		return
	}
	if ctx.ErrorPage != nil && r != nil {
		ctx.ErrorPage(w, r, http.StatusNotFound, "")
		return
	}
	http.NotFound(w, r)
}

// Forbidden answers 403 like NotFound answers 404. It says nothing about
// what exists: "you can't see this" and "there is nothing here" are
// deliberately the same amount of information to an unauthorized caller
// beyond the status itself.
func Forbidden(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
	if writeJSON(w, r, http.StatusForbidden, "") {
		return
	}
	if ctx.ErrorPage != nil && r != nil {
		ctx.ErrorPage(w, r, http.StatusForbidden, "")
		return
	}
	http.Error(w, "Forbidden.", http.StatusForbidden)
}

// wantsJSON reports whether the caller asked for JSON. The sniff is
// deliberately crude — a substring of Accept, not a parsed q-list —
// because fetch() and every JSON client send exactly this, and a browser
// navigation never does. A nil request wants nothing.
func wantsJSON(r *http.Request) bool {
	return r != nil && strings.Contains(r.Header.Get("Accept"), "application/json")
}

// writeJSON answers a JSON client and reports whether it did, so the
// three helpers above can share one shape: {"status":404}, plus "ref"
// when there is one. The app's ErrorPage is not consulted on this path
// at all — that callback renders HTML, and this caller asked for
// something it can parse.
func writeJSON(w http.ResponseWriter, r *http.Request, status int, ref string) bool {
	if !wantsJSON(r) {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := struct {
		Status int    `json:"status"`
		Ref    string `json:"ref,omitempty"`
	}{status, ref}
	// An encode error here means the connection is already gone; there
	// is nothing left to say to the client about it.
	_ = json.NewEncoder(w).Encode(body)
	return true
}

// Render hands data to the app's template tree through ctx.Render (see
// rastrillo.Ctx's Render field) — a 500 with a clear log line stands in
// for a template an app forgot to wire, rather than a nil-pointer panic.
func Render(ctx *rastrillo.Ctx, w http.ResponseWriter, page string, status int, data any) {
	if ctx.Render == nil {
		if ctx.Logger != nil {
			ctx.Logger.Error("Ctx.Render is nil; the app's ctx factory must set it")
		}
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
		return
	}
	ctx.Render(ctx, w, page, status, data)
}

// ParseID reads the {id} path value. A non-numeric id is a URL that
// was never ours, so the caller answers 404 rather than 400.
func ParseID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		return 0, false
	}
	return id, true
}
