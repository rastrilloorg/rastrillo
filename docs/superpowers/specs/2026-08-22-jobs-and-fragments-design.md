# Background jobs, status pages, and the fragment shim — design

**Date:** 2026-08-22
**Status:** Approved (Paul, in conversation: "All of this sounds great. Let's go with 1")

## Problem

Clicking a button in a CARLOS app today kicks off a background goroutine
and then lies about it twice: there is no loading state (the button just
sits there until the redirect lands), and the resulting screen is a
snapshot that never updates — the user refreshes by hand until the work
is visibly done. The framework has nothing to poll (a background process
has no observable handle) and no way to update a page (the no-JS rule,
read strictly, forbids the mechanism).

## The reframing

The rule worth keeping is not "no JS" but **"no JS required."** Every
flow keeps working with scripts disabled — curl-able, lynx-able, no
build step, no CDN — and a small first-party script only makes the
working flow nicer. ui's package doc keeps its existing "no JavaScript"
idioms untouched: menus, toggles, and modals stay native. The shim
exists solely for the one thing HTML cannot do alone — showing work
that finishes after the response was sent.

Explicitly rejected for now: vendoring htmx (adopting its `hx-*`
vocabulary as framework API surface and its upgrade cadence as our
problem — apps that want it can add it themselves) and SSE push
(long-lived connections fight serve's idle-timeout bounds and buy
nothing over a 2-second poll at CARLOS scale; nothing in this design
precludes adding it later).

## Three pieces

### 1. Package `jobs` — the observable-work primitive

A new top-level package, sibling of `sessions`/`flash`. In-memory and
mutex-guarded, deliberately not persisted: these apps are single-process
on SQLite, and a process restart kills the goroutine anyway — a
persisted row would only persist a lie. The package doc says this
honestly: a job is a goroutine; a deploy ends it; design long jobs to be
idempotent and re-runnable, and reach for the eventlog when work must
survive a restart.

```go
type Status string

const (
        Running Status = "running"
        Done    Status = "done"
        Failed  Status = "failed"
)

// Job is a point-in-time snapshot; Get returns copies, never shared
// pointers, so a caller can read fields without holding any lock.
type Job struct {
        ID         string    // random URL-safe token (crypto/rand, 16 bytes, base64url)
        Owner      string    // session Subject that started it; every read is keyed on it
        Name       string    // human label: "Export notes"
        Status     Status
        Progress   string    // latest progress text, "" until the job sets one
        Err        string    // Failed only: the error text, shown to the owner
        Location   string    // where the owner lands when Done ("" = stay on the status page)
        StartedAt  time.Time
        FinishedAt time.Time // zero while Running
}

func New(logger *slog.Logger) *Jobs

// Start runs fn in a goroutine and returns the job snapshot
// immediately. fn's error text is shown to the job's owner — return
// messages fit for them and log internals yourself (the logger passed
// to New also records every start/finish/failure). progress replaces
// the job's Progress text; call it as often as you like.
func (j *Jobs) Start(owner, name, location string, fn func(ctx context.Context, progress func(string)) error) Job

// Get returns the job only to its owner: a wrong or unknown id and a
// wrong owner are the same (Job{}, false) — parity with the scope
// package's someone-else's-row-is-a-404 rule.
func (j *Jobs) Get(id, owner string) (Job, bool)
```

Details:
- `Start`'s ctx is `context.Background()` — jobs outlive their request
  by definition, and tying them to server shutdown is deferred until a
  real graceful-drain story needs it (the package doc says so).
- A panic inside fn is recovered, logged, and recorded as Failed with
  a generic "something went wrong" — a panicking job must not take the
  process down or vanish without a trace.
- Finished jobs are swept 10 minutes after FinishedAt, opportunistically
  inside Start and Get — no background goroutine, nothing to leak.
- Owner is the session Subject (TEXT, same ruling as manifest scoping:
  keymail subjects are emails, password subjects are numeric strings).

### 2. `jobs.Handlers` — the status page and its fragment

Same plugin shape as `password`: a Config with render funcs the app
owns, handlers the app mounts behind `sessions.Require`.

```go
type Config struct {
        Jobs           *Jobs
        Render         func(w http.ResponseWriter, r *http.Request, d PageData) // full status page
        RenderFragment func(w http.ResponseWriter, r *http.Request, d PageData) // the polled partial
}

type PageData struct {
        Job          Job
        FragmentPath string // "/jobs/<id>/fragment", for data-poll and form targets
        PollSeconds  int    // 2; templates write it into data-poll-every and the noscript meta
}

func NewHandlers(cfg Config) (*Handlers, error) // errors on nil Jobs/Render/RenderFragment

// StatusPage: GET /jobs/{id}. Owner-checked via sessions.Current
// (403 "signed out" when absent — mounting contract is behind
// Require, this is defense in depth); unknown/foreign id answers 404.
// Running, Failed, or Done-without-Location: Render. Done with
// Location: 303 See Other to Location. The id comes from r.PathValue("id")
// (chi ≥ v5.1 populates it; the stdlib mux always did).
func (h *Handlers) StatusPage(w http.ResponseWriter, r *http.Request)

// Fragment: GET /jobs/{id}/fragment. Same owner rules. Running:
// RenderFragment. Done with Location: sets header
// "Rastrillo-Location: <location>" and writes 204 No Content — the
// shim navigates there. Done without Location or Failed:
// RenderFragment (whose markup omits data-poll, which is how the
// shim stops).
func (h *Handlers) Fragment(w http.ResponseWriter, r *http.Request)
```

