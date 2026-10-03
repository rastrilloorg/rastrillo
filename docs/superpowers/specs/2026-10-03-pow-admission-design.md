# pow grows up: admission that commits with the write, and sign-in behind it by default

Status: design approved in conversation 2026-10-03, section by section,
with the operator's decisions marked **(Paul)**. Revised after three rounds
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
	Attempts    int        // admissions per token through Admit; default 20
	Tracked     int        // tokens Admit tracks at once; default 100,000
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
  every post, found by the first visitor. This requires `migrate.Apply`
  before `pow.New`, which `docs/site/reference/pow.md` gets backwards
  today (Guard at :26, migration at :43); every documented boot is
  reordered.
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
the same bytes (Astra, reproduced). The posted nonce must be exactly 32
lowercase hex characters, checked before the HMAC is computed; anything
else is `ReasonSealInvalid`. That also makes the nonce a canonical token
identifier: one token, one string.

- **Scope is posted and sealed** (`pow_scope`). `Admit` and `Check`
  compare it to the scope the server expects (`Want.Scope`), so a token
  minted for one form or event does not verify on another. `Verify`
  instead *returns* it, authenticated, for the caller to authorise — a
  subordinate endpoint has to learn which parent form it is serving
  (§ "Verify"). Callers namespace it: `rastrillo/auth/begin`,
  `rastrillo/password/signin` and `…/signup`, Tito's `apply:41`.
- **Milliseconds,** not `Unix()` seconds. With second resolution a token
  issued late in a second already reads as over 500 ms old, and a script
  skips the minimum age for free (pow/challenge.go:85, 105; Tito uses
  milliseconds for this reason, T/internal/intake/token.go:53-57).
- **Flags:** bit 0 is "trap omitted" (§ "Recovery"); bit 1 is "bound".
  `Verify` refuses a bound token (below).
- A scope mismatch, a bad nonce format, a v1 seal: all
  `ReasonSealInvalid`, so a prober learns nothing from the difference.

### Issue, Form, FollowOn

```go
func (g *Guard) Issue(now time.Time, scope string) Challenge // writes nothing, as today
func (g *Guard) Form(now time.Time, scope string) Form
func (g *Guard) FollowOn(now time.Time, scope string) Form   // already age-eligible

type Form struct { /* Challenge + the Guard's URLs, mode and min age */ }
func (f Form) Fields() template.HTML    // hidden fields + honeypot (+ status line when proof is on)
func (f Form) Attrs() template.HTMLAttr // data-pow-* for the <form>; empty under NoProof
func (f Form) Script() template.HTML    // <script type="module">; empty under NoProof
func (f Form) NeedsScript() bool        // false under NoProof: render the submit enabled
```

`Issue` writes nothing (pow/challenge.go:19-25: a row per render makes
every crawler a write stream). `Form` is what templates render, so
`auth`, `password` and Tito share one shape.

**`FollowOn`** issues a token backdated by `MinAge`, for a form the
visitor reaches with nothing left to type: a prefilled checkout details
form, a confirm screen, and the re-render of either after a validation
error. That is how Tito keeps checkout usable the instant it renders
(T/internal/instance/shop.go:8288; postback.go:122), which is Paul's
decision for that door. It keeps the trap; only `Recovery` drops it.

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
func (g *Guard) Recovery(now time.Time, scope string) Form
func (a Admission) Recovered() bool // the posted token was a recovery token (sealed flag; refused posts too, once the seal verified)
```

The form to re-render after a refusal. It is **always** trapless and
**always** follow-on, whatever the reason — Tito's step-up does exactly
this (T/internal/instance/intake_stepup.go:57, 147). An earlier draft
varied recovery by reason; Astra showed the reasons then undo each other
(a fast visitor with a trap-filling password manager alternates
`honeypot` and `too_fast` forever under `NoProof`).

Recovery is sticky: once a visitor has been given a recovery token, every
later re-render of that form for them is a recovery form too. A handler
that re-renders asks `adm.Recovered()`; `auth`, which redirects, carries
it in the query (§2). Otherwise a trapless attempt that fails on a wrong
password re-renders with the trap back and the visitor is refused again.

The cost, stated: a bot can ask for a recovery form. The honeypot is a
free filter for the dumbest scripts, not the control; the proof of work
and single use are, and recovery changes neither.

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

type Parent struct {
	Scope      string // authenticated: from the seal
	Nonce      string // canonical token identifier
	Difficulty int
	Issued     time.Time
}

func (g *Guard) Admit(r *http.Request, w Want) Admission
func (a Admission) Commit(ctx context.Context, ex Execer) error
func (g *Guard) Check(r *http.Request, w Want) Admission // already spent when OK
func (g *Guard) Verify(r *http.Request) (Parent, Reason, bool)

type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

var (
	ErrSpent       = errors.New("rastrillo/pow: challenge spent or expired")
	ErrNotAdmitted = errors.New("rastrillo/pow: commit of a refused admission")
)
```

