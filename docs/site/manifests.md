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

Adding `scope = "user"` to a resource that already has rows gives every
existing row an empty owner, and a row with no owner is shown to nobody.
Assign owners to those rows in a migration of your own, written with
`rastrillo migration new`.

`examples/notes` runs both halves side by side and proves them with one
two-user suite.

## The JSON API

When your app mounts the API, a `scope = "user"` resource is also
served as JSON at `/api/v1/<name>`, to a person's token. A resource with
no owner is not, unless you add:

```toml
[api]
expose = true
```

Commit `api/v1.json` with `gen/`. See [JSON API](/docs/api).

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

Before the app serves, `gen.Boot` reads every mergeable store's events
once and refuses to start on one it cannot read: one written by a newer
version, or one that breaks the money rules below. A store with no
money fields checks only each event's version.

Every event's payload must be a JSON object. An event you append by
hand, of a kind the store does not know, with any other payload, makes
reads of that stream fail.

## Money

A resource with a money field declares the default currency for its new
records:

```toml
name     = "ticket_types"
route    = "/admin/ticket_types"
currency = "USD"

[list]
columns = [{ field = "Name", kind = "text" }, { field = "Price", kind = "money" }]
search  = true

[form]
basics = [{ name = "Name", required = true }, { name = "Price", kind = "money" }]
```

`currency` is an ISO 4217 code. `generate` refuses a money field without
one. A code that is no longer issued, such as HRK, still generates, with
a warning to choose a current one.

Each money field `Price` is two columns, `price_minor` and
`price_currency`: the amount in the currency's smallest unit, and its
code. Both are empty for a record with no amount, and both are set for
one with an amount. A new record takes the resource's `currency`. An
edit never changes a record's currency.

`rastrillo generate` writes `gen/boot.go`. Call `gen.Boot` after your
schema is applied and before you serve, and pass what it returns to
`gen.Router`. Boot upgrades the money columns and checks them, and it
refuses to start on a problem. `gen.Router` panics without it:
`gen.Router: gen.Boot has not run for this database; call it after the schema is applied and before serving`.

### Money stored before this release

Money columns used to hold integers with no currency. For a resource
with such rows, say what they meant:

```toml
[legacy]
currency = "USD"
scale = 2
```

`scale` is how many decimal places the old integers carry: 2 for cents,
0 for whole units. `currency` can be any code the storage set holds,
including one no longer issued, such as SLL.

On its first boot, `gen.Boot` replaces each old column `price` with
`price_minor` and `price_currency`. It adds the two columns, copies each
amount into the currency's smallest unit, and drops the old column. The
drop rewrites the table, so the first boot takes longer on a big table.
It all happens in one transaction. Any failure changes nothing, and the
app does not start.

An amount the currency cannot hold exactly is a failure: at scale 2,
`1250` is 12.50, which JPY cannot hold. Nothing is ever rounded. Old
rows with no `[legacy]` stop the start too, with a message such as
`ticket_types has 3 rows from before money had a currency. Add [legacy] currency and scale to manifest/ticket_types.toml and run rastrillo generate.`

This is a stop-start upgrade. Take a backup, stop every running copy,
then start the new version. An old version cannot run against the
upgraded table, because the column it reads is gone. To go back,
restore the backup.

### Money in a mergeable store

When a mergeable resource gains a money field, its events carry money,
and the next boot renames the event log's `payload` column to
`payload_v2`. `rastrillo generate` says so when it first adds that step
to `gen/boot.go`:

`announcements keeps money in events, so the next deploy renames the event log's payload column. Take a backup, stop every running copy, then start the new version. Never run old and new versions side by side. To go back, restore the backup.`

A new app with no events yet gets the same notice, and has nothing to
back up. Apps whose events carry no money keep `payload`. See
[eventlog](/docs/reference/eventlog#money-in-events).

## The app's languages

`manifest/app.toml` is not a resource. It holds the languages the app
offers, `locales` and `default_locale`; see
[Localization](/docs/localization#declaring). Because of it, no resource
can be named `app`:
`manifest/app.toml holds the app's locales, so no resource can be named "app"`.

## Schema evolution

A generated store's `gen/store/<name>/migrations.go` only grows. When
you add a field, or make a resource user-scoped, `rastrillo generate`
appends the statements that bring the table up to date, and says so:

`ticket_types: appended ALTER TABLE ticket_types ADD COLUMN status TEXT NOT NULL DEFAULT '' to gen/store/ticket_types/migrations.go; released statements are never edited`

A statement that has run is never edited, so the migration ledger never
sees a changed checksum. Money columns are not appended: `gen.Boot` adds
them.

Nothing is removed. A field you delete or rename keeps its old column,
and its data. To move data or drop a column, write your own migration
with `rastrillo migration new`.
