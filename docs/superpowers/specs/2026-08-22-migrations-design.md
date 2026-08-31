# Migrations

**Date:** 2026-08-22
**Status:** Approved in discussion; this document is the written record.
**Inputs:** the known-libraries middle layer design
(`2026-08-21-known-libraries-middle-layer-design.md` §5, which deferred
this), the current two-mechanism boot path in `SKILL.md` §2, and the
`gormlite` fork's existing SQLite rebuild machinery.

## 1. Problem

A Rastrillo app runs two schema mechanisms side by side at boot, and
`SKILL.md` has to explain the seam between them:

```go
if err := d.G.AutoMigrate(&User{}, &Note{}); err != nil { return nil, err }
for _, stmt := range sessions.Migrations {
	if err := d.G.Exec(stmt).Error; err != nil { return nil, err }
}
```

App models go through GORM's `AutoMigrate`; framework subsystems ship
raw SQL in an exported `Migrations []string` and must not be handed to
`AutoMigrate`. Five packages export one today — `auth`, `blobs`,
`eventlog`, `passkey`, `sessions` — plus the generated stores under
`gen/store/`. The legacy `rastrillo.Options.Migrations` in `serve.go`
is a third path, with a `duplicate column` error swallow
(`isDuplicateColumn`) standing in for any real record of what has run.

Nothing in that picture records which changes have been applied. The
consequences are the ones the middle-layer design accepted for v1 and
named as its limits: no destructive changes, no renames, no data
migrations, no way to review a schema change before it runs, and no way
for CI to tell that models and database have diverged.

The goal is a story a person or an agent can use day to day and trust:
edit a model, get a reviewable artifact, have CI catch drift, and have
the change apply itself on deploy.

## 2. Goals and non-goals

Goals, in order:

1. One mechanism. An app applies its schema with a single call.
2. A schema change produces an artifact that is read and reviewed
   before it ever touches a database, and what was reviewed is
   byte-for-byte what runs.
3. CI can detect that models and migrations disagree, with no database.
4. Destructive changes and renames become possible, deliberately.
5. Existing deployed databases adopt the new runner without an
   operator.

Non-goals: down migrations and rollback (§3); a CLI that applies
migrations (§4); replacing `rastrillo.Options.Migrations` for apps on
the pre-GORM path (it stays, untouched); support for any database
other than SQLite.

## 3. Two things deliberately not built

**Rollback.** A CARLOS app is one SQLite file that the platform's
activator replicates continuously. Production rollback is a
point-in-time restore of that file, not a `Down` function. Writing and
maintaining a reverse for every migration is real, permanent cost
buying almost nothing on this substrate, so migrations are forward-only
and the ledger has no rollback path. Undoing a migration in development
means deleting the local database file.

**Atlas.** `ariga.io/atlas-provider-gorm` is the best schema-diff engine
available and the wrong dependency here: it is a separate CLI binary
outside the Go toolchain, which contradicts Rastrillo's promise that
`go build` works immediately after `rastrillo new`, and its richer
features are commercially gated. Rastrillo already owns the hard part —
`gormlite/migrator.go` and `gormlite/ddlmod.go` construct SQLite table
rebuilds — and GORM's `Migrator()` exposes the introspection the diff
needs. The diff engine is built in-process (§5).

## 4. Where migrations apply: boot, and only boot

Migrations apply at boot, inside the app's `App()` function, on the
writer pool. There is no CLI command that applies them.

This follows from the deployment shape. A hibernating route has no
operator moment: a new binary lands and the next activator wake execs
it, with nothing in between where a one-shot command could run. Making
correct schema state depend on a step that has nowhere to happen would
be a reliability bug, not a feature.

Boot-applied is also cheaper than what happens today. The no-op case —
overwhelmingly the common one, since a hibernating app boots on every
wake — is a single indexed `SELECT` against the ledger, where
`AutoMigrate` currently introspects every table on every wake.

The one command that touches a real database is `rastrillo migration
baseline` (§7), which is deliberately manual and exists for a case that
should not arise.

## 5. The `migrate` package

