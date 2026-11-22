# 🤖 signin

`amadan.net/rastrillo/rastrillo/signin`

Assembles Rastrillo's default sign-in on top of [auth](/docs/reference/auth):
the emailed link first, and a passkey, an authenticator app and a
passphrase as further ways in. It builds [passkey](/docs/reference/passkey)
and [totp](/docs/reference/totp) from one config, keeps the passphrases,
limits wrong codes and passphrases in SQL, and fills auth's
`SigninState` with the doors that are on. Every way but the passkey ends
in `auth.Admit`, so refusal, the remembered way, `return_to` and the
session are the same as for a link.

[The default sign-in](/docs/signin) is the guide. A scaffolded
`--auth=magic` app wires all of this in `auth.go`.

## New and Config

```go
func New(config Config) (*Signins, error)

type Config struct {
	Auth      *auth.Auth
	Sessions  *sessions.Sessions // nil: auth.Sessions()
	DB        *sql.DB
	Origin    string
	Key       [32]byte // seals authenticator secrets
	Issuer    string   // the name beside the code in the app
	Ways      Ways
	Steps     []Step
	Lookup    func(ctx context.Context, address string) (string, bool, error)
	Authorize func(subject string) bool
	Proof     *pow.Guard
	ProofOff  bool
	Render    func(http.ResponseWriter, *http.Request, View)
	TrustedProxyHops *int
	Logger           *slog.Logger
	Now              func() time.Time
}

type Ways uint8

const (
	Passkey Ways = 1 << iota
	Authenticator
	Passphrase
)
func (ways Ways) Name() string

var Schema *migrate.Set
```

`Auth`, `DB` and `Render` are always required. `Origin`, `Key` and
`Issuer` are needed when the passkey or the authenticator way is on.
`Sessions` left nil is auth's own store, `auth.Sessions()`, so every way
mints the same cookie; set it only to that same store. Derive `Key` from the instance key with
`crypto.Derive`; `New` refuses a zero key with the authenticator on.
`Proof` is auth's unbound `pow.Guard`, because the sign-in forms carry
auth's challenge; set `ProofOff` only where auth's is off too.
`TrustedProxyHops` is auth's, so the limiter counts the same client
address.

`Ways` is the set beyond the link, which is always on and has no bit.
`Name` is a single way's word, `"passkey"`, `"authenticator"` or
`"passphrase"`, and `""` for a set.

`New` refuses an `auth` it cannot build on: records mode, `BlindLinks`
or a `SecondFactor` (see `auth.Assembles`). When auth's `SubjectFor` is
set it may create rows, so `New` wants `Lookup`, which finds the
subject an address signs in as without creating one; `ok` false is an
unknown address. With `Lookup` and the passkey way on, `Authorize`
says which subjects a passkey may sign in. Without `Lookup`, subjects
are addresses and `auth.Authorized` answers.

Merge `Schema` into the boot set after auth's, passkey's and totp's:

```go
var BootSchema = migrate.Merge(sessions.Schema, auth.Schema,
	pow.Schema, passkey.Schema, totp.Schema, signin.Schema,
	apitoken.Schema, Schema)
```

Its tables are the passphrases, the dismissed offers and the misses the
limiter counts. `Signins.Sweep` forgets misses too old to count; call
it from the loop that sweeps auth.

## Mounting

```go
func (signins *Signins) Mount(routes *action.Routes)
func (signins *Signins) Fill(state *auth.SigninState)
func (signins *Signins) Gate(next http.Handler) http.Handler
func (signins *Signins) Next(w http.ResponseWriter, r *http.Request)
```

