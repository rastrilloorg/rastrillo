# A shipped sign-in screen, and a browser that remembers how you got in

Status: design approved in conversation 2026-09-27, section by section;
revised after two rounds of adversarial review (see "Review log"),
with the operator's decisions on compatibility and provider trust.

Part F of the design-system iteration (A landed as
`busy-spinner-replaces-label`; B is `gallery-usability`; C, D, E and G
follow). It implements what the 2026-09-02 ruling decided and nothing
built: "partials now, pages later … `auth` stays free of HTML"
(docs/superpowers/plans/2026-09-01-design-system-v2-4.md:161-169).

Citations: `K/` is the keymail library,
`github.com/keymaildev/signin@v0.1.1`; `F/` is fichas'
`internal/fichas/` on `design/rulings-2026-09-19`.

## Why

Every app needs a sign-in screen on its first day, and rastrillo gives
it none. `rastrillo/auth` renders no HTML (auth/handlers.go; spec
2026-08-28 §6-v2.10), `docs/site/magic-links.md` says "the sign-in page
stays yours", and the gallery's Screens tier is copyable markup with
two bugs in it (it posts `email` where `auth.Begin` reads `address`;
it links to `/signin/other` and `/signin/reset`, which nothing serves).

So apps improvise. fichas is the live example: a page header, one field
with "Only people this instance already knows can sign in." under it,
and a button saying "Email me a link" — a data-entry form wearing a
sign-in title. The button is also wrong: sign-in is identifier-first,
and a claimed keymail address is upgraded to keymail's OAuth ceremony
instead of a link, so the button cannot know what it will do until the
address is in. And every visit starts from nothing: somebody who signed
in with keymail yesterday types their address again today.

fichas also had to solve, alone, a trap every keymail-enabled app hits:
the default CSP's `form-action 'self'` covers a form's whole redirect
chain, so `Begin`'s 303 to keymail's authorize URL is refused. SKILL.md
(:287-295) recommends widening `form-action`; fichas instead captures
the 303 and renders a page that navigates onward with a meta refresh
(F/auth_navigation.go:30-72), which keeps the CSP strict. That belongs
in the framework.

## Decisions

Taken with the operator.

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
  forgets it, by POST.
- **Backdrop:** a `backdrop` slot the app can fill with its own
  illustration, plus one generated, token-coloured default so no app
  ships a blank page. More patterns, a seeded choice between them, and
  illustration guidance are Part D.
- **Compatibility (round 1, finding 1): the continuation is opt-in.**
  `auth.Config.ContinueOnSigninPage bool`, default false. Off, `Begin`
  and `Callback` behave exactly as today: the keymail branch 303s
  straight to the authorize URL (auth/handlers.go:56-62), neither new
  cookie (§1.3) is written, the outcome URLs are unchanged. On, the keymail
  branch 303s to the sign-in page, which continues (§1.3). The partial's
  documentation says to turn it on. Nothing that exists today breaks.
