package rastrillo

import (
	"database/sql"
	"log/slog"
)

// Actor identifies who is calling an action: a human request or a named
// agent. See the design doc §8 — every action's caller is attributed,
// never anonymous, so audit trails can say who did what honestly.
type Actor struct {
	Human bool
	Name  string // empty for a human; the agent's name otherwise
}

// String is the actor's audit-trail form: "human" or "agent:<name>" —
// the encoding eventlog stores on every appended event, so a stream
// always says who did what without importing this package.
func (a Actor) String() string {
	if a.Human || a.Name == "" {
		return "human"
	}
	return "agent:" + a.Name
}

// Ctx is passed to every action: the app's own wiring — its database,
// logger, asset registry, and the Render seam generated actions call
// through — built once by the app's ctxFactory. Per-request state
// doesn't live here: identity lives in sessions.Current(r) /
// sessions.UserID(r), and locale is rastrillo.LocaleFrom(r), both read
// straight off the request rather than staged onto Ctx.
type Ctx struct {
	DB     *sql.DB
	Logger *slog.Logger

	// Assets is the app's fingerprinted static-file registry, when
	// the app wires one — the scaffold does, over its embedded
	// static/ tree. Actions link assets by hashed URL:
	// ctx.Assets.Path("static/tokens.css"). Nil for an app that
	// serves assets some other way — the same contract as DB.
	Assets *Assets

	// Actor records who is calling this action (design doc §8).
	Actor Actor

	// Render is the manifest system's seam (design doc's manifest
	// slice): generated actions cannot call an app-private helper like
	// a hand-rolled blog.Render, so they call ctx.Render instead. The
	// app's ctx factory sets it (e.g. &rastrillo.Ctx{DB: db, Render:
	// blog.Render}); a generated action nil-checks it and answers a
	// logged 500 rather than a nil-pointer panic when an app forgets
	// to wire it. See RenderFunc and internal/generate's action
	// emitter for the exact page names a generated action calls it
	// with.
	Render RenderFunc

	// ErrorPage renders the app's own error page — the seam view.Fail,
	// view.NotFound and view.Forbidden call so that a failure inside a
	// generated action looks like the rest of the app instead of
	// net/http's bare text. Wire the same function to
	// Options.ErrorPage and a panic gets the identical page:
	//
	//	page := func(w http.ResponseWriter, r *http.Request, status int, ref string) {
	//		blog.RenderError(w, r, status, ref) // ui's "error-page" partial
	//	}
	//	// in the ctx factory: &rastrillo.Ctx{DB: db, ErrorPage: page}
	//	// in Options:        ErrorPage: page
	//
	// Nil is legal and is the default: the helpers answer plain text,
	// which is honest, ugly, and exactly what an app that has not
	// thought about its error pages should see.
	ErrorPage ErrorPageFunc
}
