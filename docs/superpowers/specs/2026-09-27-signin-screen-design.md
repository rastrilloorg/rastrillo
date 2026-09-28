# A shipped sign-in screen, and a browser that remembers how you got in

Status: design approved in conversation 2026-09-27, section by section;
revised after four rounds of adversarial review (see "Review log"),
with the operator's decisions on the opt-in, provider trust, the
one-tap's labels and the advisory.

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
- **One opt-in for the screen (round 1, finding 1; operator, after
  round 3).** `auth.Config.SigninScreen bool` means "this app renders
  the shipped sign-in screen", and it turns on everything the screen
  needs from `auth`: the continuation and its cookies (§1.3), the
  attempt cookie, remembering (§1.5) and `AnswerAsSent`'s parity (§1.3).
  Default false. Off, an app sees no change at all: `Begin` and
  `Callback` behave exactly as today — the keymail branch 303s straight
  to the authorize URL (auth/handlers.go:56-62), the outcome URLs are
  unchanged — and no attempt, continuation or last-signin cookie is
  written, read or deleted. The partial's documentation says to turn
  it on. `Remember *bool` stays only as an override that turns
  remembering off while the screen is on (a shared kiosk); with the
  screen off it has no effect. (Replaces round 2's
  `ContinueOnSigninPage`, which gated the continuation alone and left
  remembering on by default for every app, including apps that never
  render the screen.)
- **How a forgotten switch is caught: a runtime advisory, not a doctor
  check (accepted by the operator after round 3).** A doctor check
  cannot be sound: doctor reads files, and while it could find
  `{{template "signin"` in templates (a template action's name is a
  constant), the switch is a Go expression — `SigninScreen:
  cfg.Screen`, a helper, a struct built elsewhere — so doctor would call
  a correctly wired app broken. Instead, `SigninState` is the partial's
  only data source, and the first time it is called with the switch off
  it logs one warning, worded as a condition rather than a diagnosis —
  "SigninScreen is off: under the default CSP, with no continuation of
  your own, a keymail address cannot leave the sign-in form, and nothing
  is remembered". It cannot know whether the app widened `form-action`
  or wraps `Begin` as fichas does (F/auth_navigation.go:30-72), so it
  never says the app is misconfigured, and it fires once per process,
  not per request.
- **The one-tap names the remembered method (operator, after round 3).**
  **Continue to Keymail** when the remembered method is keymail,
  **Continue as ‹address›** for a magic link, **Sign in with your
  passkey** for a passkey. This overrides round 2's resolution of
  finding 22 (one neutral **Continue**). The label can be wrong, because
  `Begin` classifies afresh (K/flow.go:146-151); when it is, the screen
  says so rather than hiding it (§1.3 "The honest surprise", §2).
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
// SigninScreen: this app renders the shipped sign-in screen. Turns on
// the continuation, the attempt and continuation cookies, remembering
// and AnswerAsSent's parity. Default false: exactly today's behaviour.
SigninScreen bool

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

