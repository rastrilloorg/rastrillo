# 🤖 api

`amadan.net/rastrillo/rastrillo/api`

The JSON door. This page covers the handler tree, the error shape, and
the functions a hand-written handler uses to answer the way generated
ones do. [JSON API](/docs/api) is the guide: how to mount the tree and
write your policy.

## New

```go
func New(cfg Config, routes ...Route) (http.Handler, error)

type Config struct {
	Verify              Verifier
	Ctx                 func(*http.Request) *rastrillo.Ctx
	Authorize           func(ctx context.Context, a rastrillo.Actor, o rastrillo.Operation) error
	Logger              *slog.Logger
	MaxBody             int64
	Before              []func(http.Handler) http.Handler
	After               []func(http.Handler) http.Handler
	RequireAgentSession bool
	Redact              func(string) string
}

type Route struct {
	Method, Pattern string
	Op              op.Descriptor
	Handle          func(ctx context.Context, rc *rastrillo.Ctx, w http.ResponseWriter, r *http.Request)
}

type Verifier interface {
	Verify(ctx context.Context, r *http.Request) (rastrillo.Actor, error)
}
```

`New` builds the `/api/v1` handler. Mount it at both `/api/v1` and
`/api/v1/`.

`Verify`, `Ctx` and `Authorize` are required. To keep the default
policy, pass `op.DefaultPolicy`. `New` returns an error if one is
missing, or if a route:

- has no `Op.ID`, `Op.Input`, `Op.Result` or `Handle`;
- has an `Op.Access` that does not match its method: GET and HEAD must
  be `Read`, and POST, PUT, PATCH and DELETE must be `Write`;
- has the same method and pattern as another route.

Every answer is JSON with `Cache-Control: no-store`. A request goes
through these steps in order, and the first one that refuses answers:

1. Routing: 404 for an unknown path, and 405 with `Allow` for a known
   path with the wrong method. A GET route also answers HEAD.
2. `Config.Before`, such as a rate limiter.
3. `Verify`: 401 or 503.
4. The agent headers: 422.
5. A read-only token on a `Write` route: 403.
6. `Config.After`, then the route's `Handle`.

`Handle` gets the `Ctx` that `Config.Ctx` builds, with `Actor` set to
the verified caller and `Authorize` set to `Config.Authorize`. `Pattern`
is the full path, such as `/api/v1/bookmarks/{id}`, and `r.PathValue`
works in `Handle`.

Before `Handle` runs, the tree logs one Info line to `Config.Logger`,
`api: request`, with `method`, `path` and the actor's
`Actor.LogAttrs`: `actor`, and `agent_session` and `agent_harness`
when they were sent. Every value but the method goes through
`Config.Redact`.

A `Verifier` names the caller. When it cannot, wrap `ErrUnauthorized` or
`ErrUnavailable` in the error it returns. The actor it returns must have
a `Credential` and an `Access`, or the answer is 500.

An agent may send `Agent-Session` and `Agent-Harness`. They are read only
for an agent's token, and copied to `Actor.Session` and `Actor.Harness`.
Each must be 200 bytes or fewer, with no control characters, or the
answer is 422. Set `RequireAgentSession` to refuse a write from an agent
token that sends no `Agent-Session`.

If a handler panics before writing, the answer is 500 with a `ref`. If
it panics after, the connection is closed. Either way the panic is
logged to `Config.Logger` under the ref, after `Config.Redact`.

## Errors

```go
type Error struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
	Ref     string            `json:"ref,omitempty"`
}
```

Every refusal is `{"error": {...}}` with this body. Branch on `Code`,
not on `Message`: each code maps to one status, and the prose may change.

