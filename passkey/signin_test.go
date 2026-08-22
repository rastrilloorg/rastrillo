// signin_test.go proves the sign-in-time gate: the pending
// half-session between factors, its single use, its expiry, and the
// purpose wall between step-up and sign-in ceremonies.
package passkey_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/carlosframework/rastrillo/passkey"
	"github.com/carlosframework/rastrillo/sessions"
	"github.com/carlosframework/rastrillo/webauthn/authtest"
)

// gate invokes Gate as an identity plugin would: at the moment sess
// would have been minted, with the sign-in request (whose form may
// carry return_to).
func gate(t *testing.T, e env, sess sessions.Session, returnTo string) (*httptest.ResponseRecorder, *http.Cookie) {
	t.Helper()
	form := url.Values{}
	if returnTo != "" {
		form.Set("return_to", returnTo)
	}
	r := httptest.NewRequest("POST", testOrigin+"/signin", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	done, err := e.h.Gate(w, r, sess)
	if err != nil {
		t.Fatalf("Gate: %v", err)
	}
	if !done {
		return w, nil
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "rastrillo_passkey_pending" && c.Value != "" {
			return w, c
		}
	}
	t.Fatal("Gate reported done but set no pending cookie")
	return nil, nil
}

// signInAssert runs the sign-in assertion ceremony on a pending
// cookie and returns the finish response.
func signInAssert(t *testing.T, e env, pending *http.Cookie, a *authtest.Authenticator) *httptest.ResponseRecorder {
	t.Helper()
	challenge := challengeFrom(t, postJSON(t, e.h.SignInBegin, pending, nil))
	clientData, authData, sig, err := a.Get(challenge, authtest.Options{RPID: testRPID, Origin: testOrigin})
	if err != nil {
		t.Fatal(err)
	}
	return postJSON(t, e.h.SignInFinish, pending, map[string]string{
		"id":                b64(a.CredID),
		"clientDataJSON":    b64(clientData),
		"authenticatorData": b64(authData),
		"signature":         b64(sig),
	})
}

func TestGatePassesThroughWhenNotEnrolled(t *testing.T) {
	e := newEnv(t)
	w, _ := gate(t, e, sessions.Session{Subject: "42", Method: "password"}, "")
	if w.Code == http.StatusSeeOther {
		t.Fatalf("Gate redirected for an unenrolled subject; the plugin should have signed in as usual")
	}
}

func TestGateThenSignInMintsBothFactorSession(t *testing.T) {
	e := newEnv(t)

	// Enroll while signed in (the step-up seam's own rule), then the
	// session goes away — the next sign-in starts from nothing.
	cookie := e.signIn(t, "person@example.com")
	a, err := authtest.New()
	if err != nil {
		t.Fatal(err)
	}
	enroll(t, e, cookie, a)

	// First factor verifies; the plugin offers the would-be session to
	// the Gate, which trades it for a pending half-session.
	w, pending := gate(t, e, sessions.Session{Subject: "person@example.com", Method: "magiclink", AuthTime: time.Now()}, "/notes/7")
	if pending == nil {
		t.Fatal("Gate did not take over for an enrolled subject")
	}
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/passkey/confirm" {
		t.Fatalf("Gate: %d -> %q, want 303 -> /passkey/confirm", w.Code, w.Header().Get("Location"))
	}
	// The half-session minted no real session.
	if len(w.Result().Cookies()) != 1 {
		t.Fatalf("Gate set %d cookies, want only the pending one", len(w.Result().Cookies()))
	}

	// The assertion completes the sign-in.
	fin := signInAssert(t, e, pending, a)
	if fin.Code != http.StatusOK {
		t.Fatalf("SignInFinish: %d, body %s", fin.Code, fin.Body.String())
	}
	if !strings.Contains(fin.Body.String(), `"to":"/notes/7"`) {
		t.Fatalf("finish did not return the stored return_to; body %s", fin.Body.String())
	}

	var minted, cleared *http.Cookie
	for _, c := range fin.Result().Cookies() {
		switch c.Name {
		case e.sess.CookieName():
			minted = c
		case "rastrillo_passkey_pending":
			cleared = c
		}
	}
	if minted == nil || minted.Value == "" {
		t.Fatal("finish minted no session cookie")
	}
	if cleared == nil || cleared.MaxAge != -1 {
		t.Fatal("finish did not clear the pending cookie")
	}

	// The minted session names both factors.
	r := httptest.NewRequest("GET", testOrigin+"/", nil)
	r.AddCookie(minted)
	s, ok := e.sess.From(r)
	if !ok || s.Method != "magiclink+passkey" {
		t.Fatalf("minted session = %+v, %v; want Method magiclink+passkey", s, ok)
	}
	if s.AuthTime.IsZero() {
		t.Fatal("minted session has no AuthTime; RequireFresh would mis-age it")
	}

	// The half-session was single use: the same pending cookie can
	// neither begin nor finish again.
	if again := postJSON(t, e.h.SignInBegin, pending, nil); again.Code != http.StatusForbidden {
		t.Fatalf("SignInBegin after completion: %d, want 403", again.Code)
	}
}

