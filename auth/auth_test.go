package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	rastrillo "github.com/carlosframework/rastrillo"
)

// captureMailer records the last message instead of sending it.
type captureMailer struct{ to, subject, body string }

func (m *captureMailer) Send(_ context.Context, to, subject, body string) error {
	m.to, m.subject, m.body = to, subject, body
	return nil
}

func newTestAuth(t *testing.T, mut func(*Config)) (*Auth, *captureMailer) {
	t.Helper()
	db, err := rastrillo.OpenDB(filepath.Join(t.TempDir(), "auth.db"), Migrations)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	m := &captureMailer{}
	cfg := Config{
		DB:          db,
		Origin:      "http://app.test",
		InstanceKey: "test-instance-key",
		Mailer:      m,
	}
	if mut != nil {
		mut(&cfg)
	}
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a, m
}

func TestNewValidatesConfig(t *testing.T) {
	db, _ := rastrillo.OpenDB(filepath.Join(t.TempDir(), "v.db"), Migrations)
	defer db.Close()
	cases := []struct {
		name string
		cfg  Config
	}{
		{"empty origin", Config{DB: db, InstanceKey: "k"}},
		{"relative origin", Config{DB: db, InstanceKey: "k", Origin: "app.test"}},
		{"empty instance key", Config{DB: db, Origin: "https://app.test"}},
		{"nil db", Config{Origin: "https://app.test", InstanceKey: "k"}},
	}
	for _, c := range cases {
		if _, err := New(c.cfg); err == nil {
			t.Errorf("New(%s) succeeded, want error", c.name)
		}
	}
}

// beginSignin POSTs the signin form with same-origin evidence and the
// force escape hatch (so no classification network happens in tests).
func beginSignin(t *testing.T, a *Auth, address string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"address": {address}, "force": {"1"}}
	r := httptest.NewRequest("POST", "http://app.test/signin", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	w := httptest.NewRecorder()
	a.Begin(w, r)
	return w
}

var linkRE = regexp.MustCompile(`http://app\.test/auth/verify\?token=[A-Za-z0-9_-]+`)

func TestMagicLinkEndToEnd(t *testing.T) {
	a, m := newTestAuth(t, nil)

	// Begin: force straight to the magic link.
	w := beginSignin(t, a, "person@example.com")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/signin?sent=1" {
		t.Fatalf("Begin: %d → %q, want 303 → /signin?sent=1", w.Code, w.Header().Get("Location"))
	}
	if m.to != "person@example.com" {
		t.Fatalf("mail went to %q", m.to)
	}
	link := linkRE.FindString(m.body)
	if link == "" {
		t.Fatalf("no verify link in mail body:\n%s", m.body)
	}

	// Verify: land the link, get a session.
	r := httptest.NewRequest("GET", link, nil)
	w = httptest.NewRecorder()
	a.Verify(w, r)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" {
		t.Fatalf("Verify: %d → %q, want 303 → /", w.Code, w.Header().Get("Location"))
	}
	var session *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == a.SessionCookie() && c.Value != "" {
			session = c
		}
	}
	if session == nil {
		t.Fatal("Verify set no session cookie")
	}
	if session.Name != "rastrillo_session" {
		t.Fatalf("http origin got cookie %q, want the unprefixed name", session.Name)
	}

	// The link is single-use.
	w = httptest.NewRecorder()
	a.Verify(w, httptest.NewRequest("GET", link, nil))
	if w.Header().Get("Location") != "/signin?err=expired" {
		t.Fatalf("second Verify: %q, want /signin?err=expired", w.Header().Get("Location"))
	}

	// RequireSession admits the cookie and exposes the identity.
	var got Identity
	protected := a.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = From(r)
	}))
	r = httptest.NewRequest("GET", "http://app.test/private", nil)
	r.AddCookie(session)
	protected.ServeHTTP(httptest.NewRecorder(), r)
	if got.Address != "person@example.com" || got.Method != "magiclink" {
		t.Fatalf("From = %+v", got)
	}

	// Signout revokes for real: the same cookie no longer admits.
	r = httptest.NewRequest("POST", "http://app.test/signout", nil)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.AddCookie(session)
	a.Signout(httptest.NewRecorder(), r)

	r = httptest.NewRequest("GET", "http://app.test/private", nil)
	r.AddCookie(session)
	w = httptest.NewRecorder()
	protected.ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("after signout: %d, want a redirect to the signin page", w.Code)
	}
}

func TestRequireSessionWithoutSession(t *testing.T) {
	a, _ := newTestAuth(t, nil)
	protected := a.RequireSession(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler ran without a session")
	}))

	w := httptest.NewRecorder()
	protected.ServeHTTP(w, httptest.NewRequest("GET", "http://app.test/private", nil))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/signin" {
		t.Fatalf("GET without session: %d → %q, want 303 → /signin", w.Code, w.Header().Get("Location"))
	}

	w = httptest.NewRecorder()
	protected.ServeHTTP(w, httptest.NewRequest("POST", "http://app.test/private", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("POST without session: %d, want 403", w.Code)
	}
}

