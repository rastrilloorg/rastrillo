# Live pages: a change stream and forms that swap in place — design

**Date:** 2026-10-26
**Status:** Draft, for Paul's review
**Branch:** `live-fragments`
**Written by:** Claude Opus 5.5 (claude-opus-5-5), default reasoning.
Reviewed by a builder check and by GPT-6 Astra (high reasoning); this
revision answers both.

## Why

Rastrillo apps feel like websites from 2010 next to the tools people use
every day. Nothing is broken. A dashboard is a snapshot from the moment
it loaded, and a button that changes one row reloads the whole page.
Paul's words: "a general sense that things could feel more live."

LiveView was considered and rejected. It needs a server process per
open tab and a socket for every interaction, so a page stops working
when the script does. That breaks Product Principle 1 ("the scriptless
path is the real one"), and it fights how CARLOS apps run: one process,
SQLite, idle timeouts, instances that sleep. A Next.js-style client
runtime was rejected for the same reason. This design extends what
Rastrillo already does for background jobs instead.

## The target

Patrix's dashboard (`/admin`) is the first real use. Tito Go's
dashboard not updating as sales come in is the same problem, but Tito
Go uses Rastrillo only as a parts bin, so it is an example and not the
target.

What success looks like:

- An editor has `/admin` open. A visitor leaves a comment, and
  "Comments waiting" goes from 2 to 3 within a couple of seconds. No
  reload, nothing jumps, the sidebar stays put.
- On the comments screen, a moderator clicks Approve. That row changes
  in place, the "Comment approved" notice is shown and announced, and
  focus lands somewhere sensible. Every open dashboard updates too.
- A form with a mistake shows its errors in place.
- A dashboard left open on a sleeping site does not keep it awake.
- With JavaScript off, every page works exactly as it does today.

## Goals

1. **A change stream.** An app announces that something changed
   (`"comments"`), after the write commits. Every open page showing
   that thing fetches its affected region again from the server.
2. **Forms that swap in place.** A form marked for it posts with fetch
   and swaps in the regions that changed, instead of reloading the
   page. The handler is the same one that does post-redirect-get today.
3. **One rendering path.** The server renders HTML for a region with
   the same template, data and permission checks as the full page.
   Nothing new renders HTML on the client.
4. **Sleep stays possible.** An open tab must not keep a CARLOS instance
   awake forever (section 5).

## Non-goals

- Patrix adopting it. That is a follow-up branch in Patrix, after this
  lands. This branch proves the design in an example.
- Moving the jobs stream (`jobs/events.go`) onto the new stream. It
  works; it can move later if the new one proves itself.
- Fan-out across processes. Every writer Patrix has runs in the process
  that serves the dashboard. `Notify` is the only entry point, so a
  cross-process transport can sit behind it later without changing
  callers.
- Optimistic UI, client-side state, client-side templates.
- WebSockets. The browser never sends anything over the stream.
- A replay log. The stream says *that* a topic changed, never *what*;
  the server is the truth.
- Making a non-idempotent handler safe to resubmit. That stays the
  app's job, as it is under `busy.js` today.

## Anti-goals

Any of these makes it a failure even if it technically works:

- A page that needs the script to work at all.
- Data sent over the stream, or a viewer learning about activity they
  may not see. The stream carries topic names only, and every
  subscription is checked against the viewer (section 2).
- Typed input lost because a request failed.
- A write repeated because the shim guessed it had failed.
- Numbers that have stopped moving with nothing to say so.
- A dashboard left open all night that keeps the site awake and billed.
- htmx's vocabulary or its upgrade cadence as framework API (the
  2026-08-22 jobs design already turned that down).

## What exists today

- `ui/rastrillo.js`: `data-poll="URL"` swaps an element for the
  fragment it fetches; `data-poll-push="URL"` makes an EventSource
  trigger that fetch. Every fetch sends `Rastrillo-Fragment: 1`, which
  the shim's doc reserves and no handler reads. A 204 with
  `Rastrillo-Location` navigates to a local path. Polling waits for
  `prerenderingchange` on a prerendered page.
