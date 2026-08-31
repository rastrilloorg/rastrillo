package sessions_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/sessions"
)

// TestMintReturnsTokenWithoutCookie: Mint creates a live row and hands
// back its token; no ResponseWriter is involved, so nothing can set a
// cookie — the credential is for somewhere other than this browser.
func TestMintReturnsTokenWithoutCookie(t *testing.T) {
	s, _ := newTestSessions(t, nil)

	token, err := s.Mint(sessions.Session{Subject: "7", Method: "vault"})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if token == "" {
		t.Fatal("Mint returned an empty token")
	}

	// The row is real: presenting the token as a cookie resolves it.
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: s.CookieName(), Value: token})
	sess, ok := s.From(r)
	if !ok {
		t.Fatal("minted token did not resolve to a session")
	}
	if sess.Subject != "7" || sess.Method != "vault" {
		t.Fatalf("minted session = %+v, want Subject 7 Method vault", sess)
	}
}

// TestAdoptSetsCookieForLiveToken: Adopt verifies the presented token
// and binds this browser to the existing row — no new row is minted.
func TestAdoptSetsCookieForLiveToken(t *testing.T) {
	s, _ := newTestSessions(t, nil)

	token, err := s.Mint(sessions.Session{Subject: "7", Method: "vault"})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/vault/restore", nil)
	sess, ok := s.Adopt(w, r, token)
	if !ok {
		t.Fatal("Adopt refused a live token")
	}
	if sess.Subject != "7" {
		t.Fatalf("Adopt session = %+v, want Subject 7", sess)
	}
	c := cookieFrom(t, w, s.CookieName())
	if c.Value != token {
		t.Fatal("Adopt set a cookie that is not the presented token — it should adopt, not mint")
	}
}

// TestAdoptRefusesDeadTokens: a garbage token and an expired row both
// refuse without setting any cookie.
func TestAdoptRefusesDeadTokens(t *testing.T) {
	s, _ := newTestSessions(t, func(c *sessions.Config) { c.TTL = time.Millisecond })

	token, err := s.Mint(sessions.Session{Subject: "7", Method: "vault"})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	time.Sleep(5 * time.Millisecond) // outlive the 1ms TTL

	for name, tok := range map[string]string{"expired": token, "garbage": "not-a-token"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/vault/restore", nil)
		if _, ok := s.Adopt(w, r, tok); ok {
			t.Fatalf("%s: Adopt accepted a dead token", name)
		}
		if len(w.Result().Cookies()) != 0 {
			t.Fatalf("%s: Adopt set a cookie on refusal", name)
		}
	}
}
