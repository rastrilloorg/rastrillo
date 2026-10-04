# 🤖 Testing

`rastrillo new` ships an app whose tests pass before you have written
anything, and a `make ci` that defines the gate once.

## Running tests while you work

While you edit, test the package you are changing and the harness
package that drives it:

```sh
go test -short ./internal/notes/... ./internal/notestest/
```

Before you push, run `make ci`. It is the same gate CI runs, and the
budgets below exist to keep it quick enough that you never skip it. If
`make ci` is too slow to run before every push, one of those budgets has
already failed, and that is the thing to fix.

## The harness

The scaffold writes `internal/<app>test/` — a harness plus example
tests. It builds your app the way `main.go` does and drives it over
HTTP, so what a test exercises is what a request exercises: the router,
the middleware, the session store, CSRF.

Its `post()` sends an `Origin` header, because
[`csrf.Protect`](/docs/sessions) is mounted from day one and a test that
forgot would 403 confusingly.

## Serving without a listener

```go
handler, cleanup, err := rastrillo.Handler(opts)
```

`Handler` is `Serve` minus the listener: the whole framework chrome —
`/healthz`, `/api/version`, locale-prefix stripping, `Options.Wrap` — as
an `http.Handler` you can hand to `httptest.NewServer`. Call the
returned cleanup when you are done.

`rastrillo.OpenDB` is the same corrected SQLite opener `Serve` uses,
exported so a test can open a database with the right pragma order
instead of an approximation of it.

## A database per test

Each test gets its own database, copied from one that was migrated once
when the test binary started. [`dbtest`](/docs/reference/dbtest) does the
work, and the scaffold's harness already uses it:

```go
var schema = dbtest.FromSet(notes.BootSchema)

d, err := db.Open(schema.Path(t), logger)
```

Migrating is the slow part of opening a test database. SQLite has to parse
and run every statement in your schema, and every test pays that again. In
Tito Go a fresh database took 207ms and a copy took 2.5ms, and a package of
1,600 tests went from six minutes to one.

## make ci

```sh
make ci
```

runs vet, gofmt, staticcheck, govulncheck, gitleaks, `budget`, `test`, `perf` and `rastrillo
migration check`, in that order and one at a time. It is the one gate
definition: `.amadan/ci` and `.amadan/ci.d/` are executable steps that
call the same Makefile targets, so the CI runner and your terminal cannot
disagree about what passing means.

staticcheck runs at a pinned version through `go run`, so there is nothing
to install. Raise that version in the Makefile when you raise the `go`
line in go.mod: a staticcheck older than your Go does not know that
release's deprecations, and says nothing about them.

govulncheck reports only the vulnerabilities your code can reach, and it reads a live database, so the gate can go red with no change of yours. Fix a module finding by raising that requirement in go.mod, and a standard-library one by raising `GOTOOLCHAIN` in the Makefile: it names the Go release both the gate and `make release` use.

gitleaks scans every commit for keys and tokens, because a secret you deleted is still in every clone. Until the app is the root of its own git repository there is nothing committed, and it says so instead of failing.

Keeping [`migration check`](/docs/migrations) in the gate is what stops
models and migrations drifting apart between deploys. It touches no
database, so it runs anywhere.

## The budgets

Every app on this machine that grew a slow test suite got there the same
way: one package that never stopped growing, a fresh database migrated in
every test, nothing running in parallel. Tito Go's main package reached
957,000 lines, and a one-line change took 72 seconds just to link its test
binary. The budgets stop that early, while fixing it is still cheap.

| Budget | Limit | Checked by |
|---|---|---|
| Directory size | 5,000 lines of code, 8,000 lines of tests | `make budget` |
| Test package time | 10 seconds | `make test` |
| Screen | 150ms to first byte, for every GET route | `make perf` |
| Cold start | 500ms from starting the process to the first byte | `make perf` |

The size budget always fails the gate. The timing budgets fail it only on
CI, where `AMADAN_CI` or `CI` is set. On your laptop they print the same
numbers and pass, because a busy laptop says nothing reliable about speed.

`make test` also prints a receipt: how long the test step took against a
60-second target, which packages ran and which came from Go's cache, and
the slowest packages and tests. The 60 seconds is a target to watch, not a
limit: a whole-suite time limit on a shared runner fails at random.

## Exceptions

When something genuinely cannot meet a budget yet, record it in
`.rastrillo/budgets.txt`, beside your `go.mod`, with the reason:

```
size  internal/notes          7200 -     billing split owed, see #41
time  internal/notes/import   25s        replays a 40MB fixture
perf  GET /notes/export       1200ms     streams every row
perf  GET /jobs/{id}/events   skip       server-sent events never finish
boot                          800ms      loads twelve locales at start
```

A size record states the source and test ceilings, with `-` for a column
that keeps its default. Every record needs a reason. The file is read
strictly: a line it cannot parse, a directory spelled two ways, or a
second record for the same thing is an error, with its line number.

Records cannot go stale quietly. One naming a directory, package or screen
that no longer exists fails the gate. A size record also has to shrink as
its directory does: once the code is under 60% of the recorded ceiling,
the gate asks you to lower the number, and once it is back under the
default, to remove the record.

## Test time and Go's cache

Every test package needs this `TestMain`:

```go
func TestMain(m *testing.M) { os.Exit(budget.Main(m)) }
```

If the package already has one, call `budget.Main(m)` where it called
`m.Run()`, and do your teardown after it. The scaffold's harness does this
and then removes its `dbtest` template.

