// Package api is the JSON door: the handler tree that serves an app's
// operations to token callers, and the small set of functions a
// hand-written handler uses to answer the same way the generated ones do.
//
// Every error on the wire has one shape, {"error": {"code", "message",
// "fields", "ref"}}, and every code maps to exactly one status. Callers
// branch on the code, never on the prose, so the English can change
// without breaking a client.
package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	json "github.com/go-json-experiment/json"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/wirename"
	"amadan.net/rastrillo/rastrillo/op"
)

// ErrUnauthorized and ErrUnavailable are the two answers a Verifier
// gives when it cannot name an actor. A Verifier wraps them (errors.Is
// matches), so it can keep its own detail for the log while Refuse still
// picks the right status: 401 for a credential that does not work, 503
// for a store that could not be asked. The two must stay distinct: a
// client that retries a 503 must not retry a 401, and one that discards
// its token on a 401 must not discard it on a 503.
var (
	ErrUnauthorized = errors.New("rastrillo/api: unauthorized")
	ErrUnavailable  = errors.New("rastrillo/api: credential store unavailable")
)

// Error is the body of every refusal, under an "error" member. Code is
// one of the eleven in the codes table; Fields is set only for invalid,
// keyed by wire name; Ref is set only for internal, so the person who
// reports the failure can quote what the log line carries.
type Error struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
	Ref     string            `json:"ref,omitempty"`
}

// refusal is one row of the codes table: the status a code answers with
// and the sentence it carries. The table is the only place the English
// lives, so Decode and the tree (which answer 413, 415, 404, 405 and the
// panic 500 themselves) spell each message exactly once.
type refusal struct {
	status  int
	message string
}

var codes = map[string]refusal{
	"unauthorized":           {http.StatusUnauthorized, "Send a valid API token in the Authorization header."},
	"forbidden":              {http.StatusForbidden, "This token can't do that."},
	"not_found":              {http.StatusNotFound, "Nothing here."},
	"method_not_allowed":     {http.StatusMethodNotAllowed, "This address doesn't accept that method."},
	"conflict":               {http.StatusConflict, ""}, // the message is Conflict.Message
	"too_large":              {http.StatusRequestEntityTooLarge, "The request body is too large."},
	"unsupported_media_type": {http.StatusUnsupportedMediaType, "Send the body as JSON, with Content-Type: application/json."},
	"invalid":                {http.StatusUnprocessableEntity, "Check the fields and try again."},
	"rate_limited":           {http.StatusTooManyRequests, ""}, // a Config.Before limiter writes its own, with Retry-After
	"internal":               {http.StatusInternalServerError, "Something went wrong. Quote the reference if you report it."},
	"unavailable":            {http.StatusServiceUnavailable, "Try again in a moment."},
}

// answer writes one code from the table. It panics on a code the table
// does not have, because that is a typo in this package, not a runtime
// condition: a client must never see a code the spec does not list.
func answer(w http.ResponseWriter, code string) {
	c, ok := codes[code]
	if !ok {
		panic("api: no such error code " + strconv.Quote(code))
	}
	WriteError(w, c.status, Error{Code: code, Message: c.message})
}

