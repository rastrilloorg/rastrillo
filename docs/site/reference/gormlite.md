# 🤖 gormlite

`github.com/carlosframework/rastrillo/gormlite`

A GORM SQLite dialector over `modernc.org/sqlite`. A minimal fork of
`glebarez/sqlite` that keeps Rastrillo on current modernc without a
double driver registration.

You should not need to name this package in your app —
[`db.Open`](/docs/reference/db) already wires it. It is documented
because it is exported, because an error from it will name it, and
because the reason it exists is a trap worth knowing.

## Why it exists

`glebarez/sqlite` registers the driver name `sqlite`, which
`modernc.org/sqlite` also registers. A binary containing both panics at
init, before any of your code runs, with a duplicate-driver message that
does not obviously point at either import.

`gorm.io/driver/sqlite` is the cgo one, which defeats a pure-Go build.

So never import either. [Data](/docs/data) says the same thing where you
are more likely to be reading.

## Open and DriverName

```go
func Open(dsn string) gorm.Dialector
var DriverName = "sqlite"
```

`Open` builds a dialector from a DSN. `db.Open` calls it with the pragma
ordering that has to be right.

## Dialector

```go
type Dialector struct {
	Conn *sql.DB
	// ...
}
```

Implements GORM's `Dialector`: `Dialector.Initialize`,
`Dialector.Name`, `Dialector.Migrator`, `Dialector.DataTypeOf`,
`Dialector.DefaultValueOf`, `Dialector.BindVarTo`, `Dialector.QuoteTo`,
`Dialector.Explain`, `Dialector.ClauseBuilders`,
`Dialector.SavePoint`, `Dialector.RollbackTo`, and
`Dialector.Translate`.

`Translate` is the one worth naming. It turns the driver's constraint
errors into GORM's portable sentinels, so `errors.Is(err,
gorm.ErrDuplicatedKey)` works. GORM only calls it when you open with
`TranslateError: true`, which `db.Open` does.

Passing a `Conn` uses an existing pool instead of opening a new one,
which is how `db.Open` hands GORM the writer pool and the reader pool
separately.

## Migrator

```go
type Migrator struct{ /* ... */ }
```

GORM's migrator over SQLite, implementing the table, column, index and
constraint surface: `Migrator.HasTable`, `Migrator.GetTables`,
`Migrator.DropTable`, `Migrator.HasColumn`, `Migrator.AlterColumn`,
`Migrator.DropColumn`, `Migrator.ColumnTypes`, `Migrator.HasIndex`,
`Migrator.CreateIndex`, `Migrator.DropIndex`, `Migrator.GetIndexes`,
`Migrator.RenameIndex`, `Migrator.BuildIndexOptions`,
`Migrator.HasConstraint`, `Migrator.CreateConstraint`,
`Migrator.DropConstraint`, `Migrator.CurrentDatabase`, and
`Migrator.RunWithoutForeignKey`.

`ErrConstraintsNotImplemented` is returned by the constraint operations
SQLite cannot perform in place. `Index` is the index shape
`GetIndexes` returns.

Do not use this migrator to change your schema. Schema changes go
through [`migrate`](/docs/reference/migrate), which applies numbered
migrations once each and records them. GORM's migrator is here because
the dialector interface requires it, and because `migrate`'s diff engine
drives it against an in-memory database to work out what a change would
be.
