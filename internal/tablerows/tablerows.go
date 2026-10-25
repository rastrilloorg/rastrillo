// Package tablerows counts the rows of every table in a SQLite
// database, for tests that must show a request wrote nothing. A count
// per table, not a total: a request that deleted one row and inserted
// another would leave a total unchanged.
package tablerows

import (
	"database/sql"
	"maps"
	"testing"
)

// Count returns each table's row count, keyed by table name.
func Count(t testing.TB, db *sql.DB) map[string]int {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	counts := make(map[string]int, len(names))
	for _, n := range names {
		var c int
		if err := db.QueryRow(`SELECT COUNT(*) FROM "` + n + `"`).Scan(&c); err != nil {
			t.Fatal(err)
		}
		counts[n] = c
	}
	return counts
}

// Unchanged fails t, naming what, for every table whose count differs
// between before and Count(t, db) now.
func Unchanged(t testing.TB, db *sql.DB, what string, before map[string]int) {
	t.Helper()
	after := Count(t, db)
	if maps.Equal(before, after) {
		return
	}
	for name, n := range after {
		if before[name] != n {
			t.Errorf("%s: table %s has %d rows, want %d", what, name, n, before[name])
		}
	}
	for name, n := range before {
		if _, ok := after[name]; !ok {
			t.Errorf("%s: table %s is gone, had %d rows", what, name, n)
		}
	}
}
