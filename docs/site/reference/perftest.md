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

## FirstLoad

```go
func FirstLoad(t testing.TB, cfg ScreenConfig)
```

`FirstLoad` checks how much framework CSS and JS each page makes a
browser download. It requests the same routes as `Screens`, with the
same `ScreenConfig`. For each page that answers 200 with HTML, it
fetches every framework file the page links (`tokens.css`, `theme.css`,
`shell.css` and the scripts in `static/`) and adds up their sizes,
gzipped. Your own
stylesheets and scripts are not counted, and neither is the HTML.

A page that links its slice of `tokens.css` may load 16,000 bytes.
Each component script it loads (`select.js`, `calendar.js`,
`datetime.js`) adds its own size to that. A live or
polling page links the slice for every page and loads every script, so
it has its shell's own limit, never under 16,000, with `live.js` and
the component scripts on top. A page that links the whole `tokens.css`
may load 40,000 bytes, and its report names the two edits that would
link its slice instead, or says why the file can't be sliced.

`Screens` runs the same check after it times the screens, so the
scaffold's perf test already does this. Call `FirstLoad` yourself to
check pages outside that test. Unlike `Screens`, it also runs under
`-short` and can be called more than once.

When a page is over, the failure names its largest files:

```
GET /orders: 25690 bytes of framework CSS and JS, gzipped, over its 23300-byte budget (16000 bytes for a column page, plus select.js); its largest files are tokens.css's slice (10840 bytes), select.js (7210 bytes), rastrillo.js (2350 bytes); if it cannot be lighter, record `weight GET /orders 28300 <reason>` in .rastrillo/budgets.txt: its weight plus 10%, rounded up to the next 100
```

For a page that links the whole `tokens.css`, it names the files that
grew instead. A file grows when you edit your copy in `static/`. Run
`rastrillo doctor` to see what changed, and `rastrillo doctor --fix` to
put the shipped file back. If no file grew, the failure lists every
framework file the page loads, so you can see which it should not.

A page that can't be lighter gets a weight record in `budgets.txt`, its
whole limit in gzipped bytes, with a reason:

```
weight GET /reports  18000  draws every chart on one page
```

A page over its record is told to raise it the same way. Like a size
record, it fails once the page is back within its usual limit, and
asks to be lowered once the page weighs under 60% of it.

`FirstLoad` also checks each sliced page's hooks. When the slice lacks
a rule for a hook that the whole `tokens.css` would apply to the page,
the hook can render unstyled. That happens when the hook is in HTML
built in Go or by a script, which the page's templates don't show.
The check reads the classes in an option's `data-rst-class` too, since
`select.js` puts them on the row it draws for that option:

```
GET /orders: shows rst-card, but its slice of tokens.css lacks a rule for it that the whole file has, so it can render unstyled. A page's slice holds the hooks its templates name, not markup built in Go or by a script: ask for this hook in the page's template with {{needs "rst-card"}}
```

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

`Screens` and `Boot` fail their timing checks only when `AMADAN_CI` or
`CI` is set. Locally, they print every measurement and pass the timing
checks, because a busy laptop says nothing reliable about speed.
`Screens` also checks first-load weight, which fails on every machine
when a page is over budget, because a file's size is the same
everywhere. `make perf` shows each measurement either way.
