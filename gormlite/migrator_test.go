package gormlite

import (
	"strings"
	"testing"

	"gorm.io/gorm"
)

// dropColumnWidget stands in for a table created by a hand-written
// migration: its DDL (built below with a raw Exec, not AutoMigrate) is
// unquoted, which is exactly the shape DropColumn silently failed on.
type dropColumnWidget struct {
	ID        int    `gorm:"column:id;primaryKey"`
	TokenHash string `gorm:"column:token_hash"`
}

func (dropColumnWidget) TableName() string { return "drop_column_widgets" }

func openDropColumnTestDB(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open: %v", err)
	}
	// Hand-written DDL: unquoted identifiers, as a migration author would
	// write it directly rather than through GORM's AutoMigrate (which
	// always quotes). This is the shape parseDDL stores unaltered and
	// trims, reproducing the exact stored text that broke removeColumn.
	if err := db.Exec("CREATE TABLE drop_column_widgets (id INTEGER PRIMARY KEY, token_hash TEXT)").Error; err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db
}

// TestDropColumn_UnquotedDDL proves the fix end to end: dropping a column
// stored as unquoted DDL must actually remove it, not just report success.
func TestDropColumn_UnquotedDDL(t *testing.T) {
	db := openDropColumnTestDB(t, "file:dropcolumn_unquoted?mode=memory&cache=shared")

	if !db.Migrator().HasColumn(&dropColumnWidget{}, "token_hash") {
		t.Fatal("precondition failed: token_hash should exist before DropColumn")
	}

	if err := db.Migrator().DropColumn(&dropColumnWidget{}, "token_hash"); err != nil {
		t.Fatalf("DropColumn: %v", err)
	}

	if db.Migrator().HasColumn(&dropColumnWidget{}, "token_hash") {
		t.Fatal("token_hash still present after DropColumn: silent no-op")
	}
}

// TestDropColumn_MissingColumn proves DropColumn reports failure instead
// of silently succeeding when the column isn't in the table's stored
// definition at all (e.g. it was already dropped, or never existed).
func TestDropColumn_MissingColumn(t *testing.T) {
	db := openDropColumnTestDB(t, "file:dropcolumn_missing?mode=memory&cache=shared")

	err := db.Migrator().DropColumn(&dropColumnWidget{}, "does_not_exist")
	if err == nil {
		t.Fatal("expected an error for a column absent from the stored DDL, got nil")
	}
	if !strings.Contains(err.Error(), "does_not_exist") || !strings.Contains(err.Error(), "drop_column_widgets") {
		t.Fatalf("error should name the column and the table, got: %v", err)
	}
}
