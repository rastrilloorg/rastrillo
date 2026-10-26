# auth: sign in with an OpenID Connect provider

Answers discussion [#134](https://amadan.net/rastrillo/rastrillo/-/discussion/134).
Built with Claude Sonnet 5 (`claude-sonnet-5[1m]`, "high" effort in Claude
Code) — not the strongest model this account can reach; see "Model" under
Review log for why, and the escalation this spec proposes before merge.

## Why

A firm that already runs Microsoft Entra ID or Google Workspace wants its
staff to sign in with that account, and wants turning someone off there to
cut their access here too. `auth` offers magic links, keymail, passwords and
passkeys — all first factors this application vouches for itself. None of
them lets a THIRD PARTY the app's operator trusts vouch for an address
instead.

ips built exactly this against two providers (`internal/oidc`,
`docs/design/88-single-sign-on.md`, shipped 2026-09-07) as a from-scratch
relying party: ~300 lines over the standard library, one signature
algorithm, no JOSE dependency. The ips code and design are the source
material this spec ports from; its reasoning is cited throughout rather
than repeated, and where this spec departs from it the departure is
explained.

## Decisions

### D1 — A sibling package, not inside `auth`

**Proposed. Rejected: folding the verification and the HTTP flow into
`package auth` itself.**

The discussion's "Why core" argument is right that this cannot be an addon
— sign-in touches sessions, return-to handling and the sign-in screen, and
an addon would have to wrap all three. But core already has a precedent for
"a first factor beside `auth`'s own, sharing `sessions.Sessions`": `password`
is not inside `package auth`, it is `package password`, built with
`Config.Sessions *sessions.Sessions` and its own `Config.SecondFactor` hook,
calling `cfg.Sessions.SignIn` on success exactly where `auth.Auth.Callback`
does (`password/handlers.go:95-97,116-123,474-499`). Nothing in `password`
imports `auth`, and nothing in `auth` imports `password`; both import
`sessions`, which is the actual seam.

OIDC fits the same shape with no friction: it is `package oidc` (root,
alongside `password` and `passkey`), sharing `*sessions.Sessions` with
whatever else the app has wired, with its own `Config.SecondFactor`. An app
using both password and OIDC gives both packages the same
`passkey.Handlers.Gate` as their `SecondFactor`, exactly as it would today
for password and keymail.

This also settles the "new code in core, not reuse" framing precisely:
`sessions`, `crypto.SealSym`/`crypto.Derive`, `pow`, and the session-minting
contract are REUSE; the token verification is the ~300 lines that are
genuinely new, and they get their own package so the addon rule's "does not
re-implement sessions, CSRF, migrations, forms or flash" is kept true by
construction rather than by discipline.

### D2 — The verification core is ported close to verbatim

**Proposed.**

`internal/oidc/{oidc.go,verify.go,start.go}` in ips is already
framework-agnostic — "it holds no database handle, opens no session and
knows nothing about this product" (ips, `oidc.go:6`) — and its narrowness
IS the security property (RS256-only refused by name; no refresh token; no
userinfo call; https with no exception). None of that is ips-specific. This
spec proposes porting those three files near-verbatim into
`oidc/{discover.go,verify.go,ceremony.go}`, keeping every one of ips's
documented refusals:

- One algorithm, RS256, every other value — `none` and every HMAC family —
  refused by name (ips `verify.go:105-107`).
- The discovery document's own `issuer` must equal what it was fetched
  from, and an issuer containing `{` or `}` is refused outright — this is
  what stops a multi-tenant endpoint like Microsoft's `/common` from
  admitting every tenant in the world (ips `oidc.go:312-388`).
- `state`/`nonce`/PKCE every time; `iss`/`aud`/`azp`/`exp`/`nbf`/`iat`
  checked with a fixed 2-minute clock-skew allowance (ips `verify.go`,
  `skew` constant).
- JWKS cached an hour; an unknown `kid` triggers one unconditional refetch,
  and a signature that fails against the cached key gets exactly one retry
  against a freshly-fetched set before being refused (ips
  `verify.go:309-428`). This is bounded-retry, not a cooldown timer — see
  the ips-questions reply on #134 for why "rate-limited" oversells what
  ips actually built, and this spec keeps the honest version rather than
  inventing a rate limiter nothing here needs.
- `AddressVerified` stays a tri-state (`Unstated`/`Asserted`/`Denied`):
  Entra emits no `email_verified` claim at all, and folding silence into
  either direction is a false assertion in one case (ips
  `oidc.go:130-137`, `verify.go` `flexBool`).
- The address is taken from exactly one `@`, checked at both ends, because
  an address with two `@`s satisfies a careless parse at either end and
  would let an attacker's own address impersonate someone on a domain
  allowlist (ips `verify.go:220-242`, `address`).

What changes from ips, and why each change is made:

- **The provider list becomes a registry other packages append to, not a
  closed `var providers = []Provider{...}` inside the package.** ips's
  closed list (D88.2 in its design) is the right call for ONE app
  deciding which directories it will trust — but rastrillo ships this to
  every app, and the discussion itself names three candidate providers
  (Entra, Google, Okta) where ips implements two. Hard-coding Okta into
  core on spec alone, with no app having built and proven it, is exactly
  the thing D88.2 argues against for an app's *issuer field*: a provider
  added without "somebody writing down what a firm has to go and get" is
  a worse version of the free-text box D88.2 already refused. So `oidc`
  ships with Microsoft and Google — the two ips has running in
  production — as the seeded registry, and `oidc.Register(Provider)`
  lets an app, or a later core branch, add a third with the same
  reviewed shape ips used: a `Kind`, an issuer builder, a directory-value
  validator, and the `NeedsHostedDomain` flag where the issuer is shared
  across customers. **Open question for Astra and for Tony**: is a
  package-level mutable registry (`sync.Once`-guarded, panics on a
  duplicate `Kind`, called from `init()` or boot) an acceptable shape for
  core, or should the three ips knows about (Microsoft, Google, and a
  speculative Okta nobody has run) simply be the fixed set, with a fourth
  requiring a core release the way the discussion's own "a third is a
  row in that list plus the tests beside it" (ips D88.2) already
  describes? I lean to the fixed-set answer — a registry invites an app
  calling `Register` with an unreviewed provider of its own, which is the
  free-text box again with extra steps — and propose shipping Microsoft
  and Google only, with Okta explicitly OUT of this branch's scope
  (tracked as a follow-up discussion once somebody can test it against a
  real Okta tenant), but I'm flagging the alternative because the
  discussion's title implies all three.
- **A Go native fuzz target is added for the token parser**
  (`FuzzVerifyIDToken` in `oidc/verify_fuzz_test.go`), which ips does not
  have — its 19 `Test*` functions are hand-written adversarial cases over
  a fake IdP on a real loopback TLS listener, not a fuzzed corpus. The
  discussion's spec item asks for fuzzing explicitly; this closes that
  gap rather than silently inheriting ips's lack of one. The seed corpus
  is the seven malformed shapes ips's table tests already construct
  (truncated JWT, non-JSON header, non-JSON claims, wrong part count,
  oversized body) plus a handful of boundary byte strings; `go test
  -fuzz=FuzzVerifyIDToken -fuzztime=5m` is run as part of this branch's
  before/after evidence and left in the suite as a short, bounded corpus
  run (`-fuzz` is not part of `make ci`'s default `go test ./...`, so a
  short deterministic replay of the saved corpus is what CI actually
  runs; the open-ended fuzz run is a maintainer/CI-extra step, named in
  `oidc`'s own `SKILL.md`-equivalent reference page).
- **No real Entra/Google interop test runs in `make ci`, and this is
  stated rather than implied.** Neither does ips's. A reviewer should not
  read "ported from code with production experience" as "interop-tested
  in this repository" — it isn't, in either repository. What this spec
  proposes: the ported fake-IdP suite (real TLS, real RS256 signing, the
  exact claim shapes both providers publish — multi-value `aud`, string
  vs. array `email_verified`, Microsoft's placeholder-issuer discovery
  document, Google's `hd`) runs in `make ci`; a SEPARATE, manual interop
  checklist (`oidc/INTEROP.md`) with the exact steps to register a test
  app at Entra and at Google and run a real sign-in is written for this
  branch, and its outcome against both providers is posted to a
  discussion on this branch before merge, by whoever can reach live
  tenants — most likely Tony, who already has both configured for ips.
  A checklist that has never been run is a FutureRelatedNotice to resolve
  before this leaves the spec stage; see Testing below.

### D3 — Connections are a caller-supplied lookup, not a core-owned table

**Proposed. Rejected: a core migration and a `Connections` table, the
shape ips used (`SSOConnection`, `internal/ips/ssomodels.go`).**

ips stores one sealed row per provider per instance, because ips's
"instance" and "firm" are the same thing. rastrillo has no such guarantee —
an app may be single-tenant like ips, or it may be the kind of hub product
`sessions`' signed mode now exists for (one process, several tenants, no
per-tenant database at all; `objrec` records mode). A core table would
force every app into ips's shape and give a hub app nothing to key it by.

So `oidc.Config` takes a lookup, mirroring `password.Config.Lookup` exactly:

```go
// Connections resolves a provider kind to this app's registration with
// it. ctx carries whatever the app needs to pick a tenant (a hub reads
// its own host-to-tenant mapping from r's context upstream of this
// call); ok=false means this app has no connection for that provider and
// Begin answers 404 rather than starting a ceremony nothing can finish.
Connections func(ctx context.Context, kind Kind) (Connection, bool, error)
```

A single-tenant app returns a fixed `Connection{...}` built from its own
config at boot (an env var, a secret store, whatever ips-style sealing it
wants — none of that is core's business, same as `password.Lookup` not
caring whether the hash comes from a table or an LDAP bind). A hub app
reads the tenant off the request's own resolved identity and returns that
tenant's row. This answers the discussion's "Config: one provider or
several? Per tenant?" directly: both, and the plugin is agnostic to which,
exactly as `password` is agnostic to how an app stores its hashes.

`Connection` carries what ips's `Config` carries (`Issuer` built from a
`Directory` value via `Provider.Issuer`, `ClientID`, `ClientSecret`,
`RedirectURI`) — the fields are unchanged from ips's `oidc.Config` (renamed
here to `Connection` since `Config` is now the `oidc.New` configuration,
matching `password`'s and `auth`'s own `Config`/handlers-struct split).

