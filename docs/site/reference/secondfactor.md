# 🤖 secondfactor

`amadan.net/rastrillo/rastrillo/secondfactor`

The seam between a verified first factor and the session it earns: the
pending half-session that names who must still prove a second thing, the
attempt budget on proofs that can be guessed, and the recovery codes
that are every factor's escape hatch.

The package owns no factor of its own. [passkey](/docs/reference/passkey)
and [totp](/docs/reference/totp) each verify their own proof and redeem
the half-session here, so an app with two factors enrolled has **one**
confirm page offering both, and an app adding a third factor adds it
here rather than teaching each identity plugin about it.

[Second factors and authenticator apps](/docs/second-factors) is the
guide.

## The trust boundary

A half-session opens nothing by itself. It is minted only by
`Gate.Hold`, at the exact point a verified first factor would have
minted the session, and redeemed only by `Gate.Complete`, which a
factor calls once its own proof verified. Nobody is signed in from
nothing.

## New, Config and Schema

```go
func New(cfg Config) (*Gate, error)
```

`Config` requires `Sessions`, `DB` and `Origin`. `Origin` decides the
pending cookie's `Secure` and `__Host-` attributes the way
[sessions](/docs/reference/sessions)' own does. `ConfirmPath` is the
app's "confirm it's you" page, default `/signin/confirm`; `Logger` is
optional.

**Merge `Schema` into your boot set after `passkey.Schema`.** Its second
migration adopts the half-session and recovery-code tables the passkey
package owned before this one existed: it copies every recovery code
across and drops the old tables. Merged the other way round on a fresh
database, passkey's frozen first migration recreates two tables nothing
reads. On a database that has already deployed passkey the order does
not matter, because that migration has run. The codes people printed
survive either way.

## Wiring

```go
g, _ := secondfactor.New(secondfactor.Config{Sessions: sess, DB: writer, Origin: origin})
pk, _ := passkey.New(passkey.Config{ /* ..., */ Gate: g})
tp, _ := totp.New(totp.Config{ /* ..., */ Gate: g})
g.Add(pk, tp)

au, _ := auth.New(auth.Config{ /* ..., */ SecondFactor: g.Hold})
```

`Gate.Add` registers factors. A `Factor` is the one-method interface
`Enrolled(subject string) (bool, error)` — verification is the factor's
own business. `Gate.Enrolled` reports whether a subject holds at least
one, which is the app's cue for whether a step-up can be a proof rather
than a full re-sign-in, and for what the confirm page should offer.

## Holding and completing

| Call | What it does |
|---|---|
| `Gate.Hold` | The identity plugins' `SecondFactor` hook. No factor enrolled → `(false, nil)` and the plugin signs in exactly as before. Enrolled → store the half-session, remember a same-site `return_to`, redirect to the confirm page, `(true, nil)`. |
| `Gate.Pending` | Resolves the request's cookie to its live half-session — expiry-checked, **not** consumed. The confirm page and every factor's sign-in endpoint start here. |
| `Gate.Complete` | Consumes the half-session (`DELETE ... RETURNING`, so a raced second finish loses with `ErrConsumed`), clears the cookie, and mints the real session. |
| `Gate.Strike` | Records a failed guess. The last strike of the budget deletes the row and returns `ErrExhausted`. |
| `Gate.ConfirmPath` | Where `Hold` redirects, exposed so a factor's own failure redirect lands on the same page. |

`Pending` is one live half-session: `Subject`, the `Method` its first
factor verified with, and the `ReturnTo` the person was heading for.

The minted session's method is the first factor's plus `+` plus the
factor that completed it: a plugin passing `"magiclink"` yields
`"magiclink+passkey"` or `"magiclink+totp"`. An app may call `Hold`
itself with a method of its own — `"device"` for a remembered browser,
say — and the same rule applies: nothing is minted unless a factor
completes it.

Only a factor whose proof can be guessed needs `Strike`. A signature
over a fresh challenge cannot be, so passkey never calls it; totp calls
it on every wrong code.

## Recovery codes

```go
func (g *Gate) RegenerateRecoveryCodes(subject string) ([]string, error)
```

`Gate.RegenerateRecoveryCodes` mints a fresh set, atomically replacing
any previous one, and returns the plaintexts — the only moment they
exist outside the reader's screen. Only hashes reach the database. Mount
the page that calls it behind `sessions.RequireFresh`: showing
sign-in-grade secrets is exactly the dangerous action step-up exists
for. `Gate.RecoveryCodesRemaining` is the count behind a settings page's
"6 of 10 left", and the cue for whether to offer the recovery form at
all.

`Gate.SignInRecovery` is `POST /signin/recovery`, redeeming the form's
`code` field against the pending half-session. Mount it behind
`csrf.Protect`. It is a plain form POST deliberately: recovery is
exactly the moment the authenticator, WebAuthn or JavaScript is not
working. A miss leaves the half-session alive and redirects to the
confirm page with `?recovery=failed`, never saying which check missed. A
hit burns the code and mints the session with `+recovery`, a marker an
app can spot to nudge enrolling a replacement factor.

## Errors and housekeeping

`ErrConsumed` is `Complete`'s refusal when the half-session was already
redeemed or has expired. `ErrExhausted` is `Strike`'s report that the
budget is spent and the person must start again from the first factor.

`Sweep` deletes expired half-session rows. Correctness never depends on
it — `Pending` checks expiry itself — so call it from boot, from a
sidecar pass, or not at all.
