# Background work

`jobs` runs observable in-memory goroutines: a restart kills them, fn's
ctx expires at 15 min (job turns Failed) — keep jobs idempotent, honor
ctx. `j := jobs.New(logger)` once; `j.Start(owner, name, location, fn)`
runs `fn(ctx, progress func(string)) error`, returns `(Job, error)`;
`ErrOwnerBusy` past 4 Running per owner (flash your own copy). 303 the
caller to `/jobs/`+job.ID. Owner is the session **Subject**, not
`sessions.UserID` — key job rows the same way. fn's error text reaches
the owner. `j.Get(id, owner)` 404s a foreign or unknown id.

`jobs.NewHandlers(jobs.Config{Jobs, Render, RenderFragment})` returns
`StatusPage`/`Fragment`/`Events` (SSE); mount all three in the
`sess.Require` group under `/jobs/{id}`. Two traps: `Render` draws the
whole page and `RenderFragment` the partial alone, or the layout nests
on the next poll; and it must work with scripts off — a `<noscript>`
meta refresh only while Running, or a failed page refreshes forever.
docs/site/jobs.md

The JavaScript is app-owned `static/rastrillo.js`, inert until
markup opts in, and `static/busy.js`, on by default. `data-poll="URL"` +
`data-poll-every="2"` swap the element for the fetched fragment,
repeating while the fragment still carries `data-poll` (ui's
`job-status` partial drops it once done); the partial's `PushURL`
(= `EventsPath`) emits `data-poll-push` and the shim rides SSE, falling
back to polling. `static/busy.js`: every submit button's label gives
way to a spinner for ≥650ms (the submit is held; beside the label under
reduced motion or forced colours), `aria-busy` and a double-submit
guard by DEFAULT, cleared on back; `data-busy="false"` (form or button)
opts out, `data-busy-label` shows words beside the spinner. Manners, not
idempotency — the server still refuses the second write.
`static/light.js`: the light the depth styles read (the hour on
shadows, a sheen under the pointer); to turn it off delete it and its
tag and name it in `vendoredIsMine`; doctor then lists it as yours and
`--fix` leaves it alone.

## Scheduled work

Hibernation means a `time.Ticker` is not a scheduler. Declare recurring
work outside the app (`carlos schedule set -name sync -every 6h -path
/jobs/sync`); the platform wakes the instance and POSTs there. Guard
with `carlos.Tick(r)` (bearer == `$CARLOS_ADMIN_TOKEN`, constant-time,
false with no token) and work inside the request — 202-plus-goroutine
hibernates mid-job; 2xx done, 5xx retry, 4xx don't. At-least-once
delivery: dedupe on `carlos.TickOccurrence(r)` (stable across retries),
never the clock. One-offs: `carlos.ScheduleAt(ctx, name, at, path)`
(upsert by name; `ErrNotOnCarlos` off-platform, `ErrDeclaredSchedule`,
`ErrTooManyTimers`) and `carlos.ScheduleCancel`. Seal with
`carlos.InstanceKey(own)`: the app's own key, else the platform's; mint
one only on `ErrNotOnCarlos`, never start on `ErrNoInstanceKey`.