- **How a forgotten switch is caught.** The operator asked for
  `rastrillo doctor` to flag an app that uses the partial without the
  switch, if that can be done soundly. It cannot: doctor reads files,
  and while it could find `{{template "signin"` in templates (a
  template action's name is a constant), the switch is a Go expression
  — `ContinueOnSigninPage: cfg.Continue`, a helper, a struct built
  elsewhere — so doctor would call a correctly wired app broken. The
  doctor part is dropped. In its place an advisory, not a detector:
  `SigninState` is the partial's only data source, and the first time
  it is called with the switch off it logs one warning, worded as a
  condition rather than a diagnosis — "ContinueOnSigninPage is off: under
  the default CSP, with no continuation of your own, a keymail address
  cannot leave the sign-in form". It cannot know whether the app widened
  `form-action` or wraps `Begin` as fichas does
  (F/auth_navigation.go:30-72), so it never says the app is misconfigured,
  and it fires once per process, not per request.
- **Provider trust (round 1, finding 5): any delegated keymail server
  by default; an optional allowlist.** That is keymail's protocol: the
  classifier follows the address's own `_keymail` DNS delegation
  (K/classify.go:244-282), so the server is chosen by whoever controls
  the address's domain — the same party that controls its MX and could
  receive a magic link anyway. A delegated server cannot vouch for
  anyone else's address: `CompleteKeymail` compares the address the
  server returns with the one the flow started for and fails with
  `ErrAddressMismatch` (K/flow.go:224-229), which `Callback` turns into
  a hard 403 (auth/handlers.go:88-94). Deployments that want a closed
  set set `auth.Config.KeymailServers []string` (host or host:port,
  compared case-insensitively). A server outside a configured list
  gets a magic link instead and never receives a token exchange; §1.4
  says where both are enforced.

## 1. Architecture

### 1.1 Configuration added to `auth.Config`

```go
// ContinueOnSigninPage: see Decisions. Default false.
ContinueOnSigninPage bool

// KeymailServers: see Decisions and §1.4. Empty means any delegated
// server.
KeymailServers []string

// BeginPath and ForgetPath are where the app mounted Begin (POST) and
// Forget (POST). auth mounts nothing itself; these are for the screen's
// form actions. Defaults "/signin" and "/signin/forget". fichas mounts
// Begin at "/auth/begin" (F/app.go:70), which is why this is not
// inferred from SigninPath (auth/auth.go:167-169).
BeginPath  string
ForgetPath string

// Remember: see §1.5. Nil means on.
Remember *bool
```

`auth.New` also stops relying on the library's default callback path:
it sets `Keymail.RedirectPath` to a package constant `callbackPath =
"/auth/callback"` (today implicit, K/keymail.go:33-39), and the
authorize-URL predicate (§1.4) reads the same constant, so the URL that
is built and the URL that is checked cannot drift.

### 1.2 `auth.SigninState` and `auth.PrepareSigninResponse`

```go
func (a *Auth) SigninState(r *http.Request) SigninState
func (a *Auth) PrepareSigninResponse(w http.ResponseWriter, st SigninState)
```

`SigninState` reads the query and four cookies (attempt, continuation,
pending, remembered) and returns plain data. It writes nothing, and it consults
nothing else: no database, no classifier, no `Authorize`, no mailer.
`PrepareSigninResponse` is the only writer, and the app calls it before
rendering any state:

- `Cache-Control: no-store` on every state. Returning and Sent pages
  carry an address; encryption and HttpOnly protect a cookie, not a
  rendered page in a shared cache or a back-forward history.
- `Referrer-Policy: no-referrer` on Continue only. The authorize URL
  carries `state` and the PKCE challenge, and the continuation is the
  page that navigates to it. Not on the other states, on purpose:
  under `no-referrer` a browser sends `Origin: null` on a form POST,
  and a browser without `Sec-Fetch-Site` then fails `csrf.SameOrigin`
  (csrf/csrf.go:25-44) — so every state with a form keeps the app's
  default policy (serve.go:447), and the Continue state has no form.
- Deletes any cookie `SigninState` found unopenable, expired, for
  another origin, of an unknown version, or (remembered only) present
  while remembering is off. `SigninState` records these in an
  unexported field of the returned value; that is how the read-only
  half tells the writing half what to clear.

```go
type SigninState struct {
    Step        SigninStep    // Ask, Returning, Sent, Continue
    Problem     SigninProblem // None, Rate, Address, Expired, Keymail, Generic, Reauth
    Address     string        // prefill for the email field: this attempt's address, else the remembered one; never the query
    SentTo      string        // Sent only: the address this attempt's link went to; "" unless bound (below)
    Remembered  *Remembered   // nil when nothing valid is remembered or remembering is off
    ContinueURL string        // Continue only; from the continuation cookie, never the query
    BeginPath   string        // Config.BeginPath
    ForgetPath  string        // Config.ForgetPath
    Passkey     *PasskeyDoor  // nil: no passkey door. Set by the app (§1.6)
    clear       []string      // cookies PrepareSigninResponse deletes
}
type Remembered struct {
    Method  string // "keymail", "magiclink", "passkey"
    Address string // "" exactly when Method is "passkey" (§1.5)
}
type PasskeyDoor struct {
    BeginPath, FinishPath string // where the app mounted DiscoverBegin/DiscoverFinish
    ModuleURL             string // where the app serves webauthn.JS()
    LegacyRPID            string // passkey.Config.LegacyRPID, if any
}
```

Mapping (checked in this order; the first that applies wins):

| Request | Step | Problem |
|---|---|---|
| `?continue=<id>`, continuation valid and bound (§1.3) | Continue | — |
| `?continue=<anything>`, otherwise | Ask | Expired |
| `?sent=1` | Sent | — |
| `?err=rate` | Ask | Rate |
| `?err=address` | Ask | Address |
| `?err=expired` | Ask | Expired |
| `?err=keymail` (Callback also sends `force=1`) | Ask | Keymail |
| `?err=1` | Ask | Generic |
| `?reauth=1` | Returning if remembered, else Ask | Reauth |
| none of the above, something remembered | Returning | — |
| none of the above | Ask | — |

An unknown `err` value is no problem at all. Every problem except Reauth
shows the Ask door, never the one-tap, so the visitor can correct what
they typed. Reauth is not an error — a fresh session is wanted — so it
shows whichever door the visitor would otherwise get.

`Address` comes from the attempt cookie (§1.3) when one is valid —
whatever this browser last typed into `Begin`, even from another tab,
because a prefill is the visitor's own input and stays editable — else
from `Remembered.Address`, else empty. `SentTo` is set only when the
attempt cookie is valid, its kind is `link`, and its id equals the
query's `attempt` value; otherwise Sent says "your inbox". Sent never
shows the remembered address: it is not evidence of where this link
went.

### 1.3 The attempt and continuation cookies

With the switch on, `Begin` writes up to two cookies beside the
existing pending cookie. Both use the existing `cookieName` rule
(auth/auth.go:274-279; unprefixed on a plain-http origin) and
`setCookie`'s attributes (:287-293: `Path=/`, HttpOnly, SameSite=Lax,
Secure from Origin).

- **The attempt cookie**, `__Host-rastrillo_attempt`: the latest thing
  this browser submitted to `Begin`, for the screen — prefill after a
  problem, and the Sent address. Written on every `Begin` that passes
  the same-origin check.
- **The continuation cookie**, `__Host-rastrillo_continue`: the pending
  cookie's sidecar, for the keymail round trip — the authorize URL and
  what binds a callback to it. Written only on the keymail branch, in
  the same response as the pending cookie, and deleted wherever the
  pending cookie is deleted.

They are separate because they have separate lifetimes. A magic-link
submission after a keymail one replaces the attempt, but the keymail
round trip is still outstanding and its callback must still be
recognised; one cookie for both would lose that (round 2, finding 19).

Both values are versioned envelopes:

```
"v1." + base64url( crypto.SealSym(K, json(payload)) )
K = crypto.Derive([]byte(InstanceKey), "rastrillo/auth/attempt/v1")    — attempt
K = crypto.Derive([]byte(InstanceKey), "rastrillo/auth/continue/v1")   — continuation
```

`SealSym` (crypto/crypto.go:296-315) is AES-256-GCM: authenticated and
confidential — the attempt carries an address. It checks nothing about
time (crypto/crypto.go:318), so expiry lives in the payload. Decoding
is strict (unknown fields refused); the cookies' Max-Age matches `exp`,
but only `exp` is trusted. Open refuses a wrong `o`, `now ≥ exp`,
`iat > now + 1 min`, and `exp − iat` over the cookie's cap.

| Attempt field | Meaning |
|---|---|
| `o` | `Config.Origin` (a key shared across instances cannot move a cookie between them) |
| `id` | 16 random bytes, base64url |
| `iat`, `exp` | Unix seconds; cap 15 min (the link TTL) |
| `k` | `link` or `problem` or `keymail` |
| `a` | the submitted address, trimmed, only if ≤ 254 bytes with no control bytes; else empty |

| Continuation field | Meaning |
|---|---|
| `o` | `Config.Origin` |
| `id` | 16 random bytes, base64url: the continuation id |
| `iat`, `exp` | Unix seconds; cap 10 min (the pending TTL, auth/auth.go:72-74) |
| `u` | the authorize URL |
| `st` | base64url SHA-256 of the URL's `state` |
| `ph` | base64url SHA-256 of the pending cookie's value, written in the same response |

**Begin, switch on:**

- Rate, bad address, other error: attempt kind `problem`; redirect as
  today (`?err=rate`, `?err=address`, `?err=1`).
- Magic link: attempt kind `link`; redirect to
  `SigninPath?sent=1&attempt=<attempt id>`. `sent=1` is unchanged, so a
  page that reads only it still works.
