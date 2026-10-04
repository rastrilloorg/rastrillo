# Test, CI, directory-size and screen-time budgets

**Date:** 2026-10-03, revised the same day after two rounds of
adversarial review (Astra; findings resolved in place, listed in § Review
record)
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
- **Screen time is measured in production but never gated.** Rastrillo's
  `perf` middleware already holds GET and HEAD to 150ms to first byte and
  the first request after start to 500ms (`perf.DefaultBudget`,
  `perf.DefaultColdBudget`), logging what is over. The scaffold does not
  mount it, and nothing fails a branch that makes a screen slow.
  Seapointish and Tito Go gate the same numbers in CI (titogo
  `docs/superpowers/specs/2026-08-31-performance-budget-design.md`).

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
4. **Go's test cache stays on, and the measurement travels with the
   cached result.** Each test binary prints its own `m.Run` time; Go
   caches a passing package's output and replays it byte for byte on a
   hit, so the wrapper judges a cached package on the time it took when
   it actually ran, and a retry of the same commit cannot turn an
   over-budget package green (review finding 2). The verdict stays in the
   wrapper, outside the binary: Go records a test's file and environment
   reads only between `m.Run`'s `before` and `after` (Go
   `testing.go:2434`), so a binary that read `budgets.txt` or an
   environment variable from `TestMain` would leave both out of its cache
   key. Rejected: `-count=1` everywhere (amadan's choice, ~95s against ~9s
   cached), because it pays for the whole suite on every push.
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
| Package test time | 10s per test binary's `m.Run` | Yes, in CI | measured by `budget.Main`, judged by `rastrillo budget test` |
| Warm screen | 150ms time-to-first-byte, median of 8 | Yes | `perftest.Screens`, perf lane |
| Cold boot | 500ms, process start to first byte | Yes | `perftest.Boot`, perf lane |
| Test step | 60s on the CI runner | **No**, reported | `rastrillo budget test` receipt |

The test-step figure is reported, never gated: a wall-clock gate on a
shared runner is a flake generator, and Tito Go's gate already goes red a
fifth of the time on flakes alone. It covers only the `go test` step; the
whole gate's time is the runner's own figure.

**Timing budgets mean the CI runner.** `rastrillo budget test` fails on
package time only when `AMADAN_CI` (which the amadan runner sets) or `CI`
is non-empty; elsewhere, `make ci` included, it reports against the same
numbers and never fails on them. A loaded laptop is not evidence. The perf
lane is CI-gated the same way.

## The exemptions file

`.rastrillo/budgets.txt`, one per module, at the directory holding
`go.mod`. A nested module (rutline's `home/`, Rastrillo's `examples/*`)
has its own. Finding it: from the working directory, walk up to the first
`go.mod`; the file sits beside it. A missing file means no exemptions.

Grammar, one record per line; `#` starts a comment; blank lines ignored;
fields are whitespace-separated and the reason is the rest of the line:

```
size  <dir>               <src-lines>|-  <test-lines>|-  <reason>
time  <dir>               <duration>                     <reason>
perf  <screen>            <duration>|skip                <reason>
boot                      <duration>                     <reason>
```

- `<dir>` is the canonical form `path.Clean` gives a slash-separated path
  relative to the module root: `internal/notes`, or `.` for the root. A
  leading `./` or `/`, a trailing `/`, a `..` element, or anything
  `path.Clean` would rewrite is refused rather than normalised, so one
  directory has exactly one spelling.
- `<duration>` is a Go duration (`25s`, `1200ms`).
- `<screen>` is two fields, a method and a path pattern, the name the
  `perf` package gives a route in production: `GET /notes/{id}`. The
  pattern is a chi pattern exactly as `chi.Walk` reports it, or the `Key`
  of a screen declared inside an opaque group (§4).
- In a `size` record, `-` means "the default" for that column. A stated
  column must exceed its default, and only stated columns are ratcheted,
  so a test-heavy directory with no source to speak of is expressible.
- **Refused, with the line number:** an unknown kind, a missing or empty
  reason, an unparseable number or duration, a zero or negative value, a
  non-canonical `<dir>`, two records for the same key, a `size` record
  with both columns `-`, a stated `size` column not above its default.
