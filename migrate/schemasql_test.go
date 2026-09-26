package migrate

import (
	"context"
	"strings"
	"testing"
)

func TestSchemaSQLReflectsAppliedMigrations(t *testing.T) {
	out, err := SchemaSQL(context.Background(), []Migration{
		{ID: "0001_init", SQL: "CREATE TABLE gen_notes (id INTEGER PRIMARY KEY);"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "gen_notes") {
		t.Fatalf("schema.sql = %q, want it to contain gen_notes", out)
	}
}
