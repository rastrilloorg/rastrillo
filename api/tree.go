package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	stdpath "path"
	"strings"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/op"
)

// Verifier turns a request's credential into an Actor. apitoken.Store
// implements it; OAuth grants will be a second implementation.
//
// When it cannot name an actor it returns an error wrapping
// ErrUnauthorized (401) or ErrUnavailable (503). The actor it returns
// must carry a Credential and a valid Access: the tree refuses anything
// less with a 500, because a credential-less actor would be served as
// the app itself.
type Verifier interface {
	Verify(ctx context.Context, r *http.Request) (rastrillo.Actor, error)
}

// Config is the tree's wiring. Verify, Ctx and Authorize are required.
type Config struct {
	// Verify checks the credential on every matched request.
	Verify Verifier
	// Ctx is the app's ctx factory, the same one its pages use. The
	// tree sets Actor and Authorize on a copy of what it returns.
	Ctx func(*http.Request) *rastrillo.Ctx
	// Authorize is the app's policy, set as every handler's
	// rc.Authorize. It is required so that turning on the API is a
	// choice made in writing: op.DefaultPolicy is a valid one to name.
	Authorize func(ctx context.Context, a rastrillo.Actor, o rastrillo.Operation) error
	// Logger receives refusals and panics. Nil means slog.Default().
	Logger *slog.Logger
	// MaxBody caps a request body. Zero or less means 1 MiB.
	MaxBody int64
	// Before runs on every matched request before the credential
	// check, first element outermost. A rate limiter goes here, so it
	// counts the requests that fail the check too.
	Before []func(http.Handler) http.Handler
	// After runs once the actor is known and allowed this route,
	// first element outermost; rastrillo.ActorFromContext names it.
	After []func(http.Handler) http.Handler
	// RequireAgentSession refuses a write from an agent token that
	// sends no Agent-Session header.
	RequireAgentSession bool
	// Redact takes token-shaped text out of what the tree and Refuse
	// log. (*apitoken.Store).Redact implements it. Nil redacts nothing.
	Redact func(string) string
}

// Gate returns the pipeline half of the tree's Config. Config keeps its
// own fields, because embedding Gate would break every existing
// api.Config{Verify: ...} literal: Go takes no promoted field as a key.
func (configuration Config) Gate() Gate {
	return Gate{
		Verify:              configuration.Verify,
		Logger:              configuration.Logger,
		MaxBody:             configuration.MaxBody,
		Before:              configuration.Before,
		After:               configuration.After,
		RequireAgentSession: configuration.RequireAgentSession,
		Redact:              configuration.Redact,
	}
}

// Route is one operation on the tree. Pattern is the full path as
// net/http.ServeMux reads it, such as "/api/v1/bookmarks/{id}".
type Route struct {
	Method, Pattern string
	// Op is required. Its Access decides the read-only check, and must
	// agree with Method.
	Op     op.Descriptor
	Handle func(ctx context.Context, rc *rastrillo.Ctx, w http.ResponseWriter, r *http.Request)
}

// methodAccess is the access each method must declare. GET and HEAD
// must be Read so a read-only token can use them; every other method
// must be Write so a read-only token cannot. A route that disagreed
// would let the method say one thing and the read-only check another.
var methodAccess = map[string]rastrillo.Access{
	http.MethodGet:    rastrillo.Read,
	http.MethodHead:   rastrillo.Read,
	http.MethodPost:   rastrillo.Write,
	http.MethodPut:    rastrillo.Write,
	http.MethodPatch:  rastrillo.Write,
	http.MethodDelete: rastrillo.Write,
}

// allowOrder is the order the Allow header lists methods in.
var allowOrder = []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}

type tree struct {
	cfg  Config
	gate Gate
	mux  *http.ServeMux
	// patterns is every pattern string the tree registered. The inner
	// mux can match a request and still hand back something that is not
	// one of them (a redirect it made up); only a registered pattern
	// counts as a match.
	patterns map[string]bool
}

