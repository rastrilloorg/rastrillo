# Server push for job status (SSE) — design

Date: 2026-08-22. Target: v0.15.0. Clears the site's "Server push"
Pending item. Commit this file as
docs/superpowers/specs/2026-08-22-jobs-sse-push-design.md.

## 0. Why now / the timeout truth

The v0.12.0 spec deferred SSE as "fights serve's idle-timeout bounds".
Recon shows that objection is factually void under default Options:
`WriteTimeout` is zero unless the app opts in, and `IdleTimeout` (120s)
"can never interrupt an in-flight request — only a connection doing
nothing" (serve.go:163). serve.go's own `# Timeouts` doc (serve.go:231-257)
prescribes the sanctioned streaming recipe — a per-handler idle
deadline via `http.NewResponseController`'s `SetWriteDeadline`,
re-armed as bytes move. This feature is that recipe's first real user,
and the spec corrects the v0.12.0 spec's stated rationale in passing
(one sentence added to the old spec is NOT needed — the new spec
records the correction; do not edit the old spec).

## 1. Shape

One new endpoint on the existing `jobs.Handlers`:

    GET /jobs/{id}/events        text/event-stream

mounted behind `sess.Require` beside `/jobs/{id}` and
`/jobs/{id}/fragment`. Same `lookup` as the other two: signed-out →
403 "signed out"; foreign/unknown id → 404. No change to the `jobs`
registry core: the handler observes `Jobs.Get` snapshots itself — no
broadcast primitive, nothing new to lock.

The one rendering path stays: SSE never carries HTML. Events tell the
shim *when* to re-fetch the fragment it already knows how to swap.

## 2. The stream protocol

- On connect: `Content-Type: text/event-stream`, `Cache-Control:
  no-store`, then flush headers (`http.Flusher`; if the
  ResponseWriter does not implement it, answer 500 before headers).
- The handler ticks internally every 1 second (a `time.Ticker`),
  calling `Jobs.Get(id, owner)`:
  - Job vanished (swept/expired) → send `event: gone` + `data: ` and
    close the stream.
  - Snapshot changed since last sent (compare Status+Progress+Err) →
    send `event: update` with `data:` = the job's Status (the payload
    is advisory; the shim re-fetches the fragment regardless).
  - Job left Running → send `event: done` + `data:` = Location ("" if
    none), then close. (The shim navigates via its existing localPath
    guard, or re-fetches the fragment once when data is empty.)
- Heartbeat: every 15 seconds with no event, write a comment line
  `: ping` + blank line — keeps intermediaries and the client honest.
- Before EVERY write (event or heartbeat):
  `rc.SetWriteDeadline(time.Now().Add(30 * time.Second))` via
  `http.NewResponseController(w)`; a write error ends the handler.
  After every write: `Flush()`.
- Lifetime bound: the stream closes itself after 5 minutes
  (`streamTTL`) even if the job still runs — the client reconnects
  (EventSource auto-reconnects) or falls back to polling. This keeps
  any one connection's cost bounded and plays fair with the 10-second
  graceful-shutdown drain (see next).
- `r.Context().Done()` (client gone, server draining) ends the
  handler immediately — select on it beside the ticker. This is what
  keeps `srv.Shutdown`'s 10s budget safe: streams end at once when
  drain begins because the ticker loop observes ctx.
- Constants: `streamTTL = 5 * time.Minute`, tick 1s, heartbeat 15s,
  write deadline 30s — all package consts with doc comments, tick and
  TTL overridable by tests via unexported fields on Handlers (mirror
  jobs.Jobs's `now`/`timeout` test-injection pattern).

## 3. PageData and the partial

- `jobs.PageData` gains `EventsPath string` = "/jobs/"+id+"/events",
  set by `pageData` beside FragmentPath.
- `ui/partials/job-status.html` gains an optional dict key `PushURL`:
  while running, emit `data-poll-push="{{.PushURL}}"` beside
  `data-poll` when the key is set. Apps that don't pass it get today's
  polling exactly. (The partial's existing keys stay; ui/ui_test.go's
  partial test extends.)

## 4. Shim upgrade (ui/rastrillo.js)

Vocabulary: `data-poll-push="URL"` — optional, only meaningful beside
`data-poll`. Behavior:

- If `window.EventSource` exists and the element carries
  `data-poll-push`, open an EventSource to it instead of arming the
  poll timer. On any `update` event → run the existing tick (fetch
  fragment, swap, re-scan the new element for data-poll/data-poll-push
  — a fragment that stops emitting them stops everything). On `done` →
  close the source, then run tick once (the fragment answer's 204 +
  Rastrillo-Location path handles navigation exactly as today). On
  `gone` → close and stop.
- On EventSource `error`: close it and fall back to timer polling for
  this element permanently (one downgrade, no flapping) — the existing
  backoff loop unchanged.
- No EventSource support or no data-poll-push → today's polling,
  untouched.
- The pending-fetch guard: an `update` arriving while a fetch is
  in-flight is coalesced (one boolean; re-tick after the in-flight
  fetch lands if another update arrived).
- Budget: stay a single IIFE, two-space indent, no tabs, ≤8192 bytes
  (currently 5407). Contract additions to ui/shim_test.go:
  "data-poll-push", "EventSource" among the required strings.
- Header contract comment: document the new attribute and the
  downgrade rule in the same style as the existing vocabulary lines.

## 5. Scaffold and example

- cmd/rastrillo/new.go: nothing changes structurally (the scaffold
  already ships the shim byte-identical to ui.ShimJS(), and
  new_test.go's byte-equality assertion keeps that true
  automatically).
- examples/notes: renderJobPage/renderJobFragment pass PushURL
  (PageData.EventsPath) into the job-status partial; app.go mounts
  `GET /jobs/{id}/events` in the Require group. notestest gains:
  an SSE round trip (start export, GET the events stream, read at
  least one update event and the done event with the export location —
  use a plain http.Client with no timeout on that request, bounded by
  the test's own deadline) and an isolation probe (Bob GET on Alice's
  events URL → 404 before any stream starts).

## 6. Testing (jobs package)

httptest-based, with tick/TTL shortened via the test-injection fields:
- Running job → connect → receive update events as Progress changes,
  heartbeats between; job finishes → done event with Location, stream
  closes.
- Foreign/unknown id → 404, signed-out → 403 (same lookup contract,
  asserted on the events endpoint).
- streamTTL elapses mid-run → stream closes cleanly without done.
- Client disconnect (cancel request context) → handler returns
  promptly (assert via a done channel and a deadline).
- Job swept mid-stream → gone event, close.
- Race: `go test -race ./jobs/` must stay clean (CI already covers
  ./jobs/).

## 7. What is deliberately not here

- No WebSocket, no fallback long-poll endpoint: EventSource +
  poll fallback covers every browser that runs the shim at all.
- No broadcast primitive in the jobs registry: at CARLOS scale a
  1-second Get per open stream is noise; a pub/sub core would be
  complexity without a measured need. Record as the future
  optimization if per-owner stream counts ever matter.
- No SSE for anything but job status (manifest screens etc. stay
  request/response).
- No change to the no-JS path: noscript meta refresh and the status
  page's 303 remain the floor.
- Docs: package doc paragraph in jobs/handlers.go, one sentence in
  SKILL.md §6 and README's jobs bullet — but SKILL.md/README edits are
  OUT OF SCOPE for the implementation branch (the coordinator
  integrates docs to respect the byte budget); note the intended
  sentences in the PR description instead.