**`Admit`** runs, in order: body bounds → challenge fields present
(`ReasonMissing`, new) → honeypot (unless the trap-omitted flag is
sealed) → seal and scope → clock → proof of work (skipped at `NoProof`)
→ already-spent lookup → attempt allowance (`ReasonAttempts`;
`ReasonBusy` at capacity). It writes nothing. Free checks come first and
the signature before the timestamp it vouches for (pow/guard.go:123-133).
After the first failure the remaining cheap checks still run into
`Also`; nothing that reads the database runs after a failure.

`ReasonMissing` means something different from a bad proof: the form was
never wired, or it predates this release. It is the one an operator
should see first after an upgrade.

**`Commit`** spends the nonce on whatever `ex` the caller passes:
`*sql.Tx`, `*sql.DB`, `*sql.Conn`, or — inside a GORM transaction —
`tx.Statement.ConnPool` (not `tx` itself), so `pow` does not import GORM.

- **Expiry is enforced by the spend itself, on the database's clock:**

  ```sql
  INSERT OR IGNORE INTO pow_spent_nonces (nonce, expires_ms)
  SELECT ?, ? WHERE CAST(unixepoch('subsec') * 1000 AS INTEGER) <= ?
  ```

  and `Sweep` deletes only rows whose `expires_ms` is more than ten
  minutes behind the same clock. Both statements run on the one writer,
  so they are serialised. For a sweep to have removed A's row, the
  database's clock had passed expiry plus the margin; any later insert
  of the same nonce reads that clock or a later one, and its `WHERE`
  fails. No delay between the handler's checks and the statement's
  execution can reopen replay. An earlier draft checked expiry in Go
  before calling the executor, which a stalled request defeats (Astra,
  reproduced).
