# Live pages

A live page updates parts of itself when data changes, and works
unchanged with scripts off. Package `live` is the server half
(`amadan.net/rastrillo/rastrillo/live`), `ui/live.js` (`ui.LiveJS()`) the
browser half. The stream carries topic names and versions, **never
data**: a page that hears "notes/7 moved" fetches its own region again,
from its own URL, as its own viewer, through the same handler and
checks as the full page. Worked reference: `examples/notes`, in the
repository, **not** in the
published module; its files are in `examples/notes/internal/notes/`
(`app.go`, `render.go` `renderStatus`, `templates/index.html`,
`layout.html`).

## Vocabulary

- **Topic**: `name` or `name/key`. Name `^[a-z][a-z0-9-]{0,31}$`, key
  `^[A-Za-z0-9_-]{1,64}$`. Declared up front in `live.Config.Topics`,
  each with `Allow func(r, key) bool`, run per viewer when a stream
  opens. **Key a topic by owner** (`notes/<uid>`): an unkeyed topic
  makes every viewer's page refetch on anyone's write, which tells them
  someone wrote.
- **Version**: a per-topic counter. No shared counter leaves the server.
- **Region**: an element with an `id` that the page can fetch again by
  itself. Name `^[a-z][a-z0-9-]{0,63}$`. It exists only if a template
  `region:<name>` is defined; the `region:` prefix is the whole rule that
  stops a request from executing `layout` or a partial.
- **Notice**: reserved region `notice`, the `role="status"` flash
  element, always on the page and empty when there is no flash.
- **Fragment request**: the same URL with `Rastrillo-Fragment: <region>
  <region>`; the answer is those regions' elements, with
  `Rastrillo-Regions: <names rendered>`.

## Wire formats an app touches

Region element, attributes from `View.Attrs` (never hand-written):

```html
<div id="notes-list" {{.Live.Attrs .Content.Topic}}>...</div>
```

renders `data-live="notes/7@3" data-live-boot="<12 hex>"`. A topic
undeclared or malformed is left out, and logged once per name per hub;
none left, no attributes. Head, once, in the layout (the two strings
are the translated `rastrillo.ui.live.paused` and `.unconfirmed`):

```html
{{.Live.Head (T $L "rastrillo.ui.live.paused") (T $L "rastrillo.ui.live.unconfirmed")}}
```

That meta, `rastrillo-live`, names the stream path; **no meta, no
stream**. The page data carries `Live live.View` = `hub.View(r)`. A
scaffolded app already has `static/live.js` and links it in every
layout after `busy.js`; an older app runs `rastrillo doctor --fix` and
adds `<script defer src="/static/live.js">` itself. Serving `ui.LiveJS()`
by hand at that path is for an app without the scaffold, as notes does
in `app.go`. The layout holds `{{template "region:notice" .}}` above
the content; notes defines it in `layout.html`.

A form swaps in place with `data-live-form` (value: extra region names,
may be empty). It sends the form as the browser would, plus
`Rastrillo-Page`, and swaps its closest `[data-live]` region, the listed
ones and the notice. A GET form, one with a `target`, a cross-origin
action or `enctype="text/plain"` is left to the browser. Its handler
needs no change: it writes, calls Notify, flashes, redirects 303 as
today, and the redirected GET is answered as a fragment. A redirect to
another page is answered 204 (plus `Rastrillo-Location`), and the
browser navigates to the URL the 204 came from, which keeps a locale
prefix; the header is only a fallback. That 204 is live's own answer,
not a way for an app to choose a destination: to send people
somewhere, redirect as before. A 400 or 422 swaps and focuses the first
`[aria-invalid="true"]`. A 5xx, no answer, or a 2xx with an **empty**
`Rastrillo-Regions` is an unknown outcome: the notice says
"unconfirmed", the regions refetch, and **nothing is resent**, so a
non-idempotent handler is still yours to make safe. No header at all
(any status below 500) replaces the page: `text/html` as a document,
anything else (`http.Error`) as plain text, never markup. A bare 204
stays put.
Only the form's own region swaps over unsent input; a draft in another
target waits. It sends a tick after the submit, so any handler that
calls `preventDefault` or sets `returnValue = false`, a later `window`
listener too, cancels it. **Not** a `window.onsubmit` set after live.js
loads that returns false (no script can see it): use `addEventListener`,
or set `onsubmit` on the form.

## The render order

