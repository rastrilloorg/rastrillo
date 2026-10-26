# Live pages: a change stream and forms that swap in place — design

**Date:** 2026-10-26
**Status:** Draft, for Paul's review
**Branch:** `live-fragments`
**Written by:** Claude Opus 5.5 (claude-opus-5-5), default reasoning

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
- A moderator clicks Approve on a comment. That row changes in place,
  the "Comment approved" notice appears, focus stays where it was. Any
  other open dashboard gets the change too.
- A form with a mistake shows its errors in place.
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
   awake forever (see "Sleep and restarts").

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
- A replay log. The stream is a hint; the server is the truth.

## Anti-goals

Any of these makes it a failure even if it technically works:

- A page that needs the script to work at all.
- Data sent over the stream. It carries topic names only, so it can
  never leak what a viewer may not see.
- Typed input lost because a request failed.
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
  (`eventlog`, `jobs`, `background` and `db` have none).
- `serve.go`: no Read/WriteTimeout; streams re-arm a write deadline per
  write (the "# Timeouts" recipe). `srv.Shutdown` gets 10 seconds, and
  Go does not cancel in-flight request contexts on Shutdown, so an open
  stream holds the drain until it gives up.
- `ui/busy.js`: a script that cancels a submit itself takes over the
  feedback for it; a form removed mid-hold drops its held submit.
- `csrf`: origin-based, so a same-origin fetch POST passes unchanged.
- `flash`: a one-shot cookie, taken (and cleared) by the next render.
- Fragments: no helper. `examples/notes` builds a second, layout-less
  `ui.Pages` with its own Entry to render one. Patrix does not use
  `ui.Pages`; it runs `ExecuteTemplate(w, "layout", …)` on its own map,
  and its dashboard body is already a named template
  (`dashboard-body`).

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
request for the page that carries `Rastrillo-Fragment: dashboard-stats`
(a list, space-separated, is allowed) gets back only those regions,
rendered by the same handler, with the same data and the same
permission checks, each as its own top-level element carrying its `id`.
The shim swaps each one in by `id`.

This puts the existing reserved header to work instead of adding a
route per region. One URL, one handler, one permission check. Responses
that vary on it say `Vary: Rastrillo-Fragment`.

Constraints:

- The server only ever renders a name the page itself marked as a
  region. Any other name is not an error page and not a full page; the
  builder picks the answer (an empty 204 is the obvious one), but it
  must never execute an arbitrary template by name.
- It must fit both `ui.Pages` apps and an app like Patrix that keeps
  its own template map. Whatever the helper is, Patrix's `render`
  should need a few lines, not a rewrite.
- The page's notice (`rst-notice`, the flash) is a region on every
  layout that has one, so a swap can bring the notice with it.

### 2. The change stream

**Server.** A new package (name is the builder's call; `live` is the
working name) holding an in-memory hub:

- `Notify(topics ...string)`: called by the app after the write
  commits. It never blocks the caller. A subscriber that cannot keep up
  is dropped, not waited for.
- A handler the app mounts, behind its own auth, that serves one
  `text/event-stream` per page. The client names its topics in the
  request. Each event's data is a topic name and nothing else.
- Topics are names the app declares up front. A stream asking for an
  undeclared name is refused, so a client cannot grow the hub's maps.
- Open streams are bounded per process. Over the bound, the stream is
  refused and the page simply stays static, which is today's
  behaviour.
- The handler follows the serve.go streaming recipe: a write deadline
  re-armed per write, a comment heartbeat on a quiet stream, a bounded
  lifetime per connection.
- Shutdown closes every stream deliberately, before `srv.Shutdown`
  starts waiting, the way `Options.Background` is stopped in order.
  An open stream must never eat the 10-second drain.

**Client.** The shim opens **one** EventSource per page, for the union
of the topics its regions name. Never one per region: browsers on
HTTP/1.1 allow six connections per origin, so six tabs of per-region
streams would starve the page's own requests.

- On a topic event, every region naming that topic is fetched again.
  The fetch coalesces: one in flight per region, and at most one more
  queued behind it (the busy/queued logic `poll` already has).
- When the stream reconnects, and when a hidden tab becomes visible,
  every live region is fetched once, because events may have been
  missed.
