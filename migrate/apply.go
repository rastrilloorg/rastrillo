package migrate

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	gosqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/carlosframework/rastrillo/db"
	"github.com/carlosframework/rastrillo/gormlite"
)

// LedgerDDL is exported so Task 10's baseline command can create the
// ledger table without duplicating its shape.
const LedgerDDL = `CREATE TABLE IF NOT EXISTS rastrillo_migrations (
  id         TEXT PRIMARY KEY,
  applied_at TEXT NOT NULL,
  checksum   TEXT NOT NULL
);`

// Result reports what a call did, so an app can log one line at boot.
type Result struct {
	Applied []string
	Skipped int
	Adopted bool
}

// Apply runs every migration in s that the ledger does not already
// record, in order, each in its own BEGIN IMMEDIATE transaction with
// its ledger row written inside that same transaction.
//
// Three properties follow from that shape, and all three matter for a
// hibernating app the activator may SIGKILL at any moment: a wake
// killed mid-migration rolls back cleanly and the next wake retries
// from the same point; progress is preserved across wakes, so a long
// set converges even if every wake is cut short; and when two
// instances boot at once, BEGIN IMMEDIATE serialises them onto the
// same migration — the loser blocks on the lock, and once it gets it,
// re-checks the ledger, finds the row the winner just committed, and
// skips instead of re-running the migration or failing its boot.
//
// The whole run happens on one pinned connection. PRAGMA foreign_keys
// is per-connection state and SQLite's twelve-step table rebuild
// requires toggling it outside the transaction, so a pooled
// connection would be a correctness bug, not just a slow path.
func Apply(ctx context.Context, d *db.DB, s *Set) (Result, error) {
	var res Result
	conn, err := d.Writer().Conn(ctx)
	if err != nil {
		return res, err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, LedgerDDL); err != nil {
		return res, fmt.Errorf("migrate: create ledger: %w", err)
	}

	migrations := s.All()
	applied, err := readLedger(ctx, conn)
	if err != nil {
		return res, err
	}

	if len(applied) == 0 {
		adopted, err := adopt(ctx, conn, migrations)
		if err != nil {
			return res, err
		}
		if adopted {
			res.Adopted = true
			res.Skipped = len(migrations)
			return res, nil
		}
	}

	// g is backed by the pinned connection itself, not the app's
	// writer pool — building it on d.G's pool would deadlock, because
	// that pool has exactly one connection (SQLite allows one writer)
	// and conn already holds it for this whole run. Running Fn through
	// this g instead means a Go migration executes inside the same
	// BEGIN IMMEDIATE transaction as its ledger row, so a failure
	// rolls both back together, same as a SQL migration.
	//
	// SkipDefaultTransaction is required, not an optimisation: without
	// it GORM wraps every Create/Update/Delete in its own
	// BeginTransaction, and *sql.Conn satisfies gorm.TxBeginner, so
	// that issues a real nested BEGIN on a connection already inside
	// BEGIN IMMEDIATE — SQLite refuses it ("cannot start a
	// transaction within a transaction"). The run is already in a
	// transaction, so GORM's per-statement one is redundant even when
	// it would work.
	g, err := gorm.Open(gormlite.Dialector{Conn: conn}, &gorm.Config{
		Logger:                 logger.Default.LogMode(logger.Silent),
		SkipDefaultTransaction: true,
	})
	if err != nil {
		return res, fmt.Errorf("migrate: open pinned gorm.DB: %w", err)
	}
	// Binds g to the boot deadline the caller gave Apply, so a Go
	// migration's own GORM calls inherit it instead of running against
	// context.Background() (gorm.Open's default) regardless of ctx.
	g = g.WithContext(ctx)

	for _, m := range migrations {
		sum, ok := applied[m.ID]
		if ok {
			if m.SQL != "" && sum != Checksum(m.SQL) {
				return res, fmt.Errorf(
					"migrate: %s was applied with different SQL than the file now contains; "+
						"migrations are immutable once applied — add a new one instead of editing this", m.ID)
			}
			res.Skipped++
			continue
		}
		didApply, err := runOne(ctx, conn, g, m)
		if err != nil {
			return res, fmt.Errorf("migrate: %s: %w", m.ID, err)
		}
		if didApply {
			res.Applied = append(res.Applied, m.ID)
		} else {
			res.Skipped++
		}
	}
	return res, nil
}

func readLedger(ctx context.Context, conn *sql.Conn) (map[string]string, error) {
	rows, err := conn.QueryContext(ctx, "SELECT id, checksum FROM rastrillo_migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, sum string
		if err := rows.Scan(&id, &sum); err != nil {
			return nil, err
		}
		out[id] = sum
	}
	return out, rows.Err()
}

// needsRebuild reports whether a migration performs SQLite's
// twelve-step table rebuild, which must run with foreign keys off and
// a foreign_key_check before commit.
func needsRebuild(sql string) bool {
	u := strings.ToUpper(sql)
	return strings.Contains(u, "DROP COLUMN") ||
		strings.Contains(u, "RENAME TO") ||
		strings.Contains(u, "__TEMP__")
}

