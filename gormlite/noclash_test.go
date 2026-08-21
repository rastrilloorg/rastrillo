package gormlite

import (
	"testing"

	_ "modernc.org/sqlite" // must coexist with this package's driver use
	"gorm.io/gorm"
)

// TestNoDriverClash proves a binary can import modernc.org/sqlite (as
// OpenDB does) and this dialector together. With glebarez/sqlite this
// exact arrangement panics at init: sql: Register called twice for
// driver sqlite.
func TestNoDriverClash(t *testing.T) {
	g, err := gorm.Open(Open("file:noclash?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open: %v", err)
	}
	var one int
	if err := g.Raw("SELECT 1").Scan(&one).Error; err != nil || one != 1 {
		t.Fatalf("SELECT 1 = %d, %v", one, err)
	}
}
