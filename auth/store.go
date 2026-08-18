package auth

import (
	"context"
	"database/sql"
	"time"

	"github.com/keymaildev/signin"
)

// Migrations is the package's schema — additive and idempotent, meant to
// be appended to an app's Options.Migrations. Tokens never touch either
// table: both store SHA-256 hashes only.
var Migrations = []string{
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
}

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

// createSession stores a fresh session row for a verified identity.
func (a *Auth) createSession(hash string, id Identity, now time.Time) error {
	authTime := ""
	if !id.AuthTime.IsZero() {
		authTime = id.AuthTime.UTC().Format(time.RFC3339)
	}
	_, err := a.cfg.DB.Exec(`INSERT INTO auth_sessions (token_hash, address, method, auth_time, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		hash, id.Address, string(id.Method), authTime,
		now.UTC().Format(time.RFC3339), now.Add(a.cfg.SessionTTL).UTC().Format(time.RFC3339))
	return err
}

// lookupSession resolves a token hash to its identity, expiry-checked.
func (a *Auth) lookupSession(hash string, now time.Time) (Identity, bool, error) {
	var id Identity
	var method, authTime, created, expires string
	err := a.cfg.DB.QueryRow(`SELECT address, method, auth_time, created_at, expires_at
		FROM auth_sessions WHERE token_hash = ?`, hash).
		Scan(&id.Address, &method, &authTime, &created, &expires)
	if err == sql.ErrNoRows {
		return Identity{}, false, nil
	}
	if err != nil {
		return Identity{}, false, err
	}
	exp, err := time.Parse(time.RFC3339, expires)
	if err != nil || now.After(exp) {
		return Identity{}, false, nil
	}
	id.Method = signin.Method(method)
	if at, err := time.Parse(time.RFC3339, created); err == nil {
		id.At = at
	}
	if authTime != "" {
		if at, err := time.Parse(time.RFC3339, authTime); err == nil {
			id.AuthTime = at
		}
	}
	return id, true, nil
}

// deleteSession revokes one session row — real revocation, not just a
// cleared cookie.
func (a *Auth) deleteSession(hash string) error {
	_, err := a.cfg.DB.Exec(`DELETE FROM auth_sessions WHERE token_hash = ?`, hash)
	return err
}

// Sweep deletes expired links and sessions. Correctness never depends
// on it — TakeLink and lookupSession check expiry themselves — its job
// is keeping unclicked links and abandoned sessions from accumulating
// for the life of the instance. Call it from boot, a sidecar pass, or
// not at all.
func (a *Auth) Sweep(now time.Time) error {
	cutoff := now.UTC().Format(time.RFC3339)
	if _, err := a.cfg.DB.Exec(`DELETE FROM auth_links WHERE expires_at < ?`, cutoff); err != nil {
		return err
	}
	_, err := a.cfg.DB.Exec(`DELETE FROM auth_sessions WHERE expires_at < ?`, cutoff)
	return err
}