// Remember: see §1.5. Only an off switch: with SigninScreen on, a
// pointer to false turns remembering off (and deletes what was
// remembered); nil or true leaves it on. With SigninScreen off it has
// no effect — nothing is remembered.
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
pending, remembered) and returns plain data. It writes nothing, and it
consults nothing else: no database, no classifier, no `Authorize`, no
mailer. With `SigninScreen` off it reads only the query (the
outcome URLs are today's), logs the advisory once (Decisions), and
marks nothing for deletion — so an app that never turned the screen on
gets no cookie traffic from these helpers either.
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
    SentInstead bool          // Sent only, bound as SentTo: a link went where the one-tap promised Keymail (§1.3)
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
    ScriptURL             string // where the app serves passkey.JS(), the door's own module
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
went. `SentInstead` is read from the same bound attempt (its `x` field)
and is false whenever `SentTo` is empty.

### 1.3 The attempt and continuation cookies

With `SigninScreen` on, `Begin` writes up to two cookies beside the
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
| `x` | `link` only: true when the form carried `expect=keymail` (the remembered-Keymail one-tap) and a link was sent instead |

| Continuation field | Meaning |
|---|---|
| `o` | `Config.Origin` |
| `id` | 16 random bytes, base64url: the continuation id |
| `iat`, `exp` | Unix seconds; cap 10 min (the pending TTL, auth/auth.go:72-74) |
| `u` | the authorize URL |
| `st` | base64url SHA-256 of the URL's `state` |
| `ph` | base64url SHA-256 of the pending cookie's value, written in the same response |

**Begin, `SigninScreen` on:**

- Rate, bad address, other error: attempt kind `problem`; redirect as
  today (`?err=rate`, `?err=address`, `?err=1`).
- Magic link: attempt kind `link`, with `x` set if the form carried
  `expect=keymail`; redirect to `SigninPath?sent=1&attempt=<attempt
  id>`. `sent=1` is unchanged, so a page that reads only it still
  works.
- Keymail: validate `next.Redirect` with the predicate (§1.4). If it
  fails — which a correct library never produces — log, set no cookies,
  redirect `?err=1`. Otherwise set the pending cookie as today, the
  continuation cookie, and attempt kind `keymail`, and 303 to
  `SigninPath?continue=<continuation id>`. Same-origin, so `form-action
  'self'` allows it.

**The honest surprise.** The remembered-Keymail one-tap is labelled
**Continue to Keymail** (Decisions), but `Begin` classifies afresh
(K/flow.go:146-151): the server may no longer answer, it may answer
"not keymail", `KeymailServers` may now exclude it (§1.4), or an
admission wrapper may refuse the address and answer with
`AnswerAsSent`, and in every case the visitor is told a link went out.
So that one-tap posts a hidden `expect=keymail`, `Begin` (and
`AnswerAsSent`) records it in the attempt's `x` when the answer is a
link, and the bound Sent page says so in one line — "We sent you a
sign-in link this time." — before the usual text. The line states the
outcome, never a cause: the screen cannot tell which of those reasons
applied, `AnswerAsSent` contacts no server at all, and "Keymail didn't
answer" would be false for most of them (round 4, finding 28). The
wording is identical for `Begin` and `AnswerAsSent`, so it reveals
nothing an admission wrapper hides. `expect` changes only this
browser's own Sent wording: `Begin` never reads it to choose a path,
and the outcome it describes (link rather than keymail) is already
visible to the visitor. The reverse surprise needs no
wording: a remembered magic-link visitor whose address now classifies
as keymail lands on the Continue page, which already says "Taking you
to Keymail". With `SigninScreen` off, `expect` is ignored.

**`AnswerAsSent(w, r)`** is `Begin`'s magic-link answer without the
magic link, for an admission wrapper that refuses an address before
`Begin` can classify it (fichas' `beginGuard`, F/auth.go:91-116). It
follows the switch exactly as `Begin` does. With `SigninScreen` off it
is today's answer: the same-origin check, then a plain 303 to
`SigninPath?sent=1` (auth/handlers.go:64) and no cookie at all — which
is what `Begin` gives an admitted address in that mode. With it on, it
does what `Begin` does up to the flow — the same-origin check, the
address read with `r.FormValue` — then writes the same attempt cookie
(kind `link`, the address, `x` from `expect` as `Begin` would) and the
same `?sent=1&attempt=<id>` redirect, and sends nothing.
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

**Callback, `SigninScreen` on.** Today it clears pending the moment it reads it,
then lets the library find a state mismatch (auth/handlers.go:74-86) —
so tab A's late callback destroys tab B's newer attempt. With the
screen on, between reading pending and clearing it:

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
  screen off) — exactly today's path: clear pending and continuation,
  complete, admit.

The attempt cookie plays no part in the callback, so a later magic-link
or problem submission neither blocks a keymail callback nor removes its
protection. Once a first factor is verified — by `admit` for keymail
and magic links, by passkey discovery for a passkey — the attempt is
over, and the attempt cookie is deleted through one shared seam,
`lastsignin.Jar.EndAttempt` (§1.5).

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
`SigninScreen` off too: it is policy about which servers are trusted, not
about how the browser gets there. Without a list, auth leaves both
clients exactly as today. (Rejected: calling `Flow.Begin` again with
`force` after an unlisted keymail answer — it spends each rate budget
twice and can fail on the second spend.)

### 1.5 The remembered method and the attempt's end: one shared component

A new leaf package, `rastrillo/lastsignin`, owns the remembered cookie
and the one seam every sign-in path calls when an attempt ends, so
`auth` and `passkey` do both the same way and `passkey` never needs
`InstanceKey` or `auth`'s keymail dependency:

```go
type Mode int // Off | Forgetting | On
func New(Config) (*Jar, error) // Config{Origin, InstanceKey, AttemptCookie string; Mode Mode; Now func() time.Time}
func (j *Jar) EndAttempt(w http.ResponseWriter)          // deletes the attempt cookie; Off: nothing
func (j *Jar) Remember(w http.ResponseWriter, rec Record) // On: writes; Forgetting: deletes the cookie; Off: nothing
func (j *Jar) Read(r *http.Request) (Record, ReadResult) // Absent | Valid | Invalid; never Valid unless On
func (j *Jar) Clear(w http.ResponseWriter)                // Forget's delete; Off: nothing
type Record struct{ Method, Address string }
```

- **The mode comes from the one switch.** `auth.New` builds the one jar:
  `Off` when `SigninScreen` is off (whatever `Remember` says), else
  `Forgetting` when `Remember` points at false, else `On`. `Off` writes,
  reads and deletes nothing — no Set-Cookie header at all — which is
  what "an app that does not adopt the screen sees no change" requires.
- The jar derives its own key: `crypto.Derive([]byte(InstanceKey),
  "rastrillo/lastsignin/v1")`. `AttemptCookie` is the attempt cookie's
  resolved name (§1.3), which `auth` passes in so the jar can delete it
  without knowing its format. `auth` exposes the jar as
  `a.RememberJar()`; the app hands that to `passkey.Config.Remember`
  (new field, `*lastsignin.Jar`). The partial's docs show the wiring.
- Cookie `__Host-rastrillo_last_signin` (the `cookieName` rule),
  HttpOnly, SameSite=Lax, `Path=/`, Secure from Origin, Max-Age 400
  days (the browser cap). Value `"v1." + base64url(SealSym(key,
  json({o, m, a, iat, exp})))`, with `exp = iat + 400 days`. Open
  refuses a wrong origin, `now ≥ exp`, `iat > now + 1 min`, an unknown
  method, a keymail or magiclink record without a valid address (≤ 254
  bytes, one `@`, no control bytes), and a passkey record with one.
  Every sign-in rewrites it, so the 400 days run from the latest one.
- **`auth.admit`** calls `EndAttempt` as soon as it holds a verified
  identity (before `Authorize`: the attempt is over either way), then
  `Remember({id.Method, id.Address})` after `Authorize` admits and
  `SubjectFor` succeeds, before the `SecondFactor` hook — the address,
  not the subject. Before the hook because nothing later knows the
  address (`secondfactor.Gate.Complete` holds only the subject,
  secondfactor/secondfactor.go:290-303). So a sign-in held for a second
  factor is remembered, and so is one whose second factor then fails or
  whose session insert fails: the record says which door this browser
  used and proved, nothing more. Nothing is remembered when `Authorize`
  refuses or `SubjectFor` errors.
- **`passkey.DiscoverFinish`**, when its `Remember` is set, calls
  `EndAttempt` once the assertion verifies (passkey/passkey.go:424-427),
  then `Remember({"passkey", ""})` after its `Authorize` check (:428-431)
  and before `Gate.Hold` or `Sessions.SignIn` — so both run on the held
  path and the completed path alike; `heldResponse` passes headers
  through (:465-470). **The address is cleared twice over.** The
  remembered record carries none: discovery knows a subject, not an
  address, and `SubjectFor` may make subjects opaque (auth/auth.go:
  110-139), so keeping the old address would label Bob's passkey sign-in
  with Alice's. And the attempt cookie goes too, because `SigninState`
  prefers the attempt's address for the prefill (§1.2): without
  `EndAttempt`, Alice's typed address would reappear in the email
  fallback beside Bob's passkey door until it expired (round 3, finding
  25). A failed assertion ends and writes nothing. `passkey` without a
  jar does neither, and the docs say what that costs: a previously
  typed or remembered address stays on the screen after a passkey
  sign-in.
