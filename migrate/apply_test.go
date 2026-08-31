package migrate

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"amadan.net/rastrillo/rastrillo/db"
)

func openDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "app.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func set(ns string, ms ...Migration) *Set {
	s := &Set{namespace: ns}
	for _, m := range ms {
		s.Add(m)
	}
	return s
}

func TestApplyRunsOnceAndRecords(t *testing.T) {
	d := openDB(t)
	s := set("notes", Migration{ID: "0001_init", SQL: "CREATE TABLE notes (id INTEGER PRIMARY KEY);"})

	r, err := Apply(context.Background(), d, s)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Applied) != 1 || r.Applied[0] != "notes/0001_init" {
		t.Fatalf("Applied = %v, want [notes/0001_init]", r.Applied)
	}

	// Second call is a no-op: the CREATE TABLE has no IF NOT EXISTS,
	// so a re-run would error if the ledger were not consulted.
	r2, err := Apply(context.Background(), d, s)
	if err != nil {
		t.Fatal(err)
	}
	if len(r2.Applied) != 0 || r2.Skipped != 1 {
		t.Fatalf("second Apply = %+v, want 0 applied / 1 skipped", r2)
	}
}

func TestApplyRunsGoMigrations(t *testing.T) {
	d := openDB(t)
	s := set("notes",
		Migration{ID: "0001_init", SQL: "CREATE TABLE notes (id INTEGER PRIMARY KEY, n INTEGER);"},
		Migration{ID: "0002_seed", Fn: func(g *gorm.DB) error {
			return g.Exec("INSERT INTO notes (id, n) VALUES (1, 42)").Error
		}},
	)
	if _, err := Apply(context.Background(), d, s); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := d.G.Raw("SELECT n FROM notes WHERE id = 1").Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 42 {
		t.Fatalf("n = %d, want 42", n)
	}
}

// TestApplyFnMigrationCompletesWithinTimeout guards against a
// regression to running Fn through the app's writer pool instead of
// the pinned connection: that pool holds exactly one connection
// (SQLite allows one writer), Apply already checks it out for the
// whole run, and Fn asking the same pool for a connection to do its
// own write would deadlock forever.
//
// This cannot be enforced by passing Apply a bounded context: GORM
// sets Statement.Context to context.Background() at Open time, and a
// migration's Fn calls the *gorm.DB it's given directly (e.g.
// g.Exec(...)) without threading a context through, so a blocked pool
// wait inside Fn never observes the caller's deadline regardless of
// what ctx Apply was given. So this asserts on wall-clock return
// instead, by racing Apply against a timer in a separate goroutine —
// a regression here fails fast instead of hanging the suite.
func TestApplyFnMigrationCompletesWithinTimeout(t *testing.T) {
	d := openDB(t)
	s := set("notes",
		Migration{ID: "0001_init", SQL: "CREATE TABLE notes (id INTEGER PRIMARY KEY, n INTEGER);"},
		Migration{ID: "0002_seed", Fn: func(g *gorm.DB) error {
			return g.Exec("INSERT INTO notes (id, n) VALUES (1, 42)").Error
		}},
	)
	done := make(chan error, 1)
	go func() {
		_, err := Apply(context.Background(), d, s)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Apply did not return within 2s — a Fn migration likely deadlocked on the writer pool")
	}
}

// TestApplyFnMigrationUsesGormCreate guards against a regression to
// GORM's default per-statement transaction: without
// SkipDefaultTransaction, GORM wraps Create/Update/Delete in their own
// BeginTransaction, and *sql.Conn satisfies gorm.TxBeginner, so that
// issues a real nested BEGIN on a connection already inside this
// migration's BEGIN IMMEDIATE — which SQLite refuses. Exec/Raw don't
// go through that path, which is why a Fn using only g.Exec wouldn't
// have caught this.
func TestApplyFnMigrationUsesGormCreate(t *testing.T) {
	d := openDB(t)
	type note struct {
		ID int `gorm:"primaryKey;column:id"`
		N  int `gorm:"column:n"`
	}
	s := set("notes",
		Migration{ID: "0001_init", SQL: "CREATE TABLE notes (id INTEGER PRIMARY KEY, n INTEGER);"},
		Migration{ID: "0002_seed", Fn: func(g *gorm.DB) error {
			return g.Table("notes").Create(&note{ID: 1, N: 42}).Error
		}},
	)
	if _, err := Apply(context.Background(), d, s); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := d.G.Raw("SELECT n FROM notes WHERE id = 1").Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 42 {
		t.Fatalf("n = %d, want 42", n)
	}
}