- Keymail: validate `next.Redirect` with the predicate (§1.4). If it
  fails — which a correct library never produces — log, set no cookies,
  redirect `?err=1`. Otherwise set the pending cookie as today, the
  continuation cookie, and attempt kind `keymail`, and 303 to
  `SigninPath?continue=<continuation id>`. Same-origin, so `form-action
  'self'` allows it.

**`AnswerAsSent(w, r)`** is `Begin`'s magic-link answer without the
magic link, for an admission wrapper that refuses an address before
`Begin` can classify it (fichas' `beginGuard`, F/auth.go:91-116). It
does what `Begin` does up to the flow — the same-origin check, the
address read with `r.FormValue` — then writes the same attempt cookie
(kind `link`, the address) and the same redirect, and sends nothing.
Without it, a wrapper that answers plain `?sent=1` gives a refused
address no attempt cookie, no `attempt=` and a "your inbox" Sent page,
while an admitted one gets all three: a membership oracle on the first
try (round 2, finding 17). The docs tell wrapper authors to use it; it
does not close the rate-limit leak fichas documents (F/auth.go:85-90),
which is the pre-check out of scope.

**`SigninState` on `?continue=<id>`** returns Continue only if all
hold: the continuation cookie opens and is unexpired; its `id` equals
the query value; the pending cookie is present and hashes to `ph`; `u`
passes the predicate again. Anything else is Ask/Expired. The URL is
never read from the query: `?continue=` carries an id that means
nothing without this browser's cookie. The `ph` binding means a
continuation dies with its pending cookie: once `Callback` has consumed
pending, going back to the continuation page gives Expired.

**Callback, switch on.** Today it clears pending the moment it reads it,
then lets the library find a state mismatch (auth/handlers.go:74-86) —
so tab A's late callback destroys tab B's newer attempt. With the switch
on, between reading pending and clearing it:

- If the continuation cookie opens, its `ph` equals the hash of the
  pending cookie just read, and the SHA-256 of the callback's `state`
  does not equal `st`, the callback belongs to some other attempt:
  redirect `?err=expired` and leave pending and continuation untouched,
  so the attempt they describe can still finish. The same holds for a
  callback URL a third party makes the browser open: today that throws
  away the visitor's pending attempt; with this it does not. Nothing is
  refreshed, so the pending blob's own authenticated expiry
  (K/pending.go:85) still bounds it.
- Otherwise — no continuation cookie, or one whose `ph` does not match
  (left over from an earlier attempt, or from a `Begin` run with the
  switch off) — exactly today's path: clear pending and continuation,
  complete, admit.

The attempt cookie plays no part in the callback, so a later magic-link
or problem submission neither blocks a keymail callback nor removes its
protection. `admit` deletes the attempt cookie once a first factor is
verified (before `Authorize`), since the attempt is over either way.

**What the cookies guarantee, and what they do not.** Supersession is
"the last cookie pair the browser installed", not "the last attempt
started": if two `Begin` responses arrive out of order, the older
attempt's pair wins. Either way, no tab continues or completes an
attempt other than the one the installed pair describes, and the other
tab ends at Expired rather than at somebody else's authorize URL. What
cookies cannot do: a callback that passed the check and is still in
flight when a newer `Begin` lands will, on its response, delete the
newer pending cookie (a cookie is deleted by name, unconditionally);
that newer attempt then ends at Expired and the visitor starts again.
Clearing a cookie is not server-side single use either: a pending value
copied out of the browser can be presented again until its authenticated
10-minute expiry, and completing still needs an authorization code
bound to that attempt's PKCE verifier, which the provider issues once.
That is the replay boundary. This spec adds no server-side attempt
store; the screen does not need one.

**The page.** Continue renders a zero-delay meta refresh,
`<meta http-equiv="refresh" content="0;url=…">` (fichas does the same,
F/templates/signin.html:8), a heading, one sentence and a fallback link
to the same URL. A document navigating is not the form's redirect
chain: `defaultCSP` (serve.go:428-430) has no `navigate-to` or
`sandbox`, and `form-action` governs form submissions, not a document's
refresh. The meta element sits in the card, not the head. HTML's
content model wants http-equiv meta in `<head>`, but browsers apply the
refresh wherever the element is inserted, and this is how fichas runs
in production. The browser test in §5 proves it for Chromium; Firefox
and Safari are checked by hand before merge (§5).

### 1.4 The authorize-URL predicate and the allowlist

`validAuthorizeURL(raw string) bool`, used by `Begin` before sealing and
by `SigninState` before returning `ContinueURL`. It is fichas' predicate
(F/auth_navigation.go:79-99) made stricter, with every expected value
taken from the configuration that built the URL (auth/auth.go:249-261;
K/keymail.go:44-57):

- `url.Parse` succeeds; scheme `https` (auth builds
  `Base: "https://" + server`, auth/auth.go:252); host non-empty; no
  userinfo; no fragment; `Path == "/oauth/authorize"`; `RawPath` empty.
- `url.ParseQuery(RawQuery)` succeeds, with no key other than
  `client_id`, `redirect_uri`, `scope`, `state`, `code_challenge`,
  `code_challenge_method` and `prompt`.
- The first six each present exactly once and non-empty; `prompt` absent
  or exactly once and equal to `login`.
- `client_id` equals `Config.Origin` with any trailing slash removed;
  `redirect_uri` equals that + `callbackPath`; `scope` is `identify`;
  `code_challenge_method` is `S256`; `state` and `code_challenge` are
  43 base64url characters (K/keymail.go:133-145).
- If `KeymailServers` is set, the host (lowercased) is in it.

**Where the allowlist is enforced.** Two places, both HTTP clients,
because the library talks to a keymail server twice.

1. *Classification.* The library classifies and acts on the result
   inside `Flow.Begin` (K/flow.go:146-151): there is no hook between
   them, and `Flow.Keymail` runs after the pending blob is sealed and
   cannot decline (K/flow.go:163-172). The seam that sits before the
   result exists is the classifier's HTTP client: both probes — the
   well-known document (K/classify.go:197) and the federation lookup
   (:151) — go through `Classifier.HTTP` (:86-91, :285-290), and any
   failure makes the address "not keymail" (:146-155, :197), which is
   the magic link.
2. *Token exchange.* `CompleteKeymail` builds a second client from the
   pending blob's server (K/flow.go:220) and posts the code and verifier
   through `Keymail.HTTP` (K/keymail.go:99-109), which `auth.New` leaves
   nil today (auth/auth.go:251-253) — an unrestricted default client.

With `KeymailServers` set, `auth.New` builds one guarded transport and
gives it to both: the classifier's client (keeping its 5-second timeout,
K/classify.go:86-91) and every `Keymail` the flow's `Keymail` func
returns (keeping the 15-second timeout, K/keymail.go:105-108). The
transport refuses any request that is not `https` or whose host
(lowercased, host[:port]) is not listed. `http.Client` calls the
transport for every redirect hop, so a listed server cannot redirect a
probe or an exchange — code and verifier included — to an unlisted host
or down to plain http. Consequences:

