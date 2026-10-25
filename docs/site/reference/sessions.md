# 🤖 sessions

`amadan.net/rastrillo/rastrillo/sessions`

The signed-in-state core: session rows in SQLite or signed cookies, `__Host-` cookies on https origins, and the request-context surface every identity plugin signs into.

It does not know how a session is earned. A plugin verifies a credential
and calls `SignIn`, and that call is the whole plugin contract.

[Sessions](/docs/sessions) is the guide.

## New and Config

```go
func New(cfg Config) (*Sessions, error)
```

Build one `*Sessions` per process and share it.

```go
type Config struct {
	DB         *sql.DB
	Origin     string
	TTL        time.Duration
	SigninPath string
	Logger     *slog.Logger
	Signed     *Signed
	Now        func() time.Time
}
```

`DB` is your database, and `Schema`'s migrations must already have been applied. Leave `DB` nil in signed mode. `Origin` is your external origin with scheme. It decides the `Secure` and `__Host-` cookie attributes. With session rows that is all it does, and `sessions` never redirects through it. In signed mode, `sessions` also redirects to `Origin` plus a path. `TTL` defaults to 30 days, `SigninPath` to `/signin`.

`Signed` turns on signed mode, described below. Leave it nil for session rows. `Now` is the clock and defaults to `time.Now`. Set it when another part of your app has its own clock, so both move together.

## Schema

```go
var Schema = migrate.MustFromFS(migrationFS, "sessions")
```

The package's migration set, for session rows. Merge it into your boot set ([migrate](/docs/reference/migrate)) before anything here runs. Signed mode keeps no table and needs no migrations. Tokens never touch the table: only their SHA-256 hash is stored.

## Session

```go
type Session struct {
	Subject  string
	Method   string
	AuthTime time.Time
	At       time.Time
}
```

`Subject` is your own identifier for who is signed in, kept as a string
so plugins never have to agree on a numeric type. `Method` names how the
credential was verified, in the plugin's vocabulary — `"password"`,
`"magiclink"`, `"magiclink+passkey"`. `AuthTime` is when the credential
was verified, zero if the plugin does not track it. `At` is when this
row was created.

## Signing in and out

```go
func (s *Sessions) SignIn(w http.ResponseWriter, r *http.Request, sess Session) error
func (s *Sessions) SignOut(w http.ResponseWriter, r *http.Request)
```

`SignIn` mints a row and sets the cookie, rotating the token, which is
also what makes it satisfy `RequireFresh`. `SignOut` deletes the row, so
revocation is real: the cookie is dead even if it survives.

`Sessions.CookieName` reports the cookie's name, which varies with the
origin's scheme because of the `__Host-` prefix.

```go
func (s *Sessions) Mint(sess Session) (token string, err error)
func (s *Sessions) Adopt(w http.ResponseWriter, r *http.Request, token string) (Session, bool)
```

`Mint` and `Adopt` split `SignIn` for a credential that lives somewhere
other than this browser — the [vault](/docs/reference/vault)'s copy of
an instance session. `Mint` creates a row and returns its token without
touching any cookie: same TTL, same table, same `Sweep`. `Adopt` is the
return leg: verify the presented token and, if its row is alive, set
the cookie to it — minting nothing, revoking nothing. A dead token
refuses without touching any cookie, so a failed restore falls through
to ordinary sign-in.

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

`Sessions.FromContext(ctx, r)` uses the same cookie and session validation as `From`. Its context bounds the database lookup, including waiting for a connection. Cancellation, lookup failure, or a missing, expired or revoked session returns `ok=false`. Canceling a lookup leaves the stored session unchanged.

## Fresh

```go
func Fresh(sess Session, maxAge time.Duration, now time.Time) bool
```

The predicate `RequireFresh` applies, for a handler stepping up in code
rather than per route.

Freshness is measured from `AuthTime` when set and from `At` otherwise.
A session is only ever minted at credential verification, so `At` is an
honest lower bound, and the fallback is what stops a plugin that never
sets `AuthTime` — the magic link, for one — from redirect-looping
forever.

## Reading the viewer

```go
func Current(r *http.Request) (Session, bool)
func UserID(r *http.Request) (int64, bool)
func WithSession(r *http.Request, sess Session) *http.Request
func ActorFrom(r *http.Request) (rastrillo.Actor, bool)
```

`Current` gives you the whole session. `UserID` parses `Subject` as an
`int64`, and its `ok` holds only for a plugin whose subject is a numeric
user id — not for the magic-link plugin, whose subject is an email
address. [Magic links](/docs/magic-links) has what follows from
assuming otherwise.

`WithSession` stashes a session on a request's context. Plugins use it
after verifying, and it is also what makes a session visible to code
called outside the middleware, which is how a generated store sees the
viewer.

`ActorFrom` is the signed-in person as a `rastrillo.Actor`, with `Write`
access. The generated router uses it when your ctx factory leaves `Actor`
empty. `ok` is false with no session, or with a session that has no
`Subject`.

## SafeReturn