- `jobs/events.go`: the only SSE stream. It re-reads one job each
  second; there is no broadcast primitive anywhere in Rastrillo
  (`eventlog`, `jobs`, `background` and `db` have none). Its 30-second
  write deadline is longer than the shutdown drain.
- `serve.go`: no Read/WriteTimeout; streams re-arm a write deadline per
  write (the "# Timeouts" recipe). `srv.Shutdown` gets 10 seconds. Go
  does not cancel in-flight request contexts on Shutdown, so an open
  stream holds the drain until it gives up. `Options.Background` is
  stopped *after* the drain, before the database closes.
- `ui/busy.js`: marks a submitting form busy in a capture listener,
  then releases it if the submit turns out to be cancelled
  (`busy.js:147`). Handles `pagehide`/`pageshow` for the back/forward
  cache. Leaves idempotency to the server (`busy.js:54`).
- `csrf`: origin-based, so a same-origin fetch POST passes unchanged.
- `flash`: a one-shot cookie, taken and cleared by the next render that
  calls `Take`. Patrix's `render` takes it on every render
  (`render.go:132`), and both Patrix's layout and notes' layout omit the
  notice element entirely when there is none.
- Fragments: no helper. `examples/notes` builds a second, layout-less
  `ui.Pages` with its own Entry to render one. Patrix does not use
  `ui.Pages`; it runs `ExecuteTemplate(w, "layout", …)` on its own map,
  and its dashboard body is already a named template
  (`dashboard-body`).
- CARLOS serves browsers over HTTP/2 (carloku.com answers `h2`), so one
  stream per tab does not exhaust a browser's connections there.

Prior art from Eleven (WebSocket, JSON, one in-memory hub per process):
treat the stream as a hint and fetch again on reconnect and when the tab
comes back into view; announce a change only after its commit (firing
at claim time once left a card stuck); never lose typed input on a
failed send; key a re-fetch to what the region shows now, not what it
showed when the fetch started.

## The design

### 1. Regions

A region is an element on a page that can be fetched on its own. It has
an `id`, and it names the topics it shows:

```html
<section id="dashboard-stats" data-live="entries comments">…</section>
```

The page's server-side render marks which templates are regions. A
request for the page carrying `Rastrillo-Fragment: dashboard-stats` (a
space-separated list is allowed) gets back only those regions, rendered
by the same handler with the same data and permission checks, each as a
top-level element carrying its `id`. The shim swaps each in by `id`.

This puts the existing reserved header to work instead of adding a
route per region: one URL, one handler, one permission check.

Rules:

- **Only marked regions render.** The server renders a name only if
  this page marked it as a region. It never executes a template by an
  arbitrary name.
- **Wrong page, no render.** A request naming no region this page has
  answers 204 with `Rastrillo-Location` set to its own path, which the
  shim already reads as "navigate there". It decides this before taking
  the flash, so the page it navigates to still shows the notice.
- **The flash is taken only by a render that shows it.** A fragment
  request that does not ask for the notice region must not take the
  flash. A stream-triggered fetch never asks for it; a form's fetch
  does. Otherwise a background refresh would eat a notice it never
  displays.
- **Stable containers.** Every region's element is always present, even
  when empty: a list with nothing in it renders its empty container.
  That includes the notice, which every layout renders as a persistent,
  empty `role="status"` element. A region in the response with no
  element on the page is ignored. A requested region missing from the
  response is left as it is. Regions do not nest.
- **Not cached.** Fragment and full responses for a page that has
  regions send `Cache-Control: no-store` and `Vary: Rastrillo-Fragment`,
  added to any `Vary` already present. A refetch must never be answered
  from a cache.
- **Fits both kinds of app.** Whatever the helper is, it must work for
  `ui.Pages` apps and for an app like Patrix with its own template map.
  Patrix's `render` should need a few lines, not a rewrite.

### 2. The change stream: server

