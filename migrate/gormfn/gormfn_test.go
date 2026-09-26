package gormfn_test

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/gorm"

	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/migrate/gormfn"
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

func notes(fn func(*gorm.DB) error) *migrate.Set {
	return migrate.Merge(migrate.MustFromFS(emptyFS{}, "notes").
		Add(migrate.Migration{ID: "0001_init", SQL: "CREATE TABLE notes (id INTEGER PRIMARY KEY, n INTEGER);"}).
		Add(migrate.Migration{ID: "0002_seed", Fn: gormfn.Fn(fn)}))
}

func readN(t *testing.T, d *db.DB) int {
	t.Helper()
	var n int
	if err := d.G.Raw("SELECT n FROM notes WHERE id = 1").Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

// TestCompletesWithinTimeout guards against building the *gorm.DB on
// the app's writer pool instead of the pinned connection: that pool
// holds exactly one connection and Apply already has it, so the
// migration would wait forever. GORM does not thread a ctx through
// g.Exec, so a bounded ctx would not catch it — this races Apply
// against a timer instead, so a regression fails fast.
func TestCompletesWithinTimeout(t *testing.T) {
	d := openDB(t)
	s := notes(func(g *gorm.DB) error {
		return g.Exec("INSERT INTO notes (id, n) VALUES (1, 42)").Error
	})
	done := make(chan error, 1)
	go func() {
		_, err := migrate.Apply(context.Background(), d, s)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Apply did not return within 2s — the GORM migration likely deadlocked on the writer pool")
	}
	if n := readN(t, d); n != 42 {
		t.Fatalf("n = %d, want 42", n)
	}
}

// TestCreateDoesNotNestATransaction guards SkipDefaultTransaction:
// without it GORM wraps Create in its own BEGIN on a connection already
// inside BEGIN IMMEDIATE, which SQLite refuses. Exec and Raw do not go
// through that path, which is why this uses Create.
func TestCreateDoesNotNestATransaction(t *testing.T) {
	d := openDB(t)
	type note struct {
		ID int `gorm:"primaryKey;column:id"`
		N  int `gorm:"column:n"`
	}
	s := notes(func(g *gorm.DB) error {
		return g.Table("notes").Create(&note{ID: 1, N: 42}).Error
	})
	if _, err := migrate.Apply(context.Background(), d, s); err != nil {
		t.Fatal(err)
	}
	if n := readN(t, d); n != 42 {
		t.Fatalf("n = %d, want 42", n)
	}
}

// TestFailureRollsBackWithTheLedgerRow is the point of building on the
// pinned connection: the GORM write and the ledger row share Apply's
// transaction, so a failure leaves neither.
func TestFailureRollsBackWithTheLedgerRow(t *testing.T) {
	d := openDB(t)
	s := notes(func(g *gorm.DB) error {
		if err := g.Exec("INSERT INTO notes (id, n) VALUES (1, 42)").Error; err != nil {
			return err
		}
		return errors.New("boom")
	})
	if _, err := migrate.Apply(context.Background(), d, s); err == nil {
		t.Fatal("want the migration's error")
	}
	var rows, ledger int64
	d.G.Raw("SELECT COUNT(*) FROM notes").Scan(&rows)
	d.G.Raw("SELECT COUNT(*) FROM rastrillo_migrations WHERE id = 'notes/0002_seed'").Scan(&ledger)
	if rows != 0 || ledger != 0 {
		t.Fatalf("after a failed migration: %d note rows, %d ledger rows; want 0 and 0", rows, ledger)
	}
}

// emptyFS lets a test build a namespaced Set from Add alone.
type emptyFS struct{}

func (emptyFS) Open(string) (fs.File, error) { return nil, fs.ErrNotExist }
