# pow grows up: admission that commits with the write, and sign-in behind it by default

Status: design approved in conversation 2026-10-03, section by section,
with the operator's decisions marked **(Paul)**. Revised after one round
of adversarial review by Astra (see "Review log"). Nothing is
implemented.

Citations: `T/` is Tito Go (`github.com/tito/titogo`, origin/main
`dd3b53df3`); everything else is this repo at `0aca6edb`.

## Why

Every app in the family has a public form, and rastrillo already ships
the front door for one: `pow` (7814ebea, 2026-09-05), extracted from
movement "so a second app cannot copy it". Nobody adopted it. movement
and correomona never switched, and two weeks later Tito Go built its own
— `T/internal/intake` plus `T/internal/instance/intake_*.go`, designed in
`T/docs/superpowers/specs/2026-09-18-intake-design.md` — the third
hashcash implementation in the family (with `T/internal/carlos/pow.go`
and keymail's `internal/pow`).

Tito did not copy out of ignorance. It hit four things `pow` cannot do:

1. **Spend the token in the caller's transaction.** `pow.Check` spends
   the nonce before the handler writes anything (pow/guard.go:139-180).
   A validation error afterwards has burned the visitor's solved proof;
   a write that rolls back leaves the token spent with nothing to show.
   Tito's `intakeAdmit` decides and writes nothing; `adm.Commit(tx)`
   spends inside the handler's transaction
   (T/internal/instance/intake_gate.go:7-12, 316-338).
2. **Bind to a form, not an address.** `pow` binds the work to the
   submitted address, so solving cannot start until submit. Tito binds
   the token to door and event and solves at page load.
3. **A token-only tier that needs no JavaScript.** Tito's checkout runs
   at zero bits — sealed single-use token and honeypot, usable the
   instant the server-rendered page is (intake_gate.go:59-81). `pow`
   turns any difficulty ≤ 0 into the default, and its module is what
   enables the submit.
4. **Scope.** Tito seals door and event into the token. `pow`'s seal
   covers nonce, time and difficulty only (pow/challenge.go:68-72).

Meanwhile rastrillo's own sign-in and sign-up have no anonymous-abuse
control beyond in-memory counters: `auth.Begin` (auth/handlers.go:41)
sends mail and makes the server probe a domain the visitor chose, behind
a per-process fixed window (auth/auth.go:413); `password.Signup`
(password/handlers.go:265) creates a user row per fresh address and
counts only failures (password/limit.go:44-46). The sign-in screen spec
names the gap and defers it: "an admission pre-check before
classification … worth upstreaming"
(docs/superpowers/specs/2026-09-27-signin-screen-design.md:1001-1005).

## Decisions

- **Grow `pow`; do not add a package.** The browser half and the Go half
  must agree byte for byte, and only one module can ship both
  (pow/assets.go).
- **The interface may break.** Nothing outside `pow/` imports it (only
  `buildhandler_test.go`, for the CSP hash). Tito Go is the first real
  caller and migrates after this lands.
- **Unbound by default, binding opt-in (Paul).** With single-use nonces,
  one solve already buys exactly one submission whatever address it
  carries; what binding costs is that solving cannot start before submit.
- **Sign-in and sign-up are behind it by default (Paul):** `auth.Begin`,
  `password.Signin`, `password.Signup`.
- **Proof of work is required there, so those forms need JavaScript
  (Paul).** If a missing proof is let through, every bot takes that path.
  An app can switch it off.
- **Default on, and a boot error until wired (Paul).** The zero value of
  the new config is an error from `auth.New` / `password.New` naming the
  fix. Loud in CI, never a silent production lockout.
- **Every refusal is visible, and none is a loop.** Including the
  honeypot: a visitor whose password manager filled the trap must get a
  form without the trap, not the same form again (Tito's step-up,
  T/internal/instance/intake_stepup.go:57-66).
- **The app owns its Guards.** `auth` and `password` take a `*pow.Guard`
  rather than building one. `password.Config` has no instance key or
  database to build one from (password/handlers.go:70), and an owned
  Guard would leave its spent-nonce rows with no sweeper (auth's `Sweep`
  covers links and sessions, auth/store.go:118). The app builds one
  Guard, hands it to both packages, and sweeps it from the tick it
  already has.

## 1. `pow`

### Config

```go
type Config struct {
	InstanceKey string     // required, as today
	Nonces      NonceStore // required, as today
	Difficulty  int        // default DefaultDifficulty; NoProof = token + honeypot, no JavaScript
	Bind        bool       // bind the work to [data-pow-binding]; default false
	MinAge      time.Duration
	MaxAge      time.Duration
	Attempts    int        // admitted-but-uncommitted posts per token; default 20
	ScriptURL   string     // fingerprinted pow.js; required unless NoProof
	WorkerURL   string     // fingerprinted pow-worker.js; required unless NoProof
}

const NoProof = -1
```

- `New` refuses missing URLs when proof is on (`ErrNoAssets`), so a Guard
  that renders a form the browser cannot complete does not boot. A URL
  being set does not prove the asset is served; §1 "Browser" covers the
  visitor's side of that.
- `New` asks the store whether it is ready: `SQLNonces` probes
  `pow_spent_nonces` and `New` returns `ErrNoSchema` if the table is
  missing. Otherwise a forgotten `pow.Schema` is `ReasonUnavailable` on
  every post, found by the first visitor. Every documented wiring runs
  `migrate.Apply` before constructing anything; the plan checks that
  `examples/` and SKILL.md agree.
- There is no `Name`. Scope (below) does the isolating, and difficulty is
  already sealed and compared, so a token from a 12-bit or `NoProof`
  Guard is refused by an 18-bit one sharing the key.

### The seal (v2)

```
HMAC(key, "pow/v2" || lp(scope) || lp(nonce) || u64(issuedMillis) || i32(difficulty) || u8(flags))
```

`lp` is a uvarint length followed by the bytes; integers are big-endian
fixed width. Length-prefixing makes the encoding injective. The v1 string
`nonce\x00issued\x00difficulty` was not once a caller-chosen scope joined
it: `(scope "a\x00b", nonce N)` and `(scope "a", nonce "b\x00"+N)` sign
the same bytes (Astra, reproduced). The posted nonce must also be
exactly 32 lowercase hex characters, checked before the HMAC is computed;
anything else is `ReasonSealInvalid`.

- **Scope** is not posted. The server supplies the scope it expects, so a
  token minted for one form or event does not verify on another, and the
  form carries nothing the submitter could edit. Callers namespace it:
  `auth` uses `rastrillo/auth/begin`, `password` uses
  `rastrillo/password/signin` and `…/signup`, Tito uses `apply:41`.
- **Milliseconds,** not `Unix()` seconds. With second resolution a token
  issued late in a second already reads as over 500 ms old, and a script
  skips the minimum age for free (pow/challenge.go:85, 105; Tito uses
  milliseconds for this reason, T/internal/intake/token.go:53-57).
- **Flags:** bit 0 is "trap omitted" (§1 "Recovery").
- A name or scope mismatch, a bad nonce format, a v1 seal: all
  `ReasonSealInvalid`, so a prober learns nothing from the difference.

### Issue and Form

```go
func (g *Guard) Issue(now time.Time, scope string) Challenge // writes nothing, as today
func (g *Guard) Form(now time.Time, scope string) Form

type Form struct { /* Challenge + the Guard's URLs, mode and min age */ }
func (f Form) Fields() template.HTML    // hidden fields + honeypot (+ status notice when proof is on)
func (f Form) Attrs() template.HTMLAttr // data-pow-* for the <form>; empty under NoProof
func (f Form) Script() template.HTML    // <script type="module">; empty under NoProof
```

`Issue` writes nothing (pow/challenge.go:19-25: a row per render makes
every crawler a write stream). `Form` is what templates render, so
`auth`, `password` and Tito share one shape.

**A challenge-bearing response must not be cached.** One token on a
shared cached page is one token for every visitor, and the first
submission spends it for all of them (T intake design, "Checkout is two
doors"). `pow` cannot set headers from a template, so the rule is in the
package doc and SKILL.md, `auth` already writes `no-store` through
`PrepareSigninResponse` (auth/signin.go:204-216), and `password` sets
`Cache-Control: no-store` before calling its render callbacks
(password/handlers.go:149, today it sets nothing).

### Recovery

```go
func (g *Guard) Recovery(now time.Time, scope string, why Reason) Form
```

The form to re-render after a refusal, so that retrying can change the
answer:

| `why` | What `Recovery` changes |
|---|---|
| `honeypot` | Omits the trap and seals the "trap omitted" flag, so `Admit` skips the honeypot for that token only. Proof, scope and single use still apply. |
| `too_fast` | Backdates issue by `MinAge` (Tito's follow-on token, intake_gate.go:143-174): the visitor has already spent the time. |
| anything else | A fresh challenge. |

The cost, stated: a bot that trips the honeypot can ask for a trapless
form. The honeypot is a free filter for the dumbest scripts, not the
control; the proof of work and single use are. Recovery forms are never
issued unprompted.

### Admit, Commit, Check, Verify

```go
type Want struct {
	Scope   string
	Binding string // required when Config.Bind; ignored otherwise
}

type Admission struct {
	OK     bool
	Reason Reason   // the first failure
	Also   []Reason // every other cheap failure, for the log
}

func (g *Guard) Admit(r *http.Request, w Want) Admission
func (a Admission) Commit(ctx context.Context, ex Execer) error
func (g *Guard) Check(r *http.Request, w Want) (Reason, bool)
func (g *Guard) Verify(r *http.Request, w Want) (Reason, bool)

type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

var (
	ErrSpent       = errors.New("rastrillo/pow: challenge already spent")
	ErrExpired     = errors.New("rastrillo/pow: challenge expired before commit")
	ErrNotAdmitted = errors.New("rastrillo/pow: commit of a refused admission")
)
```

**`Admit`** runs, in order: body bounds → challenge fields present
(`ReasonMissing`, new) → honeypot (unless the trap-omitted flag is
sealed) → seal → clock → proof of work (skipped at `NoProof`) →
already-spent lookup → attempt allowance (`ReasonAttempts`; `ReasonBusy`
at capacity). It writes nothing. Free checks come first and the signature
before the timestamp it vouches for (pow/guard.go:123-133). After the
first failure the remaining cheap checks still run into `Also`; nothing
that reads the database runs after a failure.

`ReasonMissing` means something different from a bad proof: the form was
never wired, or it predates this release. It is the one an operator
should see first after an upgrade.

**`Commit`** spends the nonce on whatever `ex` the caller passes:
`*sql.Tx`, `*sql.DB`, `*sql.Conn`, or — inside a GORM transaction —
`tx.Statement.ConnPool` (not `tx` itself), so `pow` does not import GORM.

- Zero rows affected is `ErrSpent`: the handler lost a race to an
  identical request and must refuse, not 500.
- A commit after the token's expiry is `ErrExpired`, checked at commit
  time, immediately before the insert. Without it: two posts admitted
  just before expiry, A commits, a sweep deletes A's now-expired row, B
  commits into the empty slot — one token, two writes (Astra,
  reproduced). Sweeping only rows past expiry **plus a 10-minute margin**
  closes the gap: for the sweep to have removed A's row, B's commit is
  already past expiry and refused.
- `Commit` on a refused admission is `ErrNotAdmitted`. Tito's returns nil
  there (intake_gate.go:321-326) and relies on no door going on past a
  refusal; a door that did would create its row with nothing consumed.
- A successful `Commit` drops the token's attempt entry.

Each consequence is a test: a validation failure never reaches `Commit`
so the token survives the retry; a rollback rolls the spend back; two
concurrent posts produce exactly one success; the delayed-commit
sequence above is refused.

**`Check`** is `Admit` then `Commit` on the store's own handle, for a
handler that redirects after every POST or has no transaction of its
own. Spending first is right there: the next GET mints a new challenge,
and the spend must land before mail or a probe does.

**`Verify`** checks seal, maximum age, proof and not-spent, and neither
counts nor spends. It is for a subordinate request made on a parent
form's behalf — Tito's email check and file upload verify the parent
token without consuming it and keep their own allowances
(T/internal/instance/intake_subordinate.go:118-180). The allowances stay
Tito's.

### The attempt allowance

An admitted token that keeps failing the handler's own validation is
otherwise free handler work for its whole `MaxAge`. `Admit` counts
admissions per nonce in process. Default 20, as Tito's
(intake_gate.go:104-107): an honest visitor correcting a form several
times is ordinary.

The map holds at most 10,000 live entries per Guard and **never evicts a
live one**; at capacity a new token is refused with `ReasonBusy` until an
entry expires or commits, and tokens already tracked keep their
allowance. Evicting the oldest instead would let an attacker cycling
10,001 tokens restore every allowance without a single new solve
(Astra). This is Tito's rule (intake_subordinate.go:81-84). The trade it
makes, stated: an attacker holding 10,000 admitted, uncommitted tokens
can make that form answer "busy" until they expire — 10,000 solves under
proof, 10,000 page loads under `NoProof`. `Check` callers (auth,
password) never occupy the map, because `Check` commits at once.

It resets on restart. That bounds handler work, not replay; replay is
the durable ledger's job.

### NonceStore

```go
type NonceStore interface {
	Spend(ctx context.Context, ex Execer, nonce string, expires time.Time) (bool, error)
	Spent(ctx context.Context, nonce string) (bool, error)
	Ready(ctx context.Context) error
	Sweep(now time.Time) error
}
```

- `SQLNonces` spends on `ex` when given one and its own handle when `ex`
  is nil.
- `Sweep` deletes at most 500 rows per call, past expiry plus the
  10-minute margin. Unbounded, a sweep on wake holds the single writer
  while the first visitor waits (Tito bounds its own,
  T/internal/instance/intake_store.go:157, 232-248). Call it from a tick.
- `MemoryNonces` ignores `ex`, so a spend inside a transaction that rolls
  back stays spent. Its doc says so; it stays a test and single-process
  tool.
- No schema change: `pow_spent_nonces` already has what this needs.

### Browser

`powcore.js` keeps the preimage `nonce:binding:counter`, with an empty
binding when unbound. The Chromium test (pow/browser_test.go) proves the
halves agree in both modes.

`pow.js`:

- **Explicit, repeatable initialisation.** The module exports
  `init(root)` and calls `init(document)` when it first evaluates. `init`
  is idempotent per form. A page that replaces its document from a POST
  response (Tito's `document.open/write`,
  T/internal/instance/static/spinner.js:259-267) does not re-run a module
  URL it has already evaluated, so the page calls `init` itself rather
  than relying on Tito's fresh-URL trick.
- **Unbound:** the worker starts at `init`. A submit before the solve
  lands is held: the working label shows, and the form is submitted with
  its original submitter once the solve and the minimum age are both
  done.
- **Bound:** a solution is keyed to (nonce, difficulty, normalised
  binding) and that tuple is checked again immediately before release. A
  binding changed by autofill or a correction while the worker ran is
  solved again; today's `solved = true` latch would release a stale
  proof (pow/browser/pow.js:71-79).
- **Minimum age, client side.** `Attrs` carries `data-pow-min-age`; the
  module holds a submit until that long after it initialised. The
  one-tap sign-in buttons autofocus (ui/partials/signin.html:50, 57), and
  a returning visitor can be quicker than any minimum. Network latency
  only makes the server's measured age longer than the client's.
- **Navigation.** `pagehide` cancels a held submit and terminates the
  worker; `pageshow` from the back-forward cache restores the controls
  `pow` disabled and restarts the solve if it never finished. Without
  this a visitor who leaves during the hold and comes back finds a dead
  button, or a submit firing on a page they left (`ui/busy.js:237-250`
  treats the same boundary, but cannot cancel another module's worker).
- **Submit controls outside the form.** The module finds the submit by
  `form.elements` and `[data-pow-submit][form=<id>]`, not only
  `form.querySelector` — Tito's checkout button sits outside its form
  (T/internal/instance/templates/checkout_details.html:121, 231).
- **Failure is visible with JavaScript on.** `<noscript>` covers only
  JavaScript off. With it on, the module can be blocked by CSP, 404, or
  throw, and the worker can fail to construct. `Fields` renders a status
  notice the visitor sees if the form has not become ready within three
  seconds — revealed without JavaScript, removed by the module when it
  marks the form ready — saying the form could not be prepared and
  offering a reload. Worker construction is wrapped; it and a later
  worker error show the same notice in place. The plan chooses the
  CSS mechanism (it must pass the default CSP); tests cover a missing
  asset, a restrictive CSP and a throwing worker.
- **`pow:solved`.** Fired on the form with the solution, and
  `whenSolved(form)` returns a promise of it, so a subordinate request
  can carry the parent's proof (Tito's `intake-proof.js` contract,
  T/internal/instance/static/email-check.js:18, 154; file-field.js:15,
  91).
- **`NoProof` needs no JavaScript.** `Attrs` and `Script` render nothing,
  the submit is rendered enabled, and the minimum age is the server's
  alone — a fast visitor meets `too_fast` and `Recovery`. That is what
  Paul chose for Tito's checkout: a token usable the instant the page is.

## 2. `auth`

```go
// Config
Proof    *pow.Guard // sign-in admission; required unless ProofOff
ProofOff bool
```

- **Neither set is `ErrProofUnset`** from `auth.New`: "Config.Proof is
  unset: build a pow.Guard (serve pow.Assets(), merge pow.Schema) and set
  it, or set ProofOff". The Guard's own `New` has already refused missing
  assets URLs and a missing table.
- **`Begin` checks first,** after the same-origin check and before the
  rate limiter and classification (auth/handlers.go:41-57), with `Check`
  at scope `rastrillo/auth/begin`. An anonymous visitor can no longer
  make the server resolve and probe a domain of their choosing, send
  mail, or spend another address's rate budget without solving.
- **A refusal redirects to `?err=check&why=<reason>`,** a new problem the
  screen shows on the ask step. Its copy goes through copy-review and
  into every locale's catalog (`rastrillo.ui.signin_problem_check`)
  before it is written into a template. `why` is attacker-controllable
  and only selects which `Recovery` form the next render gets; that is
  the cost already stated under "Recovery".
- **`SigninState.Proof *pow.Form`,** filled whenever `Proof` is set,
  screen or no screen, from `Form` or, after a refusal, `Recovery`.
  Neither writes, so `SigninState` stays free of database access
  (auth/signin.go:116-125).
- **The shipped partial** (ui/partials/signin.html) renders exactly one
  `Begin` form per render — the keymail one-tap, the link one-tap, or the
  ask form are mutually exclusive branches (signin.html:47-67). Whichever
  renders carries `Fields` and `Attrs`, its submit rendered disabled with
  `data-pow-submit`, plus a `<noscript>` line and `Script` once. The
  Forget form is not gated: it deletes cookies and nothing else.

## 3. `password`

- `Config.Proof *pow.Guard` / `ProofOff bool`, the same `ErrProofUnset`.
  The app can pass the same Guard it gave `auth`; scope keeps them apart.
- `PageData.Proof *pow.Form`, filled from `Form` or `Recovery`, so the
  app's `RenderSignin` / `RenderSignup` render it.
- `SigninPage`, `SignupPage` and every re-render set `Cache-Control:
  no-store` before the callback.
- `Signin` and `Signup` `Check` before the limiter
  (password/handlers.go:193, 287) at scopes `rastrillo/password/signin`
  and `…/signup`. A refusal re-renders with a new `ErrCheck` message and
  the `Recovery` form; the email is kept, the password is not (as for any
  failure today).
- `Check`, not `Admit`/`Commit`: a wrong password must not leave a solved
  token reusable for more guesses, and the re-render's fresh challenge
  solves while the visitor retypes.
- `ui/partials/form-foot.html` gains an optional `Proof` key that renders
  its submit disabled with `data-pow-submit` plus the `<noscript>` line.

## 4. Docs and the example

- `examples/notes` wires it end to end: one Guard, assets mounted,
  `pow.Schema` merged, `Proof` on both packages, templates rendering
  `.Proof`, `Sweep` on the existing tick.
- SKILL.md: the auth, password and public-forms paragraphs, within the
  byte budget (`skillmd_test.go`). Load-bearing facts: default on; the
  boot errors and their fixes; the app owns the Guard and sweeps it;
  `Admit`/`Commit` for a handler with its own transaction, `Check`
  otherwise, GORM via `tx.Statement.ConnPool`; scope is sealed and
  namespaced; never cache a page carrying a challenge; `NoProof` needs no
  JavaScript, everything else does.
- `docs/site/reference/pow.md`, `docs/site/magic-links.md`,
  `docs/site/passwords.md`. Fix `magic-links.md:212-215` while there: it
  says the limiter counts failures and resets on success; the code
  counts every `Begin` and never resets (signin/magiclink.go:96-110).
- CHANGELOG: breaking, with the upgrade steps.

## 5. Tito Go moves onto it (separate titogo plan)

Recorded so this design is checked against its first caller. The titogo
plan owns the detail; these are the constraints this design must meet.

- One Guard per difficulty: `NoProof` for checkout, 12 bits for the rest
  (`intakeBits`), `MinAge` 500ms, `MaxAge` 2h, `Attempts` 20. Scope is
  `<door>:<event id>` (`<door>:account` for `/buyer/register`).
- Tito's stored key becomes `InstanceKey`; it is already stored, not per
  process (T/internal/instance/intake_store.go:21-29).
- `intakeAdmit` becomes `Admit`; `adm.Commit(tx)` becomes
  `adm.Commit(ctx, tx)`. `intakeConsume` becomes `Check`. The email check
  and uploads use `Verify` plus Tito's own allowances and read the proof
  through `whenSolved`.
- The step-up screen re-renders with `Recovery`, and calls `init` after
  replacing the document.
- **Old forms at deploy** post `intake_token` / `intake_nonce`, which the
  new Guard sees as `ReasonMissing` — and Tito answers a missing token
  with a plain 429 today (T/internal/instance/intake_stepup.go:124). For
  one release, a POST carrying `intake_token` and no `pow_*` fields gets
  the step-up screen with the visitor's answers preserved and a fresh
  `Form`; the plan tests it per door.
- Deleted after that release: `internal/intake/`, `static/intake-pow*.js`,
  `intake-proof.js`, the node parity test, `intake_tokens`.
- Stays in Tito: the step-up screens, the preview bypass, the refusal
  tally and its flush to `carlos logs`, the route coverage fence.

## Not building

- **Captchas, device fingerprinting, behavioural signals, header
  classifiers, IP reputation.** Tito's design rejects each with reasons
  that apply unchanged (intake design, "Not building", "Recorded for
  later").
- **Adaptive difficulty.** An attacker could raise everyone's cost.
- **Persisted budgets** on what a form spends (mail per address, rows per
  hour). The real defence against a bulk attacker who pays for the solves
  (pow/pow.go:22-26), and its own spec: a durable per-scope limiter that
  survives hibernation, which rastrillo has none of.
- **passkey's `discover/begin`.** Unauthenticated, and it writes a
  challenge row per call (passkey/passkey.go:424, 512-523). A JSON
  endpoint behind JavaScript already; a different shape, its own change.
- **Refusal telemetry, a route coverage harness.** App concerns for now;
  the coverage fence is the strongest candidate to come back.
- **Private Access Tokens (RFC 9577)** as a fast path past the proof.
  Later.

## Testing

`pow`:

- seal: names and scopes containing NUL and length-like bytes do not
  collide; a non-hex or wrong-length nonce is refused before HMAC; a v1
  seal is refused; issue on both sides of a second boundary honours a
  500 ms minimum;
- `Commit`: concurrent commits give exactly one nil; rollback leaves the
  token admissible; an un-committed admission leaves it admissible;
  admit → commit → expiry → sweep → delayed second commit is
  `ErrExpired`; `ErrNotAdmitted` on a refused admission; GORM through
  `tx.Statement.ConnPool`;
- `Admit`: scope mismatch is `ReasonSealInvalid`; `ReasonMissing` with no
  challenge fields; `Also` carries cheap failures only;
- attempts: the 21st admission is refused; at capacity a new token is
  `ReasonBusy` and tracked tokens are unaffected; cycling 10,001 tokens
  does not restore any allowance; commit frees the entry;
- `Recovery`: the trap-omitted form passes with the trap absent and only
  that token skips the honeypot; `too_fast` recovery is admissible at
  once;
- `Verify` neither counts nor spends; `Sweep` is bounded and respects the
  margin; `New` refuses a missing table and missing asset URLs;
- Chromium: shipped solver satisfies the Go verifier in both modes; an
  unbound form solves before submit; a fast click is held, not refused;
  a bound form whose binding changes mid-solve re-solves; leave and
  return via bfcache during both holds; `init` after `document.write`; a
  submit control outside the form; missing asset, restrictive CSP and a
  throwing worker each show the notice; `NoProof` posts with JavaScript
  disabled.

`auth` / `password`:

- neither `Proof` nor `ProofOff` is `ErrProofUnset`; `ProofOff` boots and
  gates nothing;
- `Begin` with no challenge redirects to `?err=check` and sends no mail,
  writes no link, makes no DNS lookup (the fakes record none) and spends
  no rate budget;
- replay of a spent challenge is refused; honeypot refusal leads to a
  trapless form that then succeeds;
- each rendered sign-in state (keymail one-tap, link one-tap, ask,
  passkey + ask) carries the fields on its one `Begin` form and none on
  Forget; existing screen tests pass with Proof on;
- `password`: pages and re-renders are `no-store`; a wrong password
  re-renders with a fresh challenge; signup without a challenge creates
  no row.

## Open questions

- **Difficulty for sign-in.** `DefaultDifficulty` (18) is unmeasured
  (pow/pow.go:35-41). Solving overlaps typing, so most visitors never
  wait — except the one-tap returning visitor, who clicks at once. The
  plan's first task measures p95 and p99 on a mid-range Android before
  `examples/notes` and SKILL.md recommend a number. Tito measured 9ms at
  12 bits on a desktop (intake_gate.go:74-75).
- **Existing tests.** Every `auth`/`password` test builds a Config
  without `Proof`. The plan picks per test: anything exercising Begin's
  ordering or the screen wires a Guard, the rest set `ProofOff`.

## Review log

**Astra, round 1 (2026-10-03): not ready.** Sixteen findings, all
accepted:

1. Seal separator injection (blocker) → length-prefixed v2 seal, nonce
   format checked before HMAC.
2. Expiry between admit and commit reopens replay (blocker) →
   `ErrExpired` at commit, sweep margin.
3. Attempt-cap eviction restores allowances (major) → never evict live
   entries; `ReasonBusy` at capacity; trade-off stated.
4. `password` cannot build a Guard (blocker) → the app owns the Guard.
5. Tito's subordinate endpoints have no API (blocker) → `Verify`,
   `pow:solved` / `whenSolved`.
6. Document replacement loses the module (major) → `init(root)`.
7. JavaScript failure silent with JavaScript on (major) → status notice,
   wrapped worker construction.
8. No navigation lifecycle for held submits (major) → `pagehide` /
   `pageshow`.
9. Bound mode releases stale proofs (major) → solution keyed to the
   binding and re-checked.
10. `NoProof` still depended on the module; outside-form submits (major)
    → `NoProof` needs no JavaScript; `form=` controls found.
11. Persistent honeypot autofill loops (major) → `Recovery` omits the
    trap.
12. No cache contract (major) → `no-store` rule; `password` sets it.
13. Nobody sweeps owned guards' nonces (major) → app owns and sweeps;
    `Sweep` bounded.
14. Tito's open forms at deploy get a 429, not a step-up (major) → one
    release of legacy-field recovery.
15. Second-resolution timestamps defeat 500 ms (minor) → milliseconds.
16. Inaccurate citations: the three sign-in forms are exclusive branches,
    Tito's cap is 20, `Commit` gains `ctx` and GORM passes
    `tx.Statement.ConnPool` (minor) → corrected; the shared-solve feature
    is dropped as unneeded.
