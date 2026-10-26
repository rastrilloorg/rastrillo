# auth: sign in with an OpenID Connect provider

Answers discussion [#134](https://amadan.net/rastrillo/rastrillo/-/discussion/134).
Built with Claude Sonnet 5 (`claude-sonnet-5[1m]`, "high" effort in Claude
Code) — not the strongest model this account can reach; see "Model" under
Review log for why.

**Revision 2.** Revision 1 was adversarially reviewed (Review log, below) and
found unsafe to build from: three blockers, all in the hub/second-factor
wiring this revision exists to fix. Every section below reflects that
review's disposition, not the original reasoning — read Review log first if
you read revision 1 already, because D3 and D5 in particular now say the
opposite of what they said.

## Why

A firm that already runs Microsoft Entra ID or Google Workspace wants its
staff to sign in with that account. `auth` offers magic links, keymail,
passwords and passkeys — all first factors this application vouches for
itself. None of them lets a THIRD PARTY the app's operator trusts vouch for
an address instead.

**What this does not do, stated here rather than only under Not building:**
it does not make turning someone off at the provider cut their access here
immediately. Nothing after sign-in re-checks the provider — no refresh
token is kept, by design (D2) — so a session lives for its ordinary
`sessions` TTL after the provider revokes the person, same as ips's own
shipped feature. Revocation at the provider stops their NEXT sign-in; an app
that needs faster enforcement sets a short `SessionTTL` on the
`*sessions.Sessions` it hands this package, which is a knob that already
exists and needed no new code.

ips built the verification core this ports from (`internal/oidc`,
`docs/design/88-single-sign-on.md`, shipped 2026-09-07, two providers in
production). Its code and design are cited throughout; every citation is
unverified from this checkout (ips is not present here) and is flagged as
such where this spec's own correctness leans on one.

## Decisions

### D1 — A sibling package, not inside `auth`

**Unchanged from revision 1.**

`password` is the existing precedent for "a first factor beside `auth`'s
own, sharing `sessions.Sessions`": not inside `package auth`, built with
`Config.Sessions *sessions.Sessions` and its own `Config.SecondFactor` hook,
calling `cfg.Sessions.SignIn` on success (`password/handlers.go:95-97,
116-123,474-499`). `oidc` is `package oidc` (root, alongside `password` and
`passkey`), same shape.

### D2 — The verification core, ported close to verbatim, with four additions the review found missing

**Revised.** Keeps ips's narrowness (RS256-only refused by name; no refresh
token; no userinfo call; https with no exception) and ports
`oidc.go`/`verify.go`/`start.go` near-verbatim as before. Four things
revision 1 left unstated, found as finding 14 (minor) and finding 12
(minor) of the review, are now explicit rules rather than implied by "port
near-verbatim":

- **Never read `jku`, `jwk` or `x5u` from a token's header.** Not refused —
  simply never parsed: the header struct this package decodes has exactly
  two fields, `alg` and `kid` (ips `verify.go:91-94`), so a header claiming
  to carry its own key is inert by omission rather than by a check that
  could be gotten wrong.
- **A JWKS key's modulus must be at least 2048 bits and its exponent
  sane** (ips already does this, `verify.go:437-461`) — stated here as a
  rule because it is a security property this port must not silently
  drop, not merely inherit.
- **Every HTTP call this package makes — discovery, JWKS, token exchange
  — disables redirect-following.** ips's `Client` uses a plain
  `http.Client` with default redirect behaviour; this spec changes that.
  A provider's own published endpoints have no reason to redirect, and a
  client that follows one blindly turns a provider misconfiguration (or a
  compromised endpoint) into this instance being told where to send a
  client secret. `http.Client{CheckRedirect: func(..., via) error { return
  http.ErrUseLastResponse }}` on all three calls.
- **Request and response bodies stay bounded** — ips's 1MB `readBody`
  cap, ported unchanged, stated as a rule because it is the actual
  defence against a provider answering with something enormous, not an
  implementation detail.

**Provider list: fixed, not a registry.** Revision 1 left this as an open
question while its own file-layout sketch already assumed an answer
(finding 16) — that self-contradiction is resolved here by deciding:
`oidc` ships Microsoft and Google, the two ips has running in production,
as a closed set exactly like ips's own `providers.go`. No
`oidc.Register(Provider)`. A third provider (Okta, named in the discussion
but not built or interop-tested anywhere this spec can cite) is a
follow-up branch once somebody can test it against a real tenant — the
same bar ips's own D88.2 sets for itself. A registry invites an app
calling it with an unreviewed provider of its own, which is the free-text
issuer box D88.2 already refused, with extra steps.

