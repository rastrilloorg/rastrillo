# 🤖 oidc

`amadan.net/rastrillo/rastrillo/oidc`

Sign in with a third-party identity provider — Microsoft Entra ID or
Google Workspace today — as a first factor beside [password](/docs/reference/password)
and [auth](/docs/reference/auth)'s own keymail/magic-link flow, sharing
[sessions](/docs/reference/sessions) the same way they do. Ported from an
app that ran it in production, with a design document recording why each
decision is the way it is: `docs/superpowers/specs/2026-10-26-oidc-signin-design.md`
in this repository.

It is split into a verification core that holds no database handle and
knows nothing about any app (`Engine`, `Verify`, `Admitted`, the provider
list), and an HTTP layer that wires it into one app's `*sessions.Sessions`
(`Handlers`, `Config`, `New`, `Begin`, `Return`).

## Why this is written rather than imported

The obvious dependency is a general JOSE/OIDC client library, carrying a
JWE implementation, key wrapping, and a dozen signature algorithms this
package will never present. What is actually needed is one signature
algorithm over one token shape, in the standard library. The narrowness is
the security property, not a limitation:

- **RS256 and nothing else.** Every other algorithm — `none` and every
  HMAC family included — is refused BY NAME. The algorithm-confusion
  attack works by handing a verifier a token whose header asks it to
  check an HMAC with a key it holds as public; a verifier that implements
  one algorithm cannot be talked into another.
- **No refresh token** is asked for, kept, or stored. This flow
  establishes who is at the door once.
- **No userinfo call.** The identity comes out of the signed ID token,
  never out of a bearer-authenticated HTTP response.
- **https with no exception**, not even for loopback. Tests stand a real
  TLS server up rather than carrying a "local" exemption.

## Providers: a closed list, not an issuer box

```go
type Kind string

const (
	Microsoft Kind = "microsoft"
	Google    Kind = "google"
)

type Provider struct {
	Kind              Kind
	Label             string
	Directory         string
	DirectoryHint     string
	NeedsHostedDomain bool
	RedirectHint      string
}

func Providers() []Provider
func ProviderFor(kind Kind) (Provider, bool)
func (p Provider) NeedsDirectory() bool
func (p Provider) Issuer(directory string) (string, error)
func (p Provider) Refusal(directory string) string
```

An issuer field would let an app point this package at anything answering
an OpenID discovery document — a wider trust decision than "we use
Microsoft" or "we use Google", and not one this package makes on an app's
behalf. `Providers()` is the whole list; a third provider is a reviewed
row plus tests beside it, never a value an app supplies at runtime.

`Provider.Issuer` builds the issuer identifier an app stores on its
`Connection` from the one value a firm actually types (a directory/tenant
ID for Microsoft, nothing for Google) — call `Provider.Refusal` before
saving a connection, so an app is told what is wrong with a value at the
point of typing. `Provider.NeedsDirectory` says whether a provider asks
for one at all.

`Provider.NeedsHostedDomain` marks a provider whose issuer is shared by
every customer it has (Google: `https://accounts.google.com` for a
Workspace tenant and a personal Gmail account alike). For such a
provider, `Connection.AllowedDomains` is required and `Admitted` checks
the token's own `hd` claim against it — the only thing in the signature
that says which organisation the token came from.

## The verification core

```go
type Connection struct {
	Issuer, ClientID, ClientSecret, RedirectURI string
	AllowedDomains                               []string
}

type Metadata struct {
	Issuer, AuthorizationEndpoint, TokenEndpoint, JWKSURI string
}

type Engine struct {
	HTTP *http.Client
	Now  func() time.Time
}

func (e *Engine) Discover(ctx context.Context, issuer string) (Metadata, error)
func (e *Engine) Verify(ctx context.Context, m Metadata, conn Connection, raw, nonce string) (Identity, error)
func (e *Engine) Exchange(ctx context.Context, m Metadata, conn Connection, code string, s Start) (Identity, error)

func AuthCodeURL(m Metadata, conn Connection, s Start, prompt string) string
```

Build exactly one `Engine` per process. It caches every discovery
document and signing-key set it has fetched, keyed by issuer and by JWKS
URI respectively — never by `Kind` — so a hub app with many tenants under
one provider kind cannot have one tenant's signing keys verify another
tenant's tokens. A fresh `Engine` per request means a cold cache every
time; `Handlers` (below) holds one for exactly this reason.

