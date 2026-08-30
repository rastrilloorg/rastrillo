# examples/blog — a whole app, built from stock parts

A single-user blog: a public reading side (index, one post per page) and
an admin section for writing, editing, publishing and deleting. Eleven
actions on rastrillo's filesystem router — most of `admin/posts/`
manifest-generated or ejected from what generation produced, a few
still hand — one SQLite table, the partials this app itself uses (of
the larger set `ui` now ships — see `ui`'s package doc for the full,
current list) each in its intended role, and **no JavaScript** — no
`<script>` tag anywhere, no off-origin reference on any page. The look
is `static/tokens.css` unedited plus about fifty app-owned lines in
`static/blog.css`.

`examples/helloworld` proves the framework's plumbing (scaffold,
generate, serve, deploy). This proves its *shape*: that an app assembled
from stock parts, with no JavaScript and almost no hand-written CSS, is a
thing a person would ship.

## Running it

```
cd examples/blog
go build ./cmd/blog
./blog -addr :8080
```

Then read at <http://localhost:8080/> and write at
<http://localhost:8080/admin/posts>.

**No `go generate` step.** `gen/` is committed, exactly as helloworld's
is, so `go build ./cmd/blog` works on a fresh clone.

**There is no auth.** The whole `/admin/…` subtree is open. Rastrillo's
actor and session layers are designed but unbuilt (see the repo README's
"Not built yet"), so this example runs locally and says so rather than
inventing a password field (F7).

## The commands that work

```
go build ./cmd/blog     # the binary (./... would discard it — go help build)
go test ./...
go vet ./...
```

`./...` works because every action file carries `//go:build
rastrillo_actions` — the F9 fix. Without the constraint, `go build
./...`, `go vet ./...` and `go test ./...` all failed here: actions/ is
generator input the go tool tried to compile anyway. See F9 below for
the original finding and its resolution.

## How it is put together

- `manifest/posts.toml` — the one manifest-declared resource: `Title`/
  `Body` columns, a search box, no advanced fields. See "Manifests"
  below for what it generates and what this app added by hand on top.
- `actions/` — one file per route, named `<name>.<VERB>.go`, in
  directories that spell the URL. It is generator *input*, not a
  compilable package. Most of `admin/posts/` is manifest-generated and
  has no file here at all; the exceptions — the admin list
  (`admin/posts/index.GET.go`) and `publish`/`unpublish`/`delete`
  (`admin/posts/[id]/{publish,unpublish,delete}.POST.go`) — are hand
  files claiming a path the generator would otherwise own (or, for the
  three POST actions, a route the manifest system has no way to
  generate at all: v1 generates no delete, and publish/unpublish have
  no manifest concept behind them).
- `templates/posts/{list,form}.html` — ejected from their generated
  counterparts (task 11): copied out, then hand-edited to add the
  status pill and the publish/unpublish/delete strip (form.html) and
  the status-filter dropdown (list.html) neither generated template can
  express. `posts/show` is not here — it never needed ejecting.
- `gen/` — committed generator output. Never hand-edited. Alongside the
  filesystem router (`gen/router.go`) it now carries the manifest
  system's own output: `gen/store/posts/` (sqlc-compiled Go over the
  `posts` table), `gen/templates/posts/show.html` (the one screen this
  app didn't eject), `gen/locales/` (`en.toml` and `locales.go`'s
  `BaseCatalog`), and `gen/manifest.json`.
- `internal/blog/store.go` — `Open`; `Migrations` (the generated
  `posts` table plus this app's own additive `published` column — see
  "Manifests" below); `Post`; and the hand queries the generated store
  has no way to express — a status filter alongside search, and
  `SetPublished`/`Delete`. The generated store's own `Create`/`Update`
  queries still do the writing the manifest's own actions call; this
  file exists for the reads and writes a manifest field can't cover.