Answer a page through **`pages.Render(w, r, status, page, data)`**
(`live.Render(w, r, status, t, "layout", data)` for your own template
map); notes `renderStatus` is the model. `status` 0 is 200; a fragment
answer keeps it. `data(takeFlash bool) (any, error)` builds the page
data (with `Live: hub.View(r)`) and calls `flash.Take` only when
`takeFlash` is true: a stream refetch that took it would eat a message
it never shows. Render calls `data` once, and not at all when the
answer is a navigation, so the flash waits for the page navigated to.
An error from `data`, the localizer check or a template comes back
with nothing written: write your 500. A write error can follow a
committed response.

The order, for the building blocks: `Elsewhere` (a GET/HEAD whose
`Rastrillo-Page` differs from `r.URL.Path`, a form's redirect to
another page: 204, before the flash); `NoStore` (`no-store` and `Vary:
Rastrillo-Fragment`); `Wanted`, false (whole page) for no header, more
than 32 names or a non-region name such as the jobs poll's `1`; then
`Has`/`HasRegions`, which **ignore `notice`** (lacking, a GET/HEAD
navigates and any other method gets the whole page with its status);
the flash only if `!frag || TakesFlash`; then `WriteRegions` with the
status sent with the first byte, or the buffered whole page. Never
hand the ResponseWriter to `ExecuteRegions`: `Rastrillo-Regions` is
lost. With your own template map: `live.WriteRegions`, or
`live.ExecuteRegions` + `live.Respond`.

A fragment request reaches the same handler, so the handler loads
whatever the region shows even when the region is all that is rendered.

## Writing and mounting

- **`live.New(live.Config{Path, Topics})`** once at boot; `Path` is
  required. Defaults: `MaxStreams` 1000, `Lifetime` 5m (a revoked
  session loses its stream at the next reconnect), `Heartbeat` 15s,
  `WriteDeadline` 5s.
- **`hub.Middleware`**: the first `r.Use` on the router. It records the
  sequence at request start, so a write landing mid-request reads as
  newer and costs one extra fetch, never a missed change.
- **`hub.Notify("notes/7", ...)` after the write commits**: notified
  before, a refetch can read the old rows and settle on them. Never
  blocks; undeclared or malformed topics are logged and skipped; a nil
  hub does nothing. Memory grows with the distinct keyed topics notified
  since boot: none is evicted.
- **`hub.NotifyAt(at, "posts/7")`** for a change no write makes (a
  post going live at its time): call it at boot for each stored future
  time and when a schedule changes. A past time notifies at once. No
  cancel: a stale time costs one fetch; a restart's new boot ID
  refetches every page anyway. At most 10,000 (time, topic) pairs wait;
  past that the latest-due pair is dropped; the first drop is logged.
- **`r.Handle(path, hub.Handler())` after `sess.Middleware` or
  `sess.Require`**: `Allow` sees the session whichever of them stored
  in the context, so mount the handler after one (notes uses
  `Require`). GET only; one bad topic refuses the whole stream (403, 400
  for a malformed one).
- **`opts.Live = hub`** before `rastrillo.Serve`, or open streams hold
  the 10-second drain to its end. A host embedding `Handler` under its
  own server calls `hub.Close()` before its own `Shutdown`.

## What live.js does, so you do not

It owns reconnecting (backoff to a minute), one fetch per region at a
time, and a region being edited (focused field, or a field changed from
its default) is **not swapped**: it is marked `data-live-stale` and
fetched when editing ends. Focus survives a swap. After failed fetches
or ten seconds of a down stream an empty notice shows the paused note.
Hidden for a minute, the stream closes. It does not move data, run templates,
or touch `rastrillo.js`.

## CARLOS: hibernating routes

An open stream would keep a CARLOS instance awake all night, so **live
streams stay off on routes that hibernate** until carlos/platform#432
ships (the edge holds the stream and lets the instance sleep). Nothing
detects the route's kind yet: the app turns streams on explicitly, and
only on an always-on route. For a hibernating one build no hub: a nil
`*live.Hub` is safe everywhere but `Handler` (`View` is the zero View,
`Head` and `Attrs` render nothing, `Notify` and `Middleware` do
nothing; a served `Handler` panics), so the
templates need no `if` and the pages are static as before. Do not
mount `Handler`, and leave `Options.Live` nil.

Spec: `docs/superpowers/specs/2026-10-26-live-fragments-design.md`.
