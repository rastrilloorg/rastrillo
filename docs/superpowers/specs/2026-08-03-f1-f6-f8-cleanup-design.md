# F1 + F6 + F8: friction-log cleanup slice — design

**Date:** 2026-08-03
**Status:** approved
**Closes:** friction-log F1 (no status slot on `list-row-action`), F6
(`GET /` is a catch-all), F8 (static serving is cwd-relative) — all
recorded in `examples/blog/README.md`.

## Why

Three small, independent warts the blog's friction log recorded while
being built, each now cheap to fix: the admin list filters by status but
each row still carries status as prose; every app needs a hand-written
guard to stop the homepage answering `/nonsense` with a 200; and a
deployed binary loses its stylesheets because `http.Dir("static")`
resolves against whatever directory the process happens to start in —
and the platform ships a single binary with no static/ directory beside
it at all.

## F1 — status slot on `list-row-action`

Two new optional keys, flat like the row's existing key set:

- `StatusTone` — passed to `status-pill` as `Tone` ("neutral" default
  semantics live in the pill, unchanged)
- `StatusLabel` — the pill's visible text; the pill renders only when
  this is set (mirroring how `ActionHref` gates the action pill)

The pill renders in the row's right-hand group, **before** the action
pill, by calling the existing partial:

```
{{if .StatusLabel}}{{template "status-pill" dict "Tone" .StatusTone "Label" .StatusLabel}}{{end}}
```

Positioning: the row's primary link stretches an `::after` overlay
across the row; anything clickable or readable on the right sits above
it. The status pill gets the same treatment `.rst-row__action` already
has (position/z-index via a small tokens.css addition — a
`.rst-row .rst-status` rule or shared class, decided at implementation
against the existing overlay CSS). Any tokens.css change is mirrored
into the blog's vendored `static/tokens.css` (byte-equality is
test-enforced).

**Blog adoption:** `AdminRows` sets the pair — Draft → `neutral`,
Published → `positive` (the same mapping `EditForm` already uses) — and
the `Sub` line drops the status word, keeping the date:
`DraftLine` becomes `"Edited " + date` (its "status rides in Sub —
friction F1" comment goes away); `PublishedLine` stays
`"Published " + date` (there it reads as the event's date, not a status
duplicate — and the pill still carries the state). Resolving raw
status to Tone/Label stays product logic in the app, per status-pill's
own contract.

## F6 — root index stops being a catch-all

`routeFor` (internal/generate/generate.go) emits `GET /{$}` when the
joined path is bare `/` — Go 1.22+'s exact-match anchor; the module is
on Go 1.25. Only the root index changes: nested indexes
(`/admin/posts`) are already exact patterns. Unmatched GETs now fall
through to the mux's own NotFound.

**Blog adoption:** the hand-written `r.URL.Path != "/"` guard and its
F6 comment come out of `actions/index.GET.go`; `gen/` regenerated. The
existing `TestUnmatchedGETsAre404NotTheHomepage` (blogtest) is the
proof the behavior survives the guard's removal; a generate test pins
the `/{$}` mapping itself.

`README.md`'s route-table prose (if it shows `index → GET /`) is
updated to the new pattern.

## F8 — static embedded in the binary

The scaffolded app and the blog embed `static/` and serve it from the
binary:

- `rastrillo new` emits, in the scaffolded `main.go` (or a sibling
  `embed.go` if main.go's shape resists a directive — implementation's
  call, matching the scaffold's current file layout):

  ```go
  //go:embed static
  var staticFS embed.FS

  mux.Handle("GET /static/", http.FileServerFS(staticFS))
  ```

  Note `http.FileServerFS(staticFS)` serves paths as `/static/...`
  against an FS rooted at the module root — the embedded FS's paths
  already carry the `static/` prefix, so the `http.StripPrefix`
  wrapper goes away (verified at implementation; if the path algebra
  demands it, `fs.Sub` + `StripPrefix` is the fallback, still
  embedded).

- `examples/blog/cmd/blog/main.go` gets the same change.
- **`rastrillo dev` watches `static/`** so editing tokens.css/blog.css
  still round-trips in the dev loop (embedded assets are stale until
  rebuild; dev's rebuild-on-change closes that).

The scaffold's README/comment text stops warning about run-from-app-root.

## Testing

- ui: row test — pill renders only when `StatusLabel` set, correct
  Tone pass-through, sits in the right group, absent by default
  (existing minimal-fixture test keeps passing unchanged).
- generate: `routeFor`-level or golden test asserting root index →
  `GET /{$}`, nested index unchanged.
- blogtest: `TestUnmatchedGETsAre404NotTheHomepage` unchanged and
  green after the guard's removal; admin-list screens show the pill
  (extend the stock-partials inventory if the screens tests track it);
  a smoke assertion that `GET /static/tokens.css` serves 200 from the
  embedded FS.
- `rastrillo new` scaffold test updated for the embed + FileServerFS
  shape; dev's watch-list test (if one exists) gains `static/`.
- Friction log: F1, F6, F8 gain *Fixed:* postscripts in the F9/F10
  voice.

## Out of scope

F5 (shared code in actions/ — documented as by-design), F7 (auth —
needs its own design cycle), the manifest system (next major slice),
and any change to `Serve`'s API — this slice touches the generator,
the scaffold, one partial, and the blog.
