# examples/tickets — the fully generated proof

Two manifest resources (`manifest/ticket_types.toml` on the exclusive
store, `manifest/announcements.toml` on the mergeable one), zero hand
actions, zero ejected templates. Everything under
`/admin/ticket_types` and `/admin/announcements` — the stores, every
action, all the screens, every locale key — is exactly what
`rastrillo generate` produced. There is no `actions/` directory and no
`templates/` directory in this app at all: manifest-only apps are
legal (§9), and this is what shipping one looks like.

The announcements resource is the `store = "mergeable"` regression
host: the same generated actions and templates as the exclusive
resource, backed by eventlog streams instead of a sqlc table — its
delete flow appends a tombstone event the suite proves is still in the
log after the record vanishes from every screen
(`internal/ticketstest/announcements_test.go`).

`examples/blog` shows a manifest resource adopted *alongside* hand
actions and ejected templates — coexistence. This shows the other end:
nothing to eject, nothing to hand-write, because nothing here needed
it. `internal/ticketstest` is the permanent regression host for that
claim — every test drives the real generated mux through `httptest`,
so a passing suite here can only mean the generator itself still
produces a working app, never that this example patched over a gap by
hand.

The manifest exercises this slice's two additions end to end: a
declared `[[list.filters]]` (Status: draft/on_sale/sold_out) renders
the generated dropdown, composes with search and paging, and never
400s on a stale value; `required = true` on Name and Price makes the
generated create/edit-basics actions validate server-side, 400ing with
the field's own message re-rendered in the form. The suite also
restates slice 1's Money Critical at the whole-app level: Price
round-trips through an untouched Edit save without drifting or 400ing
(`internal/ticketstest/roundtrip_test.go`).

## Running it

```
cd examples/tickets
go build ./cmd/tickets
./tickets -addr :8080
```

Then visit <http://localhost:8080/admin/ticket_types>. There is no
public side — this app has nothing to prove there.

**No `go generate` step.** `gen/` is committed, exactly as the blog's
is, so `go build ./cmd/tickets` works on a fresh clone.

**There is no auth.** Same caveat as the blog: the whole `/admin/…`
subtree is open (see the repo README's "Not built yet").

## The commands that work

```
go build ./cmd/tickets
go test ./...
go vet ./...
```

There is no `actions/` directory here, so none of `./...`'s usual
friction against generator input (friction log F9, the blog's README)
even applies — there is no generator input to trip over.

## How it is put together

- `manifest/ticket_types.toml` — the one declaration this app makes.
  Everything else below is what `rastrillo generate` built from it.
- `gen/` — committed generator output: `gen/store/ticket_types/`
  (sqlc-compiled Go over the `ticket_types` table), `gen/templates/
  ticket_types/{list,show,form}.html`, `gen/actions/admin/
  ticket_types/...` (all seven), `gen/locales/`, `gen/router.go`,
  `gen/manifest.json`. Never hand-edited.
- `internal/tickets/render.go` — the one seam a generated action needs
  and cannot reach itself (`Ctx.Render`): parses `ui`'s partials plus
  `gen/templates` into one tree per screen, keyed
  `"<resource>/<page>"`, and executes the shared layout. No hand view
  model, no ejection-root walk, no per-screen enrichment — contrast
  the blog's `internal/blog/genrender.go`, which exists only because
  that app *does* have hand screens and ejected templates to
  reconcile.
- `internal/tickets/templates/layout.html` — the one shell every
  screen renders inside.
- `assets.go` / `genassets.go` — `static/` and `gen/templates` embedded
  into the binary (friction log F8), the same fix as the blog's.
- `internal/ticketstest/` — the suite: a four-state round trip (create
  → show → edit unchanged → list), the filter round trip, required-400s
  for Name and Price, `"0"` accepted, and a `generate --check` test
  that doubles as this app's regen byte-identity proof for everything
  the pipeline's own emitters write (`--check` already diffs a scratch
  regen against the committed `gen/` byte-for-byte — see
  `generatecheck_test.go`'s own doc comment for the one thing it
  deliberately doesn't cover: sqlc's own compiled store output).
- `static/tokens.css` — copied from `ui.TokensCSS()`, unedited.

## Development

Regenerate after editing the manifest:

```
GOFLAGS=-mod=mod go run github.com/carlosframework/rastrillo/cmd/rastrillo generate .
```

Check the committed `gen/` is fresh:

```
GOFLAGS=-mod=mod go run github.com/carlosframework/rastrillo/cmd/rastrillo generate --check .
```
