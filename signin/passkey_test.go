package signin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/auth"
	"amadan.net/rastrillo/rastrillo/sessions"
	"amadan.net/rastrillo/rastrillo/webauthn/authtest"
)

func encoded(raw []byte) string {
	return base64.RawURLEncoding.EncodeToString(raw)
}

// postPasskey drives one passkey endpoint with a JSON body.
func postPasskey(
	t *testing.T, handler http.HandlerFunc, cookie *http.Cookie, body any,
) *httptest.ResponseRecorder {
	t.Helper()
	var buffer bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buffer).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(http.MethodPost,
		testOrigin+"/passkey", &buffer)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler(response, request)
	return response
}

func challengeOf(t *testing.T, response *httptest.ResponseRecorder) []byte {
	t.Helper()
	var begun struct {
		Challenge string `json:"challenge"`
	}
	if response.Code != http.StatusOK ||
		json.Unmarshal(response.Body.Bytes(), &begun) != nil {
		t.Fatalf("begin: %d %s", response.Code, response.Body.String())
	}
	challenge, err := base64.RawURLEncoding.DecodeString(begun.Challenge)
	if err != nil {
		t.Fatal(err)
	}
	return challenge
}

// registerPasskey signs subject in and registers device as its passkey.
func (test *rig) registerPasskey(
	subject string, device *authtest.Authenticator,
) {
	test.t.Helper()
	store := test.signins.config.Sessions
	signedIn := httptest.NewRecorder()
	err := store.SignIn(signedIn, httptest.NewRequest(http.MethodPost,
		testOrigin+"/", nil),
		sessions.Session{Subject: subject, AuthTime: time.Now()})
	if err != nil {
		test.t.Fatal(err)
	}
	cookie := signedIn.Result().Cookies()[0]
	passkeys := test.signins.passkeys
	challenge := challengeOf(test.t,
		postPasskey(test.t, passkeys.RegisterBegin, cookie, nil))
	clientData, attestation := device.Create(challenge,
		authtest.Options{RPID: "app.test", Origin: testOrigin})
	finished := postPasskey(test.t, passkeys.RegisterFinish, cookie,
		map[string]string{"clientDataJSON": encoded(clientData),
			"attestationObject": encoded(attestation)})
	if finished.Code != http.StatusOK {
		test.t.Fatalf("RegisterFinish: %d %s", finished.Code,
			finished.Body.String())
	}
}

// discover is a passkey sign-in from the screen: discovery, no session.
func (test *rig) discover(
	device *authtest.Authenticator,
) *httptest.ResponseRecorder {
	test.t.Helper()
	passkeys := test.signins.passkeys
	challenge := challengeOf(test.t,
		postPasskey(test.t, passkeys.DiscoverBegin, nil, nil))
	clientData, authData, signature, err := device.Get(challenge,
		authtest.Options{RPID: "app.test", Origin: testOrigin})
	if err != nil {
		test.t.Fatal(err)
	}
	return postPasskey(test.t, passkeys.DiscoverFinish, nil,
		map[string]string{"id": encoded(device.CredID),
			"clientDataJSON":    encoded(clientData),
			"authenticatorData": encoded(authData),
			"signature":         encoded(signature)})
}

func (test *rig) hasSession(response *httptest.ResponseRecorder) bool {
	name := test.signins.config.Sessions.CookieName()
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == name && cookie.Value != "" {
			return true
		}
	}
	return false
}

func TestAPasskeyAdmitsOnlyWhomAuthWould(t *testing.T) {
	database := schema.Open(t)
	moment := &clock{now: time.Now()}
	authority := newAuth(t, database, func(config *auth.Config) {
		config.Authorize = func(address string) bool {
			return address != "eve@example.com"
		}
	})
	signins, err := New(baseConfig(t, database, authority, moment))
	if err != nil {
		t.Fatal(err)
	}
	test := &rig{t: t, database: database, auth: authority,
		signins: signins, clock: moment}
	adaDevice, _ := authtest.New()
	eveDevice, _ := authtest.New()
	test.registerPasskey(ada, adaDevice)
	test.registerPasskey("eve@example.com", eveDevice)
	if !test.hasSession(test.discover(adaDevice)) {
		t.Error("an authorized passkey got no session")
	}
	if test.hasSession(test.discover(eveDevice)) {
		t.Error("a passkey auth's Authorize refuses got a session")
	}
}

func TestNewNeedsAuthorizeWithLookupAndPasskeys(t *testing.T) {
	database := schema.Open(t)
	authority := newAuth(t, database, nil)
	config := baseConfig(t, database, authority, &clock{time.Now()})
	config.Lookup = func(
		_ context.Context, address string,
	) (string, bool, error) {
		return "user:" + address, true, nil
	}
	if _, err := New(config); err == nil ||
		!strings.Contains(err.Error(), "Authorize") {
		t.Fatalf("New with Lookup and no Authorize: %v, want a refusal", err)
	}
	config.Authorize = func(string) bool { return true }
	if _, err := New(config); err != nil {
		t.Fatalf("New with Lookup and Authorize: %v", err)
	}
	config.Authorize, config.Ways = nil, Authenticator|Passphrase
	if _, err := New(config); err != nil {
		t.Fatalf("New with Lookup and no passkeys: %v", err)
	}
}