`Mount` registers every route of the ways that are on, inside the
routes the app's session guard leaves open, since the sign-in POSTs are
for people not yet signed in. A way that is off gets no route, and with
`Ways` zero neither the offer nor Sign-in options is mounted. A nil
`*Signins` mounts every way's routes and serves none, so `rastrillo
routes` can list an app built with an empty `App`.

`Fill` sets the authenticator, passphrase and passkey doors on the
sign-in screen's `SigninState`, leaving the doors of ways that are off
nil. Call it between `auth.SigninState` and
`auth.PrepareSigninResponse`.

`Gate` wraps the app's handler. On a page load (a GET with
`Sec-Fetch-Mode: navigate`, or `Accept: text/html` without it) by a
signed-in person, it redirects to the first `Step` whose `Needed` is
true, then to the offer while it is due, carrying the requested page as
`return_to`. Everything else passes through, as do the steps' own paths
and `/signin`, `/auth/`, `/passkey/`, `/signout` and `/account/signin`.
A `Needed` error is a 500 and a log line, never a skipped step. A
step's page calls `Next` once it has saved.

```go
type Step struct {
	Path   action.Path
	Needed func(ctx context.Context, subject string) (bool, error)
}
```

## Routes

| Path | Method | Handler |
|---|---|---|
| `CodePath`, `/signin/code` | POST | `Signins.SignInWithCode` |
| `PassphrasePath`, `/signin/passphrase` | POST | `Signins.SignInWithPassphrase` |
| `PasskeyBeginPath`, `/passkey/discover/begin` | POST | passkey's `DiscoverBegin` |
| `PasskeyFinishPath`, `/passkey/discover/finish` | POST | passkey's `DiscoverFinish` |
| `OfferPath`, `/signin/faster` | GET | `Signins.ShowOffer` |
| `OfferLaterPath`, `/signin/faster/later` | POST | `Signins.LaterOffer` |
| `AddPasskeyPath`, `/signin/add/passkey` | GET | `Signins.ShowAddPasskey` |
| `RegisterBeginPath`, `/passkey/register/begin` | POST | `Signins.RegisterBegin` |
| `RegisterFinishPath`, `/passkey/register/finish` | POST | `Signins.RegisterFinish` |
| `AddAuthenticatorPath`, `/signin/add/authenticator` | GET, POST | `Signins.ShowAddAuthenticator`, `Signins.AddAuthenticator` |
| `AddPassphrasePath`, `/signin/add/passphrase` | GET, POST | `Signins.ShowAddPassphrase`, `Signins.AddPassphrase` |
| `OptionsPath`, `/account/signin` | GET | `Signins.ShowOptions` |
| `OptionsRemovePath`, `/account/signin/remove` | POST | `Signins.RemoveWay` |
| `WebauthnModulePath`, `/signin/js/webauthn.mjs` | GET | `webauthn.JS()` |
| `PasskeyModulePath`, `/signin/js/signin.mjs` | GET | `passkey.JS()` |
| `RegisterModulePath`, `/signin/js/register.mjs` | GET | `passkey.RegisterJS()` |
| `RevealModulePath`, `/signin/js/reveal.mjs` | GET | `RevealJS()` |

`SignInWithCode` and `SignInWithPassphrase` take `address` with `code`
or `passphrase`, behind the same proof as auth's `Begin`. An unknown
address and a wrong secret cost the same work and get the same answer,
the screen's own problem (`auth.ProblemCode`, `auth.ProblemPassphrase`).
A request over its budget is answered `auth.ProblemPaused` without the
secret being checked, so it costs less, for an unknown address and a
known one alike.
Five misses against one subject in fifteen minutes, whichever of its
addresses they came through, or thirty from one client address, pause
that subject or client; an address that signs in as nobody has its own
five. A success gives its attempt back.

`ShowOffer` goes on to `return_to` once the offer is no longer due, so
a bookmark never shows it twice. `LaterOffer` is "Not now",
remembered for good. Adding any way records the offer as answered too,
so removing that way later never brings the offer back.

The add pages and `RemoveWay` need a strong session from the last ten
minutes, timed from the sign-in itself (the session's `AuthTime`) and
never from when the session was made, which a refresh moves: not a
passkey used without user verification, and not a Keymail sign-in
whose server sent no `auth_time`. Otherwise they send
the person to sign in again with `reauth=1` and back. A link sign-in
is both, so the add pages the offer opens right after one pass. `RegisterBegin` and
`RegisterFinish` answer a fetch, so a refusal is a 401 whose JSON
`reauth` names the sign-in page, which the module goes to.
`ShowAddAuthenticator` begins a secret only when none is waiting, so
two tabs show the same QR. A passphrase is 15 to 256 characters,
counted as runes, with no other rules. `RemoveWay` takes `way`, and a
passkey's `id`; a way that is off or not the person's is a 404, and the
email link cannot be removed.

`RevealJS` is the passphrase page's show/hide module: it reveals the
"Show passphrase" switch, which ships hidden, and puts the field back
to a password field as the form is sent.

## What Render draws

```go
type View struct {
	Status  int
	Signin  *auth.SigninState
	Offer   *Offer
	Add     *Add
	Options *Options
}

type Offer struct {
	Ways                Ways
	LaterPath, ReturnTo string
}
func (offer Offer) Choices() []Choice
type Choice struct{ Way, Href string }

type Add struct {
	Way       Ways
	Action    string
	ReturnTo  string
	QR        template.HTML // authenticator only
	Key       string        // authenticator only
	Problem   string        // a locale key
	Done      bool
	Passkey   *auth.PasskeyDoor
	Address   string
	RevealURL string
}

type Options struct {
	Address    string
	Ways       []OptionRow
	RemovePath string
	Removed    bool
}
type OptionRow struct {
	Way     Ways
	Have    bool
	AddPath string
	Label   string
	ID      string
}
```

`signin` writes no HTML. It hands `Config.Render` a `View` with one of
`Signin`, `Offer`, `Add` or `Options` set, and the status to answer
with (0 for 200). ui's `signin-view` partial draws whichever it is, and
`signin-view-title` its title, so one render function on the stage
shell serves the sign-in screen and every page here. `Choices` lists the
offer's rows, passkey first, each carrying `ReturnTo` on to its add
page. A passkey has an `OptionRow` per credential, named by `Label`.