- An unlisted server is never probed, and its addresses get a magic
  link.
- A pending cookie issued before the list was tightened reaches a server
  now unlisted only as a refused exchange, which `Callback` already
  answers with `?force=1&err=keymail` (auth/handlers.go:95-98) — the
  escape hatch, not a dead end.
- The list is copied at `New` and never changes for the process's life.
  The classifier caches per address and per domain (K/classify.go:
  107-140, :158-230), and a fixed list installed before first use makes
  those caches consistent with it; changing the list means a restart,
  which also empties them.

The predicate's host check is a third line. The allowlist applies with
the switch off too: it is policy about which servers are trusted, not
about how the browser gets there. Without a list, auth leaves both
clients exactly as today. (Rejected: calling `Flow.Begin` again with
`force` after an unlisted keymail answer — it spends each rate budget
twice and can fail on the second spend.)

### 1.5 The remembered method: one shared component

A new leaf package, `rastrillo/lastsignin`, owns the cookie, so `auth`
and `passkey` write it the same way and `passkey` never needs
`InstanceKey` or `auth`'s keymail dependency:

```go
func New(Config) (*Jar, error) // Config{Origin, InstanceKey string; Disabled bool; Now func() time.Time}
func (j *Jar) Write(w http.ResponseWriter, rec Record)      // Disabled: deletes the cookie instead
func (j *Jar) Read(r *http.Request) (Record, ReadResult)    // Absent | Valid | Invalid; Disabled: never Valid
func (j *Jar) Clear(w http.ResponseWriter)
type Record struct{ Method, Address string }
```

- The jar derives its own key: `crypto.Derive([]byte(InstanceKey),
  "rastrillo/lastsignin/v1")`. `auth.New` builds the one jar from its
  own `InstanceKey`, `Origin` and `Remember`, and exposes it as
  `a.RememberJar()`. The app hands that to `passkey.Config.Remember`
  (new field, `*lastsignin.Jar`). The partial's docs show the wiring.
- Cookie `__Host-rastrillo_last_signin` (the `cookieName` rule),
  HttpOnly, SameSite=Lax, `Path=/`, Secure from Origin, Max-Age 400
  days (the browser cap). Value `"v1." + base64url(SealSym(key,
  json({o, m, a, iat, exp})))`, with `exp = iat + 400 days`. Open
  refuses a wrong origin, `now ≥ exp`, `iat > now + 1 min`, an unknown
  method, a keymail or magiclink record without a valid address (≤ 254
  bytes, one `@`, no control bytes), and a passkey record with one.
  Every sign-in rewrites it, so the 400 days run from the latest one.
- **Written by `auth.admit`** after `Authorize` admits and `SubjectFor`
  succeeds, before the `SecondFactor` hook: `{id.Method, id.Address}` —
  the address, not the subject. Before the hook because nothing later
  knows the address (`secondfactor.Gate.Complete` holds only the
  subject, secondfactor/secondfactor.go:290-303). So a sign-in held for
  a second factor is remembered, and so is one whose second factor then
  fails or whose session insert fails: the record says which door this
  browser used and proved, nothing more. Not written when `Authorize`
  refuses or `SubjectFor` errors.
- **Written by `passkey.DiscoverFinish`** when its `Remember` is set,
  after its `Authorize` check (passkey/passkey.go:428-431) and before
  `Gate.Hold` or `Sessions.SignIn` — so on the held path too:
  `{"passkey", ""}`. **The address is cleared.** Discovery knows a
  subject, not an address, and `SubjectFor` may make subjects opaque
  (auth/auth.go:110-139); keeping the old address would label Bob's
  passkey sign-in with Alice's address. A failed assertion writes
  nothing. `passkey` without a jar writes nothing, and the docs say what
  that costs: a previously remembered address stays on the screen after
  a passkey sign-in.
- Survives `Signout`.
- **`Remember = false`:** `Write` deletes any existing cookie, `Read`
  reports nothing valid, `SigninState` marks a present cookie for
  `PrepareSigninResponse` to delete. So switching it off also forgets
  what was remembered before.
- **`auth.Forget`**, mounted by the app at `ForgetPath`: refuses any
  method but POST (405) — a state change on GET would be prefetchable —
  and refuses a cross-origin submission with `a.sameOrigin`
  (auth/csrf.go:12-14), as `Begin` does. It clears the remembered and
  attempt cookies and 303s to `SigninPath`. "Use a different email" is a
  button in a small form posting there.
- **What it is never evidence of.** The remembered cookie supplies no
  identity, admission, freshness or second-factor proof. Nothing reads
  it but `SigninState` and the jar. The one-tap posts its address to
  `Begin` exactly as if typed: classified, rate-limited and proved
  afresh. A test holds this (§5).

### 1.6 The `signin` partials

```
{{define "title"}}{{template "signin-title" (dict "State" .Signin "Brand" .Brand)}}{{end}}
{{template "signin" (dict "State" .Signin "Brand" .Brand)}}
```

- `State` is `auth.SigninState`. `State.Passkey` is set by the app, which
  knows whether and where it mounted `passkey`; `auth` cannot.
- `Brand`: `Name` (required), `Pitch` (optional, one line), `Mark`
  (optional `template.HTML` — an `<img>` or inline SVG the app owns).
- `Preview` (optional bool): gallery use only. Drops the meta refresh
  and points the Continue link at `#`, so a gallery page showing the
  Continue state does not navigate away.
- `signin` renders one `<section rst-signin>` card: brand column, door
  column. New `rst-signin-*` attributes in `tokens.css`, class twins
  per the §6-v3 grammar. `signin-title` renders the document title for
  the state (§2).