### D4 — The ceremony cookie reuses `crypto.SealSym`, not a new AEAD

**Proposed. Rejected: a bespoke seal, as ips wrote (`internal/ips` does
not have `crypto.SealSym` available to it; rastrillo does).**

ips's D88.5 (ceremony state in a sealed cookie, no table, bound to the
provider as AEAD context, expiry inside the sealed blob and checked against
the clock rather than trusted from `Max-Age`) is exactly right, and every
reason ips gives transfers unchanged — including the one correctness bug
ips found and fixed, that `Max-Age` alone makes the TTL a property of the
browser rather than of the server. rastrillo already has the primitive this
needs: `crypto.SealSym(key, plaintext)` / `crypto.OpenSym`, and
`crypto.Derive(key, context)` for binding a key to a label. So the ceremony
blob is:

```go
type ceremony struct {
    State, Nonce, Verifier string
    Provider                Kind
    Expires                 time.Time
}
```

JSON-marshalled, sealed with `crypto.SealSym(crypto.Derive(instanceKey,
"rastrillo/oidc/ceremony/v1"+string(provider)), plaintext)`, base64url into
a `SameSite=Lax` cookie (ips's reasoning: the return leg is a top-level GET
from the provider's origin, which `Strict` would drop). Deriving the key
per-provider, as ips does by using the provider as the AEAD's additional
data, is folded into the label here since `crypto.SealSym` does not take
separate AAD — functionally identical: a ceremony sealed for "microsoft"
cannot be opened by the "google" return path because the KEYS differ, not
because one key's AAD differs.

### D5 — Session minting goes through `sessions.Sessions.SignIn`, which is also the hub answer

**Proposed.**

The discussion's hardest spec item — "on a hub product, the callback runs
at the hub and produces the same signed session as 5aad3df1, not a second
kind of session" — is already solved by D1 and D3 together, and needs no
OIDC-specific code: `oidc.Handlers` is handed a `*sessions.Sessions` at
construction, exactly like `password.Handlers` is. `sessions.Sessions`
itself already carries the DB-mode/Signed-mode distinction (`sessions.Config.Signed`,
landed in 5aad3df1); calling `.SignIn(w, r, sessions.Session{Subject: ...,
Method: "oidc:" + string(kind), AuthTime: identity.AuthTime})` produces
whichever kind of session the `*sessions.Sessions` the app built was
configured for. A hub app wires `oidc.New` with the SAME `*sessions.Sessions`
it built in Signed mode for its own hub sign-in; a per-tenant app wires it
with an ordinary DB-backed one. `oidc` never branches on which — that
branch already lives inside `sessions`, which is the whole point of the
seam existing.

The `Method` string follows ips's D88.9 reasoning (a shared second-factor
handler must not assume which first factor ran) by deriving from the
session's own field rather than a hard-coded constant anywhere downstream:
`"oidc:microsoft"` / `"oidc:google"`, so an app's own sign-in register can
split by provider without a second column, and `passkey`'s `+passkey`/
`+totp` suffixing (already how `auth` and `password` compose their own
`Method` strings before handing to `SecondFactor`) continues to work
unmodified — `oidc` writes the base `Method` exactly where `password`
writes `"password"` and `auth`'s own flow writes its own string, before
`SecondFactor` ever runs.