- My call: when the tab has been hidden for a while (the builder picks
  how long), the stream closes; it reopens on visible, with the fetch
  above. A tab nobody is looking at holds nothing open.
- A swap keeps keyboard focus: if the focused element has an `id` or a
  form `name` present in the new region, focus moves to its
  replacement.
- A region that is part-way through being edited (it contains the
  focused field, or a form with unsent input) is not swapped by a
  stream event. It is marked stale and fetched when that ends.
- Stream starts wait for `prerenderingchange`, as polling does.

### 3. Forms that swap

A form opts in, with one attribute on the form (name is the builder's
call). Without the script it is an ordinary form. With it:

1. The shim cancels the submit and sends the same POST with fetch,
   adding `Rastrillo-Fragment` naming the form's enclosing region, the
   regions the form names as targets, and the notice region.
2. The handler does what it does today. On success it answers 303 to
   the page. Fetch follows the redirect as a GET and keeps the header,
   so the GET of the page answers with just those regions, including
   the flash the POST set. No handler change is needed for the success
   path.
3. On a validation error the handler re-renders the form, as today.
   Because the request asks for the form's region, the same render
   helper returns only that region, with its errors, and the shim swaps
   it in.
4. If the redirect lands on a different page (a delete that goes back
   to the list), the shim navigates there. The write already happened,
   so a full GET of the target is correct.
5. On a network error or a 5xx, nothing is swapped and the form keeps
   everything that was typed. The shim shows that it failed, and the
   person can submit again.
6. While the request is out, the button shows busy and a second submit
   is refused, as `busy.js` does today. The shim takes that over for
   these forms (busy.js's own rule for cancelled submits), and must look
   the same.

The write calls `Notify` as any write does, so every other open page
updates too. The submitting page gets the event as well; the coalesced
fetch makes that harmless.

### 4. Sleep and restarts

Today an open stream keeps a CARLOS instance awake: the activator only
sleeps an instance with nothing in flight
(`carlos/platform internal/activator/activator.go:519`), and the
browser reconnects a dropped stream by itself. A dashboard left open
would keep Patrix running all night.

What Paul decided: the stream dying when the instance sleeps is fine,
and a refresh, a real request, is what wakes it.

CARLOS already has exactly that behaviour for sidecars: a request
marked `X-Carlos-Sidecar` earns no activity credit, gets a 503 while the
instance sleeps, and never wakes it (`activator.go:83-90`). We do not
borrow a sidecar's header from a browser. **This design depends on a
request to CARLOS** to offer the same rule for live streams. EventSource
cannot send custom headers, so the marker probably belongs on the
response (or the path); that is the platform's choice.

Client behaviour, which is the same whatever CARLOS decides:

- When the stream ends, the shim retries with backoff. A retry is
  activity-invisible once CARLOS supports it, so it can never wake a
  sleeping instance. While asleep it gets a 503; when someone else's
  request wakes the instance, the next retry succeeds, every region is
  fetched, and the page is live again.
- If the stream has been down for longer than a short grace (so a
  deploy's restart does not flash it), the page shows a small note that
  live updates have paused and a refresh resumes them. The wording goes
  through copy review, and it is a translated string.
- Shutdown sends nothing special. A closed stream is a closed stream;
  the same retry covers a deploy, a crash and a sleep.

## Proving it

- `examples/notes`: the list updates when a note is added in another
  tab, and delete-from-list and an inline edit use swapping forms.
- Tests for each claim above, including: a region request never
  executes an unmarked template; a stream for an undeclared topic is
  refused; shutdown with streams open finishes well inside the drain;
  `Notify` with a stuck subscriber does not block; the shim opens one
  EventSource for many regions; a swap keeps focus; a failed post
  keeps typed input; a prerendered page opens no stream; with scripts
  off the example's flows still pass.
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
- Swapping forms rely on the redirect being followed, so the success
  path needs no handler change.
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
- The answer to an unmarked region name.
- Bounds and timings: stream cap, heartbeat, lifetime, backoff, hidden
  and paused graces, server-side coalescing if any.
- How a time-driven change (a scheduled post going live with nothing
  written) is announced. `background.After` calling `Notify` at the
  scheduled time is the obvious answer.