The no-JS fallback lives in the app's status-page template, not in the
handlers: `<noscript><meta http-equiv="refresh"
content="{{.PollSeconds}}"></noscript>` — emitted only while the job
is Running, so a Failed page doesn't refresh forever. With scripts off the page
re-fetches itself every 2 seconds until StatusPage's Done branch
answers 303; with scripts on the noscript meta is inert and the shim
polls the fragment instead. No Refresh header — a header would also
fire with JS enabled and fight the shim's smooth swap.

### 3. The shim — `rastrillo.js`

One first-party, dependency-free, IIFE-wrapped file, ~100 lines with
comments, CSP-clean (external file, no eval, no inline handlers).
Delivered exactly like tokens.css: embedded in `ui`
(`ui.ShimJS() []byte`), written once into a new app's `static/` by
`rastrillo new`, app-owned and editable from then on. It is inert by
default — only elements that opt in with a data attribute get behavior.

Vocabulary (all opt-in):

- `data-poll="URL"` + optional `data-poll-every="2"` (seconds,
  default 2): fetch URL with `Rastrillo-Fragment: 1` request header,
  expect an HTML fragment, replace the element's **outerHTML**, rescan
  the replacement. outerHTML is the stop condition: a fragment rendered
  for a finished job simply omits `data-poll` and polling ends. A
  fetch error backs off (double the interval, cap 30s) and keeps
  trying — transient network blips must not strand a status page.
- A polled response carrying a `Rastrillo-Location` header navigates:
  `window.location.assign(value)`. (Same-origin responses only — the
  fetch uses default same-origin mode, so a cross-origin value never
  reaches the handler.)
- `data-busy` on a `<form>`: on submit, disable its submit buttons and
  set `aria-busy="true"` on the form — instant feedback plus
  double-submit protection. Optional `data-busy-label="Exporting…"`
  swaps the button text. The form still submits normally; this is
  decoration on the way out, never interception.

tokens.css gains one small addition: `rst-spin`, a pure-CSS spinner
for status pages and busy buttons (respecting
`prefers-reduced-motion`).

### ui partial

`ui/partials/job-status.html`: the fragment's default markup — status
line, name, progress text, spinner while running, error text on
failure — taking a dict (Name, Status, Progress, Err, PollURL,
PollSeconds) like every other partial, carrying its data contract in a
template comment, registered in TestAllPartialsAreDefined. Apps can
ignore it and write their own fragment; notes uses it.

## The notes example

The demo is "Export": a button on the index (in a `data-busy` form)
POSTs `/export`, which starts a job that renders every note the owner
has into one markdown document, writes it to a new `exports` table
(owner Subject, content, created_at — GORM model beside User/Note),
and finishes with Location `/exports/{id}` — a handler serving the
document as `text/markdown`, owner-scoped so Bob fetching Alice's
export answers 404. The job sleeps ~300ms per note with a comment
saying it simulates work so the status page is actually visible in a
demo. The status page template uses the job-status partial + the
noscript meta; layout gains the `<script src=…rastrillo.js defer>`
tag; the two-user isolation suite gains the export flow (Bob cannot
see Alice's job — 404 on her /jobs/{id} — nor her export).

## Docs and scaffold

- `cmd/rastrillo/new.go`: scaffold writes `static/rastrillo.js` beside
  tokens.css; scaffold layout gains the script tag; new_test asserts
  the file lands.
- SKILL.md: teaches jobs + the shim vocabulary. **Hard constraint:**
  the 15,000-byte budget test currently passes at 14,998 — additions
  must be paid for by trims elsewhere, verified by running
  skillmd_test.go before commit.
- README: one honest paragraph — background work is observable, the
  status page works with scripts off, the shim is ~100 readable lines
  the app owns, htmx remains a choice not a dependency.
- package docs: jobs (the honesty paragraph about process lifetime),
  ui (shim delivery, mirroring the tokens.css paragraph).

## Testing

- jobs: Start/Get round trip; owner isolation (wrong owner = not
  found); progress updates visible across Get calls; Failed captures
  the error text; panic in fn → Failed + logged, process alive; sweep
  removes finished jobs after TTL (injectable clock, same pattern as
  sessions/passkey tests); concurrent Start/Get race test (`go test
  -race` already runs in CI).
- handlers: running → 200 + Render called with FragmentPath;
  done-with-Location → 303; fragment done-with-Location → 204 +
  Rastrillo-Location; foreign id → 404; signed out → 403.
- shim: no browser harness exists or is being added — the shim is
  covered by a Go test asserting the file parses as the vocabulary
  documents (contains the attribute names, no `eval`, IIFE shape) and
  by the notes suite exercising the noscript path end-to-end (the
  fragment endpoint's HTML omits data-poll when done). Honest limit,
  stated in the test comment: JS behavior itself is verified by hand.
- notes: export round trip (POST /export → 303 to /jobs/{id} → poll
  until 303 → GET /exports/{id} serves markdown); two-user isolation
  for jobs and exports.
