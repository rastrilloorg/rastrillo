# 🤖 perftest

`amadan.net/rastrillo/rastrillo/perf/perftest`

Fails the build when a screen or a cold start is slower than
[`perf`](/docs/reference/perf)'s budgets: 150ms to first byte for a
screen, 500ms for a cold start. `perf` reports these numbers in
production; `perftest` checks them in CI. The scaffold's
`internal/<app>test/perf_test.go` calls both functions, and `make perf`
runs it.

## Screens

```go
func Screens(t testing.TB, cfg ScreenConfig)

type ScreenConfig struct {
	Routes  chi.Routes
	Handler http.Handler
	Paths   map[string]string
	Expect  map[string]int
	Signin  func(c *http.Client, base string)
	Opaque  []Opaque
}
```

`Screens` walks `Routes`, your chi router, and requests every GET route
through `Handler`, which should be the handler production serves:
`rastrillo.Handler` over your app's options. Each route gets ten requests.
The first two are discarded, the median of the rest must be within
budget, and no single one may take more than three times the budget.

Time to first byte is measured by the client, so it is what a visitor
waits for. A handler that sends its headers early and then streams is
judged on when it started answering, the way `perf` judges it in
production.

Each route must answer 200, or the status `Expect` gives for its pattern.
A redirect to sign-in or a 404 fails rather than being timed, so set
`Signin` to sign the test's client in, and give each route with a
parameter a real path in `Paths` (`"/notes/{id}": "/notes/1"`), whose row
your test creates first. A route with a parameter and no path fails.

A route given `skip` in `budgets.txt` is not requested. A `perf` record
that names no route fails, since `Screens` sees every route there is. Call
it once per test binary, from a test that is not parallel.

### Handlers the router cannot see into

```go
type Opaque struct {
	Patterns []string
	Screens  []OpaqueScreen
}

type OpaqueScreen struct {
	Key    string
	Path   string
	Expect int
}
```

chi cannot list the routes inside a handler that is not a chi router,
such as a generated `http.ServeMux` mounted at `/bookmarks/*`. Such a
mount fails until you either list its screens in an `Opaque` group, or
record `perf GET /bookmarks/* skip` with a reason:

```go
var cfg = perftest.ScreenConfig{
	Opaque: []perftest.Opaque{{
		Patterns: []string{"/bookmarks", "/bookmarks/*"},
		Screens: []perftest.OpaqueScreen{
			{Key: "GET /bookmarks/{id}", Path: "/bookmarks/1"},
		},
	}},
}
```

`Key` is the screen's name, the one `perf` logs and a `budgets.txt` record
uses.

## Boot

```go
func Boot(t *testing.T, cfg BootConfig)

type BootConfig struct {
	Prepare func(t testing.TB) string
	Build   func(dbPath string) (http.Handler, func(), error)
	Path    string
	Expect  int
}

const BootMarker = "rastrillo-budget/v1 boot-first-byte"
```

`Boot` measures a cold start: it starts the test binary again as a new
process, which builds your app with `Build` and requests `Path`, and
times everything from starting the process to the first byte of the
response. It does that three times and holds the median to 500ms, or to
the `boot` record in `budgets.txt`.

`Prepare` returns a migrated database, and runs once, before any process
starts, so the measurement never includes migrating. The scaffold passes
`schema.Path`. Each new process gets its own copy.

The child prints `BootMarker` when the first byte arrives, and the parent
stops its clock when it reads it. A child that has not finished within 10
seconds is stopped, along with anything it started. Call `Boot` from a
top-level test. Redirects are not followed: `Path` must answer `Expect`
(200 by default) itself.

## When it fails

Both functions fail the test only when `AMADAN_CI` or `CI` is set. Locally
they print every measurement and pass, because a busy laptop says nothing
reliable about speed. `make perf` shows each measurement either way.
