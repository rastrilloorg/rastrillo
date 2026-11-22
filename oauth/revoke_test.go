package oauth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	rastrillo "amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/apitoken"
	"amadan.net/rastrillo/rastrillo/oauth"
)

func (tf *tokenFlow) revoke(token, hint string) *httptest.ResponseRecorder {
	form := url.Values{"token": {token}, "client_id": {"reg"}}
	if hint != "" {
		form.Set("token_type_hint", hint)
	}
	return tf.post("/oauth/revoke", form)
}

func (tf *tokenFlow) revoked(recorder *httptest.ResponseRecorder) {
	tf.t.Helper()
	uncached(tf.t, recorder)
	if recorder.Code != http.StatusOK {
		tf.t.Fatalf("revoke: status %d, body %s", recorder.Code, recorder.Body)
	}
}

func TestRevokeAccessEndsConnection(t *testing.T) {
	tf := newTokenFlow(t)
	tokens := tf.connect("write")
	tf.revoked(tf.revoke(tokens.AccessToken, "refresh_token"))
	tf.dead(tokens.AccessToken)
	tf.refusedWith(tf.token(refreshForm(tokens.RefreshToken)),
		http.StatusBadRequest, "invalid_grant")
	tf.ended()
	// Its connection has ended already; that is still a 200.
	tf.revoked(tf.revoke(tokens.AccessToken, ""))
}

func TestRevokeRefreshEndsConnection(t *testing.T) {
	tf := newTokenFlow(t)
	first := tf.connect("write")
	rotated := tf.issued(tf.token(refreshForm(first.RefreshToken)))
	tf.revoked(tf.revoke(rotated.RefreshToken, "access_token"))
	tf.dead(first.AccessToken)
	tf.dead(rotated.AccessToken)
	tf.refusedWith(tf.token(refreshForm(rotated.RefreshToken)),
		http.StatusBadRequest, "invalid_grant")
	tf.ended()

	// A token already rotated still names its connection.
	tf = newTokenFlow(t)
	first = tf.connect("write")
	rotated = tf.issued(tf.token(refreshForm(first.RefreshToken)))
	tf.revoked(tf.revoke(first.RefreshToken, ""))
	tf.dead(rotated.AccessToken)
	tf.ended()
}

func TestRevokeUnknownIs200(t *testing.T) {
	tf := newTokenFlow(t)
	tokens := tf.connect("write")
	personal, _, err := tf.tokens.Mint(context.Background(), "ana",
		apitoken.MintInput{Agent: "Claude", Access: rastrillo.Write})
	if err != nil {
		t.Fatal(err)
	}
	wrongSecret := tokens.AccessToken[:len(tokens.AccessToken)-1] + "0"
	if wrongSecret == tokens.AccessToken {
		wrongSecret = tokens.AccessToken[:len(tokens.AccessToken)-1] + "1"
	}
	for _, token := range []string{
		"nonsense", "rt-" + strings.Repeat("A", 43), "rt-",
		"a_AAAAAAAAAAAA_" + strings.Repeat("A", 43),
		wrongSecret, personal,
	} {
		tf.revoked(tf.revoke(token, ""))
	}
	tf.works(tokens.AccessToken)
	tf.works(personal)
	if len(tf.connections()) != 1 {
		t.Fatal("a revoke of another token ended the connection")
	}
}

// Only the client a token was issued to may revoke it (RFC 7009 §2.1).
// A token the server doesn't know is still a 200, whoever asks.
func TestRevokeChecksClient(t *testing.T) {
	tf := newTokenFlow(t)
	tokens := tf.connect("write")
	for _, token := range []string{tokens.AccessToken, tokens.RefreshToken} {
		form := url.Values{"token": {token}}
		tf.refusedWith(tf.post("/oauth/revoke", form),
			http.StatusUnauthorized, "invalid_client")
		form.Set("client_id", "other")
		tf.refusedWith(tf.post("/oauth/revoke", form),
			http.StatusBadRequest, "invalid_grant")
	}
	tf.works(tokens.AccessToken)
	if len(tf.connections()) != 1 {
		t.Fatal("another client's revoke ended the connection")
	}
	unknown := url.Values{"token": {"nonsense"}, "client_id": {"other"}}
	tf.revoked(tf.post("/oauth/revoke", unknown))
}

func TestRevokeRefusesBadRequests(t *testing.T) {
	tf := newTokenFlow(t)
	tf.refusedWith(tf.post("/oauth/revoke", url.Values{}),
		http.StatusBadRequest, "invalid_request")
	request := httptest.NewRequest(http.MethodPost, "/oauth/revoke",
		strings.NewReader(`{"token":"x"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	tf.mux.ServeHTTP(recorder, request)
	tf.refusedWith(recorder, http.StatusBadRequest, "invalid_request")
}

// downStore fails the reads and writes revocation makes while down.
type downStore struct {
	oauth.Store
	down bool
}

var errDown = errors.New("disk on fire")

func (store *downStore) RefreshConnection(
	ctx context.Context, hash []byte, now time.Time,
) (oauth.Connection, bool, error) {
	if store.down {
		return oauth.Connection{}, false, errDown
	}
	return store.Store.RefreshConnection(ctx, hash, now)
}

func (store *downStore) EndConnection(
	ctx context.Context, id string, at time.Time,
) error {
	if store.down {
		return errDown
	}
	return store.Store.EndConnection(ctx, id, at)
}

func TestRevokeStorageFailureIs503(t *testing.T) {
	down := &downStore{}
	tf := newTokenFlow(t, func(config *oauth.Config) {
		down.Store, config.Store = config.Store, down
	})
	unavailable := func(recorder *httptest.ResponseRecorder) {
		t.Helper()
		uncached(t, recorder)
		if recorder.Code != http.StatusServiceUnavailable ||
			recorder.Header().Get("Retry-After") != "5" {
			t.Fatalf("status %d, Retry-After %q",
				recorder.Code, recorder.Header().Get("Retry-After"))
		}
	}
	tokens := tf.connect("write")
	down.down = true
	unavailable(tf.revoke(tokens.RefreshToken, ""))
	unavailable(tf.revoke(tokens.AccessToken, ""))
	down.down = false
	tf.works(tokens.AccessToken)

	if err := tf.tokenDB.Close(); err != nil {
		t.Fatal(err)
	}
	unavailable(tf.revoke(tokens.AccessToken, ""))
	if len(tf.connections()) != 1 {
		t.Fatal("a failed revoke ended the connection")
	}
	logged := strings.Count(tf.logs.String(), "oauth: revoking a token")
	if logged != 3 {
		t.Errorf("logged %d failed revokes, want 3:\n%s", logged, tf.logs)
	}
}
