# 🤖 db

`amadan.net/rastrillo/rastrillo/db`

Opens your SQLite database the way a CARLOS app needs it, as one
`*gorm.DB`. It is a small package, and almost all its value is in what
`Open` gets right. [Data](/docs/data) is the guide.

## Open

```go
func Open(path string, log *slog.Logger) (*DB, error)
```

Opens one file as two pools and returns a handle wrapping both. A nil
logger defaults to `slog.Default()`.

What it sets for you, and why each one is there:

- **DSN pragma order.** `busy_timeout` before `journal_mode=WAL`; the
  other order crashes under concurrent open. `foreign_keys(1)` is on.
- **`synchronous(NORMAL)`.** In WAL mode, most commits skip the extra
  sync to disk, which can make writes faster. With WAL, a power cut can
  undo recent commits without corrupting the database.
- **One writer connection.** SQLite allows one writer, and queueing
  inside the pool beats getting `SQLITE_BUSY` at the call site.
- **Several reader connections** — `runtime.NumCPU()`, floor of four.
  WAL supports many readers, and serialising them behind one connection
  turns an open `*sql.Rows` plus any second query into a silent
  deadlock.
- **`dbresolver` routing**, so your code never picks a pool.
- **An eager ping** on both pools, so the file exists on disk from boot.
  That keeps hibernation replication happy.
- **`TranslateError: true`**, so `errors.Is(err, gorm.ErrDuplicatedKey)`
  works. Without the flag GORM never calls the dialector's `Translate`.
- **UTC times.**
- **A GORM logger writing into your `*slog.Logger`**, at warn level, with
  a 200ms slow-query threshold and `ErrRecordNotFound` ignored — a
  scoped by-id miss is the ordinary 404 path and happens on every
  not-yours URL.

## DB

```go
type DB struct {
	G *gorm.DB
}
```

`G` is the handle your models and handlers use. `Writer` and `Reader`
return the two `*sql.DB` pools behind it.

## DB.Writer

```go
func (d *DB) Writer() *sql.DB
```

The write pool — one connection. `migrate.Apply` takes it directly
rather than through the resolver, because it pins a single connection
for a whole run: `PRAGMA foreign_keys` is per-connection state, and
SQLite's twelve-step table rebuild has to toggle it outside the
transaction.

`d.G.DB()` gets you the same pool through GORM, and is what most code
uses when handing a `*sql.DB` to a `database/sql` package like
[`sessions`](/docs/reference/sessions).

## DB.Reader

```go
func (pools *DB) Reader() *sql.DB
```

The read pool has several connections, so reads can run while a write
is in progress. Pass it as `Reader` to
[`sessions`](/docs/reference/sessions) or
[`auth`](/docs/reference/auth) only if a mixed-load test on your
instance shows it helps. On one core, under heavy mixed load, it slows
writes.
Only read through it. Writes go through `Writer`, which keeps them to
one connection.

## DB.Close

```go
func (d *DB) Close() error
```

Closes both pools. It returns the writer's error in preference to the
reader's, so a single returned error names the pool whose failure
matters.