A new package, `amadan.net/rastrillo/rastrillo/migrate`. The CLI
group is named `migration` (§6); the Go package keeps the short name,
because `migrate.Apply` reads better at the call site than
`migration.Apply` and short package names are idiomatic Go.

### 5.1 Types

```go
type Migration struct {
	ID  string               // "0007_note_archived", unique within its namespace
	SQL string               // exactly one of SQL or Fn is set
	Fn  func(*gorm.DB) error
}

type Set struct{ /* namespace + ordered migrations */ }

func FromFS(fsys fs.FS, namespace string) (*Set, error)
func MustFromFS(fsys fs.FS, namespace string) *Set
func (s *Set) Add(m Migration) *Set
func Merge(sets ...*Set) *Set
func (s *Set) Validate() error
func Apply(ctx context.Context, d *db.DB, s *Set) (Result, error)
```

`Apply` takes a `*db.DB` and a `ctx`, not the `*gorm.DB` this section
first proposed. The whole run has to happen on one pinned
`*sql.Conn`: `PRAGMA foreign_keys` is per-connection state and
SQLite's twelve-step rebuild must toggle it outside the transaction,
so a pooled handle would be a correctness bug rather than a slow path
— and only `*db.DB` can hand out that connection. The `ctx` carries
the boot deadline down into a Go migration's own GORM calls, which
would otherwise run against `context.Background()`. `apply.go` argues
both at length.

`Validate` reports a repeated ID. `Merge` returns a `*Set` and no
error, which is what lets a call site read as plain apply order, so a
composed set is checked here instead; `Apply` calls it before running
anything. A duplicate would otherwise be skipped in silence — the
ledger keys on ID alone, so the second migration carrying one looks
exactly like a migration another instance already applied.

`Result` reports what the call did — migrations applied, migrations
already present, and whether it took the adoption path (§7) — so an
app can log one line at boot instead of nothing.

`FromFS` reads only files matching `NNNN_name.sql`, lexically ordered.
That pattern is what lets `schema.sql` (§6.1) live in the same
directory without ever being applied.

`Merge`'s argument order is apply order. This is how `auth` gets its
requirement — today a comment in `auth/store.go` reading "Both
statements must come after `sessions.Migrations`" — enforced at the
call site instead of by prose.

### 5.2 The ledger

```sql
CREATE TABLE rastrillo_migrations (
  id         TEXT PRIMARY KEY,   -- "sessions/0001_init", "notes/0007_note_archived"
  applied_at TEXT NOT NULL,
  checksum   TEXT NOT NULL
);
```

IDs are namespaced by their `Set`, so app and subsystem migrations
share one ordered space without colliding. The checksum is over the
SQL text with whitespace normalised, so reformatting does not raise a
false alarm.

### 5.3 The call site

The two-mechanism boot collapses to one call:

```go
r, err := migrate.Apply(ctx, d, migrate.Merge(sessions.Schema, auth.Schema, notes.Schema))
if err != nil {
	return nil, err
}
logger.Info("migrate", "applied", r.Applied, "skipped", r.Skipped, "adopted", r.Adopted)
```

Each subsystem package replaces its `Migrations []string` with a
`Schema *migrate.Set`. This is a breaking change for any app already
on `Migrations`; Rastrillo is pre-1.0 and ships PR-per-change, so the
old export is removed rather than deprecated in place.

`AutoMigrate` stops being a boot mechanism entirely. It survives as an
input to the diff engine at development time (§5.4), which means the
additive-only rule in `SKILL.md` §2 can be retired: generated SQL can
use `gormlite`'s rebuild path.

### 5.4 The diff engine

`generate` and `check` require no database. Both sides of the diff are
computed in memory:

- **Desired schema:** open `:memory:`, `AutoMigrate` every model into
  it, read back the structure.
- **Current schema:** open a second `:memory:`, replay every migration
  file into it, read back the structure.

Structure is read via `sqlite_master` plus `PRAGMA table_info` and
`PRAGMA index_list`, so the comparison is a structured diff of tables,
columns, types and indexes — not a text diff of DDL, which would be
defeated by formatting.

