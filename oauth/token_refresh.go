package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"time"

	rastrillo "amadan.net/rastrillo/rastrillo"
)

// refresh trades a refresh token for new tokens. A token already
// rotated is reuse, and is caught before any parameter is checked, so
// a replay can't hide behind a wrong one.
func (server *Server) refresh(
	ctx context.Context, form url.Values,
) (tokenAnswer, *problem) {
	raw := form.Get("refresh_token")
	if raw == "" {
		return tokenAnswer{}, missing("refresh_token")
	}
	sum := sha256.Sum256([]byte(raw))
	connection, retired, err := server.cfg.Store.RefreshConnection(ctx, sum[:],
		server.cfg.Now())
	if errors.Is(err, ErrNotFound) {
		return tokenAnswer{}, refuse("invalid_grant", msgBadRefresh)
	}
	if err != nil {
		return tokenAnswer{}, server.serverError("reading a refresh token", err)
	}
	if retired {
		return tokenAnswer{}, server.reused(ctx, connection.ID, msgReused)
	}
	access, fail := server.refreshAccess(ctx, connection, form)
	if fail != nil {
		return tokenAnswer{}, fail
	}
	return server.rotate(ctx, connection, sum[:], access)
}

// rotate mints before it rotates, so a failed mint leaves the refresh
// token unspent and the client's retry isn't taken for reuse. A token
// minted for a rotation that then fails reaches nobody, so it is
// revoked.
func (server *Server) rotate(
	ctx context.Context, connection Connection, old []byte,
	access rastrillo.Access,
) (tokenAnswer, *problem) {
	now := server.cfg.Now()
	token, minted, err := server.mint(ctx, connection, access)
	if err != nil {
		return tokenAnswer{}, server.serverError("minting a token", err)
	}
	refresh, next, err := newRefresh(connection.ID, now)
	reused := false
	if err == nil {
		_, reused, err = server.cfg.Store.RotateRefresh(ctx, old, next, now)
	}
	if err == nil && reused {
		return tokenAnswer{}, server.reused(ctx, connection.ID, msgReused)
	}
	if err != nil {
		err = errors.Join(err, server.cfg.Tokens.Revoke(ctx,
			connection.Subject, minted.ID))
	}
	if errors.Is(err, ErrNotFound) {
		return tokenAnswer{}, refuse("invalid_grant", msgBadRefresh)
	}
	if err != nil {
		return tokenAnswer{}, server.serverError("rotating a refresh token",
			err)
	}
	return issue(token, refresh, access), nil
}

// refreshAccess checks the refresh request against its connection and
// returns the access to issue: the connection's, or the narrower scope
// asked for (RFC 6749 §6).
func (server *Server) refreshAccess(
	ctx context.Context, connection Connection, form url.Values,
) (rastrillo.Access, *problem) {
	id, res, scope := form.Get("client_id"),
		form.Get("resource"), form.Get("scope")
	access, valid := parseScope(scope)
	if scope == "" {
		access = connection.Access
	}
	switch {
	case id == "":
		return 0, &problem{http.StatusUnauthorized, "invalid_client",
			msgNoClient}
	case id != connection.ClientID:
		return 0, refuse("invalid_grant", msgOtherClient)
	case res != "" && res != connection.Resource:
		return 0, refuse("invalid_target", msgResource)
	case !valid || access > connection.Access:
		return 0, refuse("invalid_scope", msgScope)
	}
	ok, _, err := server.cfg.Allowed(ctx, connection.Subject)
	if err != nil {
		return 0, server.serverError("checking the person is allowed", err)
	}
	if !ok {
		return 0, refuse("invalid_grant", msgDisallowed)
	}
	return access, nil
}

func newRefresh(
	connection string, now time.Time,
) (string, Refresh, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", Refresh{}, err
	}
	token := refreshPrefix + base64.RawURLEncoding.EncodeToString(secret)
	sum := sha256.Sum256([]byte(token))
	return token, Refresh{
		Hash: sum[:], Connection: connection,
		Expires: now.Add(refreshLife),
	}, nil
}
