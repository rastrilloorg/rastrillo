package vault_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/crypto"
	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/sessions"
	"amadan.net/rastrillo/rastrillo/vault"
)

func testSessions(t *testing.T) (*sessions.Sessions, *sql.DB) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "s.db"), nil)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := migrate.Apply(context.Background(), d, sessions.Schema); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	s, err := sessions.New(sessions.Config{DB: d.Writer(), Origin: "http://app.test"})
	if err != nil {
		t.Fatalf("sessions.New: %v", err)
	}
	return s, d.Writer()
}

// TestRestoreWrapRoundTrip: the pure half of the restore handoff —
// the home seals the credential to the caller's ephemeral key with
// the nonce inside; opening verifies both.
func TestRestoreWrapRoundTrip(t *testing.T) {
	req, kp, err := vault.NewRestoreRequest("http://app.test/vault/restore")
	if err != nil {
		t.Fatalf("NewRestoreRequest: %v", err)
	}
	if req.Nonce == "" || len(req.EphPub) == 0 || req.Ret != "http://app.test/vault/restore" {
		t.Fatalf("request = %+v", req)
	}

	sealed := sealRestoreReturn(t, req, "the-token")

	token, err := vault.OpenRestoreReturn(kp, req.Nonce, sealed)
	if err != nil {
		t.Fatalf("OpenRestoreReturn: %v", err)
	}
	if token != "the-token" {
		t.Fatalf("token = %q", token)
	}

	// A replay under a different nonce fails closed.
	if _, err := vault.OpenRestoreReturn(kp, "other-nonce", sealed); err == nil {
		t.Fatal("OpenRestoreReturn accepted a wrong nonce")
	}
	// Tampered bytes fail closed.
	sealed[len(sealed)-1] ^= 1
	if _, err := vault.OpenRestoreReturn(kp, req.Nonce, sealed); err == nil {
		t.Fatal("OpenRestoreReturn accepted tampered bytes")
	}
}

// sealRestoreReturn plays the home's half: seal {token, nonce} to the
// request's ephemeral public key under the pinned context.
func sealRestoreReturn(t *testing.T, req vault.RestoreRequest, token string) []byte {
	t.Helper()
	sealed, err := crypto.Seal(req.EphPub, "rastrillo/vault/restore/v1",
		[]byte(`{"token":"`+token+`","nonce":"`+req.Nonce+`"}`))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	return sealed
}

// TestEnrolHandlerMintsVaultRow: a signed-in POST answers the home
// enrol URL whose fragment payload carries a fresh Method:"vault"
// token — a different row from the browser's own session.
func TestEnrolHandlerMintsVaultRow(t *testing.T) {
	s, _ := testSessions(t)
	h := vault.Handoff{Sessions: s, Home: "https://home.test", Origin: "http://app.test"}

	// A signed-in request: SignIn, carry the cookie, stash via Middleware.
	w0 := httptest.NewRecorder()
	if err := s.SignIn(w0, httptest.NewRequest("POST", "/", nil), sessions.Session{Subject: "7", Method: "password"}); err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	cookie := w0.Result().Cookies()[0]

	r := httptest.NewRequest("POST", "/vault/enrol", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	var handler http.Handler = http.HandlerFunc(h.Enrol)
	handler = s.Require(handler)
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("enrol answered %d: %s", w.Code, w.Body)
	}
	var payload vault.EnrolAnswer
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("enrol answer: %v", err)
	}
	if payload.Entry.Token == "" || payload.Entry.Token == cookie.Value {
		t.Fatal("enrol payload must carry a fresh token, not the browser's cookie")
	}
	if !strings.HasPrefix(payload.EnrolURL, "https://home.test/enrol#") {
		t.Fatalf("EnrolURL = %q", payload.EnrolURL)
	}
	if payload.Entry.URL != "http://app.test" || payload.Nonce == "" {
		t.Fatalf("payload = %+v, want the app origin and a nonce", payload)
	}

	// The minted token resolves to a Method:"vault" row for the same subject.
	rr := httptest.NewRequest("GET", "/", nil)
	rr.AddCookie(&http.Cookie{Name: s.CookieName(), Value: payload.Entry.Token})
	sess, ok := s.From(rr)
	if !ok || sess.Method != "vault" || sess.Subject != "7" {
		t.Fatalf("vault row = %+v ok=%v, want Subject 7 Method vault", sess, ok)
	}
}

// TestRestoreHandlerAdoptsAndFallsBack: a live token adopts (cookie
// set, redirect to return_to); a dead one redirects to signin with no
// cookie — restore failure is a signin prompt, never an error page.
func TestRestoreHandlerAdoptsAndFallsBack(t *testing.T) {
	s, _ := testSessions(t)
	h := vault.Handoff{Sessions: s, Home: "https://home.test", Origin: "http://app.test"}

	token, err := s.Mint(sessions.Session{Subject: "7", Method: "vault"})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	form := url.Values{"token": {token}, "return_to": {"/notes"}}
	r := httptest.NewRequest("POST", "/vault/restore", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.Restore(w, r)

	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/notes" {
		t.Fatalf("restore answered %d → %q, want 303 → /notes", w.Code, w.Header().Get("Location"))
	}
	if len(w.Result().Cookies()) == 0 {
		t.Fatal("restore set no cookie")
	}

	// Dead token: no cookie, off to signin.
	form = url.Values{"token": {"garbage"}}
	r = httptest.NewRequest("POST", "/vault/restore", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	h.Restore(w, r)
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/signin") {
		t.Fatalf("dead-token restore answered %d → %q, want 303 → /signin...", w.Code, w.Header().Get("Location"))
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("dead-token restore set a cookie")
	}
}