- Every string is a `rastrillo.ui.signin_*` key, in all 12 base
  catalogs (`TestBaseCatalogsShareOneKeySet`).

**The passkey door.** Present only when `State.Passkey` is non-nil:

```html
<button type="button" rst-btn data-rst-passkey hidden
  data-rst-passkey-begin="…" data-rst-passkey-finish="…"
  data-rst-passkey-module="…" data-rst-passkey-legacy-rpid="…"
  data-rst-passkey-cancelled="{{T …}}" data-rst-passkey-failed="{{T …}}"
  aria-describedby="rst-signin-passkey-msg">…</button>
<p id="rst-signin-passkey-msg" rst-signin-passkey-msg aria-live="polite"></p>
```

The enhancer is new code in `rastrillo.js`, which every shell already
loads (ui/layouts/column.html:8); apps with an older vendored copy get
it through `rastrillo doctor --fix`. The protocol:

1. **Capability.** Reveal the button only when `PublicKeyCredential`
   exists, a dynamic `import()` of `data-rst-passkey-module` succeeds
   (same origin, allowed by `default-src 'self'`), and the module's
   `available()` is true (webauthn/js/webauthn.mjs:34-36). Any failure
   leaves it hidden. Revealing never moves focus. `tokens.css` gains
   `[rst-btn][hidden] { display: none }`, because the button's own
   `display` would otherwise beat the UA's `[hidden]` rule.
2. **Click.** Ignore the click if the button is already busy. Mark it
   busy the way Part A draws it — `aria-busy="true"` and an
   `rst-spin` child, which is what tokens.css keys the spinner on
   (ui/tokens.css:310, :356) — plus `aria-disabled="true"`, but never
   `disabled`: Part A's submit rule disables the button
   (ui/rastrillo.js:244), and a disabled button drops focus, which the
   failure path below promises to keep. Clear the message, `POST`
   `begin` with `fetch` (same origin, so `csrf.Protect` sees
   `Sec-Fetch-Site: same-origin`); the answer is `{"challenge"}` only
   (passkey/passkey.go:482-499). Call `authenticate({challenge, rpId:
   location.hostname, legacyRpId})`.
3. **Adapt.** `authenticate` returns `credentialId` (webauthn.mjs:
   110-116); `assert` decodes `id` (passkey/passkey.go:344-350). The
   enhancer posts `{id: credentialId, clientDataJSON, authenticatorData,
   signature}` to `finish` and never sends `prf`.
4. **Success.** `{"ok": true, "to"}`, with or without `"pending"` (the
   held path, passkey/passkey.go:450): navigate to `to` only if it has
   no control character or `\` and `new URL(to, location.href).origin
   === location.origin`; else to `/`. A prefix test is not enough:
   `"/\t/evil.example/x"` starts with `/`, not `//`, and still parses to
   another host, because URL parsing strips tabs and newlines. The
   server's `SafeReturn` already refuses those (sessions/sessions.go:
   461-469); this is the second line.
5. **Failure.** `authenticate` throws "no passkey was offered" when the
   person dismissed the prompt or had none — deliberately
   indistinguishable (webauthn.mjs:101, :131-135). That shows the
   `cancelled` text ("No passkey was used. Try again, or use your
   email."). Every other failure — a non-2xx from either endpoint, a
   network error, any other exception — shows the `failed` text. Both
   are localized strings from the data attributes, written into the
   `aria-live="polite"` paragraph the button describes; the busy marks
   come off, and focus, which never left the button, stays there.
6. **Fallback.** Wherever the passkey door appears, the email form is on
   the same screen, visible, with or without scripts. The passkey door
   is never the only way in.

### 1.7 The `stage` shell

`ui/layouts/stage.html`, added to `layoutNames` (ui/ui.go:388) and so to
`rastrillo new --shell` and `TestLayoutsParseAndRender`'s list
(ui/ui_test.go:2832-2854), which it must pass: parses with the ui funcs,
executes against nil data, emits `CONTENT-SENTINEL`, both stylesheets,
`id="main"` and the skip link.

- Full-viewport; the card centred on both axes; two columns (brand |
  door) from ~48rem, stacked below. Skip link as every shell.
- `{{template "content" .}}` inside `<main rst-page id="main">`, like
  every shell; the page's `signin` partial goes there.
- Blocks: `title`, `lang`, `dir`, `head` (as every shell); `backdrop`,
  default `{{stageArt "rastrillo"}}` — a constant seed, because no shell
  block may read the data (ui/ui.go:406-408) — which an app redefines
  as `{{define "backdrop"}}{{stageArt "fichas"}}{{end}}` for its own
  pattern, or with its own art; `foot` (legal links; empty by default).
- 320px reflow; the card never scrolls sideways.
- How a page uses it: the app's page sets are one template per page,
  each parsing `layout.html` with the page file (cmd/rastrillo/new.go:
  575-598). The sign-in page's set parses `ui.Layout("stage")` (or a
  copy) instead of `layout.html`. `--shell=stage` exists because every
  shipped shell is selectable, and suits a one-screen app; it is not
  the usual route.

## 2. What the screen does

Draft wording; final strings go through copy review before they are
written into `locales/en.toml`, and the eleven translations follow.

Every state has one `<h1>` and a matching title from `signin-title`:

| State | `<h1>` | `<title>` |
|---|---|---|
| Ask, Returning | Sign in to ‹Name› | Sign in — ‹Name› |
| any Problem except Reauth | Sign in to ‹Name› | Problem: sign in — ‹Name› |
| Sent | Check your email | Check your email — ‹Name› |
| Continue | Taking you to Keymail | Taking you to Keymail — ‹Name› |

- **Ask.** Brand on the card's start side (top on a phone). Door side:
  one email field (`name="address"`, `autocomplete="email"`,
  `type="email"`, required, prefilled with `State.Address`) and
  **Continue**, posting to `BeginPath`. With a passkey door, **Sign in
  with a passkey** below it. No help text about who may sign in (see
  "Enumeration" below).
