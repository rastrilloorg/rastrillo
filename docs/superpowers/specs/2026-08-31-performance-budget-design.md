# The performance budget — 150ms warm, 500ms cold, as a framework goal

**Date:** 2026-08-31 · **Status:** DRAFT for review — brainstormed, not
yet planned or implemented. **Origin:** Tito Go and Seapointish both run
to these numbers by hand; the ask is to make them a rastrillo fact so an
app author does not ship a slow page without knowing.

## 0. What this is

A server response time budget the framework measures, reports, and gates
on, plus the rule for what to do when a page cannot meet it: move the
work into `jobs` and return a loader.

Nothing here is new machinery for the slow path. `jobs.Start`, the SSE
stream in `jobs/events.go`, the poll fallback, and
`ui/partials/job-status.html` already exist and are already documented.
The budget is a number pointed at machinery that shipped.

Three decisions were taken during brainstorming and are recorded in §7
with their reasoning, because each closed off an option that will look
attractive again later.

## 1. The rule

Every response must arrive in **150ms warm, 500ms cold**.

Streams (`text/event-stream`) and downloads (`Content-Disposition:
attachment`) fall outside the budget. They are classified by what the
handler wrote, not by a route list, so no waiver is needed for them and
none can rot.

**Cold is the first request the process serves**, timed from process
start. That is the only request whose latency contains the SQLite open,
the migration check, and the template parse. Every later request is
warm.

The one-line version, for `SKILL.md` and the docs: if a response is not
a stream or a download, it arrives in 150ms warm, 500ms cold, or the
work becomes a job.

### Why JSON is not looser

An earlier draft gave JSON and plain text 300ms, on the reasoning that
the concern was slow *pages*. That was wrong three ways, and the number
protected nothing.

A JSON fetch is normally issued *behind* a page rather than instead of
one, so a 150ms page followed by a 300ms fetch is 450ms before the thing
is usable — worse than the page that did the work server-side and would
have been fenced at 150. A looser budget for the request that happens in
addition to the page has it backwards.

JSON also does strictly less work: no template render. Nothing explains
it being slower than the HTML equivalent.

And there is no slow-JSON case the design does not already handle. Agent
tool dispatch synthesizes a request through the app's real mux
(`tools/tools.go:111`), so a tool call is the same handler at the same
cost as the HTTP POST. Form POSTs are 3xx and in budget. Streams and
downloads are classified out. Carlos wake work is either a job or the
exempt wake path.

One number is also the whole rule, with no sub-clause to teach.

### Why redirects are in

Post-redirect-get is where slow form processing hides. A POST that
grinds for two seconds and then redirects is exactly the case `jobs`
exists for, and exempting 3xx would hide it.

### Why carlos wake endpoints are out

`docs/site/deploying.md` tells app authors to do scheduled work inside
the request: "The instance stays awake while it is open, and the idle
clock starts when you return." A budget that fenced those endpoints
would make every app need a waiver to follow the documentation, and
waivers that common stop meaning anything within a month. A wake handler
that genuinely cannot answer inside the budget needs `Slow` (§4) and a
reason, like any other route.

## 2. One middleware, three consumers

A single budget middleware, installed outermost in `serve.go` — outside
`Wrap`, so it measures app middleware too, and so `GET /healthz` and
`GET /api/version` are measured like anything else.

Production runs it. The test gate runs it. The browser rig runs it. One
clock and one classification, so what CI measures cannot drift from what
production reports.

### The ResponseWriter wrapper

Captures status and `Content-Type` at `WriteHeader`, and records the
time of the first write separately from the total.

**It must implement `Unwrap() http.ResponseWriter`.** `jobs/events.go`
drives `http.NewResponseController` to re-arm a per-write deadline
before every event and heartbeat; a wrapper that breaks unwrapping
breaks SSE silently. That is a test, not a comment.

### The breach is judged on total handler time

Not on time to first byte. `ctx.Render` is supplied by the app
(`view/view.go:26`), so templates generally execute straight to the
writer and headers go out early — time to first byte would
systematically understate what the handler cost. `write_ms` is logged
separately so a client-paced body stays distinguishable from a slow
handler.

## 3. `Options.Budget`

```go
// Budget is the app's performance budget. The zero value means the
// framework defaults.
type Budget struct {
	Warm   time.Duration // default 150ms
	Cold   time.Duration // default 500ms
	Reason string        // required when either differs from the default
}
```