// evict marks conn unusable so the pool discards it instead of
// reusing it. It is only called when a cleanup step (ROLLBACK, or
// restoring PRAGMA foreign_keys=ON) itself fails — typically because
// ctx was already cancelled and database/sql refused the call before
// it ever reached SQLite. Returning conn to the pool in that state
// would hand the app's only writer connection to whatever runs next
// with an open transaction still holding the write lock, or foreign
// keys silently off, for the rest of the process; database/sql never
// evicts a connection on its own just because a query on it failed.
func evict(conn *sql.Conn) {
	conn.Raw(func(any) error { return driver.ErrBadConn })
}

// isDuplicateLedgerRow reports whether err is a PRIMARY KEY or UNIQUE
// violation on the ledger insert — the ledger's own last line of
// defense if a racing instance's commit lands between runOne's
// in-transaction re-check and this INSERT. BEGIN IMMEDIATE's lock
// makes that window vanishingly unlikely, not impossible.
func isDuplicateLedgerRow(err error) bool {
	var serr *gosqlite.Error
	if !errors.As(err, &serr) {
		return false
	}
	switch serr.Code() {
	case sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY, sqlite3.SQLITE_CONSTRAINT_UNIQUE:
		return true
	default:
		return false
	}
}

// runOne applies one migration inside its own BEGIN IMMEDIATE
// transaction and reports whether this call was the one that applied
// it. false, nil means it was skipped because another instance
// already applied it — not an error; Apply counts that as Skipped.
func runOne(ctx context.Context, conn *sql.Conn, g *gorm.DB, m Migration) (applied bool, err error) {
	rebuild := m.SQL != "" && needsRebuild(m.SQL)
	if rebuild {
		// Per-connection, and it must be outside the transaction:
		// SQLite silently ignores this pragma inside one.
		if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
			return false, err
		}
		// Registered before the ROLLBACK defer below, so — defers run
		// LIFO — it fires after it: ROLLBACK ends the transaction
		// first, then this restores foreign_keys=ON outside it, same
		// as the OFF above required. Swapping the order (moving the
		// BEGIN IMMEDIATE block above this defer instead of below it)
		// would toggle the pragma while the transaction was still
		// open, where SQLite ignores it, leaving foreign keys off on
		// the app's one writer connection for the rest of the process.
		defer func() {
			// context.WithoutCancel: ctx may already be the reason
			// this migration is unwinding (a boot deadline, a
			// SIGTERM), and a cancelled context here would make
			// ExecContext refuse the call before it ever reached
			// SQLite — silently leaving foreign keys off on the app's
			// only writer connection.
			if _, restoreErr := conn.ExecContext(context.WithoutCancel(ctx), "PRAGMA foreign_keys=ON"); restoreErr != nil {
				evict(conn)
			}
		}()
	}

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return false, err
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		// Same reasoning as the pragma restore above: a refused
		// ROLLBACK on a cancelled ctx would hand the pool back a
		// connection with an open transaction still holding the write
		// lock and this migration's uncommitted rows visible to
		// whatever runs next.
		if _, rbErr := conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK"); rbErr != nil {
			evict(conn)
		}
	}()

	// BEGIN IMMEDIATE took the write lock, but readLedger ran before
	// any transaction, so a racing instance that reached here first,
	// applied m, and committed is invisible until this re-check.
	// Finding it now means we lost the race, not that anything failed.
	switch err := conn.QueryRowContext(ctx,
		"SELECT 1 FROM rastrillo_migrations WHERE id = ?", m.ID).Scan(new(int)); {
	case err == nil:
		return false, nil
	case !errors.Is(err, sql.ErrNoRows):
		return false, err
	}

	switch {
	case m.Fn != nil:
		if err := m.Fn(g); err != nil {
			return false, err
		}
	default:
		if _, err := conn.ExecContext(ctx, m.SQL); err != nil {
			return false, err
		}
	}

	if rebuild {
		rows, err := conn.QueryContext(ctx, "PRAGMA foreign_key_check")
		if err != nil {
			return false, err
		}
		if rows.Next() {
			// Columns per SQLite's docs for this pragma: the table
			// holding the violation, its rowid (NULL for a WITHOUT
			// ROWID table), the parent table the missing foreign key
			// should reference, and the index of the failing foreign
			// key definition within the table.
			var table, parent string
			var rowid sql.NullInt64
			var fkid int
			scanErr := rows.Scan(&table, &rowid, &parent, &fkid)
			rows.Close()
			if scanErr != nil {
				return false, fmt.Errorf("foreign_key_check: reading violation row: %w", scanErr)
			}
			return false, fmt.Errorf(
				"foreign_key_check failed after rebuild: table %s row %v has a foreign key (definition #%d) referencing missing row in %s — "+
					"this may be a pre-existing violation unrelated to this migration, since the pragma scans the whole database",
				table, rowid, fkid, parent)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return false, fmt.Errorf("foreign_key_check: %w", err)
		}
		rows.Close()
	}

	if _, err := conn.ExecContext(ctx,
		"INSERT INTO rastrillo_migrations (id, applied_at, checksum) VALUES (?, ?, ?)",
		m.ID, time.Now().UTC().Format(time.RFC3339Nano), Checksum(m.SQL)); err != nil {
		if isDuplicateLedgerRow(err) {
			return false, nil
		}
		return false, err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return false, err
	}
	committed = true
	return true, nil
}
