# pow grows up: admission that commits with the write, and sign-in behind it by default

Status: design approved in conversation 2026-10-03, section by section,
with the operator's decisions marked **(Paul)**. Not yet adversarially
reviewed. Nothing is implemented.

Citations: `T/` is Tito Go (`github.com/tito/titogo`, origin/main
`dd3b53df3`); everything else is this repo at `0aca6edb`.

## Why

Every app in the family has a public form, and rastrillo already ships
the front door for one: `pow` (7814ebea, 2026-09-05), extracted from
movement "so a second app cannot copy it". Nobody adopted it. movement
and correomona never switched, and two weeks later Tito Go built its own
— `T/internal/intake` plus `T/internal/instance/intake_*.go`, designed in
`T/docs/superpowers/specs/2026-09-18-intake-design.md` — which is now
the third hashcash implementation in the family (with
`T/internal/carlos/pow.go` and keymail's `internal/pow`).

Tito did not copy out of ignorance. It hit four things `pow` cannot do:

1. **Spend the token in the caller's transaction.** `pow.Check` spends
   the nonce before the handler writes anything (pow/guard.go:139-180).
   A validation error afterwards ("you missed a field") has burned the
   visitor's solved proof; a write that fails or rolls back leaves the
   token spent with nothing to show for it. Tito's `intakeAdmit` decides
   and writes nothing; `adm.Commit(tx)` spends inside the handler's own
   transaction (T/internal/instance/intake_gate.go:7-12, 316-338), so a
   retry keeps its token and two concurrent posts resolve to one success.
2. **Bind to a form, not an address.** `pow` binds the work to the
   submitted address, so solving cannot start until submit. Tito binds
   the token to door and event and solves at page load. The address
   binding goes stale under autofill and typo correction, and the wait
   after the click is what made Tito turn proof of work off on checkout.
3. **A token-only tier.** Tito's checkout runs at zero bits — sealed
   single-use token and honeypot, no proof (intake_gate.go:59-81).
   `pow` turns any difficulty ≤ 0 into the default.
4. **Scope.** Tito seals door and event into the token, so a token minted
   for one form cannot be spent on another. `pow`'s seal covers nonce,
   time and difficulty only (pow/challenge.go:68-72).

Meanwhile rastrillo's own sign-in and sign-up have no anonymous-abuse
control beyond in-memory counters: `auth.Begin` (auth/handlers.go:41)
sends mail and makes the server probe a domain the visitor chose, behind
a per-process fixed window (auth/auth.go:413); `password.Signup`
(password/handlers.go:265) creates a user row per fresh address and
counts only failures (password/limit.go:44-46). The sign-in screen spec
already names the gap and defers it: "an admission pre-check before
classification … worth upstreaming"
(docs/superpowers/specs/2026-09-27-signin-screen-design.md:1001-1005).

## Decisions

- **Grow `pow`; do not add a package.** The browser half and the Go half
  must agree byte for byte, and only one module can ship both
  (pow/assets.go). A second package would be the fourth copy.
- **The interface may break.** Nothing outside `pow/` imports it
  (only `buildhandler_test.go`, for the CSP hash). Tito Go is the first
  real caller, and it migrates after this lands.
- **Unbound by default, binding opt-in (Paul).** With single-use nonces,
  one solve already buys exactly one submission whatever address it
  carries, so the address binding adds almost nothing; what it costs is
  that solving cannot start before submit. A form can still opt in.
- **Sign-in and sign-up are behind it by default (Paul).** `auth.Begin`,
  `password.Signin` and `password.Signup`.
- **Proof of work is required, so those forms need JavaScript (Paul).**
  Tito's argument holds here: if a missing proof is let through, every
  bot takes that path and the control protects nobody. The form says so
  plainly. An app can drop to token-only or off.
- **Default on, and a boot error until wired (Paul).** An app that
  renders its own form cannot get the fields automatically, and an
  upgrade that silently refused every sign-in would be a production
  lockout. So the zero value of the new config is an error from
  `auth.New` / `password.New` naming the fix: wire the assets, or switch
  it off explicitly. Loud in CI, never quiet in production.
- **Every refusal is visible.** Including the honeypot. A real visitor
  whose browser filled the trap must see a reason and a way forward,
  never a cheerful "check your email" for a link that was not sent —
  Tito's live bug (intake design, "The honeypot has an autofill name").

## 1. `pow`

### Config

```go
type Config struct {
	InstanceKey string     // required, as today
	Nonces      NonceStore // required, as today
	Name        string     // required, new: sealed; one form's token never opens another
	Difficulty  int        // default DefaultDifficulty; NoProof = token + honeypot only
	Bind        bool       // new: bind the work to [data-pow-binding]; default false
	MinAge      time.Duration
	MaxAge      time.Duration
	Attempts    int        // new: failed posts per admitted token; default 5
}

const NoProof = -1
```

`New` refuses an empty `Name` (`ErrNoName`). One `Guard` per form: a
form's name, difficulty and binding are properties of the form, and
Tito's per-door table (`intakeBits`) is exactly a set of Guards.

### The seal

Covers version, name, scope, nonce, issued-at and difficulty:

    HMAC(key, "pow/v2\x00" + name + "\x00" + scope + "\x00" + nonce + "\x00" + issued + "\x00" + difficulty)

`scope` is not posted. The server supplies the scope it expects at
admission, so a token minted for event 41 does not verify on event 42
and the form carries nothing the submitter could edit. A name or scope
mismatch is `ReasonSealInvalid`: one refusal for every way a token can
be wrong, so a prober learns nothing from the difference (Tito's
`intake.Open`, same reason).

The v1 seal stops verifying on upgrade. With no callers that costs
nothing; it is recorded for Tito's migration (§5).

### Issue

```go
func (g *Guard) Issue(now time.Time, scope string) Challenge
```

Still writes nothing (pow/challenge.go:19-25 — a row per render makes
every crawler a write stream). A confirm-shaped screen, where the
visitor has nothing left to type, backdates: `Issue(now.Add(-MinAge),
scope)`. That is Tito's `intakeMintFollowOn`
(intake_gate.go:143-174) as documentation, not API.

### Admit and Commit

```go
type Want struct {
	Scope   string
	Binding string // required when Config.Bind; ignored otherwise
}

type Admission struct {
	OK     bool
	Reason Reason   // the first failure
	Also   []Reason // every other failure, for the log
	// unexported: guard, nonce, expiry
}

func (g *Guard) Admit(r *http.Request, w Want) Admission
func (a Admission) Commit(ctx context.Context, ex Execer) error
func (g *Guard) Check(r *http.Request, w Want) (Reason, bool) // Admit, then Commit on the store's own handle

type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

var ErrSpent = errors.New("rastrillo/pow: challenge already spent")
```

`Admit` runs, in order: body bounds → challenge fields present
(`ReasonMissing`, new) → honeypot → seal → clock → proof of work (skipped
at `NoProof`) → already-spent lookup → attempt cap (`ReasonAttempts`,
new). It writes nothing. The order keeps today's reasons (guard.go:123-133):
free checks first, the signature before the timestamp it vouches for.
After the first failure the remaining *cheap* checks still run and land in
`Also` — what a refusal was also wrong about is how a false positive gets
recognised (intake_gate.go:251-253) — but nothing that reads the database
runs after a failure.

`ReasonMissing` is its own reason because it means something different
from a bad proof: the form was never wired. It is the one an operator
should see first after an upgrade.

`Commit` spends the nonce with `INSERT OR IGNORE` on whatever `ex` the
caller passes. `*sql.Tx`, `*sql.DB`, `*sql.Conn` and a GORM transaction's
`tx.Statement.ConnPool` all satisfy `Execer`, so `pow` does not import
GORM. Zero rows affected is `ErrSpent`: the handler lost a race to an
identical request and must refuse, not 500. `Commit` on an admission that
was not OK returns `ErrNotAdmitted` rather than spending nothing
silently — a handler that went on past a refusal would otherwise create
its row with nothing consumed (Tito's `Commit` returns nil there,
intake_gate.go:321-326, and relies on no door doing it).

The consequences, each a test:

- a validation failure never reaches `Commit`, so the token and the
  solve survive the retry;
- a rolled-back business write rolls the spend back with it;
- two concurrent posts with one token produce exactly one success.

`Check` stays for handlers whose first write is not in a transaction
they own, and for flows that redirect after every POST (auth, below):
spending first is correct there, because the next GET mints a new
challenge anyway.

### The attempt cap

An admitted token that keeps failing the handler's own validation is
otherwise free handler work for its whole `MaxAge`. `Admit` counts
attempts per nonce in process, in a map bounded at 10,000 entries that
prunes expired entries first and evicts oldest second. Each entry cost
the attacker a solve, so filling it is not cheap. It resets on restart;
that is acceptable because it bounds handler work, not replay — replay
is the durable ledger's job. (Tito: same choice, same reason,
intake_gate.go:273-286.)

