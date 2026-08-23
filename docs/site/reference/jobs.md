# 🤖 jobs

`github.com/carlosframework/rastrillo/jobs`

Background work you can watch: `Start` runs a function in a goroutine
and hands back an id a status page can poll.

[Background jobs](/docs/jobs) is the guide.

## In-memory, on purpose

These apps are single-process and a restart kills the goroutine, so a
persisted row would only persist a lie. A job is a goroutine, and a
deploy ends it mid-flight. Design jobs to be idempotent; work that must
survive a restart belongs in
[`eventlog`](/docs/reference/eventlog).

## New, Start and Get

```go
func New(logger *slog.Logger) *Jobs
func (j *Jobs) Start(owner, name, location string, fn func(ctx context.Context, progress func(string)) error) (Job, error)
func (j *Jobs) Get(id, owner string) (Job, bool)
```

Build one `*Jobs` at boot. The zero value is not usable.

`owner` is the session **`Subject`** — a string, because keymail
subjects are emails and password subjects are numeric strings. Key your
own job-related rows the same way.

`fn`'s error text reaches the owner, so write it for them. A panic
inside `fn` becomes `Failed` rather than taking the process down.

`Get` answers only the owner: a wrong owner and an unknown id are
indistinguishable, the same 404 rule the
[`scope`](/docs/reference/scope) package enforces for rows.

## The two bounds

`Start` returns `ErrOwnerBusy` past **four** running jobs per owner.
Flash your own copy — the framework does not choose your wording.

A job still running after **15 minutes** is marked `Failed` and stops
counting against the limit; its context expires at the same moment, so a
well-behaved `fn` stops too.

What no bound can do is kill a goroutine. An `fn` that ignores its
context runs invisibly until the process restarts — it just no longer
blocks its owner. Honour `ctx`.

## Job and Status

```go
type Job struct {
	ID         string
	Owner      string
	Name       string
	Status     Status
	Progress   string
	Err        string
	Location   string
	StartedAt  time.Time
	FinishedAt time.Time
}
```

`Status` is `Running`, `Done` or `Failed`. **There is no "queued"** —
`Start` runs the goroutine immediately.

`Location` is where the owner lands when the job finishes; empty keeps
them on the status page. `Progress` is the latest text `fn` passed to
its `progress` callback, empty until it sets one.

## The handlers

```go
func NewHandlers(cfg Config) (*Handlers, error)

type Config struct {
	Jobs           *Jobs
	Render         func(w http.ResponseWriter, r *http.Request, d PageData)
	RenderFragment func(w http.ResponseWriter, r *http.Request, d PageData)
}
```

All three are required and `NewHandlers` errors otherwise.

| Handler | Route | Behaviour |
|---|---|---|
| `Handlers.StatusPage` | `GET /jobs/{id}` | full page; 303 to `Location` when done |
| `Handlers.Fragment` | `GET /jobs/{id}/fragment` | the partial alone; 204 + `Rastrillo-Location` when done |
| `Handlers.Events` | `GET /jobs/{id}/events` | Server-Sent Events |

Mount all three inside the `sess.Require` group.

**`RenderFragment` must draw the partial alone**, without the layout, or
the layout nests inside itself on the next poll.

## PageData

```go
type PageData struct {
	Job          Job
	FragmentPath string
	EventsPath   string
	PollSeconds  int
}
```

`FragmentPath` goes in `data-poll`; `PollSeconds` feeds
`data-poll-every` and the `<noscript>` meta refresh. **Emit that meta
only while the job is running**, or a failed page refreshes forever.

`EventsPath` is opt-in: a template that puts it in `data-poll-push`
upgrades a supporting browser to server push, and an app that ignores it
gets plain polling exactly as before.

## The stream

`Events` sends `update`, `done` and `gone` events with `: ping`
heartbeats every 15 seconds, per-write deadlines, and a bounded stream
lifetime. It is one-way and it is an enhancement — the shim falls back
to timer polling on its own, permanently, if the stream fails.
