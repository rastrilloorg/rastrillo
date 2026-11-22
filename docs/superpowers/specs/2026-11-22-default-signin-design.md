# Default sign-in: a link first, more ways after, the last one remembered

Status: draft for the operator's review, 2026-11-22. Decisions taken
with the operator in conversation are marked **(operator)**; calls made
in writing this are marked **(call)**.

## Why

Every new Rastrillo app should start with the sign-in that Tito Go and
Eleven have converged on, without assembling it. Today the pieces exist
but a scaffolded app gets almost none of them:

- The scaffold wires magic links only. `SigninOneWay` is off, no passkey
  is mounted, and Keymail is switched off (`KeymailServers: []string{}`,
  cmd/rastrillo/scaffold/auth.go.tmpl:63-93).
- Nobody is ever invited to add a second way in.
- An authenticator app (`totp`) is a second factor only, and passwords
  (`password`) are a separate plugin with its own pages and subjects.
- Keymail is not invisible: the one-tap reads "Continue to Keymail" and
  the hand-off page "Taking you to Keymail" (locales/en.toml:116-136).

The aim is one call that turns the whole pattern on, so it is a default
rather than a recipe, and one shape Tito Go and Eleven can later adopt.

## What a person sees

**First visit.** One email field. The address is classified on the
server as today: a claimed Keymail address goes straight into Keymail's
OAuth ceremony; anything else gets a magic link **(operator)**.

**After the link (or Keymail), a new person goes through:**

```
[app's sign-up step(s), if any]  →  "Sign in faster next time"  →  app
```

- Sign-up steps are the app's own pages (name, company, anything),
  supplied to the package. None ship in the scaffold **(operator)**.
- "Sign in faster next time" is shown once: passkey first and marked
  recommended, then authenticator app, then passphrase, and "Not now",
  which is remembered and never asks again **(operator, option A)**.
  Keymail users skip it: Keymail already secures the account with a
  passkey **(operator)**.

