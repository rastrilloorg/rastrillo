# 🤖 Passkeys and second factors

`rastrillo/passkey` adds a WebAuthn second factor in two places: at
step-up, where an assertion refreshes a stale session instead of a full
re-sign-in, and at sign-in, where a verified first factor has to be
completed by an assertion before a session exists. It is one of two
factors the [second-factor gate](/docs/second-factors) knows; the
other is an [authenticator app](/docs/second-factors#totp).

## What a passkey is allowed to do

A passkey can sign someone in on its own, through the discover endpoints. A passkey that checked the person with a PIN, a fingerprint or a face has already proved who they are and what they hold, and asking for an emailed link as well adds work and no safety.

It also refreshes an existing session at step-up, and completes a sign-in whose first factor already checked out. A passkey that only found somebody present, without checking who, is the weaker proof: where the account has another factor, discover holds the sign-in for it.

## Wiring it

```go
pk, err := passkey.New(passkey.Config{ /* ... */ })
```

Merge `passkey.Schema` into your boot set, serve
[`webauthn.JS()`](/docs/reference/webauthn) as a static asset for the
browser half, and mount the JSON endpoints behind `csrf.Protect` like
every other mutating route:

```text
POST /passkey/register/begin    -> {"challenge": ...}
POST /passkey/register/finish   <- register()'s result
POST /passkey/stepup/begin      -> {"challenge": ...}
POST /passkey/stepup/finish     <- authenticate()'s result
POST /passkey/signin/begin      -> {"challenge": ...}
POST /passkey/signin/finish     <- authenticate()'s result
POST /passkey/discover/begin    -> {"challenge": ...}
POST /passkey/discover/finish   <- authenticate()'s result -> {"to": ...}
```

On `auth`'s shipped sign-in screen, the discover pair is the passkey button. Set `Config.Remember` to `auth`'s `RememberJar()`: a passkey sign-in then clears any address typed earlier in the same browser, and the next visit offers the passkey first. Without it, an address someone typed before using their passkey stays in the form.

## Step-up

A successful step-up calls `sessions.SignIn`, which rotates the session
with method `"passkey"` and a fresh `AuthTime` — exactly what
`sessions.RequireFresh` checks. See [Sessions](/docs/sessions) for the
middleware.

A challenge lives two minutes: long enough for an authenticator prompt,
short enough that an abandoned one is not a standing invitation.
Challenges are single-use and subject-bound.

## Sign-in-time 2FA

The pending half-session between factors is the
[second-factor gate](/docs/second-factors)'s, not this package's. Wire
it once:

```go
g, err := secondfactor.New(secondfactor.Config{ /* ... */ })
pk, err := passkey.New(passkey.Config{ /* ..., */ Gate: g})
g.Add(pk)

a, err := auth.New(auth.Config{
	// ...
	SecondFactor: g.Hold,
})
```

`Hold` is the `SecondFactor` hook every identity plugin exposes. Called
where the plugin would mint the session, it trades the immediate sign-in
for a half-session for any account with a factor enrolled and redirects
to your confirm page. That page runs `webauthn.mjs`'s `authenticate()`
against `/passkey/signin/{begin,finish}`; a verified assertion completes
the half-session through the gate, which mints the real session with the
original first-factor method plus `"+passkey"` — `"magiclink+passkey"`,
say.

Recovery codes, for the account whose only passkey is lost, live on the
gate too: `g.RegenerateRecoveryCodes`, `g.RecoveryCodesRemaining` and
the plain-form `g.SignInRecovery`. A passkey handler with no `Gate`
still enrols and steps up; only the sign-in pair refuses, because
nothing can be pending.

## What webauthn checks

`rastrillo/webauthn` is the identity half: ES256 only, and no
attestation checking.

Skipping attestation is a decision. Attestation tells you which
manufacturer made the authenticator, which matters for enterprise device
policy and not for "is this the same key as last time". Verifying it
means shipping and maintaining a root certificate store.

`LegacyRPID` accepts credentials minted under a previous hostname, so
moving domains does not invalidate everybody's passkeys. A credential
cannot be minted under the old name, only used.

Signature counters are checked and one going backwards is refused. An
authenticator that never counts is allowed, because plenty do not.

`webauthn/authtest` is a fake authenticator, public so your own tests
can drive a full ceremony without hardware.
