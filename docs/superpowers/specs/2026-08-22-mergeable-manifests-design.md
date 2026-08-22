# Manifest resources on the mergeable store — design

Date: 2026-08-22. Target: v0.16.0. Clears the site's "Manifest
resources on the mergeable store" Pending item. Commit this file as
docs/superpowers/specs/2026-08-22-mergeable-manifests-design.md.

## 0. The governing insight

Generated actions never speak SQL — they call a concrete store package
(`<name>store.New(ctx.DB)`) whose method shape is:

    List<Plural>(ctx, ListParams) ([]Model, error)
    Count<Plural>(ctx, CountParams) (int64, error)
    Get<Singular>(ctx, GetParams) (Model, error)     // miss => sql.ErrNoRows
    Create<Singular>(ctx, CreateParams) (int64, error)
    Update<Singular>Basics/Advanced(ctx, UpdateParams) error
    Delete<Singular>(ctx, DeleteParams) error

So `store = "mergeable"` is a second store emitter satisfying the SAME
shape over `eventlog`, and the actions, templates, locales, router,
and screens ship unchanged. The 2026-08-17 completion spec §9 already
ruled the one behavioral difference: **Delete appends a tombstone
event rather than `DELETE`; derive skips dead ids.**

## 1. Vocabulary change

`manifest.go` `Validate`: delete the two-line "mergeable is not yet
built" refusal (keep the unknown-value check). Update
`manifest_test.go`'s `TestValidateRejections` row and
`internal/manifest/manifest_test.go`'s `TestLoadValidates` to assert
mergeable now loads. Everything else about the vocabulary (kinds,
scope, filters, reserved names — `owner` was reserved from day one for
exactly this) is already mergeable-ready.

## 2. The generated mergeable store

`EmitStore` branches on `r.Store`:

- **Exclusive** (unchanged): sqlc.yaml entry, schema.sql, queries.sql,
  migrations.go; `RunSqlc` runs only when at least one exclusive
  resource exists (a module of only-mergeable resources needs no sqlc
  tool at all — `sqlc.yaml` is not written then).
- **Mergeable**: `gen/store/<name>/store.go` + `migrations.go`,
  rendered from templates in internal/generate (no sqlc involvement).

The store package (`<name>store`):

- `var Migrations = eventlog.Migrations` re-exported (idempotent, so
  two mergeable resources appending it twice is harmless) — the
  resource has NO table of its own; every record is an eventlog
  stream.
