package oauth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"time"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/apitoken"
)

const (
	accessLife  = time.Hour
	refreshLife = 30 * 24 * time.Hour
	maxForm     = 64 << 10
	// refreshPrefix starts every refresh token. No apitoken prefix can
	// hold a hyphen, so the two kinds tell apart without a lookup.
	refreshPrefix = "rt-"
)

// What the token and revocation endpoints tell a client that is refused.
const (
	msgBadBody = "The request body must be form-encoded " +
		"(application/x-www-form-urlencoded) and 64 KiB or less."
	msgRepeated  = "Each parameter may be sent only once."
	msgGrantType = "grant_type must be authorization_code or " +
		"refresh_token."
	msgBadCode = "The authorization code is invalid, expired, or its " +
		"connection has ended."
	msgCodeReused = "This authorization code was already used, so its " +
		"connection has ended. Connect again."
	msgBadRefresh = "The refresh token is invalid or has expired, or its " +
		"connection has ended."
	msgReused = "This refresh token was already used, so its connection " +
		"has ended. Connect again."
	msgNoClient    = "client_id is required."
	msgOtherClient = "client_id does not match the client this " +
		"authorization code or refresh_token was issued to."
	msgNotIssuedTo = "client_id does not match the client this token " +
		"was issued to."
	msgRedirect = "redirect_uri does not match the authorization request."
	msgVerifier = "code_verifier does not match the code_challenge."
	msgResource = "resource does not match the one the person approved."
	msgScope    = "scope may contain read and write, and cannot exceed " +
		"the access approved for this connection."
	msgDisallowed = "The person who approved this connection can no " +
		"longer connect apps."
	msgServer    = "Something went wrong on the server. Try again."
	msgUnrevoked = "The token could not be revoked right now. " +
		"Try again in 5 seconds."
)

// problem is an RFC 6749 §5.2 error answer.
type problem struct {
	status            int
	code, description string
}

func (refusal *problem) write(response http.ResponseWriter) {
	oauthError(response, refusal.status, refusal.code, refusal.description)
}

func refuse(code, description string) *problem {
	return &problem{http.StatusBadRequest, code, description}
}

func missing(name string) *problem {
	return refuse("invalid_request", name+" is required.")
}

type tokenAnswer struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// token answers POST /oauth/token: a code, or a refresh token, for an
// access token and a new refresh token.
func (server *Server) token(
	response http.ResponseWriter, request *http.Request,
) {
	var tokens tokenAnswer
	form, fail := readForm(response, request)
	if fail == nil {
		switch form.Get("grant_type") {
		case "":
			fail = missing("grant_type")
		case "authorization_code":
			tokens, fail = server.exchange(request.Context(), form)
		case "refresh_token":
			tokens, fail = server.refresh(request.Context(), form)
		default:
			fail = refuse("unsupported_grant_type", msgGrantType)
		}
	}
	if fail != nil {
		fail.write(response)
		return
	}
	writeJSON(response, http.StatusOK, tokens)
}

// readForm reads the form body, the only kind these endpoints take;
// parameters in the query are ignored. A parameter sent twice is
// refused (RFC 6749 §3.2).
func readForm(
	response http.ResponseWriter, request *http.Request,
) (url.Values, *problem) {
	kind, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || kind != "application/x-www-form-urlencoded" {
		return nil, refuse("invalid_request", msgBadBody)
	}
	request.Body = http.MaxBytesReader(response, request.Body, maxForm)
	if err := request.ParseForm(); err != nil {
		return nil, refuse("invalid_request", msgBadBody)
	}
	for _, values := range request.PostForm {
		if len(values) > 1 {
			return nil, refuse("invalid_request", msgRepeated)
		}
	}
	return request.PostForm, nil
}

// exchange trades a code for tokens.
func (server *Server) exchange(
	ctx context.Context, form url.Values,
) (tokenAnswer, *problem) {
	raw := form.Get("code")
	if raw == "" {
		return tokenAnswer{}, missing("code")
	}
	sum := sha256.Sum256([]byte(raw))
	code, fail := server.claim(ctx, sum[:], form)
	if fail != nil {
		return tokenAnswer{}, fail
	}
	return server.firstTokens(ctx, code.Connection)
}

// claim checks the request against the code before it claims the code,
// so a request that fails the checks leaves the code to whoever holds
// its verifier: a stolen code alone can't end the connection (OAuth 2.1
// §7.5.3). A code claimed already, by a request that passes them, is a
// replay, and ends its connection.
func (server *Server) claim(
	ctx context.Context, hash []byte, form url.Values,
) (Code, *problem) {
	now := server.cfg.Now()
	code, err := server.cfg.Store.Code(ctx, hash, now)
	if err == nil {
		if fail := codeProblem(code, form); fail != nil {
			return Code{}, fail
		}
		var used bool
		_, used, err = server.cfg.Store.TakeCode(ctx, hash, now)
		if err == nil && used {
			return Code{}, server.reused(ctx, code.Connection, msgCodeReused)
		}
	}
	if errors.Is(err, ErrNotFound) {
		return Code{}, refuse("invalid_grant", msgBadCode)
	}
	if err != nil {
		return Code{}, server.serverError("claiming a code", err)
	}
	return code, nil
}

