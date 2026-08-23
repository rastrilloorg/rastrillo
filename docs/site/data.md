# 🤖 Data

Your app keeps its data in one SQLite file, reached through one
`*gorm.DB`. This page covers the models you write and the handle
`db.Open` gives you. [Migrations](/docs/migrations) covers changing the
schema afterwards, and [Scoping](/docs/scoping) covers keeping one
user's rows away from another's.

## Models are plain GORM structs

No base type, no embedding, nothing to inherit:

```go
type User struct {
	ID           int64
	Email        string `gorm:"uniqueIndex"`
	PasswordHash string
}

type Note struct {
	ID        int64
	UserID    int64 `gorm:"index"`
	Title     string
	Body      string
	UpdatedAt time.Time
}
```

`UserID` is the owner column. It is an ordinary field with an ordinary
index. What makes it special is that every query on `Note` goes through
the owner filter — see [Scoping](/docs/scoping).

Models live in `internal/<app>/models.go`, and a `Models` slice beside
them is what `rastrillo migration generate` diffs against your
migrations.

## Opening the database

```go
d, err := db.Open("notes.db", logger)
if err != nil {
	return err
}
defer d.Close()
```

`d.G` is the `*gorm.DB` your handlers use. Everything else about
`db.Open` is the stuff that is easy to get subtly wrong, so it is worth
knowing what it did for you.

### One file, two pools

Writes go through a pool capped at one connection, because SQLite allows
one writer and queueing inside the pool beats getting `SQLITE_BUSY` at
the call site. Reads go through a pool of several, sized to the CPU
count with a floor of four. WAL supports many readers, and serialising
them behind a single connection turns an open `*sql.Rows` plus any
second query into a silent deadlock.

`dbresolver` routes each statement to the right pool, so you never pick
one.

### The DSN

```text
?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)
```

`busy_timeout` is set before `journal_mode=WAL`. The other order crashes
under concurrent open. Foreign keys are on. `Open` also pings eagerly so
the file exists on disk from boot, which is what keeps hibernation
replication happy.

### Two settings worth knowing

GORM is opened with `TranslateError: true`, so driver constraint errors
become GORM's portable sentinels and `errors.Is(err,
gorm.ErrDuplicatedKey)` works. Without that flag GORM never calls the
dialector's `Translate` at all.

The logger ignores `ErrRecordNotFound`. A scoped by-id miss is the
ordinary 404 path and happens on every not-yours URL, so it is control
flow, not something worth a log line per hit.

Times are stored UTC.

## Getting a *sql.DB out

Some packages want `database/sql`. `sessions` is the first one you will
hit:

```go
writer, err := d.G.DB()
if err != nil {
	return err
}
sess, err := sessions.New(sessions.Config{DB: writer, Origin: origin, Logger: logger})
```

`d.Writer()` returns the same write pool directly, and it is the clearer
call when you specifically need the writer. `migrate.Apply` takes it
that way, because it pins one connection for a whole run:
`PRAGMA foreign_keys` is per-connection, and a table rebuild has to
toggle it outside the transaction.

`d.Close()` closes both pools.

## Never import a second SQLite driver

```go
// Do not do this.
import _ "github.com/glebarez/sqlite"
import _ "gorm.io/driver/sqlite"
```

`glebarez/sqlite` registers the driver name `sqlite`, which
`modernc.org/sqlite` already registers. A binary containing both it and
`rastrillo/gormlite` panics at init, before any of your code runs, with
a duplicate-driver message that does not obviously point at either
import.

`gorm.io/driver/sqlite` is the cgo one, which defeats a pure-Go build.

You should not need to name a SQLite driver in your app at all.
`db.Open` wires [`gormlite`](/docs/reference/gormlite) over
`modernc.org/sqlite` for you.
