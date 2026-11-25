# 🤖 Declaring tables

Every table in your database says whose data it holds, how to find a
person's rows in it, and how erasure treats them. You write that down
once, beside the migration that creates the table, as a declaration.
A test fails when a table has none.

This version adds the declarations and the check. It does not add an
audit log, an export or erasure. Those need the declarations, so they
come after.

## Declare a table

Call `Declare` on the `*migrate.Set` that holds the table's migrations:

```go
var Schema = migrate.MustFromFS(migrationFS, "notes").Declare(migrate.Data{
	Name:   "notes",
	Key:    []string{"id"},
	Kind:   migrate.Personal,
	Owner:  migrate.UserID("user_id"),
	Erase:  migrate.EraseRow,
	Export: true,
})
```

`Declare` stamps the set's namespace on the declaration and returns the
set, so calls chain. A merged set keeps every declaration, in the order
you merged the sets.

| Field | Meaning |
|---|---|
| `Name` | The table. |
| `Key` | The primary key columns. Use `"rowid"` alone for a table without one. |
| `Kind` | `Personal` for data about people, `Shared` for data that belongs to no one person, `System` for the framework's own bookkeeping. |
| `Owner` | The column that ties a row to its person. `migrate.Subject`, `Address` and `UserID` say what the column holds. `migrate.Via(column, parent)` reaches the person through a declared parent table. |
| `Fields` | Other personal columns, each with what it holds (`HoldsSubject`, `HoldsAddress`, `HoldsUserID`, `HoldsFreeText`) and an erase treatment. |
| `Recorded` | Columns the audit log may keep a copy of. Neither the `Owner` column nor a column in `Fields` can also be recorded, because the log would keep what erasure removes. |
| `NotAudited` | Why changes to this table stay out of the audit log. Empty means the table is audited. |
| `Erase` | What erasure does with the table: `EraseRow`, `EraseNull`, `EraseTombstone` or `EraseKeep`. |
| `KeepReason` | Required with `EraseKeep`. |
| `Export` | Whether a person's export includes this table. |

A personal table needs an `Erase`. If it deletes rows, or nulls or
tombstones any field, it also needs an `Owner`, so there is a way to find
whose rows they are.

## Who is a person

One person can appear in a table under their subject, their email address
or their numeric user ID. `privacy.Identity` holds all three, and
`Matches` tells you whether a value in a column of a given kind stands for
that person. Free text never matches: a sentence is not an identifier.

`auth.Auth.Identify` builds the `Identity` for a subject. How it finds
the addresses depends on how your app signs people in.
The first applicable rule wins, in this order: blind links, blind
records, records mode, `SubjectFor`, then the default.

- By default the subject is the address.
- With a records-mode people store it reads the user record.
- With `Config.SubjectFor`, it calls `Config.AddressesFor`. Without that
  function it returns `privacy.ErrUnidentifiable`.
- With blind links or blind records only hashes are kept, so `Addresses`
  is empty and matching works on the subject alone.

## Check in your tests

`audit.CheckSchema` applies your boot set to an empty in-memory database
and compares the tables it creates with your declarations:

```go
func TestSchemaIsDeclared(t *testing.T) {
	audit.CheckSchema(t, notes.BootSchema)
}
```

It fails the test, once per problem, for a table with no declaration, a
declaration whose table no migration creates, a table declared twice, a
declaration with no `Key` or no `Kind`, a key, owner or field column
that does not exist, an owner with no column or one that holds no
subject, address or user ID, an owner parent that is not declared, a
recorded column that is the owner or in `Fields`, a personal table with
no `Erase` or no `Owner` where it needs one, and `EraseKeep` with no
reason. FTS5 shadow tables of a declared virtual table need no
declaration. `rastrillo_migrations`, `sqlite_` tables and `_litestream_`
tables never do.

Boot does not refuse an undeclared table yet. `migrate.Apply` returns the
same problems on `Result.Problems` and carries on, so a missing
declaration shows in your tests, not as a failed deploy.

## Declare what you merge

Each core package that owns tables declares them in its own `Schema`, so
merging `sessions.Schema`, `auth.Schema` and the rest into `BootSchema`
brings their declarations with them. Addons do the same: an addon
declares its tables on the set it ships, and you merge it as usual.

- **Generated stores.** `rastrillo generate` writes a `declare.go` for each
  store, with a `Declaration` for its table. Declare it on the set that
  wraps the store's migrations. Rerun `rastrillo generate` after upgrading.
  Until you do, `generate --check` fails because the new files are
  missing. Your schema test also fails until you declare the table on
  that set.
  `declaration` and `declarations` cannot be resource names, because the
  generated package already uses that name.
- **Mergeable stores.** Their data lives in `eventlog`, so `eventlog.Schema`
  must be in the `BootSchema` you check.
- **`Options.Migrations`.** An app on the older list of statements has no
  set to check. Build a `BootSchema` from the list, declare the tables on
  it and check that. `examples/tickets/internal/tickets/schema.go` shows
  the pattern.
- **A migration that drops another package's table.** Call `Retire` on the
  set with the table's name, so the declaration ends where the table does.
  It only removes declarations from sets merged before it, so merge it
  after the package that owns the table.

## The audit and privacy packages

`audit.CheckSchema(t, set)` performs the check described above.
`audit.Exemptions(set)` returns the `audit.Exemption` values that
`rastrillo audit exemptions` prints. Each has the table, its namespace,
`What` (`not audited` or `kept on erasure`) and the `Reason`.
`privacy.NewIdentity(subject, addresses)` builds an `Identity` with the
addresses trimmed, sorted and deduplicated, and the domain lower-cased.
`privacy.Identifier` is the interface that `auth.Auth` satisfies through
`Identify`. `ErrUnidentifiable` is returned when an app cannot list a
person's addresses.

## List the exemptions

```sh
rastrillo audit exemptions
```

prints every table that is not audited or is kept on erasure, with the
reason it gave, so a reviewer can read them in one place. See
[the CLI](/docs/cli#audit-exemptions).

## What this version leaves out

There is no audit log, export or erasure yet. Data outside the database is
out of scope: files in the object store, copies of `eventlog` on other
machines, and member lists that a root app syncs to its instances (see [roster](/docs/reference/roster)). A declaration says
nothing about them.

Inline blobs (`blobs.Inline`) are different: their bytes are in the
database, in the `blobs` table, and may hold personal data. That table
has no owner column, so it is declared personal and kept on erasure
until erasure can tell whose a blob is.