**Returning.** The screen leads with the way this browser used last,
and under it "Other ways to sign in" opens the chooser. This is the
existing one-way screen (`auth/oneway.go`, the smart-door rules from
Oficina Home's spec 2026-11-19), extended with two ways:

| Way | Remembered one-tap | Chooser row |
|---|---|---|
| Email link / Keymail | Continue as ‹address› | Email me a link |
| Passkey | Sign in with your passkey | Passkey |
| Authenticator app | address shown, code field | Authenticator app |
| Passphrase | address shown, passphrase field | Passphrase |

(Copy here is placeholder; see "Copy".)

**Account settings** gain a "Sign-in options" page: the ways this person
has, add any missing one, remove any but the email link.

## Decisions

- **Every added way is a way in on its own**, not a second step
  **(operator)**. There is no second factor in the default.
- **"OTP" means an authenticator app** (RFC 6238, `totp`). Codes sent by
  email are what the magic link already is **(operator)**.
- **The email link can never be removed** and is always in the chooser
  **(call)**. It is the recovery path for a lost passkey, phone or
  passphrase; remembering a way only changes what the screen leads with.
- **Authenticator and passphrase ask for the address too** **(call)**:
  they are knowledge or possession of a code, not discoverable
  credentials. The address is prefilled when remembered.
- **Keymail is on by default and invisible** **(operator)**. The scaffold
  switched it off so a visitor's domain could not make the app dial a
  server of that domain's choosing. Instead of keeping it off, the
  default is any delegated server, but every connection **the app
  server makes** for Keymail (probe, well-known, lookup, token exchange,
  and each redirect hop of those) may only reach a public unicast
  address: no loopback, private, link-local, CGNAT (100.64/10, which is
  Tailscale's range), multicast or unspecified addresses, checked at
  dial time so DNS rebinding cannot route around it **(call)**. The
  browser's navigation to the authorize URL is not covered and need not
  be: it is the visitor's own browser going where the address's domain
  owner sent it, the same party that controls where a magic link would
  be delivered. An explicit `KeymailServers` list keeps today's
  behaviour.
- **No "Keymail" in what people read** **(operator)**. The one-tap for a
  remembered Keymail address becomes "Continue as ‹address›" like any
  remembered email; the hand-off page says it is signing you in; the
  problem callout and the hard-coded mismatch message
  (auth/handlers.go:337) say what failed without naming the provider.
  Keymail's own pages are Keymail's business.
- **The chooser lists every way the app enables**, not the ways this
  address has **(call)**. The chooser has no address to look up, and
  looking one up would say who has an account. A wrong choice fails the
  same way an unknown address does.
- **Sign-up details are collected after the address is proved**
  **(operator)**. The first screen cannot tell a new person from a
  returning one without revealing who has an account, and a Keymail
  user never sees a link.

## Approaches

1. **Extend each package and have the scaffold wire them.** Keeps the
   seams; grows the generated code by roughly 60–100 lines against
   scaffold size budgets with no headroom, and every app carries the
   wiring.
2. **The same extensions, plus a `signin` package that assembles them**
   — chosen **(operator)**. `signin.New(signin.Config{…})` mounts every
   route and the scaffold calls it in a few lines. Each method stays in
   its own package. Cost: one more layer; an app with unusual needs
   wires the pieces itself, as today.
3. Rejected: **generate it all into the scaffold.** Every app gets its
   own drifting copy, which is the opposite of a default.

## Design

### The `signin` package

```go
signins, err := signin.New(signin.Config{
    Auth:     auth,      // *auth.Auth, SigninScreen and SigninOneWay on
    Sessions: sess,
    DB:       writer,    // signin's own tables; SQL only (Non-goals)
    Key:      sealKey,   // seals TOTP secrets
    Ways:     signin.Passkey | signin.Authenticator | signin.Passphrase,
    Steps:    []signin.Step{ /* app's sign-up steps, optional */ },
    Pages:    pages,     // *ui.Pages that draw the shipped partials
})
signins.Mount(routes)          // every route below
handler = signins.Gate(handler) // steps and the offer; see "The gate"
```

It owns: building `passkey`, `totp` and the passphrase store from one
config; mounting their routes; filling `SigninState` (passkey door, the
new ways); the offer and the Sign-in options pages; the gate. It renders
through `ui` partials, keeping the 2026-09-02 ruling that `auth` draws
no HTML.

Name: `signin` collides with Keymail's library name
(`github.com/keymaildev/signin`), which only `auth` imports. The builder
may pick another name if the collision bites; the shape is what matters.

### One admission path: `auth.Admit`

Today `admit` (auth/handlers.go:486-548) is the single place that runs
`Authorize`, resolves the subject, remembers the way, runs
`SecondFactor`, mints the session and redirects — but only for links and
Keymail. Passkey and password each re-implement part of it, and
password skips remembering entirely.

Export it as `(*Auth).Admit(w, r, Identity)`, and add
`(*Auth).Subject(address) (string, error)` that resolves an address to
the subject `admit` would use **without creating anyone**. The new ways
verify a credential, then call `Admit` with `Method` "totp" or
"passphrase", so refusal, remembering, return_to and the session are
identical for every way in **(call)**.

What "an existing account" means for these two ways **(call)**: a
subject that has that credential. In DB mode with no `SubjectFor` the
subject is the address, so `Subject` is pure and an address with no
TOTP secret or passphrase is simply "unknown". `SubjectFor` today may
create rows (auth/subject.go:26), so an app that sets it must also set
`signin.Config.Lookup func(ctx, address) (subject string, ok bool,
err error)`, read-only; `signin.New` refuses the combination without
it. `Admit` re-resolves the address and fails closed if the subject
differs from the one the credential was checked against, so a
credential can never admit someone else.

Passkeys keep their own admission (`DiscoverFinish`): discovery yields
a subject, not an address, and that path already authorizes and
remembers. `signin` only configures it.

### Authenticator app as a way in

`totp` gains an exported `Check(subject, code) (bool, error)` over its
existing verifier (replay-safe through `last_counter`). The sign-in
handler lives in `signin`:

1. Form posts address + code, behind the same `pow.Guard` as `Begin`.
2. `auth.Subject(address)`; unknown address and wrong code get the
   **same answer, at the same cost** (a decoy `Check` on unknown).
3. Success → `auth.Admit(…Method: "totp")`.

Single-factor TOTP is weaker than the package was built for: the code is
the only proof. Mitigations **(call)**:

- A **persistent** limiter in SQL (not `totp`'s in-memory one): 5 misses
  per subject per 15 minutes, and a per-IP budget. A subject over its
  budget gets the same answer as a wrong code and the screen points to
  the email link.
- Enrolment confirms one code before the way is live (existing
  `Begin`/`Confirm`).
- `totp`'s package comment ("never signs anybody in from nothing")
  changes to say that `signin` does, and how it is limited.

### Passphrase as a way in

`password`'s subjects are `int64` ids from the app's own lookup
(password/handlers.go:508); auth's are addresses, `SubjectFor` values or
record ids. Reusing its handlers would make a passphrase account and a
link account two different people. So `signin` keeps its own
`signin_passphrases(subject, hash, created_at)` and reuses only
`password`'s hashing and its decoy-hash timing **(call)**. Sign-in is
address + passphrase → `auth.Subject` → verify → `auth.Admit(…Method:
"passphrase")`, with the same limiter and equal answers as TOTP.

Passphrase rules **(call)**: 15 to 256 characters (NIST 800-63B's
single-factor minimum), no composition rules, `autocomplete=
"new-password"` on set and `"current-password"` on sign-in with the
address as a `username` field so password managers work.

`password` the plugin is unchanged; apps that use it today keep it.

### Remembering the two new ways

`lastsignin` gains `MethodTOTP` and `MethodPassphrase`, both carrying an
address. Older binaries read such a record as invalid and clear it,
which is safe. `auth`'s one-way rules (`SigninWay`, `ParseWay`,
`offers`, `rememberedWay`, `Ways`) gain `WayAuthenticator` and
`WayPassphrase`, offered only when `SigninState` says the app enabled
them. The chooser's order: email, passkey, authenticator, passphrase,
then "Use a different email". The partial gains two doors: the
remembered address shown with the code or passphrase field, and the
same with an address field when nothing is remembered. Reauth offers
all four (all are local).

Oficina Home's spec limits its own methods to passkey, email/Keymail
and Oficina ID; Home fills `SigninState` itself and simply does not
enable the new ways. Nothing changes for Home.

### The gate: sign-up steps and the offer

`auth` cannot tell a first sign-in from any other (in DB mode there is
no user table). So nothing here depends on "first": steps and the offer
are driven by what is still true about the person **(call)**.

```go
type Step struct {
    Path   action.Path // the app's page; the app mounts and draws it
    Needed func(ctx context.Context, subject string) (bool, error)
}
```

`signins.Gate(next)` wraps the app's handler. On a **top-level HTML
navigation** (GET, `Sec-Fetch-Mode: navigate` or `Accept: text/html`)
by a signed-in person, it redirects to the first step whose `Needed`
is true, carrying the requested URL as `return_to`. Everything else
(POSTs, API, assets, the steps' own paths, sign-in and sign-out routes)
passes through. A step's page calls `signins.Next(w, r)` when it has
saved, which goes to the next needed step or to `return_to`. Closing
the tab mid-way resumes at the unfinished step on the next visit,
whichever way they come in.

After the steps, the gate shows the offer when all of these hold: the
session's method is the email link (not Keymail, passkey or the
others), the app enables at least one added way, the person has none of
them, and they have not chosen "Not now" (`signin_offers(subject,
dismissed_at)`). Until they pick something or dismiss it, every
navigation lands on the offer, so it is seen once and cannot be lost.

An app adopting this sees its existing link users get the offer once.
That is intended.

### The offer and Sign-in options pages

- **Offer:** three choices, passkey first ("Recommended"), each opening
  its enrolment; "Not now". After enrolling, a short confirmation and on
  to `return_to`.
- **Passkey enrolment:** existing `RegisterBegin`/`RegisterFinish`.
- **Authenticator enrolment:** QR and key (`Enrolment.QR`), one code to
  confirm.
- **Passphrase:** one field with show/hide, rules above.
- **Sign-in options:** lists the ways the person has, adds any missing,
  removes any but the email link. Adding or removing needs a **fresh,
  strong** sign-in; otherwise it sends them to the sign-in screen with
  `reauth=1` first **(call)**. Fresh means an authentication gesture in
  the last 10 minutes, measured by the session's `AuthTime` and never
  substituted by the session's creation time. Strong rules out:
  - `passkey-nouv` sessions (a passkey used without PIN or biometric).
    Passkeys enrolled through `signin` request user verification as
    required, so this only affects older credentials.
  - Keymail sessions whose server returned no `auth_time` (the library
    reads it as zero, keymail.go:87, and `sessions.Fresh` would
    otherwise fall back to the session's timestamp,
    sessions/sessions.go:513). For those, the step-up sends an email
    link (`force=1`) instead of the Keymail round trip.

  The offer right after sign-up is exempt: its session was just minted
  by an email link, which is both fresh and the strongest proof the
  person has at that moment.

All routes are mounted by `signins.Mount` under `/signin/…` and
`/account/signin-options` (exact paths left to the builder), behind the
app's existing CSRF protection.

### Scaffold

`auth.go.tmpl` turns on `SigninOneWay`, drops `KeymailServers:
[]string{}`, and calls `signin.New` with all three ways enabled and no
steps, then `Mount` and `Gate`. The account page links to Sign-in
options. The scaffold budget is raised by what that costs, with the
reason recorded in `.rastrillo/budgets.txt`. The docs
(`docs/site/magic-links.md`, `passkeys.md`, a new page for the
assembly) show adding a sign-up step, with "your name" as the example.

## Copy

Every new or changed user-facing string goes through copy review before
it lands: the offer, the two new doors and chooser rows, Sign-in
options, the de-Keymailed strings and errors. The labels in this
document are placeholders.

## Non-goals

- Second factors in the default. `secondfactor` and the existing
  2FA wiring stay available; combining a TOTP *way in* with a TOTP
  *second factor* for the same app is refused at `signin.New` rather
  than designed (it would ask for the same code twice).
- Records mode (`auth.Config.Records`) and blind apps. `passkey`,
  `totp` and `secondfactor` need SQL; `signin.New` refuses records mode.
  Oficina Home keeps its own sign-in.
- Moving Tito Go or Eleven onto this. It is shaped so they can.
- Codes sent by email, social sign-in, or changing `password`.

## Constraints

- `auth` renders no HTML; pages are `ui` partials.
- Equal answers for unknown addresses on every way.
- Scaffolded apps' code limits (80 columns, 400 lines per file, 40 per
  function) and the size budget ratchet.
- Fiesta styles the one-way screen: the unmerged
  `feat/smart-door-fiesta-hooks` (eb33cd94) adds hooks there, and the
  new doors should land after it or carry the same hooks.

## Left to the builder

- Exact route paths, table names and the package name.
- The per-IP limiter's numbers and where the limiter lives (in `signin`,
  or a small shared package that `password` could later use).
- How the gate detects an HTML navigation, within the rule above.
- The confirmation page after enrolling, within the copy review.
- How `Needed` results are cached per request, if at all.

## Review log

- 2026-11-22, builder check (Astra): four questions, all settled as
  calls above. They covered which subjects count as an existing account
  (`Lookup`), passkey user verification, Keymail step-up without
  `auth_time` (a real hole: a missing `auth_time` could pass the
  freshness check), and the Keymail dial guard wrongly claiming to
  cover browser redirects.