**Fuzzing targets the claims, not only the envelope** (review finding 12:
random bytes almost never pass an RS256 check, so a fuzzer handed only raw
token strings barely reaches `flexBool`, the `aud` string-or-array decode,
or the address extraction — the code most worth fuzzing). Two targets,
not one:　`FuzzTokenShape` (ips's original idea: malformed base64, wrong
part counts, non-JSON segments — reaches the pre-signature parsing) and
`FuzzClaims` (signs a fuzzed claims JSON with a fixed test RSA key so the
signature passes, then runs the real post-signature checks — reaches
`flexBool`, `audience.UnmarshalJSON`, and `address()`). The "seven
malformed shapes" revision 1 claimed as its seed corpus was actually five;
corrected here to just point at the adversarial test table directly rather
than recount it.

### D3 — Connections: a caller-supplied lookup, keyed by tenant, never by provider kind alone

**Revised. Two blockers from the review (findings 3 and 5) land here.**

Revision 1's `Connections func(ctx, kind) (Connection, bool, error)` and its
claim that `Connection`'s shape could be copied from ips's `Config`
unchanged were both wrong in ways that only show up multi-tenant, which is
exactly the case this design exists to generalise to.

**Finding 5 — JWKS/discovery caching must be keyed by issuer, not by
provider kind.** ips caches per `Client` value because ips has one
connection per provider per instance; this spec explicitly generalises to a
hub with many tenants sharing one provider kind. A cache keyed by `Kind`
alone would let one tenant's signing keys verify another tenant's tokens —
concretely, Entra lets a tenant admin configure a custom signing key for an
app using claims-mapping, so a shared-by-kind cache is a real escalation
path, not a hygiene nitpick. Fix: `Client`'s `*KeySet` and `*MetaCache`
become maps keyed by `jwks_uri` / issuer internally, held once on
`oidc.Handlers` and shared across every `Connection` of every kind, with a
single-flight guard on a cache miss so N simultaneous sign-ins against one
cold tenant do not fire N JWKS fetches.

**Finding 3 — the address/admission rule was missing from the document
entirely**, with the two-`@` parsing and the hosted-domain requirement
cited from ips but never stated as THIS package's own rule, and
`Connection` carrying no field for it. Fixed by making both core's job,
checked before the app's own admission hook ever runs (see D5's `Admit`):

- `Identity.Address` empty, or `Identity.AddressVerified == Denied` →
  refused by `oidc` itself, before any app code runs. `Unstated` is
  allowed through — Entra never asserts the claim at all (ips
  `oidc.go:130-137`), and refusing every Entra sign-in over a claim it
  never sends is the error ips's own design already rejected.
- For a `Provider.NeedsHostedDomain` connection (Google today),
  `Connection.AllowedDomains []string` is REQUIRED (`New`/the connection
  resolver refuses a connection with none set) and `Identity.HostedDomain`
  must be present and match one of them — checked by `oidc`, not left to
  an app's membership hook to remember. This is ips's D88.11 turned into
  an unconditional core check rather than a convention: "is this address
  on our people list" against an issuer shared with every Gmail account
  that has ever existed is precisely the gap D88.11 closed, and a
  framework shipping this to many apps cannot assume every app's own
  `Admit` hook will re-derive that argument correctly.
- The address itself is read `email` first, then `preferred_username`,
  then `upn`, each accepted only with exactly one `@` (ips
  `verify.go:210-242`) — ported unchanged, and now stated here as this
  package's own rule rather than left implicit in a port.

**`Connection` gains a tenant key and loses nothing**:

```go
// ConnectionKey is the app's own name for which tenant's registration
// this is — opaque to oidc. "" for a single-tenant app. Read once at
// Begin (Config.ConnectionKey, below) and sealed into the ceremony so
// Return resolves the SAME tenant without anything the provider's
// callback carries (finding 4).
type ConnectionKey string

type Connection struct {
    Issuer, ClientID, ClientSecret, RedirectURI string // unchanged from ips's Config
    AllowedDomains []string                             // required when Provider.NeedsHostedDomain
}

Connections func(ctx context.Context, kind Kind, key ConnectionKey) (Connection, bool, error)
```

