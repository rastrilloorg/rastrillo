# Data

**Pick the data layer before the first model; both are supported.**
(a) **GORM**, the scaffold's default: `db.Open`, models,
`rastrillo migration generate`, `migrate/gormfn` Go migrations, `scope`.
About 12–16% less CRUD code. Costs ~2× read time (1.7–1.9× the
allocations), +4.8 MB of binary, a hidden `BEGIN`/`COMMIT` around every
standalone write, and silent traps: `Updates(struct)` skips zero fields
(always `Select`); int64 `CreatedAt`/`UpdatedAt` get the wall clock;
`Session` shares its parent's statement; a `d.G` statement inside a
transaction deadlocks; `Model(&T{ID: 0})` refuses the update.
Upserts, `RETURNING` and locking writes need escape hatches.
(b) **Plain `database/sql`**, as Tito Go does: open your own
one-connection writer (db.Open's DSN) and readers;
`migrate.Apply(ctx, migrate.Pool(writer), set)`; Go migrations take
`migrate.Tx`; tests copy `dbtest.FromSet(set)`; besides the root
package, import only `make gorm-free` ones — no `db`, `scope`,
`gormlite`. Explicit transactions and plans, smaller and faster. You
write the scans, every migration (generate and `check` need GORM
`Models`: drop `migration-check` from `make ci` and
`.amadan/ci.d/40-migration-check`) and every owned query's
`WHERE user_id = ?`. CRUD-heavy, small team: (a). Money, fulfilment,
transaction-critical: (b). Switching is per package; schema and ledger
are the same.
docs/superpowers/specs/2026-09-26-gorm-vs-sql-findings.md

Models: no base type, no embedding. Owner column `UserID int64` tagged
`gorm:"index"` is what `scope.Owned` filters on.

`db.Open(path, logger)` returns `*db.DB`: `.G` is the `*gorm.DB`,
SQLite-wired (one file, WAL, one writer connection, several readers,
routed per statement). `d.Close()` closes both pools; `d.G.DB()` returns
the writer `*sql.DB` for database/sql packages.

**Bytes go in the object store, never on disk or in a column.** The
instance's filesystem is ephemeral and it hibernates, so a saved upload
is gone by the next request — silently, never at build time. The
platform delivers a bucket as `CARLOS_STORE_*`; `blobs.S3FromEnv()`
binds it and answers `(nil, nil)` — not an error — when there is none,
so checking `err` alone panics later; dev falls back to
`blobs.Dir(root)`. The row keeps a `blobs.Ref` (hex SHA-256 address,
size, content type), and under 4 KiB may sit inline in SQLite — a rule
stated everywhere and enforced nowhere. Presigned GET/PUT move bytes
browser-to-bucket without the app proxying. A page of pictures is the
`media-grid` partial (skill/partials.md).
docs/site/reference/blobs.md

A central app with no database (a sign-in home, a hub) keeps records
in `objrec`: `Create` wins once per key, `Update` refuses a stale
version, `Mutate` retries. docs/site/reference/objrec.md

A home or hub that creates tenant instances on CARLOS uses `provision`;
test it with `provisiontest`. docs/site/reference/provision.md

## Migrations

Schema changes: edit a model, `rastrillo migration generate`, read the
SQL before committing. Migrations apply once at boot, ledgered — never
re-run, never reversed. `migrations.go` declares two `*migrate.Set`:
`Schema` (the app's own; `generate`/`check` diff it against `Models`)
and `BootSchema = migrate.Merge(sessions.Schema, Schema)` (argument
order = apply order) — everything `App()` applies. Subsystems go in
`BootSchema`, never `Schema`, or `check` proposes dropping their
tables. Never edit a shipped migration; add a new one. Renames are
hand-written (`rastrillo migration new rename_x`); destructive changes
need `--allow-destructive`.

**Old database.** Boot refuses on a structural diff. Run
`migration baseline --db <path> --through <id>` FIRST, then the missing
migration by hand — bare `baseline` silently strands a pending data
migration. docs/site/migrations.md

**The first deploy of a version with migrations must be
schema-neutral.** Generate `0001_init` from the models as already
deployed, ship it alone, change a model only in a later release — else
boot refuses on the new column, and `baseline` there strands it for
good.

**Deleting a secret from SQLite does not unwrite it.** A value is in
`app.db-wal` from the insert — *before* it reaches `app.db` — and
survives `DELETE`: the WAL keeps the page as it was. So a gate
grepping only `app.db` passes while the plaintext sits in the WAL, and
Litestream has shipped it. `secure_delete = ON` cleans `app.db` at the
next checkpoint, never the WAL: set it on the writer (one connection,
so it sticks) and treat it as damage control. For a value that must
not be at rest the only answer is not to write it — seal, hash, or key
by digest.

## Roles and teams

Scoping separates users, never tenants: a CARLOS app serves one team; a
multi-team product runs an instance per team (idle instances hibernate
free). Roles/membership are the `amadan.net/rastrillo/idear` addon
(Owner/Admin/Member, invitations, a membership gate that mints no
session) — never core, never a subpath of this repository.
docs/site/addons.md