func TestBeginCSRF(t *testing.T) {
	a, m := newTestAuth(t, nil)
	post := func(mut func(*http.Request)) *httptest.ResponseRecorder {
		form := url.Values{"address": {"p@example.com"}, "force": {"1"}}
		r := httptest.NewRequest("POST", "http://app.test/signin", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		mut(r)
		w := httptest.NewRecorder()
		a.Begin(w, r)
		return w
	}

	if w := post(func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }); w.Code != http.StatusForbidden {
		t.Fatalf("cross-site: %d, want 403", w.Code)
	}
	if w := post(func(r *http.Request) {}); w.Code != http.StatusForbidden {
		t.Fatalf("no origin evidence at all: %d, want 403 (not a browser form post)", w.Code)
	}
	if w := post(func(r *http.Request) { r.Header.Set("Origin", "http://evil.test") }); w.Code != http.StatusForbidden {
		t.Fatalf("foreign Origin: %d, want 403", w.Code)
	}
	m.to = ""
	if w := post(func(r *http.Request) { r.Header.Set("Origin", "http://app.test") }); w.Code != http.StatusSeeOther {
		t.Fatalf("matching Origin: %d, want 303", w.Code)
	}
	if m.to == "" {
		t.Fatal("matching Origin did not reach the flow")
	}
	if w := post(func(r *http.Request) { r.Header.Set("Referer", "http://app.test/signin") }); w.Code != http.StatusSeeOther {
		t.Fatalf("matching Referer: %d, want 303", w.Code)
	}
}

func TestAuthorizeGate(t *testing.T) {
	a, m := newTestAuth(t, func(c *Config) {
		c.Authorize = func(address string) bool { return address == "member@example.com" }
	})

	beginSignin(t, a, "stranger@example.com")
	link := linkRE.FindString(m.body)
	w := httptest.NewRecorder()
	a.Verify(w, httptest.NewRequest("GET", link, nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("unadmitted address: %d, want 403", w.Code)
	}

	beginSignin(t, a, "member@example.com")
	link = linkRE.FindString(m.body)
	w = httptest.NewRecorder()
	a.Verify(w, httptest.NewRequest("GET", link, nil))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" {
		t.Fatalf("admitted address: %d → %q, want a session", w.Code, w.Header().Get("Location"))
	}
}

func TestCallbackWithoutPendingCookie(t *testing.T) {
	a, _ := newTestAuth(t, nil)
	w := httptest.NewRecorder()
	a.Callback(w, httptest.NewRequest("GET", "http://app.test/auth/callback?code=x&state=y", nil))
	if w.Header().Get("Location") != "/signin?err=expired" {
		t.Fatalf("callback without pending: %q, want /signin?err=expired", w.Header().Get("Location"))
	}
}

func TestSecureOriginGetsHostPrefixedCookies(t *testing.T) {
	a, _ := newTestAuth(t, func(c *Config) { c.Origin = "https://app.test" })
	if a.SessionCookie() != "__Host-rastrillo_session" {
		t.Fatalf("SessionCookie = %q, want __Host- prefix on an https origin", a.SessionCookie())
	}
}

func TestLinkStoreSingleUseAndExpiry(t *testing.T) {
	a, _ := newTestAuth(t, nil)
	ls := &linkStore{db: a.cfg.DB}
	ctx := context.Background()

	if err := ls.PutLink(ctx, "h1", "p@x", "signin", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("PutLink: %v", err)
	}
	if addr, ok, _ := ls.TakeLink(ctx, "h1", "signin"); !ok || addr != "p@x" {
		t.Fatalf("TakeLink = %q, %v", addr, ok)
	}
	if _, ok, _ := ls.TakeLink(ctx, "h1", "signin"); ok {
		t.Fatal("second TakeLink succeeded; links must be single-use")
	}

	ls.PutLink(ctx, "h2", "p@x", "signin", time.Now().Add(-time.Minute))
	if _, ok, _ := ls.TakeLink(ctx, "h2", "signin"); ok {
		t.Fatal("expired link consumed as valid")
	}
	if _, ok, _ := ls.TakeLink(ctx, "h2", "signin"); ok {
		t.Fatal("expired row survived its Take")
	}

	ls.PutLink(ctx, "h3", "p@x", "signin", time.Now().Add(time.Hour))
	if _, ok, _ := ls.TakeLink(ctx, "h3", "other-purpose"); ok {
		t.Fatal("link consumed under the wrong purpose")
	}
}

