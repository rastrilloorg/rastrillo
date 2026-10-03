# Test, CI, package-size and screen-time budgets

**Date:** 2026-10-03
**Decided by:** Paul (scope, the 5k/8k ceiling, the feature split, the test
cache rule); the measurements are from the apps on this machine, cited
below.
**Branch:** `test-budgets`

## Why

Every Rastrillo app on this machine is heading where Tito Go already went,
and the scaffold is pointing them there.

- **Tito Go's `internal/instance` is one package of 2,450 files.** It grew
  from 111k lines (2026-08-01) to 957k. A one-line edit costs 28s to build
  and 72s to link the test binary; whole-package local runs went from 2.1h
  a week to 33h and slowed every other `go test` on the box from a 2.5s
  median to 13.9s (titogo `docs/testing.md` § Running tests locally).
- **The others are on the same curve.** carlos/platform's
  `internal/console` is 49k source / 62k test lines and its root package
  takes 147s in CI; amadan's `internal/hub` is 30k/44k and `internal/cli`
  is its critical path; seapointish's `apptest` is an 18k-line serial
  test package.
- **SKILL.md §1 offers one package, `internal/<app>`, and no point at which
  to split it.** Nothing in the framework says when an app has outgrown
  that shape.
- **The scaffold's harness migrates a fresh database in every test**
  (`cmd/rastrillo/new.go`, `harnessTemplate`). Tito Go measured that
  fixture at 207ms a test against 2.5ms for copying a migrated file, and
  found it was the whole cost of three packages: billing 160s → 12s,
  partners 79s → 13s. "A flat cost distribution is a fixture, not a suite."
- **Almost nothing runs in parallel**: about 7 `t.Parallel()` calls across
  ~10,800 test functions in the surveyed apps.
- **Rebuilding binaries inside tests** is the second recurring cost.
  platform's `buildCarlos` runs `go build` at 41 call sites; amadan fixed
  the same shape with `sync.Once` plus `t.Parallel()` and took its root
  package from 26.85s to ~8.5s.
- **Local practice is "run the whole gate".** Only comercio and seapointish
  tell agents to run the package they touched. Tito Go's rule, learned the
  expensive way, is that the gate is CI's job.
- **Screen time has a budget in two apps and none in the framework.**
  Seapointish and Tito Go both hold GET screens to 150ms warm and boot to
  500ms cold (titogo `docs/superpowers/specs/2026-08-31-performance-budget-design.md`);
  Rastrillo says nothing about it.

The goal is a scaffolded app whose gate stays near a minute as it grows,
because the budgets that keep it there are mechanical rather than
remembered.

## Decisions

1. **Docs plus scaffolded fences.** The rules live in SKILL.md and
   `docs/site/testing.md`; new apps get fences wired into `make ci`. The
   precedent is `skillmd_test.go`: a budget is a test, not a habit.
2. **Package ceiling: 5,000 non-test lines and 8,000 test lines per
   package.** Just above Rastrillo's own largest package
   (`internal/generate`, 4,488/5,137); a scaffolded `internal/<app>`
   reaches it at roughly 15–25 screens.
3. **Split by feature.** `internal/<app>` keeps the shared core; each
   feature becomes `internal/<app>/<feature>` exposing
   `Mount(r chi.Router, deps)`. Rejected: splitting by layer
   (`store/`, `web/`), because every handler edit still recompiles and
   retests the whole web layer.
4. **Go's test cache stays on in CI.** The runner keeps a persistent
   `GOCACHE`, so untouched packages are free; that is the cheapest route
   to a one-minute gate. amadan's `-count=1` everywhere (~95s against ~9s
   cached) was rejected for scaffolded apps. **The rule that buys it:** a
   test whose inputs Go cannot see (it execs `go build`, reads files
   outside its package, or reaches the network) runs under `-count=1` in a
   make step of its own. The scaffold harness is in-process and safe.
5. **Screen time is in scope**, at Seapointish and Tito Go's numbers.

## The budgets

| Budget | Default | Gated? | Mechanism |
|---|---|---|---|
| Package size | 5,000 non-test / 8,000 test lines | Yes | `rastrillo budget size` |
| Package test time | 10s per package that ran | Yes | `rastrillo budget tests` |
| Whole gate | 60s on the CI runner | **No**, reported every run | `rastrillo budget tests` receipt |
| Warm screen | 150ms TTFB at the handler, per GET route | Yes | `rastrillo/perfbudget` |
| Cold boot | 500ms from `App()` to first response | Yes | `rastrillo/perfbudget` |

The whole-gate number is reported and never gated: a wall-clock gate on a
shared runner is a flake generator, and Tito Go's gate already goes red a
fifth of the time on flakes alone.