A new package (the builder names it; `live` is the working name) holds
an in-memory hub.

- **`Notify(topics …)`** is called by the app after the write commits.
  It never blocks the caller. A subscriber that cannot keep up is
  dropped, not waited for, and its client catches up on reconnect.
- **Topics are declared, and checked per viewer.** A topic is a
  declared name plus an optional key: `comments`, or `notes` keyed by
  owner. The app declares the names up front, and for each it supplies
  the check that says whether this viewer may follow it (and that
  key). An author asking for `comments` in Patrix is refused, as is
  anyone asking for another person's `notes`. Without keys, one
  person's write would make every other person's private list fetch
  again, and tell them someone wrote. An undeclared name or a refused
  check fails the stream, so a client can neither grow the hub's maps
  nor listen in.
- **Versions, so a reconnect costs nothing when nothing changed.** The
  hub counts every `Notify` with one sequence number, and remembers,
  per topic, the number of its last change. Together with a boot ID
  that is new each time the process starts, that gives a version
  (`<boot>.<seq>`).
  - Every page that has regions renders the version current when it
    was rendered.
  - The client opens the stream with the version it has. The server
    first sends an event for every subscribed topic changed since then,
    then streams live ones. Each event carries the new version.
  - A different boot ID means the process restarted, so every topic
    counts as changed.

  This closes the gap between rendering the page and subscribing, and
  it means a reconnect, a return from a hidden tab or a back/forward
  restore fetches only what actually changed. A reconnect with nothing
  changed makes **no** fragment request. That matters for sleep:
  automatic refetches would otherwise be ordinary requests that keep an
  instance awake.
- **One stream per page**, served by a handler the app mounts behind
  its own auth. Each event's data is a topic and a version, nothing
  else. Auth and the topic checks run when the stream opens; a revoked
  session loses the stream at the next reconnect, which the bounded
  lifetime below guarantees.
- **Bounded.** Open streams are capped per process; over the cap, the
  stream is refused and the page stays static, which is today's
  behaviour. Each connection has a bounded lifetime, a comment
  heartbeat on a quiet stream, and a write deadline re-armed per write
  (the serve.go recipe).
- **Shutdown ends streams before the drain waits on them.** The hub
  needs a hook that runs when shutdown *starts* (before `srv.Shutdown`
  waits, unlike `Options.Background`, which stops after it). It refuses
  new streams, tells open ones to end, and makes sure a write blocked on
  a slow client cannot outlast the drain; the write deadline must be
  well inside the 10 seconds. It works the same under `Serve` and under
  an externally hosted `Handler`.

### 3. The change stream: client

The shim opens **one** EventSource per page, for the union of the
topics its regions name, never one per region.

- **One owner for reconnecting.** The shim owns every retry. On any
  error it closes the EventSource (so the browser's own reconnect never
  runs alongside it) and opens a new one with backoff, passing the
  latest version it holds in the URL. EventSource hides the HTTP status,
  so the shim treats every failure the same way and relies on backoff
  and the cap; a refused topic check is a failure like any other.
- **Fetch on event.** On a topic event, every region naming that topic
  is fetched again.
- **Responses apply in order.** Each region tracks the fetches it has
  started. A response is applied only if no later fetch for that region
  has been started, by the stream or by a form. A form's request
  supersedes any stream fetch already in flight for the same regions.
  At most one fetch per region is in flight with one queued behind it.
- **Check before swapping.** Immediately before applying a response, the
  shim checks again that the region is still on the page and not being
  edited. A region being edited (it holds the focused field, or a form
  with unsent input) is not swapped by a stream event; it is marked
  stale and fetched when the editing ends.
- **Failed fetches are retried, then reported.** A fetch that fails
  while the stream is healthy is kept and retried with bounded backoff.
  If it is still failing after the paused grace, the page shows the
  same paused note as a dead stream (section 5).
- **Focus.** After a swap, if the focused element's `id` is in the new
  region, focus moves to its replacement. If it is gone (the row was
  approved away), focus moves to the region itself, which takes
  `tabindex="-1"` for the purpose. Focus never falls to `<body>`.
