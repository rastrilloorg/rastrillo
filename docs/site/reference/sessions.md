# 🤖 sessions

`github.com/carlosframework/rastrillo/sessions`

The signed-in-state core: SQLite-backed session rows, `__Host-` cookies
on https origins, and the request-context surface every identity plugin
signs into.

It deliberately does not know how a session is *earned*. A plugin
verifies a credential and calls `SignIn`; that one call is the whole
plugin contract.

[Sessions](/docs/sessions) is the guide.

## New and Config

```go
func New(cfg Config) (*Sessions, error)
```

Build exactly one `*Sessions` per process and share it.

```go
type Config struct {
	DB         *sql.DB
	Origin     string
	TTL        time.Duration
	SigninPath string
	Logger     *slog.Logger
}
```

`DB` is the app's database, and `Schema`'s migrations must already have
been applied. `Origin` is the external origin with scheme, and it
decides the `Secure` and `__Host-` cookie attributes and nothing else —
`sessions` never redirects off it. `TTL` defaults to 30 days,
`SigninPath` to `/signin`.

## Schema

```go
var Schema = migrate.MustFromFS(migrationFS, "sessions")
```

The package's migration set. Merge it into your boot set —
[migrate](/docs/reference/migrate) — before anything here runs. Tokens
never touch the table: only their SHA-256 hash is stored.

## Session

```go
type Session struct {
	Subject  string
	Method   string
	AuthTime time.Time
	At       time.Time
}
```

`Subject` is the app's own identifier for who is signed in, kept as a
string so plugins never have to agree on a numeric type. `Method` names
how the credential was verified, in the plugin's vocabulary —
`"password"`, `"keymail"`, `"magiclink+passkey"`. `AuthTime` is when the
credential was verified, zero if the plugin does not track it. `At` is
when this row was created.

## Signing in and out

```go
func (s *Sessions) SignIn(w http.ResponseWriter, r *http.Request, sess Session) error
func (s *Sessions) SignOut(w http.ResponseWriter, r *http.Request)
```

`SignIn` mints a row and sets the cookie, rotating the token — which is
also what makes it satisfy `RequireFresh`. `SignOut` deletes the row, so
revocation is real: the cookie is dead even if it survives.

`Sessions.CookieName` reports the cookie's name, which varies with the
origin's scheme because of the `__Host-` prefix.

## Middleware

```go
func (s *Sessions) Require(next http.Handler) http.Handler
func (s *Sessions) Middleware(next http.Handler) http.Handler
func (s *Sessions) RequireFresh(maxAge time.Duration) func(http.Handler) http.Handler
```

`Require` admits only a signed-in caller: a signed-out `GET`/`HEAD`
redirects to `SigninPath` with a same-site `return_to`, anything else
403s. `Middleware` resolves a session when there is one and blocks
nothing. `RequireFresh` is `Require` plus step-up — past `maxAge` a
`GET`/`HEAD` goes to `SigninPath?reauth=1`, anything else 403s.

`Sessions.From` resolves the session from a request directly, for code
outside the middleware chain.

## Fresh

```go
func Fresh(sess Session, maxAge time.Duration, now time.Time) bool
```

The predicate `RequireFresh` applies, for a handler stepping up
conditionally rather than per route.

Freshness is measured from `AuthTime` when set and from `At` otherwise.
A session is only ever minted at credential verification, so `At` is an
honest lower bound — and the fallback is what stops a plugin that never
sets `AuthTime`, like the magic link, from redirect-looping forever.

## Reading the viewer

```go
func Current(r *http.Request) (Session, bool)
func UserID(r *http.Request) (int64, bool)
func WithSession(r *http.Request, sess Session) *http.Request
```

`Current` returns the whole session. `UserID` parses `Subject` as an
`int64`, and its `ok` holds **only** for a plugin whose subject is a
numeric user id — not for keymail, whose subject is an email address.
[Magic links](/docs/magic-links) has the mistake that follows from
assuming otherwise.

`WithSession` stashes a session on a request's context. Plugins use it
after verifying; it is also what makes a session visible to code called
outside the middleware, which is how a generated store sees the viewer.

## SafeReturn

```go
func SafeReturn(r *http.Request, fallback string) string
```

Returns `return_to` when it is a same-site absolute path — exactly one
leading `/`, no scheme, no backslash, no control characters — and the
fallback otherwise. Anything laxer is an open redirect on a sign-in
endpoint.

Control characters matter because browsers strip tab, CR and LF from a
URL before parsing it: `/\t/evil.example` passes a bare `//` check and
still navigates scheme-relative off-site.

## Tokens

```go
func NewToken() (token, hash string, err error)
func HashToken(token string) string
```

Mint a token and its stored hash, or hash a presented one. Exported
because plugins storing their own single-use credentials — a magic link,
a recovery code — should hash them the same way rather than inventing a
second scheme.

## Sweep

```go
func (s *Sessions) Sweep(now time.Time) error
```

Deletes expired rows. Expired sessions are already refused on read, so
this is hygiene rather than enforcement.