- `internal/blog/view.go` — the template tree, `Render`, `Fail`, and
  every hand view model. One base tree (ui's partials + the layout)
  cloned once per screen at startup. `pages` is keyed bare (`"index"`,
  `"post"`) for hand screens with nothing to do with a manifest
  resource, and `"<resource>/<page>"` (e.g. `"posts/list"`) for every
  manifest one — walking `gen/templates/` first and `templates/`
  (the ejection root) second, so an ejected file wins at the same key.
- `internal/blog/genrender.go` — the generated/ejected screens' own
  adapter code: `genT` (locale lookups against
  `gen/locales.BaseCatalog`), `headFor`/`genHead` (a `<head>` title for
  the one screen with no `Head` field of its own, `posts/show`), and
  `formStripData` (builds the Edit screen's status-pill/publish/
  unpublish/delete strip data from a generated `formView`-shaped value
  by reflection, since a generated action has no way to know about
  `published` at all).
- `internal/blog/templates/` — `layout.html` and one file per hand
  screen (`pages/index.html`, `pages/post.html`).
- `internal/blogtest/` — the tests, a directory of `_test.go` files that
  drives the real generated mux through `httptest`.
- `static/` — `tokens.css` (shipped by `rastrillo new`, unedited) and
  `blog.css` (the app's own).

## Manifests

`posts` is declared once, in `manifest/posts.toml`, and generation used
it to produce a store, three of the four action groups it can generate,
and one full screen — everything else here is this app's own addition
on top, ejection or hand file, none of it faked as "coming later."

**Generated, still:** the `posts/show` screen end to end
(`gen/templates/posts/show.html`, `gen/actions/admin/posts/id/
index_get/index.GET.go`); the new/create and edit-basics actions
(`gen/actions/admin/posts/{new_get,index_post}/`,
`gen/actions/admin/posts/id/{edit_get,edit_basics_post}/`); and the
whole `gen/store/posts/` sqlc package those actions read and write
through.

**Ejected:** `templates/posts/list.html` and `templates/posts/form.html`
— copied out from their generated versions, then hand-edited. Ejecting
a file stops generation of *only* that one file; `posts/show` above
kept regenerating right through both ejections, and re-running
`rastrillo generate` never touches either ejected file again.

**Hand, entirely:** the admin list action itself
(`actions/admin/posts/index.GET.go` — a hand file sitting at the exact
path the generator's `EmitActions` would otherwise claim, so that one
file is skipped, not overridden) and `publish`/`unpublish`/`delete`
(`actions/admin/posts/[id]/{publish,unpublish,delete}.POST.go` — routes
the manifest system has no way to generate: v1 ships no delete action,
and there is no manifest concept for a status toggle).

**Why `published` is an app migration, not a manifest field.** Neither
form has a "Published" field to fill in — the status changes only
through the publish/unpublish actions — so `posts.toml` declares just
`Title` and `Body`. `published` is `internal/blog/store.go`'s own
additive column: `Migrations` is `postsstore.Migrations` (the generated
`CREATE TABLE IF NOT EXISTS`) followed by this app's `ALTER TABLE posts
ADD COLUMN published INTEGER NOT NULL DEFAULT 0`. Order matters — a
fresh database needs the table before anything can add a column to it —
and `rastrillo.OpenDB` swallows sqlite's "duplicate column" error, which
is what makes reapplying that `ALTER` on every boot safe.

**Why `Title` has `required = true` in the manifest, but no `required`
attribute in the ejected form.** The manifest declares `required = true`,
which generates server-side validation: an empty Title submission
re-renders the form with a 400 status and an error message. Today's
generated form template also passes `"Required" true` to the field
partial, which emits a client-side `required` attribute — but
`templates/posts/form.html` was ejected (copied out and hand-edited for
other reasons: status pills, publish/unpublish/delete buttons) *before*
`required = true` was added to the manifest, so the snapshot it copied
predates the client-side marker. Ejection stops generation of that one
file cold, so the marker never arrives — no future regen will add it,
ejected or not. Server-side validation is untouched by any of this: the
ejected template still inherits the field partial's error rendering, so
a blank Title from either path gets the same 400 re-render. The server
is authoritative; the client-side `required` attribute is a nicety this
particular ejected file happens to be missing.

## Development

Regenerate after adding, renaming or removing an action:

```
go run github.com/carlosframework/rastrillo/cmd/rastrillo generate .
```

Check a committed `gen/` is fresh (for CI, or before review):

```
tmp=$(mktemp -d) && cp -R actions go.mod "$tmp"/
go run github.com/carlosframework/rastrillo/cmd/rastrillo generate "$tmp"
diff -r gen "$tmp/gen"
```

Copying `go.mod` is what makes the generated import paths match; the
`actions/` tree and that one file are the generator's whole input. This
is a procedure rather than a Go test because `internal/generate` is
internal to the rastrillo module and the CLI always writes to
`<dir>/gen`, so it cannot be pointed at a scratch output directory from
inside a test. The everyday guard against a stale `gen/` is the route
tests, which run through the committed router and fail immediately if it
no longer matches the actions.

This diff is the hand-action half of freshness; it doesn't touch
`manifest/posts.toml`'s own output at all (a fresh temp dir has no
`manifest/`, so `GenerateManifests` no-ops in it). The manifest half —
idempotency of `gen/store/`, `gen/templates/posts/show.html`,
`gen/locales/` and `gen/manifest.json`, plus hand-vs-generated route
collisions — is what `rastrillo generate --check .` verifies in place,
alongside the action build-tag and locale-completeness checks it
already runs.

## What building this revealed

Recorded, not fixed. Nothing outside `examples/blog/` changed on this
branch; each of these belongs to a later framework slice.

**F1 — `list-row-action` has no status slot.** A blog list's most
load-bearing fact per row is "draft or published", and the row contract
offers `Main`, `Sub`, one action pill and a decorative `aria-hidden`
lead marker. Status goes into `Sub` as prose. It works and it is
accessible; the row partial wants a `Status` `Tone`/`Label` pair that
renders `status-pill` in the row's right-hand group.
*Fixed:* list-row-action grew the StatusTone/StatusLabel pair and this list uses it — the pill carries the state, Sub keeps the date ("Edited 2 August 2026").

**F2 — no form partials, and the focus ring stops at the library's
edge.** The `field`/`field-textarea` family is deferred, so every form
here is hand-rolled, and `tokens.css` scopes its `:focus-visible` rule to
library containers, so `blog.css` has to restate the outline for controls
outside them. Roughly half of `blog.css` would disappear the day form
partials land.
*Fixed:* the field-text/field-textarea/form-foot partials landed and these forms use them (see `templates/posts/form.html`, ejected from the manifest system's own generated form under task 11 — the field/form-foot calls are unchanged from what generation produced); tokens.css now scopes the focus ring to [rst-page], so the restatement is gone. blog.css kept only the page styling the library leaves to the app.

**F3 — no `dropdown`, so "show drafts only" is missing.** Also deferred.
The obvious next control on the admin list — a status filter — has
nowhere to attach, so the example ships without it rather than
hand-rolling a control the library has already scheduled.
*Fixed:* list-bar takes a Filter dropdown — a native <details> menu of links — and this list filters by status with it (All / Drafts / Published, composing with search and paging).

**F4 — `Serve`'s `DBPath`/`Migrations` are unusable by any app that puts
the DB in `Ctx`.** `Serve` opens the handle, defers its `Close`, and
never exposes it; `openDB` is unexported and `Options` carries no `DB`
field and no Ctx factory. So this app opens its own handle and
hand-copies the pragma DSN — `busy_timeout` before `journal_mode(WAL)`,
then `SetMaxOpenConns(1)` — which is precisely the hand-propagation the
framework exists to end. Next slice: return the handle from `Serve`, or
let `Options` carry the Ctx factory.
*Eased, not fixed:* `rastrillo.Resolve` now applies the activation
contract without serving, so this app honors `-db`, `serve`, and
`$STATE_DIRECTORY` while still opening its own handle (see
`cmd/blog/main.go`). The hand-copied pragma DSN remains — the real fix
is still the next-slice shape above.
*Fixed:* `Options.Router` now receives the `*sql.DB` that `Serve`
opened — pragmas, eager ping, and `Options.Migrations` applied — so
`cmd/blog/main.go` is back to plain `Run` and the hand-copied DSN is
gone. `rastrillo.OpenDB` is the same opener exported, which is what
`blog.Open` (kept for the tests) now wraps.

**F5 — `actions/` cannot hold shared code, by two rules.** The generator
copies only files matching `<name>.<VERB>.go`, and separately skips any
file whose base name starts with `_`. So neither `helpers.go` nor
`_helpers.go` ever reaches `gen/`, and an action calling into one
compiles in `actions/` and fails in `gen/`. Shared code lives in a normal
package (`internal/blog`); that is the only path, not a preference.

**F6 — `actions/index.GET.go` is a catch-all.** `index` maps to `GET /`
and Go's mux treats `/` as a prefix, so every unmatched GET lands in the
index action. Every app needs the `r.URL.Path != "/"` guard, or the
generator should emit a 404 fallback for it.
*Fixed:* the generator anchors the root index to GET /{$}, so unmatched GETs fall to the mux's own 404; this app's hand guard is gone from actions/index.GET.go.

**F7 — no auth, because there is none to use.** The `/admin/…` subtree is
open. A real deployment would put it behind an authenticated session and
set `Ctx.Actor` from it instead of this app's hardcoded human actor;
nothing else about the app would change shape.

**F8 — static serving is relative to the working directory.** The
scaffolded line is `http.FileServer(http.Dir("static"))`, so the binary
must be run from the app root or every screen renders unstyled.
*Fixed:* static/ is embedded (assets.go) and served with http.FileServerFS — the shipped binary carries its stylesheets, wherever it starts. Edits need a rebuild; rastrillo dev watches static/ and does it on save.

**F9 — `go build ./...`, `go vet ./...` and `go test ./...` all fail in a
rastrillo app.** `actions/` is generator input, not a package Go can
compile: two actions in one directory both declare `Handle`
(`actions/admin/posts/` has three), and a `[id]` directory is a malformed
Go import path — which is what every `./...` invocation trips over first,
`vet` included. helloworld never hit this because it has one action in
one directory. The working commands are listed above; the framework
should say so in `rastrillo new`'s output, and could remove the problem
entirely by reading actions from a directory the Go tool ignores.
*Fixed:* action files now carry `//go:build rastrillo_actions` — never
satisfied by a normal build, so every `./...` invocation skips
generator input (including the `[id]` directories) instead of failing
on it. `rastrillo new` scaffolds the constraint, `rastrillo generate`
strips it from the `gen/` copies, and `generate --check` fails with the
exact line to add when a file lacks it. This app is tagged; its
`./...` commands pass.

**F10 — `tokens.css` still styles a pagination state the partial no
longer emits.** `ui/partials/pagination.html` renders a disabled item as
a bare `<span>{{.Label}}</span>` — `aria-disabled` was deliberately
dropped in `c00653c` — but `tokens.css` still carries
`[rst-pagination] [aria-disabled="true"] { border-style: dashed; color: var(--rst-text-faint) }`,
which now matches nothing. The visible result on this blog's list screens
is that a disabled `Previous` on page 1 looks identical to a live page
link: same border, same colour, differing only in not being clickable.
The fix belongs in the library — either restore the attribute on the
span, or restyle the rule to target the disabled item as the partial
actually emits it — and not in an example, so this branch changes
nothing.
*Fixed:* in the library, the second way — the span now carries
`rst-pagination-disabled` and tokens.css styles that class.
`aria-disabled` stays dropped on purpose: the attribute belongs on
elements with an interactive role, and a bare span has none.