// New builds the /api/v1 handler tree. It checks every route here, at
// boot, so a route that would answer wrongly fails the app's start
// instead of a client's request.
//
// Every request gets Cache-Control: no-store and a JSON answer, in this
// order, each step's refusal winning over every later one: routing (404,
// 405), Config.Before, the credential (401, 503), the agent headers
// (422), the agent session and read-only checks (422, 403), Config.After,
// then the handler. Its answer is held, up to 2 MiB, and goes out only
// once whole; when Config.Verify is a Rechecker, only while the
// credential and the route's checks still pass. A panic is a JSON 500
// with a ref, or an aborted connection when the answer had started.
func New(cfg Config, routes ...Route) (http.Handler, error) {
	gate := cfg.Gate()
	if err := gate.Validate(); err != nil {
		return nil, err
	}
	switch {
	case cfg.Ctx == nil:
		return nil, errors.New("api: Config.Ctx is required")
	case cfg.Authorize == nil:
		return nil, errors.New("api: Config.Authorize is required; name op.DefaultPolicy to keep the default")
	}
	t := &tree{cfg: cfg, gate: gate, mux: http.NewServeMux(),
		patterns: make(map[string]bool, len(routes))}
	for _, rt := range routes {
		if err := t.add(rt); err != nil {
			return nil, err
		}
	}
	return t, nil
}

func (t *tree) add(rt Route) error {
	name := rt.Method + " " + rt.Pattern
	want, known := methodAccess[rt.Method]
	switch {
	case !known:
		return fmt.Errorf("api: route %s: method must be one of GET, HEAD, POST, PUT, PATCH, DELETE", name)
	case rt.Op.ID == "":
		return fmt.Errorf("api: route %s: Op.ID is empty", name)
	case !rt.Op.Access.Valid():
		return fmt.Errorf("api: route %s (%s): Op.Access is not Read or Write", name, rt.Op.ID)
	case rt.Op.Access != want:
		return fmt.Errorf("api: route %s (%s): Op.Access is %s, but %s must be %s", name, rt.Op.ID, rt.Op.Access, rt.Method, want)
	case len(rt.Op.Input) == 0:
		return fmt.Errorf("api: route %s (%s): Op.Input is empty", name, rt.Op.ID)
	case len(rt.Op.Result) == 0:
		return fmt.Errorf("api: route %s (%s): Op.Result is empty", name, rt.Op.ID)
	case rt.Handle == nil:
		return fmt.Errorf("api: route %s (%s): Handle is nil", name, rt.Op.ID)
	case t.patterns[name]:
		return fmt.Errorf("api: route %s (%s): another route has the same method and pattern", name, rt.Op.ID)
	}

	h := http.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.handle(rt, w, r) }))
	admit := func(actor rastrillo.Actor) error {
		return t.gate.Admit(actor, rt.Op.Operation)
	}
	h, err := t.gate.compose(admit, h)
	if err != nil {
		return err
	}
	// ServeMux.Handle panics on a malformed pattern or one that
	// conflicts with another; New promises an error instead.
	if err := register(t.mux, name, h); err != nil {
		return fmt.Errorf("api: route %s (%s): %v", name, rt.Op.ID, err)
	}
	t.patterns[name] = true
	return nil
}

func register(mux *http.ServeMux, pattern string, h http.Handler) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("%v", v)
		}
	}()
	mux.Handle(pattern, h)
	return nil
}

func (t *tree) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	t.gate.serve(w, r, http.HandlerFunc(t.route))
}

// route answers 404 and 405 itself, before Before runs: with no route
// there is no operation, and no actor yet to recheck an answer for.
func (tree *tree) route(
	response http.ResponseWriter, request *http.Request,
) {
	if !tree.matches(request) {
		if allow := tree.allowed(request); allow != "" {
			response.Header().Set("Allow", allow)
			answer(response, "method_not_allowed")
			return
		}
		answer(response, "not_found")
		return
	}
	// ServeHTTP, not the handler mux.Handler returned: only ServeHTTP
	// fills r.PathValue.
	tree.mux.ServeHTTP(response, request)
}