[`budget.Main`](/docs/reference/budget) prints how long the package's
tests took, and `make test` (`rastrillo budget test`) compares that with
the budget. A package that ran tests without printing it fails, so a new
package cannot slip past the budget by forgetting the line.

The measuring and the judging are split on purpose, so that Go's test
cache can stay on. Go saves a passing package's output and replays it
when nothing has changed, the time line included. That means a package
served from the cache is judged on how long it took when it really ran,
and retrying a red build cannot turn a slow package green.

Keep the cache honest. Go knows which files and environment variables a
test reads, but not what it runs or fetches. A test that runs `go build`,
reads files outside its own module, or uses the network gets a Makefile
target of its own with `-count=1`, which turns the cache off for it.

## The perf lane

`make perf` runs the tests in `internal/notestest/perf_test.go`, which the
scaffold writes for you. It walks your router and requests every GET route
over real HTTP, through the same handler production serves, and holds each
one to 150ms to first byte. It also starts the app in a fresh process
three times and holds the cold start to 500ms. These are the same numbers
the [`perf`](/docs/reference/perf) middleware logs in production.

A route you add fails here until it is measured. Most routes need nothing
from you. A route with a parameter needs a real path to request, whose row
the test creates first; a route behind sign-in needs the test to sign in.
[`perftest`](/docs/reference/perftest) has the details, including routes
served by a handler the router cannot see into.

The lane runs one test at a time and never uses the cache: a measurement
taken beside other work, or replayed from yesterday, is not a measurement.
That is also why perf tests never call `t.Parallel()`. The lane fails if
the perf tests did not both run, so deleting them does not make it pass.

## Splitting by feature

When `internal/notes` reaches 5,000 lines, split it by feature. The core
package keeps the models, the rendering and the scoping helper; each
feature becomes a package of its own with its handlers, templates and
tests, and mounts its routes:

```go
// internal/notes/sharing/sharing.go
func Mount(r chi.Router, d Deps) {
	r.Get("/notes/{id}/share", d.share)
	r.Post("/notes/{id}/share", d.createShare)
}
```

`Router` in `app.go` calls each feature's `Mount`. An edit to one feature
then rebuilds and retests only that feature, Go's cache skips the rest,
and the packages compile and test in parallel.

Don't split by layer instead (`store/`, `web/`). Every handler change still
rebuilds and retests the whole web layer, so it only delays the problem.

Generated code counts toward the budget, since it costs compile time like
anything else. If a manifest's generated screens push a directory over,
split the manifest, or record an exception that says so.

## Keeping tests fast

Start each test with `t.Parallel()`. Anything before that line runs
before the parallel phase, one test at a time, and a fixture built there
is held for the whole run. Perf tests are the exception.

If tests need a binary, build it once with `sync.Once`, not once per test.
amadan's end-to-end tests went from 27 seconds to under 9 when they did.

Use a browser test only for what only a browser can prove: JavaScript
behaviour against the real templates, or a visual check. Everything else
is an `httptest` request against the real handlers, asserting on the HTML.
Browser tests sit behind `-tags browser`, outside `make ci`.

When CI needs a tool, such as Chrome or Node, its absence should fail the
test, not skip it. A suite that quietly skips what it cannot run looks
green while testing less.

A test that fails only on CI is usually racing work it started itself.
`GOMAXPROCS=1 go test -count=10 -run TestX ./internal/notes/` makes the
same interleaving happen every time. Fix it by removing the race, not by
waiting longer.

When you add a check like these, break the thing it guards on purpose and
watch it fail before you trust it.

## Testing a two-user rule

The rule most worth a test is the one in [scoping](/docs/scoping):
another user's row must answer 404. Sign in as A, create a row, sign in
as B, request it by id, assert 404. `examples/notes` proves both its
declared and hand-written halves with one two-user suite.

Write that test once per owned resource. It is the cheapest insurance
against the most expensive bug.

## Passkey ceremonies

`webauthn/authtest` is a fake authenticator, public so your tests can
drive a full registration and assertion without hardware. See
[Passkeys](/docs/passkeys).

## The pins test

```sh
go test -tags pins ./internal/iconsets/
```

Build-tagged and deliberately outside the ordinary suite, because it
reaches jsdelivr and the npm registry. A check that fails when someone
else's CDN has a bad afternoon teaches people to ignore failures. Run it
at release — [Icons](/docs/icons) explains what it verifies.

## What the CI runner needs

The budgets assume a runner that keeps Go's build and test cache between
runs; without it every run pays for the whole suite. Set `TMPDIR` to a
directory on disk, because the tests put their databases there. And set
`AMADAN_CI` or `CI`, which the amadan runner does for you, or the timing
budgets only report.

The timing numbers suit the runners amadan uses. On a slower runner,
raise them with records in `budgets.txt` and say so in the reason.

## Adopting the budgets in an existing app

1. Add `func TestMain(m *testing.M) { os.Exit(budget.Main(m)) }` to every
   test package, or call `budget.Main` inside the one you have.
2. Add the `budget`, `test` and `perf` targets from a fresh `rastrillo new`
   app's Makefile, add `budget` and `perf` to `ci:`, and add their
   `.amadan/ci.d/` steps.
3. Run `make budget` and record each directory that is over in
   `.rastrillo/budgets.txt`, with a reason. From then on the records can
   only shrink.
4. Copy the scaffold's `perf_test.go`, split `App` into `Router`, `Mux` and
   `Configure` the way the scaffold does, and run `make perf`.
