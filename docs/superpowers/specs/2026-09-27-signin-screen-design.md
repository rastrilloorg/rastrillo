# A shipped sign-in screen, and a browser that remembers how you got in

Status: design approved in conversation 2026-09-27, section by section;
this document goes to adversarial review, then to the operator.

Part F of the design-system iteration (A landed as
`busy-spinner-replaces-label`; B is `gallery-usability`; C, D, E and G
follow). It implements what the 2026-09-02 ruling decided and nothing
built: "partials now, pages later … `auth` stays free of HTML"
(docs/superpowers/plans/2026-09-01-design-system-v2-4.md:161-169).

## Why

Every app needs a sign-in screen on its first day, and rastrillo gives
it none. `rastrillo/auth` renders no HTML (auth/handlers.go; spec
2026-08-28 §6-v2.10), `docs/site/magic-links.md` says "the sign-in page
stays yours", and the gallery's Screens tier is copyable markup with
two bugs in it (it posts `email` where `auth.Begin` reads `address`;
it links to `/signin/other` and `/signin/reset`, which nothing serves).

So apps improvise. fichas (oficina/fichas) is the live example: a page
header, one field with "Only people this instance already knows can
sign in." under it, and a button saying "Email me a link" — a data-entry
form wearing a sign-in title. The button is also wrong: sign-in is
identifier-first, and a claimed keymail address is upgraded to
keymail's OAuth ceremony instead of a link, so the button cannot know
what it will do until the address is in. And every visit starts from
nothing: somebody who signed in with keymail yesterday types their
address again today.

fichas also had to solve, alone, a trap every keymail-enabled app hits:
the default CSP's `form-action 'self'` covers a form's whole redirect
chain, so `Begin`'s 303 to keymail's authorize URL is refused. SKILL.md
recommends widening `form-action`; fichas instead captures the 303 and
renders a page that navigates onward with a meta refresh
(internal/fichas/auth_navigation.go), which keeps the CSP strict. That
belongs in the framework.

## Decisions taken with the operator

- **Architecture:** partials in `ui` plus a data helper in `auth`;
  `auth` still renders no HTML. (Rejected: `auth` rendering pages —
  reverses the ruling; better gallery markup only — apps keep
  improvising.)
- **Doors in this cut:** identifier-first email (magic link or
  keymail) as the primary door, passkey where the app has passkeys,
  and the remembered one-tap. Password stays app-rendered: it also
  collides with `auth` on `POST /signin`, which is its own fix.
- **What is remembered:** the method and the address, in a sealed
  HttpOnly cookie that survives sign-out; "Use a different email"
  forgets it.
- **Backdrop:** a `backdrop` slot the app can fill with its own
  illustration, plus one generated, token-coloured default so no app
  ships a blank page. More patterns, a seeded choice between them, and
  illustration guidance are Part D.

## 1. Architecture

### 1.1 `auth.SigninState(r) SigninState`

A method on `*auth.Auth` that reads the request — query, cookies — and
returns plain data. No HTML, no writes.

```go
type SigninState struct {
    Step       SigninStep // Ask, Sent, Continue
    Problem    SigninProblem // "", Rate, Address, Expired, Keymail, Generic, Reauth
    Address    string // prefill: the remembered address, or none
    Remembered *Remembered // nil when nothing is remembered
    ContinueURL string // Step == Continue only; from the sealed cookie, never the query
    Passkeys   bool // the app configured passkey discovery (set by the app, see 1.4)
    Action     string // "/signin" — Begin's path, for the form
    ForgetAction string // path of the forget handler (1.3)
}
type Remembered struct {
    Method  string // "keymail", "magiclink", "passkey"
    Address string // empty for passkey sign-ins that have no address
}
```

Mapping from today's outcomes (auth/handlers.go):

| Request | Step | Problem |
|---|---|---|
| no query | Ask | — |
| `?sent=1` | Sent | — |
| `?err=rate` | Ask | Rate |
| `?err=address` | Ask | Address |
| `?err=expired` | Ask | Expired |
| `?force=1&err=keymail` | Ask | Keymail (the only state that offers `force`) |
| `?err=1` | Ask | Generic (now documented) |
| `?reauth=1` | Ask | Reauth |
| `?continue=1` with a valid continuation cookie | Continue | — |
| `?continue=1` with no/expired/tampered cookie | Ask | Expired |

Unknown query values fall back to Ask with no problem. `Sent` never
echoes an address from the query; it shows the remembered address only
if one exists, else a generic "your inbox".

### 1.2 The keymail continuation, moved into `auth`

`Begin`, on `signin.NextKeymail`, today sets the pending cookie and
303s to `next.Redirect` (auth/handlers.go:56-62). It becomes:

1. Set the pending cookie, as now.
2. Seal `next.Redirect` into a second short-lived cookie,
   `__Host-rastrillo_continue` (same TTL as pending, 10 min), with a key
   derived from `InstanceKey` (`crypto.Derive`, label
   `"rastrillo/auth/continue"`), `crypto.SealSym`.
