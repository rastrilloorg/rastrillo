package migrate

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Memory opens an empty in-memory database. Connections are capped at
// one because a second connection to ":memory:" is a second, empty
// database — the caller closes it.
func Memory() (*sql.DB, error) {
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}
	d.SetMaxOpenConns(1)
	return d, nil
}

// Replay applies every SQL migration in ms to a fresh in-memory
// database and returns it. Go migrations are skipped: they may need
// data or services that do not exist here, and the structure they
// produce is not what a schema comparison is about.
//
// This is the "current schema" side of every comparison in the
// package — adoption, check, and generate all start here, and none of
// them touches a real database.
func Replay(ctx context.Context, ms []Migration) (*sql.DB, error) {
	d, err := Memory()
	if err != nil {
		return nil, err
	}
	for _, m := range ms {
		if m.SQL == "" {
			continue
		}
		if _, err := d.ExecContext(ctx, m.SQL); err != nil {
			d.Close()
			return nil, fmt.Errorf("migrate: replay %s: %w", m.ID, err)
		}
	}
	return d, nil
}