- **Stale records fail, each where the complete inventory lives.** `budget
  size` fails a `size` record whose directory no longer exists or holds no
  Go files. `rastrillo budget test` fails a `time` record naming no
  package it saw, on a `./...` run (§3). `perftest.Screens` fails a `perf`
  record naming a route it did not find (one call per module; see §4).
- **The ratchet, on size only, per stated column:** a directory measuring
  under 60% of a stated ceiling fails with "measured 3,100, exemption
  says 7,200: lower it". A column measuring at or under its default fails
  with "no longer needs an exemption: write `-`". Timing has no ratchet; runner noise would make it
  flake. `perftest.Screens` prints an advisory line instead when a route
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

### 2. `budget.Main`, the in-binary measurement

New public package `amadan.net/rastrillo/rastrillo/budget`, GORM-free
(joins `GORM_FREE` in the Makefile).

```go
func TestMain(m *testing.M) { os.Exit(budget.Main(m)) }
```

- Times `m.Run()`, returns its code unchanged, and after it prints one
  line, `rastrillo-budget/v1 ran 2.413s` (the version lets the parser
  refuse a format it does not know). It judges nothing, reads no file
  and no environment variable, so it adds nothing to the cache key and
  cannot make a package fail.
- Because the line is part of the binary's output, a cached replay
  carries the time from the run that was cached. That is what lets the
  wrapper judge cached packages (Decision 4).