| Code | Status | When |
|---|---|---|
| `unauthorized` | 401 | the token is missing or does not work |
| `forbidden` | 403 | the token may not do this |
| `not_found` | 404 | no such row, or not the caller's; no such path |
| `method_not_allowed` | 405 | a known path, wrong method |
| `conflict` | 409 | the write lost a race |
| `too_large` | 413 | the body is over the cap |
| `unsupported_media_type` | 415 | a write body without `Content-Type: application/json` |
| `invalid` | 422 | a field was refused, and `fields` says which; or the body is not one JSON object |
| `rate_limited` | 429 | a `Config.Before` limiter said no |
| `internal` | 500 | something else went wrong; `ref` is in the log |
| `unavailable` | 503 | the token store could not be asked |

`Fields` is keyed by wire name, such as `title`. `Ref` is set only on a
500. Quote it when you report the failure: the log line carries the same
one.

## Write, WriteError and Refuse

```go
func Write(w http.ResponseWriter, status int, v any)
func WriteError(w http.ResponseWriter, status int, e Error)
func Refuse(w http.ResponseWriter, r *http.Request, err error)
```

`Write` answers with `v` as JSON, with `Content-Type: application/json`
and `Cache-Control: no-store`. An empty list encodes as `[]`, never
`null`. `WriteError` puts `e` under `"error"` and writes it. Use it for a
code `Refuse` cannot derive from an error, such as `rate_limited` from a
rate limiter.

`Refuse` answers an error. An `op` refusal gets its code and status:
`op.Invalid` is 422 with its fields re-keyed by `FieldName`,
`op.NotFound` 404, `op.Forbidden` 403, `op.Conflict` 409 with
`Conflict.Message` as the message. `ErrUnauthorized` is 401 with
`WWW-Authenticate: Bearer`, and `ErrUnavailable` is 503. Any other error
is a 500: the error text goes to the log under a fresh ref, and the
response carries only the ref. `op.Forbidden.Reason` is logged and never
sent.

```go
var ErrUnauthorized = errors.New("rastrillo/api: unauthorized")
var ErrUnavailable = errors.New("rastrillo/api: credential store unavailable")
```

A `Verifier` wraps one of these when it cannot name an actor. Keep them
distinct: a client retries a 503 and gives up on a 401.

## Decode, NoBody and WithMaxBody

```go
func Decode(w http.ResponseWriter, r *http.Request, dst any) bool
func NoBody(w http.ResponseWriter, r *http.Request) bool
func WithMaxBody(r *http.Request, n int64) *http.Request
```

`Decode` reads the request body into `dst`, which must be a non-nil
pointer. If it returns false, it has already answered: return at once
and do not read `dst`. It checks, in this order:

1. `Content-Type` must be `application/json`, or the answer is 415. A
   `charset` parameter is fine.
2. The body must fit the cap, or the answer is 413. The whole body is
   read first, so a body that is too large is 413 even if it is also
   broken.
3. The body must be one JSON object, or the answer is 422 with no
   `fields`.
4. Each member must be one `dst` has, in the same case, sent once, of
   the right type, and not `null`. If not, the answer is 422, and
   `fields` names the member.

`null` is allowed only for an `op.Nullable` field, such as optional
money, where it clears the value. It is refused everywhere else,
including inside a `Nullable` value.

Call `NoBody` first in a handler that takes no input, such as a DELETE.
Any body at all is 422, whatever its `Content-Type`.

The cap is 1 MiB. The tree sets it from `Config.MaxBody` with
`WithMaxBody`. Call `WithMaxBody` yourself only outside the tree or in a
test. An `n` of 0 or less keeps 1 MiB.

## FieldName

```go
func FieldName(manifestName string) string
```

The wire name of a manifest field: `Title` is `title`, `MaxPerOrder` is
`max_per_order`, `IPAddress` is `ip_address`. Use it when you build an
`op.Invalid` by hand and want to know what the client will see.

## WithLogger and WithRedact

```go
func WithLogger(r *http.Request, l *slog.Logger) *http.Request
func WithRedact(r *http.Request, redact func(string) string) *http.Request
```

`Refuse` reads its logger and redactor from the request. The tree sets
both from `Config` on every request; set them yourself only in a test
that reads the log line. Without them, `Refuse` logs to `slog.Default()`
and redacts nothing.