A single-tenant app's lookup ignores `key` and returns a fixed
`Connection`. A hub app's lookup resolves `key` against its own tenant
table — the same thing `password.Config.Lookup` already does for an email,
generalised to a second axis.

### D4 — The ceremony cookie: sealed, per-provider, now carrying the tenant key and the return path, with attributes fully specified

**Revised. Findings 4, 9 and 10 land here.**

The crypto revision 1 proposed checks out against the real primitives
(`crypto.Derive(key []byte, context string) []byte`, `crypto.SealSym`/
`OpenSym`, `crypto.go:287,310,330`) — the review confirmed this directly.
What revision 1 left unspecified was everything ELSE about the cookie, and
one piece of state (the tenant) it didn't carry at all.

```go
type ceremony struct {
    State, Nonce, Verifier string
    Key                     ConnectionKey // finding 4: so Return can call
                                          // Connections again with the SAME
                                          // tenant, with nothing taken from
                                          // the provider's own callback
    ReturnTo                string        // finding 10: SafeReturnTo'd at
                                          // Begin, before sealing — Return
                                          // trusts it because Begin already
                                          // validated it same-site, not
                                          // because it came back sealed
    Expires                 time.Time
}
```

Sealed with `crypto.SealSym(crypto.Derive(instanceKey,
"rastrillo/oidc/ceremony/v1:"+string(kind)), plaintext)` — bound to the
PROVIDER KIND only (not to `Key`, which lives inside the sealed plaintext
instead): this is what makes "a ceremony sealed for google cannot be opened
on microsoft's return path" true by construction, since `GET
/signin/oidc/microsoft/return` derives a different key and `OpenSym` simply
fails — exactly ips's D88.5 property, unchanged.

**Cookie attributes, specified (finding 9), reusing `auth.Auth`'s own
pattern rather than inventing one:**

- Name: `rastrillo_oidc_ceremony_<kind>` (one per provider kind — two
  simultaneous ceremonies for DIFFERENT providers, two browser tabs, must
  not overwrite each other; two ceremonies for the SAME provider
  colliding is accepted, same as ips).
- `__Host-` prefix when the app's origin is https, unprefixed when it is
  not — the exact `secure()`/`cookieName()` pattern `auth.Auth` already
  implements (`auth/auth.go:624-638`), copied rather than re-derived.
- `HttpOnly`, `SameSite=Lax` (ips's reasoning: the return leg is a
  top-level GET from the provider's own origin, which `Strict` would
  drop), `Secure` matching the origin.
- 10-minute `Max-Age`, matching ips — and the Expires field INSIDE the
  sealed blob is still the real bound, checked against the clock; `Max-Age`
  is defence in depth, never trusted alone (ips's own D88.5 bug, avoided
  by construction rather than by remembering not to repeat it).
- Cleared on every outcome of Return — success, refusal, a cookie that
  fails to open, or the provider's own `?error=`. A cookie that outlives
  its one use is a replay surface for no benefit.

**`InstanceKey` is a `string`, matching `auth.Config.InstanceKey`** (finding
16: revision 1 wrote `[]byte`, inconsistent with the sibling package it
otherwise copies closely), and `New` refuses one shorter than 32 bytes —
`auth.New` already refuses an EMPTY key with `ErrEmptyInstanceKey` and
explains why (an empty input hashes to one fixed, publicly-computable
value); this spec adds a length floor on top because `crypto.Derive` has no
floor of its own and a short key is a short key regardless of whether it is
empty.

### D5 — Session minting: `sessions.Sessions.SignIn`, with the real constraints of Signed mode now part of the design instead of asserted away

**Revised. Findings 1, 2 and 6 — three of the review's blockers — land
here.** Revision 1 claimed "no branch in oidc itself" for hub vs. per-tenant
sessions and that D88.9's `Method`-suffix composition "continues to work
unmodified". Both claims were checked against `sessions/signed.go` and
`secondfactor/secondfactor.go` by the review and found false. This revision
does not repeat them.

**Finding 1 (blocker) — `Authorize func(ctx, Identity) (ok bool, string)`
had no way to say WHO the session is for.** `password.Config.Lookup` maps
an email to the app's OWN id (`password/handlers.go:102`); `auth.Config`
has `SubjectFor` for exactly this. Revision 1's D3 claimed to mirror
`Lookup` "exactly" and did not — it resolved a provider registration, not a
person. Replaced with:

```go
// Admit maps a verified, already-domain-checked identity (D3's core
// refusals have already run) to THIS app's own subject, or refuses it —
// ips's D88.8: "the directory says who somebody is; this instance still
// says whether they work here." Required: New refuses a nil Admit.
//
// The returned subject becomes sessions.Session.Subject. Two
// consequences that make this the app's decision and not a default:
//   - In Signed (hub) mode a subject must satisfy identifier(subject,
//     128) — an opaque id, never an address (sessions/signed.go:204) —
//     so an app running hub mode cannot return a raw email here.
//   - secondfactor.Gate decides whether someone has a passkey or TOTP
//     enrolled by looking up THIS subject (Enrolled(subject),
//     secondfactor/secondfactor.go:117). An app with password or
//     keymail sign-in already wired MUST return the SAME subject those
//     use for the same person — a different spelling here is an
//     enrolled second factor silently skipped, on the one door that
//     bypasses a password.
//
// An error means something failed (a membership lookup that could not
// run) and answers 500. A refusal — wrap ErrNotAdmitted, or return it
// bare — answers the one-sentence-with-detail-withheld-to-everyone-but-
// this-address page ips's D88.8 describes.
Admit func(ctx context.Context, conn Connection, id Identity) (subject string, err error)

