package auth

import (
	"context"
	"database/sql"
	"embed"
	"time"

	"amadan.net/rastrillo/rastrillo/migrate"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Schema is auth's own migrations, and only its own — it does not
// embed sessions.Schema. The backfill in 0002 reads the sessions
// table, so a caller must order the sets:
//
//	migrate.Apply(ctx, d, migrate.Merge(sessions.Schema, auth.Schema))
//
// Merge's argument order is apply order, which is how that
// requirement is now stated at the call site — it used to be a
// comment here and an append() that embedded sessions.Migrations into
// this package's own list.
//
// auth_sessions is no longer written to (session storage moved to the
// shared sessions package), but the CREATE TABLE stays: the table is
// additive-only and abandoned in place, per the migration rule. 0002
// is a one-shot upgrade — under the ledger it runs exactly once, where
// the old Migrations []string re-ran it every boot and relied on its
// own DELETE leaving nothing to copy the second time:
//
//  1. copy any live auth_sessions rows into the shared sessions table —
//     additive and idempotent (OR IGNORE) — so an app upgrading from a
//     pre-sessions-core auth does not sign its family out;
//  2. empty auth_sessions.
//
// Accepted cost: rolling back to a pre-sessions-core rastrillo after
// this migration has run finds auth_sessions empty, forcing everyone to
// sign in again on the old code path. One forced re-sign-in on rollback
// beats a revoked session silently coming back to life on every restart.
//
// Only a database that predates the sessions core (no `sessions` table
// yet) hits this: migrate.Apply refuses it outright, since a non-empty,
// ledger-less database that doesn't fully match the replayed set can't
// be adopted automatically — Adoption cannot guess which migrations
// already ran. An app on any recent version already has `sessions` and
// adopts cleanly. Recovery when it happens:
//
//  1. Boot refuses, printing "missing table sessions".
//  2. rastrillo migration baseline --db <path> --through sessions/0001_init
//  3. Apply sessions/0001_init by hand (it's just CREATE TABLE IF NOT
//     EXISTS, so this is safe even if it partly ran already).
//  4. Reboot: auth/0001 no-ops (IF NOT EXISTS), auth/0002 runs the
//     backfill for real. Nobody gets signed out.
//
// Steps 2 and 3 are in that order deliberately; do not "tidy" them
// back. Stamp runs no DDL, so baseline does not need the sessions
// table to exist yet — but between creating the table and stamping,
// the database structurally matches the full set with an empty ledger,
// and that is precisely the state Apply adopts (apply.go gates
// adoption on len(applied) == 0). On a hibernating platform the
// operator does not choose when the app wakes: one inbound request in
// that window adopts, stamps auth/0002 without running it, and strands
// every row in auth_sessions for good. Stamping first makes the ledger
// non-empty, which closes that window permanently. The worst case then
// becomes a wake between steps 2 and 3, where auth/0002 fails loudly
// with "no such table: sessions" and rolls back — auth/0001's ledger
// row having committed in its own transaction — and the reboot in
// step 4 still runs the backfill. A loud refusal beats a silent
// stranding.
//
// The trap is baseline with no --through: it stamps every migration,
// including 0002, so the backfill never runs and the rows in
// auth_sessions are stranded — silently signing everyone out.
var Schema = migrate.MustFromFS(migrationFS, "auth")

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
// boot, a sidecar pass, or not at all. Session rows are the sessions
// core's own table now, so sweeping them is delegated there.
func (a *Auth) Sweep(now time.Time) error {
	cutoff := now.UTC().Format(time.RFC3339)
	if _, err := a.cfg.DB.Exec(`DELETE FROM auth_links WHERE expires_at < ?`, cutoff); err != nil {
		return err
	}
	return a.sessions.Sweep(now)
}
