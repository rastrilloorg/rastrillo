package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// adopt handles first boot against a database that has no ledger.
//
// An empty ledger cannot simply mean "replay everything": a deployed
// app already has its tables, and migration 0005_add_column would
// fail on a column that is already there. That is the failure the old
// isDuplicateColumn swallow existed to paper over.
//
// It returns true when it stamped the ledger and the caller should run
// nothing.
func adopt(ctx context.Context, conn *sql.Conn, ms []Migration) (bool, error) {
	empty, err := isEmpty(ctx, conn)
	if err != nil {
		return false, err
	}
	if empty {
		// New app. Normal path.
		return false, nil
	}

	live, err := Read(ctx, conn)
	if err != nil {
		return false, err
	}
	mem, err := Replay(ctx, ms)
	if err != nil {
		return false, err
	}
	defer mem.Close()
	// live already has the ledger table: Apply creates it before
	// calling adopt. Give mem the same one so it doesn't show up as
	// an "extra table" in every comparison — the ledger isn't part of
	// the migration set being adopted.
	if _, err := mem.ExecContext(ctx, LedgerDDL); err != nil {
		return false, err
	}
	want, err := Read(ctx, mem)
	if err != nil {
		return false, err
	}

	if diff := live.Diff(want); len(diff) > 0 {
		return false, fmt.Errorf(
			"migrate: this database has tables but no migration ledger, and its schema does not match "+
				"the migration set, so it cannot be adopted safely:\n  %s\n"+
				"Read the differences, then stamp the ledger with: rastrillo migration baseline --db <path>",
			strings.Join(diff, "\n  "))
	}
	return true, Stamp(ctx, conn, ms, "")
}

// isEmpty reports whether the database has no user tables. The ledger
// itself is excluded: Apply creates it before calling adopt.
func isEmpty(ctx context.Context, conn *sql.Conn) (bool, error) {
	var n int
	err := conn.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master
	  WHERE type = 'table' AND name NOT LIKE 'sqlite_%' AND name <> 'rastrillo_migrations'`).Scan(&n)
	return n == 0, err
}

// Stamp records migrations as applied without running them. When
// through is non-empty, it stops after that ID — the escape hatch for
// a database that is partway through the set.
func Stamp(ctx context.Context, conn *sql.Conn, ms []Migration, through string) error {
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		// Same hazard as runOne's rollback: ctx may already be the
		// reason this call is unwinding (a boot deadline, a SIGTERM),
		// and a cancelled context here would make ExecContext refuse
		// the ROLLBACK before it ever reached SQLite — handing the
		// pool back a connection with an open transaction still
		// holding the write lock. Reuse the same detached-context
		// ROLLBACK + evict treatment runOne uses, rather than a
		// second way of doing this in the package.
		if _, rbErr := conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK"); rbErr != nil {
			evict(conn)
		}
	}()

	for _, m := range ms {
		if _, err := conn.ExecContext(ctx,
			"INSERT OR IGNORE INTO rastrillo_migrations (id, applied_at, checksum) VALUES (?, ?, ?)",
			m.ID, time.Now().UTC().Format(time.RFC3339Nano), Checksum(m.SQL)); err != nil {
			return err
		}
		if through != "" && m.ID == through {
			break
		}
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}