- Survives `Signout`.
- **`Remember = false`** (screen on): `Remember` deletes any existing
  cookie, `Read` reports nothing valid, `SigninState` marks a present
  cookie for `PrepareSigninResponse` to delete. So switching it off
  also forgets what was remembered before. With the screen off,
  `Remember` has no effect: nothing is remembered to begin with.
- **`auth.Forget`**, mounted by the app at `ForgetPath`: refuses any
  method but POST (405) — a state change on GET would be prefetchable —
  and refuses a cross-origin submission with `a.sameOrigin`
  (auth/csrf.go:12-14), as `Begin` does. It calls `Clear` and
  `EndAttempt` and 303s to `SigninPath`. "Use a different email" is a
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
<script type="module" src="{{.Passkey.ScriptURL}}"></script>
```

**Delivery (amended after the plan review).** The enhancer is its own
ES module, `passkey/js/signin.mjs`, embedded in `passkey` and exposed as
`passkey.JS()`; the app serves it beside `webauthn.JS()` and puts its
URL in `State.Passkey.ScriptURL`. The partial emits the external
`<script type="module" src>` only when it renders a passkey door, and
never in `Preview`, so ordinary pages, email-only screens and gallery
previews never request it. It replaces the first decision — new code in
`rastrillo.js`, which every shell loads — because that file is at its
16 KiB cap (`TestShimIsSmall`), whose own rule is to split past it, and
because every page of every app would have paid for a button that
exists on one. No CSP change: the default policy has no `script-src`,
so scripts fall back to `default-src 'self'`, which admits a
same-origin module; there is no inline initializer. The protocol:

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
   held path, passkey/passkey.go:450): navigate to `to` only if both
   hold, else to `/`:
   - it is a local absolute path — starts with exactly one `/` (so not
     `//` and no scheme), no `\`, no control character. This is the
     rule `sessions.SafeReturn` enforces (sessions/sessions.go:461-469)
     and `rastrillo.js` has as `localPath` (ui/rastrillo.js:66-69); the
     module carries the same rule as its own exported `localPath`,
     tested directly in Node against the cases below. A same-origin
     *absolute URL* is refused too: the server
     never sends one, so accepting it would only widen the contract.
   - and `new URL(to, location.href).origin === location.origin`. The
     prefix rule alone already refuses `"/\t/evil.example/x"` by its
     control character; the parsed-origin check is the second line, for
     whatever a future edit to the prefix rule lets through.
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
- **Returning.** The one-tap names the remembered method (Decisions):
  - Keymail: **Continue to Keymail**, posting to `BeginPath` with the
    remembered address and `expect=keymail` as hidden fields. The
    address shows beneath in `<p id="rst-signin-remembered">…<bdi>…
    </bdi></p>` ("as ‹address›"), and the button has
    `aria-describedby="rst-signin-remembered"`, so a screen reader hears
    the address with the action.
  - Magic link: **Continue as ‹address›**, the address in a `<bdi>`
    inside the label, posting the remembered address as a hidden field.
    The label already carries the address, so there is no separate line
    and no `aria-describedby`.
  - Passkey: **Sign in with your passkey** — the passkey door, with the
    Ask form beside it (§1.6 step 6).
  - Both email one-taps re-run `Begin`, which classifies afresh
    (K/flow.go:146-151), so the label can promise a method the flow no
    longer takes. When Keymail was promised and a link went out, Sent
    says so (§1.3 "The honest surprise"); when a link was promised and
    Keymail answers, the Continue page says where it is going.
  **Use a different email** posts to `ForgetPath`.