// Write answers with v as JSON. It sets Content-Type and Cache-Control
// no-store: an API answer is scoped to the token that asked, so a shared
// cache must never hand it to the next caller.
//
// The body is encoded before any header is written, so an unencodable
// value panics with nothing on the wire. The tree's recoverer then
// answers a 500 with a ref and logs it; a quiet 500 from here would
// have no logger to tell. json v2 writes a nil slice as [] and a nil
// map as {}, which is what the spec promises for an empty list.
func Write(w http.ResponseWriter, status int, v any) {
	// Deterministic sorts map keys, so a fields map encodes the same way
	// every time and a client's test can compare bodies as strings.
	b, err := json.Marshal(v, json.Deterministic(true))
	if err != nil {
		panic(fmt.Errorf("api: encode %T: %w", v, err))
	}
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	// Content-Length is set by hand so a HEAD answer, whose body net/http
	// discards, still tells the client how long the GET would be.
	h.Set("Content-Length", strconv.Itoa(len(b)))
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

// WriteError answers with e under an "error" member. Use it for the
// codes Refuse cannot derive from an error value: a rate limiter in
// Config.Before answers 429 this way, with its own Retry-After header.
func WriteError(w http.ResponseWriter, status int, e Error) {
	Write(w, status, struct {
		Error Error `json:"error"`
	}{e})
}

// Refuse answers err the way the codes table says. op refusals map to
// their codes, with Invalid's fields re-keyed to wire names; the two
// sentinels map to 401 (with a Bearer challenge, as RFC 6750 asks) and
// 503. Anything else is a 500 that says nothing about the cause: the
// error text goes to the log under a fresh ref, after the redactor has
// taken any token out of it, and the response carries only the ref.
//
// Forbidden.Reason is logged too, at Info, because it never reaches
// the response: a policy that explained itself would otherwise explain
// to nobody.
func Refuse(w http.ResponseWriter, r *http.Request, err error) {
	var (
		invalid   op.Invalid
		notFound  op.NotFound
		forbidden op.Forbidden
		conflict  op.Conflict
	)
	switch {
	case errors.As(err, &invalid):
		fields := make(map[string]string, len(invalid.Fields))
		for name, msg := range invalid.Fields {
			fields[FieldName(name)] = msg
		}
		WriteError(w, codes["invalid"].status, Error{
			Code:    "invalid",
			Message: codes["invalid"].message,
			Fields:  fields,
		})
	case errors.As(err, &notFound):
		answer(w, "not_found")
	case errors.As(err, &forbidden):
		if forbidden.Reason != "" {
			loggerFrom(r).Info("api: forbidden", "reason", redactorFrom(r)(forbidden.Reason), "method", method(r), "path", path(r))
		}
		answer(w, "forbidden")
	case errors.As(err, &conflict):
		WriteError(w, codes["conflict"].status, Error{Code: "conflict", Message: conflict.Message})
	case errors.Is(err, ErrUnauthorized):
		w.Header().Set("WWW-Authenticate", "Bearer")
		answer(w, "unauthorized")
	case errors.Is(err, ErrUnavailable):
		answer(w, "unavailable")
	default:
		ref := rastrillo.NewRef()
		loggerFrom(r).Error("api: internal error", "err", redactorFrom(r)(err.Error()), "ref", ref, "method", method(r), "path", path(r))
		WriteError(w, codes["internal"].status, Error{
			Code:    "internal",
			Message: codes["internal"].message,
			Ref:     ref,
		})
	}
}

func method(r *http.Request) string {
	if r == nil {
		return ""
	}
	return r.Method
}

// path is redacted here, not at each call, so no log line can carry the
// path raw: a client that pastes its token into a path segment would
// otherwise have the secret logged beside a redacted message.
func path(r *http.Request) string {
	if r == nil || r.URL == nil {
		return ""
	}
	return redactorFrom(r)(r.URL.Path)
}

// The logger and redactor ride on the request context rather than on a
// package variable, so two trees in one process (a test's and the
// app's) do not fight over them, and so Refuse keeps the signature a
// hand-written handler can call without holding a Config.
type (
	loggerCtxKey struct{}
	redactCtxKey struct{}
)

// WithLogger stamps the logger Refuse writes to. The tree does this for
// every request from Config.Logger; a test does it to read the log line.
func WithLogger(r *http.Request, l *slog.Logger) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), loggerCtxKey{}, l))
}

// WithRedact stamps the function Refuse passes error text through
// before logging it. The tree sets Config.Redact here, so a token quoted
// inside an error message reaches the log as its public ID only.
func WithRedact(r *http.Request, redact func(string) string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), redactCtxKey{}, redact))
}

func loggerFrom(r *http.Request) *slog.Logger {
	if r != nil {
		if l, ok := r.Context().Value(loggerCtxKey{}).(*slog.Logger); ok && l != nil {
			return l
		}
	}
	return slog.Default()
}

func redactorFrom(r *http.Request) func(string) string {
	if r != nil {
		if f, ok := r.Context().Value(redactCtxKey{}).(func(string) string); ok && f != nil {
			return f
		}
	}
	return func(s string) string { return s }
}

// FieldName turns a manifest field name into its wire name: "Title" is
// "title", "MaxPerOrder" is "max_per_order", "IPAddress" is "ip_address".
//
// It is the generator's sqlName with one more rule: an uppercase run
// splits before its last letter when a lowercase letter follows, so an
// acronym stays one word. sqlName has no such rule because it never
// needed one: a column called ipaddress is merely ugly, but a client
// reading "ipaddress" on the wire would not know where the words are.
// The rule lives in internal/wirename, which Resource.Validate also uses,
// so a filter name is checked against the name this function produces.
func FieldName(manifestName string) string {
	return wirename.Name(manifestName)
}
