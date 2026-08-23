# 🤖 password

`github.com/carlosframework/rastrillo/password`

An email-and-password identity plugin over the
[sessions](/docs/reference/sessions) core. Storage, rendering and CSRF
stay with the app.

[Passwords](/docs/passwords) is the guide.

## New and Config

```go
func New(cfg Config) (*Handlers, error)
```

`New` validates the configuration at boot rather than at request time.
It errors when `Sessions`, `Lookup` or `RenderSignin` is missing, and —
the one worth knowing — when `Create` is set without `RenderSignup`.

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

`Create` stores a new user. **Any** error it returns reads as a
duplicate email, the only realistic failure for a unique-email store.
Nil disables signup entirely: `SignupPage` and `Signup` both 404.

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

Five methods, and their verbs are enforced:

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

**The callback must not write a status.** `password` has already written
the 422 before calling it.

## Rate limiting

Sign-in and sign-up share one per-email budget: 10 failures in 15
minutes answers 429 until one ages out, and a success resets it.

Sharing the budget is the point. Sign-up leaks the same fact sign-in
does — whether an address is registered — so letting an attacker switch
endpoints for a fresh allowance would defeat the limit.

It is in-memory, and so per-process. IP-level throttling belongs to the
deployment.

## Hashing

```go
func Hash(password string) (string, error)
func Verify(encoded, password string) bool
func NeedsRehash(encoded string) bool
```

`Hash` and `Verify` handle the encoding; the parameters are pinned and
asserted by a test so they cannot drift quietly. `Verify` answers false
for a garbage or truncated encoding rather than erroring — a corrupt
stored hash must not be distinguishable from a wrong password.

`NeedsRehash` reports whether a stored hash predates the current
parameters, so you can upgrade it transparently at the next successful
sign-in — the only moment the plaintext is available.
