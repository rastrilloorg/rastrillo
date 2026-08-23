# 🤖 Background jobs

`jobs` is the observable handle for background work: `Start` runs a
function in a goroutine and hands back an id a status page can poll.

## It is in-memory, and that is the honest choice

These apps are single-process, and a restart kills the goroutine
anyway — so a persisted row would only persist a lie. A job is a
goroutine, and a deploy ends it mid-flight.

Two consequences to design around:

- **Make long jobs idempotent and re-runnable.** They will be
  interrupted.
- **Work that must survive a restart does not belong here.** Reach for
  [`eventlog`](/docs/reference/eventlog) instead.

## Starting one

```go
j := jobs.New(logger) // once, at boot

job, err := j.Start(owner, "Export notes", "/notes", func(ctx context.Context, progress func(string)) error {
	progress("gathering")
	// ...
	return nil
})
if errors.Is(err, jobs.ErrOwnerBusy) {
	flash.Set(w, "error", "You already have several exports running.")
	http.Redirect(w, r, "/notes", http.StatusSeeOther)
	return
}
http.Redirect(w, r, "/jobs/"+job.ID, http.StatusSeeOther)
```

`owner` is the session **`Subject`**, not `sessions.UserID` — a string,
because keymail subjects are email addresses and password subjects are
numeric strings. Key your own job-related rows the same way.

`location` is where the owner lands when the job finishes; `""` keeps
them on the status page.

`fn`'s error text reaches the owner, so write it for them.

### Two bounds, and one thing no bound can do

An owner may have at most **four** jobs running at once; `Start` past
that returns `ErrOwnerBusy`, and the copy you flash is yours to write.

A job still running after **15 minutes** is marked `Failed` and stops
counting against the owner's limit. Its context expires at the same
moment, so a well-behaved `fn` stops too.

What no bound can do is kill a goroutine. An `fn` that ignores its
context runs invisibly until the process restarts — it just no longer
blocks its owner. Honour `ctx`.

### Reading a job

```go
job, ok := j.Get(id, owner)
```

`Get` answers only the owner. A wrong owner and an unknown id are
indistinguishable — the same 404-not-403 rule
[scoping](/docs/scoping) enforces for rows.

There is no "queued" status: `Start` runs the goroutine immediately.
`Running`, `Done` and `Failed` are all of them. A panic inside `fn`
becomes `Failed` rather than taking the process down.

## The status page

```go
h, err := jobs.NewHandlers(jobs.Config{
	Jobs:           j,
	Render:         renderJobPage,
	RenderFragment: renderJobFragment,
})
```

All three fields are required and `NewHandlers` errors otherwise. Mount
the three routes inside the `sess.Require` group:

```go
r.Group(func(r chi.Router) {
	r.Use(sess.Require)
	r.Get("/jobs/{id}", h.StatusPage)
	r.Get("/jobs/{id}/fragment", h.Fragment)
	r.Get("/jobs/{id}/events", h.Events)
})
```

Both renderers receive a `jobs.PageData` carrying the `Job`,
`FragmentPath`, `EventsPath` and `PollSeconds`.

**`RenderFragment` must draw the partial alone**, without the layout. If
it renders a whole page, the layout nests inside itself on the next
poll.

A finished job with a `Location` makes `StatusPage` answer 303. The
fragment's equivalent is 204 plus a `Rastrillo-Location` header.

## It must work with scripts off

The status page carries a `<noscript>` meta refresh of `PollSeconds` —
**only while the job is running.** Emit it unconditionally and a failed
page refreshes forever.

## The JavaScript, and its limits

The only JavaScript in the framework is `static/rastrillo.js`, a ~130
line app-owned shim that `rastrillo new` writes beside `tokens.css`. It
is inert until markup opts in:

| Attribute | Effect |
|---|---|
| `data-poll="URL"` | replace this element with the fetched fragment |
| `data-poll-every="2"` | seconds between polls |
| `data-poll-push="URL"` | upgrade to Server-Sent Events, falling back to polling |
| `data-busy` | disable a submitting button |
| `data-busy-label` | retitle it while busy |

Polling repeats while the new fragment still carries `data-poll`. The
`ui` package's `job-status` partial drops the attribute once the job is
done, which is what stops the loop — a fragment that always carries it
polls forever.

`PushURL` on the partial (the same value as `EventsPath`) is opt-in. Set
it and a supporting browser rides `Events`; the shim falls back to
timer polling on its own, permanently, if the stream fails. Apps that
ignore it get plain polling exactly as before.

The SSE stream sends `update`, `done` and `gone` events with `: ping`
heartbeats every 15 seconds, per-write deadlines, and a 5-minute
lifetime. It is one-way and it is an enhancement — nothing about the
page depends on it.

htmx remains a choice, not a dependency. `examples/notes` demonstrates
the whole loop with an Export flow.