- Zero rows is `ErrSpent`, whether the token was spent by an identical
  request or expired in the meantime. The handler refuses either way,
  never 500s, and the visitor gets a recovery form. `Commit` does not
  look further to tell the two apart: `Execer` cannot query, and a
  lookup through the store's own handle while the caller's transaction
  holds the only connection (serve.go:635; Tito's T/internal/instance/serve.go:1548)
  waits forever for itself (Astra, round 3).
- `Commit` on a refused admission is `ErrNotAdmitted`. Tito's returns nil
  there (intake_gate.go:321-326) and relies on no door going on past a
  refusal; a door that did would create its row with nothing consumed.
- `Commit` does **not** release the token's attempt entry. The insert
  succeeding says nothing about whether the caller's transaction will;
  releasing on it let admit → spend → business refusal → rollback repeat
  forever on one solve (Astra, reproduced against Tito's portal
  registration, T/internal/instance/portals.go:1865, 1891-1899).

Each consequence is a test: a validation failure never reaches `Commit`
so the token survives the retry; a rollback rolls the spend back and
keeps the attempt count; two concurrent posts produce exactly one
success; a commit stalled past expiry and a sweep is refused.

**`Check`** verifies and spends in one step on the store's own handle,
and **does not touch the attempt map**: it spends at once, so there is no
uncommitted state to count. For a handler that redirects after every POST
or has no transaction of its own. Spending first is right there: the
next GET mints a new challenge, and the spend must land before mail or a
probe does. Because `Check` bypasses the map, a Guard's capacity filled
through `Admit` by one form cannot make another form's `Check` answer
busy.

**`Verify`** checks seal, maximum age, proof at the *sealed* difficulty,
and not-spent; it neither counts nor spends, and it returns the
authenticated `Parent`. It is for a subordinate request made on a parent
form's behalf. Tito's email check accepts a parent from any door and
event in the account, and an upload a parent for its own event only
(T/internal/instance/intake_subordinate.go:132-180); with `Parent` the
caller makes that decision on authenticated data, rejects difficulties
it does not accept, and keys its own allowances by `Parent.Nonce`, the
canonical identifier. Any Guard sharing the instance key can `Verify`
any of the app's *unbound* tokens, so the email check needs no knowledge
of which Guard minted its parent. A bound token is refused
(`ReasonSealInvalid`): its proof cannot be checked without the binding,
and a subordinate request does not carry one. Tito's parents are all
unbound; an app that needs subordinate requests on a bound form is a
later change, not a silent pass.

### The attempt allowance

An admitted token that keeps failing the handler's own validation is
otherwise free handler work for its whole `MaxAge`. `Admit` counts
admissions per nonce in process. Default 20, as Tito's
(intake_gate.go:104-107): an honest visitor correcting a form several
times is ordinary.

Entries live until their token expires — not until commit (above). The
map holds at most `Tracked` entries per Guard (default 100,000, a few
megabytes) and **never evicts a live one**: at capacity a new token is
refused with `ReasonBusy` until an entry expires, and tokens already
tracked keep their allowance. Evicting the oldest would let an attacker
cycling one more token than capacity restore every allowance without a
new solve (Astra). Tito's rule (intake_subordinate.go:81-84).

The trade it makes, stated: an attacker holding `Tracked` admitted tokens
can make that form answer "busy" until they expire — 100,000 solves under
proof, 100,000 page loads under `NoProof`. And because committed tokens
also hold an entry until expiry, an honest form taking more than
`Tracked` submissions in `MaxAge` meets the same ceiling; raise `Tracked`
for such a form. Forms using only `Check` are unaffected either way.

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
  is nil, with the expiry predicate above.
- `Sweep` deletes at most 500 rows per call, ten minutes past expiry on
  the database's clock. Unbounded, a sweep on wake holds the single
  writer while the first visitor waits (Tito bounds its own,
  T/internal/instance/intake_store.go:157, 232-248). Call it from a tick.
- `MemoryNonces` applies the same rules under its mutex and ignores
  `ex`, so a spend inside a transaction that rolls back stays spent. Its
  doc says so; it stays a test and single-process tool.
- **Schema change:** `pow_spent_nonces` gains `expires_ms INTEGER`
  (milliseconds, comparable in SQL) and drops the RFC 3339 `expires_at`.
  The table is empty everywhere that matters — no app has shipped `pow`
  — so the migration recreates it.

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
- **Minimum age, client side.** `Attrs` carries `data-pow-min-age`: the
  age the token still lacks at render, `MinAge` minus time since issue,
  never below zero — so a `FollowOn` or `Recovery` form, issued already
  eligible, carries zero and is never held. The module holds a submit
  until that long after it initialised. The
  one-tap sign-in buttons autofocus (ui/partials/signin.html:50, 57), and
  a returning visitor can be quicker than any minimum. Network latency
  only makes the server's measured age longer than the client's.
- **Navigation.** While a submit is held, a temporary `beforeunload`
  listener cancels it the moment the visitor starts navigating away —
  `pagehide` fires only once the destination commits, and until then a
  solve finishing would submit over the navigation the visitor chose
  (Tito handles the same window this way,
  T/internal/instance/static/spinner.js:133-152). The listener exists
  only during a hold, so it does not keep ordinary pages out of the
  back-forward cache.
- **`ui/busy.js` holds too.** The shipped busy script, loaded by the
  default stage layout (ui/layouts/stage.html:10) and the notes example,
  adds its own 650 ms hold before submitting and cancels only on
  `pagehide` (ui/busy.js:186, 227, 240-243) — the same slow-destination
  race, independent of `pow`. Two changes: `busy.js` gains the same
  `beforeunload` cancellation during its hold, and it skips its hold
  for a submit `pow` has just released (the form carries
  `data-pow-released`), since the visitor has already watched the
  working state. `busy.js` is vendored into apps once, so the CHANGELOG
  tells existing apps to refresh it; tests load both scripts together. `pagehide` terminates the worker; `pageshow` from
  the back-forward cache restores the controls `pow` disabled and
  restarts the solve if it never finished (`ui/busy.js:237-250` treats
  the same boundary, but cannot cancel another module's worker).
- **Submit controls outside the form.** The module finds the submit by
  `form.elements` and `[data-pow-submit][form=<id>]`, not only
  `form.querySelector` — Tito's checkout button sits outside its form
  (T/internal/instance/templates/checkout_details.html:121, 231).
- **Failure is visible with JavaScript on.** `<noscript>` covers only
  JavaScript off. With it on, the module can be blocked by CSP, 404, or
  throw, and the worker can fail to construct. `Fields` renders a status
  line beside the submit, **visible from the start**, whose words are
  true whether or not the module ever runs — along the lines of "If this
  form does not respond, reload the page" (final copy through
  copy-review). The module hides it when it marks the form ready, and
  shows it again, with a reload link, on worker failure. No stylesheet,
  no timer and no CSP change: if nothing runs, the line is simply still
  there and still right. Worker construction is wrapped in the same
  handling. Tests cover a missing asset, a restrictive CSP and a
  throwing worker.
- **`whenSolved(form, {timeout})`** lets a subordinate request carry
  the parent's proof, replacing Tito's `intake-proof.js`
  (T/internal/instance/static/intake-proof.js:10-35, awaited by
  email-check.js:154 and file-field.js:91). Its contract:
  - resolves with `{fields}` — the `pow_*` name/value pairs a request
    must carry for `Verify`;
  - a form with no `data-pow-form` (`NoProof`, or not a pow form)
    resolves immediately with the challenge fields it has;
  - already solved: resolves immediately;
  - bound: rejects at once — a bound proof cannot be `Verify`'d (above);
  - rejects on worker failure, on navigation cancelling the solve, and
    after `timeout` (default 10 s, Tito's bound). Never pending forever.
  `pow:solved` fires on the form as well.
- **`NoProof` needs no JavaScript.** `Attrs` and `Script` render
  nothing, `NeedsScript` is false so the submit is rendered enabled, and
  the minimum age is the server's alone. A form the visitor reaches
  already filled in is issued with `FollowOn`, so it is usable the
  instant it renders — Paul's decision for Tito's checkout.

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
- **A bound Guard is `ErrProofMode`.** The sign-in forms carry the
  address in three different shapes (two hidden inputs, one field
  partial, signin.html:47-67) and binding them is not worth a second
  contract when unbound is the default. A `NoProof` Guard is accepted and
  renders correctly: the partial disables the submit only when
  `NeedsScript` is true, so it never leaves a disabled button with no
  script to enable it.
- **`Begin` checks first,** after the same-origin check and before the
  rate limiter and classification (auth/handlers.go:41-57), with `Check`
  at scope `rastrillo/auth/begin`. An anonymous visitor can no longer
  make the server resolve and probe a domain of their choosing, send
  mail, or spend another address's rate budget without solving.
- **A refusal redirects to `?err=check&rec=1`,** a new problem the
  screen shows on the ask step. Its copy goes through copy-review and
  into every locale's catalog (`rastrillo.ui.signin_problem_check`)
  before it is written into a template. Every other error redirect from
  `Begin` also carries `rec=1` when the posted token was a recovery token
  (`adm.Recovered()`), so recovery stays sticky across a mistyped
  address. `rec` is attacker-controllable and only selects a recovery
  form — the cost already stated under "Recovery".
- **`SigninState.Proof *pow.Form`,** filled whenever `Proof` is set,
  screen or no screen: `Recovery` when the query carries `rec=1`, `Form`
  otherwise.
  Neither writes, so `SigninState` stays free of database access
  (auth/signin.go:116-125).
- **The shipped partial** (ui/partials/signin.html) renders exactly one
  `Begin` form per render — the keymail one-tap, the link one-tap, or the
  ask form are mutually exclusive branches (signin.html:47-67). Whichever
  renders carries `Fields` and `Attrs`; when `NeedsScript`, its submit is
  rendered disabled with `data-pow-submit`, with a `<noscript>` line and
  `Script` once. The
  Forget form is not gated: it deletes cookies and nothing else.

## 3. `password`

- `Config.Proof *pow.Guard` / `ProofOff bool`, the same `ErrProofUnset`
  and `ErrProofMode` for a bound Guard. The app can pass the same Guard
  it gave `auth`; scope keeps their tokens apart, and `Check` keeps them
  out of each other's capacity.
- `PageData.Proof *pow.Form`, so the app's `RenderSignin` /
  `RenderSignup` render it: `Recovery` after a refusal or after any
  failure whose posted token was a recovery token, `Form` otherwise.
- `SigninPage`, `SignupPage` and every re-render set `Cache-Control:
  no-store` before the callback.
- `Signin` and `Signup` `Check` before the limiter
  (password/handlers.go:193, 287) at scopes `rastrillo/password/signin`
  and `…/signup`. A refusal re-renders with a new `ErrCheck` message; the
  email is kept, the password is not (as for any failure today).
- `Check`, not `Admit`/`Commit`: a wrong password must not leave a solved
  token reusable for more guesses, and the re-render's fresh challenge
  solves while the visitor retypes.
- `ui/partials/form-foot.html` gains an optional `Proof` key that, when
  `NeedsScript`, renders its submit disabled with `data-pow-submit` plus
  the `<noscript>` line.

## 4. Docs and the example

- `examples/notes` (password sign-in, examples/notes/internal/notes/app.go:65)
  wires it end to end: a stable instance key, `pow.Schema` merged and
  applied before `pow.New`, one Guard, assets mounted, `Proof` on
  `password`, templates rendering `.Proof`, and a periodic `Sweep` — the
  example has no tick today, so it gains one on rastrillo's background
  group. `auth` is shown in the docs rather than the example.
- **Constructor inventory.** Every `auth.New` and `password.New` call
  must choose `Proof` or `ProofOff`: in `auth/`, `password/`, `ui/`,
  `examples/`, and `passkey/signinscreen_test.go:201`. The plan lists
  them all by grep before the first change.
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
  `adm.Commit(ctx, tx)`. `intakeConsume` becomes `Check`.
- The email check and uploads call `Verify`, authorise the returned
  `Parent` (any scope in the account for the email check; the upload's
  own event for uploads), key their 50 and 25 allowances by
  `Parent.Nonce`, and read the proof in the browser through `whenSolved`.
- Checkout details, confirm screens and their validation re-renders are
  issued with `FollowOn`, as `shop.go:8288` and `postback.go:122` do
  today.
- The step-up screen re-renders with `Recovery`, and calls `init` after
  replacing the document.
- Upload callers must handle `whenSolved` rejecting: Tito's helper
  resolves on timeout (T/internal/instance/static/intake-proof.js:26-36)
  and `file-field.js` relies on that, with uncaught awaits and cleanup
  only on success (file-field.js:91, 149-177). The migration catches the
  rejection, shows a retryable upload error, and clears the input and
  required state in `finally`; the plan tests a timeout followed by
  choosing the same file again.
- **Old forms at deploy** post `intake_token` / `intake_nonce`, which the
  new Guard sees as `ReasonMissing` — and Tito answers a missing token
  with a plain 429 today (T/internal/instance/intake_stepup.go:124). For
  one release, a POST carrying `intake_token` and no `pow_*` fields gets
  the step-up screen with the visitor's answers preserved and a
  `Recovery` form; the plan tests it per door.
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

- seal: scopes containing NUL and length-like bytes do not
  collide; a non-hex or wrong-length nonce is refused before HMAC; a v1
  seal is refused; issue on both sides of a second boundary honours a
  500 ms minimum;
- `Commit`: concurrent commits give exactly one nil; rollback leaves the
  token admissible; an un-committed admission leaves it admissible;
  an executor stalled after the handler's checks until past expiry and a
  sweep is `ErrSpent` (the predicate is on the database clock);
  a rolled-back commit keeps the attempt count; `ErrNotAdmitted` on a refused admission; GORM through
  `tx.Statement.ConnPool`;
- `Admit`: scope mismatch is `ReasonSealInvalid`; `ReasonMissing` with no
  challenge fields; `Also` carries cheap failures only;
- attempts: the 21st admission is refused, across commits that roll
  back; at capacity a new token is `ReasonBusy` and tracked tokens are
  unaffected; cycling capacity + 1 tokens restores no allowance; `Check`
  succeeds while another scope has filled the Guard's map;
- `Commit` returning `ErrSpent` inside a transaction on a one-connection
  pool returns promptly (no second statement outside the transaction);
- `Recovery` is trapless and admissible at once; only recovery tokens
  skip the honeypot; a `NoProof` fast visitor with a trap-filling
  password manager gets through on the first recovery; `Recovered()` is
  true on a refused recovery token;
- `FollowOn` is admissible at once and keeps the trap;
- `Verify` neither counts nor spends, returns the sealed scope, and
  accepts a token from any Guard sharing the key at its sealed
  difficulty; `Sweep` is bounded and respects the
  margin; `New` refuses a missing table and missing asset URLs;
- Chromium: shipped solver satisfies the Go verifier in both modes; an
  unbound form solves before submit; a fast click is held, not refused;
  a bound form whose binding changes mid-solve re-solves; leave and
  return via bfcache during both holds; start navigating to a slow
  destination during a hold and the held submit never fires; `init`
  after `document.write`; a submit control outside the form; missing
  asset, restrictive CSP and a throwing worker each leave the status line
  showing; `whenSolved` resolves at once for `NoProof` and when solved,
  starts a bound solve, and rejects on failure, navigation and timeout;
  `NoProof` posts with JavaScript disabled.

`auth` / `password`:

- neither `Proof` nor `ProofOff` is `ErrProofUnset`; a bound Guard is
  `ErrProofMode`; `ProofOff` boots and gates nothing; a `NoProof` Guard
  renders an enabled submit;
- `Begin` with no challenge redirects to `?err=check` and sends no mail,
  writes no link, makes no DNS lookup (the fakes record none) and spends
  no rate budget;
- replay of a spent challenge is refused; honeypot refusal leads to a
  trapless form that then succeeds;
- each rendered sign-in state (keymail one-tap, link one-tap, ask,
  passkey + ask) carries the fields on its one `Begin` form and none on
  Forget; existing screen tests pass with Proof on; a recovered token
  that then fails on the address keeps `rec=1`;
- `password`: pages and re-renders are `no-store`; a wrong password
  re-renders with a fresh challenge, a recovery one if the post was a
  recovery; signup without a challenge creates no row.

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
- **`unixepoch('subsec')`** needs SQLite 3.42; the plan confirms the
  version bundled with `modernc.org/sqlite` v1.55.0 and falls back to
  `julianday('now')` arithmetic if not.

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

**Astra, round 2 (2026-10-03): not ready.** Round one re-verdicted ten
resolved, six partial; eleven new findings, all accepted:

17. Expiry checked in Go before the executor is defeated by a stalled
    request (blocker) → expiry predicate inside the insert, on the
    database clock; sweep on the same clock.
18. Subordinate endpoints cannot learn or authorise the parent's scope
    (blocker) → scope posted and sealed; `Verify` returns an
    authenticated `Parent` with the canonical nonce.
19. `Commit` releasing the attempt entry lets rollbacks reset it (major)
    → entries live until expiry; `Tracked` raised to 100,000 and
    configurable, trade-off stated.
20. A shared Guard's full map blocks `Check` callers (major) → `Check`
    bypasses the map.
21. Per-reason recovery properties undo each other (major) → recovery is
    always trapless and follow-on, and sticky via `Recovered()`.
22. Initial checkout lost immediate eligibility (major, Paul's decision)
    → `FollowOn` for prefilled forms and their re-renders.
23. `whenSolved` had no completion contract (major) → defined, bounded,
    never pending forever.
24. Guard modes the shipped forms cannot render (major) → bound Guards
    refused; `NeedsScript` drives the disabled submit.
25. `pagehide` fires too late to cancel a held submit (major) →
    `beforeunload` during holds only.
26. The failure notice needed an unshipped stylesheet (minor) → a status
    line visible from the start whose words are true either way; no CSS.
27. Migration inventory wrong (minor) → example wiring corrected,
    constructor inventory listed, boot order fixed in the reference doc.

**Astra, round 3 (2026-10-03): not ready.** Findings 1-27 re-verdicted
resolved, except 8 and 25 partial (busy.js, below). Six new, all
accepted:

28. Classifying a zero-row spend with a second lookup deadlocks a
    one-connection pool (blocker) → one `ErrSpent` for spent or expired;
    `ErrExpired` removed.
29. `Verify` cannot check a bound proof (major) → "bound" sealed;
    `Verify` refuses bound tokens; `whenSolved` rejects on bound forms.
30. A rejecting `whenSolved` breaks Tito's upload recovery (major) →
    migration requirement and test.
31. `busy.js`'s own hold re-opens the navigation race (major) →
    `beforeunload` in busy.js, and no second hold after a `pow` release.
32. Follow-on forms still held for the full minimum age (minor) →
    `data-pow-min-age` is the remaining age.
33. Legacy recovery used `Form` (minor) → `Recovery`.
