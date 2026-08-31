# 🤖 passkey

`amadan.net/rastrillo/rastrillo/passkey`

A WebAuthn second factor on two seams: **step-up**, where an assertion
refreshes a stale session instead of a full re-sign-in, and
**sign-in**, where a verified first factor must be completed by an
assertion before a session exists.

[Passkeys and second factors](/docs/passkeys) is the guide.

## The trust boundary

A passkey never signs anybody in from nothing. Step-up endpoints demand
a valid session — stale is fine, absent is not. The sign-in pair demands
a live pending half-session, which only a verified first factor mints.
So a stolen credential id on its own opens no door.

## New, Config and Schema

```go
func New(cfg Config) (*Handlers, error)
```

Merge `passkey.Schema` into your boot set, and serve
[`webauthn.JS()`](/docs/reference/webauthn) as a static asset for the
browser half.

Credentials are public material: a public key verifies signatures and
nothing else. Challenges are single-use rows consumed by
`DELETE ... RETURNING`.

## The endpoints

Mount them behind `csrf.Protect` like every other mutating route:

| Route | Handler |
|---|---|
| `POST /passkey/register/begin` | `Handlers.RegisterBegin` |
| `POST /passkey/register/finish` | `Handlers.RegisterFinish` |
| `POST /passkey/stepup/begin` | `Handlers.StepUpBegin` |
| `POST /passkey/stepup/finish` | `Handlers.StepUpFinish` |
| `POST /passkey/signin/begin` | `Handlers.SignInBegin` |
| `POST /passkey/signin/finish` | `Handlers.SignInFinish` |
| `POST /passkey/signin/recovery` | `Handlers.SignInRecovery` |

The begin handlers answer `{"challenge": ...}`; the finish handlers take
`webauthn.mjs`'s `register()` or `authenticate()` result.

A successful step-up calls `sessions.SignIn`, rotating the session with
method `"passkey"` and a fresh `AuthTime` — exactly what
`sessions.RequireFresh` checks.

## The timeouts

A challenge lives two minutes: long enough for an authenticator prompt,
short enough that an abandoned one is not a standing invitation.

A pending half-session lives five minutes. Miss that window and you sign
in again from the top.

## Gate

```go
func (h *Handlers) Gate(w http.ResponseWriter, r *http.Request, sess sessions.Session) (bool, error)
```

The `SecondFactor` hook both identity plugins expose. Called where a
plugin would mint the session, it trades the immediate sign-in for a
pending half-session — a short-lived cookie plus a hashed row naming who
must still assert — and redirects to `Config.ConfirmPath`.

A verified assertion consumes the pending row, clears the cookie, and
mints the real session with the original first-factor method plus
`"+passkey"` — `"magiclink+passkey"`, say.

An account with no passkey passes the Gate untouched, returning
`(false, nil)`, so you can turn it on for everyone and let enrollment
decide who it applies to.

`Handlers.Enrolled(subject)` reports whether an account has a
credential, for a settings page or a conditional prompt.

## Recovery codes

```go
func (h *Handlers) RegenerateRecoveryCodes(subject string) ([]string, error)
func (h *Handlers) RecoveryCodesRemaining(subject string) (int, error)
```

`RegenerateRecoveryCodes` mints ten single-use codes and replaces any
existing set. Show them once, from a page you mount behind
`sessions.RequireFresh`.

`SignInRecovery` redeems one against the pending half-session where an
assertion would have gone. It is a plain form POST reading the field `code`, with no JavaScript,
deliberately: recovery is exactly the moment WebAuthn is not working.

A wrong code does not consume the half-session; it redirects to
`ConfirmPath?recovery=failed` so another can be tried. A correct one
burns the code, consumes the pending session, and mints a session whose
method is the first factor plus `"+recovery"` — a marker you can use to
nudge re-enrollment.

This is sign-in only. There is no recovery step-up, and `RequireFresh`
stays satisfiable only by an assertion or a full re-sign-in.

There is no attempt counter either. Redeeming needs a live half-session,
held for at most five minutes, and ten codes at 2⁻⁵⁰ apiece put brute
force far below any practical odds inside that window.

## Sweep

```go
func Sweep(db *sql.DB, now time.Time) error
```

Deletes expired challenges and pending half-sessions. Both are refused
on read once expired, so this is hygiene rather than enforcement.
