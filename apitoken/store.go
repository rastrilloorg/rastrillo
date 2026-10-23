package apitoken

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/api"
)

// Config is the store's wiring. DB and Prefix are required.
type Config struct {
	// DB holds the api_tokens table (Schema).
	DB *sql.DB
	// Prefix starts every token, ^[a-z][a-z0-9]{0,15}$. DerivePrefix
	// makes one from the app name. Changing it invalidates every token
	// already issued, since Verify refuses any other prefix.
	Prefix string
	// Allowed says whether a token's owner may still use the app: a
	// suspended or deleted account is refused even with a live token.
	// Nil allows everyone. An error answers 503, not 401.
	Allowed func(ctx context.Context, subject string) (bool, error)
	// Now is the clock. Default time.Now.
	Now func() time.Time
}

// Token is one row of api_tokens: everything about a token except its
// secret, which is never stored.
type Token struct {
	ID, Subject, Agent             string
	Access                         rastrillo.Access
	CreatedAt                      time.Time
	LastUsed, ExpiresAt, RevokedAt *time.Time
}

// Actor is who the token acts as: the person, the agent if there is
// one, the token as the credential, and its access. It is what Verify
// answers for a request on this token, so a page that names the actor
// behind a token names the same one the API logs.
func (t Token) Actor() rastrillo.Actor {
	return rastrillo.Actor{Subject: t.Subject, Agent: t.Agent, Credential: t.ID, Access: t.Access}
}

// MintInput is what the person chose on the token page.
type MintInput struct {
	// Agent names the agent the token is for ("Claude"). It must be ""
	// exactly when ForSelf is set.
	Agent string
	// ForSelf marks a token for the person themselves, for a script
	// with no AI in it. It is a separate field, not an empty Agent, so
	// a form that lost its agent name is refused instead of minting a
	// token that looks like the person acting directly.
	ForSelf bool
	// Access is Read or Write; zero is refused.
	Access rastrillo.Access
	// TTL is how long the token lives. 0 means it never expires.
	TTL time.Duration
}

// Store mints, verifies and revokes tokens. Build one per process.
type Store struct {
	cfg    Config
	redact *regexp.Regexp
}

