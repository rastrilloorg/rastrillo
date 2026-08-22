package sessions_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/carlosframework/rastrillo/db"
	"github.com/carlosframework/rastrillo/migrate"
	"github.com/carlosframework/rastrillo/sessions"
)

// newTestSessions is the shared helper: temp DB, Schema applied,
// New(Config{DB: db, Origin: origin}). Origin defaults "http://app.test".
func newTestSessions(t *testing.T, mut func(*sessions.Config)) (*sessions.Sessions, *sql.DB) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "s.db"), nil)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := migrate.Apply(context.Background(), d, sessions.Schema); err != nil {
		t.Fatalf("migrate.Apply: %v", err)
	}
	cfg := sessions.Config{DB: d.Writer(), Origin: "http://app.test"}
	if mut != nil {
		mut(&cfg)
	}
	s, err := sessions.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s, d.Writer()
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

func TestSecureAttributeFollowsOrigin(t *testing.T) {
	https, _ := newTestSessions(t, func(cfg *sessions.Config) { cfg.Origin = "https://app.example.com" })
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "https://app.example.com/signin", nil)
	if err := https.SignIn(w, r, sessions.Session{Subject: "42"}); err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if c := cookieFrom(t, w, https.CookieName()); !c.Secure {
		t.Errorf("https origin: cookie Secure = false, want true (__Host- requires it)")
	}

	plain, _ := newTestSessions(t, nil)
	w = httptest.NewRecorder()
	r = httptest.NewRequest("POST", "http://app.test/signin", nil)
	if err := plain.SignIn(w, r, sessions.Session{Subject: "42"}); err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if c := cookieFrom(t, w, plain.CookieName()); c.Secure {
		t.Errorf("http origin: cookie Secure = true — browsers drop Secure cookies set over plain http")
	}
}

func TestNewValidatesConfig(t *testing.T) {
	if _, err := sessions.New(sessions.Config{Origin: "http://app.test"}); err == nil {
		t.Errorf("New with nil DB: err = nil, want error")
	}

	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "v.db"))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	for _, origin := range []string{"", "app.example.com", "ftp://app.example.com"} {
		if _, err := sessions.New(sessions.Config{DB: db, Origin: origin}); err == nil {
			t.Errorf("New with Origin %q: err = nil, want error", origin)
		}
	}
}

func TestMiddlewareStashesButNeverBlocks(t *testing.T) {
	s, _ := newTestSessions(t, nil)
	w := httptest.NewRecorder()
	signInReq := httptest.NewRequest("POST", "http://app.test/signin", nil)
	if err := s.SignIn(w, signInReq, sessions.Session{Subject: "42", Method: "password"}); err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	cookie := cookieFrom(t, w, s.CookieName())

	var called, gotOK bool
	var gotSubject string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		var sess sessions.Session
		sess, gotOK = sessions.Current(r)
		gotSubject = sess.Subject
	})
	h := s.Middleware(next)

	r := httptest.NewRequest("GET", "http://app.test/", nil)
	r.AddCookie(cookie)
	h.ServeHTTP(httptest.NewRecorder(), r)
	if !called {
		t.Fatalf("signed-in: next not called")
	}
	if !gotOK || gotSubject != "42" {
		t.Errorf("signed-in: Current = (%q, %v), want (42, true)", gotSubject, gotOK)
	}

	called, gotOK = false, false
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "http://app.test/", nil))
	if !called {
		t.Fatalf("signed-out: next not called — Middleware must never block")
	}
	if gotOK {
		t.Errorf("signed-out: Current ok = true, want false")
	}
}

// clearedCookie reports whether the response carries a clearing
// Set-Cookie (MaxAge < 0) for name.
func clearedCookie(w *httptest.ResponseRecorder, name string) bool {
	for _, c := range w.Result().Cookies() {
		if c.Name == name && c.MaxAge < 0 {
			return true
		}
	}
	return false
}

func TestStaleCookieClearedButErrorIsNot(t *testing.T) {
	s, db := newTestSessions(t, nil)
	w := httptest.NewRecorder()
	signInReq := httptest.NewRequest("POST", "http://app.test/signin", nil)
	if err := s.SignIn(w, signInReq, sessions.Session{Subject: "42"}); err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	cookie := cookieFrom(t, w, s.CookieName())
	if _, err := db.Exec(`DELETE FROM sessions`); err != nil {
		t.Fatalf("revoke all: %v", err)
	}

	// Revoked row + surviving cookie: both middlewares clear it.
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	for name, h := range map[string]http.Handler{"Require": s.Require(next), "Middleware": s.Middleware(next)} {
		r := httptest.NewRequest("GET", "http://app.test/", nil)
		r.AddCookie(cookie)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if !clearedCookie(rec, s.CookieName()) {
			t.Errorf("%s: stale cookie not cleared", name)
		}
	}

	// A lookup ERROR is not staleness: closing the DB makes every
	// lookup fail, and a transient failure must never cost the visitor
	// their (possibly live) cookie.
	db.Close()
	r := httptest.NewRequest("GET", "http://app.test/", nil)
	r.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.Require(next).ServeHTTP(rec, r)
	if clearedCookie(rec, s.CookieName()) {
		t.Errorf("cookie cleared on lookup error — only a definitive miss may clear")
	}
}