**Every gated budget has the same shape.** The default appears in code and
nowhere else. Exceptions live in one hand-written file,
`.rastrillo/budgets.txt`, one line each, carrying a number and a reason:

```
size  internal/notes            7200 9000  billing split owed; #41
tests internal/notes/importer   25s        replays a 40MB fixture
perf  GET /notes/export         1200ms     streams every row
```

Three fences hold that file honest, none calendar-based:

1. **Nothing unbudgeted.** Anything over the default without a line fails.
2. **No stale lines.** A line naming a package or route that no longer
   exists fails, or the file becomes a graveyard.
3. **The ratchet**, on size and perf only: a measurement under 60% of its
   exemption fails with "measured 3,100, exemption says 7,200 — lower it".
   Test time has no ratchet; runner noise would make it flake.

## Components

### 1. `rastrillo/dbtest` (new framework package)

Migrates a database once per test binary, then copies the file per test.

- `dbtest.Open(t, migrate func(*db.DB) error) *db.DB`. The first call in
  a binary builds a template in a process-private temp directory by
  running the app's own migration through the real `db.Open`; each call
  copies the template into `t.TempDir()` and opens the copy through the
  real `db.Open`, so pragmas and pool wiring are production's.
- **The fence is built in, not optional.** The first `Open` in a binary
  also migrates a second, fresh database and compares `sqlite_master`
  plus every row of every table between the two. `rastrillo_migrations`
  is compared on `id` and `checksum`; `applied_at` legitimately differs.
  A mismatch fails the test with the first differing object named. This is
  Tito Go's `TestTemplateDBMatchesAFreshMigration`, made unavoidable.
- **Values a migration generates per database** would be shared by every
  copy. An audit of `sessions`, `keyring`, `vault`, `passkey`, `auth` and
  `password` found none at migration time (their `INSERT`s and random
  values are runtime, per request). The row comparison is what catches a
  future one: two fresh migrations that disagree fail with "this
  migration generates a per-database value; dbtest cannot share it".
- Templates are deleted when the binary exits (`TestMain`-free: a
  `sync.Once` plus a cleanup registered on the first test). A killed
  binary leaves one file in `$TMPDIR`; acceptable, and named
  `rastrillo-dbtest-*` so it is findable.

### 2. `rastrillo budget` (new CLI subcommand)

Run from `make` through `go run github.com/carlosframework/rastrillo/cmd/rastrillo`,
the way `migration-check` already is, so CI needs nothing on `PATH`.