```go
func SafeReturn(r *http.Request, fallback string) string
func SafeReturnTo(to, fallback string) string
```

Returns `return_to` when it is a same-site absolute path — exactly one
leading `/`, no scheme, no backslash, no control characters — and the
fallback otherwise. Anything laxer is an open redirect on a sign-in
endpoint.

Control characters are in that list because browsers strip tab, CR and
LF from a URL before parsing it, so `/\t/evil.example` passes a bare
`//` check and still navigates scheme-relative off-site.

`SafeReturnTo` is the same check on a string you already have, such as a destination you kept in a cookie.

## Tokens

```go
func NewToken() (token, hash string, err error)
func HashToken(token string) string
```

Mint a token and its stored hash, or hash a presented one. Exported
because a plugin storing its own single-use credentials — a magic link,
a recovery code — should hash them the same way instead of inventing a
second scheme.

## The inventory

```go
func (s *Sessions) Hash(r *http.Request) (string, bool)
func (s *Sessions) List(subject string) ([]Info, error)
func (s *Sessions) Revoke(subject, hash string) error
func (s *Sessions) RevokeOthers(r *http.Request, subject string) (int, error)
func (s *Sessions) RevokeAll(subject string) (int, error)
```

What an app needs to show a person their own live sessions and let them
end any of them. `List` is every live row a subject holds, newest first,
as `Info`: the row's `Hash` (an opaque handle that names the row and
opens nothing, so it is safe to carry in a form), the `Session`, and
`ExpiresAt`. `Hash` is the storage hash of the token a request presents
— how a handler finds its own row in the list. `Revoke` ends one row by
its hash, and takes the subject with it so a handle from one person's
list is never redeemed by another; an unknown row and someone else's row
alike answer `ErrNotYours`. `RevokeOthers` ends every row but the one
the request presents — what a credential change does, so a stolen
session does not outlive the password it was stolen alongside — and
`RevokeAll` ends every row, the presented one included: sign out
everywhere. Both return how many they ended. What a session looks like
from the outside — the browser, the address, when it was last seen — is
the app's to remember, keyed by the hash.

## Sweep

```go
func (s *Sessions) Sweep(now time.Time) error
```

Deletes expired rows. Expired sessions are already refused on read, so
this is hygiene rather than enforcement.

## Signing keys

```go
const SignedLifetime = 15 * time.Minute

type SigningKey struct{ /* unexported */ }

func NewSigningKey(kid string) (*SigningKey, error)
func ParseSigningKey(s string) (*SigningKey, error)
func (k *SigningKey) KeyID() string
func (k *SigningKey) Secret() string
func (k *SigningKey) Public() string
func ParsePublicKeys(s string) (map[string][]byte, error)
```

A hub signs session cookies with a P-256 `SigningKey`. Its apps verify them with the public half. A signed cookie lasts `SignedLifetime`.

`NewSigningKey` makes a new key. The key id `kid` is 1 to 32 characters of `a-z`, `0-9`, `_` and `-`.

`Secret` returns `<kid>.<base64url of the 32-byte private scalar>`. `ParseSigningKey` reads it back. Keep it secret: only the hub holds it.

`Public` returns `<kid>.<base64url of the 65-byte uncompressed point>`. `ParsePublicKeys` reads one or two of these, separated by a comma. Use two while you rotate keys. Each needs its own key id.

