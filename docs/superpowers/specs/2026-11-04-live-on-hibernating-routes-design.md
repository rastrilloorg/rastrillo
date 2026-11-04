# Live pages on routes that hibernate

Status: design, 2026-11-04. Branch `live-held-streams`. Follows the live
fragments design (`2026-10-26-live-fragments-design.md`, section 5,
"Sleep and restarts"), which kept live streams off hibernating routes
until carlos/platform#432 shipped. It shipped in platform v0.33.7: the
edge holds a marked stream (platform spec
`docs/superpowers/specs/2026-10-28-edge-held-streams-design.md`,
`internal/edge/heldstream.go`).

## What the platform gives us

A stream whose URL carries `carlos_hold=1` is held by the edge that
manages the instance. The edge strips the marker, never wakes the
instance for the stream and never counts it as activity, in any
outcome. While the instance sleeps the browser's connection stays open
with the edge's own heartbeats. While it is awake the edge relays the
app's events, and it reopens each upstream leg (after a wake, or when
the app ends a leg) with `Last-Event-ID` set to the last id it relayed.
Missed events are the app's to resend.

The bounds Rastrillo must live with: one hour per browser connection,
then the edge closes it at an event boundary; 64 held streams per host,
past which the edge answers `200` with `retry: 30000` and closes; a leg
shorter than 30 seconds is a short leg, and five in a row close the
browser's stream; a `3xx` on a first leg becomes `204`, which ends an
`EventSource`; later legs' headers, `Set-Cookie` included, are dropped;
comment-only blocks from the app are dropped (the edge heartbeats every
25 seconds); same-origin only; WebSockets are never held.

## Decided

**1. live.js always marks its stream.** `EventSource` opens
`PATH?carlos_hold=1&boot=…&t=…`. The marker is harmless everywhere
else: on an always-on CARLOS route the edge strips it, and off CARLOS
the stream handler reads only `boot` and `t`, so an unknown parameter
is ignored (pinned by `TestTheHoldMarkerIsIgnoredWhereNoEdgeStripsIt`).
A per-page switch ("am I on CARLOS, and does this route hibernate?")
would need a signal the platform does not give, and getting it wrong
one way keeps the instance awake all night. The marker goes in live.js
rather than in the meta tag's path so the stream URL stays one thing
the script builds; the vendored-file test in every app already fails
until `rastrillo doctor --fix` re-copies live.js.

**2. A restart forces one refetch; versions do not survive.** Topic
counters live in memory, and hibernation ends the process. Every event
id already starts with the hub's boot ID, a fresh random value per
process, and `resume` applies a `Last-Event-ID` only when its boot is
this process's. So after a wake the edge's cursor, and the URL's
versions, are from another boot: every followed topic is sent as
changed, with ids under the new boot, and live.js refetches each
region whose `data-live-boot` differs. An id therefore never goes
backwards, because no process reads another's numbers, and no change
can be missed, because a process that cannot vouch for a version
refetches. The next leg carries the new boot's cursor and costs
nothing.

Persisting counters was rejected. The hub is in memory and imports only
the standard library; persisting would put a write beside every
`Notify` and a store in every app, to save one fetch per region per
open page per wake. Those fetches happen while the instance is already
awake (a change event only comes from a live leg), they make no further
event, and the instance sleeps again after its idle window. With 64
held streams per host the cost is bounded.

`NotifyAt` times that fall while the instance sleeps do not fire; the
next wake refetches every open page anyway, and `carlos.ScheduleAt` can
wake the instance at a time pages must show promptly.

**3. Reconnects back off unless a stream stayed open.** live.js reset
its backoff to one second on every `open`. A stream the edge opens and
closes at once (over the per-host cap, or after refusing a reopen) fires
`open` then `error`, so the page reconnected every second while the cap
lasted; the edge's `retry: 30000` never applies because live.js closes
the `EventSource` on error and owns reconnecting. Now the delay resets
only after a stream has been open 30 seconds, the edge's own short-leg
bound. A max-hold close after an hour reconnects after one second, with
the regions' current versions, so it costs no fetch.

**4. The server's own bounds fit the edge's.** The hub's stream
`Lifetime` (5 minutes by default) ends legs well past the 30-second
short-leg bound; the doc on `Config.Lifetime` says to keep it there.
Heartbeats stay `: ping` blocks, which the edge drops and a direct
browser ignores; they carry no `id`, so they never move a cursor. Every
change event carries an `id`.

## What still holds

- Always-on routes work as before; the marker is stripped there.
- A route that still serves an old live.js (no marker) keeps a
  hibernating instance awake. Re-vendor with `rastrillo doctor --fix`.
- A signed-out stream behind `sessions.Require` is redirected to
  sign-in; the edge answers that with `204` instead of following it.
  Either way the `EventSource` ends and live.js retries with its
  backoff, up to a minute, as it did when the browser followed the
  redirect to an HTML page. `Allow` refusals are `403`.
- A cookie refreshed on the stream request is lost on later legs; the
  page's own fetches carry it.
- Job progress streams (`rastrillo.js` push) are not marked: a running
  job keeps the instance busy anyway.

## Proven

- `live/held_test.go` plays the edge across a sleep and a wake: a leg
  relays a change, the process stops, a new hub resumes from the old
  cursor and sends every topic under its own boot, and a further leg
  with the new cursor is quiet. Removing the boot check from `resume`
  fails it.
- `ui/live_test.go` holds the stream URL to `carlos_hold=1` and the
  backoff to resetting only after a 30-second stream.
- The platform's own witnesses (`internal/activator/heldstream_e2e_test.go`)
  cover the edge with a real child serving ids and `Last-Event-ID`.

Open: an end-to-end run of a Rastrillo app on a hibernating CARLOS route
(the live fragments spec's "Proven end to end"), which needs a deploy,
and Patrix turning its dashboard on.
