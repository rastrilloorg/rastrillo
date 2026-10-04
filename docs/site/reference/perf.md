# 🤖 perf

`amadan.net/rastrillo/rastrillo/perf`

Time every request against a budget, and show where the time went. Mount
`Middleware` in `Options.Wrap`, next to your routes.

Every response gets a `Server-Timing` header. `app` is the time to the
first byte, and each `Span` you mark adds its own entry. Open a slow
request in the browser's network panel and the numbers are there.

```go
started := time.Now() // first thing in main
var rec perf.Recorder

opts.Wrap = func(mux http.Handler) http.Handler {
	return csrf.Protect(origin)(perf.Middleware(&rec, perf.Options{Started: started})(mux))
}
```

## Budgets

```go
func Middleware(c *Recorder, opts Options) func(http.Handler) http.Handler

type Options struct {
	Screen     func(r *http.Request) string
	Skip       func(r *http.Request) bool
	Budget     time.Duration
	ColdBudget time.Duration
	Started    time.Time
	Logger     *slog.Logger
}

const (
	DefaultBudget     = 150 * time.Millisecond
	DefaultColdBudget = 500 * time.Millisecond
)
```

A GET or HEAD is over budget when its first byte takes longer than
`Budget`, 150ms by default. A download or stream that started quickly is
not over budget, however long it runs. Other methods have no budget:
sending an email or taking a payment takes the time it takes.

The first request after the process starts is held to `ColdBudget`,
500ms by default, and its header adds `startup`: the time from `Started`
until the app finished starting. An app that sleeps between visits pays
its whole start-up on someone's click, and this keeps that cost visible.

An over-budget request logs a warning, at most once a minute per route
and 120 times a minute in all, so a slow database does not flood the
log.

`perf` reports budgets in production. [`perftest`](/docs/reference/perftest)
enforces the same budgets in CI, and fails a build that makes a screen or a
cold start slower than them.

## Grouping

Requests are grouped by the route pattern they matched, such as
`GET /orders/{id}`, never by their path. A path can carry a token, and
the group name ends up in log lines. A request that matched no route is
`unmatched`.

Put `Middleware` last in `Wrap`, next to the mux. Middleware between it
and the mux that copies the request hides the matched pattern, and every
request shows as `unmatched`.

`Skip` leaves requests unmeasured; by default `/healthz`, `/api/version`
and anything under `/static/`. `Screen` replaces the route pattern as
the group name. Whatever it returns must not come from the path.

## Span and Label

```go
func Span(r *http.Request, name string) func()
func Label(r *http.Request, label string)
```

`Span` times one part of the work. Call the function it returns when
that part ends, as in `defer perf.Span(r, "db")()`. Spans with the same
name add up. Use a few fixed names, never a value.

`Label` adds your own grouping to a request, such as the account it
belongs to. Like the route, it appears in log lines.

## Recorder

```go
type Recorder struct { /* unexported; the zero value is ready */ }

func (c *Recorder) Snapshot() []Sample
func (c *Recorder) Groups() []Group
func Groups(samples []Sample) []Group

const Capacity = 2048

type Sample struct {
	At      time.Time
	Screen  string
	Method  string
	Label   string
	Status  int
	TTFB    float64
	Total   float64
	Spans   map[string]float64
	Startup float64
	Budget  float64
	Cold    bool
	Over    bool
}

type Group struct {
	Screen   string
	Method   string
	Label    string
	Requests int
	Over     int
	P50      float64
	P95      float64
	Total95  float64
}
```

A `Recorder` keeps the last 2,048 requests. `Snapshot` returns them,
oldest first. `Groups` gives each route's request count, over-budget
count, and p50 and p95 times, slowest first, for a dashboard of your
own.
