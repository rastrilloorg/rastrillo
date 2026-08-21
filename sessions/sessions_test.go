package sessions_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/carlosframework/rastrillo/sessions"
)

// newTestSessions is the shared helper: temp DB, Migrations applied,
// New(Config{DB: db, Origin: origin}). Origin defaults "http://app.test".
func newTestSessions(t *testing.T, mut func(*sessions.Config)) (*sessions.Sessions, *sql.DB) {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), "s.db") + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	for _, stmt := range sessions.Migrations {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("migration %q: %v", stmt, err)
		}
	}
	cfg := sessions.Config{DB: db, Origin: "http://app.test"}
	if mut != nil {
		mut(&cfg)
	}
	s, err := sessions.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s, db
}

// cookieFrom extracts the named cookie from a recorder's response, or
// fails the test — every SignIn in these tests is expected to set one.
func cookieFrom(t *testing.T, w *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no cookie %q in response; got %v", name, w.Result().Cookies())
	return nil
}

func TestSignInSetsCookieAndStoresHash(t *testing.T) {
	s, db := newTestSessions(t, nil)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "http://app.test/signin", nil)
	if err := s.SignIn(w, r, sessions.Session{Subject: "42", Method: "password"}); err != nil {
		t.Fatalf("SignIn: %v", err)
	}

	cookie := cookieFrom(t, w, s.CookieName())
	if !cookie.HttpOnly {
		t.Errorf("cookie HttpOnly = false, want true")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie SameSite = %v, want Lax", cookie.SameSite)
	}
	if cookie.Path != "/" {
		t.Errorf("cookie Path = %q, want /", cookie.Path)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("sessions row count = %d, want 1", count)
	}
	var hash string
	if err := db.QueryRow(`SELECT token_hash FROM sessions`).Scan(&hash); err != nil {
		t.Fatalf("select token_hash: %v", err)
	}
	if hash == cookie.Value {
		t.Errorf("token_hash equals cookie value — only the hash should be stored")
	}
}

func TestFromRoundTrip(t *testing.T) {
	s, _ := newTestSessions(t, nil)
	w := httptest.NewRecorder()
	signInReq := httptest.NewRequest("POST", "http://app.test/signin", nil)
	if err := s.SignIn(w, signInReq, sessions.Session{Subject: "42", Method: "password"}); err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	cookie := cookieFrom(t, w, s.CookieName())

	r := httptest.NewRequest("GET", "http://app.test/", nil)
	r.AddCookie(cookie)
	sess, ok := s.From(r)
	if !ok {
		t.Fatalf("From: ok = false, want true")
	}
	if sess.Subject != "42" {
		t.Errorf("Subject = %q, want 42", sess.Subject)
	}
}

func TestSignOutRevokesServerSide(t *testing.T) {
	s, _ := newTestSessions(t, nil)
	w := httptest.NewRecorder()
	signInReq := httptest.NewRequest("POST", "http://app.test/signin", nil)
	if err := s.SignIn(w, signInReq, sessions.Session{Subject: "1", Method: "password"}); err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	cookie := cookieFrom(t, w, s.CookieName())

	signOutReq := httptest.NewRequest("POST", "http://app.test/signout", nil)
	signOutReq.AddCookie(cookie)
	s.SignOut(httptest.NewRecorder(), signOutReq)

	r := httptest.NewRequest("GET", "http://app.test/", nil)
	r.AddCookie(cookie)
	if _, ok := s.From(r); ok {
		t.Errorf("From ok = true after SignOut, want false (row must be deleted)")
	}
}

func TestExpiredSessionRejected(t *testing.T) {
	s, _ := newTestSessions(t, func(cfg *sessions.Config) { cfg.TTL = time.Millisecond })
	w := httptest.NewRecorder()
	signInReq := httptest.NewRequest("POST", "http://app.test/signin", nil)
	if err := s.SignIn(w, signInReq, sessions.Session{Subject: "1"}); err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	cookie := cookieFrom(t, w, s.CookieName())

	time.Sleep(5 * time.Millisecond)

	r := httptest.NewRequest("GET", "http://app.test/", nil)
	r.AddCookie(cookie)
	if _, ok := s.From(r); ok {
		t.Errorf("From ok = true for expired session, want false")
	}
}