### NonceStore

```go
type NonceStore interface {
	Spend(ctx context.Context, ex Execer, nonce string, expires time.Time) (bool, error)
	Spent(ctx context.Context, nonce string) (bool, error)
	Sweep(now time.Time) error
}
```

`SQLNonces` spends on `ex` when given one and its own handle when `ex`
is nil. `MemoryNonces` ignores `ex`, so a spend inside a transaction
that rolls back stays spent; its doc says so, and it remains a test and
single-process tool. No schema change: `pow_spent_nonces` already has
what this needs.

### Browser

`powcore.js` keeps the preimage `nonce:binding:counter`, with an empty
binding when unbound — the Chromium test (pow/browser_test.go) keeps
proving the two halves agree, in both modes now.

`pow.js` changes:

- **Unbound:** the worker starts when the module loads. A submit before
  the solve finishes shows the working label and posts when it lands.
  `[data-pow-binding]` is required only for a bound form.
- **Bound:** unchanged — solves on submit.
- **`NoProof`:** no worker; enables the submit.
- **Minimum age, client side.** `FormAttrs` adds `data-pow-min-age`. The
  module holds a submit until that long after it loaded, so a real
  browser never meets `ReasonTooFast` — the one-tap sign-in buttons
  autofocus (ui/partials/signin.html:50, 57), and a returning visitor
  can be quicker than any minimum. Network latency only ever makes the
  server's measured age longer than the client's, so the hold is safe.
