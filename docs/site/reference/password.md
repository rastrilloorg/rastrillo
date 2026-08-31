# 🤖 password

`amadan.net/rastrillo/rastrillo/password`

An email-and-password identity plugin over the
[sessions](/docs/reference/sessions) core. Storage, rendering and CSRF
stay with the app.

[Passwords](/docs/passwords) is the guide.

## New and Config

```go
func New(cfg Config) (*Handlers, error)
```

`New` validates the configuration at boot, so you find out then rather
than on the first request. It errors when `Sessions`, `Lookup` or
`RenderSignin` is missing, and when `Create` is set without
`RenderSignup`.

```go
type Config struct {
	Sessions     *sessions.Sessions
	Lookup       func(ctx context.Context, email string) (id int64, hash string, err error)
	Create       func(ctx context.Context, email, hash string) (int64, error)
	SignedInPath string
	SecondFactor func(w http.ResponseWriter, r *http.Request, sess sessions.Session) (done bool, err error)
	RenderSignin func(w http.ResponseWriter, r *http.Request, d PageData)
	RenderSignup func(w http.ResponseWriter, r *http.Request, d PageData)
	Logger       *slog.Logger
}
```

`Lookup` receives an email already lowercased and trimmed, and returns
`sql.ErrNoRows` for an unknown address — treated identically to a wrong
password, verified against a decoy hash so the timing does not differ.

`Create` stores a new user. An error wrapping `ErrRefused` is a policy
refusal: `Signup` renders the refusal's own message verbatim at 403. Any other
error reads as a duplicate email, the only realistic failure for a
unique-email store. Leave it nil and signup is disabled entirely:
`SignupPage` and `Signup` both 404.

```go
var ErrRefused = errors.New("rastrillo/password: signup refused")

func Refuse(msg string) error
```

`Refuse` builds a refusal carrying visitor copy. A membership layer is
the motivating caller: telling an uninvited visitor that their address
is already registered is simply false.

The 403 is not free. It is distinguishable from the 422, so a refused
address and a registered one no longer look the same to a prober — the
duplicate message made them identical. That is the trade the seam
accepts: a true answer that can be told apart beats a false one that
cannot. An app that would rather not make the distinction leaves
`Create` nil and closes signup outright.

Write one refusal message that fits every refused address, and never
interpolate the submitted address into it. Nothing enforces that, and a
message that varies by address turns the 403 from a single outcome into
a finer oracle than the status alone. The message a wrapper adds is
never rendered — only the string handed to `Refuse` — and an empty one
renders the package's own generic copy rather than a blank page.

The refusal costs a rate-limiter unit, because `Hash` runs before
`Create`; it is charged to the refusal budget, not the shared one (see
[Rate limiting](#rate-limiting) below).

`SignedInPath` is where a fresh session lands absent a same-site
`return_to`; it defaults to `/`.

`SecondFactor` is called at the exact point a verified password would
mint the session, with the session that *would* be minted. Return
`done=true` when the hook took over the response, `false` when sign-in
should proceed unchanged. Nil is the plain behaviour.
`passkey.Handlers.Gate` is the shipped implementation.

## Handlers

```go
type Handlers struct{ /* unexported */ }
```

The verbs are enforced:

| Method | Verb | Notes |
|---|---|---|
| `Handlers.SigninPage` | GET | renders the form |
| `Handlers.Signin` | POST only | 405 otherwise |
| `Handlers.SignupPage` | GET | 404 when `Create` is nil |
| `Handlers.Signup` | POST only | 405 otherwise; 404 when `Create` is nil |
| `Handlers.Signout` | POST only | 405 otherwise |

Sign-out being POST-only is deliberate: a GET sign-out can be triggered
by any image tag on any page.

## PageData

```go
type PageData struct {
	Error    string
	Email    string
	ReturnTo string
}
```

What both render callbacks receive — enough to re-render the form with
the address still filled in and the problem stated.

Your callback must not write a status. `password` has already written
it before calling — 422, 403 or 429, depending on the outcome.

## Rate limiting

There are two per-email budgets, each 10 failures in 15 minutes,
answering 429 until one ages out. A successful sign-up resets both; a
successful sign-in resets the shared one.

**The shared budget** is spent by wrong credentials at sign-in and by
the duplicate-email answer at sign-up, and it gates both doors. They
share it on purpose: sign-up leaks the same fact sign-in does, whether
an address is registered, so letting an attacker switch endpoints for a
fresh allowance would defeat the limit.

**The refusal budget** is spent only by a `Create` policy refusal, and
gates sign-up alone. A refusal has to cost something — `Hash` runs
before `Create`, so an unmetered refusal path is a PBKDF2 amplifier —
but it must not cost the shared budget. A refused address is one nobody
has an account for yet; charging sign-in for it would let a stranger
who merely knows an invitee's address post ten refused signups and hold
that address at 429 on *both* doors, renewing it at about one request
every 90 seconds. `Signin` never consults it.

Both are in-memory, and so per-process. IP-level throttling belongs to
the deployment.

## Hashing

```go
func Hash(password string) (string, error)
func Verify(encoded, password string) bool
func NeedsRehash(encoded string) bool
```

`Hash` and `Verify` handle the encoding, and the parameters are pinned
by a test so they cannot drift quietly. `Verify` answers false for a
garbage or truncated encoding instead of erroring, so a corrupt stored
hash is indistinguishable from a wrong password.

`NeedsRehash` tells you whether a stored hash predates the current
parameters, so you can upgrade it transparently at the next successful
sign-in, the only moment you have the plaintext.