- **Returning.** Keymail or magiclink: one primary button, **Continue**,
  posting to `BeginPath` with the remembered address as a hidden field;
  the address shows beneath in `<p id="rst-signin-remembered">…<bdi>…
  </bdi></p>` ("as ‹address›"), and the button has
  `aria-describedby="rst-signin-remembered"`, so a screen reader hears
  the address with the action. The label is the Ask form's own
  **Continue**, not "Continue with Keymail" or "Email me a link": the
  one-tap re-runs `Begin`, which classifies afresh (K/flow.go:146-151),
  so the method used last time does not decide the method this time — a
  label that promised it would be the misleading button "Why" describes
  (round 2, finding 22). The remembered method matters for one thing on
  this screen: passkey or email. **Use a different email** posts to
  `ForgetPath`. Passkey: the passkey door and the Ask form together
  (§1.6 step 6).
- **Sent.** "Check your email", then "We sent a link to ‹SentTo›" if
  bound, else "We sent a link to your inbox"; "the link works once and
  expires"; **Use a different email** (posts to `ForgetPath`).
- **Continue.** Redirect-only content: the heading, "Taking you to
  Keymail to confirm it's you.", the zero-delay meta refresh, and a
  **Continue to Keymail** link to the same URL. No form (§1.2), no other
  controls.
- **Problems**, as a callout above the door, with the Ask form below it
  prefilled from `State.Address`: Rate — try again in a few minutes;
  Address — that does not look like an email address; Expired — that
  link has expired or was used, send a new one; Keymail — Keymail could
  not confirm it is you, and the form's button becomes **Send me a link
  instead** with a hidden `force=1` (the only place the escape hatch
  appears, and it works whether or not an address was kept); Generic —
  something went wrong, try again; Reauth — an info callout, "Sign in
  again to continue", above the Returning or Ask door.

**Focus and announcement.** One matrix; the first row that applies
wins.

| State | Autofocus | Announced by |
|---|---|---|
| Address problem | the email field, whose error it describes | focus: label, invalid, description |
| Rate, Expired, Keymail, Generic | the callout (`tabindex="-1"`) | focus: the callout's text |
| Reauth, Returning keymail/magiclink | the **Continue** one-tap | focus; the info callout is read in order |
| Reauth, Ask; Ask | the email field | focus; the info callout, if any, is read in order |
| Returning passkey, Sent, Continue | nothing | the new title |

- Address problem: the message is the field's own error (`aria-invalid`,
  `aria-describedby`, ui/partials/field.html:20-26), rendered without
  `role="alert"`. The `field` partial emits that role unconditionally
  today (:22); it gains an optional `QuietError bool` that omits it. A
  message present at load that focus already lands on needs no live
  announcement, and one would race the focus announcement.
- Other error problems: the callout is not `role="alert"` for the same
  reason. The `callout` partial (ui/partials/callout.html) gains two
  optional keys, `ID string` and `Focus bool`; `Focus` emits
  `tabindex="-1" autofocus`. Both are additive; existing callers are
  unchanged.
- Autofocus is a request, not a guarantee: a browser skips it when
  something already has focus, when the URL has a fragment, or after the
  person has interacted (HTML's autofocus rules). The matrix says where
  focus starts on an ordinary load; the order of the markup — callout
  above the door — is what holds when autofocus does not run.
- The only live region is the passkey message (§1.6).
- Busy: the Part A rule covers every submit button (spinner in place
  of the label, width kept). The passkey button keeps its own busy
  marks (§1.6 step 2). Nothing needs JavaScript except the passkey door.

**Enumeration.** The screen adds no membership oracle: every state is
computed from the query and this browser's own cookies, and nothing on
the screen depends on whether an address is known, admitted, keymail or
deliverable. An admission wrapper in front of `Begin` keeps that true
only if it answers a refusal with `AnswerAsSent` (§1.3), which makes
the cookies, the redirect and the rendered Sent page the same as for a
sent link. The screen does not make the surrounding flow
enumeration-proof. Classification changes the outcome for keymail
addresses before `Authorize` runs (K/flow.go:146-151), and fichas
documents that its admission guard plus the per-address rate limit leaks
after five tries (F/auth.go:85-90). Both exist today; the admission
pre-check that would address them is out of scope.

## 3. The default backdrop

- Template func `stageArt(seed string) template.HTML` in `ui.Funcs`
  (ui/funcs.go), emitting an inline `<svg rst-stage-art
  aria-hidden="true" focusable="false">` of fine wavy lines and a soft
  radial glow, like keymail's. Named `stageArt`, and styled through
  `rst-stage-art`, not "backdrop": `rst-backdrop` is already the modal
  overlay's attribute (ui/tokens.css:1439; the scroll lock at :123),
  and a second meaning would mislead the next reader.
- Deterministic in `seed`: it picks line frequency, amplitude, angle
  and glow position from bounded ranges, so two apps differ and one app
  never changes between loads. An empty seed is the constant default.
- Coloured only through CSS (`[rst-stage-art]` rules using
  `--rst-accent`, `--rst-line`, `--rst-bg` via `color-mix`), so it
  follows theme and scheme. No colour literal, no `style` attribute,
  no `<style>`, no external reference in its output.
- Hidden under `forced-colors: active` and in print. Under 4 KB.
- An app replaces it by defining the `backdrop` block — an `<img>`, a
  CSS background in its own stylesheet, or its own SVG.

## 4. Rollout in this branch

- Gallery Screens rebuilt on the real partial and shell: the
  hand-written sign-in screens become states of `signin` rendered in a
  `stage` frame (Ask, Returning ×3, Sent bound and unbound, Continue,
  Address and Keymail problems). Continue uses `Preview`. The password
  screen stays as copyable markup with its warning, now explicitly
  "not shipped"; the dead `/signin/other` and `/signin/reset` links go.
- `docs/site/magic-links.md`: "The sign-in page stays yours" becomes
  "Use the shipped screen, or your own", with the wiring (both config
  paths, `SigninState` + `PrepareSigninResponse`, the `stage` page set,
  `RememberJar` into passkey), the switch, the allowlist and the trust
  argument from "Decisions", and for an app with an admission wrapper,
  `AnswerAsSent` and why a plain `?sent=1` is an oracle. The outcome
  table gains `err=1`,
  `continue=<id>`, `sent=1&attempt=<id>` and `reauth=1`.
- `docs/site/passkeys.md`: fix the stale "A passkey never signs anybody
  in from nothing", list `discover` among the routes, document
  `Config.Remember`.
- SKILL.md (:287-295): keymail apps set `ContinueOnSigninPage`; widening
  `form-action` is only for a hand-built page with the switch off. The
  "only listed servers work" sentence becomes `KeymailServers`. Within
  the byte budget by trimming, per AGENTS.md.
- CHANGELOG: Added (`signin`/`signin-title` partials, `stage` shell,
  `stageArt`, `SigninState`, `PrepareSigninResponse`, `Forget`,
  `AnswerAsSent`, the `field` partial's `QuietError`, the `callout`
  partial's `ID` and `Focus`,
  `ContinueOnSigninPage`, `KeymailServers`, `BeginPath`, `ForgetPath`,
  `Remember`, `lastsignin`, `passkey.Config.Remember`). Changed: auth
  now writes the remembered cookie by default — set `Remember` to false
  to stop it and delete existing ones. Nothing else changes for an app
  that leaves the switch off.
- fichas adopts it in its own repo as a follow-up branch; not this
  branch.

## 5. Tests

- **SigninState:** every row of the §1.2 table, including precedence
  when several parameters are present; unknown values; `Address` never
  from the query; `SentTo` only when bound, never the remembered
  address. Sent consults nothing: built with an `Authorize` that fails
  the test if called, a closed `*sql.DB` and a classifier transport that
  fails the test, `?sent=1` still yields Sent. The one-time warning
  fires with the switch off and never with it on.
- **PrepareSigninResponse:** no-store on every state; no-referrer on
  Continue and on no other state; deletes exactly the cookies marked.
- **Envelopes** (attempt and continuation each): round trip; refused
  when tampered, sealed under another key (including each other's),
  for another origin, expired (payload `exp`, with the cookie still
  sent), `iat` in the future, over its cap, unknown version prefix,
  unknown field.
- **Continuation:** switch off, `Begin`'s keymail answer is today's 303
  to the authorize URL, byte for byte, and neither new cookie is set.
  Switch on: 303 to `?continue=<id>`; forged `?continue=https://evil`
  and a wrong id yield Ask/Expired; a missing or different pending
  cookie yields Expired; replay after expiry yields Expired; a URL
  failing the predicate is refused at `Begin` (no cookies set) and at
  `SigninState`.
- **Two tabs** (the installed pair is what counts, §1.3): A's pair
  installed, then B's — A's continuation → Expired; A's callback (state
  A) → `?err=expired` with B's pending and continuation untouched, then
  B's callback completes. B's pair installed, then A's (responses out of
  order) — the mirror image: B is Expired, A completes. Keymail A,
  keymail B, then a magic link C: A's stale callback still leaves B's
  pending in place (the attempt cookie changing kind does not remove the
  protection), and B completes. A continuation cookie whose `ph` does
  not match the pending cookie (left from an earlier attempt, or a
  switch-off `Begin` in between) does not block the current callback. A
  third-party callback URL leaves the pending cookie in place.