- **Sent.** "Check your email"; if `SentInstead`, first "We sent you a
  sign-in link this time." (the outcome, not a cause, §1.3); then "We sent a link to
  ‹SentTo›" if bound, else "We sent a link to your inbox"; "the link
  works once and expires"; **Use a different email** (posts to
  `ForgetPath`).
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
| Reauth, Returning keymail/magiclink | the one-tap (**Continue to Keymail** or **Continue as ‹address›**) | focus: the label, and for Keymail the address it describes; the info callout is read in order |
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
  `stage` frame (Ask, Returning ×3, Sent bound, unbound and with the Keymail-surprise line, Continue,
  Address and Keymail problems). Continue uses `Preview`. The password
  screen stays as copyable markup with its warning, now explicitly
  "not shipped"; the dead `/signin/other` and `/signin/reset` links go.
- `docs/site/magic-links.md`: "The sign-in page stays yours" becomes
  "Use the shipped screen, or your own", with the wiring (both config
  paths, `SigninState` + `PrepareSigninResponse`, the `stage` page set,
  `RememberJar` into passkey), `SigninScreen` and what it turns on,
  `Remember` as the kiosk off-switch, the allowlist and the trust
  argument from "Decisions", and for an app with an admission wrapper,
  `AnswerAsSent` and why a plain `?sent=1` is an oracle. The outcome
  table gains `err=1`,
  `continue=<id>`, `sent=1&attempt=<id>` and `reauth=1`.
