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
	BlindLinks       *BlindLinks
	DB               *sql.DB
	Reader           *sql.DB
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
	SigninOneWay     bool
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

var ErrBlindConfiguration, ErrBlindOperation error

type BlindLink struct {
	Hash, LookupKey, Purpose string
	Expires                  time.Time
}

type BlindLinkStore interface {
	Put(context.Context, BlindLink) error
	Take(context.Context, string) (BlindLink, bool, error)
	Spend(context.Context, string) (int, error)
	Sweep(context.Context, time.Time) error
}

type BlindLinks struct {
	Store          BlindLinkStore
	LookupKey      func(context.Context, string) (string, error)
	ResolveSubject func(context.Context, string) (string, error)
}
```

`auth` passes `Reader` to [`sessions`](/docs/reference/sessions). Leave it nil unless a mixed-load test on your instance shows it helps, as the sessions page says.

`Proof` is sign-in's front door, required unless `ProofOff`; [Magic links](/docs/magic-links#the-front-door) and [pow](/docs/reference/pow) cover it.

`SigninScreen` turns on the shipped sign-in screen's side of `auth`: the attempt and continuation cookies, the keymail continuation, and remembering the way in. Off, nothing about `Begin` or `Callback` changes. `BeginPath` and `ForgetPath` (default `/signin` and `/signin/forget`) are where you mounted `Begin` and `Forget`, for the screen's forms. `Remember` set to `false` keeps the screen and stops remembering.

`SigninOneWay` draws the screen one way in per page: the way this
browser used last, then **Other ways to sign in**, a page of its own
(`?ways=1`) that opens each way's page (`?way=email`, `?way=passkey` or
`?way=` a federated provider). It needs `SigninScreen`. Off, the screen
is exactly what it was.

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

`KeymailServers` limits keymail to the servers you list, both when an address is checked and when a sign-in finishes; an unlisted server's addresses get a link. Left nil, any server an address's domain names is used, and every connection auth makes to it may only reach a public address: no loopback, private, link-local, 100.64.0.0/10, multicast or unspecified address, checked as each connection opens. An empty, non-nil list turns Keymail off. See [Magic links](/docs/magic-links#aside-the-keymail-upgrade).

## Blind link storage

Set `Config.BlindLinks` when no address, including an encrypted address,
may enter storage. Supply a `BlindLinkStore`, `LookupKey` and
`ResolveSubject`. `LookupKey` receives the lowercase, trimmed address;
return your app’s HMAC email index as hex or base64url. Keep the pepper in
your app. `ResolveSubject` receives the key after the link is consumed;
enforce admission there and return an opaque account or person ref.

`Take` must atomically consume each hash at most once across processes and
restarts. Auth checks the returned record’s hash, purpose, key and expiry.
Failed checks or resolution leave it spent. `Put` must reject duplicate
hashes. Both hooks must return a nonempty string with no surrounding
whitespace and no `@`. These checks cannot prove opacity; never return an
encoded or encrypted address.

Return `sessions.ErrNoUser` from `ResolveSubject` for a 403 or `Config.Refused`.
That callback receives the verified lookup key in `Identity.Address`.
Other resolver errors create no session. `SecondFactor` receives the resolved
subject. Match `ErrBlindConfiguration` and `ErrBlindOperation` with
`errors.Is`. They are distinct errors and do not wrap address-bearing causes.

Apply `sessions.Schema` and your link schema; keep `DB` set for sessions.
Leave `Authorize`, `SubjectFor`, `SigninScreen`, `Records`, `Blind`, `Pepper`
and `Signed` unset, and `KeymailServers` empty. This mode disables Keymail
and cookies that remember addresses. Use a mailer that does not retain or
log recipients. Your hooks, middleware and logger attributes must follow
the same rule. The older records-mode `Blind` flag still stores decryptable
link addresses.

Before switching an existing app, stop old writers, invalidate pending
links, and migrate or revoke address-based sessions and related records.
Neither this option nor `secure_delete` can erase earlier WAL frames or
backups. Leave `BlindLinks` nil to keep existing sign-in behavior.

## Schema

```go
var Schema = migrate.MustFromFS(migrationFS, "auth")
```

In default SQL mode, merge `auth.Schema` **after** `sessions.Schema`.
Auth’s backfill reads the sessions table. `migrate.Merge`’s argument order
is the apply order.

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
func (a *Auth) Sessions() *sessions.Sessions
```

`SigninState` reads the query and this browser's own cookies and returns what the page shows, as plain data. It consults nothing else, so the page cannot reveal whether an address is known. Set `Passkey` on the result if you mounted passkey discovery. `PrepareSigninResponse` writes what goes with it: `Cache-Control: no-store`, `Referrer-Policy: no-referrer` on the page that moves on to Keymail, and deletions for any cookie that could not be trusted. Call it before rendering. With `SigninScreen` off, `SigninState` reads only the query and logs one warning per process. On the request `RenderConfirm` is given, `SigninState` answers `StepConfirm` with `Confirm` set and nothing else from the browser, and the partial draws the Sign in button.