3. 303 to `SigninPath?continue=1` — same-origin, so `form-action 'self'`
   allows it.

`SigninState` opens the cookie and returns its URL as `ContinueURL`
only if it opens, is unexpired, and re-validates as an https URL whose
path is keymail's authorize path with this app's `client_id` and
`redirect_uri` (the check fichas makes, auth_navigation.go:74-97). The
page renders a meta refresh plus a fallback link. A document navigating
is not the form's redirect chain, so the CSP stays strict.

The URL is never read from the query string: `?continue=1` is a flag,
and a forged `?continue=https://evil` has no effect. `Callback` clears
the continuation cookie along with pending. The response carrying the
continuation page is `Cache-Control: no-store` and
`Referrer-Policy: no-referrer` (the authorize URL carries state) — set
by `SigninState`'s companion `auth.SigninHeaders(w, state)` helper, which
the app calls before rendering; the partial documents the call.

Apps that widened `form-action` per SKILL.md keep working: the
continuation page just navigates onward. SKILL.md's advice changes to
this path.

### 1.3 The remembered method

- Cookie `__Host-rastrillo_last_signin` (unprefixed on plain-http
  origins, per the existing `cookieName` rule), HttpOnly, SameSite=Lax,
  Secure from Origin, Max-Age 400 days (the browser cap), sealed with a
  key derived from `InstanceKey` (label `"rastrillo/auth/remember"`).
  Payload: `{v:1, method, address}`. A cookie that fails to open, or has
  an unknown version, is ignored and cleared.
- Written by `auth.admit` once the first factor has proved the
  address and the `Authorize` gate has admitted it — before the
  `SecondFactor` hook runs — with `id.Method` (`keymail` or
  `magiclink`) and `id.Address`, the address rather than `SubjectFor`'s
  subject. Before the hook because nothing later knows the address:
  `secondfactor.Gate.Complete` holds only the subject
  (secondfactor/secondfactor.go:290). A visitor who proves the address
  and then fails the second factor is still remembered; what is
  remembered is only which door they used, which they just proved.
- Written by `passkey`'s discover finish with method `passkey`. The
  address is kept if a remembered cookie already carries one; passkey
  sign-in alone knows no address.
- Survives `Signout`.
- `POST /signin/forget` (`auth.Forget`, same-origin check as `Begin`)
  clears it and 303s to `SigninPath`. "Use a different email" is a
  button in a small form posting there, not a link — a GET that
  changes state would be prefetchable.
- New `auth.Config.Remember *bool`, default on; `false` disables
  writing and reading (for apps on shared kiosks).

### 1.4 The `signin` partial

`{{template "signin" (dict "State" .Signin "Brand" .Brand)}}`

- `State` is `auth.SigninState`. `State.Passkeys` is set by the app
  (it knows whether it mounted `passkey`); `auth` cannot know.
- `Brand`: `Name` (required), `Pitch` (optional, one line), `Mark`
  (optional `template.HTML` — an `<img>` or inline SVG the app owns).
- Renders one `<section rst-signin>` card: brand column, door column.
  New `rst-signin-*` attributes in `tokens.css`, class twins per the
  §6-v3 grammar.
- Every string is a `rastrillo.ui.signin_*` key, in all 12 base
  catalogs (`TestBaseCatalogsShareOneKeySet`).
- The passkey door is a `<button type="button" data-rst-passkey
  hidden>`; the shim reveals it and wires it to the existing
  `passkey/discover/*` flow via `webauthn.mjs`. No script, no button.

### 1.5 The `stage` shell

`ui/layouts/stage.html`, selectable by `rastrillo new --shell=stage`
and usable by any page.

- Full-viewport; the card centred on both axes; two columns (brand |
  door) from ~48rem, stacked below. Skip link as every shell.
- Blocks: `title`, `lang`, `dir`, `head` (as every shell), `backdrop`
  (default: the generated pattern, 1.6), `foot` (legal links; empty by
  default).
- 320px reflow; the card never scrolls sideways.

## 2. What the screen does

Draft wording; final strings go through copy review before they are
written into `locales/en.toml`, and the eleven translations follow.

- **First visit.** Brand on the card's start side (top on a phone).
  Door side: one email field (`name="address"`, `autocomplete="email"`,
  `type="email"`, required, autofocus) and **Continue**. Where
  `Passkeys`, a quiet **Sign in with a passkey** below. No help text
  about who may sign in: an unknown address gets the same "check your
  email" as a known one, and the screen must not contradict that.
- **Returning.** One button: **Continue to Keymail** (method keymail),
  **Continue as ‹address›** (magiclink), or **Sign in with your
  passkey** (passkey). The address shows beneath in `<bdi>`. The button
  is a POST to `Begin` carrying the address as a hidden field (keymail,
  magiclink), or the passkey button. **Use a different email** posts to
  `Forget`.
- **Sent.** "Check your email", the address if remembered, "the link
  works once and expires", **Use a different email**.