### D6 — `amr`/`acr` is recorded, and consumed by nobody in core

**Proposed, porting ips's D88.4 verbatim as a core design decision rather
than an app policy.**

A provider MAY say what it did to authenticate somebody (`amr`). Entra
usually does; Google mostly does not; a claim present for some tenants and
silently absent for others is not evidence a security posture can rest on.
`Identity.Methods` carries the raw claim for an app to log or display, and
nothing in `oidc` or in the `SecondFactor` contract reads it to decide
whether a second factor is required. This is deliberately NOT exposed as an
app-configurable option (no `Config.TrustAMR bool`): the discussion's own
"Checks" list does not ask for one, and ips's design argues in detail
(D88.4) for why the natural "firm configures a checkbox" shape is actually
a firm's configuration standing in for a verified fact — the same argument
against an issuer free-text box, one level up. If this is ever revisited,
the shape that would work is refusing the sign-in outright unless `amr`/
`acr` names a value the app configured, never a flag that widens trust on
its own say-so; recorded here so nobody re-derives the argument from
scratch.

### D7 — `prompt=select_account`, `response_mode=query`, no exceptions

Ported from ips unchanged, with ips's reasoning kept: `select_account` so a
person already signed in to a personal account in the browser is not
silently admitted as that account (ips `oidc.go:415-429`); `query` rather
than `fragment` (never reaches the server) or `form_post` (a cross-origin
POST into this application, which `csrf.Protect`/`form-action` would have
to special-case — ips's own D88.6 "the start is a LINK" rediscovery applies
here identically, since rastrillo sets the same `form-action 'self'`
policy, `serve.go`).