- **Hidden tabs and page lifecycle.** My call: after the tab has been
  hidden for a while, the stream closes, and it reopens when the tab is
  visible again. On `pagehide` the stream closes and in-flight fetches
  are abandoned at once. On `pageshow` from the back/forward cache, the
  stream reopens with the page's version, which fetches only what
  changed. An interrupted form submit is never replayed.
- **Prerender.** The stream opens on `prerenderingchange`, as polling
  does.

### 4. Forms that swap

A form opts in with one attribute (the builder names it). Without the
script it is an ordinary form. With it:

1. **Send what the browser would.** The shim cancels the submit and
   sends the same request with fetch: the form's method, action and
   encoding, the submitter's name and value, and the submitter's
   `formaction`/`formmethod`/`formenctype` overrides. It adds
   `Rastrillo-Fragment` naming the form's own region, any regions the
   form names as targets, and the notice region. A form the shim cannot
   send faithfully (a `target`, a cross-origin action, a GET form)
   submits natively, as today.
2. **Success that comes back here swaps.** The handler does what it
   does today and answers 303 to the page. Fetch follows the redirect
   as a GET and keeps the header, so the page answers with just those
   regions, including the flash the POST set. The success path needs no
   handler change when the handler already redirects back to the page
   the form is on.
3. **Success that goes elsewhere navigates.** If the redirect lands on
   another page, that page has none of the regions, so it answers 204
   and `Rastrillo-Location` without taking the flash (section 1), and
   the shim navigates there. The write happened; a full GET of the
   target is correct and shows the notice. A redirect to another origin
   fails the fetch; the shim treats it as an unknown outcome (step 5).
4. **Errors in place.** A validation failure re-renders the form as
   today (400 or 422). The same render helper returns only the requested
   regions, and the shim swaps them in and moves focus to the first
   field with an error. Any other response that does not carry the
   requested regions is shown the way the browser would have shown it:
   the shim does not guess.
5. **Unknown outcome is not failure.** A network error, or a 5xx with no
   regions in it, does not mean the write failed: the POST may have
   committed and only the redirected GET failed. The shim keeps
   everything that was typed, says it could not confirm the change, and
   fetches the form's regions again so the page shows the real state.
   It never resubmits by itself.
6. **Busy, without fighting `busy.js`.** While the request is out, the
   button shows busy and a second submit is refused, looking exactly as
   `busy.js` does today. `busy.js` currently releases a cancelled form,
   which would clear the shim's busy state mid-request, so the two need
   an explicit handover: `busy.js` leaves swapping forms alone, and the
   shim applies the same busy rule to them. Both orders of the two
   scripts' listeners are tested.

The write calls `Notify` as any write does, so every other open page
updates too. The submitting page gets the event as well; the ordering
rule makes that harmless.

**Announcements.** The notice region is a persistent `role="status"`
element, so a swapped-in notice is announced. Validation errors are
announced through the form's existing error summary. The paused note
uses the same status region.

### 5. Sleep and restarts

Today an open stream keeps a CARLOS instance awake: the activator only
sleeps an instance with nothing in flight
(`carlos/platform internal/activator/activator.go:519`). A dashboard
left open would keep Patrix running all night.

What Paul decided: the stream dying when the instance sleeps is fine,
and a refresh, a real request, is what wakes it.

CARLOS already has exactly that behaviour for sidecars: a request
marked `X-Carlos-Sidecar` earns no activity credit, gets a 503 while the
instance sleeps, and never wakes it (`activator.go:83-90`, checked
before the wake path at `:2808`). We do not borrow a sidecar's header
from a browser. **This design depends on a request to CARLOS** to offer
the same rule for live streams.

- **The marker must be on the request.** The activator decides whether
  to wake as the request arrives, before any response exists, and
  EventSource cannot send custom headers. So the marker is in the URL
  (a path prefix or a query parameter). The stream's URL shape becomes
  a contract between Rastrillo and the platform, agreed in the CARLOS
  request before the handler and the shim are written.