- **Continue.** "Taking you to Keymail…", meta refresh to
  `ContinueURL`, and a **Continue to Keymail** link for when it does
  not fire.
- **Problems**, as an inline callout above the door with the address
  kept in the field: Rate — try again in a few minutes; Address — that
  does not look like an email address; Expired — that link has expired
  or was used, send a new one; Keymail — keymail could not confirm it
  is you, with **Send me a link instead** (a form with the address and
  `force=1`), the only place the escape hatch appears; Generic —
  something went wrong, try again; Reauth — sign in again to continue.
- **Behaviour.** The busy rule covers Continue (Part A: spinner in
  place of the label, width kept). Nothing needs JavaScript except the
  passkey door.

## 3. The default backdrop

- Template func `backdrop(seed string) template.HTML` in `ui.Funcs`,
  emitting an inline `<svg aria-hidden="true" focusable="false">` of
  fine wavy lines and a soft radial glow, like keymail's.
- Deterministic in `seed` (the app passes its name): the seed picks
  line frequency, amplitude, angle and glow position from bounded
  ranges, so two apps differ and one app never changes between loads.
- Coloured only through CSS (`[rst-backdrop-art]` rules using
  `--rst-accent`, `--rst-line`, `--rst-bg` via `color-mix`), so it
  follows theme and scheme; no colour literal in the SVG
  (`TestTokensCSSHasNoColourLiterals` stays green and extends to the
  func's output).
- Hidden under `forced-colors: active` and in print. Under 4 KB.
  No `style` attribute (the strict CSP), no external reference.
- An app replaces it by defining the `backdrop` block — an `<img>`, a
  CSS background in its own stylesheet, or its own SVG.

## 4. Rollout in this branch

- Gallery Screens rebuilt on the real partial and shell: the five
  hand-written sign-in screens become states of `signin` rendered in a
  `stage` frame (Ask, Returning ×3, Sent, Continue, one Problem). The
  password screen stays as copyable markup with its warning, now
  explicitly "not shipped"; the dead `/signin/other` and
  `/signin/reset` links go.
- `docs/site/magic-links.md`: "The sign-in page stays yours" becomes
  "Use the shipped screen, or your own", with the wiring and the
  continuation. The outcome table gains `err=1`, `continue=1`,
  `reauth=1`.
- `docs/site/passkeys.md`: fix the stale "A passkey never signs anybody
  in from nothing" and list `discover` among the routes.
- SKILL.md: the sign-in line and the CSP advice (continuation instead
  of widening `form-action`).
- CHANGELOG: Added (signin partial, stage shell, backdrop,
  `SigninState`, `Forget`, remembered method); Changed (`Begin`'s
  keymail 303 now goes to `SigninPath?continue=1` — an app that renders
  its own page must handle `continue=1` or adopt the partial).
- fichas adopts it in its own repo as a follow-up branch; not this
  branch.

## 5. Tests

- `SigninState`: every row of the 1.1 table; unknown values; `Sent`
  never echoes a query address.
- Continuation: the URL comes only from the sealed cookie; a forged
  `?continue=<url>` yields Ask/Expired; a tampered, expired or
  foreign-key cookie yields Ask/Expired; a sealed URL failing the
  authorize-path/client_id/redirect_uri check is refused; `Callback`
  clears it; `SigninHeaders` sets no-store and no-referrer.
- Remembered: written on keymail and magiclink admit with the address
  (not the subject), including when SecondFactor then holds; not when
  `Authorize` refuses; on passkey discover; survives Signout; `Forget` clears it and
  refuses cross-origin; `Remember=false` neither writes nor reads; a
  bad cookie is ignored and cleared.
- Partial: renders every step × problem × remembered method with
  fixtures; no inline styles (`TestPartialsAndLayoutsEmitNoInlineStyles`
  covers new partial and shell); every control named; class/attribute
  twins; locale key parity.
- Backdrop: deterministic per seed, different across seeds, no colour
  literals, no `style`, under 4 KB.
- Browser drive (`-tags browser`), fake classifier and mailer: Ask →
  Sent; keymail address → continuation page → (stubbed) authorize URL
  reached with CSP `form-action 'self'` intact; after a sign-in, the
  next visit offers the one-tap and it posts the remembered address;
  "Use a different email" returns to Ask; passkey door hidden with
  scripts off, shown with them on.
- a11y: axe WCAG 2.2 AA on every screen state in every theme × scheme;
  320px reflow; keyboard walk.

## Out of scope

- A password door, and the `POST /signin` clash between `auth` and
  `password`.
- Social/OIDC sign-in (the framework has none; the gallery keeps the
  disclaimer).
- `return_to` through `RequireSession` and `admit` (noted; a separate
  fix).
- An admission pre-check before classification (fichas' `beginGuard`,
  which stops an anonymous visitor making the server probe a domain of
  their choosing). Worth upstreaming; it is a security change with its
  own review, not a screen.
- More backdrops, a seeded choice between them, illustration guidance
  (Part D). Marketing hero and inert screenshot frames (Part G).
