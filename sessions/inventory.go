package sessions

import (
	"database/sql"
	"errors"
	"net/http"
	"time"
)

// The inventory: what an app needs to show a person their own live
// sessions and let them end one. Rows are the truth of who is signed
// in (a deleted row is a dead session, whatever cookie a browser still
// holds), so this is where revocation lives; what a session looks like
// from the outside — the browser, the address it came from, when it
// was last seen — is the app's to remember, keyed by Info.Hash.

// Info is one live session as the inventory sees it: the row without
// its token. Hash is the storage hash, an opaque handle for Revoke —
// it names the row and opens nothing, so it is safe to carry in a form.
type Info struct {
	Hash string
	Session
	ExpiresAt time.Time
}

// ErrNotYours is Revoke's answer to a hash that is not one of the
// subject's sessions — an unknown row and another person's row alike,
// so a guessed handle learns nothing.
var ErrNotYours = errors.New("rastrillo/sessions: no such session for this subject")

// Hash is the storage hash of the session token r presents, whether or
// not that token resolves to a live row. It is how a handler finds its
// own row in List, and the one RevokeOthers keeps.
func (s *Sessions) Hash(r *http.Request) (string, bool) {
	c, err := r.Cookie(s.CookieName())
	if err != nil || c.Value == "" {
		return "", false
	}
	return HashToken(c.Value), true
}

// List is every live session subject holds, newest first.
func (s *Sessions) List(subject string) ([]Info, error) {
	now := time.Now()
	rows, err := s.cfg.DB.Query(`SELECT token_hash, method, auth_time, created_at, expires_at
		FROM sessions WHERE subject = ? AND expires_at > ? ORDER BY created_at DESC, token_hash`,
		subject, now.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Info
	for rows.Next() {
		var in Info
		var method, authTime, created, expires string
		if err := rows.Scan(&in.Hash, &method, &authTime, &created, &expires); err != nil {
			return nil, err
		}
		in.Subject, in.Method = subject, method
		if at, err := time.Parse(time.RFC3339, created); err == nil {
			in.At = at
		}
		if authTime != "" {
			if at, err := time.Parse(time.RFC3339, authTime); err == nil {
				in.AuthTime = at
			}
		}
		if at, err := time.Parse(time.RFC3339, expires); err == nil {
			in.ExpiresAt = at
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

// Revoke ends one of subject's sessions by its Info.Hash. The subject
// is part of the key on purpose: a handle from List is only ever
// redeemed against the person who listed it.
func (s *Sessions) Revoke(subject, hash string) error {
	res, err := s.cfg.DB.Exec(`DELETE FROM sessions WHERE token_hash = ? AND subject = ?`, hash, subject)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotYours
	}
	return nil
}

// RevokeOthers ends every session subject holds except the one r
// presents — what a credential change does, so a stolen session does
// not outlive the password it was stolen alongside — and returns how
// many it ended. With no cookie on r, every session goes.
func (s *Sessions) RevokeOthers(r *http.Request, subject string) (int, error) {
	keep, _ := s.Hash(r)
	return s.revokeWhere(`subject = ? AND token_hash <> ?`, subject, keep)
}

// RevokeAll ends every session subject holds, the presented one
// included: "sign out everywhere". The caller clears its own cookie.
func (s *Sessions) RevokeAll(subject string) (int, error) {
	return s.revokeWhere(`subject = ?`, subject)
}

func (s *Sessions) revokeWhere(where string, args ...any) (int, error) {
	res, err := s.cfg.DB.Exec(`DELETE FROM sessions WHERE `+where, args...)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	return int(n), nil
}
