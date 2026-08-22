package migrate

import (
	"context"
	"strings"
	"testing"
)

type genNote struct {
	ID    int64
	Title string
}

type genNoteWithBody struct {
	ID    int64
	Title string
	Body  string
}

func (genNoteWithBody) TableName() string { return "gen_notes" }
func (genNote) TableName() string         { return "gen_notes" }

func TestGenerateEmitsCreateTableForNewModel(t *testing.T) {
	changes, err := Generate(context.Background(), nil, []any{&genNote{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) == 0 {
		t.Fatal("no changes generated for a model with no migrations")
	}
	joined := allSQL(changes)
	if !strings.Contains(strings.ToUpper(joined), "CREATE TABLE") || !strings.Contains(joined, "gen_notes") {
		t.Fatalf("generated SQL = %q, want a CREATE TABLE for gen_notes", joined)
	}
	for _, c := range changes {
		if c.Destructive {
			t.Fatalf("creating a table was marked destructive: %q", c.SQL)
		}
	}
}

func TestGenerateEmitsAddColumnForNewField(t *testing.T) {
	// The existing migration set already created the narrow table.
	existing := []Migration{{ID: "0001_init", SQL: "CREATE TABLE gen_notes (id INTEGER PRIMARY KEY, title TEXT);"}}
	changes, err := Generate(context.Background(), existing, []any{&genNoteWithBody{}})
	if err != nil {
		t.Fatal(err)
	}
	// GORM's own AddColumn (migrator/migrator.go) always emits
	// "ALTER TABLE ... ADD <col> <type>" — never the "ADD COLUMN"
	// keyword — for every dialect that doesn't override it, gormlite
	// included. Generate must keep whatever GORM actually said rather
	// than hand-editing it to add a word GORM never wrote, so the
	// assertion checks for GORM's real output instead of "ADD COLUMN".
	joined := strings.ToUpper(allSQL(changes))
	if !strings.Contains(joined, "ALTER TABLE") || !strings.Contains(joined, "ADD") ||
		!strings.Contains(strings.ToLower(allSQL(changes)), "body") {
		t.Fatalf("generated SQL = %q, want ALTER TABLE ... ADD ... body", allSQL(changes))
	}
}

func TestGenerateIsEmptyWhenModelsAndMigrationsAgree(t *testing.T) {
	existing := []Migration{{ID: "0001_init", SQL: "CREATE TABLE gen_notes (id INTEGER PRIMARY KEY, title TEXT);"}}
	changes, err := Generate(context.Background(), existing, []any{&genNote{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("changes = %q, want none — models and migrations already agree", allSQL(changes))
	}
}

func TestGenerateMarksDroppedColumnDestructive(t *testing.T) {
	// Migrations have a column the model no longer declares.
	existing := []Migration{{ID: "0001_init",
		SQL: "CREATE TABLE gen_notes (id INTEGER PRIMARY KEY, title TEXT, gone TEXT);"}}
	changes, err := Generate(context.Background(), existing, []any{&genNote{}})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, c := range changes {
		if c.Destructive {
			found = true
		}
	}
	if !found {
		t.Fatalf("changes = %q, want one marked Destructive for the dropped column", allSQL(changes))
	}
}

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

func allSQL(cs []Change) string {
	var b strings.Builder
	for _, c := range cs {
		b.WriteString(c.SQL)
		b.WriteString("\n")
	}
	return b.String()
}
