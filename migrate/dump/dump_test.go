package dump

import (
	"encoding/json"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/migrate"
)

type dumpNote struct {
	ID    int64
	Title string
}

func TestComputeReportsChangesAndSchema(t *testing.T) {
	p, err := Compute(nil, nil, []any{&dumpNote{}})
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
	p, err := Compute(ms, nil, []any{&dumpNote{}})
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

// TestComputeGeneratesAgainstOwnSetNotBoot is the constraint this task
// calls out as the one most easily broken by accident: Compute must
// diff ms (the app's own set) against Models, never boot. If it fed
// boot into Generate/SchemaSQL instead, a framework subsystem's table
// (here, sessions, present only in boot) would show up as an "extra"
// table Models knows nothing about, and the structural drop-pass
// would propose dropping it.
func TestComputeGeneratesAgainstOwnSetNotBoot(t *testing.T) {
	ms := []migrate.Migration{{ID: "0001_init",
		SQL: "CREATE TABLE dump_notes (id INTEGER PRIMARY KEY, title TEXT);"}}
	boot := []migrate.Migration{
		{ID: "sessions/0001_init", SQL: "CREATE TABLE sessions (id TEXT PRIMARY KEY);"},
		ms[0],
	}
	p, err := Compute(ms, boot, []any{&dumpNote{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Changes) != 0 {
		t.Fatalf("Changes = %+v, want none — a table only boot knows about must not affect Generate's diff", p.Changes)
	}
	for _, c := range p.Changes {
		if strings.Contains(strings.ToUpper(c.SQL), "SESSIONS") {
			t.Fatalf("Compute proposed touching the sessions table from boot: %q", c.SQL)
		}
	}
	if strings.Contains(p.Schema, "sessions") {
		t.Fatalf("Schema = %q, want it built from ms alone, not boot", p.Schema)
	}
}

// TestPayloadRoundTripsBootSetWithSQLIntact is the test the brief asks
// for directly: Boot must survive a JSON round trip with each
// migration's ID and SQL intact, because baseline hands this straight
// to migrate.Stamp, which checksums the SQL — a lossy round trip would
// make the next real boot refuse with "applied with different SQL".
func TestPayloadRoundTripsBootSetWithSQLIntact(t *testing.T) {
	boot := []migrate.Migration{
		{ID: "sessions/0001_init", SQL: "CREATE TABLE sessions (id TEXT PRIMARY KEY);"},
		{ID: "app/0001_init", SQL: "CREATE TABLE dump_notes (id INTEGER PRIMARY KEY);"},
	}
	p, err := Compute(nil, boot, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("payload with a boot set must marshal to JSON: %v", err)
	}
	var got Payload
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Boot) != len(boot) {
		t.Fatalf("Boot = %+v, want %d migrations round-tripped", got.Boot, len(boot))
	}
	for i, m := range boot {
		if got.Boot[i].ID != m.ID || got.Boot[i].SQL != m.SQL {
			t.Fatalf("Boot[%d] = %+v, want ID=%q SQL=%q", i, got.Boot[i], m.ID, m.SQL)
		}
	}
}