`ReturnTo` is the page to land on after sign-in, taken from the query's `return_to` when it is on this site. The shipped sign-in screen posts it with every form. On a page of your own, put it in a hidden `return_to` field in each form, and use `PasskeyFinish()` as the passkey button's finish URL.

```go
type SigninState struct {
	Step          SigninStep
	Problem       SigninProblem
	Address       string
	SentTo        string
	SentInstead   bool
	Remembered    *Remembered
	ContinueURL   string
	Confirm       *ConfirmPageData
	BeginPath     string
	ForgetPath    string
	Force         bool
	ReturnTo      string
	Proof         *pow.Form
	Passkey       *PasskeyDoor
	Unavailable   bool
	OneWay        bool
	Way           SigninWay
	SigninPath    string
	Reauth        bool
	Federated     *FederatedDoor
	Authenticator *SecretDoor
	Passphrase    *SecretDoor
	AppProblem    *AppProblem
}
func (s SigninState) Door() string
func (s SigninState) Focus() string
func (s SigninState) PasskeyFinish() string
func (s *SigninState) ChooseWay(query url.Values)
func (s SigninState) Ways() []SigninWayRow
func (s SigninState) WaysHref() string
func (s SigninState) BackHref() string
func (s SigninState) EmailHref() string
func (s SigninState) FederatedHref() string
func (s SigninState) ConditionalUI() bool
func ParseWay(value string) SigninWay

type SigninStep string

const (
	StepAsk       SigninStep = "ask"
	StepReturning SigninStep = "returning"
	StepSent      SigninStep = "sent"
	StepContinue  SigninStep = "continue"
	StepConfirm   SigninStep = "confirm"
	StepWays      SigninStep = "ways" // the one-way screen's chooser
)

type SigninWay string // WayEmail, WayPasskey, WayAuthenticator,
                       // WayPassphrase, or a provider's name

type SigninProblem string

const (
	ProblemNone       SigninProblem = ""
	ProblemRate       SigninProblem = "rate"
	ProblemAddress    SigninProblem = "address"
	ProblemExpired    SigninProblem = "expired"
	ProblemKeymail    SigninProblem = "keymail"
	ProblemGeneric    SigninProblem = "generic"
	ProblemReauth     SigninProblem = "reauth"
	ProblemCheck      SigninProblem = "check"
	// The secret doors' own: a wrong code, a wrong passphrase, and an
	// address resting after too many of either.
	ProblemCode       SigninProblem = "code"
	ProblemPassphrase SigninProblem = "passphrase"
	ProblemPaused     SigninProblem = "paused"
)

type Remembered struct{ Method, Address, Provider string }

type FederatedDoor struct {
	Provider, Href, Label string
	Icon                  template.HTML
}

type AppProblem struct {
	Text string
	Ask  bool
}

type SigninWayRow struct {
	Way                  SigninWay
	Href, Detail, Label  string
	Icon                 template.HTML
}

type PasskeyDoor struct {
	BeginPath, FinishPath string
	ModuleURL, ScriptURL  string
	LegacyRPID            string
}

type SecretDoor struct{ Path string }
```

`Authenticator` and `Passphrase` are the secret doors, set by the page
like `Passkey`: each draws the address and the secret, posted together
to `Path`. Only the one-way screen draws them, and a browser that last
signed in with one is shown it again with the address filled in. Their
problems, `ProblemCode`, `ProblemPassphrase` and `ProblemPaused`, stay
on the door they came from. The [signin](/docs/reference/signin)
package fills all three doors with `Signins.Fill`; `Reauth` is a
step-up kept apart from `Problem`, so an error does not lose it.

On the one-way screen, set `Federated` for a sign-in another service
runs, such as Oficina ID: `Provider` is the `lastsignin` provider you
remember it under, `Href` where its button goes (the screen adds
`return_to`), `Label` the provider's name, and `Icon` an optional glyph
drawn in `currentColor`. With nothing remembered, it is the one way in.
A step-up (`?reauth=1`) never offers it. Remember the sign-in where you
mint the session, with the address of the account you resolved or none:
`RememberJar().Remember(w, lastsignin.Record{Method:
lastsignin.MethodFederated, Provider: lastsignin.ProviderOficina,
Address: address})`.

Set `AppProblem` for a problem only your app can name, such as a
refusal from `Refused`: `Text` is shown as the page's problem, and `Ask`
draws the email field instead of a one-tap for the refused address. It
works with the option on or off. A page that is not `auth`'s can fill a
`SigninState` itself and call `ChooseWay` with the query.

`Forget` is the Use a different email button: POST only, same-origin only, it forgets the remembered way in and redirects to `SigninPath`. On the one-way screen it opens the email way. `AnswerAsSent` is `Begin`'s answer for a sent link, without the link, for an admission check in front of `Begin`; see [Magic links](/docs/magic-links#an-admission-check-in-front-of-begin). `RememberJar` is the jar that remembers the way in; give it to `passkey.Config.Remember`. `Sessions` is the session store auth mints into; give it to `signin.Config.Sessions`, so the ways signin adds share auth's sessions.

