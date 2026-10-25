package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	stdpath "path"
	"runtime/debug"
	"strings"
	"unicode"

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
	// MaxBody caps a request body for Decode. Zero or less means 1 MiB.
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

// Route is one operation on the tree. Pattern is the full path as
// net/http.ServeMux reads it, such as "/api/v1/bookmarks/{id}".
type Route struct {
	Method, Pattern string
	// Op is required. Its Access decides the read-only check, and must
	// agree with Method.
	Op     op.Descriptor
	Handle func(ctx context.Context, rc *rastrillo.Ctx, w http.ResponseWriter, r *http.Request)
}

// The 422 messages for the agent headers. They are not body members,
// so they carry no fields and the message names the header instead.
const (
	msgAgentHeader  = "Send %s as 200 bytes or fewer, with no control characters."
	msgAgentSession = "Send an Agent-Session header with every write."
)

// maxAgentHeader caps Agent-Session and Agent-Harness. They are the
// client's own claims and go into every log line for the request, so
// an unbounded one would let a client write as much as it likes there.
const maxAgentHeader = 200

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
	cfg Config
	mux *http.ServeMux
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
// (422), the read-only check (403), Config.After, then the handler. A
// panic is a JSON 500 with a ref, or an aborted connection when the
// response had already started.
func New(cfg Config, routes ...Route) (http.Handler, error) {
	switch {
	case cfg.Verify == nil:
		return nil, errors.New("api: Config.Verify is required")
	case cfg.Ctx == nil:
		return nil, errors.New("api: Config.Ctx is required")
	case cfg.Authorize == nil:
		return nil, errors.New("api: Config.Authorize is required; name op.DefaultPolicy to keep the default")
	}
	for i, m := range cfg.Before {
		if m == nil {
			return nil, fmt.Errorf("api: Config.Before[%d] is nil", i)
		}
	}
	for i, m := range cfg.After {
		if m == nil {
			return nil, fmt.Errorf("api: Config.After[%d] is nil", i)
		}
	}
	t := &tree{cfg: cfg, mux: http.NewServeMux(), patterns: make(map[string]bool, len(routes))}
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
	h, err := wrap(h, t.cfg.After, "After")
	if err != nil {
		return err
	}
	h = t.gate(rt, h)
	if h, err = wrap(h, t.cfg.Before, "Before"); err != nil {
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

func wrap(h http.Handler, mw []func(http.Handler) http.Handler, field string) (http.Handler, error) {
	for i := len(mw) - 1; i >= 0; i-- {
		if h = mw[i](h); h == nil {
			return nil, fmt.Errorf("api: Config.%s[%d] returned a nil handler", field, i)
		}
	}
	return h, nil
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
	w.Header().Set("Cache-Control", "no-store")
	// The default, not only what Write sets: a handler or middleware
	// that writes bytes itself would otherwise get net/http's sniffed
	// text/plain, and one that writes only a status would get no type.
	// A handler that answers something else sets its own over this.
	w.Header().Set("Content-Type", "application/json")
	// Stamped before anything can refuse, so Refuse and Decode, and a
	// Before middleware calling WriteError, all see this tree's settings
	// rather than slog.Default and the 1 MiB default.
	r = WithLogger(r, t.cfg.Logger)
	r = WithRedact(r, t.cfg.Redact)
	r = WithMaxBody(r, t.cfg.MaxBody)

	rw := &startedWriter{ResponseWriter: w}
	// Outermost, so a panic in a Before or After middleware is answered
	// as one in a handler is: in JSON, with a ref.
	defer func() {
		if v := recover(); v != nil {
			t.recovered(rw, r, v)
		}
	}()

	if !t.matches(r) {
		if allow := t.allowed(r); allow != "" {
			rw.Header().Set("Allow", allow)
			answer(rw, "method_not_allowed")
			return
		}
		answer(rw, "not_found")
		return
	}
	// ServeHTTP, not the handler mux.Handler returned: only ServeHTTP
	// fills r.PathValue.
	t.mux.ServeHTTP(rw, r)
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

// gate is steps 4 to 6: the credential, the agent headers and the
// read-only check, in that order, then the verified actor stamped on
// the request for After and the handler.
func (t *tree) gate(rt Route, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a, err := t.cfg.Verify.Verify(r.Context(), r)
		if err != nil {
			Refuse(w, r, err)
			return
		}
		// The actor a Verifier returns is all the API knows about who is
		// calling. With no credential, op.Check and DefaultPolicy would
		// treat it as the app itself and allow everything; with no
		// access, the read-only check below would have nothing to go on.
		// Either is a Verifier bug, so it is a 500, not a guess.
		if a.Credential == "" || !a.Access.Valid() {
			Refuse(w, r, fmt.Errorf("api: Verify returned %s with no error but no credential or access", a))
			return
		}
		// Agent-Session and Agent-Harness are only an agent's to send. A
		// person's own token could otherwise pose as an agent in the log,
		// so for anyone else they are dropped unread, and whatever the
		// Verifier put in Session and Harness is dropped too: those two
		// come from the request or not at all.
		a.Session, a.Harness = "", ""
		if a.Agent != "" {
			var ok bool
			if a.Session, ok = agentHeader(w, r, "Agent-Session"); !ok {
				return
			}
			if a.Harness, ok = agentHeader(w, r, "Agent-Harness"); !ok {
				return
			}
			if t.cfg.RequireAgentSession && rt.Op.Access == rastrillo.Write && a.Session == "" {
				invalid(w, msgAgentSession, nil)
				return
			}
		}
		// Not "== Read": an Access added later would otherwise pass as a
		// writer. Only Write may write.
		if rt.Op.Access == rastrillo.Write && a.Access != rastrillo.Write {
			Refuse(w, r, op.Forbidden{Reason: "read-only credential"})
			return
		}
		next.ServeHTTP(w, rastrillo.WithActor(r, a))
	})
}

// agentHeader reads one agent header, or answers 422 and returns false.
// Control characters are refused rather than stripped: these values go
// into log lines, and a newline in one could forge a line of its own.
func agentHeader(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	v := r.Header.Get(name)
	bad := len(v) > maxAgentHeader
	for _, c := range v {
		if unicode.IsControl(c) {
			bad = true
			break
		}
	}
	if bad {
		invalid(w, fmt.Sprintf(msgAgentHeader, name), nil)
		return "", false
	}
	return v, true
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

// recovered answers a panic. Before the first byte, that is a JSON 500
// with a ref the log line shares. After it, appending JSON would corrupt
// a body the client is already reading, so the connection is aborted
// and the client sees a truncated response, not a wrong one. Either way
// the value is logged here first: net/http logs nothing for
// http.ErrAbortHandler, so without this line the panic would vanish.
func (t *tree) recovered(w *startedWriter, r *http.Request, v any) {
	if v == http.ErrAbortHandler {
		// A handler that aborted on purpose has nothing to report.
		panic(v)
	}
	// Every free-text field goes through the redactor, the path too: a
	// client that pastes its token into a path segment would otherwise
	// put the secret in the log line beside the redacted value.
	redact := redactorFrom(r)
	ref := rastrillo.NewRef()
	loggerFrom(r).Error("api: panic",
		"err", redact(fmt.Sprint(v)), "ref", ref, "method", r.Method, "path", redact(r.URL.Path),
		"stack", redact(string(debug.Stack())))
	if w.started {
		panic(http.ErrAbortHandler)
	}
	WriteError(w, codes["internal"].status, Error{Code: "internal", Message: codes["internal"].message, Ref: ref})
}

// startedWriter records whether the response has started, which decides
// what a panic can still do about it.
type startedWriter struct {
	http.ResponseWriter
	started bool
}

func (w *startedWriter) WriteHeader(status int) {
	w.started = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *startedWriter) Write(b []byte) (int, error) {
	w.started = true
	return w.ResponseWriter.Write(b)
}

// FlushError is here, not left to Unwrap, because a flush sends the
// status line: a handler that flushed before writing has started the
// response as surely as one that wrote.
func (w *startedWriter) FlushError() error {
	w.started = true
	return http.NewResponseController(w.ResponseWriter).Flush()
}

// Flush keeps the wrapper an http.Flusher for a handler that asserts
// the interface rather than using http.ResponseController.
func (w *startedWriter) Flush() { _ = w.FlushError() }

// Unwrap lets http.ResponseController reach the real writer for
// everything else, such as deadlines.
func (w *startedWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
