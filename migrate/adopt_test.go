package migrate

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosframework/rastrillo/db"
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
