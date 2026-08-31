package migrate

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/db"
)

// legacy is the shape a deployed app already has: the tables exist,
// created by the old Migrations []string path, and there is no ledger.
const legacy = `CREATE TABLE IF NOT EXISTS sessions (
	  token_hash TEXT PRIMARY KEY,
	  subject    TEXT NOT NULL,
	  created_at TEXT NOT NULL
	);`

func TestAdoptStampsMatchingDatabaseWithoutRunningDDL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	d, err := db.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.G.Exec(legacy).Error; err != nil {
		t.Fatal(err)
	}
	// A row proves no DDL re-ran: a CREATE TABLE would have failed,
	// and a DROP/rebuild would have lost this.
	if err := d.G.Exec(`INSERT INTO sessions VALUES ('h','s','now')`).Error; err != nil {
		t.Fatal(err)
	}

	s := set("sessions", Migration{ID: "0001_init", SQL: legacy})
	r, err := Apply(context.Background(), d, s)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Adopted {
		t.Fatalf("Result = %+v, want Adopted", r)
	}
	if len(r.Applied) != 0 {
		t.Fatalf("Applied = %v, want none — adoption must run zero DDL", r.Applied)
	}
	var n int64
	d.G.Raw("SELECT count(*) FROM sessions").Scan(&n)
	if n != 1 {
		t.Fatalf("row count = %d, want the pre-existing row intact", n)
	}
	var ledger int64
	d.G.Raw("SELECT count(*) FROM rastrillo_migrations").Scan(&ledger)
	if ledger != 1 {
		t.Fatalf("ledger rows = %d, want 1 stamped", ledger)
	}
	d.Close()
}

func TestAdoptRefusesMismatchedDatabase(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "app.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.G.Exec("CREATE TABLE sessions (token_hash TEXT PRIMARY KEY, unexpected TEXT)").Error; err != nil {
		t.Fatal(err)
	}
	_, err = Apply(context.Background(), d, set("sessions", Migration{ID: "0001_init", SQL: legacy}))
	if err == nil {
		t.Fatal("want refusal for a non-empty database that does not match the migration set")
	}
	if !strings.Contains(err.Error(), "unexpected") {
		t.Fatalf("error = %v, want it to name the structural difference", err)
	}
}

func TestEmptyDatabaseAppliesNormally(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "app.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	r, err := Apply(context.Background(), d, set("sessions", Migration{ID: "0001_init", SQL: legacy}))
	if err != nil {
		t.Fatal(err)
	}
	if r.Adopted {
		t.Fatal("an empty database must take the normal apply path, not adoption")
	}
	if len(r.Applied) != 1 {
		t.Fatalf("Applied = %v, want one", r.Applied)
	}
}

// TestAdoptRefusalRefusesToRecommendBaselineWhenSomethingIsMissing
// guards the failure mode that makes the refusal message worse than
// no message at all. A diff carrying a "missing" line means the
// migration set would create something this database lacks; bare
// `baseline` records every migration as applied without running any
// of them, so following that advice strands the missing object
// permanently — the app then boots green and fails at runtime.
func TestAdoptRefusalRefusesToRecommendBaselineWhenSomethingIsMissing(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "app.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	// Same shape as a release that adds one field to a model: the
	// live table is the old one, the migration set has the new column.
	if err := d.G.Exec("CREATE TABLE sessions (token_hash TEXT PRIMARY KEY, subject TEXT NOT NULL, created_at TEXT NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	withCol := legacy[:len(legacy)-len("\n\t);")] + ",\n\t  archived TEXT\n\t);"
	_, err = Apply(context.Background(), d, set("sessions", Migration{ID: "0001_init", SQL: withCol}))
	if err == nil {
		t.Fatal("want refusal")
	}
	msg := err.Error()
	if !strings.Contains(msg, "missing column archived") {
		t.Fatalf("error = %v, want it to name the missing column", err)
	}
	if !strings.Contains(msg, "without running") {
		t.Fatalf("error = %v, want it to say baseline records migrations without running them", err)
	}
	if strings.Contains(msg, "then stamp the ledger with") {
		t.Fatalf("error = %v, must not recommend bare baseline: it would strand the missing column", err)
	}
}

// TestAdoptRefusalRecommendsBaselineWhenTheDiffIsExtrasOnly is the
// other half: when the migration set would create nothing this
// database lacks, baseline strands nothing and is the right advice.
func TestAdoptRefusalRecommendsBaselineWhenTheDiffIsExtrasOnly(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "app.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.G.Exec(legacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := d.G.Exec("CREATE TABLE leftovers (id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatal(err)
	}
	_, err = Apply(context.Background(), d, set("sessions", Migration{ID: "0001_init", SQL: legacy}))
	if err == nil {
		t.Fatal("want refusal")
	}
	if !strings.Contains(err.Error(), "rastrillo migration baseline --db") {
		t.Fatalf("error = %v, want it to recommend baseline for an extras-only diff", err)
	}
	if strings.Contains(err.Error(), "without running") {
		t.Fatalf("error = %v, must not warn against baseline when nothing is missing", err)
	}
}
