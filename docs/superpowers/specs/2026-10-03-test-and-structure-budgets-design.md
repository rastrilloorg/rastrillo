# Test, CI, directory-size and screen-time budgets

**Date:** 2026-10-03, revised the same day after an adversarial review
(Astra; findings resolved in place, listed in § Review record)
**Decided by:** Paul (scope, the 5k/8k ceiling, the feature split, keeping
Go's test cache on, screen budgets in scope); the measurements are from
the apps on this machine, cited below.
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
  to split it.**
- **The scaffold's harness migrates a fresh database in every test**
  (`cmd/rastrillo/new.go`, `harnessTemplate`: `db.Open` on a new temp file,
  then `App` runs `migrate.Apply`). Rastrillo already ships the fix,
  `dbtest` (one migrated template per test binary, copied per test; Tito
  Go measured 207ms against 2.5ms), and the scaffold does not use it.
- **Almost nothing runs in parallel**: about 7 `t.Parallel()` calls across
  ~10,800 test functions in the surveyed apps.
- **Rebuilding binaries inside tests** is the second recurring cost.
  platform's `buildCarlos` runs `go build` at 41 call sites; amadan fixed
  the same shape with `sync.Once` plus `t.Parallel()` and took its root
  package from 26.85s to ~8.5s.
- **Screen time has a budget in two apps and none in the framework.**
  Seapointish and Tito Go both hold GET screens to 150ms warm and boot to
  500ms cold (titogo `docs/superpowers/specs/2026-08-31-performance-budget-design.md`).

The goal is a scaffolded app whose gate stays near a minute as it grows,
because the budgets that keep it there are mechanical rather than
remembered.

## Decisions

1. **Docs plus scaffolded fences.** The rules live in SKILL.md and
   `docs/site/testing.md`; new apps get fences wired into `make ci`. The
   precedent is `skillmd_test.go`: a budget is a test, not a habit.
2. **Directory ceiling: 5,000 non-test and 8,000 test lines.** Measured
   per directory, not per compiled package (see § `budget size`). A
   scaffolded `internal/<app>` reaches it at roughly 15–25 screens.
3. **Split by feature.** `internal/<app>` keeps the shared core; each
   feature becomes `internal/<app>/<feature>` exposing
   `Mount(r chi.Router, deps)`. Rejected: splitting by layer (`store/`,
   `web/`), because every handler edit still recompiles and retests the
   whole web layer.
4. **Go's test cache stays on, and the time budget is enforced inside the
   test binary so the cache cannot erase it.** Go caches only passing
   runs. A package whose binary fails its own budget is never cached, so a
   retry of the same commit fails again. Rejected: judging time from the
   `go test -json` stream, because a cached replay of an over-budget
   package passes (review finding 2); and `-count=1` everywhere (amadan's
   choice, ~95s against ~9s cached), because it pays for the whole suite
   on every push.
5. **Screen time runs in its own serial, uncached lane**, never inside the
   ordinary suite: parallel tests skew it, and its sweep would consume the
   package-time budget by construction (review findings 9 and 13).
6. **Local loop and pre-push are different things.** The edit loop runs
   the package being changed plus the harness package. `make ci` before
   pushing stays the rule (repo and scaffold `AGENTS.md` both say so), and
   the budgets are what keep it cheap enough to obey. Tito Go's "never run
   the suite locally" is the state an app reaches when a budget has
   already failed.

## The budgets

| Budget | Default | Gated? | Where it is enforced |
|---|---|---|---|
| Directory size | 5,000 non-test / 8,000 test lines | Yes | `rastrillo budget size` |
| Package test time | 10s per test binary's `m.Run` | Yes, in CI | `budget.Main` inside each test binary |
| Warm screen | 150ms time-to-first-byte, median of 8 | Yes | `budget.Screens`, perf lane |
| Cold boot | 500ms, process start to first byte | Yes | `budget.Boot`, perf lane |
| Test step | 60s on the CI runner | **No**, reported | `rastrillo budget test` receipt |

The test-step figure is reported, never gated: a wall-clock gate on a
shared runner is a flake generator, and Tito Go's gate already goes red a
fifth of the time on flakes alone. It covers only the `go test` step; the
whole gate's time is the runner's own figure.

**Timing budgets mean the CI runner.** Local runs report against the same
numbers and never fail on them; a loaded laptop is not evidence.

## The exemptions file

`.rastrillo/budgets.txt`, one per module, at the directory holding
`go.mod`. A nested module (rutline's `home/`, Rastrillo's `examples/*`)
has its own. Finding it: from the working directory, walk up to the first
`go.mod`; the file sits beside it. A missing file means no exemptions.

Grammar, one record per line; `#` starts a comment; blank lines ignored;
fields are whitespace-separated and the reason is the rest of the line:

```
size  <dir>               <src-lines> <test-lines>  <reason>
time  <dir>               <duration>                <reason>
perf  <METHOD> <pattern>  <duration>|skip           <reason>
boot                      <duration>                <reason>
```

- `<dir>` is slash-separated, relative to the module root (`internal/notes`,
  `.` for the root). `<duration>` is a Go duration (`25s`, `1200ms`).
  `<pattern>` is the chi pattern exactly as `chi.Walk` reports it.
- A `size` record states both ceilings; the default fills neither in.
- **Refused, with the line number:** an unknown kind, a missing or empty
  reason, an unparseable number or duration, a zero or negative value, two
  records for the same key, a `size` record no larger than the defaults on
  both counts.
- **Stale records fail.** `budget size` fails a `size` or `time` record
  whose directory no longer exists or holds no Go files. `budget.Screens`
  fails a `perf` record naming a route it did not find, because it is the
  complete inventory (one call per module; see § `budget.Screens`).
- **The ratchet, on size only:** a directory measuring under 60% of its
  exemption on either count fails with "measured 3,100, exemption says
  7,200: lower it". Timing has no ratchet; runner noise would make it
  flake. `budget.Screens` prints an advisory line instead when a route
  measures under 60% of its exemption.

One parser, `internal/budgetfile`, serves the CLI and the public package.

## Components

### 1. `rastrillo budget size`

Walks the module tree from the root and counts lines per directory, split
into `_test.go` and the rest.

- **A directory, not a compiled package.** Every `.go` file counts,
  whatever its build constraints, and internal and external (`_test`
  package) test files count together. That is the honest proxy for "how
  much does an edit here make the toolchain reread", and it is
  deterministic. Named for what it is: the directory size budget.
- **Skips** what Go skips (`.`- and `_`-prefixed directories, `testdata`,
  `vendor`) plus `node_modules`, and **stops at nested modules**: a
  directory with its own `go.mod` is that module's to budget.
- **Generated code counts.** It costs compile time like anything else. The
  remedy for a generator-owned directory is splitting the manifests that
  feed it, or an exemption saying so; the docs say both.
- Exit 0 clean, 1 on any violation, 2 on a malformed `budgets.txt`.
  Output lists every violation, then a one-line summary.

### 2. `budget.Main`, the in-binary time budget

New public package `amadan.net/rastrillo/rastrillo/budget`, GORM-free
(joins `GORM_FREE` in the Makefile).

```go
func TestMain(m *testing.M) { os.Exit(budget.Main(m)) }
```

- Times `m.Run()` and finds this package's `time` record (default 10s) by
  locating the module root from the test's working directory, which `go
  test` sets to the package directory.
- Prints `rastrillo budget: armed internal/notes 10s` before running, and
  after: `rastrillo budget: internal/notes 2.4s of 10s`.
- **Enforces only when `RASTRILLO_BUDGET=enforce`**, which the scaffold's
  CI targets set. Over budget there, it prints `rastrillo budget: FAIL
  internal/notes took 12.3s, budget 10s` and returns 1, so the package
  fails and Go does not cache it. Without the variable it reports and
  returns `m.Run`'s code. Go records environment variables a test reads in
  its cache key, so local and CI runs never share a cached verdict.
- `budgets.txt` is read through `os.ReadFile`, which Go's test cache also
  records, so editing an exemption re-runs the packages it affects.
- An app that already has a `TestMain` calls `budget.Main(m)` in place of
  `m.Run()` and does its own teardown after (`dbtest`'s `Remove`).

### 3. `rastrillo budget test`

Runs the suite and owns its verdict, replacing a pipe (review finding 1).

```
rastrillo budget test [go test flags and packages]
```

- Executes `go test -json <args>` itself and **exits with `go test`'s own
  status**; a stream that ends early cannot read as green. Forwards the
  child's stderr unchanged.
- Parses both event schemas: test events keyed by `Package`, and build
  events (`build-output`, `build-fail`) keyed by `ImportPath`.
- **Requires the marker:** a package that ran tests (any test-level event)
  without printing `rastrillo budget: armed` fails the step, naming the
  package and the one line to add. Packages that end in `skip` (no test
  files) are counted and exempt. Cached packages replay their output, the
  marker included, so they pass this check honestly.
- **Readable output**, so the log of a red run leads with what failed:
  every failed test's output in full; the full output of a package that
  failed without a failing named test (setup failure, panic, timeout,
  `budget.Main` refusal); every build failure's `build-output`. Passing
  output is dropped.
- **The receipt**, printed every run: test-step wall time against the 60s
  target; packages run, cached, skipped and failed; the five slowest
  packages that ran (from their `budget.Main` lines) and the five slowest
  tests. Labelled as the test step, not the gate.
- A malformed JSON line is passed through as output and counted; more than
  none fails the step after `go test` exits, because it means something
  else wrote to stdout.

### 4. `budget.Screens` and `budget.Boot`, screen time

In the same `budget` package, in a file the perf lane compiles: the
scaffold puts the app's perf test under `//go:build perf`.

```go
budget.Screens(t, budget.Screen{
	Routes:  r,              // chi.Routes: the app's router
	Handler: h,              // the assembled handler requests go through
	Paths:   map[string]string{"/notes/{id}": "/notes/1"},
	Signin:  func(req *http.Request) { ... }, // adds the session cookie
	Extra:   []string{"/bookmarks/new"},        // routes inside opaque mounts
})
budget.Boot(t, budget.BootConfig{Path: "/", Expect: 200})
```

- **Inventory.** `chi.Walk` over `Routes`, GET only. A mounted handler
  that is not a `chi.Routes` (a generated `http.ServeMux`, as
  `examples/notes` mounts at `/bookmarks`) is opaque: its mount pattern
  fails the inventory unless it appears in a `perf … skip` record or its
  concrete screens are listed in `Extra`. Framework routes (`/healthz`,
  `/api/version`) and the app's `/static/` are not screens and are never
  required. `Screens` is the complete inventory for the module, so it is
  where stale `perf` records fail; a second call in the same module is an
  error.
- **Requests go through `Handler`**, the assembled handler, so middleware,
  CSRF and sessions are what production runs.
- **A screen must succeed to be measured.** Each request must answer 200
  (or the status the route's `Expect` names). A 303 to sign-in or a 404
  for a missing row fails with "not measuring an error page"; seeding the
  rows `Paths` names is the test's job, in the same file.
- **Protocol:** 10 requests, discard 2, hold the median of 8 to 150ms (or
  the exemption), and fail any single request over 3× that. TTFB is
  stamped at the first `WriteHeader` or `Write`; the wrapper keeps
  `Flush`, `Hijack` and `Unwrap`, so `http.ResponseController` and SSE
  work. Each request carries a context deadline of 3× the budget plus 1s;
  a handler still running at the deadline is cancelled, and a streaming
  route (`text/event-stream`) is measured to first byte and then
  cancelled. `perf … skip` exempts a route outright, with its reason.
- **`Boot` is process-cold.** It re-executes the test binary with
  `-test.run` naming a child test and an environment flag; the child
  opens a `dbtest` copy, builds the app, serves one request to `Path`
  and prints the first-byte moment. The parent's clock runs from exec to
  that line, so process start, runtime and package init, `db.Open`,
  migrate (a no-op on the copy) and template parsing are all inside it.
  One measurement per run of a cold process cannot be repeated without a
  new process, so `Boot` runs it 3 times and holds the median to 500ms.
- **The perf lane:** `make perf` runs `go test -tags perf -count=1 -p 1
  -run '^TestPerf' ./...` through `rastrillo budget test`, without
  `RASTRILLO_BUDGET=enforce` so its sweep is never charged to the package
  time budget. `-count=1` because a cached screen measurement is not one;
  `-p 1` so no other package's binary shares the machine.

### 5. Scaffold changes (`rastrillo new`)

- **`App` keeps its signature** (`SKILL.md` §1, `cmd/<app>/main.go`) and
  is split inside `app.go` into `Router(d, origin, logger) (chi.Router,
  error)`, which migrates and builds the routes exactly as `App` does
  today, and `Mux(r http.Handler) *http.ServeMux`, which mounts static
  files and the router. `App` is `Router` then `Mux`. The perf test calls
  `Router` once and passes it as both `Routes` and, through `Mux`, as
  `Handler`.
- **Harness:** `var schema = dbtest.FromSet(BootSchema)`; `newApp` opens
  `db.Open(schema.Path(t), logger)`; a `TestMain` calls `budget.Main` and
  then `schema.Remove()`. Every example test starts with `t.Parallel()`.
- **`internal/<app>test/perf_test.go`** (`//go:build perf`):
  `TestPerfScreens` and `TestPerfBoot`, passing out of the box against
  the placeholder index.
- **`.rastrillo/budgets.txt`**, written empty except for a header giving
  the grammar and saying every line needs a reason.
- **Makefile** (module path `amadan.net/rastrillo/rastrillo`; `RASTRILLO :=
  go run amadan.net/rastrillo/rastrillo/cmd/rastrillo`):
  - `budget: $(RASTRILLO) budget size`
  - `test: RASTRILLO_BUDGET=enforce $(RASTRILLO) budget test ./...`
  - `perf: $(RASTRILLO) budget test -tags perf -count=1 -p 1 -run '^TestPerf' ./...`
  - `ci: vet fmt-check staticcheck budget test perf migration-check`
- **`.amadan/ci.d/`** gains `15-budget` and `35-perf`, one per target as
  `AGENTS.md` requires; staticcheck's step stays. `budget` runs before
  `test` because it is cheap and structural.
- **Scaffolded `AGENTS.md`** gains the loop: while editing, `go test
  -short ./internal/<app>/... ./internal/<app>test/`; before pushing,
  `make ci`; if `make ci` is too slow to run before every push, a budget
  has already failed, so fix that rather than skipping the gate.

### 6. Rastrillo's own gate

- Runs `budget size` over its own module. Today two directories are over:
  `internal/designsystem` (9,720 / 7,789) and `ui` (2,885 / 19,251). Both
  get `size` records with reasons in Rastrillo's own `budgets.txt`; the
  ratchet then holds them.
- The `example-%` targets add `budget size` per example module. Their test
  targets are unchanged: the examples are reference apps, not budgeted
  ones, until one adopts the scaffold's harness.

### 7. Docs

- **SKILL.md** (27,514 of 30,000 bytes) gains about 1,000 bytes: a short
  testing section (the budgets, `budget.Main`, the perf lane, `dbtest`,
  `t.Parallel()` first, the cache rule, edit loop against pre-push) and
  one line in §1: past 5,000 lines, split `internal/<app>` by feature.
  The ceiling does not move.
- **`docs/site/testing.md`** gets the full treatment, and
  `docs/site/reference/budget.md` the API:
  - each budget, its reasoning and its exemptions;
  - the feature-split recipe, including what to do about generated code;
  - `t.Parallel()` as the first statement (anything before it runs
    serially);
  - build any binary a test needs once, behind `sync.Once`;
  - the `-count=1` rule: a test whose inputs Go cannot see (execs `go
    build`, reads files outside its package, reaches the network) runs
    under `-count=1` in a make target of its own;
  - browser tests only for what only a browser can prove, behind `-tags
    browser`; a scoped `-race` target (sheets-core's model) where
    concurrency lives;
  - a required tool missing in CI fails, never skips;
  - `GOMAXPROCS=1 go test -count=10 -run TestX` to reproduce a CI-only
    failure, then remove the race rather than lengthening the wait;
  - every speed change carries a fence, and a fence nobody has watched
    fail is not a fence;
  - adopting the budgets in an existing app.

## Proving the fences

Each fence ships with tests that show it going red, one mutation per
assertion.

- **`internal/budgetfile`:** each refusal in § The exemptions file, by
  line number; a valid file round-trips.
- **`budget size`:** a fixture tree with 5,001 non-test lines fails; at
  exactly 5,000 it passes; 8,001 test lines fails; a stale record fails; a
  record under 60% fails; a nested module is not counted into its parent
  and is budgeted from its own root.
- **`budget.Main`:** run through a fixture module's real `go test`: over
  budget with enforce fails and a second identical run fails again (the
  cache did not keep it); over budget without enforce passes and reports;
  an exemption raises the limit; editing the exemption re-runs the
  package.
- **`rastrillo budget test`:** against real `go test` output from fixture
  modules, not only canned streams: a passing suite; a failing test; a
  build failure; a vet failure during `go test`; a `TestMain` setup
  failure; a panic; a `-timeout`; a package without the marker; a package
  with no test files; a cached rerun; a child killed mid-run (exit status
  wins). Each asserts the exit code and what the output leads with.
- **`budget.Screens`:** a fixture router with a 200ms route fails; one
  slow request inside a passing median fails on the 3× ceiling; a route
  answering 303 fails; an opaque `ServeMux` mount fails until listed or
  skipped; a stale `perf` record fails; an SSE route is measured to first
  byte and cancelled without hanging; a parameterised route with no
  `Paths` entry fails.
- **`budget.Boot`:** a fixture whose init sleeps 600ms fails; the child
  is a fresh process (a package-level counter reads zero in it).
- **Scaffold, end to end:** a new test runs the scaffold's whole `make
  ci` on a fresh app with only the Go toolchain on `PATH`,
  `GOFLAGS=-mod=readonly`, `GOPROXY=off` and a populated module cache:
  every step passes, including `budget`, `test` and `perf`. It replaces
  nothing; `TestScaffoldMigratesAndPassesCheck` and
  `TestScaffoldedAppTestsPass` keep their narrower jobs.

## Platforms

Linux and macOS, the platforms the scaffold's `make` gate already
assumes. Temporary files follow `os.MkdirTemp`, so `$TMPDIR` is honoured.
Windows is not supported by the make gate today and this does not change
that.

## Rollout

One branch, in order: `internal/budgetfile` and `budget size`;
`budget.Main` and `budget test`; `budget.Screens` and `budget.Boot`; the
scaffold; Rastrillo's own gate; the docs. Nothing changes for an existing
app until it opts in. The adoption recipe in `docs/site/testing.md`:
add `budget.Main` to each test package, run `budget size`, write each
over-budget directory into `budgets.txt` with a reason, and let the
ratchet walk them down.

## Out of scope

- Retrofitting existing apps; each adopts in its own branch.
- platform's and seapointish's instructions to put `GOCACHE` under `/tmp`
  or in the worktree, which break this machine's cache rule. Worth raising
  in those repos separately.
- Sharding. A scaffolded app inside its budgets should not need it.
- Changes to `dbtest`: the scaffold adopts it as it is.

## Review record

Astra's review of the first draft, and where each finding went:

| # | Finding | Resolution |
|---|---|---|
| 1 | A pipe cannot carry `go test`'s exit status | `budget test` runs `go test` itself (§3) |
| 2 | A cached rerun erases a budget failure | Enforced in-binary by `budget.Main` (Decision 4, §2) |
| 3 | Build, setup and package-level failures dropped | Both event schemas, package output kept (§3) |
| 4–8 | New `dbtest` design: cleanup, WAL, GORM-free, identity, comparison | Dropped; the existing `dbtest` is adopted unchanged |
| 9, 13 | Perf in the parallel suite; perf eats package time | Separate serial uncached lane (Decision 5, §4) |
| 10 | `chi.Walk` cannot see opaque mounts | Opaque mounts must be listed or skipped (§4) |
| 11 | Fast error pages pass; streaming hangs | Status required; deadlines; SSE handled (§4) |
| 12 | Cold boot undefined | Re-exec child; exec to first byte (§4) |
| 14 | Exemptions file under-specified | Grammar, discovery, refusals, stale ownership |
| 15 | Perf ratchet flakes | Advisory only |
| 16 | Directory is not a package | Named and specified as a directory budget (§1) |
| 17 | Dogfooding fails today | Two recorded exemptions (§6) |
| 18 | Wrong module path; staticcheck dropped | Fixed (§5) |
| 19 | Local loop contradicts "make ci before push" | Edit loop vs pre-push (Decision 6) |
| 20 | SKILL.md ceiling obsolete | 30,000 stands; no raise (§7) |
| 21 | No real end-to-end proof | Whole `make ci` scaffold test, offline (§ Proving) |
| 22 | Receipt cannot time the gate; platforms | Labelled test-step only; § Platforms |