// matches reports whether r reaches a registered route. It never lets
// the inner mux answer for itself: the mux's 404 and 405 are plain
// text, and for a path that is not clean it answers a 301 in HTML. The
// outer ServeMux has already cleaned the path when the tree is mounted
// as the scaffold mounts it, so an unclean path here is a 404.
func (t *tree) matches(r *http.Request) bool {
	if p := r.URL.EscapedPath(); p != cleanPath(p) {
		return false
	}
	_, pattern := t.mux.Handler(r)
	return t.patterns[pattern]
}

// allowed lists the methods the path answers to, for a 405's Allow
// header, or "" when it answers to none and the request is a 404.
// ServeMux keeps its own list unexported, so each method is asked for.
func (t *tree) allowed(r *http.Request) string {
	var methods []string
	probe := new(http.Request)
	for _, m := range allowOrder {
		*probe = *r
		probe.Method = m
		if t.matches(probe) {
			methods = append(methods, m)
		}
	}
	return strings.Join(methods, ", ")
}

// cleanPath is net/http's own: path.Clean that keeps a trailing slash.
// It decides whether ServeMux would redirect, and so must agree with it.
func cleanPath(p string) string {
	if p == "" {
		return "/"
	}
	if p[0] != '/' {
		p = "/" + p
	}
	np := stdpath.Clean(p)
	if p[len(p)-1] == '/' && np != "/" {
		np += "/"
	}
	return np
}

// handle is step 7's end: the route's handler, with the app's Ctx.
func (t *tree) handle(rt Route, w http.ResponseWriter, r *http.Request) {
	c := t.cfg.Ctx(r)
	if c == nil {
		Refuse(w, r, errors.New("api: Config.Ctx returned nil"))
		return
	}
	// A copy, because a factory may hand every request the same Ctx.
	// Writing this request's actor into a shared one would show it to
	// the next request, under another token.
	rc := *c
	// The gate stamped it; After cannot have removed it, only replaced
	// it, which is After's to do.
	rc.Actor, _ = rastrillo.ActorFromContext(r.Context())
	rc.Authorize = t.cfg.Authorize
	t.disclose(r.Context(), rt, rc)
	// One line per request that reaches a handler, with the actor's
	// audit string and the agent's own session and harness claims. An
	// app with no eventlog has nothing else that says which token made
	// a write, so a revoked token's last act would otherwise be
	// untraceable. Before the handler, not after: a handler that panics
	// or hangs still leaves the line. Every value but the method goes
	// through the redactor: the path, and the session and harness the
	// client wrote itself, can each carry a pasted token. The
	// Authorization header is never a field here.
	redact := redactorFrom(r)
	attrs := []slog.Attr{slog.String("method", r.Method), slog.String("path", path(r))}
	for _, a := range rc.Actor.LogAttrs() {
		attrs = append(attrs, slog.String(a.Key, redact(a.Value.String())))
	}
	loggerFrom(r).LogAttrs(r.Context(), slog.LevelInfo, "api: request", attrs...)
	rt.Handle(r.Context(), &rc, w, r)
}

// disclose registers the route's own checks for the gate to run again,
// with the fresh actor, while the answer goes out: the read-only check
// and the app's policy, for the operation that ran.
func (tree *tree) disclose(
	ctx context.Context, route Route, rc rastrillo.Ctx,
) {
	Disclosed(ctx, func(ctx context.Context, fresh rastrillo.Actor) error {
		if err := tree.gate.Admit(fresh, route.Op.Operation); err != nil {
			return err
		}
		checked := rc
		checked.Actor = fresh
		return op.Check(ctx, &checked, route.Op.Operation)
	})
}
