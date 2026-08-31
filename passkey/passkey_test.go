package passkey_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/passkey"
	"amadan.net/rastrillo/rastrillo/sessions"
	"amadan.net/rastrillo/rastrillo/webauthn/authtest"
)

const (
	testOrigin = "http://app.test"
	testRPID   = "app.test"
)

type env struct {
	h    *passkey.Handlers
	sess *sessions.Sessions
	db   *sql.DB
}

func newEnv(t *testing.T) env {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "p.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	full := migrate.Merge(sessions.Schema, passkey.Schema)
	if _, err := migrate.Apply(context.Background(), d, full); err != nil {
		t.Fatalf("migrate.Apply: %v", err)
	}
	sqlDB := d.Writer()
	sess, err := sessions.New(sessions.Config{DB: sqlDB, Origin: testOrigin})
	if err != nil {
		t.Fatal(err)
	}
	h, err := passkey.New(passkey.Config{Sessions: sess, DB: sqlDB, Origin: testOrigin})
	if err != nil {
		t.Fatal(err)
	}
	return env{h: h, sess: sess, db: sqlDB}
}

// signIn mints a session and returns its cookie.
func (e env) signIn(t *testing.T, subject string) *http.Cookie {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", testOrigin+"/signin", nil)
	if err := e.sess.SignIn(w, r, sessions.Session{Subject: subject, AuthTime: time.Now()}); err != nil {
		t.Fatal(err)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == e.sess.CookieName() {
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

// postJSON drives one handler with an optional session cookie.
func postJSON(t *testing.T, h http.HandlerFunc, cookie *http.Cookie, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	r := httptest.NewRequest("POST", testOrigin+"/passkey", &buf)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

// challengeFrom decodes a begin response's challenge.
func challengeFrom(t *testing.T, w *httptest.ResponseRecorder) []byte {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("begin: status %d, body %s", w.Code, w.Body.String())
	}
	var out struct {
		Challenge string `json:"challenge"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	b, err := base64.RawURLEncoding.DecodeString(out.Challenge)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// enroll runs the full registration ceremony for cookie's subject.
func enroll(t *testing.T, e env, cookie *http.Cookie, a *authtest.Authenticator) {
	t.Helper()
	challenge := challengeFrom(t, postJSON(t, e.h.RegisterBegin, cookie, nil))
	clientData, attestation := a.Create(challenge, authtest.Options{RPID: testRPID, Origin: testOrigin})
	w := postJSON(t, e.h.RegisterFinish, cookie, map[string]string{
		"clientDataJSON":    b64(clientData),
		"attestationObject": b64(attestation),
	})
	if w.Code != http.StatusOK {
		t.Fatalf("RegisterFinish: status %d, body %s", w.Code, w.Body.String())
	}
}

// assert runs the step-up assertion ceremony and returns the response.
func assert(t *testing.T, e env, cookie *http.Cookie, a *authtest.Authenticator, opts authtest.Options) *httptest.ResponseRecorder {
	t.Helper()
	challenge := challengeFrom(t, postJSON(t, e.h.StepUpBegin, cookie, nil))
	if opts.RPID == "" {
		opts.RPID = testRPID
	}
	if opts.Origin == "" {
		opts.Origin = testOrigin
	}
	clientData, authData, sig, err := a.Get(challenge, opts)
	if err != nil {
		t.Fatal(err)
	}
	return postJSON(t, e.h.StepUpFinish, cookie, map[string]string{
		"id":                b64(a.CredID),
		"clientDataJSON":    b64(clientData),
		"authenticatorData": b64(authData),
		"signature":         b64(sig),
	})
}

func TestEnrollThenStepUpSatisfiesRequireFresh(t *testing.T) {
	e := newEnv(t)
	cookie := e.signIn(t, "42")
	a, err := authtest.New()
	if err != nil {
		t.Fatal(err)
	}
	enroll(t, e, cookie, a)
	if ok, _ := e.h.Enrolled("42"); !ok {
		t.Fatal("Enrolled = false after registration")
	}

	// Make the session stale: RequireFresh must refuse it.
	if _, err := e.db.Exec(`UPDATE sessions SET auth_time = ?, created_at = ?`,
		time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
		time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	guard := e.sess.RequireFresh(5 * time.Minute)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	r := httptest.NewRequest("GET", testOrigin+"/settings", nil)
	r.AddCookie(cookie)
	rec := httptest.NewRecorder()
	guard.ServeHTTP(rec, r)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("stale session not refused: %d", rec.Code)
	}

	// Step up by passkey: the STALE session is enough to assert.
	w := assert(t, e, cookie, a, authtest.Options{})
	if w.Code != http.StatusOK {
		t.Fatalf("StepUpFinish: status %d, body %s", w.Code, w.Body.String())
	}
	var fresh *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == e.sess.CookieName() && c.Value != "" {
			fresh = c
		}
	}
	if fresh == nil {
		t.Fatal("step-up rotated no session cookie")
	}
	if fresh.Value == cookie.Value {
		t.Fatal("step-up did not rotate the token")
	}

	// The rotated session satisfies RequireFresh, and names its method.
	r = httptest.NewRequest("GET", testOrigin+"/settings", nil)
	r.AddCookie(fresh)
	rec = httptest.NewRecorder()
	var method string
	e.sess.RequireFresh(5*time.Minute)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		s, _ := sessions.Current(r)
		method = s.Method
	})).ServeHTTP(rec, r)
	if method != "passkey" {
		t.Fatalf("post-step-up session method = %q (status %d), want passkey", method, rec.Code)
	}

	// The old, pre-step-up cookie was revoked by the rotation.
	if _, ok := e.sess.From(func() *http.Request {
		r := httptest.NewRequest("GET", testOrigin+"/", nil)
		r.AddCookie(cookie)
		return r
	}()); ok {
		t.Fatal("pre-step-up cookie still admits after rotation")
	}
}

func TestNoSessionNoCeremony(t *testing.T) {
	e := newEnv(t)
	if w := postJSON(t, e.h.RegisterBegin, nil, nil); w.Code != http.StatusForbidden {
		t.Errorf("RegisterBegin without session: %d, want 403", w.Code)
	}
	if w := postJSON(t, e.h.StepUpBegin, nil, nil); w.Code != http.StatusForbidden {
		t.Errorf("StepUpBegin without session: %d, want 403", w.Code)
	}
}

func TestStepUpBeginWithoutEnrollment(t *testing.T) {
	e := newEnv(t)
	cookie := e.signIn(t, "42")
	if w := postJSON(t, e.h.StepUpBegin, cookie, nil); w.Code != http.StatusNotFound {
		t.Errorf("StepUpBegin unenrolled: %d, want 404", w.Code)
	}
}

func TestChallengeIsSingleUseAndSubjectBound(t *testing.T) {
	e := newEnv(t)
	alice := e.signIn(t, "alice")
	a, _ := authtest.New()
	enroll(t, e, alice, a)

	// Replay: run a full assertion, then replay the same body — the
	// challenge row was consumed, so the replay must fail.
	challenge := challengeFrom(t, postJSON(t, e.h.StepUpBegin, alice, nil))
	clientData, authData, sig, err := a.Get(challenge, authtest.Options{RPID: testRPID, Origin: testOrigin})
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]string{
		"id": b64(a.CredID), "clientDataJSON": b64(clientData),
		"authenticatorData": b64(authData), "signature": b64(sig),
	}
	if w := postJSON(t, e.h.StepUpFinish, alice, body); w.Code != http.StatusOK {
		t.Fatalf("first assertion: %d, body %s", w.Code, w.Body.String())
	}
	// SignIn rotated alice's cookie; re-sign-in to hold a valid session
	// for the replay attempt (the replayed ceremony itself must fail on
	// the consumed challenge, not on the session).
	alice2 := e.signIn(t, "alice")
	if w := postJSON(t, e.h.StepUpFinish, alice2, body); w.Code != http.StatusBadRequest {
		t.Errorf("replayed assertion: %d, want 400", w.Code)
	}

	// Subject binding: bob cannot finish with a challenge minted for
	// alice, even holding alice's public ceremony output.
	bob := e.signIn(t, "bob")
	ab, _ := authtest.New()
	enroll(t, e, bob, ab)
	aliceChallenge := challengeFrom(t, postJSON(t, e.h.StepUpBegin, alice2, nil))
	cd, ad, s2, err := ab.Get(aliceChallenge, authtest.Options{RPID: testRPID, Origin: testOrigin})
	if err != nil {
		t.Fatal(err)
	}
	w := postJSON(t, e.h.StepUpFinish, bob, map[string]string{
		"id": b64(ab.CredID), "clientDataJSON": b64(cd),
		"authenticatorData": b64(ad), "signature": b64(s2),
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("cross-subject challenge: %d, want 400", w.Code)
	}
}

func TestBadCeremoniesRefused(t *testing.T) {
	e := newEnv(t)
	cookie := e.signIn(t, "42")
	a, _ := authtest.New()
	enroll(t, e, cookie, a)

	// Wrong origin in the signed client data.
	if w := assert(t, e, cookie, a, authtest.Options{Origin: "http://evil.test"}); w.Code != http.StatusBadRequest {
		t.Errorf("wrong-origin assertion: %d, want 400", w.Code)
	}
	// Corrupted signature.
	if w := assert(t, e, cookie, a, authtest.Options{CorruptSig: true}); w.Code != http.StatusBadRequest {
		t.Errorf("corrupt-signature assertion: %d, want 400", w.Code)
	}
	// A counter that goes backwards (cloned-credential signal).
	if w := assert(t, e, cookie, a, authtest.Options{}); w.Code != http.StatusOK {
		t.Fatalf("baseline assertion failed: %d", w.Code)
	}
	cookie = e.signIn(t, "42") // baseline rotated the session
	stale := uint32(1)
	if w := assert(t, e, cookie, a, authtest.Options{Count: &stale}); w.Code != http.StatusBadRequest {
		t.Errorf("stale-counter assertion: %d, want 400", w.Code)
	}
}

func TestNewValidatesConfig(t *testing.T) {
	e := newEnv(t)
	cases := []passkey.Config{
		{DB: e.db, Origin: testOrigin},              // no sessions
		{Sessions: e.sess, Origin: testOrigin},      // no db
		{Sessions: e.sess, DB: e.db},                // no origin
		{Sessions: e.sess, DB: e.db, Origin: "app"}, // relative origin
	}
	for i, cfg := range cases {
		if _, err := passkey.New(cfg); err == nil {
			t.Errorf("case %d: New succeeded, want error", i)
		}
	}
}

func TestSweepDeletesOnlyExpired(t *testing.T) {
	e := newEnv(t)
	cookie := e.signIn(t, "42")
	a, _ := authtest.New()
	enroll(t, e, cookie, a)

	// A live challenge survives a sweep and still finishes.
	challenge := challengeFrom(t, postJSON(t, e.h.StepUpBegin, cookie, nil))
	if err := passkey.Sweep(e.db, time.Now()); err != nil {
		t.Fatal(err)
	}
	cd, ad, sig, err := a.Get(challenge, authtest.Options{RPID: testRPID, Origin: testOrigin})
	if err != nil {
		t.Fatal(err)
	}
	w := postJSON(t, e.h.StepUpFinish, cookie, map[string]string{
		"id": b64(a.CredID), "clientDataJSON": b64(cd),
		"authenticatorData": b64(ad), "signature": b64(sig),
	})
	if w.Code != http.StatusOK {
		t.Fatalf("live challenge swept away: %d, body %s", w.Code, w.Body.String())
	}

	// An expired one is gone after the sweep.
	cookie = e.signIn(t, "42")
	challengeFrom(t, postJSON(t, e.h.StepUpBegin, cookie, nil))
	if err := passkey.Sweep(e.db, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM passkey_challenges`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("challenges after sweep = %d, want 0", n)
	}
}
