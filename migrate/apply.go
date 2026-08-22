package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"

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

// adopt is Task 4's deliverable: the three first-boot branches for a
// database that predates the ledger. Until then an empty ledger always
// takes the normal apply path.
func adopt(ctx context.Context, conn *sql.Conn, ms []Migration) (bool, error) { return false, nil }

// Apply runs every migration in s that the ledger does not already
// record, in order, each in its own BEGIN IMMEDIATE transaction with
// its ledger row written inside that same transaction.
//
// Three properties follow from that shape, and all three matter for a
// hibernating app the activator may SIGKILL at any moment: a wake
// killed mid-migration rolls back cleanly and the next wake retries
// from the same point; progress is preserved across wakes, so a long
// set converges even if every wake is cut short; and BEGIN IMMEDIATE
// takes the write lock up front, so two instances booting at once
// cannot both apply.
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
	g, err := gorm.Open(gormlite.Dialector{Conn: conn}, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return res, fmt.Errorf("migrate: open pinned gorm.DB: %w", err)
	}

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
		if err := runOne(ctx, conn, g, m); err != nil {
			return res, fmt.Errorf("migrate: %s: %w", m.ID, err)
		}
		res.Applied = append(res.Applied, m.ID)
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

func runOne(ctx context.Context, conn *sql.Conn, g *gorm.DB, m Migration) error {
	rebuild := m.SQL != "" && needsRebuild(m.SQL)
	if rebuild {
		// Per-connection, and it must be outside the transaction:
		// SQLite silently ignores this pragma inside one.
		if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
			return err
		}
		defer conn.ExecContext(ctx, "PRAGMA foreign_keys=ON")
	}

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			conn.ExecContext(ctx, "ROLLBACK")
		}
	}()

	switch {
	case m.Fn != nil:
		if err := m.Fn(g); err != nil {
			return err
		}
	default:
		if _, err := conn.ExecContext(ctx, m.SQL); err != nil {
			return err
		}
	}

	if rebuild {
		rows, err := conn.QueryContext(ctx, "PRAGMA foreign_key_check")
		if err != nil {
			return err
		}
		bad := rows.Next()
		rows.Close()
		if bad {
			return fmt.Errorf("foreign_key_check failed after rebuild")
		}
	}

	if _, err := conn.ExecContext(ctx,
		"INSERT INTO rastrillo_migrations (id, applied_at, checksum) VALUES (?, ?, ?)",
		m.ID, time.Now().UTC().Format(time.RFC3339Nano), Checksum(m.SQL)); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}
