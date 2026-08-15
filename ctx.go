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

// Ctx is passed to every action. It is the one extension point for
// per-request state a manifest or middleware needs to add — see Scope.
type Ctx struct {
	DB     *sql.DB
	Logger *slog.Logger

	// Assets is the app's fingerprinted static-file registry, when
	// the app wires one — the scaffold does, over its embedded
	// static/ tree. Actions link assets by hashed URL:
	// ctx.Assets.Path("static/tokens.css"). Nil for an app that
	// serves assets some other way — the same contract as DB.
	Assets *Assets

	// Locale is the resolved locale for this request (design doc §10).
	// The v1 request-scoped surface for localization is
	// rastrillo.LocaleFrom(r) / rastrillo.T(r, ...) / rastrillo.Tf(r,
	// ...), which every action already has via r; this field stays
	// empty because the generated router's default ctxFactory doesn't
	// look at the request at all. An app can populate it by supplying
	// its own per-request Ctx factory (the seam the generated router
	// leaves open) that sets Locale from rastrillo.LocaleFrom(r).
	Locale string

	// Actor records who is calling this action (design doc §8).
	Actor Actor

	// Scope is resolved by app-level middleware (_middleware.go, design
	// doc §4) and type-asserted by the handler. Rastrillo never reads it.
	Scope any

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
}
