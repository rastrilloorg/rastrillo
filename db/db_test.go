package db

import (
	"bytes"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"
)

func TestOpenWALAndPing(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "app.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var jm string
	if err := d.G.Raw("PRAGMA journal_mode").Scan(&jm).Error; err != nil {
		t.Fatal(err)
	}
	if jm != "wal" {
		t.Fatalf("journal_mode = %q, want wal", jm)
	}
}

// TestReadDuringOpenRows is the single-connection-deadlock regression:
// with one shared connection, an open *sql.Rows plus any second query
// hangs forever. The reader pool must allow a second concurrent read.
func TestReadDuringOpenRows(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "app.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.G.Exec("CREATE TABLE t (n INTEGER)").Error; err != nil {
		t.Fatal(err)
	}
	if err := d.G.Exec("INSERT INTO t VALUES (1), (2)").Error; err != nil {
		t.Fatal(err)
	}
	rows, err := d.reader.Query("SELECT n FROM t")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	rows.Next() // hold the first result set open...
	var n int
	// ...and issue a second read; this must not hang.
	if err := d.G.Raw("SELECT count(*) FROM t").Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("count = %d, want 2", n)
	}
}

// TestRecordNotFoundNotLogged pins IgnoreRecordNotFoundError: a scoped
// by-ID miss is the 404 contract's routine outcome and must not spam
// the log, while real errors still land there.
func TestRecordNotFoundNotLogged(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	d, err := Open(filepath.Join(t.TempDir(), "app.db"), log)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.G.Exec("CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT)").Error; err != nil {
		t.Fatal(err)
	}

	// The assertion is about the by-ID miss below, not the setup: a
	// slow CI runner can trip the 200ms SLOW SQL warning on the CREATE
	// TABLE itself (observed at 452ms on a GitHub runner), so start
	// measuring from here.
	buf.Reset()

	var got struct{ ID int64 }
	err = d.G.Table("notes").Where("id = ?", 99).Take(&got).Error
	if err != gorm.ErrRecordNotFound {
		t.Fatalf("err = %v, want ErrRecordNotFound", err)
	}
	if s := buf.String(); s != "" {
		t.Errorf("record-not-found produced log output: %q", s)
	}

	if err := d.G.Exec("SELECT * FROM no_such_table").Error; err == nil {
		t.Fatal("bad SQL did not error")
	}
	if !strings.Contains(buf.String(), "no_such_table") {
		t.Errorf("real error missing from log output: %q", buf.String())
	}
}

// TestConstraintErrorsTranslate pins TranslateError + the fork's
// Translate: a UNIQUE violation surfaces as gorm.ErrDuplicatedKey and
// an FK violation as gorm.ErrForeignKeyViolated — the sentinels GORM
// apps test against with errors.Is.
func TestConstraintErrorsTranslate(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "app.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, stmt := range []string{
		"CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT UNIQUE)",
		"CREATE TABLE notes (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id))",
		"INSERT INTO users (id, email) VALUES (1, 'a@example.com')",
	} {
		if err := d.G.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}

	err = d.G.Exec("INSERT INTO users (id, email) VALUES (2, 'a@example.com')").Error
	if !errors.Is(err, gorm.ErrDuplicatedKey) {
		t.Errorf("duplicate email: err = %v, want gorm.ErrDuplicatedKey", err)
	}
	err = d.G.Exec("INSERT INTO notes (user_id) VALUES (99)").Error
	if !errors.Is(err, gorm.ErrForeignKeyViolated) {
		t.Errorf("dangling user_id: err = %v, want gorm.ErrForeignKeyViolated", err)
	}
}

func TestWriterSerialized(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "app.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if got := d.writer.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("writer MaxOpenConnections = %d, want 1", got)
	}
}
