package pow

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"amadan.net/rastrillo/rastrillo/migrate"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Schema is pow's own migrations — one table of spent nonces. Merge it
// with the rest of your app's set and apply it BEFORE pow.New, which
// checks the table is there:
//
//	migrate.Apply(ctx, d, migrate.Merge(sessions.Schema, pow.Schema, app.Schema))
var Schema = migrate.MustFromFS(migrationFS, "pow")

// ErrNoSchema means pow_spent_nonces is missing or predates v2. New
// refuses to build a Guard over it: otherwise every submission is
// ReasonUnavailable and the first visitor finds out.
var ErrNoSchema = errors.New("rastrillo/pow: the pow_spent_nonces table is missing or out of date: merge pow.Schema into the set you migrate.Apply before pow.New")

// Execer is what Commit spends on: *sql.Tx, *sql.DB, *sql.Conn, or a
// GORM transaction's tx.Statement.ConnPool. It has no query method on
// purpose. Commit must never read through another handle while the
// caller's transaction holds the only connection: that read would wait
// for the transaction, which waits for Commit.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// NonceStore remembers which challenges have been spent.
//
// Spend records a nonce and reports whether it was fresh. It must be
// atomic, and it must refuse a nonce whose expires has passed on the
// store's own clock, inside the same statement: a check done before
// calling it can be overtaken by a stalled request and a sweep, and the
// same token would then be spent twice. ex is the caller's transaction,
// or nil for the store's own handle.
type NonceStore interface {
	Spend(ctx context.Context, ex Execer, nonce string, expires time.Time) (bool, error)
	Spent(ctx context.Context, nonce string) (bool, error)
	Ready(ctx context.Context) error
	Sweep(now time.Time) error
}

// sweepMargin is how long past its sealed expiry a spent row is kept.
// The spend refuses anything past expiry on the database clock, so any
// margin closes replay; ten minutes keeps a clock step or a slow sweep
// from mattering.
const sweepMargin = 10 * time.Minute

// sweepBatch bounds one Sweep. A sweep on wake that deleted everything
// at once would hold SQLite's single writer while the first visitor
// waits.
const sweepBatch = 500

// dbNowMillis is SQLite's clock in milliseconds. Spend and Sweep both
// read it, on the one writer, so they agree on what "now" is.
const dbNowMillis = `CAST(unixepoch('subsec') * 1000 AS INTEGER)`

// SQLNonces stores spent nonces in the app database. An in-memory store
// forgets them on restart, and a deploy is then a window in which every
// challenge minted in the last MaxAge is replayable.
func SQLNonces(db *sql.DB) NonceStore { return &sqlNonces{db: db} }

type sqlNonces struct{ db *sql.DB }

func (s *sqlNonces) Spend(ctx context.Context, ex Execer, nonce string, expires time.Time) (bool, error) {
	if ex == nil {
		ex = s.db
	}
	ms := expires.UnixMilli()
	res, err := ex.ExecContext(ctx,
		`INSERT OR IGNORE INTO pow_spent_nonces (nonce, expires_ms) SELECT ?, ? WHERE `+dbNowMillis+` <= ?`,
		nonce, ms, ms)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func (s *sqlNonces) Spent(ctx context.Context, nonce string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM pow_spent_nonces WHERE nonce = ?`, nonce).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// Ready selects the v2 columns. "no such table" and "no such column"
// are both a migration that did not run, and both become ErrNoSchema so
// the boot error names the fix; anything else is passed through.
func (s *sqlNonces) Ready(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT nonce, expires_ms FROM pow_spent_nonces LIMIT 0`)
	if err != nil {
		if strings.Contains(err.Error(), "no such") {
			return fmt.Errorf("%w (%v)", ErrNoSchema, err)
		}
		return err
	}
	return rows.Close()
}

func (s *sqlNonces) Sweep(time.Time) error {
	_, err := s.db.Exec(`DELETE FROM pow_spent_nonces WHERE rowid IN (
		SELECT rowid FROM pow_spent_nonces WHERE expires_ms < `+dbNowMillis+` - ? LIMIT ?)`,
		sweepMargin.Milliseconds(), sweepBatch)
	return err
}

// MemoryNonces keeps spent nonces in the process, under the same rules
// as SQLNonces. It ignores the caller's transaction, so a spend inside
// a transaction that rolls back stays spent: honest for tests and a
// single process that can afford to forget, wrong for anything else.
func MemoryNonces() NonceStore {
	return &memNonces{spent: make(map[string]time.Time), now: time.Now}
}

type memNonces struct {
	mu    sync.Mutex
	spent map[string]time.Time
	now   func() time.Time
}

func (m *memNonces) Spend(_ context.Context, _ Execer, nonce string, expires time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.now().After(expires) {
		return false, nil
	}
	if _, ok := m.spent[nonce]; ok {
		return false, nil
	}
	m.spent[nonce] = expires
	return true, nil
}

func (m *memNonces) Spent(_ context.Context, nonce string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.spent[nonce]
	return ok, nil
}

func (m *memNonces) Ready(context.Context) error { return nil }

func (m *memNonces) Sweep(now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for nonce, expires := range m.spent {
		if n == sweepBatch {
			break
		}
		if expires.Before(now.Add(-sweepMargin)) {
			delete(m.spent, nonce)
			n++
		}
	}
	return nil
}