## Other ways in

```go
func (auth *Auth) Admit(w http.ResponseWriter, r *http.Request,
	identity Identity, subject string)
func (auth *Auth) Subject(ctx context.Context, address string) (string, error)
func (auth *Auth) Authorized(address string) bool
func (auth *Auth) Assembles() error
func (auth *Auth) SigninPath() string
func (auth *Auth) NoteProblem(w http.ResponseWriter, address string)
var ErrNeedsLookup error
```

These are for a way in that `auth` does not run itself, such as the
[signin](/docs/reference/signin) package's authenticator and passphrase.
Verify the credential, then call `Admit` with the `Identity` (its
`Method` names the way, such as `"totp"`) and the subject the
credential belongs to. `Admit` runs `Authorize`, remembers the way,
offers the session to `SecondFactor`, mints it and lands on `return_to`,
exactly as a link does. It records the sign-in's time as the session's
`AuthTime` unless the identity carries one; Keymail's stays as its
server reported it, none included. If the address now resolves to a different
subject than the one you checked, it refuses.

`Subject` finds the subject an address signs in as without creating
anything. Only a plain database configuration can answer: with
`Records`, `BlindLinks` or `SubjectFor` it returns `ErrNeedsLookup`,
and the caller brings its own lookup. `Authorized` is `Authorize`'s
answer for an address, true when there is no hook, so a way auth does
not run admits whom auth's own ways would. `Assembles` reports the
first setting signin cannot build on (records mode, `BlindLinks`, a
`SecondFactor`), and `SigninPath` is where another way in sends a
visitor back with its problem in the query. `NoteProblem` keeps the
address that visitor typed in the screen's sealed attempt cookie, as
`Begin` does after a problem, so the field is filled again without the
address going in the URL.

## Guarding and reading

```go
func (a *Auth) RequireSession(next http.Handler) http.Handler
func (a *Auth) RequireFreshSession(maxAge time.Duration) func(http.Handler) http.Handler
func From(r *http.Request) (Identity, bool)
func (a *Auth) SessionFrom(r *http.Request) (Identity, bool)
```

A signed-out page request goes to `SigninPath?return_to=<the page>`, plus `reauth=1` when `RequireFreshSession` wants a fresh sign-in. After signing in, the visitor lands back on that page. Anything else gets a 403. On a tenant's direct host, a page request goes to the same page on the product path instead; see [Under a base path](#under-a-base-path).

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
The default policy allows the page's single `<style>` block by hash. If
you set `Options.CSP`, include
`'sha256-81gZUb3tp60YCqhJ0dItBPh5hRQiGbfst4gpwCNPTfI='` in `style-src`,
and in `style-src-elem` if your policy has one; otherwise, the page will
be unstyled.

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

	Suspended bool `json:"suspended,omitempty"`
}

func (a *Auth) User(ctx context.Context, uid string) (User, error)
func (a *Auth) UserByEmail(ctx context.Context, address string) (User, error)
func (a *Auth) EnsureUser(ctx context.Context, address string) (User, error)
func (a *Auth) Users(ctx context.Context, cursor string, limit int) ([]User, string, error)
func (a *Auth) SetSuspended(ctx context.Context, uid string, suspended bool) error
func (a *Auth) SessionCurrent(r *http.Request) (Identity, error)
func (a *Auth) EndSessions(ctx context.Context, uid string) error
func (a *Auth) SignoutEverywhere(w http.ResponseWriter, r *http.Request)
func (a *Auth) Refresh(w http.ResponseWriter, r *http.Request)

var ErrRecords error
```

The first sign-in creates the user. After that, `Identity.Address` (from `From` and `SessionFrom`) is the user id, not the address. Read the address with `User`, whose `Email` is empty in blind mode. `User` returns an error wrapping `objrec.ErrNotFound` for an unknown id.

`EndSessions` ends every session of a user: it adds one to the user's epoch, and the refresh route signs out any cookie that carries the old one. It returns `sessions.ErrNoUser` for a user with no record, and writes nothing.

`SetSuspended` suspends or resumes a user. Suspending adds one to the epoch, so cookies already out stop refreshing within 15 minutes. A suspended person's sign-in goes the way of an address `Authorize` refused: `Refused` is called and no cookie is set. It returns `sessions.ErrNoUser` for an unknown id.

`UserByEmail` finds the user for an address and never creates one; with none it returns an error wrapping `objrec.ErrNotFound`. `EnsureUser` creates the user as a first sign-in would, which an invitation needs. Both lowercase and trim the address. `Users` pages through every user in key order: pass `""` for the first page, then the cursor it returns, which is `""` after the last.

`SessionCurrent` is `SessionFrom` plus one read of the user. It refuses a session whose epoch is no longer the user's, or whose user is suspended, with `sessions.ErrNoUser`. A signed cookie is otherwise trusted for up to 15 minutes without a read, so call this before anything that mints a longer-lived credential or acts with authority.

These all return an error on an `Auth` built with a database. `Config.Now` sets the clock for links, signed sessions and refresh. It defaults to `time.Now`.

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

