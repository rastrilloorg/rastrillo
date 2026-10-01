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
// The database is compared against a replay of the set. A match
// stamps every migration as applied and returns their count: the
// caller runs nothing. Failing that, it is compared against a replay
// of the set WITHOUT its post-adoption migrations (Migration.
// PostAdoption): a match there is a database from before those
// migrations existed, so everything else is stamped, the count says
// how many, and the caller runs the post-adoption ones — which is how
// an adoption-era table gains a column. No match at all is a refusal.
func adopt(ctx context.Context, conn *sql.Conn, ms []Migration) (int, error) {
	empty, err := isEmpty(ctx, conn)
	if err != nil {
		return 0, err
	}
	if empty {
		// New app. Normal path.
		return 0, nil
	}

	live, err := Read(ctx, conn)
	if err != nil {
		return 0, err
	}
	lines, err := adoptionDiff(ctx, live, ms)
	if err != nil {
		return 0, err
	}
	if len(lines) == 0 {
		return len(ms), Stamp(ctx, conn, ms, "")
	}

	var base []Migration
	for _, m := range ms {
		if !m.PostAdoption {
			base = append(base, m)
		}
	}
	if len(base) < len(ms) {
		baseLines, err := adoptionDiff(ctx, live, base)
		if err != nil {
			return 0, err
		}
		if len(baseLines) == 0 {
			// Stamp only the base: the post-adoption migrations are
			// left unrecorded for the caller to run, in order, each in
			// its own transaction like any pending migration.
			return len(base), stampOnly(ctx, conn, base)
		}
	}

	texts := make([]string, 0, len(lines))
	strands := false
	for _, l := range lines {
		texts = append(texts, l.text)
		if !l.extra {
			strands = true
		}
	}
	return 0, fmt.Errorf(
		"migrate: this database has tables but no migration ledger, and its schema does not match "+
			"the migration set, so it cannot be adopted safely. Below, \"missing X\" means this "+
			"database lacks X and the migration set has it; \"extra X\" means this database has X "+
			"and no migration defines it:\n  %s\n%s",
		strings.Join(texts, "\n  "), recovery(strands))
}

// adoptionDiff replays ms into memory and reports how live differs
// from the result. live already has the ledger table: Apply creates
// it before calling adopt. The replay gets the same one so it doesn't
// show up as an "extra table" — the ledger isn't part of the set being
// adopted.
func adoptionDiff(ctx context.Context, live Snapshot, ms []Migration) ([]diffLine, error) {
	mem, err := Replay(ctx, ms)
	if err != nil {
		return nil, err
	}
	defer mem.Close()
	if _, err := mem.ExecContext(ctx, LedgerDDL); err != nil {
		return nil, err
	}
	want, err := Read(ctx, mem)
	if err != nil {
		return nil, err
	}
	return live.diffLines(want), nil
}

// stampOnly records exactly ms as applied — a subset of the composed
// set, so Stamp's "through" form cannot express it.
func stampOnly(ctx context.Context, conn *sql.Conn, ms []Migration) error {
	return Stamp(ctx, conn, ms, "")
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
