# Fingerprinted assets and a scaffolded test harness

**Date:** 2026-08-11
**Status:** Draft, awaiting review
**Extends:** the CARLOS framework design
([`carlosframework/platform` spec library](https://github.com/carlosframework/platform/blob/main/docs/superpowers/specs/2026-08-01-carlos-framework-design.md)),
§4 (filesystem routing / generate), §5 (Serve), and the scaffold shipped
by `rastrillo new`.

## The story

Out of the box, a rastrillo app should get two things it currently
doesn't:

1. **Assets that are long-cached but update on reload.** Today the
   scaffold serves `static/` through a bare `http.FileServer` with no
   cache headers. Browsers cache heuristically: sometimes a changed
   stylesheet doesn't show up without a hard refresh, and nothing is ever
   cached as aggressively as it safely could be. The fix is the standard
   one — content-hashed asset URLs (`/static/tokens.d1e8a70b5ccab1dc.css`)
   served `Cache-Control: public, max-age=31536000, immutable`, with the
   HTML always referencing the current hash. A changed file gets a new
   URL, so a plain reload picks it up; an unchanged file is never
   re-fetched.

2. **A working test harness, with tests already passing.** Today
   `rastrillo new` scaffolds zero tests; the TDD loop the blog example
   demonstrates (`examples/blog/internal/blogtest`) has to be
   reinvented per app. The scaffold should deliver that harness on day
   one — the real generated router, request helpers, and example tests
   that pass immediately — so the first feature an app grows starts from
   "copy the failing test next to the passing ones", not from "figure
   out how to test this at all".

The two meet in the scaffold: the example tests assert the fingerprinting
behavior, so the asset story is itself delivered test-first.

**Platform synergy** (motivation, not a dependency): the platform's edge
cache stores any response with a cacheable `Cache-Control`
(edge-cache-before-wake design). Fingerprinted assets marked `immutable`
are therefore served from the edge without waking a hibernating app —
today every asset request wakes it.

## Part A — asset fingerprinting: `rastrillo.Assets`

### API (root package, matching the flat surface of Serve/Run/Icon)

```go
// NewAssets wraps a file tree — os.DirFS("static") in the scaffold, an
// embed.FS if the app prefers — in a content-hash registry.
func NewAssets(fsys fs.FS) *Assets

// Path maps a file name to its currently-hashed URL path element:
// Path("tokens.css") → "tokens.d1e8a70b5ccab1dc.css". Names may include
// subdirectories; the hash is inserted into the basename. A missing
// file returns the name unchanged (the 404 then surfaces at request
// time, visibly, instead of a panic at render time).
func (a *Assets) Path(name string) string

// Handler serves the tree. The app mounts it exactly where the old
// FileServer sat:
//
//	mux.Handle("GET /static/", http.StripPrefix("/static/", assets.Handler()))
func (a *Assets) Handler() http.Handler
```

No `Options` field, no framework-owned route: per the existing rule, the
app serves its own static files — the framework only supplies the
handler the scaffold wires up.

### Hashing and freshness

- SHA-256 of the file content, truncated to 16 hex characters, inserted
  before the extension: `tokens.css` → `tokens.<16hex>.css`.
- Hashes are cached per file, keyed by (mtime, size) from a `Stat` on
  every lookup. A stat is ~1µs; in exchange, an edited file re-hashes on
  the next `Path` call or request with **no restart and no watcher** —
  which matters because `rastrillo dev` deliberately does not watch
  `static/` (nothing needs regenerating or rebuilding for an asset
  edit). Save the CSS, reload the page: the HTML links the new hash, the
  browser fetches fresh. That is the whole "long cache, updates on
  reload" contract.
- An `fs.FS` without useful mtimes (embed.FS reports zero times) simply
  hashes once and caches forever — correct, since embedded content can't
  change without a rebuild-and-restart.

### Serving rules

For a request for `name.<16 hex>.ext` under the handler:

1. If a file literally named that exists, serve it plainly
   (`Cache-Control: no-cache`) — real names win over hash-stripping.
2. Otherwise strip the hash and look up `name.ext`. Hash matches the
   current content → serve with
   `Cache-Control: public, max-age=31536000, immutable`.
3. Hash doesn't match (a stale page is asking for an old version) →
   serve the **current** content with `Cache-Control: no-cache`. A
   slightly-stale stylesheet on a stale page beats a naked 404.

A bare `name.ext` (no hash) serves with `no-cache` — deep links and
hand-written URLs keep working, they just don't get the long cache.
Missing files 404. `http.ServeContent` does the byte-serving
(Content-Type, ranges); no ETag — the URL is the validator.

### Scaffold wiring

- `Ctx` gains an `Assets *Assets` field (additive; nil for apps that
  don't use it, same contract as `DB`).
- `mainTemplate` replaces the bare FileServer:

  ```go
  assets := rastrillo.NewAssets(os.DirFS("static"))
  ctx := &rastrillo.Ctx{Logger: logger, Assets: assets}
  // ...
  mux.Handle("GET /static/", http.StripPrefix("/static/", assets.Handler()))
  ```

- `actionTemplate` (index.GET.go) grows up from `fmt.Fprintln` plain
  text to a minimal HTML page whose stylesheet href is
  `"/static/" + ctx.Assets.Path("tokens.css")` — `Path` returns only
  the hashed name element, because `Assets` never knows where the app
  mounted it; the caller owns the prefix, same as the mount line in
  `main.go`. The scaffold actually
  demonstrates the feature and ships a styled page. The template stays
  small (a `html/template` literal in the action; no new files, no
  ui-partials dependency).

Apps that render through `html/template` trees register the same method
as a func — `template.FuncMap{"asset": assets.Path}` — the blog example
gets updated to do exactly that, as the reference for real apps.

### Approaches considered

- **Runtime hashing (chosen).** No build step, single static binary
  preserved, dev freshness for free via stat-checks.
- **Generate-time manifest** (`rastrillo generate` writes hashes into
  `gen/`): rejected — `dev` doesn't watch `static/`, so edits would
  serve stale hashes until an unrelated regenerate; more moving parts
  for no gain at this scale.
- **Query-string versioning** (`?v=<hash>`): rejected — intermediary
  caches treat query strings inconsistently, and path-based names are
  the established convention the edge cache is guaranteed to key on.

## Part B — tests out of the box

### What `rastrillo new` scaffolds

A new app gains `internal/<name>test/` — the blog's `blogtest` pattern,
delivered as app-owned files (like `tokens.css`: written once, never
touched by `new`/`generate` again, edit or delete freely):

- **`harness_test.go`** — `newApp(t)` builds the app exactly as
  `cmd/<name>/main.go` does: the real `gen.Router`, the same `Ctx`
  wiring including `Assets`, plus `get`/`post` request helpers over
  `httptest`. The starter app has no database; a comment marks the seam
  (open a temp-file SQLite via `rastrillo.OpenDB` — a real file, not
  `:memory:`, because `SetMaxOpenConns(1)`+WAL is the configuration
  under test — and put it on the Ctx), matching what the blog harness
  does for real.
- **`index_test.go`** — example tests that pass on a fresh scaffold and
  double as executable documentation of both stories:
  1. `GET /` is 200 and contains the scaffolded page's heading.
  2. The index HTML links a *hashed* stylesheet path.
  3. Fetching that hashed path returns 200,
     `Cache-Control: public, max-age=31536000, immutable`, and the
     tokens.css content; fetching the bare name returns `no-cache`.

The package sits under `internal/` so it can't leak into the app's
public API, and carries only `_test.go` files so it adds nothing to the
binary.

### The loop, stated where it's needed

A header comment in `harness_test.go` (and a line in `rastrillo new`'s
output) states the one non-obvious mechanic: tests import `gen`, so
after editing `actions/`, run `rastrillo generate` before `go test
./...` — or leave `rastrillo dev` running, which regenerates on save.
`new`'s closing hint becomes:

```
cd <name> && go test ./...
```

— a scaffold whose first suggested command is running its own passing
tests.

### Approaches considered

- **App-owned scaffolded harness (chosen).** ~60 lines the developer
  can read, own, and grow; zero new framework API.
- **A `rastrillo/apptest` framework package** with the harness as
  library code: rejected for v1 — the helpers are tiny, and a library
  boundary here would force the framework to know how apps wire their
  Ctx, which is exactly the part each app changes first.

## Testing the feature itself

- `assets_test.go` (framework): table-driven over an `fstest.MapFS` —
  hashed path shape, immutable header on match, `no-cache` on
  stale/bare, current-content-on-stale-hash, 404 on missing,
  subdirectory names, literal-name-wins, and the mtime-change → new
  hash path using a real temp dir.
- `cmd/rastrillo/new_test.go` grows assertions that the scaffold
  contains the harness and example tests, and the existing
  scaffold-compiles check extends to `go test ./...` passing in the
  scaffolded app.
- `examples/blog` adopts `Assets` (handler + `asset` template func) and
  `blogtest` gains the cache-header test, keeping the example the
  canonical demonstration.

## Out of scope

- CSS/JS bundling, minification, or source maps — hashing only.
- Serving compression (the platform edge handles transport concerns).
- A framework-owned `/static/` route or any `Options` field for assets.
- Import-rewriting hashed references *between* assets (a CSS file
  referencing an image by hashed URL). Until an app needs it, assets
  reference each other by bare name and forgo the long cache on those
  internal fetches.