## 1. `oidc` — the package

```
oidc/
  discover.go     // Discover, Metadata, Client (ported from ips oidc.go)
  verify.go       // Verify, KeySet, claims, Tristate (ported from ips verify.go)
  verify_fuzz_test.go
  ceremony.go     // Start→ceremony rename: NewStart, Challenge, CheckState (ips start.go) + seal/open using crypto.SealSym
  providers.go    // Kind, Provider, Microsoft, Google, Providers(), ProviderFor (ported from ips, closed set per D2)
  handlers.go     // Config, Handlers, New, Begin, Return — the HTTP wiring, new
  identity.go     // Identity (ported from ips oidc.go, unchanged)
```

`Config` (handlers.go, new):

```go
type Config struct {
    Sessions    *sessions.Sessions          // required, shared with the rest of the app
    InstanceKey []byte                      // required, seals the ceremony cookie (crypto.Derive)
    Connections func(ctx context.Context, kind Kind) (Connection, bool, error) // required
    Authorize   func(ctx context.Context, id Identity) (ok bool, reason string) // required — D88.8's membership question
    SecondFactor func(w http.ResponseWriter, r *http.Request, sess sessions.Session) (done bool, err error) // nil: no second factor
    Refused      func(w http.ResponseWriter, r *http.Request, id Identity, reason string) // nil: a bare-text 403, same default as auth.Config.Refused
    SignedInPath string                     // default "/"
    Logger       *slog.Logger
    Now          func() time.Time           // nil: time.Now; test seam
    HTTP         *http.Client               // nil: oidc.Client's own default (15s timeout)
}
```