`Discover` refuses two shapes of discovery document: one whose own
`issuer` disagrees with the issuer it was fetched from, and one whose
issuer contains `{` or `}` — Microsoft's multi-tenant endpoints
(`/common`, `/organizations`, `/consumers`) answer with a placeholder
there, and accepting that would admit an Entra account from any
organisation in the world.

`Verify` is exported separately from `Exchange` so a test can drive it
with a token it minted itself; nothing in this package's own HTTP wiring
calls it directly. It checks the signature first, against a key fetched
from the issuer's own published JWKS, and reads nothing inside the token
before that succeeds. After the signature: `iss` against the discovery
document, `aud` against `Connection.ClientID`, `azp` when `aud` carries
more than one value, `nonce` against the one minted for this ceremony,
and `exp`/`nbf`/`iat` with a two-minute clock-skew allowance.

```go
var ErrToken error        // any refusal of an ID token
var ErrNotConfigured error
var ErrMultiTenant error   // Discover's placeholder-issuer refusal
var ErrInsecure error      // a URL in this flow was not https
var ErrStateMismatch error // Start.CheckState's refusal

type Error struct {
	Stage, Message, Body string
	Status                int
}
func (e *Error) Error() string
```

`Error` carries the stage a failure happened at and the provider's own
words, bounded — useful to somebody administering a connection, never
shown to whoever is trying to sign in.

### Identity and the admission question

```go
type Identity struct {
	Subject, Issuer, Address, Name, HostedDomain, Tenant string
	AddressVerified                                       Tristate
	Methods                                               []string
	AuthTime                                               time.Time
}

type Tristate int

const (
	Unstated Tristate = iota
	Asserted
	Denied
)

func (t Tristate) String() string

func Admitted(p Provider, conn Connection, id Identity) error
var ErrNotAdmitted error
```

Every field on `Identity` was inside the verified signature; nothing was
read from a query string or an unauthenticated response. `AddressVerified`
is a tri-state rather than a bool because a provider's SILENCE about the
claim is not a denial: Entra never emits `email_verified` at all, and
folding that silence into `false` would refuse every Entra sign-in.

