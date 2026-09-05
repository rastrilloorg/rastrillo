package pow

import (
	"context"
	"database/sql"
	"embed"
	"sync"
	"time"

	"amadan.net/rastrillo/rastrillo/migrate"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Schema is pow's own migrations — one table of spent nonces. Merge it
// with the rest of your app's set:
//
//	migrate.Apply(ctx, d, migrate.Merge(sessions.Schema, pow.Schema, app.Schema))
var Schema = migrate.MustFromFS(migrationFS, "pow")

// NonceStore remembers which challenges have been spent, so a solved
// one cannot be replayed for the rest of its life.
//
// Spend records a nonce and reports whether it was fresh — false means
// this exact challenge has already been accepted. It has to be atomic:
// a SELECT followed by an INSERT lets two concurrent replays both
// observe an empty table, which is the race single-use exists to close.
type NonceStore interface {
	Spend(ctx context.Context, nonce string, expires time.Time) (bool, error)
	Sweep(now time.Time) error
}

// SQLNonces stores spent nonces in the app database, which is what any
// app that can restart wants: an in-memory store forgets every spent
// nonce when the process does, and a deploy is then a window in which
// every challenge minted in the previous two hours is replayable again.
func SQLNonces(db *sql.DB) NonceStore { return &sqlNonces{db: db} }

type sqlNonces struct{ db *sql.DB }

// Spend is one statement. INSERT OR IGNORE plus RowsAffected answers
// "was it fresh?" without a read, so there is no window between looking
// and writing — the same reason auth takes a link with a single
// DELETE ... RETURNING.
func (s *sqlNonces) Spend(ctx context.Context, nonce string, expires time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO pow_spent_nonces (nonce, expires_at) VALUES (?, ?)`,
		nonce, expires.UTC().Format(time.RFC3339))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func (s *sqlNonces) Sweep(now time.Time) error {
	_, err := s.db.Exec(`DELETE FROM pow_spent_nonces WHERE expires_at < ?`,
		now.UTC().Format(time.RFC3339))
	return err
}

// MemoryNonces keeps spent nonces in the process. It is honest for a
// test, and for a single process that can afford to forget them on a
// restart — but a restart is exactly when an attacker's stockpile of
// solved challenges becomes spendable again, so reach for SQLNonces
// unless you have a reason.
func MemoryNonces() NonceStore {
	return &memNonces{spent: make(map[string]time.Time)}
}

type memNonces struct {
	mu    sync.Mutex
	spent map[string]time.Time
}

func (m *memNonces) Spend(_ context.Context, nonce string, expires time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.spent[nonce]; ok {
		return false, nil
	}
	m.spent[nonce] = expires
	return true, nil
}

func (m *memNonces) Sweep(now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for nonce, expires := range m.spent {
		if expires.Before(now) {
			delete(m.spent, nonce)
		}
	}
	return nil
}