`Authorize` is required, not optional-with-an-admit-everyone default, unlike
`auth.Config.Authorize` (nil admits every verified address there). ips's
D88.8 is explicit that a verified token is not an admission — "a guest, a
contractor or a former colleague with a live account is in the tenant and
not at the firm" — and that is true of every third-party IdP, not an ips
policy choice. Core should not let an app wire this up and forget the
membership question the way a nil default would silently allow; `New`
returns an error if `Authorize` is nil, the same shape `auth.New` already
uses for `Proof`/`ProofOff` to force a conscious choice rather than a
silent gap.

Two routes, app-mounted exactly as `password`'s and `auth`'s are (`oidc`
mounts nothing itself):

```
GET /signin/oidc/{provider}         -> h.Begin
GET /signin/oidc/{provider}/return  -> h.Return
```

`{provider}` is read with `chi.URLParam`, parsed as a `Kind`, and an unknown
value 404s before anything else runs (`ProviderFor`'s existing refusal,
ported). `Begin` resolves the connection, builds a `ceremony`, discovers
the metadata, seals the cookie, 303s to the authorization endpoint — ips's
whole "the wire" sequence (`docs/design/88-single-sign-on.md`, "## The
wire"), unchanged in shape.

## 2. Wiring into an app

```go
oidcHandlers, err := oidc.New(oidc.Config{
    Sessions: sess,
    InstanceKey: instanceKey,
    Connections: myConnectionLookup,
    Authorize: myMembershipCheck,
    SecondFactor: passkeyHandlers.Gate,
})
r.Get("/signin/oidc/{provider}", oidcHandlers.Begin)
r.Get("/signin/oidc/{provider}/return", oidcHandlers.Return)
```

The sign-in screen's partial (`ui/`) grows an OPTIONAL per-provider button
block, off by default — `auth.Config.SigninScreen`-style opt-in, fed by
`oidc.Providers()` filtered to whatever the app's `Connections` lookup
actually answers for (a Google-only app does not show a Microsoft button
that 404s). This is a `ui` change, not an `oidc` one, and is scoped as a
follow-up task on this branch rather than blocking the auth-layer review —
see Tasks on the branch page.

## Not building

Ported from ips's own "What is NOT built" (D88, the register entry pattern
AGENTS.md and ips's own rules require — stated here rather than left to be
inferred):

- **No account provisioning.** An address must already be one `Authorize`
  admits. A directory that could create accounts here would make whoever
  administers the tenant an administrator of this framework's apps.
- **No group or role mapping.** What somebody may do is the app's grant,
  never read from the provider's token or asked for in scope.
- **No single log-out.** Signing out ends the session here and nothing at
  the provider — the ordinary behaviour of every relying party, and true
  of ips's own shipped feature (see the #134 reply).
- **No `amr`/`acr` policy** — D6, above.
- **No Okta in this branch** — D2, above; a follow-up discussion once
  somebody can test against a real tenant.
- **No automatic token refresh / no refresh token at all** — D2's port of
  ips's D88.1: this flow establishes identity once, nothing here acts at
  the provider afterwards, so there is nothing long-lived to keep.

## Testing