`Admitted` is the connection-level admission rule every provider needs,
run before an app's own membership hook ever sees the identity: no
address at all, or `AddressVerified == Denied`, is refused; `Unstated` is
let through. For a `NeedsHostedDomain` provider, `Identity.HostedDomain`
must be present and match one of `Connection.AllowedDomains`. `Subject`
(the token's `sub`) is recorded and does not by itself resolve an app's
account — see `Config.Admit`, below, for why mapping it to a subject is
the app's decision rather than this package's default.

## The ceremony

```go
type Start struct {
	State, Nonce, Verifier string
}

func NewStart() (Start, error)
func (s Start) Challenge() string
func (s Start) Valid() bool
func (s Start) CheckState(returned string) error
```

`Start` is the three unguessable values that bind one sign-in attempt
together: `state` is compared on return and stops somebody else's
completed ceremony being replayed into this browser; `nonce` travels
inside the ID token's signature and stops a token minted for another
attempt being presented to this one; the PKCE verifier (hashed into
`Challenge()` on the way out) stops an intercepted authorization code
being exchanged by anybody who does not hold it.

The ceremony itself — `Start` plus which connection and provider it is
for, and its own expiry — is sealed into a cookie under the app's
instance key, never stored in a table: an anonymous caller pressing a
sign-in button repeatedly writes nothing to disk, and the cookie IS the
browser binding, since a ceremony begun in one browser cannot be finished
in another. Its expiry is checked from INSIDE the sealed value against
the clock, never trusted from the cookie's own `Max-Age` alone, which is
a property of the browser rather than of this instance.

## Wiring one app

```go
type ConnectionKey string

type Config struct {
	Sessions      *sessions.Sessions
	InstanceKey   string
	Connections   func(ctx context.Context, kind Kind, key ConnectionKey) (Connection, bool, error)
	ConnectionKey func(r *http.Request) ConnectionKey
	Admit         func(ctx context.Context, conn Connection, id Identity) (subject string, err error)
	SecondFactor  func(w http.ResponseWriter, r *http.Request, sess sessions.Session) (done bool, err error)
	Refused       func(w http.ResponseWriter, r *http.Request, id Identity, reason error)
	SignedInPath  string
	Logger        *slog.Logger
	Now           func() time.Time
	HTTP          *http.Client
}

func New(cfg Config) (*Handlers, error)

type Handlers struct{}
func (h *Handlers) Begin(w http.ResponseWriter, r *http.Request)
func (h *Handlers) Return(w http.ResponseWriter, r *http.Request)
```

`New` validates the configuration at boot: `Sessions`, an `InstanceKey` of
at least 32 bytes, `Connections` and `Admit` are all required, so an app
that wires this up and forgets the membership question fails at boot
rather than admitting whoever a directory hands back.

Mount `Begin` and `Return` yourself, under whatever path carries a
`{provider}` URL parameter (`chi.URLParam(r, "provider")`):

```go
r.Get("/signin/oidc/{provider}", h.Begin)
r.Get("/signin/oidc/{provider}/return", h.Return)
```

`Connections` resolves a provider `Kind` and a `ConnectionKey` (opaque to
this package — `""` for a single-tenant app) to an app's registration.
`ConnectionKey` is read ONCE, at `Begin`, and sealed into the ceremony;
`Return` never calls the hook again, because a hub's tenants are routed by
path on one shared origin and the provider's own callback carries nothing
that would let `Return` re-derive which tenant a ceremony belongs to the
same way. This is also the whole answer to running on a hub product (a
signed-mode `*sessions.Sessions`, [sessions](/docs/reference/sessions)):
`Begin`/`Return` contain no branch on how `Sessions` was built, because
`Sessions.SignIn` already knows.

`Admit` maps a verified, already-domain-checked identity to the app's
OWN subject — never the address itself. This matters for two reasons
stated on the field's own doc comment: a signed-mode `Sessions` refuses
an address as a subject outright, and `secondfactor.Gate` looks up who
has a passkey or TOTP enrolled by this exact subject, so an app with
password or keymail sign-in already wired must return the same subject
those use for the same person. `SecondFactor` always runs AFTER `Admit`
succeeds, never before: calling it first would let an identity `Admit`
would have refused reach a pending half-session with no further
admission check.

`Refused` answers an identity that verified but was not admitted —
`Admitted`'s own refusals, `Admit`'s refusal, or a step-up
(`?reauth=1`, honouring `sessions.Sessions.RequireFresh`) the provider did
not actually satisfy freshly. Nil gets a bare 403 naming nothing beyond
"not admitted here"; only someone who has just proved they hold the
address reaches this at all, so naming the reason is no oracle for an
anonymous caller — every OTHER failure (a malformed return, an expired
ceremony, a token that fails verification) answers one generic page
regardless of cause, logged for an operator and never disclosed.

```go
var ErrStepUpNotFresh error
```

`ErrStepUpNotFresh` is what `Refused` is called with when `Begin` asked
the provider for a fresh sign-in (`prompt=login&max_age=0`, sent when the
request carries `?reauth=1`) and the resulting token's `auth_time` is not
within the last minute — some providers answer an already-interactive
session with a stale `auth_time` regardless of `max_age`, and this is
refused rather than minted as a session that would only loop at the next
freshness check.

## What this does not do

No account provisioning — an address must already be one `Admit` accepts.
No group or role mapping. No single log-out: ending a session here does
nothing at the provider, and turning someone off at the provider only
stops their NEXT sign-in, not a session already open — set a short
`SessionTTL` on `Sessions` if faster enforcement matters. No `amr`/`acr`
policy: a provider's claim about its own second factor is recorded on
`Identity.Methods` and decides nothing, for the same reason a firm's own
"our tenant enforces MFA" checkbox is not a fact this package can stand
behind. No second factor in Signed (hub) mode today — `secondfactor`'s
own `Method` suffix does not fit Signed mode's key-id alphabet, a gap
between those two core packages rather than anything specific to this
one; running `SecondFactor` against a Signed-mode `Sessions` fails closed
(an error from `Sessions.SignIn`, no session minted) rather than shipping
a silently broken combination, but only at sign-in time, since this
package cannot detect Signed mode from the outside to refuse the
combination at `New`.
