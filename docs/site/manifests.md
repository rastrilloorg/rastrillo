# 🤖 Manifests

Manifests are the declarative path: an optional, equal alternative to
hand-written handlers.

Declare a resource in `manifest/<name>.toml`, run `rastrillo generate`,
and you get its store, its four screens' worth of actions and templates,
and their locale keys — as readable committed code composing the same
`form` and `view` helpers a hand-written app uses.

## The two paths live side by side

Per resource. Declare the screens that are pure CRUD, hand-write the
ones that are not, and move a resource between the paths whenever its
needs change: eject one generated file, or delete your hand-written
files and re-declare it.

`examples/tickets` is the fully generated proof — one manifest resource,
zero hand-written actions, zero ejected templates. `examples/blog` is
the mixed case.

## A resource

```toml
name  = "posts"
route = "/admin/posts"
store = "exclusive"

[list]
columns = [{ field = "Title" }, { field = "Status" }]
search  = true

[[list.filters]]
field  = "Status"
values = ["draft", "published"]

[form]
basics = [{ name = "Title", required = true }, { name = "Body", kind = "textarea" }]
```

## The vocabulary is honestly scoped

One flat resource. Three field kinds: `text`, `textarea`, `money`. No
relations.

Relations and custom flows take the code path. That boundary is where
the generator currently stops, and reaching it is not a failure —
mixing the two paths is the design.

Every generated store adds fixed columns: `id`, `created_at`,
`updated_at`, plus `owner` for a scoped resource. A field colliding with
one of those is refused at generate time instead of producing a
confusing table.

## Ownership

```toml
scope = "user"
```

Owner-filters every generated query by the session subject, so someone
else's row answers 404 — the same discipline the
[scope](/docs/scoping) package enforces by hand, declared instead of
written.

Mount the resource behind `sessions.Require` or `auth.RequireSession`.
The generated queries carry the filter; they do not authenticate.

`examples/notes` runs both halves side by side and proves them with one
two-user suite.

## The two stores

`store = "exclusive"` is the default: one SQL table, with `sqlc` query
colocation for the store.

`store = "mergeable"` keeps each record as an
[`eventlog`](/docs/reference/eventlog) stream — one stream per record,
derived reads, tombstone deletes, misses surfacing as `sql.ErrNoRows`. A
scoped mergeable resource checks the row's owner the same way the
exclusive one does.

Two limits before you choose it, both live today. Ids are writer-local,
because the platform's edge-sync transport is not built yet
(`eventlog.Ingest` is the seam it will call). And every generated
event's actor is `"app"`; threading a real actor through is not done.

If neither matters for your resource, it works. If they do, use
`exclusive` for now.

## Schema evolution

Generated stores emit only the initial `CREATE`. There is no automatic
diff-and-`ALTER` for a declared resource, so evolving one's schema is
your own migration — write it with `rastrillo migration new`.

There is a trap in how this meets the migration ledger. A generated
store's `gen/store/<name>/migrations.go` is a raw `[]string` you wrap
into a `*migrate.Set` by hand. Reshape a resource after first boot and
the regenerated SQL no longer matches the ledger's recorded checksum, so
boot refuses.
[Migrations](/docs/migrations#recovering-an-old-database) has the
recovery, and its step order matters.
