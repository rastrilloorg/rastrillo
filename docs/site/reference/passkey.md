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
| `POST /passkey/discover/begin` | `Handlers.DiscoverBegin` |
| `POST /passkey/discover/finish` | `Handlers.DiscoverFinish` |

The begin handlers answer `{"challenge": ...}`; the finish handlers take
`webauthn.mjs`'s `register()` or `authenticate()` result. `RegisterFinish`
also reads an optional `"label"`, the name the person gives the new
credential.

A successful step-up calls `sessions.SignIn`, rotating the session with
a fresh `AuthTime` — exactly what `sessions.RequireFresh` checks — and a
method that says how the authenticator checked the person: `Method`
(`"passkey"`) when it verified them with a PIN, fingerprint or face,
`MethodUnverified` (`"passkey-nouv"`) when it only found somebody
present. A finished registration rotates the session the same way: the
ceremony proved the new passkey as surely as an assertion would. The Gate
flow suffixes the same two words onto the first factor.

## Discover: the passkey as the front door

A passkey asserted with user verification has already proved possession
and the person, so `DiscoverBegin`/`DiscoverFinish` let it sign in on
its own: no session, no half-session, no address typed. The browser is
asked for any credential it holds for this relying party
(`allowCredentials` empty) and the credential it answers with says who.
`Config.Authorize`, when set, is asked before anyone is admitted this
way — the roster check the app's other doors take.

An assertion WITHOUT user verification is a weaker proof. With
`Config.OtherFactor` reporting another factor for the subject, the
sign-in is held at the Gate as `MethodUnverified` for that factor to
complete, and the JSON answer's `"to"` is the confirm page; with nothing
else to prove, the person is signed in at the weaker method and it is
the app's job to nudge. Verified or not, the answer carries `"to"`, the
same-site `return_to` or `Config.SignedInPath`.

`Config.Remember` takes `auth`'s `RememberJar()`. With it, a verified discover assertion ends the sign-in screen's attempt and remembers passkey as this browser's way in, with no address.

## The inventory

```go
func (h *Handlers) List(subject string) ([]Info, error)
func (h *Handlers) Rename(subject, id, label string) error
func (h *Handlers) Remove(subject, id string) error
```

`Info` is one credential as a person's Security page shows it: `ID` (the
credential id, hex), `Label`, `UserVerified` (the authenticator verified
the user at registration, so a policy rates it a tier higher),
`BackupEligible` and `BackupState` (a synced passkey, or one bound to the
device it lives on), `AAGUID` (the authenticator's make, where
attestation carried one), `CreatedAt` and `LastUsedAt`. Nothing secret
is in it. `Rename` and `Remove` take the subject with the id, so a
handle from one person's list is never redeemed by another; an id that
is not theirs answers `ErrNotYours`. Whether an account may lose a
credential — its last factor, its only strong one — is the app's rule
to hold before calling `Remove`.

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
