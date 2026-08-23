# 🤖 migrate

`github.com/carlosframework/rastrillo/migrate`

One ledgered schema mechanism: an ordered, namespaced set of migrations,
applied exactly once each and recorded. It replaced the two mechanisms a
Rastrillo app used to run side by side — GORM `AutoMigrate` for models
and a raw `Migrations []string` for framework subsystems.

[Migrations](/docs/migrations) is the guide, and it covers the two
failure modes that cost data.

## Migrations are forward-only

There is no `Down`. Production rollback for a CARLOS app is a
point-in-time restore of the SQLite file the activator replicates, so
none is offered instead of offered and untrustworthy.

## Apply

```go
func Apply(ctx context.Context, d *db.DB, s *Set) (Result, error)
```

Runs every migration the ledger does not already record, in order, each
in its own `BEGIN IMMEDIATE` transaction with its ledger row written
inside that same transaction, all on one pinned connection.

Three properties follow, and they matter on a platform that can SIGKILL
a hibernating app mid-wake. A killed migration rolls back cleanly and
the next wake retries. Progress survives across wakes. And two instances
booting at once serialise, the loser re-checking the ledger and skipping
instead of re-running or failing.

The pinned connection is correctness, not speed. `PRAGMA foreign_keys`
is per-connection, and SQLite's table rebuild has to toggle it outside
the transaction.

`Result` reports `Applied`, `Skipped` and `Adopted`, so your app can log
one line at boot.

## Set and Merge

```go
type Set struct{ /* ... */ }

func FromFS(fsys fs.FS, namespace string) (*Set, error)
func MustFromFS(fsys fs.FS, namespace string) *Set
func Merge(sets ...*Set) *Set
```

`Merge`'s argument order is apply order. That is how a package which
must run after another states the requirement at the call site instead
of in a comment: `auth` after `sessions`, because auth's backfill reads
the sessions table.

Your app declares two sets. `Schema` holds your own migrations, and is
what `generate` and `check` diff against `Models`. `BootSchema` is
`migrate.Merge(sessions.Schema, Schema)`, everything `App()` applies.
Add a subsystem to `BootSchema`, never `Schema`, or `check` proposes
dropping a table your models do not know about.

`Set.Add` appends, `Set.All` returns the migrations in order, and
`Set.Validate` reports a malformed set — a bad id, a duplicate within a
namespace. The same id in different namespaces is fine, which is what
lets every subsystem number from 0001.

## Migration

```go
type Migration struct {
	ID  string
	SQL string
	Fn  func(*gorm.DB) error
}
```

Exactly one of `SQL` or `Fn` is set. `SQL` is the default and the only
thing `rastrillo migration generate` emits.

`Fn` is the escape hatch for a change SQL cannot express. It runs on the
same pinned connection inside the same transaction as its ledger row, so
a failure rolls its writes back too — `Apply` builds it a `*gorm.DB`
backed by that one connection rather than your pool.

Do not reference your live model structs from a Go migration. A model
changes over time and would silently change the meaning of a migration
that already ran. Copy the struct into the migration.

## Checksum and immutability

```go
func Checksum(sql string) string
```

Every ledger row records one, and `Apply` refuses to boot when a shipped
migration's SQL no longer matches what was recorded.

Whitespace and formatting are ignored, so reformatting an old migration
is safe. Changing what it does is not: add a new migration.

## Generating and diffing

```go
func Generate(ctx context.Context, ms []Migration, models []any) ([]Change, error)
func SchemaSQL(ctx context.Context, ms []Migration) (string, error)
```

`Generate` is what `rastrillo migration generate` and
`rastrillo migration check` both run. It replays the migrations into an
in-memory database, compares the result against your models, and returns
the `Change` list that would close the gap. A `Change` carries its `SQL`
and whether it is `Destructive`.

`SchemaSQL` returns the schema the migrations produce, without a
database.

## Reading a real database

```go
func Read(ctx context.Context, q Querier) (Snapshot, error)
func Replay(ctx context.Context, ms []Migration) (*Memory, error)
func Stamp(ctx context.Context, conn *sql.Conn, ms []Migration, through string) error
```

`Read` snapshots a live schema — its `Table`, `Column` and `Index`
values — through any `Querier`, the small interface both a `*sql.Conn`
and a `*Memory` satisfy. `Replay` builds a `*Memory` containing what the
migrations produce, so the two can be compared: `Snapshot.Equal` ignores
DDL formatting and `Snapshot.Diff` names what differs.

`Stamp` records migrations as applied without running them, up to
`through`. It is what `rastrillo migration baseline` calls, and the
guide explains why passing `through` matters and why bare stamping is
dangerous.

`LedgerDDL` is the ledger table's own DDL, exported so `baseline` can
create the table without duplicating its shape.