func TestRequireTrustsMiddlewareResolution(t *testing.T) {
	s, db := newTestSessions(t, nil)
	w := httptest.NewRecorder()
	signInReq := httptest.NewRequest("POST", "http://app.test/signin", nil)
	if err := s.SignIn(w, signInReq, sessions.Session{Subject: "42"}); err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	cookie := cookieFrom(t, w, s.CookieName())

	// Capture the request exactly as Middleware hands it downstream —
	// session already resolved and stashed in the context.
	var stashed *http.Request
	capture := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { stashed = r })
	r := httptest.NewRequest("GET", "http://app.test/", nil)
	r.AddCookie(cookie)
	s.Middleware(capture).ServeHTTP(httptest.NewRecorder(), r)
	if stashed == nil {
		t.Fatalf("Middleware never called next")
	}

	// Close the DB: from here, any lookup fails. If Require re-queried
	// instead of trusting the stash, it would refuse this request.
	db.Close()
	var gotSubject string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, _ := sessions.Current(r)
		gotSubject = sess.Subject
	})
	rec := httptest.NewRecorder()
	s.Require(inner).ServeHTTP(rec, stashed)
	if gotSubject != "42" {
		t.Errorf("Require behind Middleware re-resolved (Subject = %q, status = %d) — the stash must be trusted, one lookup per request", gotSubject, rec.Code)
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

func TestRequireFreshStepUp(t *testing.T) {
	s, db := newTestSessions(t, nil)
	signIn := func(sess sessions.Session) *http.Cookie {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "http://app.test/signin", nil)
		if err := s.SignIn(w, r, sess); err != nil {
			t.Fatalf("SignIn: %v", err)
		}
		return cookieFrom(t, w, s.CookieName())
	}
	drive := func(cookie *http.Cookie, method string) (*httptest.ResponseRecorder, bool) {
		var admitted bool
		h := s.RequireFresh(5 * time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			admitted = true
		}))
		r := httptest.NewRequest(method, "http://app.test/settings/keys", nil)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w, admitted
	}

	// Fresh AuthTime: admitted.
	fresh := signIn(sessions.Session{Subject: "42", AuthTime: time.Now()})
	if _, admitted := drive(fresh, "GET"); !admitted {
		t.Fatalf("fresh AuthTime refused")
	}

	// Stale AuthTime: GET redirects with return_to and reauth=1.
	staleCookie := signIn(sessions.Session{Subject: "42", AuthTime: time.Now().Add(-time.Hour)})
	w, admitted := drive(staleCookie, "GET")
	if admitted {
		t.Fatalf("hour-old AuthTime admitted past a 5m gate")
	}
	if w.Code != http.StatusSeeOther {
		t.Fatalf("stale GET: code = %d, want 303", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/signin?return_to=%2Fsettings%2Fkeys&reauth=1" {
		t.Errorf("stale GET Location = %q, want return_to plus reauth=1", loc)
	}

	// Stale POST: 403, not a redirect.
	if w, _ := drive(staleCookie, "POST"); w.Code != http.StatusForbidden {
		t.Errorf("stale POST: code = %d, want 403", w.Code)
	}

	// Zero AuthTime (a magic-link plugin): the row's creation time
	// stands in, so a just-minted session is fresh — no redirect loop.
	zeroAuth := signIn(sessions.Session{Subject: "42"})
	if _, admitted := drive(zeroAuth, "GET"); !admitted {
		t.Fatalf("just-minted session with zero AuthTime refused — the At fallback must admit it")
	}

	// ...and once the row itself is old, it goes stale like any other.
	old := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	if _, err := db.Exec(`UPDATE sessions SET created_at = ?`, old); err != nil {
		t.Fatal(err)
	}
	if _, admitted := drive(zeroAuth, "GET"); admitted {
		t.Errorf("hour-old zero-AuthTime session admitted past a 5m gate")
	}
}

func TestSweepDeletesExpiredKeepsLive(t *testing.T) {
	s, db := newTestSessions(t, nil)
	now := time.Now().UTC()
	insert := func(hash string, expires time.Time) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO sessions (token_hash, subject, method, auth_time, created_at, expires_at)
			VALUES (?, ?, ?, ?, ?, ?)`, hash, "1", "password", "",
			now.Format(time.RFC3339), expires.UTC().Format(time.RFC3339)); err != nil {
			t.Fatalf("insert %q: %v", hash, err)
		}
	}
	insert("expired", now.Add(-time.Hour))
	insert("live", now.Add(time.Hour))

	if err := s.Sweep(now); err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	var hashes []string
	rows, err := db.Query(`SELECT token_hash FROM sessions`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			t.Fatalf("scan: %v", err)
		}
		hashes = append(hashes, h)
	}
	if len(hashes) != 1 || hashes[0] != "live" {
		t.Fatalf("sessions after Sweep = %v, want only %q", hashes, "live")
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
