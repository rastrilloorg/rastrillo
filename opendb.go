package rastrillo

import (
	"database/sql"
	"fmt"
	"strings"

	"amadan.net/rastrillo/rastrillo/internal/capturemode"
	"amadan.net/rastrillo/rastrillo/internal/checkconfig"
)

// OpenDB applies the SQLite convention the survey found hand-propagated
// repo to repo (design doc §5), with fixes: busy_timeout before WAL (the
// reverse crashes with SQLITE_BUSY under concurrent open), NORMAL sync
// (FULL fsyncs every commit, #234), one connection, an eager ping so the
// file exists from boot, then migrate. Exported so tests and non-Serve
// code need not copy the DSN by hand (the blog's F4). It always opens at
// NORMAL; Serve's openDB honours Options.DurableCommits (#261).
func OpenDB(path string, migrations []string) (*sql.DB, error) {
	return openDB(path, migrations, false)
}

func openDB(path string, migrations []string, durable bool) (*sql.DB, error) {
	checkconfig.Guard("rastrillo.OpenDB")
	synchronous := "NORMAL"
	if durable {
		synchronous = "FULL"
	}
	path = capturemode.DBPath(path)
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)"+
		"&_pragma=journal_mode(WAL)&_pragma=synchronous("+synchronous+")")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	// database/sql's Open is lazy — it never touches the driver, so with
	// zero migrations the file would never materialize. A hibernate
	// route's activator starts replicating this path from boot, so a
	// zero-migration app must still create it: Ping forces the
	// connection open now, at boot, instead of on the first request.
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping %s: %w", path, err)
	}

	for index, migration := range migrations {
		if _, err := db.Exec(migration); err != nil {
			if isDuplicateColumn(err) {
				continue
			}
			db.Close()
			return nil, fmt.Errorf("migration %d: %w", index, err)
		}
	}
	return db, nil
}

// isDuplicateColumn matches the one class of migration error the family
// convention treats as success: an additive ALTER TABLE ... ADD COLUMN
// re-run against a database that already has it.
func isDuplicateColumn(err error) bool {
	return strings.Contains(err.Error(), "duplicate column")
}
