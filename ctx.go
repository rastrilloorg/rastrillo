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

// Ctx is passed to every action. It is the one extension point for
// per-request state a manifest or middleware needs to add — see Scope.
type Ctx struct {
	DB     *sql.DB
	Logger *slog.Logger

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
}