`rastrillo session-key <kid>` prints a new key in both forms. See [the CLI](/docs/cli#rastrillo-session-key).

## Signed mode

```go
type Signed struct {
	Name        string
	Key         *SigningKey
	Keys        map[string][]byte
	Epoch       func(ctx context.Context, subject string) (int64, error)
	RefreshPath string
}

const DefaultRefreshPath = "/_carlos/refresh"

var ErrSigned = errors.New("rastrillo/sessions: signed sessions keep no rows")
var ErrVerifyOnly = errors.New("rastrillo/sessions: this app verifies sessions and cannot mint them")
var ErrNoUser = errors.New("rastrillo/sessions: no such subject")

func (s *Sessions) RefreshIfExpired(w http.ResponseWriter, r *http.Request) bool
func (s *Sessions) Refresh(w http.ResponseWriter, r *http.Request)
```

In signed mode there are no session rows. The session is a cookie signed with P-256 that lasts 15 minutes, and the hub reissues it at `RefreshPath`. A hub and the apps under it on the same origin share the one cookie. The hub sets `Key` and signs. Each app under it sets only `Keys` and verifies.

Rules `New` checks:

- `DB` is nil.
- `Origin` is a scheme and host only, with no path or trailing `/`. Signed mode redirects to `Origin` plus a path.
- `Name` is 1 to 32 of `a-z`, `0-9`, `_` and `-`, starting with a letter or digit. The cookie is `__Host-` plus `Name` on https, and `Name` on http.
- `Keys` and `Key` together hold one or two key ids. Use two while you rotate keys. If `Keys` holds `Key`'s key id, it must hold `Key`'s public key. `ParsePublicKeys` builds `Keys`.
- `Key` needs `Epoch`.
- `RefreshPath` is a clean path that starts with `/`: no `?`, `#` or `%`, no `.`, `..` or empty segment, and no trailing `/`. It defaults to `DefaultRefreshPath`.

`New` keeps its own copy of `Signed`, so changing your map afterwards changes nothing.

`Epoch` returns the subject's current epoch, or `ErrNoUser` when the subject no longer exists. Each cookie carries the epoch it was signed with. Change the epoch to end every session the subject holds at its next refresh.

A cookie expires 15 minutes after it was signed, or at the end of the session's `TTL` if that comes first.

| Call | Hub (`Key` set) | App (`Keys` only) |
|---|---|---|
| `SignIn` | reads `Epoch`, then sets a new cookie with `Max-Age` = `TTL` | `ErrVerifyOnly` |
| `SignOut` | clears this browser's cookie. A copy of it can still be refreshed until the epoch changes or the session ends | same |
| `From`, `FromContext` | valid cookies only | same |
| `Middleware` | valid: stash it. Expired: `RefreshIfExpired`. Otherwise pass through. Never clears the cookie | same |
| `Require`, `RequireFresh` | as `Middleware`, then send a page request to `Origin + SigninPath + "?return_to="` and the full request path | same |
| `Mint`, `List`, `Revoke`, `RevokeOthers`, `RevokeAll` | `ErrSigned` | `ErrSigned` |
| `Adopt` | `(Session{}, false)` | same |
| `Hash` | `("", false)` | same |
| `Sweep` | `nil` | `nil` |

`SignIn` refuses a `Method` that is not 1 to 32 of `a-z`, `0-9`, `_` and `-`. A second factor's method, such as `magiclink+totp`, is refused, so signed mode has no second factors.

A cookie that does not verify is treated as signed out and left in place. It may be signed with a new key this app does not have yet, and clearing it would sign the person out of every app on the origin.

`RefreshIfExpired` sends a request with an expired cookie to `Origin + RefreshPath + "?next="` and the full request path, and reports whether it did. `GET` and `HEAD` get 303. Other methods get 307, so the browser sends a form again with its body. A request to `RefreshPath` itself is never sent. In row mode it does nothing and returns false.

`Refresh` is the hub's refresh route. Mount it at `RefreshPath` for every method. It never reads the body. "Go back" below means a redirect to `next`: 303 for `GET` and `HEAD`, 307 for anything else, so the browser sends a form again with its body. "Sign out" means clear the cookie and 303 to `SigninPath + "?return_to="` and `next`. It checks, in this order:

1. In row mode or on an app with `Keys` only: 404.
2. A method other than `GET` and `HEAD` must pass `csrf.SameOrigin`, or it gets 403 and no cookie changes.
3. `next` must pass `SafeReturnTo` and must not be `RefreshPath`. Otherwise it is `/`.
4. No cookie: 303 to sign in, and no cookie is set.
5. A cookie that does not verify: sign out.
6. A cookie that has not expired: go back. Another tab refreshed first. Each of these visits is counted in a cookie named the session cookie plus `_rf`. The tenth within 10 seconds answers 500 and logs an error, because the hub and an app disagree on the time.
7. The session's `TTL` has passed: sign out.
8. `Epoch` returns `ErrNoUser` or a different epoch: sign out. Any other error answers 503 and keeps the cookie.
9. Otherwise it signs a new cookie under `Key` for the same session, with `Max-Age` set to what is left of the `TTL`, and goes back.

A cookie signed with a key in `Keys` comes back signed with `Key`. That is how a key rotation moves people onto the new key.

## Under a base path

```go
var ErrPathRouted = errors.New("rastrillo/sessions: no row-backed sessions under a base path; sign in at the hub")
```

Under a [`basepath`](/docs/reference/basepath), and on the app's direct host, the row-backed store stands aside. Every app on the origin shares one `__Host-` cookie, so signing in to one would sign the reader out of the others. There `SignIn` returns `ErrPathRouted` and `Adopt` returns `(Session{}, false)`, each logging an error and setting no cookie. `From`, `Middleware` and `Require` read no session and never clear the cookie, which may be the hub's. `SignOut` does nothing. `Hash` returns `("", false)` and `RevokeOthers` returns `ErrPathRouted`, revoking nothing, because there is no session of this store's own to keep. Sessions come from the hub: use signed mode.

In signed mode an app verifies the hub's cookie under its base path as usual, and reads no cookie on the direct host.

On the direct host, `Require` and `RequireFresh` send a signed-out `GET` or `HEAD` to the same page under the product path, `basepath.ProductURL`, in both modes. The platform's own calls get 403 instead.

## Rotating the signing key

Rotate the signing key in this order. If the hub signs with a key id a tenant does not have, everyone is signed out of that tenant.

1. Run `rastrillo session-key k2`. Set both public keys on every tenant with `carlos env set`.
2. On the hub, make `k2` the signing key, and keep `k1`'s public key in `Keys`.
3. Remove `k1` everywhere after the 30-day session life. After a compromise, remove it at once: every cookie signed with it stops working within 15 minutes, and the people holding one sign in again.
