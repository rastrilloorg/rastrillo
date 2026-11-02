package apitoken

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/api"
)

// Recheck implements api.Rechecker. It makes every check Verify makes
// and writes nothing, so the gate can call it as each answer goes out
// without touching last_used.
func (store *Store) Recheck(
	ctx context.Context, request *http.Request, _ rastrillo.Actor,
) (rastrillo.Actor, error) {
	found, err := store.lookup(ctx, request)
	return found.actor, err
}

// lookedUp is what lookup read: the owner's Actor, when the token was
// last used, and the clock reading the checks used, so Verify makes its
// one write from the same instant.
type lookedUp struct {
	actor    rastrillo.Actor
	lastUsed *time.Time
	now      time.Time
}

// row is one api_tokens row as the database holds it.
type row struct {
	subject, agent, access string
	hash                   []byte
	lastUsed, expires      sql.NullString
	revoked                sql.NullString
}

// lookup is every check Verify and Recheck share, and it writes
// nothing. It is one function so a rule added to one cannot be missed
// by the other.
func (store *Store) lookup(
	ctx context.Context, request *http.Request,
) (lookedUp, error) {
	id, secret, err := store.credential(request)
	if err != nil {
		return lookedUp{}, err
	}
	stored, err := store.fetch(ctx, id, secret)
	if err != nil {
		return lookedUp{}, err
	}
	exp, last, acc, err := parseColumns(stored, id)
	if err != nil {
		return lookedUp{}, err
	}
	now := store.now()
	if exp != nil && !now.Before(*exp) {
		return lookedUp{}, fmt.Errorf(
			"%w: %s expired", api.ErrUnauthorized, id)
	}
	if err := store.ownerAllowed(ctx, id, stored.subject); err != nil {
		return lookedUp{}, err
	}
	actor := rastrillo.Actor{
		Subject: stored.subject, Agent: stored.agent,
		Credential: id, Access: acc,
	}
	return lookedUp{actor: actor, lastUsed: last, now: now}, nil
}

// parseColumns reads the time and access columns back.
func parseColumns(stored row, id string) (
	expires, lastUsed *time.Time, access rastrillo.Access, err error,
) {
	// A column this package could not have written is a storage
	// failure, so 503 like any other: a 401 would tell the agent its
	// token is dead when the fault is in the database. It still grants
	// nothing. These checks come after the secret, so only the token's
	// holder can learn that its row is damaged.
	expires, err = parseTime(stored.expires)
	if err != nil {
		return nil, nil, 0, fmt.Errorf(
			"%w: apitoken: %s expires_at: %w", api.ErrUnavailable, id, err)
	}
	lastUsed, err = parseTime(stored.lastUsed)
	if err != nil {
		return nil, nil, 0, fmt.Errorf(
			"%w: apitoken: %s last_used: %w", api.ErrUnavailable, id, err)
	}
	access, err = parseAccess(stored.access)
	if err != nil {
		return nil, nil, 0, fmt.Errorf(
			"%w: apitoken: %s: %w", api.ErrUnavailable, id, err)
	}
	return expires, lastUsed, access, nil
}

// credential reads the token out of the request's Authorization header
// and splits it into the row's ID and the secret.
func (store *Store) credential(
	request *http.Request,
) (id, secret string, err error) {
	// Only the Authorization header: never a cookie, so a page cannot
	// be made to call the API with the browser's credentials, and
	// never the query string, so tokens stay out of access logs.
	headers := request.Header.Values("Authorization")
	if len(headers) != 1 {
		return "", "", fmt.Errorf(
			"%w: want one Authorization header, got %d",
			api.ErrUnauthorized, len(headers))
	}
	scheme, token, _ := strings.Cut(headers[0], " ")
	if !strings.EqualFold(scheme, "Bearer") {
		return "", "", fmt.Errorf(
			"%w: not a Bearer token", api.ErrUnauthorized)
	}
	id, secret, ok := store.parse(strings.TrimLeft(token, " "))
	if !ok {
		return "", "", fmt.Errorf(
			"%w: not a %s token", api.ErrUnauthorized, store.cfg.Prefix)
	}
	return id, secret, nil
}

// fetch reads the token's row and checks the secret and that it is
// not revoked.
func (store *Store) fetch(
	ctx context.Context, id, secret string,
) (row, error) {
	var stored row
	err := store.cfg.DB.QueryRowContext(ctx,
		`SELECT subject, agent, access, secret_hash, last_used,
		        expires_at, revoked_at
		 FROM api_tokens WHERE id = ?`, id).Scan(
		&stored.subject, &stored.agent, &stored.access, &stored.hash,
		&stored.lastUsed, &stored.expires, &stored.revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return row{}, fmt.Errorf(
			"%w: no token %s", api.ErrUnauthorized, id)
	}
	if err != nil {
		return row{}, fmt.Errorf(
			"%w: apitoken: looking up %s: %w", api.ErrUnavailable, id, err)
	}
	// Constant time on the 32-byte hashes, so how long the comparison
	// takes says nothing about how much of a guessed secret was right.
	sum := sha256.Sum256([]byte(secret))
	if subtle.ConstantTimeCompare(sum[:], stored.hash) != 1 {
		return row{}, fmt.Errorf(
			"%w: wrong secret for %s", api.ErrUnauthorized, id)
	}
	if stored.revoked.Valid {
		return row{}, fmt.Errorf(
			"%w: %s is revoked", api.ErrUnauthorized, id)
	}
	return stored, nil
}

// ownerAllowed asks Config.Allowed, when set, whether the token's owner
// may still use the app.
func (store *Store) ownerAllowed(
	ctx context.Context, id, subject string,
) error {
	if store.cfg.Allowed == nil {
		return nil
	}
	allowed, err := store.cfg.Allowed(ctx, subject)
	if err != nil {
		return fmt.Errorf("%w: apitoken: Allowed(%s): %w",
			api.ErrUnavailable, subject, err)
	}
	if !allowed {
		return fmt.Errorf("%w: the owner of %s is not allowed",
			api.ErrUnauthorized, id)
	}
	return nil
}
