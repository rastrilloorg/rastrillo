package sessions_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"amadan.net/rastrillo/rastrillo/sessions"
)

// signInFresh mints a session for subject in a fresh browser and
// returns the cookie it set.
func signInFresh(t *testing.T, s *sessions.Sessions, subject, method string) *http.Cookie {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "http://app.test/signin", nil)
	if err := s.SignIn(w, r, sessions.Session{Subject: subject, Method: method}); err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	return cookieFrom(t, w, s.CookieName())
}

func alive(s *sessions.Sessions, c *http.Cookie) bool {
	r := httptest.NewRequest("GET", "http://app.test/", nil)
	r.AddCookie(c)
	_, ok := s.From(r)
	return ok
}

func TestListIsTheSubjectsLiveSessionsOnly(t *testing.T) {
	s, _ := newTestSessions(t, nil)
	a := signInFresh(t, s, "alice", "magiclink+passkey")
	b := signInFresh(t, s, "alice", "password+totp")
	signInFresh(t, s, "bob", "magiclink")

	list, err := s.List("alice")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("List(alice) = %d sessions, want 2", len(list))
	}
	hashes := map[string]bool{sessions.HashToken(a.Value): true, sessions.HashToken(b.Value): true}
	for _, in := range list {
		if !hashes[in.Hash] {
			t.Errorf("List returned a hash that is not one of alice's: %q", in.Hash)
		}
		if in.Subject != "alice" || in.Method == "" || in.At.IsZero() || in.ExpiresAt.IsZero() {
			t.Errorf("Info incomplete: %+v", in)
		}
	}
}

func TestHashNamesThePresentedSession(t *testing.T) {
	s, _ := newTestSessions(t, nil)
	c := signInFresh(t, s, "alice", "m")
	r := httptest.NewRequest("GET", "http://app.test/", nil)
	r.AddCookie(c)
	h, ok := s.Hash(r)
	if !ok || h != sessions.HashToken(c.Value) {
		t.Fatalf("Hash = %q, %v; want the cookie's hash", h, ok)
	}
	if _, ok := s.Hash(httptest.NewRequest("GET", "http://app.test/", nil)); ok {
		t.Fatal("Hash ok with no cookie")
	}
}

func TestRevokeEndsOneAndOnlyTheSubjectsOwn(t *testing.T) {
	s, _ := newTestSessions(t, nil)
	a := signInFresh(t, s, "alice", "m")
	b := signInFresh(t, s, "alice", "m")
	bob := signInFresh(t, s, "bob", "m")

	if err := s.Revoke("alice", sessions.HashToken(a.Value)); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if alive(s, a) || !alive(s, b) {
		t.Fatalf("after Revoke(a): a alive=%v b alive=%v, want false true", alive(s, a), alive(s, b))
	}
	// Bob's handle in Alice's hands names nothing.
	if err := s.Revoke("alice", sessions.HashToken(bob.Value)); !errors.Is(err, sessions.ErrNotYours) {
		t.Fatalf("Revoke(alice, bob's) = %v, want ErrNotYours", err)
	}
	if !alive(s, bob) {
		t.Fatal("bob's session died to alice's revoke")
	}
	if err := s.Revoke("alice", "no-such-hash"); !errors.Is(err, sessions.ErrNotYours) {
		t.Fatalf("Revoke(unknown) = %v, want ErrNotYours", err)
	}
}

func TestRevokeOthersKeepsThePresentedSession(t *testing.T) {
	s, _ := newTestSessions(t, nil)
	here := signInFresh(t, s, "alice", "m")
	phone := signInFresh(t, s, "alice", "m")
	tv := signInFresh(t, s, "alice", "m")
	bob := signInFresh(t, s, "bob", "m")

	r := httptest.NewRequest("POST", "http://app.test/profile/security/password", nil)
	r.AddCookie(here)
	n, err := s.RevokeOthers(r, "alice")
	if err != nil {
		t.Fatalf("RevokeOthers: %v", err)
	}
	if n != 2 {
		t.Errorf("RevokeOthers ended %d, want 2", n)
	}
	if !alive(s, here) || alive(s, phone) || alive(s, tv) || !alive(s, bob) {
		t.Fatalf("here=%v phone=%v tv=%v bob=%v; want true false false true", alive(s, here), alive(s, phone), alive(s, tv), alive(s, bob))
	}
}

func TestRevokeAllEndsEverything(t *testing.T) {
	s, _ := newTestSessions(t, nil)
	here := signInFresh(t, s, "alice", "m")
	phone := signInFresh(t, s, "alice", "m")
	bob := signInFresh(t, s, "bob", "m")
	n, err := s.RevokeAll("alice")
	if err != nil {
		t.Fatalf("RevokeAll: %v", err)
	}
	if n != 2 || alive(s, here) || alive(s, phone) || !alive(s, bob) {
		t.Fatalf("n=%d here=%v phone=%v bob=%v; want 2 false false true", n, alive(s, here), alive(s, phone), alive(s, bob))
	}
	if list, _ := s.List("alice"); len(list) != 0 {
		t.Fatalf("List after RevokeAll = %d, want 0", len(list))
	}
}