var ErrNotAdmitted = errors.New("oidc: that address is not admitted here")
```

**Finding 6 (blocker) — the order `Admit` → `SecondFactor` → `SignIn` is
normative, not implicit**, closing the route the review found: calling
`SecondFactor` before `Admit` lets an unadmitted identity reach a pending
half-session, which `secondfactor.Gate.Complete` later promotes to a real
one with no further admission check (`secondfactor.go:290-302`). `Return`'s
one call sequence is: verify the token → run D3's core refusals →
`Admit` → on success, build `sessions.Session{...}` → `SecondFactor` (if
set) → `Sessions.SignIn`. A test asserts `Admit` returning a refusal means
`SecondFactor` is never invoked, mirroring this document's own rule about
testing a refusal's REASON rather than its status.

**Finding 2 (blocker) — three concrete incompatibilities with Signed mode,
named rather than glossed over:**

- **`Method` must match Signed mode's key-id alphabet,
  `[a-z0-9_-]{1,32}` (`sessions/signed.go:339-350`).** `"oidc:microsoft"`
  is refused outright (colon). Fixed: `"oidc-microsoft"` / `"oidc-google"`.
- **`AuthTime` later than the session's own issue time is refused
  (`signed.go:212`).** An IdP's clock running even slightly ahead of this
  instance's — well within the 2-minute skew this package already
  tolerates for token verification — would make `SignIn` fail with a
  500 in Signed mode specifically. Fixed: the session's `AuthTime` is
  `min(server now, identity.AuthTime)`, never the provider's figure taken
  on faith. (`Identity.AuthTime` itself, what the TOKEN asserted, is
  unaffected — this clamp is only on what `oidc` hands to
  `sessions.Session`.)
- **The `+passkey`/`+totp` suffix `secondfactor.Gate` appends
  (`secondfactor.go:300`) cannot be minted in Signed mode at all**,
  because the composed string also fails the same key-id alphabet the
  moment it is produced by a factor whose own spelling collides with it —
  this is a PRE-EXISTING gap in core between `secondfactor` and Signed
  mode, not something this branch introduces or can fix by itself. Stated
  here as an explicit limitation: **OIDC plus a second factor does not
  work in Signed (hub) mode today.** `New` with both `Config.SecondFactor`
  set AND a `*sessions.Sessions` built in Signed mode is refused rather
  than silently shipping a combination nobody has made work. Raised as a
  question for core in Open questions, below — it belongs to `sessions`/
  `secondfactor`, not to this branch, and fixing it here would be
  reaching past what this branch owns.

**What D5 now actually claims, stated plainly so it cannot be
over-read again:** `oidc` itself contains no branch on DB-mode vs.
Signed-mode `*sessions.Sessions` — that part of revision 1's claim holds,
confirmed by the review reading `sessions.SignIn`'s own dispatch. What
revision 1 got wrong was implying that meant the REST of the contract
(subject shape, Method alphabet, AuthTime ordering) was mode-agnostic too.
It is not, and the three fixes above are what make it actually work rather
than merely compile.

### D6 — Step-up and `amr`/`acr`: one gap found, one decision confirmed sound

**Revised — adds the step-up handling finding 8 found missing; D88.4's
non-substitution argument was reviewed and found sound as written,
unchanged.**

`Identity.Methods` (`amr`) is recorded and consumed by nobody in `oidc` —
confirmed by the review against the discussion's own "Checks" list and
ips's D88.4 reasoning, which this spec ports as a core decision rather than
an app policy: a provider's claim about its own second factor is not
evidence this application's own second-factor requirement can rest on,
for the same reason a firm's own "our tenant enforces MFA" checkbox was
refused (D88.4).

**Step-up (finding 8), new:** `sessions.RequireFresh` sends a stale session
back to `SigninPath` with `?reauth=1` (`sessions.go:429-447`), and revision
1 gave `Begin` no way to honour that — `prompt=select_account` does not
force re-authentication, and a provider's own `auth_time` can be days old.
`Begin` reads `?reauth=1` off its own query string (the same convention
`auth`/`password` answer, not a new one) and when present sends
`prompt=login&max_age=0` instead of `prompt=select_account`, and `Return`
then requires the RESULTING `identity.AuthTime` to be within the last
minute before treating the sign-in as satisfying step-up — not merely
present, since a provider that ignores `max_age` (several do, for an
already-interactive session) could otherwise hand back a stale
`auth_time` that looks fresh enough.

### D7 — The start is a link; `response_mode=query`'s real reason corrected

**Revised — the review found revision 1's form-action reasoning for
`response_mode` wrong; kept correct where it was already correct.**

Two separate claims, previously conflated:

- **The sign-in BUTTON must be an `<a>`, never a form whose submission
  redirects off-site.** This part of ips's D88.6 does transfer: `serve.go`
  sets the same `form-action 'self'` policy on every response (grep
  `serve.go` for `form-action`), and that policy is enforced against the
  redirect a FORM SUBMISSION on THIS page produces. A `<form>` posting to
  a same-origin handler that then 303s to the provider would have that
  redirect dropped silently. The `ui` task (D1, §2) must render the
  provider buttons as links.
- **`response_mode=query` rather than `form_post` is chosen for
  simplicity and because this flow has no reason to want a POST, not
  because of `form-action`.** Revision 1 claimed `form-action` was why
  `form_post` is refused; the review correctly points out `form-action`
  governs redirects THIS app's responses produce, and a `form_post`
  response would be the PROVIDER's page posting TO our `Return` endpoint
  — a request arriving at us, which `form-action` on our OWN earlier
  response has no bearing on. What actually matters there is whether
  `Return` wants to accept a cross-site POST at all, which is a
  `SameSite`/CSRF question, not a CSP one. `query` is kept as the choice
  because it keeps the entire ceremony a chain of GETs with nothing new
  for `csrf.Protect` to carry an exception for, which is a real but
  smaller reason than revision 1 gave.

## 1. `oidc` — the package

```
oidc/
  discover.go     // Discover, Metadata, Client (ported from ips oidc.go; D2's redirect/bound-body rules)
  verify.go       // Verify, claims, Tristate (ported from ips verify.go)
  keys.go         // KeySet/MetaCache, now keyed by issuer/jwks_uri across all connections (D3, finding 5)
  verify_fuzz_test.go   // FuzzTokenShape, FuzzClaims (D2)
  ceremony.go     // ceremony struct, seal/open via crypto.SealSym, PKCE (D4)
  providers.go    // Kind, Provider, Microsoft, Google, Providers(), ProviderFor — fixed set (D2)
  identity.go     // Identity (ported from ips oidc.go, unchanged)
  handlers.go     // Config, Handlers, New, Begin, Return — new
