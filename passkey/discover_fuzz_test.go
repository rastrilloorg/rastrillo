package passkey_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"amadan.net/rastrillo/rastrillo/passkey"
	"amadan.net/rastrillo/rastrillo/webauthn/authtest"
)

// fuzzChallenge and the authenticator's key are fixed, and each run
// re-stores both, so a ceremony saved in the corpus still signs in.
var fuzzChallenge = bytes.Repeat([]byte{7}, 32)

type discoverBody struct {
	ID         string `json:"id"`
	ClientData string `json:"clientDataJSON"`
	AuthData   string `json:"authenticatorData"`
	Signature  string `json:"signature"`
}

// FuzzDiscoverFinish posts arbitrary bodies to the one ceremony
// endpoint a stranger reaches: discover signs in whoever the
// credential names. Only the client and authenticator data the
// enrolled key signed may sign anyone in, and no body may make the
// handler panic or fail on the server's side.
func FuzzDiscoverFinish(f *testing.F) {
	env := newEnvWith(f, func(config *passkey.Config) {
		config.Logger = slog.New(slog.DiscardHandler)
	})
	authenticator := fuzzAuthenticator(f)
	clientData, authData, signature, err := authenticator.Get(
		fuzzChallenge, authtest.Options{RPID: testRPID, Origin: testOrigin})
	if err != nil {
		f.Fatal(err)
	}
	valid := discoverBody{b64(authenticator.CredID), b64(clientData),
		b64(authData), b64(signature)}
	for _, body := range []discoverBody{
		valid,
		{valid.ID, valid.ClientData, valid.AuthData, ""},
		{"", valid.ClientData, valid.AuthData, valid.Signature},
		{valid.ID, valid.ClientData + "A", valid.AuthData, valid.Signature},
	} {
		encoded, _ := json.Marshal(body)
		f.Add(encoded)
	}
	// Encodings the handler reads as the signed ceremony: a newline
	// base64 skips, a second JSON value the decoder never reads, and a
	// member name in another case.
	newline := valid
	newline.AuthData = valid.AuthData[:8] + "\n" + valid.AuthData[8:]
	encoded, _ := json.Marshal(newline)
	f.Add(encoded)
	encoded, _ = json.Marshal(valid)
	f.Add(append(encoded, []byte(`{"authenticatorData":"AAAA"}`)...))
	f.Add(bytes.Replace(encoded, []byte(`"authenticatorData"`),
		[]byte(`"AuthenticatorData"`), 1))
	f.Add([]byte(`{"clientDataJSON":null}`))
	f.Add([]byte(`[]`))
	// A valid body that stopped signing in would leave the target
	// fuzzing nothing but refusals.
	encoded, _ = json.Marshal(valid)
	if code := postDiscover(f, env, authenticator, encoded).Code; code != 200 {
		f.Fatalf("the signed ceremony answered %d", code)
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		response := postDiscover(t, env, authenticator, body)
		switch response.Code {
		case http.StatusBadRequest:
			return
		case http.StatusOK:
		default:
			t.Fatalf("%q answered %d: %s", body, response.Code,
				response.Body)
		}
		if !signedBytes(body, clientData, authData) {
			t.Fatalf("%q signed someone in; only %+v was signed",
				body, valid)
		}
	})
}

// signedBytes reads body as decodeCeremony does, one JSON value from
// a Decoder and base64 that skips newlines, and reports whether it
// carries exactly the client and authenticator data that were signed.
// Comparing the text instead would call an equivalent spelling a
// forgery.
func signedBytes(body, clientData, authData []byte) bool {
	var got discoverBody
	if json.NewDecoder(bytes.NewReader(body)).Decode(&got) != nil {
		return false
	}
	gotClient, err := base64.RawURLEncoding.DecodeString(got.ClientData)
	if err != nil {
		return false
	}
	gotAuth, err := base64.RawURLEncoding.DecodeString(got.AuthData)
	return err == nil && bytes.Equal(gotClient, clientData) &&
		bytes.Equal(gotAuth, authData)
}

func postDiscover(
	t testing.TB,
	env env,
	authenticator *authtest.Authenticator,
	body []byte,
) *httptest.ResponseRecorder {
	enrolFuzzCredential(t, env.db.Exec, authenticator)
	request := httptest.NewRequest("POST", testOrigin+"/passkey",
		bytes.NewReader(body))
	response := httptest.NewRecorder()
	env.h.DiscoverFinish(response, request)
	return response
}

func fuzzAuthenticator(f *testing.F) *authtest.Authenticator {
	f.Helper()
	key, err := ecdsa.ParseRawPrivateKey(elliptic.P256(),
		bytes.Repeat([]byte{9}, 32))
	if err != nil {
		f.Fatal(err)
	}
	return &authtest.Authenticator{Key: key, CredID: []byte("fuzz")}
}

// enrolFuzzCredential stores the challenge afresh and winds the
// counter back, because a run that signed in spent the one and moved
// the other, and the next run must be judged on its body alone.
func enrolFuzzCredential(
	t testing.TB,
	exec func(string, ...any) (sql.Result, error),
	authenticator *authtest.Authenticator,
) {
	t.Helper()
	point := append([]byte{4}, append(
		authenticator.Key.X.FillBytes(make([]byte, 32)),
		authenticator.Key.Y.FillBytes(make([]byte, 32))...)...)
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT OR REPLACE INTO passkey_challenges
			(challenge, subject, purpose, expires_at)
			VALUES (?, '', 'discover', '2999-01-01T00:00:00Z')`,
			[]any{hex.EncodeToString(fuzzChallenge)}},
		{`INSERT OR REPLACE INTO passkey_credentials
			(id, subject, public_key, sign_count, created_at)
			VALUES (?, 'fuzz-subject', ?, 0, '2026-01-01T00:00:00Z')`,
			[]any{hex.EncodeToString(authenticator.CredID), point}},
	} {
		if _, err := exec(statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
}