`Serve` returns a boot error if a value differs from the default and
`Reason` is empty, in the same style as the existing "returning nil is a
boot error" on `Options.Wrap`. The gate prints configured against
default on every run when they differ.

The failure mode this guards against: the first red CI run gets fixed by
raising the number, and the framework goal quietly stops being one. A
mandatory reason and a printed diff make a raised budget as visible in
CI logs as a waiver. Neither is a lock, and that is understood.

`RASTRILLO_BUDGET_SCALE` multiplies the numbers **for the gate only**,
never at runtime. It tunes for slow shared CI hardware, never for the
app, and the gate prints loudly whenever it is not 1.

## 4. The waiver

```go
func Slow(h http.Handler, reason string) http.Handler
```

Panics at wiring time on an empty reason. Records the pattern and reason
in a list **the gate prints in full on every run**, so a growing waiver
list appears in every CI log rather than only to whoever greps for it.

Streams and downloads are classified out and never need it, so it should
stay rare — the one case visible in the framework today is a wake
endpoint that must hold the request open past the budget.

## 5. Runtime

### The log line

One structured WARN per breach, in every environment:

`budget_breach`, `pattern` (from `http.Request.Pattern` — route-keyed,
so it aggregates and carries no ids or personal data), `method`,
`status`, `class` (page/other/exempt — diagnostic only, since all three
are measured and the first two share one budget), `phase` (warm/cold),
`dur_ms`,
`write_ms`, `budget_ms`.

Throttled per pattern: the first breach logs immediately, then at most
one line a minute carrying a suppressed count. A slow-network day should
not drown the log, and a log pipeline should be able to alert on
`budget_breach` without tuning.

### `Server-Timing` is dev-only

`password/handlers.go` and `password/limit.go` do deliberate timing
equalisation. A precise server-side duration header hands an attacker a
clock with the network jitter removed. A blanket dev-only rule is more
teachable than a per-route exclusion list, and the header is a
convenience, not the mechanism.

### The dev signal

There is no honest dev signal in the repo today. `run.go:59` reads only
`STATE_DIRECTORY`, `serve.go:485` reads only `LISTEN_FDS` and
`LISTEN_PID`, and `cmd/rastrillo/dev.go:247` spawns the app with
`exec.Command(bin, appArgs...)` and never touches `c.Env`.

The framework owns both ends of that spawn, so the signal is asserted
rather than guessed: `rastrillo dev` sets `RASTRILLO_DEV=1`, and
`serve.go` reads it.

Do not infer dev from an absent `STATE_DIRECTORY` or from a localhost
bind. Both misfire in tests and in containers, and a misfire here turns
the header on in production.

One consequence to document in a line: `go run .` without the dev tool
behaves as production, so it logs the WARN and sends no header.

### In the browser, in dev

`ui/rastrillo.js` reads `serverTiming` off the navigation entry and
`console.warn`s a breach. No on-page marker and no dev-only rendering
path. In production the header is absent, so the warning is
self-limiting.

### No hard fail

A panic or error page on breach converts a performance blip on a busy
laptop into a functional outage, and the escape people reach for is
raising `Options.Budget` — the rot §3 is guarding against.

## 6. The gate

### Primary gate: `httptest`, no build tag

`harness/rig.go` is behind the `browser` build tag, so a rig-based gate
would run only where Chromium exists, and issue #86 already records
browser tests behaving differently on GitHub Actions. Chromium
scheduling noise against a millisecond budget is the kind of flake that
gets a gate deleted.

So the primary gate is plain `httptest` plus the same middleware:
deterministic, runs everywhere, and measures the same server-side clock
production reports.

**Cold** constructs a fresh server and times its first request. No
fixtures, deterministic, and it only gets harder to pass as migrations
accumulate and templates multiply.

**Warm** takes a median of N requests after a warm-up.

### The rig is a second consumer

During browser runs, breaches feed `harness/watch.go`'s problem list
alongside console errors and 4xx/5xx responses. Advisory strength,
catching fragment fetches and job-status polling that static walking
misses.

### The scaffold ships it on

