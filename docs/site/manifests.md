# 🤖 Manifests

Manifests are the declarative path: an optional, equal alternative to
hand-written handlers. Not a requirement, and not a legacy mode.

Declare a resource in `manifest/<name>.toml`, run `rastrillo generate`,
and get its store, its four screens' worth of actions and templates, and
their locale keys — as readable committed code that composes the same
`form` and `view` helpers a hand-written app uses.

## The two paths live side by side

Per resource. Declare the screens that are pure CRUD, hand-write the
ones that are not, and move a resource between the paths whenever its
needs change — eject one generated file, or delete your hand-written
files and re-declare it.

`examples/tickets` is the fully generated proof: one manifest resource,
zero hand-written actions, zero ejected templates. `examples/blog` is
the mixed case, with a declared resource beside hand-written actions and
ejected templates.

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

One flat resource. **Three field kinds** — `text`, `textarea`, `money`.
**No relations.**

Relations and custom flows take the code path. That boundary is where
the generator currently stops, not where it is fated to stop, and
nothing about reaching it is a failure — mixing the two paths is the
design.

Every generated store adds fixed columns — `id`, `created_at`,
`updated_at`, plus `owner` for a scoped resource — and a field colliding
with one of those is refused at generate time rather than producing a
confusing table.

## Ownership

```toml
scope = "user"
```

Owner-filters every generated query by the session subject: someone
else's row answers 404, the same discipline the
[scope](/docs/scoping) package enforces by hand, declared instead of
written.

Mount the resource behind `sessions.Require` or `auth.RequireSession`.
The generated queries carry the filter; they do not authenticate.

`examples/notes` runs both halves side by side and proves them with one
two-user suite.

## The two stores

### store = "exclusive"

The default. One SQL table, with `sqlc` query colocation for the store.

### store = "mergeable"

Each record is an [`eventlog`](/docs/reference/eventlog) stream —
one stream per record, derived reads, tombstone deletes, misses
surfacing as `sql.ErrNoRows`. A scoped mergeable resource checks the
row's owner the same way the exclusive one does.

Two limits to know before choosing it, both live today:

- **Ids are writer-local.** The platform's edge-sync transport is not
  built yet; `eventlog.Ingest` is the seam it will call.
- **Every generated event's actor is `"app"`.** Threading a real actor
  through is not done.

If neither of those matters to your resource, it works. If they do,
`exclusive` is the honest choice for now.

## Schema evolution

Generated stores emit only the initial `CREATE`. There is no automatic
diff-and-`ALTER` for a declared resource, so **evolving a declared
resource's schema is your app's own migration** — write it with
`rastrillo migration new`.

There is a trap in the interaction with the migration ledger. A
generated store's `gen/store/<name>/migrations.go` is a raw `[]string`
you wrap into a `*migrate.Set` by hand. Reshaping a resource after first
boot regenerates that SQL, whose checksum no longer matches the ledger's
record — and boot refuses. [Migrations](/docs/migrations#recovering-an-old-database)
has the recovery, and its step order matters.
