# 🤖 Second factors and authenticator apps

`rastrillo/secondfactor` is the seam between a verified first factor and
the session it earns. `rastrillo/passkey` and `rastrillo/totp` are the
two factors that complete it. An account with both enrolled gets one
confirm page offering either; an app adding a third factor adds it
here rather than teaching each identity plugin about it.

## The half-session

A half-session names who must still prove a second thing. It is minted
only by `Gate.Hold`, which an identity plugin calls at the exact point a
verified first factor would mint the session, and it is redeemed only
by `Gate.Complete`, which a factor calls after *its* proof verified.
Nobody is signed in from nothing, and a half-session opens nothing by
itself: a short-lived cookie plus a hashed row, five minutes at most.

```go
g, err := secondfactor.New(secondfactor.Config{
	Sessions:    sess,
	DB:          writer,
	Origin:      origin,
	ConfirmPath: "/signin/confirm",   // the default
})
pk, err := passkey.New(passkey.Config{Sessions: sess, DB: writer, Origin: origin, Gate: g})
tp, err := totp.New(totp.Config{
	DB: writer, Sessions: sess, Gate: g,
	Key:    crypto.Derive([]byte(instanceKey), "rastrillo/totp"),
	Issuer: site,
})
g.Add(pk, tp)

au, err := auth.New(auth.Config{ /* ..., */ SecondFactor: g.Hold})
```

Merge `secondfactor.Schema` into your boot set **after**
`passkey.Schema`: its second migration adopts the half-session and
recovery-code tables the passkey package owned before this package
existed, copying every recovery code across. Merge `totp.Schema`
anywhere after `sessions.Schema`.

Mount the gate's one endpoint and each factor's own, behind
`csrf.Protect` like every other mutating route:

```text
POST /signin/recovery         <- form field "code"     (g.SignInRecovery)
POST /passkey/signin/begin    -> {"challenge": ...}    (pk.SignInBegin)
POST /passkey/signin/finish   <- authenticate()'s result
POST /totp/signin             <- form field "code"     (tp.SignIn)
POST /totp/stepup             <- "code", "return_to"   (tp.StepUp)
```

## The confirm page

It is yours. Ask `g.Pending(r)` who is waiting; a miss means nobody is,
and the page sends them to sign in. Then offer whichever factors that
subject holds — `pk.Enrolled`, `tp.Enrolled` — and the recovery form.
A passkey completes through `webauthn.mjs`; a code and a recovery code
are plain form POSTs, so the page works with JavaScript off, which is
also the moment a passkey ceremony cannot run.

The minted session carries the first factor's method plus the factor
that completed it: `"magiclink+passkey"`, `"google+totp"`,
`"password+recovery"`. `AuthTime` is now, which is what
`sessions.RequireFresh` checks.

A miss on a guessable proof is a `Strike`: five per half-session, then
it is gone and the person starts again from the first factor. `totp`
strikes; `passkey` never needs to, because an assertion over a fresh
challenge cannot be guessed.

## Hold from your own code

`Hold` takes the session that *would* be minted, and an app may call it
directly with a method of its own. A remembered browser that should
need only a passkey to get back in is one line: hold a session whose
`Method` is `"device"`, and nothing is minted unless a factor completes
it, exactly as for a magic link.

## Recovery codes

For the account whose only factor is lost.

```go
codes, err := g.RegenerateRecoveryCodes(subject)
```

Ten single-use codes, shown once, from a page you mount behind
`sessions.RequireFresh`. `g.RecoveryCodesRemaining(subject)` tells you
how many are left, for a settings page that should nag.

`SignInRecovery` redeems one against the half-session where a proof
would have gone. A wrong code redirects back to
`ConfirmPath?recovery=failed` and leaves the half-session alive; a
correct one burns the code and mints `"<first factor>+recovery"`, a
marker you can use to nudge enrolling a replacement. Sign-in only:
there is no recovery step-up, and no attempt counter, because ten codes
at 2⁻⁵⁰ apiece inside a five-minute window need none.

## TOTP {#totp}

`rastrillo/totp` is RFC 6238 exactly as every authenticator app
defaults to it — SHA-1, six digits, thirty seconds, one step of skew
either side — and nothing else, because an app that deviates loses half
its users' apps.

Enrolment is three calls behind your settings page:

```go
en, err := tp.Begin(subject, address)   // en.Key, en.URI, en.QR (inline SVG)
en, ok, err := tp.Pending(subject, address) // the same again, for a GET
ok, err := tp.Confirm(subject, code)    // the factor is live only after this
err := tp.Disable(subject)              // behind RequireFresh
```

Show `en.QR` (mark it `template.HTML`) and `en.Key` beside it for
typing in by hand; ask for one code to prove the app scanned it. `Begin`
on a live authenticator returns `ErrEnrolled` — disable first, from a
page behind `RequireFresh`, so a stale session cannot swap the factor
out from under its owner.

Secrets are sealed under `Config.Key` (AES-256-GCM) before they touch
the database, so a copied database file is not a copied factor. Derive
the key from the instance key; never store it beside the data.

A verified code's step is remembered and never accepted twice, so a
code read over a shoulder is spent the moment its owner uses it. At
sign-in the guess budget is the half-session's; at step-up and
enrolment it is ten misses in fifteen minutes per subject, in memory.

`StepUp` is the form-POST counterpart of a passkey assertion: a valid
but stale session plus a code rotates the session fresh with method
`"totp"`. A miss lands on `Config.StepUpFailedPath` (default
`/signin?reauth=1&totp=failed`) with `return_to` carried along.

`totp.Code(key, t)` is the code an authenticator holding `key` shows at
`t` — for a test that wants to play the phone.