`cmd/rastrillo/new.go` gains `.amadan/ci.d/40-budget`, exec-ing a new
`budget` target in the generated `Makefile`. Both places, per AGENTS.md,
or step-reporting runners silently skip it. Its own visible step, so
`RASTRILLO_BUDGET_SCALE` can be applied to one step on a slow runner.

### What the generated test honestly asserts

A generated warm test against an empty database passes trivially. It is
only worse than no test if it claims realism, so it will not.

`budget_test.go` ships with a hand-maintained `pages := []string{"/"}`
list and an empty `seed(t, db)` hook, headed by a comment naming the
failure it cannot yet prevent: an empty database makes every page fast,
so this gate is only as honest as the fixtures you add.

The gate prints what it measured — pages walked, and `seeded: none` —
so triviality is visible in every CI log, the same move as printing the
waiver list. The expensive parts are done by shipping it: the middleware
is in the loop, the route list exists, the step is wired. Tightening it
is additive.

Cold, meanwhile, is real from day one and needs no fixtures at all.

### One example proves it

`examples/notes` gets one budget test with real seeded rows. It is the
only in-repo proof that the generated shape compiles, runs, and fails
when a page is genuinely slow. Tito Go and Seapointish prove it
downstream, but nothing in this repo would.

CI must invoke it explicitly, because the examples are separate modules
and the root `go test ./...` does not compile them.

Test suites for `blog`, `helloworld` and `tickets` are out of scope.

## 7. Rulings, and what each closed off

**One number for everything that is not a stream or a download.** Two
alternatives were rejected. Gating only pages and merely logging the rest
leaves a slow JSON endpoint degrading every page that fetches it with no
gate ever firing. Gating non-pages at a looser 300ms — the draft this
spec was first written against — closed that hole with a number that
protected nothing, for the reasons in §1. The carlos pattern that a
blanket budget would otherwise tax is handled by classification and by
`Slow`, not by a second tier.

**Classification comes from what the handler wrote, not from where it
flushed.** Stopping the clock on first flush was considered and
rejected: a handler that flushes early, including by accident through a
streaming template, would leave the budget with no trace, and gate
behaviour would depend on internal buffering. That contradicts the rig's
stated value that a silent failure is impossible.

**The numbers are per-app configuration, not constants.** This departs
from the `jobs.maxRunningPerOwner` precedent ("a constant, not config").
Apps genuinely differ, and §3's mandatory reason plus printed diff are
the mitigation for the rot that follows.

## 8. Testing

- Classification: html, redirect, JSON, `text/event-stream`,
  `Content-Disposition: attachment`.
- Cold against warm: the first request of a process is cold, the second
  is not.
- `Unwrap` preservation: an SSE handler still drives
  `http.NewResponseController` through the wrapper.
- `write_ms` splits out from total on a client-paced body.
- Throttle: N breaches on one pattern within a minute produce one line
  and a suppressed count.
- Boot error when `Budget` differs from the default with no `Reason`.
- `Slow` panics on an empty reason.
- Scaffold: `40-budget` exists, the `Makefile` has the `budget` target,
  and the step execs the target rather than running its own command
  (`cmd/rastrillo/new_scaffold_test.go:55`).
- The gate can fail: a deliberately slow handler must turn it red.

## 9. Docs

- New `docs/site/performance.md`: the rule, the reasoning, and the jobs
  escape.
- `docs/site/jobs.md` gains a pointer to it.
- `docs/site/testing.md` gains the gate.
- `SKILL.md` gains one short paragraph. It sits at 17,487 bytes against
  the 18,000 ceiling (`skillmd_test.go:46`), so trim first, and raise
  only with the justification that comment demands.

## 10. Out of scope, named

- No metrics endpoint and no breach counter. If the production half
  proves to be observability theatre for apps with no log habit, the
  additive fix is a counter on `/api/version` or in the eventlog.
- No per-route budget configuration beyond `Slow`.
- No browser-timing budget.
- No test suites for the other three examples.

## 11. Known weaknesses

- **The scaffolded warm gate can stay green and empty forever.** A team
  sees a passing budget step, believes 150ms is enforced, and meets its
  first real breach in production logs. `seeded: none` in the output is
  the mitigation, and it only works on someone who reads CI output.
- **The production half is only as strong as a log habit.** Nothing
  fires if nobody tails or alerts on `budget_breach`.
- **`Options.Budget` can be raised to turn red green.** §3's guards make
  it visible, not impossible.