- **Predicate:** table-driven — each rule in §1.4 violated once,
  including a duplicated parameter, an extra parameter, `prompt=none`,
  userinfo, a fragment, an encoded path, `http`, the wrong
  `redirect_uri`; the allowlist admits a listed host and refuses an
  unlisted one.
- **Allowlist:** with `KeymailServers` set, an address delegating to an
  unlisted server is never probed (the fake transport records nothing
  for it) and gets `?sent=1`; a listed one gets the keymail branch; a
  listed server whose probe or token endpoint redirects to an unlisted
  host, or to plain http, is refused at the hop (probe: magic link;
  exchange: `?force=1&err=keymail`, and the unlisted host receives no
  request); a pending cookie for a server removed from the list ends at
  the exchange the same way; both clients keep their timeouts.
- **AnswerAsSent:** a wrapper that refuses with it and one that passes
  to `Begin` for a magic link give the same status, the same
  `Set-Cookie` names and attributes, the same redirect shape and the
  same rendered Sent page (with the address), for the same input;
  `AnswerAsSent` sends no mail and consults no classifier.
- **Remembered:** written on keymail and magiclink admit with the
  address (not the subject), including when SecondFactor then holds;
  not when `Authorize` refuses or `SubjectFor` errors; passkey discover
  writes `{passkey, ""}` over a remembered address, on the minted and
  the held path, and nothing on a failed assertion; survives Signout;
  `Forget` clears both cookies, refuses GET and cross-origin;
  `Remember=false` deletes an existing cookie on the next sign-in and on
  the next sign-in page, and never reads one; a bad, expired or
  wrong-origin cookie is ignored and cleared; a request carrying only a
  valid remembered cookie is refused by `RequireSession`.
- **Partial:** renders every step × problem × remembered method with
  fixtures; the heading/title table; autofocus exactly per the §2
  matrix, one element at most; no `role="alert"` anywhere in the
  rendered screen; the one-tap's label is **Continue** for both email
  methods and its `aria-describedby`; the Keymail problem posts `force=1`;
  Continue has no `<form>`; `Preview` emits no `http-equiv`; no inline
  styles (`TestPartialsAndLayoutsEmitNoInlineStyles` covers the new
  partials and shell); every control named; class/attribute twins;
  locale key parity.
- **Stage and art:** `TestLayoutsParseAndRender` with `stage` in the
  list; `stageArt` deterministic per seed, different across seeds,
  under 4 KB, and its **rendered** output checked for `style`,
  `<style`, colour literals and external references — the existing
  inline-style test scans template source (ui/ui_test.go:3366-3385) and
  would not see generated SVG. The art's CSS colours are included in
  the contrast test's non-text checks where they sit behind the card's
  edge.
- **Browser** (`-tags browser`, Chromium via the harness; fake
  classifier and mailer; the keymail host intercepted with CDP's Fetch
  domain so nothing real is contacted), scripts on and off:
  Ask → Sent showing the typed address; keymail address → `POST` →
  303 → continuation document → provider request observed, with the
  response's CSP still `form-action 'self'` and no CSP violation
  reported; the fallback link reaches the same URL. After a sign-in,
  the next visit offers the one-tap and it posts the remembered
  address; "Use a different email" returns to Ask. Passkey door hidden
  with scripts off, shown with them on; with the virtual authenticator
  (as webauthn/browser_test.go does) a discover sign-in lands and the
  next visit is Returning (passkey) with no address; a cancelled prompt
  shows the cancelled message in the live region with focus still on
  the button, which was never `disabled`; a second click while busy
  starts no second ceremony. The navigation check (a node test, the way
  select.js is tested, ui/select_test.go:94) refuses `//evil`, `/\t/evil`, `/\n/evil`,
  `/\\evil` and an absolute URL, and accepts a same-origin path.
