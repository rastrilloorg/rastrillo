package db

import (
	"bytes"
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
