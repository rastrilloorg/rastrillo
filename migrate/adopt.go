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

	if lines := live.diffLines(want); len(lines) > 0 {
		texts := make([]string, 0, len(lines))
		strands := false
		for _, l := range lines {
			texts = append(texts, l.text)
			if !l.extra {
				strands = true
			}
		}
		return false, fmt.Errorf(
			"migrate: this database has tables but no migration ledger, and its schema does not match "+
				"the migration set, so it cannot be adopted safely. Below, \"missing X\" means this "+
				"database lacks X and the migration set has it; \"extra X\" means this database has X "+
				"and no migration defines it:\n  %s\n%s",
			strings.Join(texts, "\n  "), recovery(strands))
	}
	return true, Stamp(ctx, conn, ms, "")
}

// recovery is the second half of the refusal: what to actually do.
// It has to branch, because the two halves of a diff need opposite
// advice and getting it wrong is worse than saying nothing.
//
// `baseline` writes ledger rows and runs no DDL. That is exactly right
// when every difference is an "extra" — the migration set would create
// nothing this database lacks, so recording the set as applied leaves
// nothing uncreated. It is exactly wrong the moment one line is a
// "missing" or a "differs": the operator would stamp a migration as
// applied that has never run, the object it was supposed to create is
// then stranded forever (nothing will ever run that migration again),
// and the app boots green and fails at runtime on the first request
// that touches it. The unqualified "then stamp the ledger with:
// baseline" this used to print handed an operator that outcome as the
// recommended next step.
func recovery(strands bool) string {
	if !strands {
		return "Every difference above is something this database has that no migration defines, so " +
			"stamping the set as applied leaves nothing uncreated. Stamp the ledger with:\n" +
			"  rastrillo migration baseline --db <path>"
	}
	return "Do NOT run `rastrillo migration baseline --db <path>` here. Bare baseline records every " +
		"migration as applied without running any of them, so each \"missing\" above would never be " +
		"created — this app would boot green and then fail at runtime on the first request that " +
		"touches it.\nBring the schema into line first: apply the missing DDL by hand, then " +
		"`baseline --db <path> --through <last id that genuinely ran>` so the rest still runs. Or " +
		"split the release, so the deploy that introduces migrations changes no schema.\n" +
		"The first deploy on a rastrillo with migrations must be schema-neutral: generate 0001_init " +
		"from the models exactly as they are already deployed, ship that alone, and change a model " +
		"only in a later release."
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