- `docs/site/passkeys.md`: fix the stale "A passkey never signs anybody
  in from nothing", list `discover` among the routes, document
  `Config.Remember`.
- SKILL.md (:287-295): an app on the shipped screen sets
  `SigninScreen`; widening `form-action` is only for a hand-built page
  with the screen off. The
  "only listed servers work" sentence becomes `KeymailServers`. Within
  the byte budget by trimming, per AGENTS.md.
- CHANGELOG: Added (`signin`/`signin-title` partials, `stage` shell,
  `stageArt`, `SigninState`, `PrepareSigninResponse`, `Forget`,
  `AnswerAsSent`, the `field` partial's `QuietError`, the `callout`
  partial's `ID` and `Focus`,
  `SigninScreen`, `KeymailServers`, `BeginPath`, `ForgetPath`,
  `Remember`, `lastsignin`, `passkey.Config.Remember`, `passkey.JS()`). Changed:
  nothing for an app that leaves `SigninScreen` off — no new cookie,
  and `Begin`/`Callback` as before; `KeymailServers`, if set, applies
  either way. Turning the screen on turns on the continuation and
  remembering together; `Remember: &false` keeps the screen and stops
  remembering.
- fichas adopts it in its own repo as a follow-up branch; not this
  branch.

## 5. Tests

- **SigninState:** every row of the §1.2 table, including precedence
  when several parameters are present; unknown values; `Address` never
  from the query; `SentTo` only when bound, never the remembered
  address. Sent consults nothing: built with an `Authorize` that fails
  the test if called, a closed `*sql.DB` and a classifier transport that
  fails the test, `?sent=1` still yields Sent. `SentInstead` only on a
  bound attempt with `x`. The one-time warning fires with
  `SigninScreen` off (and names it) and never with it on; with it off,
  valid attempt, continuation and remembered cookies on the request are
  ignored and none is marked for deletion.
- **PrepareSigninResponse:** no-store on every state; no-referrer on
  Continue and on no other state; deletes exactly the cookies marked.