func TestClassifierLookupPathFix(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
	}))
	defer srv.Close()

	c := &http.Client{Transport: lookupPathFix{next: http.DefaultTransport}}
	if _, err := c.Get(srv.URL + "/api/lookup?addr=p%40x"); err != nil {
		t.Fatalf("get: %v", err)
	}
	if gotPath != "/api/federation/lookup" {
		t.Fatalf("probe path = %q, want the rewritten /api/federation/lookup (keymail's real route)", gotPath)
	}
	if gotQuery != "addr=p%40x" {
		t.Fatalf("query = %q, want preserved", gotQuery)
	}

	if _, err := c.Get(srv.URL + "/.well-known/keymail"); err != nil {
		t.Fatalf("get: %v", err)
	}
	if gotPath != "/.well-known/keymail" {
		t.Fatalf("well-known path = %q, must pass through untouched", gotPath)
	}
}

// legacyMigrations is the pre-sessions-core schema — just auth's own
// two tables, as a pre-upgrade deployment would have had.
var legacyMigrations = []string{
	`CREATE TABLE IF NOT EXISTS auth_links (
	  hash       TEXT PRIMARY KEY,
	  address    TEXT NOT NULL,
	  purpose    TEXT NOT NULL,
	  expires_at TEXT NOT NULL
	);`,
	`CREATE TABLE IF NOT EXISTS auth_sessions (
	  token_hash TEXT PRIMARY KEY,
	  address    TEXT NOT NULL,
	  method     TEXT NOT NULL,
	  auth_time  TEXT NOT NULL DEFAULT '',
	  created_at TEXT NOT NULL,
	  expires_at TEXT NOT NULL
	);`,
}

// TestUpgradeCopiesLiveAuthSessionsThenSelfHeals pins the upgrade path
// end to end: a session minted under the pre-sessions-core schema still
// admits after the app upgrades to auth.Migrations (the copy), and —
// the resurrection fix — a session revoked post-upgrade stays revoked
// even after the migrations run again on a later boot (auth_sessions
// having been emptied means there is nothing left to re-copy).
func TestUpgradeCopiesLiveAuthSessionsThenSelfHeals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.db")
	db, err := rastrillo.OpenDB(path, legacyMigrations)
	if err != nil {
		t.Fatalf("OpenDB (legacy): %v", err)
	}

	token, hash, err := NewToken()
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}
	now := time.Now().UTC()
	if _, err := db.Exec(`INSERT INTO auth_sessions (token_hash, address, method, auth_time, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		hash, "old@example.com", "magiclink", "",
		now.Format(time.RFC3339), now.Add(time.Hour).Format(time.RFC3339)); err != nil {
		t.Fatalf("seed auth_sessions: %v", err)
	}
	db.Close()

	// Re-open with the real Migrations — the upgrade boot.
	db, err = rastrillo.OpenDB(path, Migrations)
	if err != nil {
		t.Fatalf("OpenDB (upgrade): %v", err)
	}
	defer db.Close()

	a, err := New(Config{DB: db, Origin: "http://app.test", InstanceKey: "k", Mailer: &captureMailer{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	cookie := &http.Cookie{Name: a.SessionCookie(), Value: token}
	r := httptest.NewRequest("GET", "http://app.test/", nil)
	r.AddCookie(cookie)
	if id, ok := a.SessionFrom(r); !ok || id.Address != "old@example.com" {
		t.Fatalf("SessionFrom after upgrade = %+v, ok=%v, want the copied pre-upgrade session admitted", id, ok)
	}

	// Sign out — revokes the copied row in `sessions`.
	signout := httptest.NewRequest("POST", "http://app.test/signout", nil)
	signout.Header.Set("Sec-Fetch-Site", "same-origin")
	signout.AddCookie(cookie)
	a.Signout(httptest.NewRecorder(), signout)

	// Simulate a restart: re-run the migration statements again.
	// Without emptying auth_sessions, the still-populated row would be
	// re-copied by INSERT OR IGNORE (its PK now absent from `sessions`
	// post-signout) and the revoked token would work again.
	for _, stmt := range Migrations {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("re-run migration %q: %v", stmt, err)
		}
	}

	r2 := httptest.NewRequest("GET", "http://app.test/", nil)
	r2.AddCookie(cookie)
	if _, ok := a.SessionFrom(r2); ok {
		t.Fatal("SessionFrom admitted a revoked session resurrected by re-running the migrations")
	}
}

func TestSweep(t *testing.T) {
	a, m := newTestAuth(t, nil)
	ls := &linkStore{db: a.cfg.DB}
	ls.PutLink(context.Background(), "old", "p@x", "signin", time.Now().Add(-time.Hour))
	ls.PutLink(context.Background(), "new", "p@x", "signin", time.Now().Add(time.Hour))
	if err := a.Sweep(time.Now()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	var n int
	a.cfg.DB.QueryRow(`SELECT COUNT(*) FROM auth_links`).Scan(&n)
	if n != 1 {
		t.Fatalf("links after sweep = %d, want 1 (the unexpired one)", n)
	}
	_ = m
}