Per CONTRIBUTING rule 3, every behaviour here needs a test shown failing
before the change and passing after. Plan, in the order the branch will
produce the evidence:

1. **Ported fake-IdP suite** (ips's 19 tests, adapted to the new package
   names and the `Connection`-lookup shape) — algorithm confusion
   (`none`, every HMAC family, RS256-signed-as-HS256-with-the-public-
   modulus), multi-tenant placeholder issuer, domain-vs-GUID directory
   validation, `hd` present/absent/wrong, tri-state `email_verified`,
   state mismatch, replayed nonce, expired/not-yet-valid/future-issued
   tokens, key rotation (cache miss → refetch → match).
2. **`FuzzVerifyIDToken`** — new, not in ips. Seed corpus from the above;
   run `-fuzztime=5m` as this branch's own before/after evidence (a crash
   found pre-fix, the same input passing clean post-fix, or — if nothing
   is found — the corpus and the run's own output posted as the
   "behaviour that already works" case CONTRIBUTING rule 3 allows to skip
   a fail-first requirement).
3. **Ceremony-cookie tests** — tamper with the sealed blob, use a cookie
   sealed for a different provider on another's return path, present an
   expired-inside-the-blob ceremony with a live `Max-Age` (the exact ips
   first-cut bug, D88.5), confirm `SameSite=Lax` survives the top-level-GET
   return.
4. **Session-minting tests, both modes** — a DB-backed `*sessions.Sessions`
   and a Signed-mode one built the way `5aad3df1`'s own tests build it,
   asserting the SAME `Return` handler produces a DB session in one case
   and a hub-signed cookie in the other, with no branch in `oidc` itself —
   this is the test that actually proves D5 rather than asserting it in
   prose.
5. **The membership gate** — `Authorize` returning false after a token
   verifies; `New` refusing a nil `Authorize`.
6. **Interop checklist** (`oidc/INTEROP.md`) — written this branch, RUN
   against live Entra and Google tenants before merge, outcome posted to
   a discussion here. This is the one piece of evidence this spec cannot
   manufacture itself; flagged again under Open questions.

## Open questions

1. **Fixed provider set vs. a `Register` registry** — D2. Leaning fixed
   (Microsoft + Google only, this branch), want Astra's and a person's
   read on whether a registry is a foreseeable hole.
2. **Who runs the real-tenant interop checklist, and when.** This spec
   cannot self-certify interop; it can only ship the checklist and the
   fake-IdP suite that exercises the same code paths. Tony already has
   both Microsoft and Google connections configured for ips's production
   use and is the most likely person who can run this before merge.
3. **Should `oidc.Config.Authorize`'s hard requirement (no nil-admits-all
   default) be the house style for every NEW third-party-identity package
   core adds**, given `auth.Config.Authorize` itself defaults open? I
   propose yes for anything whose identity comes from outside the app
   (D88.8's actual argument — a verified token is not an admission — is
   about the SOURCE of the identity, not about `auth` specifically), but
   `auth`'s own default is unchanged by this branch and the inconsistency
   is worth a person's explicit sign-off rather than silently drifting.

## Review log

**Model.** Written by Claude Sonnet 5 (`claude-sonnet-5[1m]`), "high"
reasoning effort, inside Claude Code. This is not the strongest model
available to this account generally — Claude Opus 5.5 is — and CONTRIBUTING
rule 1 asks for the strongest model and a stated reason otherwise. The
reason: this spec was produced inside a long session that had already read
ips's full implementation, its design document, and this repository's own
`auth`/`password`/`sessions`/`crypto` conventions, in context; re-running
that research under a fresh Opus session would have cost more than the
marginal quality difference on a SPEC (as opposed to the hand-written
crypto itself) is likely worth. **The code in the next phase of this
branch, and the adversarial review of both, should run at this account's
strongest available tier** — flagging that commitment here rather than
deciding it silently when that phase starts.

**Cross-provider review.** Not yet run. This section is appended to once
`codex exec -m gpt-6-astra -c model_reasoning_effort=high -s read-only` has
reviewed this document, following the pattern in discussion #129.
