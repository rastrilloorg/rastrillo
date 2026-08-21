package auth

import (
	"context"
	"database/sql"
	"time"

	"github.com/carlosframework/rastrillo/sessions"
)

// Migrations is the package's schema — additive and idempotent, meant to
// be appended to an app's Options.Migrations. Tokens never touch any of
// these tables: all store SHA-256 hashes only.
//
// auth_sessions is no longer written to (session storage moved to the
// shared sessions package), but the CREATE TABLE stays: the table is
// additive-only and abandoned in place, per the migration rule. The
// final statement copies any live auth_sessions rows into the sessions
// table — additive and idempotent (OR IGNORE) — so an app upgrading
// from a pre-sessions-core auth does not sign its family out. It must
// come after sessions.Migrations so the sessions table already exists.
var Migrations = append(append([]string{
	`CREATE TABLE IF NOT EXISTS auth_links (
	  hash       TEXT PRIMARY KEY,
	  address    TEXT NOT NULL,
	  purpose    TEXT NOT NULL,
	  expires_at TEXT NOT NULL
	);`,
	`CREATE TABLE IF NOT EXISTS auth_sessions (
	  token_hash TEXT PRIMARY KEY,
	  address    TEXT NOT NULL,
	  method     TEXT NOT NULL,
	  auth_time  TEXT NOT NULL DEFAULT '',
	  created_at TEXT NOT NULL,
	  expires_at TEXT NOT NULL
	);`,
}, sessions.Migrations...),
	// Copy any live auth_sessions rows into the shared sessions table —
	// additive and idempotent (OR IGNORE), so upgrading does not sign
	// the family out. The old table stays, abandoned, per the
	// additive-only rule.
	`INSERT OR IGNORE INTO sessions (token_hash, subject, method, auth_time, created_at, expires_at)
	   SELECT token_hash, address, method, auth_time, created_at, expires_at FROM auth_sessions;`,
)

// linkStore implements signin.LinkStore over the app database.
type linkStore struct{ db *sql.DB }

func (l *linkStore) PutLink(ctx context.Context, hash, address, purpose string, expires time.Time) error {
	_, err := l.db.ExecContext(ctx, `INSERT OR REPLACE INTO auth_links (hash, address, purpose, expires_at)
		VALUES (?, ?, ?, ?)`, hash, address, purpose, expires.UTC().Format(time.RFC3339))
	return err
}

// TakeLink consumes a link in one DELETE ... RETURNING statement. A
// split SELECT-then-DELETE would let two concurrent callers both
// observe the row before either deletes it — even at SetMaxOpenConns(1)
// — defeating single use; one statement closes the race (seapointish's
// fix, kept). Unknown hash, wrong purpose and expired row are all the
// same "not ok": telling them apart would be an oracle. And the row is
// gone even when expired — a presented token is spent either way.
func (l *linkStore) TakeLink(ctx context.Context, hash, purpose string) (string, bool, error) {
	var address, expires string
	err := l.db.QueryRowContext(ctx, `DELETE FROM auth_links WHERE hash = ? AND purpose = ?
		RETURNING address, expires_at`, hash, purpose).Scan(&address, &expires)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	at, err := time.Parse(time.RFC3339, expires)
	if err != nil || time.Now().After(at) {
		return "", false, nil
	}
	return address, true, nil
}

// Sweep deletes expired links and sessions. Correctness never depends
// on it — TakeLink and the sessions core's own expiry check handle it
// themselves — its job is keeping unclicked links and abandoned
// sessions from accumulating for the life of the instance. Call it from
// boot, a sidecar pass, or not at all.
func (a *Auth) Sweep(now time.Time) error {
	cutoff := now.UTC().Format(time.RFC3339)
	if _, err := a.cfg.DB.Exec(`DELETE FROM auth_links WHERE expires_at < ?`, cutoff); err != nil {
		return err
	}
	_, err := a.cfg.DB.Exec(`DELETE FROM sessions WHERE expires_at < ?`, cutoff)
	return err
}
