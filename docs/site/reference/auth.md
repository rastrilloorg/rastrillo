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
	Proof            *pow.Guard
	ProofOff         bool
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
	RenderConfirm    func(w http.ResponseWriter, r *http.Request, d ConfirmPageData)
	KeymailServers   []string
	Records          objrec.Store
	Signed           *sessions.Signed
	Pepper           []byte
	Blind            bool
}
```

```go
var ErrProofUnset, ErrProofMode error // from New: neither Proof nor ProofOff; a bound Guard
const ProofScope = "rastrillo/auth/begin"
```

`Proof` is sign-in's front door, required unless `ProofOff`; [Magic links](/docs/magic-links#the-front-door) and [pow](/docs/reference/pow) cover it.

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

`Records`, `Signed`, `Pepper` and `Blind` are records mode, for a centre with no database. See [Records mode](#records-mode).

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
POST /auth/verify    -> Auth.Verify     (confirming that landing)
GET  /auth/callback  -> Auth.Callback   (the keymail OAuth return)
POST /signout        -> Auth.Signout
POST /signin/forget  -> Auth.Forget
```

In records mode, also mount these two:

```text
POST /signout-everywhere -> Auth.SignoutEverywhere
*    /_carlos/refresh    -> Auth.Refresh   (every method, at Signed.RefreshPath)
```

