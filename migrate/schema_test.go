package migrate

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func memDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	// One connection: a second connection to ":memory:" is a second,
	// empty database.
	d.SetMaxOpenConns(1)
	t.Cleanup(func() { d.Close() })
	return d
}

func TestReadCapturesColumnsAndIndexes(t *testing.T) {
	d := memDB(t)
	ctx := context.Background()
	mustExec(t, d, `CREATE TABLE notes (
	  id INTEGER PRIMARY KEY,
	  title TEXT NOT NULL DEFAULT '',
	  body TEXT
	);`)
	mustExec(t, d, `CREATE INDEX notes_title ON notes (title);`)

	snap, err := Read(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Tables) != 1 || snap.Tables[0].Name != "notes" {
		t.Fatalf("tables = %+v, want one table 'notes'", snap.Tables)
	}
	cols := snap.Tables[0].Columns
	if len(cols) != 3 {
		t.Fatalf("columns = %+v, want 3", cols)
	}
	if cols[1].Name != "title" || !cols[1].NotNull {
		t.Fatalf("title column = %+v, want NotNull", cols[1])
	}
	if len(snap.Tables[0].Indexes) != 1 || snap.Tables[0].Indexes[0].Name != "notes_title" {
		t.Fatalf("indexes = %+v, want notes_title", snap.Tables[0].Indexes)
	}
}

func TestReadSkipsSQLiteInternalTables(t *testing.T) {
	d := memDB(t)
	mustExec(t, d, `CREATE TABLE t (id INTEGER PRIMARY KEY AUTOINCREMENT);`)
	mustExec(t, d, `INSERT INTO t DEFAULT VALUES;`)
	snap, err := Read(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	for _, tb := range snap.Tables {
		if tb.Name == "sqlite_sequence" {
			t.Fatal("Read returned the internal sqlite_sequence table")
		}
	}
}

func TestEqualIgnoresDDLFormatting(t *testing.T) {
	a, b := memDB(t), memDB(t)
	mustExec(t, a, "CREATE TABLE t (id INTEGER PRIMARY KEY, n TEXT NOT NULL);")
	mustExec(t, b, "CREATE TABLE t (\n  id  INTEGER  PRIMARY KEY,\n  n   TEXT NOT NULL\n);")
	sa, err := Read(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	sb, err := Read(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	if !sa.Equal(sb) {
		t.Fatalf("formatting changed the snapshot:\n%v", sa.Diff(sb))
	}
}

func TestDiffNamesTheMissingColumn(t *testing.T) {
	a, b := memDB(t), memDB(t)
	mustExec(t, a, "CREATE TABLE t (id INTEGER PRIMARY KEY);")
	mustExec(t, b, "CREATE TABLE t (id INTEGER PRIMARY KEY, extra TEXT);")
	sa, _ := Read(context.Background(), a)
	sb, _ := Read(context.Background(), b)
	d := sa.Diff(sb)
	if len(d) != 1 || !contains(d[0], "extra") {
		t.Fatalf("Diff = %v, want one entry naming 'extra'", d)
	}
}

func mustExec(t *testing.T, d *sql.DB, q string) {
	t.Helper()
	if _, err := d.Exec(q); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && strings.Contains(s, sub) }