func TestSignInWithoutPendingRefused(t *testing.T) {
	e := newEnv(t)
	if w := postJSON(t, e.h.SignInBegin, nil, nil); w.Code != http.StatusForbidden {
		t.Errorf("SignInBegin without pending: %d, want 403", w.Code)
	}
	if w := postJSON(t, e.h.SignInFinish, nil, map[string]string{}); w.Code != http.StatusForbidden {
		t.Errorf("SignInFinish without pending: %d, want 403", w.Code)
	}
}

func TestPendingHalfSessionExpires(t *testing.T) {
	e := newEnv(t)
	cookie := e.signIn(t, "42")
	a, err := authtest.New()
	if err != nil {
		t.Fatal(err)
	}
	enroll(t, e, cookie, a)

	_, pending := gate(t, e, sessions.Session{Subject: "42", Method: "password"}, "")
	if pending == nil {
		t.Fatal("Gate did not take over")
	}
	if _, err := e.db.Exec(`UPDATE passkey_pending SET expires_at = ?`,
		time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if w := postJSON(t, e.h.SignInBegin, pending, nil); w.Code != http.StatusForbidden {
		t.Fatalf("SignInBegin on expired pending: %d, want 403", w.Code)
	}
}

// TestStepUpChallengeCannotFinishSignIn pins the purpose wall: a
// challenge minted for step-up (an existing session upgrading its
// freshness) must never complete the sign-in ceremony (minting a
// session from a half-session), even for the same subject and the
// same authenticator.
func TestStepUpChallengeCannotFinishSignIn(t *testing.T) {
	e := newEnv(t)
	cookie := e.signIn(t, "42")
	a, err := authtest.New()
	if err != nil {
		t.Fatal(err)
	}
	enroll(t, e, cookie, a)

	_, pending := gate(t, e, sessions.Session{Subject: "42", Method: "password"}, "")
	if pending == nil {
		t.Fatal("Gate did not take over")
	}

	// Mint the challenge via the STEP-UP begin (purpose "stepup"),
	// assert over it, and try to finish the SIGN-IN with it.
	challenge := challengeFrom(t, postJSON(t, e.h.StepUpBegin, cookie, nil))
	clientData, authData, sig, err := a.Get(challenge, authtest.Options{RPID: testRPID, Origin: testOrigin})
	if err != nil {
		t.Fatal(err)
	}
	w := postJSON(t, e.h.SignInFinish, pending, map[string]string{
		"id":                b64(a.CredID),
		"clientDataJSON":    b64(clientData),
		"authenticatorData": b64(authData),
		"signature":         b64(sig),
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("cross-purpose finish: %d, want 400", w.Code)
	}
}

func TestSweepClearsExpiredPending(t *testing.T) {
	e := newEnv(t)
	stamp := func(d time.Duration) string { return time.Now().Add(d).UTC().Format(time.RFC3339) }
	for hash, exp := range map[string]string{"dead": stamp(-time.Minute), "live": stamp(time.Minute)} {
		if _, err := e.db.Exec(
			`INSERT INTO passkey_pending (token_hash, subject, expires_at) VALUES (?, 's', ?)`,
			hash, exp); err != nil {
			t.Fatal(err)
		}
	}
	if err := passkey.Sweep(e.db, time.Now()); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM passkey_pending`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("after Sweep: %d pending rows, want the 1 live one", n)
	}
}