- An app that already has a `TestMain` calls `budget.Main(m)` in place of
  `m.Run()` and does its own teardown after it (`dbtest`'s `Remove`).
- It reads `perftest.Boot`'s child variable only to stay silent in a child
  process (§4); the child's line would otherwise be judged as a second
  package run.

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
- **Reassembles output before reading it.** test2json may split a long
  line across `output` events, so the package-level output (events with
  no `Test`) is concatenated in order and split on newlines; the
  measurement is an exact whole-line match of
  `^rastrillo-budget/v1 ran (\S+)$`, parsed with `time.ParseDuration`.
  Two such lines in one package, or an unknown version, fail the step.
- **Classifies cached packages** from the package summary output, which
  ends `(cached)` on a hit, never from `Elapsed` (on a hit it measures
  the replay).
- **Judges package time.** For each package, the measurement line gives
  its time, cached or not; the package's `time` record in
  `budgets.txt` (default 10s, keyed by directory: the import path minus
  the module path from `go.mod`) gives its limit. Over the limit fails
  the step when `AMADAN_CI` or `CI` is set and is reported otherwise.
  `-no-time` turns judging off, for the perf lane.
- **Requires the measurement:** a package that passed with test-level
  events but no measurement line fails the step, naming the package and
  the one line to add. A package that failed is already a failure and is
  reported as one; a package ending in `skip` (no test files) or with no
  test-level events (`-run` matched nothing) is counted and exempt.
- **`-require Name,…`** names tests that must each end in `pass` at least
  once in the run; a missing, skipped or failed one fails the step. The
  perf lane uses it, so deleting or renaming the perf tests cannot leave
  a lane that runs nothing and reports green.
- **Owns `time` staleness.** It fails a `time` record whose directory is
  not among the packages that reported test-level events, but only when
  the run's package argument was exactly `./...`, the one case where that
  list is complete. A `time` record for a directory with no tests is
  therefore stale too.
- **Readable output**, so the log of a red run leads with what failed:
  every failed test's output in full; the full output of a package that
  failed without a failing named test (setup failure, panic, timeout);
  every build failure's `build-output`; every budget refusal. Passing
  output is dropped.
- **The receipt**, printed every run: test-step wall time against the 60s
  target; packages run, cached, skipped and failed; the five slowest
  packages (from their `budget.Main` lines) and the five slowest tests.
  Labelled as the test step, not the gate.
- A malformed JSON line is passed through as output and counted; more than
  none fails the step after `go test` exits, because it means something
  else wrote to stdout.

### 4. `perftest.Screens` and `perftest.Boot`, screen time

A new package, `amadan.net/rastrillo/rastrillo/perf/perftest`: the CI
half of the existing `perf` package, as `httptest` is to `net/http`. It
uses `perf.DefaultBudget` and `perf.DefaultColdBudget` rather than its
own numbers, and names screens the way `perf` groups them in production
(`GET /notes/{id}`), so a slow screen has one name in CI and in the logs.
GORM-free. The scaffold puts the app's perf tests in a file under
`//go:build perf`, so the ordinary suite never compiles them.

```go
perftest.Screens(t, perftest.ScreenConfig{
	Routes:  r,       // chi.Routes: the app's router, for the inventory
	Handler: h,       // rastrillo.Handler(opts): what production serves
	Paths:   map[string]string{"/notes/{id}": "/notes/1"},
	Expect:  map[string]int{"/export": http.StatusOK},
	Signin:  func(c *http.Client, base string) { ... }, // signs c in once
	Opaque: []perftest.Opaque{{
		Patterns: []string{"/bookmarks", "/bookmarks/*"},
		Screens: []perftest.OpaqueScreen{
			{Key: "GET /bookmarks", Path: "/bookmarks"},
			{Key: "GET /bookmarks/{id}", Path: "/bookmarks/1"},
		},
	}},
})

perftest.Boot(t, perftest.BootConfig{
	DBPath: schema.Path(t), // prepared by the parent, copied per child
	Build:  func(dbPath string) (http.Handler, func(), error) { ... },
	Path:   "/",
})
```

**Inventory.** `chi.Walk` over `Routes`, GET only.

- A route whose handler is not a `chi.Routes` and whose pattern ends in
  `/*`, or that appears in an `Opaque` group's `Patterns`, is an opaque
  mount: something chi cannot see inside, such as the generated
  `http.ServeMux` `examples/notes` registers at `/bookmarks` and
  `/bookmarks/*`. Every opaque pattern must belong to exactly one
  `Opaque` group or carry a `perf GET <pattern> skip` record. The group's
  `Screens` are the inventory inside it; each has a stable `Key` in
  screen syntax (`GET /bookmarks/{id}`), which is what its `perf` record
  names, a concrete `Path`, and an optional `Expect`.
- A walked route's screen name is `GET ` plus its chi pattern, the name
  `perf` gives it in production.
- A group pattern that `chi.Walk` did not report is an error: the group
  has gone stale.
- Framework routes (`/healthz`, `/api/version`) and the app's `/static/`
  live outside the chi router, so they are not in the inventory.
- `Screens` is the module's complete inventory, so stale `perf` records
  fail here: any record naming neither a walked pattern nor a declared
  `Key`. A second `Screens` call in one test binary is an error.
- A pattern with a `{param}` and no `Paths` entry fails rather than being
  skipped.

**Measurement is over real HTTP.** `Handler` is served by
`httptest.NewServer`, and the client is an ordinary `http.Client`. TTFB is
the moment `httptrace.ClientTrace.GotFirstResponseByte` fires, measured
from just before the request is written: what a client actually observes,
so a handler that calls `WriteHeader` early and then works for a second is
charged the second. A `1xx` informational response also fires that hook;
the docs say so, and no scaffolded handler sends one.

- **A screen must succeed to be measured.** Each response must carry 200,
  or the status `Expect` names for that pattern or key. A 303 to sign-in
  or a 404 for a missing row fails with "not measuring an error page";
  seeding the rows `Paths` names is the test's job, in the same file.
- **Protocol:** 10 requests, discard 2, hold the median of 8 to 150ms (or
  the record), and fail any single request over 3× that. The body is read
  to EOF and closed, so the connection is reused cleanly, except for a
  `text/event-stream` response, which is measured to first byte and then
  closed.
- **A handler that will not finish stops the sweep.** Each request has a
  client deadline of 3× the budget plus 1s. Cancelling a request does not
  stop a handler that ignores its context, so on a deadline `Screens`
  fails the test, closes client connections, and measures nothing further:
  a goroutine still running would contaminate every later number. The
  server is closed with a 2s bound, and a server that still will not
  close is reported, not waited on.
- Over budget fails only when `AMADAN_CI` or `CI` is set; otherwise every
  result is reported and the test passes. `Screens` reads those variables
  inside the test. A route measuring under 60% of its record gets an
  advisory line, never a failure.
- `Screens` never calls `t.Parallel()`, and the perf lane's `-parallel 1`
  keeps sibling perf tests apart: a perf test sharing the machine with
  its siblings measures them, not itself.

**`Boot` is process-cold.** The parent prepares the database and the
child does nothing but start:

- The parent copies `DBPath` once per child run, so no child migrates or
  builds a `dbtest` template.
- It re-executes its own test binary (`os.Args[0]`) with
  `-test.run=^<this test's name>$`, `-test.count=1`, and
  `RASTRILLO_BOOT_CHILD=<copy path>` in the environment. The same
  `TestMain` runs; `budget.Main` sees the variable and stays silent;
  `m.Run` selects the same test.
- Inside that test, `Boot` sees the variable and takes the child branch:
  it calls `Build(path)`, serves the handler on a loopback listener,
  requests `Path` over HTTP, prints `rastrillo-budget/v1 boot-first-byte`
  the moment the first response byte arrives, calls the returned
  cleanup, and returns. The child branch never spawns, which is the
  recursion guard.
- The parent's clock runs from `exec` to the moment it reads that line on
  the child's stdout, so process start, runtime and package init,
  `db.Open`, the app's migrate (a no-op on the copy) and template parsing
  are all inside it. The child must print the line and then exit 0 within
  10s, or it is killed and the test fails with its output.
- Three children, run one after another; the median is held to 500ms (or
  the `boot` record). Over budget fails only under `AMADAN_CI` or `CI`,
  as for `Screens`.

### 4a. The lanes

| Lane | Make target | Flags | Time judged | Cached | In `ci` |
|---|---|---|---|---|---|
| Suite | `test` | `./...` | package time | yes | yes |
| Perf | `perf` | `-tags perf -count=1 -p 1 -parallel 1 -run '^TestPerf'` | screens and boot | no | yes |
| Browser | none | `-tags browser`, as today | no | as today | no, unchanged |
| Edit loop | none | `-short`, chosen packages | reported only | yes | no |

- The perf lane runs through `rastrillo budget test -no-time -require
  TestPerfScreens,TestPerfBoot`, so its sweep is never charged to package
  time, and it cannot pass having run neither test.
- `-p 1` keeps other packages' binaries off the machine; `-parallel 1`
  keeps the perf package's own tests apart.
- Under `-short` the perf tests skip; the perf lane never passes `-short`,
  and `-require` would fail it if it did.
- The scaffold's Makefile declares `.NOTPARALLEL:`, so `make -j ci` cannot
  run the perf lane beside the suite.
- Perf numbers mean the CI runner. Locally they are reported, so `make
  ci` on a laptop prints them and never fails on them.

### 5. Scaffold changes (`rastrillo new`)

- **`App` keeps its signature** (`SKILL.md` §1, `cmd/<app>/main.go`) and
  is split inside `app.go` into `Router(d, origin, logger) (chi.Router,
  error)`, which migrates and builds the routes exactly as `App` does
  today, and `Mux(r http.Handler) *http.ServeMux`, which mounts static
  files and the router. `App` is `Router` then `Mux`.
- **One place sets the serving options.** A new `Configure(opts
  *rastrillo.Options, mux *http.ServeMux, started time.Time)` in `app.go`
  sets `Mux`, `ErrorPage`, and `Wrap` to `perf.Middleware` with `Started:
  started`, so a new app gets production screen timing (a `Server-Timing`
  header and over-budget warnings) from its first deploy. `main.go`
  records `started := time.Now()` first thing and calls `Configure`
  between `Resolve` and `Serve`; the perf test calls it on a fresh
  `rastrillo.Options` and passes the result to `rastrillo.Handler`.
  Measured requests therefore go through the production chrome
  (`Options.Wrap`, locale handling, security headers, panic recovery),
  and the chi router from `Router` is used only for the inventory.
- **Harness:** `var schema = dbtest.FromSet(BootSchema)`; `newApp` opens
  `db.Open(schema.Path(t), logger)` (the copy is made before either pool
  opens); a `TestMain` saves `budget.Main(m)`'s code, calls
  `schema.Remove()`, then `os.Exit`s with the code. Every example test
  starts with `t.Parallel()`; the perf tests do not.
- **`internal/<app>test/perf_test.go`** (`//go:build perf`):
  `TestPerfScreens` and `TestPerfBoot`, passing out of the box against
  the placeholder index.
- **`.rastrillo/budgets.txt`**, written empty except for a header giving
  the grammar and saying every line needs a reason.
- **Makefile** (`RASTRILLO := go run
  amadan.net/rastrillo/rastrillo/cmd/rastrillo`, the module path the
  scaffold's `tool` directive already declares):
  - `.NOTPARALLEL:`
  - `budget: $(RASTRILLO) budget size`
  - `test: $(RASTRILLO) budget test ./...`
  - `perf: $(RASTRILLO) budget test -no-time -require TestPerfScreens,TestPerfBoot -tags perf -count=1 -p 1 -parallel 1 -run '^TestPerf' ./...`
  - `staticcheck` gains the tag: `-tags browser,perf`, so the perf files
    are analysed.
  - `ci: vet fmt-check staticcheck budget test perf migration-check`
- **`.amadan/ci.d/`** gains `15-budget` and `35-perf`, one per target as
  `AGENTS.md` requires; staticcheck's step stays. `budget` runs before
  `test` because it is cheap and structural.
- **Existing scaffold assertions move with it:** the exact `ci:` string,
  the step list and the executable checks in
  `TestNewScaffoldsCIAndManifest`.
- **Scaffolded `AGENTS.md`** gains the loop: while editing, `go test
  -short ./internal/<app>/... ./internal/<app>test/`; before pushing,
  `make ci`; if `make ci` is too slow to run before every push, a budget
  has already failed, so fix that rather than skipping the gate.

### 6. Rastrillo's own gate

- A new `budget` target runs `go run ./cmd/rastrillo budget size` over
  its own module, joins `ci`, and gets its own step,
  `.amadan/ci.d/12-budget`, per the target/step parity rule. Today two
  directories are over: `internal/designsystem` (9,720 source; its 7,789
  test lines are under) and `ui` (19,251 test; its 2,885 source lines
  are under). Their records state only the column that is over:
  `size internal/designsystem 10500 - <reason>` and `size ui - 20500
  <reason>`, numbers set in the implementation from the counts on the
  day, with a reason each. The ratchet then holds them.
- The `example-%` targets add `budget size` per example module. Their test
  targets are unchanged: the examples are reference apps, not budgeted
  ones, until one adopts the scaffold's harness.

### 7. Docs

- **SKILL.md** (27,514 of 30,000 bytes) gains about 1,000 bytes: a short
  testing section (the budgets, `budget.Main`, the perf lane, `dbtest`,
  `t.Parallel()` first, the cache rule, edit loop against pre-push) and
  one line in §1: past 5,000 lines, split `internal/<app>` by feature.
  The ceiling does not move.
- **Reference pages** `docs/site/reference/budget.md` and
  `docs/site/reference/perftest.md` document every exported symbol, and
  both packages join `referencePages` in `internal/docsite`
  (`TestExportedSymbolsAreDocumented` holds them to it). `perf.md` gains
  a line pointing at `perftest` for the CI half.
- **`CHANGELOG.md`** gets an Unreleased entry: what a new app's gate now
  runs, and the adoption recipe for an existing one.
- **`docs/site/testing.md`** gets the full treatment:
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
  stated column under 60% fails; a stated column back at its default
  fails asking for `-`; a test-only directory with `- 9500` passes; a
  nested module is not counted into its parent and is budgeted from its
  own root.
- **`budget.Main` and time judging,** through a fixture module's real `go
  test` with a sleeping test: under `AMADAN_CI=1` the step fails, and a
  second identical run, now served from the cache, fails again with the
  same measured time; without it the step passes and reports; a `time`
  record raises the limit, and lowering that record fails the next run
  with the package still cached; `budget.Main` returns `m.Run`'s code
  unchanged.
- **`rastrillo budget test`:** against real `go test` output from fixture
  modules, not only canned streams: a passing suite; a failing test; a
  build failure; a vet failure during `go test`; a `TestMain` setup
  failure; a panic; a `-timeout`; a package without the measurement line;
  a package that prints the line twice; stdout written before `m.Run`; a
  package with no test files; `-run` matching nothing; a cached rerun
  classified as cached; `-require` naming a test that is missing, skipped
  or failed; a stale `time` record on a `./...` run and the same record
  ignored on a narrower run; a child killed mid-run (exit status wins).
  Each asserts the exit code and what the output leads with.
- **`perftest.Screens`:** a fixture router with a 200ms route fails; one
  slow request inside a passing median fails on the 3× ceiling; a handler
  that writes its header at once and its body 300ms later fails (TTFB is
  what the client sees); a route answering 303 fails; an `Expect` entry
  accepts its status; an opaque `ServeMux` mount fails until grouped or
  skipped; a group pattern chi did not report fails; a `perf` record
  naming a group `Key` is honoured, and one naming nothing fails; an SSE
  route is measured to first byte and closed without hanging; a handler
  that ignores its context fails the test and stops the sweep within the
  deadline; a parameterised route with no `Paths` entry fails; without
  `CI` an over-budget route reports and passes.
- **`perftest.Boot`:** a fixture whose init sleeps 600ms fails under `CI`;
  the child is a fresh process (a package-level counter reads zero in
  it); a child that never prints the line is killed at 10s and its output
  reported; a child that prints the line and exits non-zero fails; the
  child never spawns a grandchild.
- **Scaffold files:** the Makefile declares `.NOTPARALLEL:`; staticcheck
  runs with `-tags browser,perf`; the `ci:` string and step list match;
  `main.go` and the perf test both call `Configure`.
- **Scaffold, end to end:** a new test runs the scaffold's whole `make
  ci` on a fresh app with only the Go toolchain on `PATH`,
  `GOFLAGS=-mod=readonly`, `GOPROXY=off` and a populated module cache:
  every step passes, including `budget`, `test` and `perf`. It replaces
  nothing; `TestScaffoldMigratesAndPassesCheck` and
  `TestScaffoldedAppTestsPass` keep their narrower jobs.

## Platforms

Linux and macOS, the platforms the scaffold's `make` gate already
assumes; GNU make or BSD make, both of which honour `.NOTPARALLEL:`.
Windows is not supported by the make gate today and this does not change
that.

**Runner prerequisites**, stated in `docs/site/testing.md`: a persistent
`GOCACHE` (the test cache is the speed this design is built on); `TMPDIR`
set to a disk-backed directory, since `os.MkdirTemp` falls back to `/tmp`
when it is unset and `/tmp` may be RAM; `AMADAN_CI` or `CI` set, or the
timing budgets only report. The timing numbers were chosen for the amadan
runner class Tito Go's measurements came from; an app on a slower runner
raises them with records, and says so in the reason.

## Rollout

One branch, in order: `internal/budgetfile` and `budget size`;
`budget.Main` and `budget test`; `perftest.Screens` and `perftest.Boot`; the
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

Astra reviewed this design twice. Round two re-verdicted round one (14
resolved, 8 partial) and raised 11 new findings; the partials and the new
findings are resolved as follows.

| # | Finding | Resolution |
|---|---|---|
| 2, 23 | Reads around `m.Run` are outside Go's cache window | `budget.Main` reads nothing; the wrapper judges (Decision 4) |
| 2 | Cache classification unspecified | From the `(cached)` package summary (§3) |
| 9, 25 | `-p 1` does not serialise tests; `make -j` | `-parallel 1`, no `t.Parallel` in perf, `.NOTPARALLEL:` (§4a) |
| 10, 29 | Opaque routes lack identity | `Opaque` groups with patterns and keyed screens (§4) |
| 11 | No `Expect`; context-ignoring handlers hang | `Expect` map; deadline fails and stops the sweep (§4) |
| 12, 24 | Boot child protocol; template rebuilt in child | Parent-prepared copy, child branch, recursion guard, 10s kill (§4) |
| 14 | Canonical keys; `time` for test-less dirs | `path.Clean` form only; such records are stale (§3, file grammar) |
| 21, 30 | Lane matrix; perf lane can run nothing | § 4a; `-require` (§3) |
| 22 | Runner prerequisites | § Platforms |
| 26 | Local reporting contradicts the recipes | One control, `AMADAN_CI`/`CI`, read by the wrapper and inside tests |
| 27 | Two-column ratchet impossible for one-sided dirs | `-` columns; ratchet per stated column |
| 28 | `Mux(Router())` is not production | `Configure` + `rastrillo.Handler` (§5) |
| 31 | Scaffold assertions; perf files unanalysed; Rastrillo's step | §5; `-tags browser,perf`; `12-budget` (§6) |
| 32 | First `WriteHeader` is not TTFB | Measured over HTTP with `GotFirstResponseByte` (§4) |
| 33 | Marker framing | Reassembled output, versioned exact-line match (§3) |

Round one, and where each finding went:

| # | Finding | Resolution |
|---|---|---|
| 1 | A pipe cannot carry `go test`'s exit status | `budget test` runs `go test` itself (§3) |
| 2 | A cached rerun erases a budget failure | The binary prints its time, the cache replays it, the wrapper judges every run (Decision 4, §§2–3) |
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