// New validates cfg and returns a Store.
func New(cfg Config) (*Store, error) {
	if cfg.DB == nil {
		return nil, errors.New("rastrillo/apitoken: Config.DB is required")
	}
	if !prefixRE.MatchString(cfg.Prefix) {
		return nil, fmt.Errorf("rastrillo/apitoken: Config.Prefix %q must match %s; DerivePrefix makes one from the app name", cfg.Prefix, prefixRE)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Store{
		cfg:    cfg,
		redact: regexp.MustCompile(`(` + regexp.QuoteMeta(cfg.Prefix) + `_[0-9A-Za-z]{12}_)[0-9A-Za-z]+`),
	}, nil
}

// now is the clock in UTC, cut to the second the table stores, so the
// Token Mint returns equals the one List reads back.
func (s *Store) now() time.Time { return s.cfg.Now().UTC().Truncate(time.Second) }

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// parseTime reads a time column back. It accepts only the exact form
// formatTime writes: Verify's last_used UPDATE compares these strings
// in SQL, and that comparison is only chronological while every value
// has the same width and zone. A value in any other form is a row this
// package did not write, which is a storage failure, not a time.
func parseTime(v sql.NullString) (*time.Time, error) {
	if !v.Valid {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, v.String)
	if err != nil {
		return nil, err
	}
	if formatTime(t) != v.String {
		return nil, fmt.Errorf("time %q is not UTC RFC 3339 to the second", v.String)
	}
	return &t, nil
}

func parseAccess(v string) (rastrillo.Access, error) {
	switch v {
	case "read":
		return rastrillo.Read, nil
	case "write":
		return rastrillo.Write, nil
	}
	return 0, fmt.Errorf("access %q is not read or write", v)
}

// Mint creates a token for subject and returns it whole. The returned
// string is the only copy of the secret: show it once and drop it.
func (s *Store) Mint(ctx context.Context, subject string, in MintInput) (string, Token, error) {
	if subject == "" {
		return "", Token{}, errors.New("rastrillo/apitoken: Mint needs a subject")
	}
	if !in.Access.Valid() {
		return "", Token{}, fmt.Errorf("rastrillo/apitoken: Mint Access %d is not Read or Write", in.Access)
	}
	if in.TTL < 0 {
		return "", Token{}, errors.New("rastrillo/apitoken: Mint TTL is negative; use 0 for never")
	}
	agent := ""
	if in.ForSelf {
		if in.Agent != "" {
			return "", Token{}, errors.New("rastrillo/apitoken: Mint has ForSelf and an agent name; choose one")
		}
	} else {
		var ok bool
		if agent, ok = ValidAgentName(in.Agent); !ok {
			return "", Token{}, errors.New("rastrillo/apitoken: Mint needs an agent name of 1 to 40 letters, digits, spaces, hyphens and dots, or ForSelf")
		}
	}
	id, err := random(idLen)
	if err != nil {
		return "", Token{}, err
	}
	secret, err := random(secretLen)
	if err != nil {
		return "", Token{}, err
	}
	hash := sha256.Sum256([]byte(secret))
	t := Token{ID: id, Subject: subject, Agent: agent, Access: in.Access, CreatedAt: s.now()}
	var expires any
	if in.TTL > 0 {
		e := t.CreatedAt.Add(in.TTL)
		t.ExpiresAt = &e
		expires = formatTime(e)
	}
	if _, err := s.cfg.DB.ExecContext(ctx,
		`INSERT INTO api_tokens (id, subject, agent, access, secret_hash, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, subject, agent, in.Access.String(), hash[:], formatTime(t.CreatedAt), expires); err != nil {
		return "", Token{}, fmt.Errorf("rastrillo/apitoken: Mint: %w", err)
	}
	return s.cfg.Prefix + "_" + id + "_" + secret, t, nil
}

// Verify implements api.Verifier: it turns the request's bearer token
// into the owner's Actor. Subject, Agent, Credential and Access all
// come from the row; nothing the request says can change them.
//
// Every way a token fails (absent, malformed, another prefix, unknown,
// wrong secret, revoked, expired, owner not Allowed) is the same
// api.ErrUnauthorized, so a caller probing with stolen or guessed
// tokens learns nothing about which part was wrong. The wrapped detail
// is for the log only.
//
// A database or Allowed error is api.ErrUnavailable instead, a 503: an
// outage must not look like a revoked token, or an agent would discard
// a token that still works.
//
// The secret is checked with SHA-256, not bcrypt. A slow hash makes
// guessing a weak password expensive; a 256-bit random secret cannot be
// guessed, so a stolen SHA-256 of it is as useless as a stolen bcrypt,
// and bcrypt's 50 to 100ms on every API call would spend most of the
// first-byte budget.
func (s *Store) Verify(ctx context.Context, r *http.Request) (rastrillo.Actor, error) {
	// Only the Authorization header: never a cookie, so a page cannot
	// be made to call the API with the browser's credentials, and
	// never the query string, so tokens stay out of access logs.
	hs := r.Header.Values("Authorization")
	if len(hs) != 1 {
		return rastrillo.Actor{}, fmt.Errorf("%w: want one Authorization header, got %d", api.ErrUnauthorized, len(hs))
	}
	scheme, token, _ := strings.Cut(hs[0], " ")
	if !strings.EqualFold(scheme, "Bearer") {
		return rastrillo.Actor{}, fmt.Errorf("%w: not a Bearer token", api.ErrUnauthorized)
	}
	id, secret, ok := s.parse(strings.TrimLeft(token, " "))
	if !ok {
		return rastrillo.Actor{}, fmt.Errorf("%w: not a %s token", api.ErrUnauthorized, s.cfg.Prefix)
	}

	var (
		subject, agent, access string
		hash                   []byte
		lastUsed, expires      sql.NullString
		revoked                sql.NullString
	)
	err := s.cfg.DB.QueryRowContext(ctx,
		`SELECT subject, agent, access, secret_hash, last_used, expires_at, revoked_at FROM api_tokens WHERE id = ?`, id).
		Scan(&subject, &agent, &access, &hash, &lastUsed, &expires, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return rastrillo.Actor{}, fmt.Errorf("%w: no token %s", api.ErrUnauthorized, id)
	}
	if err != nil {
		return rastrillo.Actor{}, fmt.Errorf("%w: apitoken: looking up %s: %w", api.ErrUnavailable, id, err)
	}
	// Constant time on the 32-byte hashes, so how long the comparison
	// takes says nothing about how much of a guessed secret was right.
	sum := sha256.Sum256([]byte(secret))
	if subtle.ConstantTimeCompare(sum[:], hash) != 1 {
		return rastrillo.Actor{}, fmt.Errorf("%w: wrong secret for %s", api.ErrUnauthorized, id)
	}
	if revoked.Valid {
		return rastrillo.Actor{}, fmt.Errorf("%w: %s is revoked", api.ErrUnauthorized, id)
	}
	// A column this package could not have written is a storage
	// failure, so 503 like any other: a 401 would tell the agent its
	// token is dead when the fault is in the database. It still grants
	// nothing. These checks come after the secret, so only the token's
	// holder can learn that its row is damaged.
	exp, err := parseTime(expires)
	if err != nil {
		return rastrillo.Actor{}, fmt.Errorf("%w: apitoken: %s expires_at: %w", api.ErrUnavailable, id, err)
	}
	last, err := parseTime(lastUsed)
	if err != nil {
		return rastrillo.Actor{}, fmt.Errorf("%w: apitoken: %s last_used: %w", api.ErrUnavailable, id, err)
	}
	acc, err := parseAccess(access)
	if err != nil {
		return rastrillo.Actor{}, fmt.Errorf("%w: apitoken: %s: %w", api.ErrUnavailable, id, err)
	}
	now := s.now()
	if exp != nil && !now.Before(*exp) {
		return rastrillo.Actor{}, fmt.Errorf("%w: %s expired", api.ErrUnauthorized, id)
	}
	if s.cfg.Allowed != nil {
		allowed, err := s.cfg.Allowed(ctx, subject)
		if err != nil {
			return rastrillo.Actor{}, fmt.Errorf("%w: apitoken: Allowed(%s): %w", api.ErrUnavailable, subject, err)
		}
		if !allowed {
			return rastrillo.Actor{}, fmt.Errorf("%w: the owner of %s is not allowed", api.ErrUnauthorized, id)
		}
	}
	// At most once a minute, so a busy agent's reads do not each
	// become a write on SQLite's single writer. The Go check skips the
	// write attempt on the common path; the WHERE clause is the rule.
	// Requests that read the row together all pass the Go check, and
	// only the condition, rechecked by the database at write time,
	// lets just one of them write. It also stops a slow request from
	// moving last_used back past a newer one's time. The strings
	// compare in time order because parseTime admits only formatTime's
	// fixed-width UTC form.
	if last == nil || now.Sub(*last) >= time.Minute {
		if _, err := s.cfg.DB.ExecContext(ctx,
			`UPDATE api_tokens SET last_used = ? WHERE id = ? AND (last_used IS NULL OR last_used <= ?)`,
			formatTime(now), id, formatTime(now.Add(-time.Minute))); err != nil {
			return rastrillo.Actor{}, fmt.Errorf("%w: apitoken: last_used for %s: %w", api.ErrUnavailable, id, err)
		}
	}
	return rastrillo.Actor{Subject: subject, Agent: agent, Credential: id, Access: acc}, nil
}

// List returns every token subject owns, newest first, including
// revoked and expired ones: RevokedAt and ExpiresAt say which.
func (s *Store) List(ctx context.Context, subject string) ([]Token, error) {
	rows, err := s.cfg.DB.QueryContext(ctx,
		`SELECT id, agent, access, created_at, last_used, expires_at, revoked_at FROM api_tokens
		 WHERE subject = ? ORDER BY created_at DESC, id DESC`, subject)
	if err != nil {
		return nil, fmt.Errorf("rastrillo/apitoken: List: %w", err)
	}
	defer rows.Close()
	var out []Token
	for rows.Next() {
		var (
			t                          = Token{Subject: subject}
			access, created            string
			lastUsed, expires, revoked sql.NullString
		)
		if err := rows.Scan(&t.ID, &t.Agent, &access, &created, &lastUsed, &expires, &revoked); err != nil {
			return nil, fmt.Errorf("rastrillo/apitoken: List: %w", err)
		}
		// A damaged row fails the whole list rather than showing the
		// page a zero Access or a missing expiry as if they were real.
		if t.Access, err = parseAccess(access); err != nil {
			return nil, fmt.Errorf("rastrillo/apitoken: List: %s: %w", t.ID, err)
		}
		at, err := parseTime(sql.NullString{String: created, Valid: true})
		if err != nil {
			return nil, fmt.Errorf("rastrillo/apitoken: List: %s created_at: %w", t.ID, err)
		}
		t.CreatedAt = *at
		for _, f := range []struct {
			dst **time.Time
			v   sql.NullString
		}{{&t.LastUsed, lastUsed}, {&t.ExpiresAt, expires}, {&t.RevokedAt, revoked}} {
			if *f.dst, err = parseTime(f.v); err != nil {
				return nil, fmt.Errorf("rastrillo/apitoken: List: %s: %w", t.ID, err)
			}
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Revoke ends one of subject's tokens. Another person's token ID, or an
// unknown one, is sql.ErrNoRows, so the page answers 404 and does not
// reveal that the ID exists. Revoking twice keeps the first time.
//
// Every method here takes the subject and puts it in the WHERE clause
// beside the id: the subject must come from the session, never from the
// form, and the query is what makes a forged ID harmless.
func (s *Store) Revoke(ctx context.Context, subject, id string) error {
	res, err := s.cfg.DB.ExecContext(ctx,
		`UPDATE api_tokens SET revoked_at = COALESCE(revoked_at, ?) WHERE id = ? AND subject = ?`,
		formatTime(s.now()), id, subject)
	if err != nil {
		return fmt.Errorf("rastrillo/apitoken: Revoke: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("rastrillo/apitoken: Revoke: %w", err)
	} else if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// RevokeAgent ends every live token subject made for agent, and returns
// how many. The name is matched as stored, after the same trimming
// Mint does. An empty name is refused: it would otherwise revoke the
// person's own tokens, which is not what a per-agent kill switch says.
func (s *Store) RevokeAgent(ctx context.Context, subject, agent string) (int64, error) {
	name, ok := ValidAgentName(agent)
	if !ok {
		return 0, errors.New("rastrillo/apitoken: RevokeAgent needs an agent name")
	}
	return s.revoke(ctx, `UPDATE api_tokens SET revoked_at = ? WHERE subject = ? AND agent = ? AND revoked_at IS NULL`,
		formatTime(s.now()), subject, name)
}

// RevokeAll ends every live token subject owns, and returns how many.
func (s *Store) RevokeAll(ctx context.Context, subject string) (int64, error) {
	return s.revoke(ctx, `UPDATE api_tokens SET revoked_at = ? WHERE subject = ? AND revoked_at IS NULL`,
		formatTime(s.now()), subject)
}

func (s *Store) revoke(ctx context.Context, query string, args ...any) (int64, error) {
	res, err := s.cfg.DB.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("rastrillo/apitoken: revoke: %w", err)
	}
	return res.RowsAffected()
}