- **Only the stream needs it.** Thanks to versions (section 2), the
  page makes fragment requests only when something actually changed,
  and a change is itself activity. Reconnects with nothing new make no
  ordinary requests.
- **Asleep looks like a failed stream.** While the instance sleeps, the
  stream request gets a 503 and never wakes it. The shim keeps retrying
  with backoff. When someone else's request wakes the instance, the
  next retry succeeds; the new boot ID makes every region fetch once,
  and the page is live again. A refresh wakes it straight away.
- **The paused note.** If the stream has been down longer than a short
  grace (so a deploy's restart doesn't flash it), the page says live
  updates have paused and a refresh resumes them. The wording goes
  through copy review, and it is a translated string.
- **Proven end to end.** Adoption waits on an integration test against
  the activator: a page with an open stream and no other traffic lets
  the instance sleep after the idle window, and the stream's retries do
  not wake it.

## Proving it

- `examples/notes`, with `notes` keyed by owner:
  - The list updates when the same person adds a note in another tab.
  - Delete from the list is a swapping form; its handler already
    redirects to the list.
  - An inline edit on the list would need its handler to redirect back
    to the list and its errors to render in the list's region. The plan
    either makes that change or leaves inline edit out; it does not
    claim the existing handler works unchanged.
- Tests for each rule above, including: an unmarked region name never
  renders; a stream for an undeclared topic, or for another owner's
  key, is refused; a stream-triggered fetch never takes the flash;
  shutdown with streams open, including one blocked on a slow client,
  finishes inside the drain; `Notify` with a stuck subscriber does not
  block; a reconnect with nothing changed makes no fragment request; a
  stale response does not overwrite a newer one; a swap never lands in
  a field being edited; focus never falls to `<body>`; an unknown
  outcome keeps typed input and does not resubmit; `busy.js` and the
  shim in both listener orders; a prerendered page opens no stream;
  back/forward restore catches up; with scripts off the example's flows
  still pass.
- The gates already in place hold: axe, translation parity (the paused
  note is new copy), the shim's size and inertness tests, `-race` on
  the new package.
- `SKILL.md` gets the vocabulary, within its byte budget.

## Decided by Paul

- Not LiveView, not a Next.js-style runtime. Extend what jobs do.
- Both pieces: the change stream and forms that swap.
- Patrix's dashboard is the target, Tito Go only an example.
- Sleep: the stream dying when the instance sleeps is fine; a refresh
  wakes it.

## My calls (check these)

- One header (`Rastrillo-Fragment` with region names) on the page's own
  URL, instead of a route per region.
- A request for regions the page doesn't have answers 204 and
  `Rastrillo-Location`, before the flash is taken.
- Topics can carry a key, and every topic is checked per viewer.
- Versions (a boot ID and a sequence number) instead of fetching every
  region on every reconnect.
- Swapping forms rely on the redirect being followed, so the success
  path needs no handler change when the handler already redirects back.
- An unknown outcome refetches and asks the person, never resubmits.
- A hidden tab closes its stream after a while.
- A region being edited is not swapped under the person.
- No timer fallback for live regions: without a stream they are static,
  as today.
- The paused note is shown, not silent.
- Not migrating jobs onto the new stream in this branch.

## Left to the builder

- Package, attribute and API names.
- How a template marks a region, and how a render helper fits both
  `ui.Pages` and a hand-rolled template map.
- Bounds and timings: stream cap, heartbeat, lifetime, write deadline
  (inside the drain), backoff, hidden and paused graces.
- How the hub bounds its per-topic memory for keyed topics (treating an
  evicted topic as changed is the safe answer).
- How a time-driven change (a scheduled post going live with nothing
  written) is announced. A timer alone is not enough: `background`
  discards timers on shutdown. Whatever arms it must be rebuilt from
  the stored publication times at startup and after any schedule
  change, and a process that wakes after a deadline has a new boot ID,
  so its pages refetch anyway.