- **One challenge, several forms.** Forms carrying the same
  `data-pow-nonce` share one solve. The sign-in screen has three forms
  posting to `Begin`; only one can be submitted, and single use spends
  the challenge whichever it is.

### Form, for templates

```go
type Form struct {
	Challenge
	ScriptURL, WorkerURL string
}

func (f Form) Fields() template.HTML     // Challenge.Fields
func (f Form) Attrs() template.HTMLAttr  // FormAttrs(WorkerURL)
func (f Form) Script() template.HTML     // <script type="module" src=ScriptURL>
```

One value an app or a shipped partial renders, so `auth`, `password`
and Tito share the shape rather than each inventing a struct of three
URLs.

## 2. `auth`

```go
type Proof struct {
	Off                  bool
	ScriptURL, WorkerURL string        // required unless Off
	Difficulty           int           // default pow.DefaultDifficulty
	MinAge               time.Duration // default 500ms
}
// Config.Proof Proof
```

- **The zero value is `ErrProofUnset`** from `auth.New`: "Config.Proof is
  unset: serve pow.Assets() and set ScriptURL and WorkerURL, or set
  Off: true". The URLs being required is what forces the assets to be
  mounted.
- **auth owns the Guard,** built in `New` from `InstanceKey` and `DB`
  with name `rastrillo/auth/signin`, the way it already owns its
  sessions, limiter and link store (auth/auth.go:349-420).
