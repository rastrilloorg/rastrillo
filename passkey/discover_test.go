package passkey_test

import (
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"amadan.net/rastrillo/rastrillo/passkey"
	"amadan.net/rastrillo/rastrillo/webauthn/authtest"
)

// discover runs the discover ceremony with a — no session, no
// half-session — and returns the finish response.
func discover(t *testing.T, e env, a *authtest.Authenticator, opts authtest.Options) *httptest.ResponseRecorder {
	t.Helper()
	challenge := challengeFrom(t, postJSON(t, e.h.DiscoverBegin, nil, nil))
	if opts.RPID == "" {
		opts.RPID, opts.Origin = testRPID, testOrigin
	}
	clientData, authData, sig, err := a.Get(challenge, opts)
	if err != nil {
		t.Fatal(err)
	}
	return postJSON(t, e.h.DiscoverFinish, nil, map[string]string{
		"id": b64(a.CredID), "clientDataJSON": b64(clientData), "authenticatorData": b64(authData), "signature": b64(sig),
	})
}

func sessionCookie(w *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == "rastrillo_session" && c.Value != "" {
			return c
		}
	}
	return nil
}

func TestDiscoverWithUserVerificationSignsInAlone(t *testing.T) {
	e := newEnv(t)
	a, _ := authtest.New()
	enroll(t, e, e.signIn(t, "alice"), a)

	w := discover(t, e, a, authtest.Options{})
	if w.Code != http.StatusOK {
		t.Fatalf("discover finish: %d %s", w.Code, w.Body.String())
	}
	c := sessionCookie(w)
	if c == nil {
		t.Fatal("no session was minted")
	}
	r := httptest.NewRequest("GET", testOrigin+"/", nil)
	r.AddCookie(c)
	sess, ok := e.sess.From(r)
	if !ok || sess.Subject != "alice" || sess.Method != passkey.Method {
		t.Fatalf("session = %+v, %v; want alice by %q", sess, ok, passkey.Method)
	}
}

func TestDiscoverWithoutVerificationIsHeldWhenAnotherFactorExists(t *testing.T) {
	e := newEnvWith(t, func(c *passkey.Config) {
		c.OtherFactor = func(subject string) (bool, error) { return true, nil }
	})
	a, _ := authtest.New()
	enroll(t, e, e.signIn(t, "alice"), a)

	w := discover(t, e, a, authtest.Options{Flags: authtest.FlagUserPresent})
	if w.Code != http.StatusOK {
		t.Fatalf("discover finish: %d %s", w.Code, w.Body.String())
	}
	if sessionCookie(w) != nil {
		t.Fatal("an unverified assertion minted a full session")
	}
	var pending *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "rastrillo_secondfactor" && c.Value != "" {
			pending = c
		}
	}
	if pending == nil {
		t.Fatal("no half-session was held")
	}
	r := httptest.NewRequest("GET", testOrigin+"/", nil)
	r.AddCookie(pending)
	p, ok := e.g.Pending(r)
	if !ok || p.Method != passkey.MethodUnverified {
		t.Fatalf("pending = %+v, %v; want a %q half-session", p, ok, passkey.MethodUnverified)
	}
	if body := w.Body.String(); !contains(body, `"to":"/signin/confirm"`) {
		t.Fatalf("the answer does not send the page to the confirm step: %s", body)
	}
}

func TestDiscoverWithoutVerificationAndNothingElseSignsInAtTheWeakerTier(t *testing.T) {
	e := newEnv(t)
	a, _ := authtest.New()
	enroll(t, e, e.signIn(t, "alice"), a)
	w := discover(t, e, a, authtest.Options{Flags: authtest.FlagUserPresent})
	c := sessionCookie(w)
	if w.Code != http.StatusOK || c == nil {
		t.Fatalf("discover finish: %d %s", w.Code, w.Body.String())
	}
	r := httptest.NewRequest("GET", testOrigin+"/", nil)
	r.AddCookie(c)
	if sess, _ := e.sess.From(r); sess.Method != passkey.MethodUnverified {
		t.Fatalf("method = %q, want %q", sess.Method, passkey.MethodUnverified)
	}
}

func TestDiscoverHonoursAuthorize(t *testing.T) {
	e := newEnvWith(t, func(c *passkey.Config) {
		c.Authorize = func(subject string) bool { return subject != "alice" }
	})
	a, _ := authtest.New()
	enroll(t, e, e.signIn(t, "alice"), a)
	w := discover(t, e, a, authtest.Options{})
	if w.Code != http.StatusForbidden || sessionCookie(w) != nil {
		t.Fatalf("an unadmitted subject got %d", w.Code)
	}
}

func TestDiscoverRefusesAnUnknownCredentialAndAReplay(t *testing.T) {
	e := newEnv(t)
	a, _ := authtest.New()
	enroll(t, e, e.signIn(t, "alice"), a)
	stranger, _ := authtest.New()
	if w := discover(t, e, stranger, authtest.Options{}); w.Code != http.StatusBadRequest {
		t.Fatalf("an unknown credential got %d", w.Code)
	}
	challenge := challengeFrom(t, postJSON(t, e.h.DiscoverBegin, nil, nil))
	clientData, authData, sig, _ := a.Get(challenge, authtest.Options{RPID: testRPID, Origin: testOrigin})
	body := map[string]string{"id": b64(a.CredID), "clientDataJSON": b64(clientData), "authenticatorData": b64(authData), "signature": b64(sig)}
	if w := postJSON(t, e.h.DiscoverFinish, nil, body); w.Code != http.StatusOK {
		t.Fatalf("first finish: %d", w.Code)
	}
	if w := postJSON(t, e.h.DiscoverFinish, nil, body); w.Code != http.StatusBadRequest {
		t.Fatalf("a replayed assertion got %d", w.Code)
	}
}

