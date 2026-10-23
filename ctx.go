package rastrillo

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
)

// Access is what a credential, or an operation, may do.
//
// The zero value is deliberately invalid. The old Access made zero mean
// read, so a route that forgot to declare its access was readable by a
// read-only token. Now a forgotten Access fails Valid and is refused.
type Access int

const (
	// Read observes and never changes state.
	Read Access = 1
	// Write changes state. A read-only credential may not call it.
	Write Access = 2
)

// Valid reports whether a is Read or Write.
func (a Access) Valid() bool { return a == Read || a == Write }

func (a Access) String() string {
	switch a {
	case Read:
		return "read"
	case Write:
		return "write"
	}
	return "invalid"
}

// Operation describes one thing an actor can ask the app to do, in the
// terms Ctx.Authorize needs to decide. It lives in this package, not in
// op, because Actor needs Access and Ctx needs Operation while op
// imports this package for Ctx: defining them in op would be an import
// cycle.
type Operation struct {
	ID       string // stable: "bookmarks.create"
	Resource string // "bookmarks"
	Scoped   bool   // the resource is scope = "user": each row belongs to one person
	Access   Access // Read or Write
}

// Actor identifies who is calling: the person, and the agent acting for
// them when there is one. Every caller is attributed, so an audit trail
// can say who did what honestly. The zero Actor is the app itself: a
// sidecar, a job, a migration.
//
// An API request's Subject, Agent, Credential and Access all come from
// the verified token, never from the request, so an agent cannot claim
// to be someone else. Session and Harness are the client's own claims,
// recorded as claims.
type Actor struct {
	// Subject is the person: sessions.Session.Subject, or the token
	// owner's. The same TEXT subject sessions and scoped stores use.
	Subject string
	// Agent is empty when the person acts directly, and the agent's
	// name ("Claude") when an agent acts for them.
	Agent string
	// Credential is the token's public ID; empty for a browser session.
	Credential string
	// Access is what the credential allows; Write for a session.
	Access Access
	// Session is the agent's own session id, when it sent one.
	Session string
	// Harness is the agent's client, e.g. "claude-code/2.1.288", when
	// it sent one.
	Harness string
}

// String is the actor's compact audit form, the encoding eventlog
// stores on every appended event:
//
//	app
//	user:<subject>
//	user:<subject> token:<public id>
//	agent:<agent> for user:<subject> token:<public id>
//
// The token's public ID is in it so that two tokens both named "Claude"
// stay distinguishable. Nothing in this repo parses the string.
func (a Actor) String() string {
	if a.Subject == "" && a.Agent == "" && a.Credential == "" {
		return "app"
	}
	who := "user:" + a.Subject
	if a.Agent != "" {
		who = "agent:" + a.Agent + " for " + who
	}
	if a.Credential != "" {
		who += " token:" + a.Credential
	}
	return who
}

// LogAttrs is the actor as structured log fields. Session and harness
// are logged per request here rather than stored in every event, and
// only when the client sent them.
func (a Actor) LogAttrs() []slog.Attr {
	attrs := []slog.Attr{slog.String("actor", a.String())}
	if a.Session != "" {
		attrs = append(attrs, slog.String("agent_session", a.Session))
	}
	if a.Harness != "" {
		attrs = append(attrs, slog.String("agent_harness", a.Harness))
	}
	return attrs
}

// IsToken reports whether the caller came in on a token rather than a
// browser session, the app, or an anonymous request.
func (a Actor) IsToken() bool { return a.Credential != "" }

// ActorPhrase is how a page says who did something: "" for the app,
// the person's name, or "Claude for Paul" when an agent acted for them.
// It is the words only, with no badge or emoji; the app styles them.
//
// name turns a subject into the name a person recognises, because only
// the app knows how it names its users; nil, or a name it cannot find,
// shows the subject, so an agent is never "for" nobody. t translates
// the "{agent} for {person}" sentence (rastrillo.ui.agent_for) with
// name/value pairs, as Locales.Tf does; it must not be nil. Both are
// parameters, not lookups this function makes, so it needs no request
// and a list page can name every row's actor in Go.
func ActorPhrase(a Actor, name func(subject string) string, t func(key string, args ...any) string) string {
	if a.Subject == "" && a.Agent == "" {
		return ""
	}
	person := ""
	if name != nil {
		person = name(a.Subject)
	}
	if person == "" {
		person = a.Subject
	}
	if a.Agent == "" {
		return person
	}
	// One translated sentence, not a concatenation: word order around
	// "for" differs by language, and the translator owns it.
	return t("rastrillo.ui.agent_for", "agent", a.Agent, "person", person)
}

type actorCtxKey struct{}

// WithActor stamps who is making this request onto its context. The
// generated router copies it onto Ctx.Actor after the app's ctxFactory
// runs, so an app factory that doesn't set Actor still gets honest
// attribution.
func WithActor(r *http.Request, a Actor) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), actorCtxKey{}, a))
}

// ActorFromContext reports the actor WithActor stamped, if any.
func ActorFromContext(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(actorCtxKey{}).(Actor)
	return a, ok
}

// Ctx is passed to every action: the app's own wiring — its database,
// logger, asset registry, and the Render seam generated actions call
// through — built by the app's ctxFactory, plus Actor, who this request
// is for. Both routers and the API tree copy the factory's Ctx before
// they set Actor, so a factory may return the same Ctx every time; the
// copy is what keeps one request's actor off the next. The rest of
// per-request state stays on the request: the session is
// sessions.Current(r), and locale is rastrillo.LocaleFrom(r).
type Ctx struct {
	DB     *sql.DB
	Logger *slog.Logger

	// Assets is the app's fingerprinted static-file registry, when
	// the app wires one — the scaffold does, over its embedded
	// static/ tree. Actions link assets by hashed URL:
	// ctx.Assets.Path("static/tokens.css"). Nil for an app that
	// serves assets some other way — the same contract as DB.
	Assets *Assets

	// Actor records who is calling this action.
	Actor Actor

	// Authorize is the app's one authorization policy. Both doors, the
	// HTML pages and the JSON API, pass through it, so a rule such as
	// "only admins create ticket types" is written once and cannot be
	// forgotten by one door. Its error is the answer: nil allows. Nil
	// means the default policy, op.DefaultPolicy.
	Authorize func(ctx context.Context, a Actor, o Operation) error

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
