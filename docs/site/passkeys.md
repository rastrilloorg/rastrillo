# 🤖 Passkeys and second factors

`rastrillo/passkey` adds a WebAuthn second factor in the two places one
belongs: **step-up**, where a stale session is refreshed by an assertion
instead of a full re-sign-in, and **sign-in**, where a verified first
factor must be completed by an assertion before a session exists.

## The trust boundary

A passkey never signs anybody in from nothing.

It upgrades an *existing* session's freshness, or completes a sign-in
whose first factor already verified. Step-up endpoints demand a valid
session — stale is fine, absent is not. The sign-in pair demands a live
pending half-session, which only a verified first factor mints.

Either way a stolen credential id alone opens no door, and the primary
factor — magic link, keymail, password — stays the way an account is
entered.

## Wiring it

```go
pk, err := passkey.New(passkey.Config{ /* ... */ })
```

Merge `passkey.Schema` into your boot set, serve `webauthn.JS()` as a
static asset for the browser half, and mount the JSON endpoints behind
`csrf.Protect` like every other mutating route:

```text
POST /passkey/register/begin    -> {"challenge": ...}
POST /passkey/register/finish   <- register()'s result
POST /passkey/stepup/begin      -> {"challenge": ...}
POST /passkey/stepup/finish     <- authenticate()'s result
POST /passkey/signin/begin      -> {"challenge": ...}
POST /passkey/signin/finish     <- authenticate()'s result
POST /passkey/signin/recovery   <- form field "code"
```

## Step-up

A successful step-up calls `sessions.SignIn`, which rotates the session
with method `"passkey"` and a fresh `AuthTime` — exactly what
`sessions.RequireFresh` checks. See [Sessions](/docs/sessions) for the
middleware.

Two timeouts bound the ceremony. A challenge lives **2 minutes**: long
enough for an authenticator prompt, short enough that an abandoned one
is not a standing invitation. Challenges are single-use and
subject-bound, consumed by `DELETE ... RETURNING`.

## Sign-in-time 2FA: the Gate

`Handlers.Gate` is the `SecondFactor` hook both identity plugins expose:

```go
a, err := auth.New(auth.Config{
	// ...
	SecondFactor: pk.Gate,
})
```

Called at the exact point the plugin would mint the session, it lets an
enrolled account trade the immediate sign-in for a **pending
half-session** — a short-lived cookie plus a hashed row naming who must
still assert, which opens nothing by itself — and redirects to
`Config.ConfirmPath`, your "confirm with your passkey" page.

That page runs `webauthn.mjs`'s `authenticate()` against
`/passkey/signin/{begin,finish}`. A verified assertion consumes the
pending row, clears the cookie, and mints the real session with the
original first-factor method plus `"+passkey"` — `"magiclink+passkey"`,
say — and `AuthTime` now.

An account with **no** passkey passes the Gate untouched, returning
`(false, nil)`, and the plugin signs in exactly as it always did. You
can enable the Gate for everyone and let enrollment decide.

The gap between factors is bounded at **5 minutes**: first-factor
success to finished assertion inside that window, or sign in again from
the top.

## Recovery codes

For the account whose only passkey is lost.

```go
codes, err := pk.RegenerateRecoveryCodes(subject)
```

Mints **ten single-use codes**, shown once, from a page you mount behind
`sessions.RequireFresh`. `pk.RecoveryCodesRemaining(subject)` reports
how many are left, for a settings page that should nag.

`SignInRecovery` redeems one against the pending half-session where an
assertion would have gone. It is a **plain form POST with no
JavaScript**, deliberately: recovery is exactly the moment WebAuthn is
not working, and a flow that needs a working WebAuthn stack to recover
from a broken one is not a recovery flow.

A wrong code does **not** consume the half-session — it redirects back
to `ConfirmPath?recovery=failed` so the user can try another. A correct
one burns the code, consumes the pending session, and mints a session
whose method is the first factor plus `"+recovery"`, a marker your app
can use to nudge enrolling a replacement passkey.

**Sign-in only, by design.** There is no recovery step-up:
`RequireFresh` stays satisfiable only by an assertion or a full
re-sign-in.

There is deliberately **no attempt counter**. Redeeming requires a live
half-session — the first factor already verified — held for at most five
minutes, and ten codes at 2⁻⁵⁰ apiece leave brute force far below any
practical odds inside that window.

## What webauthn does and does not check

`rastrillo/webauthn` is the identity half: ES256 only, **no attestation
checking**, and a deliberately small CBOR subset reader.

Not checking attestation is a decision, not a gap. Attestation tells you
which manufacturer made the authenticator, which matters for enterprise
device policy and not for "is this the same key as last time". Verifying
it means shipping and maintaining a root certificate store.

`LegacyRPID` accepts credentials minted under a previous hostname, so
moving domains does not invalidate everybody's passkeys. A credential
cannot be *minted* under the old name — only used.

Signature counters are checked, and one going backwards is refused; an
authenticator that never counts is allowed, because many do not.

`webauthn/authtest` is a fake authenticator, public so your own tests
can drive a full ceremony without hardware.
