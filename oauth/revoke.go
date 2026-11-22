package oauth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"net/http"
	"strings"
)

// revoke answers POST /oauth/revoke (RFC 7009). Either kind of token
// ends its whole connection, when client_id names the client it was
// issued to. A token this server doesn't know is a 200 all the same:
// the client wants it gone, and it is. token_type_hint is ignored,
// since the token's form says its kind.
func (server *Server) revoke(
	response http.ResponseWriter, request *http.Request,
) {
	form, fail := readForm(response, request)
	switch {
	case fail != nil:
	case form.Get("token") == "":
		fail = missing("token")
	case form.Get("client_id") == "":
		fail = &problem{http.StatusUnauthorized, "invalid_client",
			msgNoClient}
	}
	if fail != nil {
		fail.write(response)
		return
	}
	ctx := request.Context()
	connection, err := server.connectionOf(ctx, form.Get("token"))
	if err == nil && connection.ID != "" {
		if connection.ClientID != form.Get("client_id") {
			refuse("invalid_grant", msgNotIssuedTo).write(response)
			return
		}
		err = server.end(ctx, connection.ID)
	}
	if err != nil && !errors.Is(err, ErrNotFound) {
		server.cfg.Logger.Error("oauth: revoking a token", "err", err)
		response.Header().Set("Retry-After", "5")
		oauthError(response, http.StatusServiceUnavailable,
			"temporarily_unavailable", msgUnrevoked)
		return
	}
	response.WriteHeader(http.StatusOK)
}

// connectionOf returns the connection token belongs to, or the zero
// Connection when there is none to end. A refresh token already
// rotated still names its connection: whoever holds it held the
// connection, and ending one only takes access away.
func (server *Server) connectionOf(
	ctx context.Context, token string,
) (Connection, error) {
	if strings.HasPrefix(token, refreshPrefix) {
		sum := sha256.Sum256([]byte(token))
		connection, _, err := server.cfg.Store.RefreshConnection(ctx, sum[:],
			server.cfg.Now())
		if errors.Is(err, ErrNotFound) {
			return Connection{}, nil
		}
		return connection, err
	}
	id, err := server.cfg.Tokens.ConnectionOf(ctx, token)
	if errors.Is(err, sql.ErrNoRows) || err == nil && id == "" {
		return Connection{}, nil
	}
	if err != nil {
		return Connection{}, err
	}
	found, err := server.cfg.Store.Connection(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return Connection{}, nil
	}
	return found, err
}