func TestListRenameRemove(t *testing.T) {
	e := newEnv(t)
	cookie := e.signIn(t, "alice")
	a, _ := authtest.New()
	cookie = enroll(t, e, cookie, a)
	b, _ := authtest.New()
	enrollLabelled(t, e, cookie, b, "Work laptop")

	list, err := e.h.List("alice")
	if err != nil || len(list) != 2 {
		t.Fatalf("List = %d, %v; want 2", len(list), err)
	}
	if list[1].Label != "Work laptop" || !list[1].UserVerified || list[1].CreatedAt.IsZero() || len(list[1].AAGUID) != 32 {
		t.Fatalf("second credential recorded as %+v", list[1])
	}
	if list[0].LastUsedAt.IsZero() == false {
		t.Fatal("a never-asserted credential has a last-used time")
	}
	if err := e.h.Rename("alice", list[0].ID, "Phone"); err != nil {
		t.Fatal(err)
	}
	if err := e.h.Rename("bob", list[0].ID, "Mine now"); !errors.Is(err, passkey.ErrNotYours) {
		t.Fatalf("Rename by another subject = %v, want ErrNotYours", err)
	}
	// Assert with a, and its last-used time appears.
	if w := discover(t, e, a, authtest.Options{}); w.Code != http.StatusOK {
		t.Fatalf("discover: %d", w.Code)
	}
	list, _ = e.h.List("alice")
	if list[0].Label != "Phone" || list[0].LastUsedAt.IsZero() {
		t.Fatalf("after rename and use: %+v", list[0])
	}
	if err := e.h.Remove("alice", hex.EncodeToString(a.CredID)); err != nil {
		t.Fatal(err)
	}
	if w := discover(t, e, a, authtest.Options{}); w.Code != http.StatusBadRequest {
		t.Fatalf("a removed credential still asserts: %d", w.Code)
	}
	if err := e.h.Remove("alice", hex.EncodeToString(a.CredID)); !errors.Is(err, passkey.ErrNotYours) {
		t.Fatalf("removing twice = %v, want ErrNotYours", err)
	}
	if ok, _ := e.h.Enrolled("alice"); !ok {
		t.Fatal("b should still be enrolled")
	}
}

func TestACredentialBelongsToWhoeverRegisteredItFirst(t *testing.T) {
	e := newEnv(t)
	a, _ := authtest.New()
	enroll(t, e, e.signIn(t, "alice"), a)
	bob := e.signIn(t, "bob")
	challenge := challengeFrom(t, postJSON(t, e.h.RegisterBegin, bob, nil))
	clientData, attestation := a.Create(challenge, authtest.Options{RPID: testRPID, Origin: testOrigin})
	w := postJSON(t, e.h.RegisterFinish, bob, map[string]string{"clientDataJSON": b64(clientData), "attestationObject": b64(attestation)})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bob registering alice's credential got %d", w.Code)
	}
	if list, _ := e.h.List("alice"); len(list) != 1 {
		t.Fatal("alice's credential was disturbed")
	}
}

func TestRegistrationWithoutVerificationIsRecordedAsSuch(t *testing.T) {
	e := newEnv(t)
	cookie := e.signIn(t, "alice")
	a, _ := authtest.New()
	challenge := challengeFrom(t, postJSON(t, e.h.RegisterBegin, cookie, nil))
	clientData, attestation := a.Create(challenge, authtest.Options{RPID: testRPID, Origin: testOrigin, Flags: authtest.FlagUserPresent | authtest.FlagAttestedData})
	w := postJSON(t, e.h.RegisterFinish, cookie, map[string]string{"clientDataJSON": b64(clientData), "attestationObject": b64(attestation)})
	if w.Code != http.StatusOK {
		t.Fatalf("register: %d %s", w.Code, w.Body.String())
	}
	list, _ := e.h.List("alice")
	if len(list) != 1 || list[0].UserVerified {
		t.Fatalf("recorded as %+v, want an unverified credential", list)
	}
}

func enrollLabelled(t *testing.T, e env, cookie *http.Cookie, a *authtest.Authenticator, label string) *http.Cookie {
	t.Helper()
	challenge := challengeFrom(t, postJSON(t, e.h.RegisterBegin, cookie, nil))
	clientData, attestation := a.Create(challenge, authtest.Options{RPID: testRPID, Origin: testOrigin})
	w := postJSON(t, e.h.RegisterFinish, cookie, map[string]string{"clientDataJSON": b64(clientData), "attestationObject": b64(attestation), "label": label})
	if w.Code != http.StatusOK {
		t.Fatalf("RegisterFinish: %d %s", w.Code, w.Body.String())
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "rastrillo_session" && c.Value != "" {
			return c
		}
	}
	return cookie
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
