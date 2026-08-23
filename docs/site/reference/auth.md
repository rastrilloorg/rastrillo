# 🤖 auth

`github.com/carlosframework/rastrillo/auth`

The framework's turnkey sign-in and the family default: a magic-link
email that works for every address, auto-upgrading to the keymail
ceremony when the address resolves to a claimed keymail inbox.

It wraps `github.com/keymaildev/signin`, filling the holes that package
deliberately leaves — link storage, mailer, cookies, sessions, CSRF,
admission — once here instead of once per app.

[Magic links and keymail](/docs/magic-links) is the guide, and it
carries the one trap that costs you a working app.

## New and Config

```go
func New(cfg Config) (*Auth, error)
```

Build one `*Auth` at boot.

```go
type Config struct {
	DB           *sql.DB
	Origin       string
	InstanceKey  string
	Mailer       mail.Sender
	Authorize    func(address string) bool
	SecondFactor func(w http.ResponseWriter, r *http.Request, sess sessions.Session) (done bool, err error)
	SigninPath   string
	SignedInPath string
}
```

**`InstanceKey` must not be empty**, and `New` returns
`ErrEmptyInstanceKey` when it is. It seals the pending blob with an
HMAC, and an empty input hashes to one fixed, publicly computable value
— identical across every deployment that made the same mistake — which
would let an attacker forge a pending blob naming their own keymail
server.

`Origin` is the OAuth `client_id` keymail validates redirects against,
the base of emailed links, and what decides the cookie attributes.

`Mailer` is a [`mail.Sender`](/docs/reference/mail). Nil falls back to
`mail.Logged` with a warning on every send: an emailed link is a live
credential, so the fallback is development-only and says so.

`Authorize` is the admission gate — given a verified address, may it
have a session? Nil admits every verified address. Membership tables,
roles and admin bootstrap are app policy layered on this hook.

`SecondFactor` is the same seam `password` has.
`DefaultSessionTTL` is the TTL used when the config does not override
it.

## Schema

```go
var Schema = migrate.MustFromFS(migrationFS, "auth")
```

Merge it **after** `sessions.Schema` — auth's backfill migration reads
the sessions table, and `migrate.Merge`'s argument order is apply order.

## The four handlers

```text
POST /signin         -> Auth.Begin
GET  /auth/callback  -> Auth.Callback   (the keymail OAuth return)
GET  /auth/verify    -> Auth.Verify     (the magic-link landing)
POST /signout        -> Auth.Signout
```

The sign-in *page* stays the app's. These handlers report outcomes by
redirecting to `SigninPath` with a query the page renders: `?sent=1`,
`?err=rate|address|expired|keymail`, and `?force=1` to offer the
plain-email escape hatch after a failed keymail approval.

## Guarding and reading

```go
func (a *Auth) RequireSession(next http.Handler) http.Handler
func (a *Auth) RequireFreshSession(maxAge time.Duration) func(http.Handler) http.Handler
func From(r *http.Request) (Identity, bool)
func (a *Auth) SessionFrom(r *http.Request) (Identity, bool)
```

`RequireSession` stashes both the `Identity` and the underlying
`sessions.Session`, so `From` and `sessions.Current` both work
downstream.

**Do not use `sessions.UserID` under this plugin.** The subject is a
verified email address, so it returns `(0, false)` and the ordinary
scoping seam, which drops that `ok`, would scope every query in the app
to `user_id = 0`. Read the viewer with `From` and map the address to
your user row's id first.

`Identity` is an alias for `signin.Identity` — the same value the
upstream ceremony produces, not a copy that could drift from it.

## Odds and ends

`Auth.SessionCookie` reports the session cookie's name.
`Auth.Sweep` deletes expired links and sessions.
`NewToken` and `HashToken` re-export the
[sessions](/docs/reference/sessions) helpers, so a caller already
holding an `*Auth` need not import both.

## Links are single-use

A link is consumed in one `DELETE ... RETURNING`. A split
`SELECT`-then-`DELETE` would let two concurrent callers both observe the
row before either deleted it — even at one writer connection — defeating
single use.

An unknown hash, a wrong purpose and an expired row are all the same
"not ok": telling them apart would be an oracle. The row is deleted even
when expired, because a presented token is spent either way.