// firstTokens issues the tokens for a claimed code. The code is spent,
// so a connection that fails here could never get a token: it is ended
// rather than left on the person's list.
func (server *Server) firstTokens(
	ctx context.Context, id string,
) (tokenAnswer, *problem) {
	connection, err := server.cfg.Store.Connection(ctx, id)
	if err == nil && connection.Ended != nil {
		return tokenAnswer{}, refuse("invalid_grant", msgBadCode)
	}
	var tokens tokenAnswer
	if err == nil {
		tokens, err = server.newTokens(ctx, connection)
	}
	if err != nil {
		if err := server.end(ctx, id); err != nil {
			server.cfg.Logger.Error("oauth: ending a connection", "err", err)
		}
		return tokenAnswer{}, server.serverError("issuing tokens for a code",
			err)
	}
	return tokens, nil
}

// newTokens mints a new connection's access token and its first refresh
// token.
func (server *Server) newTokens(
	ctx context.Context, connection Connection,
) (tokenAnswer, error) {
	token, _, err := server.mint(ctx, connection, connection.Access)
	if err != nil {
		return tokenAnswer{}, fmt.Errorf("minting a token: %w", err)
	}
	refresh, next, err := newRefresh(connection.ID, server.cfg.Now())
	if err == nil {
		err = server.cfg.Store.SaveRefresh(ctx, next)
	}
	if err != nil {
		return tokenAnswer{}, fmt.Errorf("saving a refresh token: %w", err)
	}
	return issue(token, refresh, connection.Access), nil
}

// codeProblem checks the request matches what the code was issued for.
// redirect_uri is optional, as OAuth 2.1 has it, but checked if sent.
func codeProblem(code Code, form url.Values) *problem {
	switch {
	case form.Get("client_id") == "":
		return &problem{http.StatusUnauthorized, "invalid_client", msgNoClient}
	case form.Get("client_id") != code.ClientID:
		return refuse("invalid_grant", msgOtherClient)
	case form.Has("redirect_uri") && form.Get("redirect_uri") != code.Redirect:
		return refuse("invalid_grant", msgRedirect)
	case form.Get("resource") != code.Resource:
		return refuse("invalid_target", msgResource)
	case !verified(form.Get("code_verifier"), code.Challenge):
		return refuse("invalid_grant", msgVerifier)
	}
	return nil
}

// verified checks the verifier is well formed (RFC 7636 §4.1) before it
// checks its hash, since a challenge can be the hash of an empty or
// malformed verifier.
func verified(verifier, challenge string) bool {
	if !validChallenge(verifier) {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return subtle.ConstantTimeCompare(
		[]byte(base64.RawURLEncoding.EncodeToString(sum[:])),
		[]byte(challenge)) == 1
}

// reused ends a connection whose code or refresh token came back after
// it was spent: someone else may hold a copy, as OAuth 2.1 warns.
func (server *Server) reused(
	ctx context.Context, connection, description string,
) *problem {
	if err := server.end(ctx, connection); err != nil {
		return server.serverError("ending a reused connection", err)
	}
	return refuse("invalid_grant", description)
}

func (server *Server) serverError(doing string, err error) *problem {
	server.cfg.Logger.Error("oauth: "+doing, "err", err)
	return &problem{http.StatusInternalServerError, "server_error",
		msgServer}
}

// mint makes an access token for connection, named after the client as the
// person saw it, when that name is one apitoken can carry.
func (server *Server) mint(
	ctx context.Context, connection Connection, access rastrillo.Access,
) (string, apitoken.Token, error) {
	agent, ok := apitoken.ValidAgentName(connection.ClientName)
	if !ok {
		agent = "Agent"
	}
	return server.cfg.Tokens.Mint(ctx, connection.Subject, apitoken.MintInput{
		Agent: agent, Access: access, TTL: accessLife,
		Connection: connection.ID,
	})
}

func issue(access, refresh string, granted rastrillo.Access) tokenAnswer {
	scope := "read"
	if granted == rastrillo.Write {
		scope = "read write"
	}
	return tokenAnswer{
		AccessToken: access, TokenType: "Bearer",
		ExpiresIn: int(accessLife / time.Second), RefreshToken: refresh,
		Scope: scope,
	}
}