Deltas emit as `CREATE TABLE` and `ALTER TABLE ... ADD COLUMN` where
SQLite permits, and fall through to `gormlite`'s twelve-step rebuild
construction where it does not.

`schema.sql` is the dump of the second in-memory database, which makes
`check` exactly the question "do these two in-memory databases match?"
It runs in CI with no database, no fixtures and no network.

### 5.5 How the CLI reaches the app's models

`rastrillo` is a standalone binary; it cannot import the app's model
structs directly, and reimplementing GORM's struct-tag-to-DDL mapping
by parsing `models.go` would duplicate GORM badly and drift from it.

Instead the app declares its models, and the CLI compiles against them.
The scaffold adds one line to `models.go`:

```go
// Models is every model AutoMigrate manages. rastrillo migration
// generate reads it to compute the desired schema.
var Models = []any{&User{}, &Note{}}
```

`generate` and `check` then write a temporary program into the app
module that imports the app package and `migrate`'s dump helper,
`go run` it, and read the resulting schema back as JSON on stdout. The
temporary file is removed afterwards. This is the same approach
`atlas-provider-gorm` takes for the same reason, and it reuses a
constraint `rastrillo dev` already relies on: the app module compiles.

Two honest costs. The commands need a working Go toolchain and a
compiling app, so a broken build fails `generate` with the compiler's
own error rather than a schema diff. And they are `go run`-slow —
around a second — rather than instant. Both are acceptable for commands
run at the moment a model changes, not in a hot loop.

`Models` is also the single place `migrate` and any future tooling
learn what the app's models are, which is why it lives in `models.go`
next to the structs rather than in `app.go`.

## 6. The CLI

Four subcommands under a new `migration` group, alongside the existing
flat `new` / `generate` / `dev` — plus `baseline`, the manual adoption
escape hatch documented in §7:

```
rastrillo migration generate [dir]     diff models → new NNNN_*.sql + refreshed schema.sql
rastrillo migration new <name> [dir]   empty NNNN_<name>.sql to hand-write
rastrillo migration status [dir] --db  applied vs pending against a real database
rastrillo migration check [dir]        CI gate: models and migrations agree
```

The group is a noun, not `migrate`, for two reasons. `rastrillo
generate migration` is unavailable: `rastrillo generate [dir]` already
takes a positional directory defaulting to `.`, so the subword would be
ambiguous with a directory named `migration`. And `rastrillo migrate`,
typed bare, reads as "apply my migrations now" — which is precisely what
this CLI does not do (§4). A noun group promises nothing.

This introduces two `generate` commands, the routing one and the
migration one. They are consistent: in both, `generate` means "emit a
derived artifact from source you wrote."

### 6.1 On disk

Added to `SKILL.md`'s five-file shape:

```
internal/notes/migrations/0001_init.sql
internal/notes/migrations/0008_note_archived.sql
internal/notes/migrations/schema.sql        accumulated snapshot, checked in
internal/notes/migrations.go                //go:embed + var Schema
```

```go
//go:embed migrations/*.sql
var migrationFS embed.FS

var Schema = migrate.MustFromFS(migrationFS, "notes")
```

### 6.2 The daily loop

1. Edit `models.go` — add `Archived bool`.
2. `rastrillo migration generate` writes `0008_note_archived.sql` and
   updates `schema.sql`.
3. Read the SQL. Commit both files.
4. CI runs `rastrillo migration check`.
5. The next activator wake applies it.

### 6.3 Two guardrails

**Destructive changes need consent.** If the diff contains a column
drop or a narrowing type change, `generate` refuses to write it, prints
exactly what it would have done, and directs the user to re-run with
`--allow-destructive`. Nothing is silently skipped either: `check`
still fails until the migration exists, so the decision must be made
rather than drifted past.

**Renames cannot be inferred.** `Title` → `Heading` is
indistinguishable from a drop plus an add, and a heuristic that guesses
wrong destroys real data. The documented path is `rastrillo migration
new rename_title`, hand-writing `ALTER TABLE notes RENAME COLUMN title
TO heading`; `generate` then sees no diff. Rails behaves the same way,
and `SKILL.md` states it plainly.

### 6.4 `rastrillo dev`