- Stream key: `"<name>/<id>"`. Event kinds: `"created"`,
  `"updated"`, `"deleted"`. Payloads: JSON objects of the written
  field group (created: all fields + owner when scoped + created_at;
  updated: the group's fields + updated_at; deleted: empty).
- Actor ruling: the store stamps `"app"` on every event for now, so
  the action bodies and param structs stay byte-parallel to the
  exclusive path's. Threading `ctx.Actor` into store mutations is a
  recorded additive follow-up (it matters when agent-written manifest
  actions need per-event provenance; today every generated mutation
  is consent-gated upstream), stated in the generated package doc.
- Writer identity: `eventlog` gains ONE public helper —
  `LocalWriter(ctx, db) (string, error)` — which reads (or mints and
  persists, crypto/rand 16 bytes base64url) a single-row
  `eventlog_writer` table appended to `eventlog.Migrations`. The
  generated store resolves it lazily per call (one-row SELECT is
  noise; no constructor error, keeping `New(db)`'s shape). This is
  the durable per-instance identity the platform's future transport
  needs; a hardcoded writer would poison history the first time two
  edges merge.
- ID allocation: int64 (so `view.ParseID`, hrefs, and the model shape
  stay identical). `Create` computes `max(existing id in this
  resource's streams) + 1` and appends the created event; correctness
  rests on the single-writer SQLite pool both db paths already
  enforce (one process, one writer connection — race-free by
  construction; no Go-side lock, which would not survive a second
  process anyway). The honest caveat goes in the generated package
  doc: ids are writer-local; when the platform's transport lands,
  cross-writer id namespacing is that design's problem, and this
  allocator is the first thing it replaces.
- Reads: derive-on-read, no materialized cache — the eventlog package
  doc's own stance ("replay is cheap until proven otherwise").
  `List/Count/Get` load the resource's events via `EventsByPrefix`,
  group by stream, `Derive` each into the model struct (deleted →
  dropped), then apply owner filter (scoped: `owner = params.Owner`,
  no escape, mirroring the exclusive store's always-on clause),
  search (case-insensitive substring over the text/textarea columns —
  mirror the exclusive whereClauses semantics; read store.go and
  match them), the single Values filter, the exclusive path's sort
  order (read queriesSQL's ORDER BY and mirror it exactly), and
  offset/limit pagination. Misses and foreign rows return
  `sql.ErrNoRows` so the generated actions' `errors.Is` branch works
  untouched — 404-not-403 preserved for free.
- Model struct: same fields as the exclusive model
  (`ID int64; Owner, <Fields>, CreatedAt, UpdatedAt string`), so
  templates and `fieldExpr` work unchanged.

## 3. eventlog additions (additive only)

- `func (l *Log) EventsByPrefix(ctx context.Context, prefix string) ([]Event, error)`
  — every event whose stream has the prefix, in the same total order
  as `Events` (lamport, ts, writer, seq; `l.Order` applied per stream
  group if set — match Events' semantics, sorted stably). Needed
  because list screens must enumerate a resource's records and the
  package owns its table.
- `func LocalWriter(ctx context.Context, db *sql.DB) (string, error)`
  + the `eventlog_writer` one-row table appended to `Migrations`.
- Tests for both (prefix isolation: "bookmarks/1" never matches
  "bookmarksarchive/…" — use `stream LIKE prefix || '%'` carefully or
  a range scan; pin with a test), plus a vectors-untouched assertion
  (the merge-vectors file must not change).

## 4. Generator plumbing

- `EmitStore` branches per §2; `sqlc.yaml` includes only exclusive
  resources; `emitPipeline` passes `runSqlc` only when any exclusive
  resource exists.
- `EmitActions`/`EmitTemplates`/`EmitLocales`/`Router`: unchanged
  (verify by generating a mergeable resource and compiling).
- `gen/manifest.json`: `"store": "mergeable"` simply appears — the
  artifact already serializes Store.
- `generate --check`: works unchanged (the mergeable store is
  template-rendered, no sqlc gap).
- Scaffold's `manifestReadme` (cmd/rastrillo/new.go): one sentence
  noting `store = "mergeable"` and that it needs no sqlc tool.

## 5. Example and regression host

`examples/tickets` (the permanent no-mask generator regression host)
gains a second manifest, `manifest/announcements.toml`:

    name  = "announcements"
    route = "/admin/announcements"
    store = "mergeable"
    [list]
    columns = [{ field = "Title" }]
    search  = true
    [form]
    basics = [{ name = "Title", required = true }, { name = "Body", kind = "textarea" }]

Commit its generated `gen/` output (like ticket_types'). main.go
appends `announcementsstore.Migrations`. ticketstest gains:
- `TestAnnouncementsFourStateRoundTrip` (list/create/show/edit/delete
  through the real generated mux — mirror roundtrip_test.go).
- `TestAnnouncementsDeleteIsATombstone`: after the delete flow, the
  record is gone from the list AND the eventlog still holds the
  stream's created/updated/deleted events (query via
  eventlog.Open + EventsByPrefix) — the proof this is an event store,
  not a DELETE.
- `TestGenerateCheckIsGreen` continues to cover both resources.
- A two-derive determinism probe: derive the list twice, byte-equal.
examples/notes is NOT touched (site hero counts stay stable).

## 6. Out of scope (state in the spec file)

- Transport/edge sync (platform's; `Ingest` remains the seam).
- Threading `ctx.Actor` into generated store mutations (follow-up).
- Snapshots/caching of derived state; mergeable `scope = "user"`
  IS in scope (it is just an owner field in payloads + a filter) —
  include a scoped mergeable unit test at the generate level, but the
  tickets example resource stays unscoped (tickets has no sessions).
- Cross-writer id namespacing (documented caveat).
- Manifest vocabulary growth of any kind.

## 7. Testing summary

internal/generate tests: golden-file style assertions for the emitted
mergeable store of a scoped and an unscoped resource (mirror how
store.go's emitters are tested today — read the existing generate
tests and match their idiom); eventlog tests for EventsByPrefix and
LocalWriter; tickets suite per §5; full CI (which runs the examples
and `generate --check` + `git diff --exit-code`) green.