- **`budget size [dir]`.** Walks the directory tree (not `go list`, so a
  nested module like rutline's `home/` cannot escape) and counts lines per
  package directory, split into `_test.go` and the rest. Skips what Go
  itself skips (`.`- and `_`-prefixed directories, `testdata`, `vendor`)
  plus `node_modules`. **Generated code counts**: it costs compile time
  like anything else, and seapointish's `gen/actions` is where its bloat
  sits; an app that needs it takes an exemption.
- **`budget tests`.** Reads `go test -json` on stdin.
  - Holds each package that actually ran to 10s (`elapsed` on its final
    `pass`/`fail` event); cached packages are skipped, by decision 4.
  - **Owns the exit status.** In `go test -json ./... | rastrillo budget
    tests` the shell reports only the last command's status, so a red
    `go test` would otherwise pass. It fails on any `fail` or
    `build-fail` event, and on a stream reporting zero packages (Tito
    Go's "refusing a truncated suite").
  - Echoes failing tests' output in full, and nothing else verbose, so
    the log of a red run leads with what failed.
  - Prints a receipt every run, gated or not: wall time against the 60s
    target, packages run against cached, the five slowest packages and
    the five slowest tests.
- **`budget lint`.** Advisory, exits 0: tests whose first statement is not
  `t.Parallel()`, tests that exec `go build` outside a `sync.Once`, and
  package-level `var`s assigned in test files. Static checks of these have
  false positives, so it reports and gates nothing.

### 3. `rastrillo/perfbudget` (new framework package)

- `perfbudget.Screens(t, routes chi.Routes, h http.Handler, opts)` walks
  every GET route with `chi.Walk`. Each one must be measured or appear in
  `.rastrillo/budgets.txt`; an unbudgeted route fails the moment it is
  added.
- **Protocol (Tito Go's, unchanged):** 10 requests, discard the first 2,
  hold the median of the remaining 8 to 150ms (or the route's
  exemption), and fail any single request over 3× that. TTFB is measured
  by wrapping `h` and stamping the first `WriteHeader`/`Write`, so it is
  the handler's time, not the client's.
- `opts.Params` maps a route pattern to a concrete path
  (`"/notes/{id}": "/notes/1"`) and `opts.Signin` signs the measuring
  client in, since most screens are behind `sess.Require`. A pattern with
  parameters and no entry fails rather than being skipped.
- `perfbudget.Boot(t, build func() (http.Handler, error), path)` times
  `build` plus the first response to `path` against 500ms on a database
  that is migrated but cold to this process.
- **Scaffold consequence:** `App()` returns `*http.ServeMux`, which
  `chi.Walk` cannot see. The scaffold splits it into `Router(...)
  (chi.Router, error)` and a thin `App` that mounts it, so the perf test
  can walk the routes the server actually serves.

### 4. Scaffold changes (`rastrillo new`)

- **Harness** uses `dbtest.Open`; every example test starts with
  `t.Parallel()`.
- **A perf test** (`internal/<app>test/perf_test.go`) calling
  `perfbudget.Screens` and `perfbudget.Boot`, passing out of the box.
- **Makefile:**
  - `test: go test -json ./... | go run …/cmd/rastrillo budget tests`
  - `budget: go run …/cmd/rastrillo budget size`
  - `ci: vet fmt-check budget test migration-check`. Perf runs inside
    `test`, as an ordinary test.
- **`.amadan/ci.d/`** gains `15-budget`, one file per target as
  `AGENTS.md` requires. `budget` runs before `test`: it is cheap and
  structural, so it fails in seconds.
- **`.rastrillo/budgets.txt`** is written empty with a header saying
  what a line looks like and that every line needs a reason.
- **Scaffolded `AGENTS.md`** gains the local loop: run `go test -short
  ./internal/<pkg>` for the package you are touching, and leave the full
  gate to CI. Running the whole suite locally is never what makes a
  branch ready.

### 5. Docs

- **SKILL.md** gains a short section, about 900 bytes: the five budgets,
  the local loop, the feature split, `dbtest`, `t.Parallel()` first, and
  the `-count=1` rule. §1 gains one line: past 5,000 lines, split
  `internal/<app>` by feature. A trim pass comes first; `skillBudget`
  rises to 19,000 only for what the trim cannot pay for, with the reason
  written in `skillmd_test.go` beside the earlier raises.
- **`docs/site/testing.md`** gets the full treatment: each budget and its
  reasoning, the feature-split recipe, the practices below with the
  measurements behind them, and "Adopting the budgets in an existing app".
  - `t.Parallel()` as the first statement; anything before it runs
    serially.
  - One database migration per test binary (`dbtest`).
  - Build any binary a test needs once, behind `sync.Once`.
  - Browser tests only for what only a browser can prove, behind
    `-tags browser`, as the scaffold already does.
  - A required tool that is missing in CI fails; it never skips.
  - Reproduce a CI-only failure with `GOMAXPROCS=1 go test -count=10
    -run TestX`, and remove the race rather than lengthening the wait.
  - Every speed change carries a fence, and a fence nobody has watched
    fail is not a fence.

## Proving the fences

Every fence ships with a test that shows it going red, one mutation per
assertion (Tito Go: the first failing assertion masks every one after it).

- **`budget size`:** a fixture tree with a 5,001-line package fails; at
  exactly 5,000 it passes; a stale exemption fails; an exemption measured
  under 60% fails; a package hidden in a nested module is still counted.
- **`budget tests`:** canned `-json` streams for passing, over budget,
  cached, a failing test, a build failure and an empty stream, each
  asserting the exit code and the receipt.
- **`perfbudget`:** a fixture router with a 200ms route fails; a route
  with no budget fails; a stale exemption fails; one slow request inside a
  passing median fails on the 3× ceiling; a parameterised route with no
  `Params` entry fails.
- **`dbtest`:** a migration edited after the template was built fails the
  schema comparison; a migration inserting a random value fails the
  two-fresh-databases check; a copy is writable without touching the
  template.
- **Scaffold:** `new_scaffold_test.go` builds a fresh app and runs its
  `make ci` end to end, as it does today, now including `budget` and the
  perf test.
- **Dogfooding:** Rastrillo's own gate runs `budget size` over its repo,
  examples included. It fits today.

## Rollout

One branch, landed in order: `dbtest`, `budget`, `perfbudget`, the
scaffold, the docs. Nothing changes for an existing app until it opts in.
The adoption recipe in `docs/site/testing.md` is: run `budget size`, write
each over-budget package into `.rastrillo/budgets.txt` with a reason, and
let the ratchet walk them down.

## Out of scope

- Retrofitting existing apps; each adopts in its own branch.
- platform's and seapointish's instructions to put `GOCACHE` under `/tmp`
  or in the worktree, which break this machine's cache rule. Worth raising
  in those repos separately.
- Sharding. A scaffolded app inside its budgets should not need it; Tito
  Go's shard planner stays Tito Go's.
- A race-detector lane. sheets-core's scoped `-race` step is the model if
  an app needs one; the docs mention it.
