package tablerows_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/internal/tablerows"
)

// recorder is a testing.TB that keeps its errors instead of failing.
type recorder struct {
	testing.TB
	errs []string
}

func (r *recorder) Helper()                   {}
func (r *recorder) Errorf(f string, a ...any) { r.errs = append(r.errs, fmt.Sprintf(f, a...)) }

func open(t *testing.T) *sql.DB {
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	w := d.Writer()
	for _, q := range []string{`CREATE TABLE pending (x)`, `CREATE TABLE sessions (x)`, `INSERT INTO pending VALUES (1)`} {
		if _, err := w.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

// A row moved from one table to another leaves the total alone, and
// is still two changes.
func TestUnchangedSeesARowMoveBetweenTables(t *testing.T) {
	w := open(t)
	before := tablerows.Count(t, w)
	if before["pending"] != 1 || before["sessions"] != 0 {
		t.Fatalf("Count = %v", before)
	}
	if _, err := w.Exec(`DELETE FROM pending; INSERT INTO sessions VALUES (1)`); err != nil {
		t.Fatal(err)
	}
	r := &recorder{TB: t}
	tablerows.Unchanged(r, w, "move", before)
	if len(r.errs) != 2 {
		t.Errorf("Unchanged reported %q, want one error per table", r.errs)
	}
	r = &recorder{TB: t}
	tablerows.Unchanged(r, w, "still", tablerows.Count(t, w))
	if len(r.errs) != 0 {
		t.Errorf("Unchanged reported %q with nothing changed", r.errs)
	}
}