- **Envelopes** (attempt and continuation each): round trip; refused
  when tampered, sealed under another key (including each other's),
  for another origin, expired (payload `exp`, with the cookie still
  sent), `iat` in the future, over its cap, unknown version prefix,
  unknown field.
- **Screen off is today:** with `SigninScreen` off and `KeymailServers`
  unset (and `Remember` nil, true and false alike), `Begin`'s every
  answer — keymail 303 to the authorize URL, `?sent=1`, each error —
  and `Callback`'s and `admit`'s responses are byte for byte today's,
  with no `Set-Cookie` beyond the cookies today's flow writes on that
  path: the pending cookie, the session cookie, and on a path a
  `SecondFactor` hook holds, the second-factor cookie
  (`__Host-rastrillo_secondfactor`, secondfactor/secondfactor.go:
  196-205). Passkey discovery with the jar wired sets no cookie beyond
  the session's on the completed path and the second-factor cookie on
  the held path (`Gate.Hold`, passkey/passkey.go:434-450).
  `KeymailServers` is excluded because it is a deliberate change that
  applies in both modes (§1.4).
- **Continuation:** screen on: 303 to `?continue=<id>`; forged `?continue=https://evil`
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
  screen-off `Begin` in between) does not block the current callback. A
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
- **AnswerAsSent,** in both modes: a wrapper that refuses with it and
  one that passes to `Begin` for a magic link give the same status, the
  same `Set-Cookie` names and attributes, the same redirect shape and
  the same rendered Sent page, for the same input — with the screen
  off, plain `?sent=1`, no cookie and "your inbox"; with it on,
  `?sent=1&attempt=<id>`, the attempt cookie and the address, and with
  `expect=keymail` the same `SentInstead` line. `AnswerAsSent` sends no
  mail and consults no classifier.
- **Honest surprise:** a remembered-Keymail one-tap whose address now
  classifies as not keymail (the server stops answering; or an
  allowlist that excludes it) gets a link and a Sent page with the
  "We sent you a sign-in link this time" line, and an admission wrapper
  refusing the same one-tap with `AnswerAsSent` gives the identical
  line; the same address typed into Ask, with no
  `expect`, gets the ordinary Sent page; `expect` never changes which
  path `Begin` takes.
- **Remembered and the attempt's end** (screen on): written on keymail
  and magiclink admit with the address (not the subject), including when
  SecondFactor then holds; not when `Authorize` refuses or `SubjectFor`
  errors; the attempt cookie deleted on every verified first factor.
  Passkey discovery with an existing attempt address *and* a remembered
  email address: afterwards the attempt cookie is gone and the record is
  `{passkey, ""}`, on the minted and the held path, and the next
  sign-in page's `Address` is empty; a failed assertion changes
  neither cookie. Survives Signout; `Forget` clears both cookies,
  refuses GET and cross-origin; `Remember=false` deletes an existing
  cookie on the next sign-in and on the next sign-in page, and never
  reads one; a bad, expired or
  wrong-origin cookie is ignored and cleared; a request carrying only a
  valid remembered cookie is refused by `RequireSession`.
