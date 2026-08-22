package migrate

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/carlosframework/rastrillo/db"
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
// own write would deadlock forever. Bounding the context turns that
// regression into a clear test failure instead of a hung suite.
func TestApplyFnMigrationCompletesWithinTimeout(t *testing.T) {
	d := openDB(t)
	s := set("notes",
		Migration{ID: "0001_init", SQL: "CREATE TABLE notes (id INTEGER PRIMARY KEY, n INTEGER);"},
		Migration{ID: "0002_seed", Fn: func(g *gorm.DB) error {
			return g.Exec("INSERT INTO notes (id, n) VALUES (1, 42)").Error
		}},
	)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := Apply(ctx, d, s); err != nil {
		t.Fatal(err)
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

var _ = errors.Is
