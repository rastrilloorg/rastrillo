# 🤖 totp

`amadan.net/rastrillo/rastrillo/totp`

An authenticator-app second factor: RFC 6238 time-based one-time
passwords, the six digits every authenticator app produces, on the same
two seams the [passkey](/docs/reference/passkey) package uses. At
sign-in it completes [secondfactor](/docs/reference/secondfactor)'s
pending half-session; at step-up it makes a valid-but-stale session
fresh again.

It is the factor for the person without a passkey-capable device, and
the one whose proof is a plain form field: every endpoint here is a form
POST, so a confirm page built on it works with JavaScript off — which is
also the moment a passkey ceremony cannot run.

[Second factors and authenticator apps](/docs/second-factors) is the
guide.

## The trust boundary

Nobody is signed in from nothing: `Handlers.SignIn` demands a live
half-session, `Handlers.StepUp` demands a session.

## New, Config and Schema

```go
func New(cfg Config) (*Handlers, error)
```

`Config` requires `DB`, `Sessions`, `Key` and `Issuer`. `Gate` is the
shared second-factor seam — leaving it nil keeps enrolment and step-up
working and makes sign-in refuse. `Issuer` names the app in the
authenticator's list, so make it the name people know the app by:
they will see it beside the code for years. `StepUpFailedPath` defaults
to `/signin?reauth=1&totp=failed`, and `Logger` is optional.

Merge `Schema` into your boot set. It is one table: a sealed secret per
subject, confirmed or not, and the last step it verified at.

`Handlers` is one per process — the step-up limiter's state lives on the
value. Register it with the gate, `g.Add(tp)`, so `Handlers.Enrolled`
answers the `secondfactor.Factor` contract.

## Secrets at rest

A TOTP secret is symmetric: whoever reads it can mint codes. It is
sealed under `Config.Key` (AES-256-GCM, `crypto.SealSym`) before it
touches the database, so a copied database file is not a copied factor.
Derive the key from the instance key with `crypto.Derive` — never store
it beside the data it seals. Losing it loses every enrolment.

## Enrolment

| Call | What it does |
|---|---|
| `Handlers.Begin` | Mints a secret and returns what to show. An unconfirmed secret from an abandoned attempt is replaced; a confirmed one is `ErrEnrolled`. |
| `Handlers.Pending` | The enrolment `Begin` started and nobody has confirmed yet, so a set-up page can be a GET that shows the same QR after a refresh. |
| `Handlers.Confirm` | Checks one code against the unconfirmed secret and, if it verifies, makes the factor live. |
| `Handlers.Disable` | Removes the authenticator, confirmed or not. |

`Enrolment` is what a set-up page shows: `Key` to type in by hand, `URI`
for the `otpauth:` link, and `QR`, the same URI as an inline SVG — mark
it `template.HTML`. The factor is live only once `Confirm` has seen a
code from it, which is what proves the app really scanned the QR.

`Begin` and `Disable` belong on pages behind `sessions.RequireFresh`,
like every change to how an account is entered. `ErrEnrolled` says to
disable first rather than silently replacing a live secret, which would
let a stale session swap the factor out from under its owner.

## The endpoints

Mount them behind `csrf.Protect` like every other mutating route:

| Route | Handler | Fields |
|---|---|---|
| `POST /totp/signin` | `Handlers.SignIn` | `code` |
| `POST /totp/stepup` | `Handlers.StepUp` | `code`, `return_to` |

`SignIn` verifies the code against the pending half-session's subject
and completes it through the gate, so the real session carries the first
factor's method plus `+totp`. A miss is a `secondfactor.Gate.Strike`:
the half-session survives until the fifth, redirecting to the confirm
page with `?totp=failed`, then `?totp=exhausted` once the budget is
spent. Which digits were wrong is never said.

`StepUp` verifies a code for the caller — whose session may be stale,
which is the point — and rotates the session fresh with `Method`
(`"totp"`) and `AuthTime` now, which is what satisfies
`sessions.RequireFresh` again. Success redirects to the form's same-site
`return_to`, else `/`; a miss to `Config.StepUpFailedPath` with
`return_to` carried along.

## Guessing

Six digits is a million-code space, and a code stays valid for the
current 30-second step plus one either side, so a guess is worth three
in a million. At sign-in the budget is the half-session's — five misses
and the person starts again from the first factor. At step-up and
enrolment it is a per-subject budget of ten misses in fifteen minutes,
in memory, the [password](/docs/reference/password) plugin's shape. A
verified code's step is remembered and never accepted twice, so a code
read over a shoulder is spent the moment its owner uses it.

```go
func Code(key string, t time.Time) (string, error)
```

`Code` is the code an authenticator holding `key` — the base32 string
`Enrolment` shows — produces at `t`. It is there for a test that wants
to play the phone, and it is also the only way a code is computed here:
verification runs the same function across the window.