// TestRunOneSkipsWhenLedgerRowAppearsInsideTransaction exercises the
// losing side of a concurrent boot: Apply's readLedger runs before any
// transaction, so two instances can both see the ledger empty for a
// migration; whichever loses the BEGIN IMMEDIATE race must find the
// winner's row once it gets the lock and skip, not re-run the
// migration body or fail. This simulates the winner's commit directly
// rather than racing goroutines, so it is deterministic.
func TestRunOneSkipsWhenLedgerRowAppearsInsideTransaction(t *testing.T) {
	d := openDB(t)
	ctx := context.Background()
	conn, err := d.Writer().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, LedgerDDL); err != nil {
		t.Fatal(err)
	}

	m := Migration{ID: "notes/0001_init", SQL: "CREATE TABLE notes (id INTEGER PRIMARY KEY);"}
	// Stand in for a racing instance that already committed m between
	// Apply's readLedger and this call reaching BEGIN IMMEDIATE.
	if _, err := conn.ExecContext(ctx,
		"INSERT INTO rastrillo_migrations (id, applied_at, checksum) VALUES (?, ?, ?)",
		m.ID, time.Now().UTC().Format(time.RFC3339Nano), Checksum(m.SQL)); err != nil {
		t.Fatal(err)
	}

	applied, err := runOne(ctx, conn, nil, m)
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("runOne applied a migration another instance already recorded")
	}
	var name string
	conn.QueryRowContext(ctx,
		"SELECT name FROM sqlite_master WHERE type='table' AND name='notes'").Scan(&name)
	if name != "" {
		t.Fatal("runOne ran the migration body after losing the race")
	}
}

func TestApplyRollsBackFailedMigrationAndLeavesLedgerClean(t *testing.T) {
	d := openDB(t)
	s := set("notes",
		Migration{ID: "0001_init", SQL: "CREATE TABLE notes (id INTEGER PRIMARY KEY);"},
		Migration{ID: "0002_bad", SQL: "CREATE TABLE ok (n INTEGER); CREATE TABLE ok (n INTEGER);"},
	)
	if _, err := Apply(context.Background(), d, s); err == nil {
		t.Fatal("want error from the duplicate CREATE TABLE")
	}
	// 0001 committed; 0002 rolled back entirely, including the table
	// its first statement created.
	var count int64
	d.G.Raw("SELECT count(*) FROM rastrillo_migrations").Scan(&count)
	if count != 1 {
		t.Fatalf("ledger rows = %d, want 1", count)
	}
	var name string
	d.G.Raw("SELECT name FROM sqlite_master WHERE type='table' AND name='ok'").Scan(&name)
	if name != "" {
		t.Fatal("table 'ok' survived a rolled-back migration")
	}
}

// TestApplyRollsBackCleanlyWhenContextIsCancelledDuringTheMigration
// guards against a regression to using ctx, instead of a context
// detached from its cancellation, for the ROLLBACK and PRAGMA
// foreign_keys=ON cleanup: a cancelled context there makes
// database/sql refuse the call before it reaches SQLite, and the
// pinned connection — the app's only writer — would go back to the
// pool with an open transaction still holding the write lock. The Fn
// cancels its own context right before failing, so by the time the
// deferred cleanup runs, ctx is already done — the same shape as a
// boot deadline or SIGTERM landing mid-migration.
func TestApplyRollsBackCleanlyWhenContextIsCancelledDuringTheMigration(t *testing.T) {
	d := openDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	s := set("notes",
		Migration{ID: "0001_init", SQL: "CREATE TABLE notes (id INTEGER PRIMARY KEY);"},
		Migration{ID: "0002_bad", Fn: func(g *gorm.DB) error {
			cancel()
			return errors.New("boom")
		}},
	)
	if _, err := Apply(ctx, d, s); err == nil {
		t.Fatal("want error from the failing Fn")
	}

	// The pinned connection must have rolled back cleanly despite the
	// cancelled context, not been silently handed back to the pool
	// with an open transaction still holding the write lock: a fresh
	// Apply with a live context must work as if nothing had happened.
	s2 := set("notes", Migration{ID: "0003_ok", SQL: "CREATE TABLE fine (n INTEGER);"})
	if _, err := Apply(context.Background(), d, s2); err != nil {
		t.Fatalf("Apply after a cancelled-context rollback failed: %v", err)
	}
}