`dev` gains `migrations/` in its watch list, and warns on drift —
*"models and migrations disagree; run `rastrillo migration generate`"* —
without ever generating one. Generating a migration is a decision, not
a save-side-effect.

## 7. Adopting databases that already exist

Deployed apps have `sessions`, `auth_sessions`, `notes` and no ledger.
An empty ledger must not mean "replay everything": migration
`0005_add_column` would fail on a column already present, which is the
failure `isDuplicateColumn` currently papers over.

First boot with no `rastrillo_migrations` table branches on whether the
database has user tables (`sqlite_master` rows of type `table` not
prefixed `sqlite_`):

- **Empty database** — apply everything normally. The common case, and
  the only case for new apps.
- **Non-empty, live schema matches the full migration set replayed in
  memory** — stamp every migration as applied and run no DDL. Clean
  adoption with no operator, which §4 requires.
- **Non-empty, schema does not match** — refuse to boot and log the
  structural diff. Applying arbitrary DDL to a database the runner
  cannot account for is worse than failing loudly, and `/healthz` means
  the platform notices at once.

The third case needs an escape hatch: `rastrillo migration baseline
--db <path> [--through NNNN]` stamps the ledger after a human has read
the diff.

This puts a hard constraint on the conversion in §5.3: each subsystem's
`0001_init.sql` must produce structure equivalent to today's
`Migrations []string`, or every deployed app lands in the refuse-to-boot
branch. Each converted package carries a test asserting exactly that.

## 8. Transactions and the SIGKILL window

SQLite DDL is transactional, which the runner exploits. Each migration
runs in its own `BEGIN IMMEDIATE` transaction, with its ledger row
inserted inside that same transaction. Three properties follow:

- A wake killed mid-migration rolls back cleanly; the next wake retries
  from the same point.
- Progress is preserved across wakes, so a long set converges even if
  every wake is cut short by the activator's SIGKILL budget.
- `BEGIN IMMEDIATE` takes the write lock up front, so two instances
  booting simultaneously cannot both apply — the second finds the row
  committed.

One wrinkle to design in rather than retrofit: `db.Open` sets
`foreign_keys(1)`, but SQLite's twelve-step rebuild requires `PRAGMA
foreign_keys=OFF` *outside* the transaction, with `PRAGMA
foreign_key_check` before commit. The runner recognises a rebuild
migration and brackets it accordingly.

Migrations run on the writer pool, which is capped at one connection,
so they serialise against application writes naturally. The reader pool
stays open during a migration; WAL readers see a consistent snapshot.

## 9. Checksum enforcement

Checksums answer "was an applied migration edited after it shipped?",
which requires the ledger — so `check`, which has no database, cannot
verify them. The split:

- **`check` (no database, CI):** do models and migrations agree? This
  is the drift gate and nothing else.
- **Boot and `status --db`:** does any applied migration's checksum
  differ from its file? If so the deployed database received different
  SQL than the repository claims. Boot refuses, naming the fix —
  migrations are immutable once applied; add a new one.

## 10. Tests

The diff engine is unusually testable: table-driven over (models,
existing migrations) → expected SQL, with no database and no fixtures.
Beyond that, four suites carry the real weight:

1. **Ledger behaviour** — idempotent re-apply, ordering, `Merge`
   argument order, checksum mismatch, rollback on a failing migration.
2. **Adoption** — build a database from a package's current
   `Migrations []string`, boot the new runner against it, assert it
   stamped the ledger and ran zero DDL.
3. **Example drift** — every example app passes `migration check` in
   CI, so the framework catches its own drift.
4. **Harness ergonomics** — `notestest`, `blogtest` and `ticketstest`
   move to `migrate.Apply` with the app's `Schema`, proving the
   ergonomics where they are felt.

## 11. Phasing

This touches five framework packages, the generator, three examples and
`SKILL.md`, so it lands in four steps:

1. `migrate`, with its tests, as pure addition — nothing else changes.
2. Convert the subsystem packages, each with its schema-equivalence
   adoption test.
3. The `migration` CLI group and the `dev` drift warning.
4. Scaffold, examples and `SKILL.md` together — these three have to
   agree, so they move as one.
