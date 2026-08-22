package dump

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/carlosframework/rastrillo/migrate"
)

type dumpNote struct {
	ID    int64
	Title string
}

func TestComputeReportsChangesAndSchema(t *testing.T) {
	p, err := Compute(nil, []any{&dumpNote{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Changes) == 0 {
		t.Fatal("want a CREATE TABLE change for a model with no migrations")
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("payload must marshal to JSON: %v", err)
	}
	if !strings.Contains(string(b), "dump_notes") {
		t.Fatalf("payload = %s, want it to mention the table", b)
	}
}

func TestComputeIsQuietWhenInSync(t *testing.T) {
	ms := []migrate.Migration{{ID: "0001_init",
		SQL: "CREATE TABLE dump_notes (id INTEGER PRIMARY KEY, title TEXT);"}}
	p, err := Compute(ms, []any{&dumpNote{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Changes) != 0 {
		t.Fatalf("Changes = %+v, want none", p.Changes)
	}
	if !strings.Contains(p.Schema, "dump_notes") {
		t.Fatalf("Schema = %q, want the replayed DDL", p.Schema)
	}
}
