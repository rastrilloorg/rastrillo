// Package view holds the plain HTTP-response helpers a generated action
// needs against a *rastrillo.Ctx: rendering a page, failing loudly but
// safely, and reading the {id} path value. These used to be stamped
// privately into every generated action file; they live here once now,
// serving generated and hand-written handlers alike.
package view

import (
	"net/http"
	"strconv"

	"github.com/carlosframework/rastrillo"
)

// Fail logs through Ctx.Logger (when set) and answers a plain 500.
func Fail(ctx *rastrillo.Ctx, w http.ResponseWriter, what string, err error) {
	if ctx.Logger != nil {
		ctx.Logger.Error(what, "err", err)
	}
	http.Error(w, "Something went wrong.", http.StatusInternalServerError)
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
