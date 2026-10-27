# Live pages

A live page updates parts of itself when data changes, and works
unchanged with scripts off. Package `live` is the server half
(`amadan.net/rastrillo/rastrillo/live`), `ui/live.js` (`ui.LiveJS()`) the
browser half. The stream carries topic names and versions, **never
data**: a page that hears "notes/7 moved" fetches its own region again,
from its own URL, as its own viewer, through the same handler and
checks as the full page. Worked reference: `examples/notes`, in the
repository, **not** in the
published module (`app.go`, `render.go` `renderStatus`,
`templates/index.html`, `layout.html`).

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
undeclared or malformed is logged and left out; none left, no
attributes. Head, once, in the layout (the two strings are the
translated `rastrillo.ui.live.paused` and `.unconfirmed`):

```html
{{.Live.Head (T $L "rastrillo.ui.live.paused") (T $L "rastrillo.ui.live.unconfirmed")}}
```

That meta, `rastrillo-live`, names the stream path; **no meta, no
stream**. The page data carries `Live live.View` = `hub.View(r)`. Load
the script with `<script defer src="/static/live.js">` and serve
`ui.LiveJS()` there (notes `app.go`). The layout holds
`{{template "region:notice" .}}` above the content; notes defines it in
`layout.html`.

A form swaps in place with `data-live-form` (value: extra region names,
may be empty). It sends the form as the browser would, plus
`Rastrillo-Page`, and swaps its closest `[data-live]` region, the listed
ones and the notice. Its handler needs no change: it writes, calls
Notify, flashes, redirects 303 as today, and the redirected GET is
answered as a fragment. A redirect to another page answers 204 plus
`Rastrillo-Location`, and the browser navigates. A 400 or 422 swaps and
focuses the first `[aria-invalid="true"]`. A 5xx or no answer is an
unknown outcome: the notice says "unconfirmed", the regions refetch,
and **nothing is resent**, so a non-idempotent handler is still yours to
make safe.

## The render order

A handler that can answer a fragment follows live.go's package doc, in
this order (notes `renderStatus` is the model):

1. `live.Elsewhere(w, r)`: a GET/HEAD carrying `Rastrillo-Page` that
   differs from `r.URL.Path` is a form's redirect to another page.
   Answers 204 + `Rastrillo-Location`; return. **Before the flash is
   taken**, so the page navigated to still shows it.
2. `live.NoStore(w)`: `Cache-Control: no-store` and `Vary:
   Rastrillo-Fragment` on full pages and fragments both, one URL
   answering two shapes.
3. `names, frag := live.Wanted(r)`. `frag` false (no header, or any name
   not a region name, e.g. the jobs poll's `1`) means render the full
   page.
4. If `frag` and `!pages.HasRegions(page, names)` (`live.Has` for your
   own template set): `live.Respond(w, r, nil, nil)`, a navigate answer;
   return. Still before the flash.
5. `live.TakesFlash(r)`: true for a full page or when `names` has
   `notice`. Call `flash.Take` only then; a stream refetch that took it
   would eat a message it never shows.
6. Build page data (with `Live: hub.View(r)`), then `pages.WriteRegions(w,
   r, page, names, data)` for a fragment, `pages.Execute` otherwise.
   Give `WriteRegions` the ResponseWriter, not the output of
   `ExecuteRegions`: it renders to a buffer, sets `Rastrillo-Regions`
   and only then writes, since headers sent with the first body byte
   would lose it. Nothing is sent on error (write your 500). For a
   status such as 422, wrap the writer so the status goes with the
   first byte (notes `statusWriter`). Own template map: `live.WriteRegions`, or
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
  hub does nothing.
- **`r.Handle(path, hub.Handler())` inside `sess.Require`**: `Allow`
  reads the session, so outside it every stream is refused. GET only;
  one bad topic refuses the whole stream (403, 400 for a malformed one).
- **`opts.Live = hub`** before `rastrillo.Serve`, or open streams hold
  the 10-second drain to its end. A host embedding `Handler` under its
  own server calls `hub.Close()` before its own `Shutdown`.

## What live.js does, so you do not

It owns reconnecting (backoff to a minute), one fetch per region at a
time, and a region being edited (focused field, or a field changed from
its default) is **not swapped**: it is marked `data-live-stale` and
fetched when editing ends. Focus survives a swap. After failed fetches
or ten seconds of a down stream the notice shows the paused note. Hidden
for a minute, the stream closes. It does not move data, run templates,
or touch `rastrillo.js`.

## CARLOS: hibernating routes

An open stream would keep a CARLOS instance awake all night, so **live
streams stay off on routes that hibernate** until carlos/platform#432
ships (the edge holds the stream and lets the instance sleep). Nothing
detects the route's kind yet: the app turns streams on explicitly, and
only on an always-on route. For a hibernating one build no hub: a nil
`*live.Hub` is safe everywhere (`View` is the zero View, `Head` and
`Attrs` render nothing, `Notify` and `Middleware` do nothing), so the
templates need no `if` and the pages are static as before. Do not
mount `Handler`, and leave `Options.Live` nil.

Spec: `docs/superpowers/specs/2026-10-26-live-fragments-design.md`.
