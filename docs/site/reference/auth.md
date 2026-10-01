# 🤖 auth

`amadan.net/rastrillo/rastrillo/auth`

Passwordless sign-in by emailed link. It wraps
`github.com/keymaildev/signin`, filling the holes that package
deliberately leaves — link storage, mailer, cookies, sessions, CSRF,
admission — once here instead of once per app.

[Magic links](/docs/magic-links) is the guide, and it carries the one
trap that costs you a working app.

Addresses with a claimed keymail inbox upgrade to keymail's OAuth
ceremony automatically. That is an aside rather than a feature you wire:
same handlers, same session, same `Subject` either way. The guide's
[keymail section](/docs/magic-links#aside-the-keymail-upgrade) has the
details.

## New and Config

```go
func New(cfg Config) (*Auth, error)
```

Build one at boot.

```go
type Config struct {
	DB               *sql.DB
	Origin           string
	InstanceKey      string
	Mailer           mail.Sender
	Authorize        func(address string) bool
	SecondFactor     func(w http.ResponseWriter, r *http.Request, sess sessions.Session) (done bool, err error)
	SigninPath       string
	SignedInPath     string
	TrustedProxyHops *int
	SigninScreen     bool
	BeginPath        string
	ForgetPath       string
	Remember         *bool
	KeymailServers   []string
}
```

`SigninScreen` turns on the shipped sign-in screen's side of `auth`: the attempt and continuation cookies, the keymail continuation, and remembering the way in. Off, nothing about `Begin` or `Callback` changes. `BeginPath` and `ForgetPath` (default `/signin` and `/signin/forget`) are where you mounted `Begin` and `Forget`, for the screen's forms. `Remember` set to `false` keeps the screen and stops remembering.

`InstanceKey` must not be empty, and `New` returns
`ErrEmptyInstanceKey` when it is. It seals the pending blob with an
HMAC, and an empty input hashes to one fixed, publicly computable value
— identical across every deployment that made the same mistake — which
would let an attacker forge a pending blob naming their own keymail
server.

`Origin` is the base of emailed links and what decides the cookie
attributes. It doubles as the OAuth `client_id` keymail validates
redirects against.

`Mailer` is a [`mail.Sender`](/docs/reference/mail). Leave it nil and
you get `mail.Logged` with a warning on every send: an emailed link is a
live credential, so the fallback is development-only and says so.

`Subject` and `Body` write the sign-in email. `Body` takes the link and
returns the whole text, so the email can name your app. Leave it nil and
you get `DefaultBody`, which says how long the link works. `LinkTTL` is
that lifetime, 15 minutes. If your body says the number, take it from
`LinkTTL`.

`Authorize` is the admission gate: given a verified address, may it have
a session? Nil admits every verified address. Membership tables, roles
and admin bootstrap are your policy layered on this hook.

`SecondFactor` is the same seam `password` has.
`DefaultSessionTTL` is the TTL used when the config does not override
it.

`TrustedProxyHops` is how many proxies you run in front of the app, and
decides which address the per-IP sign-in limit counts. Leave it unset
and it is 1 on CARLOS, whose edge adds the visitor's address, and 0
anywhere else, where the limit counts the connection's address. A value
you set always wins, 0 included. See
[clientip](/docs/reference/clientip).

`KeymailServers` limits keymail to the servers you list, both when an address is checked and when a sign-in finishes; an unlisted server's addresses get a link. See [Magic links](/docs/magic-links#aside-the-keymail-upgrade).

## Schema

```go
var Schema = migrate.MustFromFS(migrationFS, "auth")
```

Merge it **after** `sessions.Schema` — auth's backfill migration reads
the sessions table, and `migrate.Merge`'s argument order is apply order.

## The handlers

```text
POST /signin         -> Auth.Begin
GET  /auth/verify    -> Auth.Verify     (the emailed link's landing)
GET  /auth/callback  -> Auth.Callback   (the keymail OAuth return)
POST /signout        -> Auth.Signout
POST /signin/forget  -> Auth.Forget
```

These handlers report outcomes by redirecting to `SigninPath`: `?sent=1`, `?err=rate|address|expired|1`, `?err=keymail` with `?force=1` after a failed keymail approval, and, with `SigninScreen` on, `?sent=1&attempt=<id>` and `?continue=<id>`. The shipped screen reads them through `SigninState`; a page of your own renders them itself.

## The sign-in screen

```go
func (a *Auth) SigninState(r *http.Request) SigninState
func (a *Auth) PrepareSigninResponse(w http.ResponseWriter, st SigninState)
func (a *Auth) Forget(w http.ResponseWriter, r *http.Request)
func (a *Auth) AnswerAsSent(w http.ResponseWriter, r *http.Request)
func (a *Auth) RememberJar() *lastsignin.Jar
```

`SigninState` reads the query and this browser's own cookies and returns what the page shows, as plain data. It consults nothing else, so the page cannot reveal whether an address is known. Set `Passkey` on the result if you mounted passkey discovery. `PrepareSigninResponse` writes what goes with it: `Cache-Control: no-store`, `Referrer-Policy: no-referrer` on the page that moves on to Keymail, and deletions for any cookie that could not be trusted. Call it before rendering. With `SigninScreen` off, `SigninState` reads only the query and logs one warning per process.

```go
type SigninState struct {
	Step        SigninStep
	Problem     SigninProblem
	Address     string
	SentTo      string
	SentInstead bool
	Remembered  *Remembered
	ContinueURL string
	BeginPath   string
	ForgetPath  string
	Passkey     *PasskeyDoor
}
func (s SigninState) Door() string
func (s SigninState) Focus() string

type SigninStep string

const (
	StepAsk       SigninStep = "ask"
	StepReturning SigninStep = "returning"
	StepSent      SigninStep = "sent"
	StepContinue  SigninStep = "continue"
)

type SigninProblem string

const (
	ProblemNone    SigninProblem = ""
	ProblemRate    SigninProblem = "rate"
	ProblemAddress SigninProblem = "address"
	ProblemExpired SigninProblem = "expired"
	ProblemKeymail SigninProblem = "keymail"
	ProblemGeneric SigninProblem = "generic"
	ProblemReauth  SigninProblem = "reauth"
)

type Remembered struct{ Method, Address string }

type PasskeyDoor struct {
	BeginPath, FinishPath string
	ModuleURL, ScriptURL  string
	LegacyRPID            string
}
```

`Forget` is the Use a different email button: POST only, same-origin only, it forgets the remembered way in and redirects to `SigninPath`. `AnswerAsSent` is `Begin`'s answer for a sent link, without the link, for an admission check in front of `Begin`; see [Magic links](/docs/magic-links#an-admission-check-in-front-of-begin). `RememberJar` is the jar that remembers the way in; give it to `passkey.Config.Remember`.

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

Do not use `sessions.UserID` under this plugin. The subject is a
verified email address — on both the emailed-link and keymail paths —
so it returns `(0, false)`, and the ordinary scoping seam drops that
`ok` and scopes every query in your app to `user_id = 0`. Read the
viewer with `From` and map the address to your user row's id first.

`Identity` is an alias for `signin.Identity`, the same value the
upstream ceremony produces, so it cannot drift from it.

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

An unknown hash, a wrong purpose and an expired row all come back as the
same "not ok"; telling them apart would be an oracle. The row is deleted
even when expired, because a presented token is spent either way.
## SpendLinks

```go
func (a *Auth) SpendLinks(ctx context.Context, address string) (int, error)
```

Deletes every outstanding sign-in link for an address and returns how
many it spent. A link is a credential waiting to be used; once the
person is in by any door — this one, a password, a passkey on a
remembered browser — the ones still in their inbox are a credential
nobody needs, and an app calls this at every sign-in so a stolen inbox
cannot cash one later.