func TestApplyRefusesEditedMigration(t *testing.T) {
	d := openDB(t)
	orig := set("notes", Migration{ID: "0001_init", SQL: "CREATE TABLE notes (id INTEGER PRIMARY KEY);"})
	if _, err := Apply(context.Background(), d, orig); err != nil {
		t.Fatal(err)
	}
	edited := set("notes", Migration{ID: "0001_init", SQL: "CREATE TABLE notes (id INTEGER PRIMARY KEY, oops TEXT);"})
	_, err := Apply(context.Background(), d, edited)
	if err == nil {
		t.Fatal("want error when an applied migration's SQL changed")
	}
	if !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("error = %v, want it to name the immutability rule", err)
	}
}

func TestApplyToleratesReformattedMigration(t *testing.T) {
	d := openDB(t)
	if _, err := Apply(context.Background(), d,
		set("notes", Migration{ID: "0001_init", SQL: "CREATE TABLE notes (id INTEGER PRIMARY KEY);"})); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(context.Background(), d,
		set("notes", Migration{ID: "0001_init", SQL: "CREATE TABLE notes (\n  id INTEGER PRIMARY KEY\n);"})); err != nil {
		t.Fatalf("reformatting tripped the checksum: %v", err)
	}
}

// TestApplyRefusesDuplicateMigrationIDs reproduces the collision an
// app hits by being named after a framework subsystem: `rastrillo new
// sessions` scaffolds a package whose migrate namespace is
// "sessions", so its own 0001_init and sessions.Schema's both become
// "sessions/0001_init". Before this check, runOne's in-transaction
// ledger re-check found the first one's row, returned (false, nil),
// and Apply reported success on a boot that created none of the app's
// tables.
func TestApplyRefusesDuplicateMigrationIDs(t *testing.T) {
	d := openDB(t)
	subsystem := set("sessions", Migration{ID: "0001_init", SQL: "CREATE TABLE sessions (token_hash TEXT PRIMARY KEY);"})
	theApp := set("sessions", Migration{ID: "0001_init", SQL: "CREATE TABLE notes (id INTEGER PRIMARY KEY);"})

	_, err := Apply(context.Background(), d, Merge(subsystem, theApp))
	if err == nil {
		t.Fatal("want an error: the app's migration would be silently skipped and its tables never created")
	}
	if !strings.Contains(err.Error(), "sessions/0001_init") {
		t.Fatalf("error = %v, want it to name the colliding id", err)
	}
	var n int64
	d.G.Raw("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'sessions'").Scan(&n)
	if n != 0 {
		t.Fatal("Apply must refuse before running any migration")
	}
}

// TestApplyAcceptsTheSameIDInDifferentNamespaces is the other side:
// every subsystem ships a 0001_init, and Merge qualifies them, so
// those must stay legal.
func TestApplyAcceptsTheSameIDInDifferentNamespaces(t *testing.T) {
	d := openDB(t)
	a := set("sessions", Migration{ID: "0001_init", SQL: "CREATE TABLE sessions (token_hash TEXT PRIMARY KEY);"})
	b := set("notes", Migration{ID: "0001_init", SQL: "CREATE TABLE notes (id INTEGER PRIMARY KEY);"})
	r, err := Apply(context.Background(), d, Merge(a, b))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Applied) != 2 {
		t.Fatalf("Applied = %v, want both", r.Applied)
	}
}

// TestNeedsRebuildRecognisesGormlitesOwnRebuild pins the heuristic
// against the statements gormlite actually emits, rather than against
// what a reader assumes it emits. A dead third clause matched
// "__TEMP__" for a table gormlite in fact names "<table>__temp"; the
// check has always worked through RENAME TO alone, and this is what
// says so.
func TestNeedsRebuildRecognisesGormlitesOwnRebuild(t *testing.T) {
	rebuild := "CREATE TABLE `notes__temp`  (`id` integer PRIMARY KEY AUTOINCREMENT,`title` text);\n" +
		"INSERT INTO `notes__temp`(`id`,`title`) SELECT `id`,`title` FROM `notes`;\n" +
		"DROP TABLE `notes`;\n" +
		"ALTER TABLE `notes__temp` RENAME TO `notes`;\n"
	if !needsRebuild(rebuild) {
		t.Fatal("gormlite's rebuild must run with foreign keys off and a foreign_key_check before commit")
	}
	if needsRebuild("ALTER TABLE notes ADD archived numeric;") {
		t.Fatal("an additive migration must not pay for foreign_key_check over the whole database")
	}
}