`Verify` needs both methods mounted. The GET draws a page with a Sign in button and spends nothing; the POST spends the link, runs `Authorize`, and answers a bad or used link with `?err=expired`. Anything else gets a 405. See [Magic links](/docs/magic-links#link-scanners-eat-magic-links).

These handlers report outcomes by redirecting to `SigninPath`: `?sent=1`, `?err=rate|address|expired|1`, `?err=keymail` with `?force=1` after a failed keymail approval, and, with `SigninScreen` on, `?sent=1&attempt=<id>` and `?continue=<id>`. The shipped screen reads them through `SigninState`; a page of your own renders them itself. A problem keeps the sign-in's `return_to` on that query, so the corrected form posts it again.

## The sign-in screen

```go
func (a *Auth) SigninState(r *http.Request) SigninState
func (a *Auth) PrepareSigninResponse(w http.ResponseWriter, st SigninState)
func (a *Auth) Forget(w http.ResponseWriter, r *http.Request)
func (a *Auth) AnswerAsSent(w http.ResponseWriter, r *http.Request)
func (a *Auth) RememberJar() *lastsignin.Jar
```

`SigninState` reads the query and this browser's own cookies and returns what the page shows, as plain data. It consults nothing else, so the page cannot reveal whether an address is known. Set `Passkey` on the result if you mounted passkey discovery. `PrepareSigninResponse` writes what goes with it: `Cache-Control: no-store`, `Referrer-Policy: no-referrer` on the page that moves on to Keymail, and deletions for any cookie that could not be trusted. Call it before rendering. With `SigninScreen` off, `SigninState` reads only the query and logs one warning per process. On the request `RenderConfirm` is given, `SigninState` answers `StepConfirm` with `Confirm` set and nothing else from the browser, and the partial draws the Sign in button.

`ReturnTo` is the page to land on after sign-in, taken from the query's `return_to` when it is on this site. The shipped sign-in screen posts it with every form. On a page of your own, put it in a hidden `return_to` field in each form, and use `PasskeyFinish()` as the passkey button's finish URL.

```go
type SigninState struct {
	Step        SigninStep
	Problem     SigninProblem
	Address     string
	SentTo      string
	SentInstead bool
	Remembered  *Remembered
	ContinueURL string
	Confirm     *ConfirmPageData
	BeginPath   string
	ForgetPath  string
	Force       bool
	ReturnTo    string
	Proof       *pow.Form
	Passkey     *PasskeyDoor
	Unavailable bool
}
func (s SigninState) Door() string
func (s SigninState) Focus() string
func (s SigninState) PasskeyFinish() string

type SigninStep string

const (
	StepAsk       SigninStep = "ask"
	StepReturning SigninStep = "returning"
	StepSent      SigninStep = "sent"
	StepContinue  SigninStep = "continue"
	StepConfirm   SigninStep = "confirm"
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
	ProblemCheck   SigninProblem = "check"
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

A signed-out page request goes to `SigninPath?return_to=<the page>`, plus `reauth=1` when `RequireFreshSession` wants a fresh sign-in. After signing in, the visitor lands back on that page. Anything else gets a 403.

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

## Under a base path

Under a [`basepath`](/docs/reference/basepath), and on the app's direct host, a tenant has no sign-in of its own: the hub signs people in. Every handler above answers 404 there, and logs an error, before it reads a cookie, writes a row or sends a link. `SendLink` takes no request, so it is not refused. `SigninState` returns a state with `Unavailable` set and nothing else, and the `signin` partial renders nothing for it; a page of your own should draw no form. On the direct host, `RequireSession` and `RequireFreshSession` send a signed-out `GET` or `HEAD` to the same page under the product path, `basepath.ProductURL`, and answer the platform's own calls 403. Under the base path they still send it to `SigninPath`, which answers 404 there, as does the sign-in page a row-store tenant's `sessions.Require` sends readers to: such a tenant has no sign-in under a base path, by design.

## Odds and ends

`Auth.SessionCookie` reports the session cookie's name.
`Auth.Sweep` deletes expired links and sessions.
`NewToken` and `HashToken` re-export the
[sessions](/docs/reference/sessions) helpers, so a caller already
holding an `*Auth` need not import both.

## The confirm page

```go
type ConfirmPageData struct {
	Token  string // carry it in a hidden field named "token"
	Action string // where the form posts: the request's own path
	Host   string // Config.Origin without its scheme, for the copy
}
```

`Config.RenderConfirm` draws the page in your layout. With
`SigninScreen` on, set it to your sign-in page's handler: `SigninState`
answers `StepConfirm` on the request it gets. Nil gets a self-contained
English page, so upgrading needs no code change beyond the POST route.

Before calling you, `Verify` sets `Cache-Control: no-store`,
`Referrer-Policy: strict-origin`, `X-Robots-Tag: noindex, nofollow`,
`X-Frame-Options: DENY` and adds `Content-Security-Policy:
frame-ancestors 'none'` beside the app's own policy. `strict-origin`
keeps the token in the page's address out of the `Referer` of its
asset requests, and still sends a real `Origin` on the button's POST,
where `no-referrer` would send `Origin: null` and fail the same-origin
check. The framing headers are login-CSRF defence: a framed confirm
button carrying an attacker's token signs your user into the attacker's
account.

## Links are single-use

A link is consumed in one `DELETE ... RETURNING`. A split
`SELECT`-then-`DELETE` would let two concurrent callers both observe the
row before either deleted it — even at one writer connection — defeating
single use.

An unknown hash, a wrong purpose and an expired row all come back as the
same "not ok"; telling them apart would be an oracle. The row is deleted
even when expired, because a presented token is spent either way.

## SendLink

```go
func (a *Auth) SendLink(ctx context.Context, address, ipKey string) error
var ErrRateLimited, ErrBadAddress error // signin's, re-exported
```

Mails the same link `Begin` would, from code: for a caller the app
already trusts to have decided the address should get one, and that
cannot post the form (a server's POST carries no same-origin evidence
and no proof). The same single-use row, the same letter through
`Mailer`, the same budgets: `ipKey` stands where the client address
would in the per-IP budget, so pass a fixed name for the channel
(`"relay"`), never an address the caller was told, and that channel
shares one budget of 20 per window. The address budget is the form's,
shared with it. No proof is checked, no keymail classification runs,
no attempt cookie is written. A refusal is `ErrRateLimited` or
`ErrBadAddress`, for a log line and not an answer: a caller that must
not be an oracle answers the same either way. See
[Magic links](/docs/magic-links#sending-a-link-from-code).

## Records mode

Set `Records` to keep users and sign-in links in an [objrec](/docs/reference/objrec) store instead of a database. The session is then a signed cookie this app mints and its tenants verify. See [sessions](/docs/reference/sessions).

```go
a, err := auth.New(auth.Config{
	Origin:      "https://ppl.example.com",
	InstanceKey: instanceKey,
	Proof:       guard,
	Mailer:      mailer,
	Records:     store,
	Pepper:      pepper, // 32 bytes or more
	Signed:      &sessions.Signed{Name: "ppl", Key: key},
})
```

`New` refuses a records-mode config when:

- `DB` is set.
- `Signed` is nil or has no `Key`. The centre mints the cookie.
- `Signed.Epoch` is set. `auth` reads the epoch from `users/`.
- `Pepper` is shorter than 32 bytes.
- `SubjectFor` is set. The subject is the user id.
- `Blind` is set and `Mailer` is nil or `mail.Logged`. Both write the recipient and the live link to the log. For local development, pass a `Sender` of your own.

It also refuses `Pepper`, `Blind` or `Signed` without `Records`. `New` copies `Signed` and never writes yours.

Records mode has no second factors. `New` accepts `SecondFactor`, but a sign-in that finishes a second factor fails. Leave `SecondFactor` nil.

The records:

| Key | Body |
|---|---|
| `users/<uid>.json` | `User`. `email` is absent in blind mode |
| `emails/<eh>.json` | `{"uid": "<uid>"}` |
| `links/<yyyy-mm-dd>/<hash>.json` | `{"eh": "<eh>", "addr": "<the address, sealed>", "purpose": "signin", "exp": <unix>}` |
| `links-used/<yyyy-mm-dd>/<hash>` | `{}` |

`<uid>` is 128 random bits in lowercase base32. `<eh>` is the lowercase hex HMAC-SHA256 of the lowercased address under `Pepper`.

```go
type User struct {
	ID      string    `json:"uid"`
	Email   string    `json:"email,omitempty"`
	EH      string    `json:"eh"`
	Created time.Time `json:"created"`
	Epoch   int64     `json:"epoch"`
}

func (a *Auth) User(ctx context.Context, uid string) (User, error)
func (a *Auth) EndSessions(ctx context.Context, uid string) error
func (a *Auth) SignoutEverywhere(w http.ResponseWriter, r *http.Request)
func (a *Auth) Refresh(w http.ResponseWriter, r *http.Request)

var ErrRecords error
```

The first sign-in creates the user. After that, `Identity.Address` (from `From` and `SessionFrom`) is the user id, not the address. Read the address with `User`, whose `Email` is empty in blind mode. `User` returns an error wrapping `objrec.ErrNotFound` for an unknown id.

`EndSessions` ends every session of a user: it adds one to the user's epoch, and the refresh route signs out any cookie that carries the old one. It returns `sessions.ErrNoUser` for a user with no record, and writes nothing.

`SignoutEverywhere` is POST only and same-origin only. It calls `EndSessions` for the signed-in user, clears the cookie and redirects to `SigninPath`. An expired cookie is sent to refresh first. With no session it redirects to `SigninPath` and ends nothing. If the store fails it answers 503 and keeps the cookie.

`Refresh` is `sessions.Refresh`. `auth` mounts nothing: mount `Refresh` yourself, for every method, at `Signed.RefreshPath`. With a database, `SignoutEverywhere` and `Refresh` answer 404.

`RequireSession` and `RequireFreshSession` send an expired cookie to refresh before they refuse. `Sweep` sweeps the link records. `SpendLinks` returns `ErrRecords`.

What each mode keeps:

- Default: the store holds each person's address, their user id and their epoch.
- Blind: the store holds only the HMAC. The centre can email someone only in reply to an address they just typed: no notices, and no "your sign-in address changed" mail. Logs carry no address, and a `Mailer` is required.
  If you turn on `Blind` for a store that already holds addresses, run `Sweep` once after the restart. It removes every address the store still holds. Until then, each one is removed the next time that person signs in or their session is refreshed.
- Both: a waiting link holds the address sealed under the instance key until it is redeemed or swept. No cookie carries a readable address.
- Sign-out ends this browser's session. A copy of its cookie can still be refreshed until "sign out everywhere" or the end of the 30-day session life. "Sign out everywhere" ends every copy within 15 minutes.
- Never rotate the pepper. Every `emails/` key is computed from it.
- The page a person returns to after sign-in is kept in this browser, as with SQLite `auth`. A link opened on another device lands on `SignedInPath`, and a newer sign-in attempt in the same browser replaces the destination.

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
In records mode it returns `ErrRecords`.