- **Partial:** renders every step × problem × remembered method with
  fixtures; the heading/title table; autofocus exactly per the §2
  matrix, one element at most; no `role="alert"` anywhere in the
  rendered screen; the one-tap reads **Continue to Keymail** with
  `aria-describedby` naming the address line and a hidden
  `expect=keymail`, **Continue as ‹address›** (in `<bdi>`) with no
  `expect` and no describedby, and **Sign in with your passkey** for a
  passkey; the Keymail problem posts `force=1`;
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
  starts no second ceremony; pages with no passkey door — an ordinary
  page, an email-only screen — never request the module. The page
  navigates only after finish answers `ok: true`: a Node drive of the
  module's ceremony covers malformed, non-2xx and failed-network
  answers. The navigation check (a Node test of the module's own
  function) refuses `//evil`, `/\t/evil`, `/\n/evil`,
  `/\\evil`, a cross-origin absolute URL and a same-origin absolute
  URL (the local-path rule, §1.6 step 4), and accepts `/`, `/home` and
  `/confirm?x=1`.
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
| 1 | Blocker: custom sign-in pages stop completing keymail | Opt-in `ContinueOnSigninPage`, default off; off is today's behaviour exactly. Doctor check found unsound and replaced by a one-time runtime warning (Decisions). After round 3 the operator folded this into the single `SigninScreen` switch |
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
| 22 | Important: remembered-method labels promise a method `Begin` re-decides | One-tap label is **Continue** for both email methods; the address is its description (§2). **Overridden by the operator after round 3**: see below |
| 23 | Important: focus rules conflict (`field`'s `role="alert"`, Reauth, disabled busy button) | One focus matrix with Reauth precedence; `field` `QuietError`; `callout` `ID`/`Focus`; passkey busy via `aria-busy`/`aria-disabled`, never `disabled`; autofocus described as a request (§1.6, §2) |
| 24 | Minor: runtime warning is not a sound detector | Reworded as a conditional advisory that never calls the app misconfigured (Decisions) |

### Round 3 (Astra, 2026-09-27) — not ready; no Blockers, 1 Important, 2 Minor

Re-verdict: 3, 5, 12, 13 and 18–24 resolved; 15 and 17 partly resolved,
their remaining gap being finding 26. All fixed below.

| # | Finding | Resolution |
|---|---|---|
| 25 | Important: passkey discovery leaves an earlier attempt's typed address in the prefill | `lastsignin.Jar.EndAttempt` is the one seam every sign-in path calls on a verified first factor; passkey discovery calls it before hold or session, so held and completed paths both clear it; test with an existing attempt address (§1.5, §5) |
| 26 | Minor: `AnswerAsSent` switch-off behaviour unstated | It follows the switch as `Begin` does: off, plain `?sent=1` and no cookie; on, attempt-cookie parity. Parity tests in both modes (§1.3, §5) |
| 27 | Minor: navigation predicate and test disagree on absolute URLs | Keep the local-absolute-path rule (`localPath`, ui/rastrillo.js:66-69; `SafeReturn`) and the parsed-origin check; a same-origin absolute URL is refused, and the test says so (§1.6, §5) |

Operator decisions after round 3:

- **One switch.** `ContinueOnSigninPage` is replaced by `SigninScreen`,
  which turns on the continuation and remembering together; off means
  no new cookie of any kind and today's `Begin`/`Callback`. `Remember`
  is only an off-switch while the screen is on. This also withdraws
  round 2's "auth writes the remembered cookie by default", which
  changed apps that never render the screen (Decisions, §1.1, §1.5,
  §4, §5).
- **The one-tap names the method** — **Continue to Keymail**,
  **Continue as ‹address›**, **Sign in with your passkey** — overriding
  the resolution of finding 22. The honesty the finding asked for moves
  to the outcome: a Keymail one-tap that ends in a link says so on the
  Sent page, through a hidden `expect=keymail` that changes only the
  wording (§1.3, §2, §5).
- **The runtime advisory** in place of a doctor check is accepted
  (Decisions).

### Round 4 (Astra, 2026-09-27) — ready for an implementation plan; 2 Minor

Re-verdict: 25, 26 and 27 resolved. Two Minor corrections, both applied
here.

| # | Finding | Resolution |
|---|---|---|
| 28 | Minor: `expect=keymail` cannot establish why Keymail was not used; "Keymail didn't answer" is false for an allowlist refusal, a negative classification, `force=1`, and `AnswerAsSent` (which contacts nothing) | The Sent line states the outcome only — "We sent you a sign-in link this time." — identical for `Begin` and `AnswerAsSent` (§1.3, §2, §5) |
| 29 | Minor: "Screen off is today" allowed only pending and session cookies, but today's `admit` and discovery can hold for a second factor and write `__Host-rastrillo_secondfactor` | The test compares against the cookies today's flow writes on each path, held paths included, and qualifies unchanged behaviour as "with `KeymailServers` unset" (§5) |

### Plan review (Astra, 2026-09-27) — one design change

The plan review's finding 3 found the enhancer's delivery wrong for the
code as it stands: `rastrillo.js` is 16,364 of its 16,384-byte cap and
its size test says to split past 16 KiB, and the first plan raised the
cap instead. Decided with the operator: the enhancer is a separate
module, `passkey.JS()`, loaded by the partial only with a rendered
passkey door (§1.6 "Delivery", `PasskeyDoor.ScriptURL` in §1.2, §4, §5).
The review's other fifteen findings were about the plan, not the design,
and are fixed there.
