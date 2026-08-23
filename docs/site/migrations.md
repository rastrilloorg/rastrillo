# 🤖 Migrations

A Rastrillo app's schema changes through numbered migrations that apply
once each, at boot, recorded in a ledger. They are never re-run and
never reversed.

This page is longer than most, because two of its rules have a failure
mode that costs you data if you get them wrong, and both are avoidable
once you have read them once.

## The shape

Two `*migrate.Set` values live in `internal/<app>/migrations.go`, beside
`models.go`:

```go
// Schema is the app's own migrations — what generate and check diff
// against Models.
var Schema = migrate.MustFromFS(migrationFS, "notes")

// BootSchema is everything App() applies, in apply order.
var BootSchema = migrate.Merge(sessions.Schema, Schema)
```

`migrate.Merge`'s argument order **is** apply order. That is how a
package which must run after another states the requirement at the call
site rather than in a comment — `auth` after `sessions`, because auth's
tables reference sessions'.

Add a subsystem to `BootSchema`, never to `Schema`. `Schema` is what
`migration check` diffs against your models, so a subsystem's tables
there would look like tables your models do not know about, and `check`
would propose dropping them.

`App()` applies the set once at boot:

```go
if _, err := migrate.Apply(context.Background(), d, BootSchema); err != nil {
	return nil, err
}
```

## Changing a model

Edit the struct, then:

```sh
rastrillo migration generate
```

It diffs your models against your migrations and writes the delta as a
new numbered file. **Read the SQL before you commit it.** `generate` may
emit a full table rebuild rather than an `ALTER TABLE`, which is
correct — SQLite's `ALTER` is limited — but it is worth seeing.

Then `rastrillo migration check` in CI keeps them from drifting apart;
`make ci` in a scaffolded app already runs it.

### Things generate cannot infer

A **rename** is indistinguishable from a drop plus an add to any tool
looking only at the before and after. Write it by hand:

```sh
rastrillo migration new rename_title_to_heading
```

which writes a numbered stub for you to fill in:

```sql
ALTER TABLE notes RENAME COLUMN title TO heading;
```

A change that **drops data** is refused unless you pass
`--allow-destructive`, and the refusal prints the SQL it would have
written so you can see what it considers destructive.

### Never edit a shipped migration

Applied SQL is immutable to the ledger: each row records a checksum, and
`Apply` refuses to boot when a migration's SQL no longer matches what
was recorded. Add a new migration instead.

Whitespace and formatting are ignored in that comparison, so
reformatting an old migration is safe. Changing what it does is not.

## What Apply guarantees

Every migration runs in its own `BEGIN IMMEDIATE` transaction, with its
ledger row written inside that same transaction, on one pinned
connection.

Three properties follow, and all three matter on a platform that can
SIGKILL a hibernating app at any moment:

- A wake killed mid-migration rolls back cleanly, and the next wake
  retries from the same point.
- Progress is preserved across wakes, so a long set converges even if
  every wake is cut short.
- Two instances booting at once serialise onto the same migration. The
  loser blocks on the lock, then re-checks the ledger, finds the row the
  winner just committed, and skips — rather than re-running it or
  failing its boot.

The single pinned connection is a correctness requirement, not a
performance choice: `PRAGMA foreign_keys` is per-connection state, and
SQLite's twelve-step table rebuild has to toggle it outside the
transaction.

### There is no Down

Migrations are forward-only. Production rollback for a CARLOS app is a
point-in-time restore of the SQLite file the activator replicates, not a
`Down` function, so none is offered.

### Go migrations

`migrate.Migration` sets exactly one of `SQL` or `Fn`. `Fn` is the
escape hatch for a change SQL cannot express, and it runs on the same
pinned connection inside the same transaction, so a failure rolls its
writes back with the ledger row.

**A Go migration must not reference your live model structs.** A model
changes over time, and would silently change the meaning of a migration
that already ran. Copy the struct into the migration file.

## The first deploy of a version with migrations must be schema-neutral

If your app is already deployed and you are adding migrations for the
first time, generate `0001_init` from the models **as already
deployed**, and ship it alone. Change a model only in a later release.

Otherwise boot refuses on the new column — and `baseline`, the tool you
would reach for, would strand that migration for good.

## Recovering an old database

Boot refuses on a structural diff. Two ways to get there: a real
database predates a subsystem's migrations, or a manifest resource's
generated `gen/store/<name>/migrations.go` got reshaped after first
boot, so its regenerated SQL no longer matches the ledger's checksum.

Either way the recovery is the same four steps, and **their order is
load-bearing**:

1. Note which migration the database already matches.
2. `rastrillo migration baseline --db <path> --through <id>` — stamping
   **only** up to that id.
3. Apply the missing migration by hand.
4. Reboot. The remaining migrations run normally.

### Why steps 2 and 3 are in that order

Do not tidy them the other way round. `Stamp` runs no DDL, so `baseline`
does not need the missing table to exist yet.

But between creating the table by hand and stamping, the database
structurally matches the full set **with an empty ledger** — and that is
exactly the state `Apply` adopts, because adoption is gated on the
ledger being empty. On a hibernating platform you do not choose when the
app wakes. One inbound request in that window adopts the schema, stamps
the later migration without running it, and strands every row it was
supposed to backfill, for good.

Stamping first makes the ledger non-empty, which closes that window
permanently. The worst case then becomes a wake between steps 2 and 3,
where the later migration fails loudly with `no such table` and rolls
back — and the reboot in step 4 still runs the backfill. A loud refusal
beats a silent stranding.

### The trap

`baseline` with no `--through` stamps **every** migration, including the
ones that have not run. Any pending data migration is then recorded as
applied without ever running, and the rows it would have backfilled are
stranded silently.

That is what bare `baseline` is for, and it is why the command is manual
by design and why the CLI will not apply migrations for you. Pass
`--through` unless you have specifically decided otherwise.

`auth/store.go`'s package comment walks the whole thing with real
migration ids, if you would like to see it worked through.

## Seeing what a database has applied

```sh
rastrillo migration status --db ./notes.db
```

Reports what the ledger records and any pending drift. A database that
has never booted this version gets a plain "no ledger yet" line rather
than an error.