- **By hand before merge,** recorded in the branch description: the
  continuation in Firefox and Safari (Safari enforces `form-action`
  across intermediate GETs, F/auth_navigation.go:13-18); VoiceOver and
  NVDA on Ask, an error problem, Returning and the passkey failure.
- **a11y:** axe WCAG 2.2 AA on every screen state in every theme ×
  scheme; 320px reflow; keyboard walk.

## Out of scope

- A password door, and the `POST /signin` clash between `auth` and
  `password`.
- Social/OIDC sign-in (the framework has none; the gallery keeps the
  disclaimer).
- `return_to` through `RequireSession` and `admit` (noted; a separate
  fix).
- An admission pre-check before classification (fichas' `beginGuard`,
  F/auth.go:68-120, which stops an anonymous visitor making the server
  probe a domain of their choosing, and the rate-limit ordering that
  leaks membership). Worth upstreaming; a security change with its own
  review, not a screen.
- More backdrops, a seeded choice between them, illustration guidance
  (Part D). Marketing hero and inert screenshot frames (Part G).

## Review log

### Round 1 (Astra, 2026-09-27) — not ready; 2 Blockers, 13 Important, 1 Minor

| # | Finding | Resolution |
|---|---|---|
| 1 | Blocker: custom sign-in pages stop completing keymail | Opt-in `ContinueOnSigninPage`, default off; off is today's behaviour exactly. Doctor check found unsound and replaced by a one-time runtime warning (Decisions) |
| 2 | Blocker: continuation expiry unenforceable | Versioned `v1.` envelopes with authenticated `iat`/`exp`, origin and ids; the continuation also carries state and pending hashes (§1.3) |
| 3 | Two tabs continue the wrong attempt; stale callback destroys the newer one | Id-bound `?continue=`; pending-hash binding; Callback leaves the attempt the installed pair describes intact on a state mismatch (§1.3). Reworked in round 2 (#19, #20) |
| 4 | URL validation weaker than fichas' | Full predicate, stricter than fichas', from the building config; checked before sealing and before rendering (§1.4) |
| 5 | Continuation removes the CSP's host restriction | Operator: any delegated server by default, with the trust argument cited; optional `KeymailServers`, enforced in the classifier's transport — and, after round 2 (#18), the exchange's (Decisions, §1.4) |
| 6 | Submitted address lost before error recovery | Attempt cookie keeps it for 10–15 min; Sent shows an address only when bound to the attempt (§1.2, §1.3) |
| 7 | Passkey sign-in keeps someone else's address | Passkey writes `{passkey, ""}` (§1.5) |
| 8 | Passkey cannot write the cookie or honour Remember | `lastsignin.Jar` owns key and policy; `passkey.Config.Remember`; held, failed and completed paths defined (§1.5) |
| 9 | Read-only helper vs cookie cleanup; cache scope | `SigninState` read-only; `PrepareSigninResponse` clears and sets no-store on all states, no-referrer on Continue (§1.2) |
| 10 | Remembered cookie lacks lifetime and trust contract | Authenticated `iat`/`exp`, origin, method/address rules, deletion when disabled, never evidence of anything (§1.5) |
| 11 | Handler paths cannot be inferred | `BeginPath`, `ForgetPath` config; `PasskeyDoor` paths set by the app (§1.1, §1.2) |
| 12 | Passkey shim has no protocol | Six-step protocol: delivery, capability, `credentialId`→`id`, navigation, localized announced errors, email always present (§1.6) |
| 13 | Accessibility stops at automated checks | Headings/titles per state, focus rules, error association, button description, manual screen-reader check (§2, §5) |
| 14 | Meta refresh timing unstated | Zero delay, redirect-only content, fallback link, no form; browser test of the whole chain, scripts on and off (§1.3, §2, §5) |
| 15 | Enumeration claim too broad | Narrowed to "adds no membership oracle", with a test that Sent consults nothing (§2, §5) |
| 16 | Shell and backdrop contracts incomplete | `content` slot, constant nil-safe seed, registered in `layoutNames` and its test, rendered-SVG test, `Preview` for the gallery; renamed away from `rst-backdrop` (§1.7, §3) |

### Round 2 (Astra, 2026-09-27) — not ready; no Blockers, 6 Important, 2 Minor

Re-verdict of round 1: 1, 2, 4, 6–11, 14 and 16 resolved; 3, 5, 12 and
13 partly resolved; 15 not resolved. The gaps are the new findings
below, all fixed in this revision. No Blockers, so no round 3.

| # | Finding | Resolution |
|---|---|---|
| 17 | Important: an admission wrapper answering plain `?sent=1` is a first-try membership oracle once `Begin` writes an attempt cookie | `AnswerAsSent` gives a refusal the same cookie, redirect and page as a sent link; docs and a composed test (§1.3, §2, §5) |
| 18 | Important: allowlist covers classification, not token exchange | The same guarded transport (https only, listed hosts, every redirect hop) on the exchange clients; immutable list; timeouts kept (§1.4) |
| 19 | Important: stale-callback protection lost after a link or problem attempt; guard not bound to the pending cookie it protects | Split into an attempt cookie (screen) and a continuation cookie (the pending cookie's sidecar); the guard applies only when the sidecar's `ph` matches the pending cookie (§1.3) |
| 20 | Important: "newest supersedes" and "single-use" overstate cookies | Stated as "the last installed pair"; in-flight deletion limit and the real replay boundary (pending expiry + provider's single-use code) written down; tests per response order (§1.3, §5) |
| 21 | Minor: `/`-prefix navigation check passes `/\t/evil` | Parse against `location.href` and compare origins; refuse control characters and `\` (§1.6) |
| 22 | Important: remembered-method labels promise a method `Begin` re-decides | One-tap label is **Continue** for both email methods; the address is its description (§2) |
| 23 | Important: focus rules conflict (`field`'s `role="alert"`, Reauth, disabled busy button) | One focus matrix with Reauth precedence; `field` `QuietError`; `callout` `ID`/`Focus`; passkey busy via `aria-busy`/`aria-disabled`, never `disabled`; autofocus described as a request (§1.6, §2) |
| 24 | Minor: runtime warning is not a sound detector | Reworded as a conditional advisory that never calls the app misconfigured (Decisions) |
