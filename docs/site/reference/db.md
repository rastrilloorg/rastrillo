# 🤖 db

`github.com/carlosframework/rastrillo/db`

Opens the app's SQLite database the way a CARLOS app needs it, exposed
as one `*gorm.DB`. Four symbols, and almost all of the value is in what
`Open` gets right rather than in the surface.

[Data](/docs/data) is the guide.

## Open

```go
func Open(path string, log *slog.Logger) (*DB, error)
```

Opens one file as two pools and returns a handle wrapping both. A nil
logger defaults to `slog.Default()`.

What it sets, and why each one matters:

- **DSN pragma order.** `busy_timeout` is set *before*
  `journal_mode=WAL`; the reverse order crashes under concurrent open.
  `foreign_keys(1)` is on.
- **One writer connection.** SQLite allows one writer, and queueing
  inside the pool beats `SQLITE_BUSY` at the call site.
- **Several reader connections** — `runtime.NumCPU()`, floor of four.
  WAL supports many readers, and serialising them behind one connection
  turns an open `*sql.Rows` plus any second query into a silent
  deadlock.
- **`dbresolver` routing**, so app code never picks a pool.
- **An eager ping** on both pools, so the file exists on disk from boot.
  That is what keeps hibernation replication happy.
- **`TranslateError: true`**, so `errors.Is(err, gorm.ErrDuplicatedKey)`
  works. Without the flag GORM never calls the dialector's `Translate`
  at all.
- **UTC times**, via `NowFunc`.
- **A GORM logger writing into your `*slog.Logger`**, at warn level,
  with a 200ms slow-query threshold and `ErrRecordNotFound` ignored — a
  scoped by-id miss is the ordinary 404 path and happens on every
  not-yours URL.

## DB

```go
type DB struct {
	G *gorm.DB
}
```

`G` is the handle your models and handlers use. The two `*sql.DB` pools
behind it are unexported.

## DB.Writer

```go
func (d *DB) Writer() *sql.DB
```

The write pool — one connection. `migrate.Apply` takes it directly
rather than through the resolver, because it pins a single connection
for a whole run: `PRAGMA foreign_keys` is per-connection state, and
SQLite's twelve-step table rebuild must toggle it outside the
transaction.

`d.G.DB()` returns the same pool through GORM, and is what most code
reaches for when handing a `*sql.DB` to a `database/sql` package like
[`sessions`](/docs/reference/sessions).

## DB.Close

```go
func (d *DB) Close() error
```

Closes both pools. Returns the writer's error in preference to the
reader's, so a single returned error names the pool whose failure
matters.