- **Schema:** the app merges `pow.Schema`, by the existing convention
  that no package embeds another's migrations (auth/store.go:16-25).
  `New` probes `pow_spent_nonces` and refuses to start without it —
  otherwise a missing table is `ReasonUnavailable` on every sign-in,
  found by the first visitor. (The plan confirms `New` runs after
  `migrate.Apply` in every documented wiring.)
- **`Begin` checks first.** After the same-origin check and before the
  rate limiter and classification (auth/handlers.go:41-57). This is the
  admission pre-check the sign-in screen spec deferred: an anonymous
  visitor can no longer make the server resolve and probe a domain of
  their choosing (or spend another address's rate budget) without
  solving. `Begin` uses `Check`, not `Admit`/`Commit`: every outcome
  redirects, the next GET mints a fresh challenge, and the spend must
  land before mail or a probe does.
- **A refusal redirects to `?err=check`,** a new problem the screen
  shows on the ask step: the browser could not complete a check, reload
  and try again, and this page needs JavaScript. Its copy goes through
  copy-review and into every locale's
  `rastrillo.ui.signin_problem_check` before it is written into a
  template. The honeypot takes this path too.
- **`SigninState.Proof *pow.Form`,** filled whenever Proof is on,
  screen or no screen. `Issue` writes nothing, so `SigninState` stays
  free of database access (auth/signin.go:116-125). One challenge per
  render, shared by the ask form and both one-tap forms.
- **The shipped partial** (ui/partials/signin.html) renders `Fields` and
  `Attrs` on its three `Begin` forms, its submits rendered disabled with
  `data-pow-submit`, a `<noscript>` line, and `Script` once. The Forget
  form is not gated: it deletes cookies and nothing else.

## 3. `password`

The same `Proof` shape on `password.Config`, the same `ErrProofUnset`,
two Guards (`rastrillo/password/signin`, `rastrillo/password/signup`),
and `PageData.Proof *pow.Form` so the app's `RenderSignin` /
`RenderSignup` can render it. `Signin` and `Signup` check before the
limiter (password/handlers.go:193, 287). A refusal re-renders the page
with a new `ErrCheck` message and a fresh challenge; the email is kept,
the password is not (as today for any failure).

`password` re-renders rather than redirects, so it could use
`Admit`/`Commit`. It uses `Check` anyway: a wrong password must not
leave a solved token reusable for four more guesses, and the re-render
carries a fresh challenge that solves in the background while the
visitor retypes.

`ui/partials/form-foot.html` gains an optional `Proof` key that renders
its submit disabled with `data-pow-submit` plus the `<noscript>` line.

## 4. Docs and the example

- `examples/notes` wires both: assets mounted, `Proof` set, templates
  rendering `.Proof`. It is the reference an app copies.
- SKILL.md: the auth, password and public-forms paragraphs, within the
  byte budget (`skillmd_test.go`). The load-bearing facts: default on;
  the boot error and its two fixes; `pow.Schema` in the merge;
  `Admit`/`Commit` for a handler with its own transaction; scope and
  name are sealed; forms need JavaScript.
- `docs/site/reference/pow.md`, `docs/site/magic-links.md`,
  `docs/site/passwords.md`. Fix `magic-links.md:212-215` while there: it
  says the rate limiter counts failures and resets on success; the code
  counts every `Begin` and never resets (signin/magiclink.go:96-110).
- CHANGELOG: breaking, with the upgrade steps.

## 5. Tito Go moves onto it (separate titogo plan)

Recorded here so this design is checked against its first caller:

- Each door in `intake.Door` becomes one `pow.Guard`: name is the door,
  scope is the event id, difficulty from `intakeBits`, `NoProof` for
  checkout, `MinAge` 500ms, `MaxAge` 2h, `Attempts` 5.
- Tito's sealing key moves into `InstanceKey`; it is already stored, not
  per process (T/internal/instance/intake_store.go:21-29).
- `intakeAdmit` becomes `Admit`; `adm.Commit(tx)` is unchanged in shape.
  `intakeConsume`, the own-transaction fallback, becomes `Check`.
- Deleted: `internal/intake/token.go`, `internal/intake/pow.go`,
  `static/intake-pow*.js`, `intake-proof.js`, the node parity test.
  Tito serves `pow.Assets()` instead, and its CSP gains
  `pow.HoneypotStyleHash` with `'unsafe-hashes'`.
- What stays in Tito: the step-up screens per reason, the preview bypass,
  the refusal tally and its flush to `carlos logs`, the route coverage
  fence. Its `intake_tokens` ledger is replaced by `pow_spent_nonces`.
- Forms open at deploy carry v1-format tokens; each meets the step-up
  screen once, which mints a fresh one.

## Not building

- **Captchas, device fingerprinting, behavioural signals, header
  classifiers, IP reputation.** Tito's design rejects each with reasons
  that apply here unchanged (intake design, "Not building" and
  "Recorded for later").
- **Adaptive difficulty.** An attacker could raise the cost for every
  real visitor on purpose.
- **Persisted budgets** on what a form spends (mail per address, rows
  per hour). The real defence against a bulk attacker who pays for the
  solves — `pow`'s own doc says so (pow/pow.go:22-26) — and its own
  spec: a durable, per-scope limiter that survives hibernation, which
  rastrillo has none of today.
- **passkey's `discover/begin`.** Unauthenticated, and it writes a
  challenge row per call (passkey/passkey.go:424, 512-523). It is a JSON
  endpoint behind JavaScript already; gating it is a different shape and
  its own change.
- **Refusal telemetry, step-up screens, a route coverage harness.**
  App concerns for now; the coverage fence is the strongest candidate to
  come back as a harness check.
- **Private Access Tokens (RFC 9577)** as a fast path that skips the
  proof. Coverage is too partial to be more than that; later.

## Testing

`pow`:

- concurrent `Commit` on one admission: exactly one nil, the rest
  `ErrSpent`;
- a rolled-back transaction leaves the token admissible;
- an `Admit` with no `Commit` (a validation failure) leaves it
  admissible;
- name mismatch and scope mismatch are both `ReasonSealInvalid`;
- a v1-format seal is refused;
- `NoProof`: no counter needed, still single use, still honeypot;
- `Bind`: an unbound post to a bound guard is `ReasonShort`;
- the attempt cap refuses the sixth post and is per nonce;
- `ReasonMissing` for a post carrying no challenge fields;
- `Also` carries every cheap failure and no database-backed one;
- Chromium, both modes: the shipped solver satisfies the Go verifier;
  an unbound form solves before submit; a fast click is held, not
  refused; three forms sharing a nonce solve once.

`auth` / `password`:

- zero `Proof` is `ErrProofUnset`; `Off: true` boots and gates nothing;
  a missing `pow_spent_nonces` table refuses to boot;
- `Begin` with no challenge redirects to `?err=check` and sends no mail,
  writes no link and makes no DNS lookup (the fakes record none);
- the rate limiter is not spent by a refused post;
- a valid challenge spent once; its replay is refused;
- the sign-in screen renders the fields on every `Begin` form and not on
  Forget; existing screen tests still pass with Proof on;
- `password`: a wrong password re-renders with a fresh challenge;
  signup without a challenge creates no row.

## Open questions

- **Difficulty for sign-in.** `DefaultDifficulty` (18) is unmeasured
  (pow/pow.go:35-41). Solving now overlaps typing, so most visitors never
  wait — except the one-tap returning visitor, who clicks at once. The
  plan's first task measures p95 and p99 on a mid-range Android before
  picking a number for `auth` and `password`. Tito measured 9ms at 12
  bits on a desktop (intake_gate.go:74-75).
- **`ErrProofUnset` and existing tests.** Every `auth`/`password` test
  builds a Config without `Proof`; each needs `Off: true` or a wired
  Proof. The plan picks per test: anything exercising Begin's ordering
  or the screen wires it, the rest switch it off.