func TestSignInRotatesToken(t *testing.T) {
	s, _ := newTestSessions(t, nil)
	w1 := httptest.NewRecorder()
	r1 := httptest.NewRequest("POST", "http://app.test/signin", nil)
	if err := s.SignIn(w1, r1, sessions.Session{Subject: "1"}); err != nil {
		t.Fatalf("SignIn 1: %v", err)
	}
	cookieA := cookieFrom(t, w1, s.CookieName())

	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("POST", "http://app.test/signin", nil)
	r2.AddCookie(cookieA)
	if err := s.SignIn(w2, r2, sessions.Session{Subject: "1"}); err != nil {
		t.Fatalf("SignIn 2: %v", err)
	}
	cookieB := cookieFrom(t, w2, s.CookieName())

	if cookieA.Value == cookieB.Value {
		t.Fatalf("cookie B equals cookie A, want a rotated (distinct) token")
	}

	r := httptest.NewRequest("GET", "http://app.test/", nil)
	r.AddCookie(cookieA)
	if _, ok := s.From(r); ok {
		t.Errorf("From ok = true for rotated-out cookie A, want false (old row deleted)")
	}
}

func TestHostPrefixOnHTTPS(t *testing.T) {
	https, _ := newTestSessions(t, func(cfg *sessions.Config) { cfg.Origin = "https://app.example.com" })
	if got := https.CookieName(); got != "__Host-rastrillo_session" {
		t.Errorf("CookieName() = %q, want __Host-rastrillo_session", got)
	}

	plain, _ := newTestSessions(t, func(cfg *sessions.Config) { cfg.Origin = "http://localhost:8080" })
	if got := plain.CookieName(); got != "rastrillo_session" {
		t.Errorf("CookieName() = %q, want rastrillo_session", got)
	}
}

func TestRequireRedirectsWithReturnTo(t *testing.T) {
	s, _ := newTestSessions(t, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("next called, want short-circuit for a signed-out request")
	})
	h := s.Require(next)

	getR := httptest.NewRequest("GET", "http://app.test/notes/7", nil)
	getW := httptest.NewRecorder()
	h.ServeHTTP(getW, getR)
	if getW.Code != http.StatusSeeOther {
		t.Fatalf("GET status = %d, want 303", getW.Code)
	}
	if loc := getW.Header().Get("Location"); loc != "/signin?return_to=%2Fnotes%2F7" {
		t.Errorf("Location = %q, want /signin?return_to=%%2Fnotes%%2F7", loc)
	}

	postR := httptest.NewRequest("POST", "http://app.test/notes/7", nil)
	postW := httptest.NewRecorder()
	h.ServeHTTP(postW, postR)
	if postW.Code != http.StatusForbidden {
		t.Errorf("POST status = %d, want 403", postW.Code)
	}
	if loc := postW.Header().Get("Location"); loc != "" {
		t.Errorf("POST set Location = %q, want none (no redirect for non-GET/HEAD)", loc)
	}
}

func TestRequireStashesCurrent(t *testing.T) {
	s, _ := newTestSessions(t, nil)
	w := httptest.NewRecorder()
	signInReq := httptest.NewRequest("POST", "http://app.test/signin", nil)
	if err := s.SignIn(w, signInReq, sessions.Session{Subject: "42", Method: "password"}); err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	cookie := cookieFrom(t, w, s.CookieName())

	var gotOK, gotUserOK bool
	var gotUserID int64
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, gotOK = sessions.Current(r)
		gotUserID, gotUserOK = sessions.UserID(r)
	})
	h := s.Require(next)

	r := httptest.NewRequest("GET", "http://app.test/notes/7", nil)
	r.AddCookie(cookie)
	h.ServeHTTP(httptest.NewRecorder(), r)

	if !gotOK {
		t.Fatalf("Current ok = false inside Require, want true")
	}
	if !gotUserOK || gotUserID != 42 {
		t.Errorf("UserID = (%d, %v), want (42, true)", gotUserID, gotUserOK)
	}
}

func TestSafeReturnRejectsAbsoluteAndSchemeless(t *testing.T) {
	const fallback = "/"
	cases := []struct {
		returnTo string
		want     string
	}{
		{"/notes/7", "/notes/7"},
		{"https://evil.example", fallback},
		{"//evil.example", fallback},
		{"/ok\\evil", fallback},
		{"", fallback},
	}
	for _, c := range cases {
		u := "http://app.test/signin?return_to=" + url.QueryEscape(c.returnTo)
		r := httptest.NewRequest("GET", u, nil)
		if got := sessions.SafeReturn(r, fallback); got != c.want {
			t.Errorf("SafeReturn(return_to=%q) = %q, want %q", c.returnTo, got, c.want)
		}
	}
}