```

`Config` (handlers.go):

```go
type Config struct {
    Sessions     *sessions.Sessions // required
    InstanceKey  string             // required, >= 32 bytes (D4)
    Connections  func(ctx context.Context, kind Kind, key ConnectionKey) (Connection, bool, error) // required
    ConnectionKey func(r *http.Request) ConnectionKey // nil: always "" (single-tenant). Read ONCE, at Begin; Return uses the value sealed into the ceremony, never this hook again (D3/D4, finding 4)
    Admit        func(ctx context.Context, conn Connection, id Identity) (subject string, err error) // required (D5)
    SecondFactor func(w http.ResponseWriter, r *http.Request, sess sessions.Session) (done bool, err error) // nil: no second factor. Refused at New when set together with a Signed-mode Sessions (D5, finding 2)
    Refused      func(w http.ResponseWriter, r *http.Request, id Identity, reason error) // nil: a bare-text 403 naming nothing to an anonymous caller, ips D88.8's shape
    SignedInPath string             // default "/"
    Logger       *slog.Logger
    Now          func() time.Time   // nil: time.Now; test seam
    HTTP         *http.Client       // nil: this package's own default (15s timeout, no redirects — D2)
}
```

Two routes, app-mounted (`oidc` mounts nothing itself, same as `password`):

```
GET /signin/oidc/{provider}         -> h.Begin
GET /signin/oidc/{provider}/return  -> h.Return
```

**Error paths (finding 11), specified as a table rather than left to
whoever builds it first:**

| Condition | Response |
| --- | --- |
| `{provider}` not a known `Kind` | 404, no cookie touched |
| `Connections` returns an error (Begin or Return) | 500, logged with the error; no detail to the caller |
| `Connections` returns `ok=false` (Begin) | 404 — this app has no such connection |
| `Connections` returns `ok=false` (Return) — connection removed mid-ceremony | Same page as an expired ceremony, below; cookie cleared |
| Ceremony cookie missing, fails to open, or was sealed for a different `Kind` | One generic "that sign-in took too long — try again" page, cookie cleared, nothing provider-specific disclosed |
| Ceremony expired (inside the sealed blob) | Same page as above |
| Provider's callback carries `?error=...` | Same page as above; the provider's own error code is logged for an operator, never shown |
| Discovery or token exchange fails after the ceremony was built | Same page as above; `*oidc.Error`'s detail (ips's pattern: the provider's own words, bounded) logged for an operator |
| Token fails verification (`ErrToken`) | Same page as above |
| D3's core refusals (no address / denied-verified / hosted-domain mismatch) | Same page as above; THIS failure, and only this one, may be logged to an app's admission register per ips's D88.8 reasoning — see `Refused` |
| `Admit` refuses (`ErrNotAdmitted`) | `Config.Refused` is called with the identity and the reason; default is a bare 403 naming nothing beyond "not admitted" |
| `Admit` errors (not a refusal) | 500, logged |

Every row above clears the ceremony cookie. The design keeps ips's D88.8
split between "nobody learns why, because they're anonymous with an
interesting query string" and "the one refusal where naming the address
back is safe, because they just proved it's theirs to their own
organisation's directory" — the table above is where that split actually
lives, rather than in prose that a builder has to translate into cases.

## 2. Wiring into an app

```go
oidcHandlers, err := oidc.New(oidc.Config{
    Sessions:    sess,
    InstanceKey: instanceKey,
    Connections: myConnectionLookup,
    Admit:       myMembershipAndSubjectLookup,
    SecondFactor: passkeyHandlers.Gate.Hold,
})
r.Get("/signin/oidc/{provider}", oidcHandlers.Begin)
r.Get("/signin/oidc/{provider}/return", oidcHandlers.Return)
```

(Revision 1 wrote `passkeyHandlers.Gate` on its own — `Gate` is a
`*secondfactor.Gate` field on `passkey.Handlers`, not a function; the hook
needs its `Hold` method, `passkeyHandlers.Gate.Hold`. Corrected per the
review's finding 16.)

The sign-in screen partial's optional per-provider buttons (D1, D7) are a
`ui` task, scoped as a follow-up on this branch rather than blocking the
auth-layer review — see Tasks on the branch page.

## Not building

- **No account provisioning, no group/role mapping, no single log-out** —
  unchanged from revision 1, ported from ips's own "What is NOT built".
- **No `amr`/`acr` policy** — D6.
- **No Okta in this branch** — D2.
- **No automatic refresh / no refresh token** — D2.
- **No second factor in Signed (hub) mode** — D5, finding 2's third bullet.
  This is a real present-day limitation of `sessions`/`secondfactor`
  together, not something this branch can close; raised in Open questions
  for core.

## Testing

Per CONTRIBUTING rule 3, every behaviour needs a test shown failing before
the change and passing after.

1. **Ported fake-IdP suite** (ips's adversarial cases, adapted to the new
   `Connection`/`Admit` shape): algorithm confusion, multi-tenant
   placeholder issuer, domain-vs-GUID directory validation, `hd` present/
   absent/wrong, tri-state `email_verified`, state mismatch, replayed
   nonce, expired/not-yet-valid/future-issued tokens, key rotation.
2. **`FuzzTokenShape` and `FuzzClaims`** (D2) — the latter is new relative
   to anything ips has, and is this branch's actual answer to the
   discussion's fuzzing requirement, since a raw-bytes fuzzer alone barely
   reaches the post-signature code (review finding 12).
3. **Ceremony-cookie tests** — tamper with the sealed blob; a cookie
   sealed for one `Kind` presented at another's return path; an
   expired-inside-the-blob ceremony with a live `Max-Age`; two
   simultaneous ceremonies for different providers in two cookies not
   colliding; `SameSite=Lax` surviving the top-level-GET return.
4. **Session-minting tests, both modes, actually run rather than
   asserted** — a DB-backed `*sessions.Sessions` AND a Signed-mode one,
   confirming: the `Method` string is accepted by Signed mode's key-id
   alphabet; an `AuthTime` one second ahead of the server's own clock does
   NOT 500 in Signed mode (the clamp, D5); `New` refuses `SecondFactor` +
   Signed-mode together (D5's stated limitation, enforced rather than
   merely documented).
5. **`Admit`-before-`SecondFactor` ordering** (finding 6) — `Admit`
   refusing means `SecondFactor` is never called; asserted on a spy hook,
   not on the final status alone (this document's own standing rule about
   testing a refusal's reason, applied to itself).
6. **Hub tenant threading** (finding 4) — two `Connection`s under the same
   `Kind` with different `ConnectionKey`s; a ceremony begun under one key
   resolves the SAME key at Return with nothing taken from the provider's
   callback; a `Connections` lookup that returns `ok=false` at Return
   (simulating a tenant removed mid-ceremony) hits the table's row for it.
7. **Interop checklist** (`oidc/INTEROP.md`) — written this branch, run
   against live Entra and Google tenants before merge, outcome posted to
   a discussion. The one piece of evidence this spec cannot manufacture
   itself.

## Open questions

1. **For core, not for this branch: `secondfactor`'s `Method` suffix
   composition is incompatible with Signed mode's key-id alphabet**
   (D5, finding 2's third bullet). Any first factor built after Signed
   mode landed inherits this; `oidc` works around it by refusing the
   combination rather than shipping something broken, but the actual fix
   — widening Signed mode's alphabet, or giving `secondfactor` its own
   narrower one that both sides agree on — belongs to `sessions`/
   `secondfactor`, and is worth its own discussion regardless of whether
   this branch merges.
2. **Who runs the real-tenant interop checklist, and when.** Unchanged
   from revision 1: this spec can ship the checklist and the fake-IdP
   suite that exercises the same code paths, not a live-credentialed run.
3. **Should `oidc.Config.Admit`'s hard requirement (no default) be the
   house style for every third-party-identity package core adds**, given
   `auth.Config.Authorize` itself defaults open? Unchanged from revision
   1 — still want a person's explicit read rather than letting the
   inconsistency drift.

## Review log

**Model.** Written by Claude Sonnet 5 (`claude-sonnet-5[1m]`), "high"
reasoning effort, inside Claude Code — not the strongest model available to
this account (Claude Opus 5.5 is). Revision 1's stated reason for that
choice (a long session already holding ips's implementation and this
repository's conventions in context) still applies to revision 2, which is
a repair of findings surfaced by review rather than a from-scratch design.
The actual CODE for this branch, and the review of it, should run at this
account's strongest tier — unchanged commitment from revision 1.

**Cross-provider review attempted, found unreachable.** `codex` is
installed; `codex login status` reports "Logged in using ChatGPT". Every
model tried — `gpt-6-astra` (the pattern from #129/#122/#113), `gpt-6.1-sol`,
`gpt-5.6-sol` (this account's own `~/.codex/config.toml` default), and
plain `gpt-5` — was refused identically: `"The '<model>' model is not
supported when using Codex with a ChatGPT account."` This looks like a
subscription-tier gap on this account rather than a wrong model name.
Written up in [discussion #139](https://amadan.net/rastrillo/rastrillo/-/branch/oidc-signin)
with the exact commands and errors. Per CONTRIBUTING rule 2's own fallback
— "If you cannot reach another provider, say why in the discussion and
stop. A person must approve a same-provider review on the branch before it
merges" — this is said there, and this branch should not merge on the
review below alone without that approval.

**Same-provider review (interim, pending the approval above).** A fresh
Claude Opus 5.5 session, no shared context with the session that wrote
revision 1, briefed adversarially and told to check every claim against
this checkout's actual code rather than take the spec's word for it. Full
prompt and reply posted verbatim as [a discussion on this
branch](https://amadan.net/rastrillo/rastrillo/-/branch/oidc-signin) (see
the branch's Discussions tab for the exact thread — posted alongside this
commit). Sixteen findings; disposition of each:

| # | Severity | Finding | Disposition |
| - | - | - | - |
| 1 | Blocker | No subject-minting hook; `Authorize` had no way to say who the session is for; breaks Signed mode's opaque-id rule and `secondfactor`'s `Enrolled` lookup | **Fixed** — D5, `Admit` replaces `Authorize` |
| 2 | Blocker | `Method` string, `AuthTime` ordering, and `+factor` composition all break in Signed mode; "no branch in oidc" overclaimed | **Fixed** (first two) / **documented as a core limitation, out of this branch's scope** (third) — D5 |
| 3 | Blocker | Address/admission rule cited from ips but never stated as this package's own, and `Connection` had no domain field | **Fixed** — D3 |
| 4 | Major | No way to carry the tenant through to Return in hub mode | **Fixed** — D3/D4, `ConnectionKey` sealed into the ceremony |
| 5 | Major | JWKS/discovery caching keyed by provider kind, not by connection — cross-tenant key confusion risk | **Fixed** — D3, caches rekeyed by issuer/jwks_uri |
| 6 | Major | Order of `Authorize`/`Admit` vs. `SecondFactor` unspecified; a route to an unauthorised session | **Fixed** — D5, normative order + test |
| 7 | Major | "Why" overclaimed that this cuts access when someone is disabled at the provider | **Fixed** — Why section rewritten |
| 8 | Major | Step-up/reauth undefined; `AuthTime` could be stale or ahead of the server's clock | **Fixed** — D6 |
| 9 | Major | Ceremony cookie's attributes and lifecycle unspecified; `InstanceKey` had no length floor | **Fixed** — D4 |
| 10 | Minor | `return_to` not carried through the ceremony | **Fixed** — D4, `ReturnTo` field |
| 11 | Minor/major | Error paths ambiguous enough that two builders would differ | **Fixed** — §1, the error-path table |
| 12 | Minor | Fuzz target as described barely reaches the claims parser; "seven" shapes were actually five | **Fixed** — D2, `FuzzClaims` added |
| 13 | Minor | `form-action`/`response_mode` reasoning was wrong (conflated two different things) | **Fixed** — D7 |
| 14 | Minor | Token/key hygiene rules implied by "port near-verbatim" rather than stated | **Fixed** — D2 |
| 15 | Minor | `Begin` is an unauthenticated state-changing GET with no PoW and unspecified discovery caching | **Fixed** — discovery caching in D3's cache rework; PoW's absence is a deliberate, now-stated decision (the resource `pow` defends — another address's quota — is not spent here) rather than a silent gap |
| 16 | Minor | D2's registry-vs-fixed-set self-contradiction; `passkeyHandlers.Gate` is a field not a function; `InstanceKey` type mismatch; a garbled phrase | **Fixed** — D2 (fixed set decided), §2 (`.Gate.Hold`), D4 (`string`), and the phrase is gone in this revision |

Two of the review's own citations were independently re-verified against
the checkout before being accepted here (findings 1 and 2's most
load-bearing claims: `sessions/signed.go`'s `identifier(c.Sub,128)` and
`validKeyID`, and `secondfactor/secondfactor.go`'s `Enrolled`/`Hold`
contract) — this document does not ask a reader to trust a sub-review's
transcript any further than this spec's own author did.

**Still pending before this spec is ready for code:** Tony's approval to
proceed on a same-provider review per the fallback rule above, and a
decision on Open question 1 (whether the Signed-mode/`secondfactor`
incompatibility this branch surfaces is worth raising as its own core
discussion before or alongside this one).
