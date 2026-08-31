package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/keymaildev/signin"

	"github.com/carlosframework/rastrillo/db"
	"github.com/carlosframework/rastrillo/migrate"
	"github.com/carlosframework/rastrillo/sessions"
)

// captureMailer records the last message instead of sending it.
type captureMailer struct{ to, subject, body string }

func (m *captureMailer) Send(_ context.Context, to, subject, body string) error {
	m.to, m.subject, m.body = to, subject, body
	return nil
}

func newTestAuth(t *testing.T, mut func(*Config)) (*Auth, *captureMailer) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "auth.db"), nil)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := migrate.Apply(context.Background(), d, migrate.Merge(sessions.Schema, Schema)); err != nil {
		t.Fatalf("migrate.Apply: %v", err)
	}
	m := &captureMailer{}
	cfg := Config{
		DB:          d.Writer(),
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
	d, err := db.Open(filepath.Join(t.TempDir(), "v.db"), nil)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()
	if _, err := migrate.Apply(context.Background(), d, migrate.Merge(sessions.Schema, Schema)); err != nil {
		t.Fatalf("migrate.Apply: %v", err)
	}
	sqlDB := d.Writer()
	cases := []struct {
		name string
		cfg  Config
	}{
		{"empty origin", Config{DB: sqlDB, InstanceKey: "k"}},
		{"relative origin", Config{DB: sqlDB, InstanceKey: "k", Origin: "app.test"}},
		{"empty instance key", Config{DB: sqlDB, Origin: "https://app.test"}},
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

	// RequireSession admits the cookie and exposes the identity — to
	// From AND to sessions.Current (the WithSession stash), so code
	// that reads identity through the sessions package alone (generated
	// scoped actions) agrees with From about who is signed in.
	var got Identity
	var gotSess sessions.Session
	var gotSessOK bool
	protected := a.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = From(r)
		gotSess, gotSessOK = sessions.Current(r)
	}))
	r = httptest.NewRequest("GET", "http://app.test/private", nil)
	r.AddCookie(session)
	protected.ServeHTTP(httptest.NewRecorder(), r)
	if got.Address != "person@example.com" || got.Method != "magiclink" {
		t.Fatalf("From = %+v", got)
	}
	if !gotSessOK || gotSess.Subject != "person@example.com" {
		t.Fatalf("sessions.Current behind RequireSession = %+v, %v; want the same subject From sees", gotSess, gotSessOK)
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

// TestVerifySecondFactorIntercepts pins the 2FA seam on the
// magic-link path: a SecondFactor hook that reports done gets the
// response — no session cookie, no SignedInPath redirect — and sees
// the exact session admit would have minted. (passkey.Handlers.Gate
// is the shipped implementation; a recorder stands in here, since the
// seam — not the ceremony — is this plugin's contract.)
func TestVerifySecondFactorIntercepts(t *testing.T) {
	var saw sessions.Session
	a, m := newTestAuth(t, func(cfg *Config) {
		cfg.SecondFactor = func(w http.ResponseWriter, r *http.Request, sess sessions.Session) (bool, error) {
			saw = sess
			http.Redirect(w, r, "/passkey/confirm", http.StatusSeeOther)
			return true, nil
		}
	})

	beginSignin(t, a, "person@example.com")
	link := linkRE.FindString(m.body)
	if link == "" {
		t.Fatalf("no verify link in mail body:\n%s", m.body)
	}
	w := httptest.NewRecorder()
	a.Verify(w, httptest.NewRequest("GET", link, nil))

	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/passkey/confirm" {
		t.Fatalf("gated verify: %d -> %q, want 303 -> /passkey/confirm", w.Code, w.Header().Get("Location"))
	}
	if saw.Subject != "person@example.com" || saw.Method != "magiclink" {
		t.Errorf("hook saw %+v, want the would-be magiclink session", saw)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == a.SessionCookie() && c.Value != "" {
			t.Fatal("gated verify minted a session cookie anyway")
		}
	}
}

func TestRequireFreshSessionStepUp(t *testing.T) {
	a, m := newTestAuth(t, nil)
	beginSignin(t, a, "person@example.com")
	link := linkRE.FindString(m.body)
	w := httptest.NewRecorder()
	a.Verify(w, httptest.NewRequest("GET", link, nil))
	var session *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == a.SessionCookie() && c.Value != "" {
			session = c
		}
	}
	if session == nil {
		t.Fatal("no session cookie")
	}

	drive := func(method string) (*httptest.ResponseRecorder, bool) {
		var admitted bool
		h := a.RequireFreshSession(5 * time.Minute)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			admitted = true
		}))
		r := httptest.NewRequest(method, "http://app.test/settings", nil)
		r.AddCookie(session)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec, admitted
	}

	// A magic-link session has no auth_time; its minting moment stands
	// in, and it was minted just now.
	if _, admitted := drive("GET"); !admitted {
		t.Fatal("just-verified session refused by a 5m freshness gate")
	}

	// Backdate the row: the same session goes stale and step-up kicks in.
	old := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	if _, err := a.cfg.DB.Exec(`UPDATE sessions SET created_at = ?`, old); err != nil {
		t.Fatal(err)
	}
	rec, admitted := drive("GET")
	if admitted {
		t.Fatal("hour-old session admitted past a 5m gate")
	}
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/signin?reauth=1" {
		t.Fatalf("stale GET: %d → %q, want 303 → /signin?reauth=1", rec.Code, rec.Header().Get("Location"))
	}
	if rec, _ := drive("POST"); rec.Code != http.StatusForbidden {
		t.Fatalf("stale POST: %d, want 403", rec.Code)
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

// TestClassifierProbesFederationLookup pins the reason the local
// RoundTripper workaround could be deleted: signin v0.1.1's stock
// classifier probes keymail's real /api/federation/lookup route. If a
// signin upgrade ever regresses to the old /api/lookup, this catches
// it here rather than in production (where classification fails open
// and the miss is silent).
func TestClassifierProbesFederationLookup(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/.well-known/keymail" {
			w.Write([]byte(`{"version":"1","host":"x"}`))
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	c := &signin.Classifier{
		Scheme:    "http",
		LookupTXT: func(context.Context, string) ([]string, error) { return []string{"v=1 host=" + host}, nil },
	}
	got := c.Classify(context.Background(), "p@"+host)
	if !got.Keymail {
		t.Fatalf("Classify = %+v, want Keymail=true from a 200 probe", got)
	}
	want := []string{"/.well-known/keymail", "/api/federation/lookup"}
	if !slices.Equal(paths, want) {
		t.Fatalf("probe paths = %v, want %v", paths, want)
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

// TestUpgradeFromLegacyAuthSessionsRefusesAutomaticAdoption covers the
// pre-sessions-core upgrade: a database with rows in auth_sessions and
// no sessions table. Automatic Apply must refuse it, not silently
// backfill it.
//
// This is NOT a transcription slip in auth's SQL: adopt() (migrate
// design spec §7) is deliberately all-or-nothing — a non-empty,
// ledger-less database either matches the full replayed set exactly
// (stamp everything, run zero DDL) or refuses to boot. A pre-
// sessions-core database is missing the `sessions` table, so it can
// never structurally match a merged (sessions.Schema, auth.Schema)
// set, and automatic Apply correctly refuses it rather than guessing.
// This is also why the backfill cannot run via adoption even when the
// database DOES structurally match (a crash between the old
// Migrations' CREATE TABLE and its backfill, say): adopt() stamps
// 0002 as applied without executing it, same as every other migration
// in a matching set. The documented recovery (spec §7's escape hatch)
// is operator-driven: create the missing table for real, then
// `rastrillo migration baseline --through <last-non-backfill-id>` so
// the next boot's normal Apply loop actually runs 0002.
func TestUpgradeFromLegacyAuthSessionsRefusesAutomaticAdoption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	d, err := db.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, stmt := range legacyMigrations {
		if err := d.G.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := d.G.Exec(`INSERT INTO auth_sessions
	  (token_hash, address, method, auth_time, created_at, expires_at)
	  VALUES ('h1','a@example.com','link','','now','2099-01-01T00:00:00Z')`).Error; err != nil {
		t.Fatal(err)
	}

	full := migrate.Merge(sessions.Schema, Schema)
	_, err = migrate.Apply(context.Background(), d, full)
	if err == nil {
		t.Fatal("Apply admitted a pre-sessions-core database with no `sessions` table; " +
			"adoption is structural-match-or-refuse, so this must fail loudly, not guess")
	}
	if !strings.Contains(err.Error(), "missing table sessions") {
		t.Fatalf("error = %v, want it to name the missing sessions table", err)
	}
}

// TestBackfillRunsOnceViaOperatorBaseline exercises the documented
// recovery path from the test above: an operator brings the database's
// structure in line with the full migration set by hand (the missing
// `sessions` table), then baselines the ledger through auth's own
// schema migration — leaving the backfill (0002) unstamped so the next
// Apply actually runs it. That run must happen exactly once: a session
// revoked after the backfill must not be resurrected by a later boot.
func TestBackfillRunsOnceViaOperatorBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recovered.db")
	d, err := db.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, stmt := range legacyMigrations {
		if err := d.G.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := d.G.Exec(`INSERT INTO auth_sessions
	  (token_hash, address, method, auth_time, created_at, expires_at)
	  VALUES ('h1','a@example.com','link','','now','2099-01-01T00:00:00Z')`).Error; err != nil {
		t.Fatal(err)
	}

	// The operator's manual half: create the table migrate's own
	// sessions/0001_init would have, then baseline through auth's own
	// schema migration (not its backfill).
	for _, m := range sessions.Schema.All() {
		if err := d.G.Exec(m.SQL).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := d.G.Exec(migrate.LedgerDDL).Error; err != nil {
		t.Fatal(err)
	}
	full := migrate.Merge(sessions.Schema, Schema)
	ctx := context.Background()
	conn, err := d.Writer().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrate.Stamp(ctx, conn, full.All(), "auth/0001_init"); err != nil {
		t.Fatal(err)
	}
	conn.Close()

	// The ledger now has everything through auth/0001_init stamped;
	// 0002 is not. This boot must run it for real.
	if _, err := migrate.Apply(ctx, d, full); err != nil {
		t.Fatal(err)
	}
	var n int64
	d.G.Raw("SELECT count(*) FROM sessions WHERE token_hash = 'h1'").Scan(&n)
	if n != 1 {
		t.Fatalf("backfilled sessions rows = %d, want 1", n)
	}

	// Revoke, then boot again. The backfill must not resurrect it.
	if err := d.G.Exec("DELETE FROM sessions WHERE token_hash = 'h1'").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.Apply(ctx, d, full); err != nil {
		t.Fatal(err)
	}
	d.G.Raw("SELECT count(*) FROM sessions WHERE token_hash = 'h1'").Scan(&n)
	if n != 0 {
		t.Fatal("a second boot resurrected a revoked session — the backfill re-ran")
	}
}

// TestAuthSchemaDoesNotEmbedSessions guards the requirement moving
// from a prose comment into the type system: auth.Schema must not
// contain the sessions table, so ordering is expressed at the call
// site via migrate.Merge(sessions.Schema, auth.Schema).
func TestAuthSchemaDoesNotEmbedSessions(t *testing.T) {
	for _, m := range Schema.All() {
		if strings.Contains(m.SQL, "CREATE TABLE IF NOT EXISTS sessions") {
			t.Fatalf("%s embeds the sessions table; callers must Merge(sessions.Schema, auth.Schema)", m.ID)
		}
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

// TestRecoveryStampsBeforeCreatingTheTable pins the order of steps 2
// and 3 in store.go's recovery procedure, and the reason for it.
//
// Creating the sessions table first leaves the database structurally
// matching the full set with an empty ledger — exactly the state
// adopt() stamps wholesale. On a hibernating platform a single inbound
// request in that window adopts, records auth/0002 as applied without
// running it, and strands every auth_sessions row. Stamping first
// makes the ledger non-empty, so adoption can never fire; a wake
// before the table exists then fails loudly instead, and the backfill
// still runs once the operator finishes.
func TestRecoveryStampsBeforeCreatingTheTable(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "recovered.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, stmt := range legacyMigrations {
		if err := d.G.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := d.G.Exec(`INSERT INTO auth_sessions
	  (token_hash, address, method, auth_time, created_at, expires_at)
	  VALUES ('h1','a@example.com','link','','now','2099-01-01T00:00:00Z')`).Error; err != nil {
		t.Fatal(err)
	}

	full := migrate.Merge(sessions.Schema, Schema)
	ctx := context.Background()

	// Step 2, before the table exists: what `rastrillo migration
	// baseline --db <path> --through sessions/0001_init` does.
	if err := d.G.Exec(migrate.LedgerDDL).Error; err != nil {
		t.Fatal(err)
	}
	conn, err := d.Writer().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrate.Stamp(ctx, conn, full.All(), "sessions/0001_init"); err != nil {
		t.Fatalf("Stamp runs no DDL, so it must not need the sessions table: %v", err)
	}
	conn.Close()

	// A wake in the window between steps 2 and 3. It must fail —
	// loudly — and it must not record the backfill.
	if _, err := migrate.Apply(ctx, d, full); err == nil {
		t.Fatal("a wake before the sessions table exists must refuse, not adopt")
	}
	var stamped int64
	d.G.Raw("SELECT count(*) FROM rastrillo_migrations WHERE id = 'auth/0002_sessions_backfill'").Scan(&stamped)
	if stamped != 0 {
		t.Fatal("auth/0002 was recorded without running: the backfill is stranded and every user is signed out")
	}

	// Step 3, then step 4.
	for _, m := range sessions.Schema.All() {
		if err := d.G.Exec(m.SQL).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := migrate.Apply(ctx, d, full); err != nil {
		t.Fatal(err)
	}
	var n int64
	d.G.Raw("SELECT count(*) FROM sessions WHERE token_hash = 'h1'").Scan(&n)
	if n != 1 {
		t.Fatalf("backfilled sessions rows = %d, want 1 — the recovery must still sign nobody out", n)
	}
}

// TestCreatingTheTableFirstStrandsTheBackfill is the evidence for the
// order pinned above: it performs step 3 before step 2 and shows the
// harm. With the table created and the ledger still empty the database
// structurally matches the full set, so Apply adopts — recording
// auth/0002 as applied without running it. The auth_sessions rows are
// then stranded permanently and every signed-in user is signed out.
//
// It asserts adoption's own documented behaviour (all-or-nothing
// stamping), not a bug. If adoption ever learns to run data migrations
// this test is the one that should fail, and store.go's step order can
// be revisited then.
func TestCreatingTheTableFirstStrandsTheBackfill(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "wrong-order.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, stmt := range legacyMigrations {
		if err := d.G.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := d.G.Exec(`INSERT INTO auth_sessions
	  (token_hash, address, method, auth_time, created_at, expires_at)
	  VALUES ('h1','a@example.com','link','','now','2099-01-01T00:00:00Z')`).Error; err != nil {
		t.Fatal(err)
	}
	for _, m := range sessions.Schema.All() {
		if err := d.G.Exec(m.SQL).Error; err != nil {
			t.Fatal(err)
		}
	}

	// The wake the operator does not control, landing before baseline.
	r, err := migrate.Apply(context.Background(), d, migrate.Merge(sessions.Schema, Schema))
	if err != nil {
		t.Fatalf("the database matches the set, so this wake adopts rather than refusing: %v", err)
	}
	if !r.Adopted {
		t.Fatalf("Result = %+v, want Adopted — that is the window the recovery order closes", r)
	}
	var stamped int64
	d.G.Raw("SELECT count(*) FROM rastrillo_migrations WHERE id = 'auth/0002_sessions_backfill'").Scan(&stamped)
	if stamped != 1 {
		t.Fatal("expected adoption to stamp the backfill without running it")
	}
	var n int64
	d.G.Raw("SELECT count(*) FROM sessions WHERE token_hash = 'h1'").Scan(&n)
	if n != 0 {
		t.Fatal("expected the backfill never to have run — this is the stranding the order exists to prevent")
	}
}

// SubjectFor is the server-blind seam: an app that must not store
// readable addresses at rest maps the verified address to an opaque
// person ref, and that ref — not the address — is what reaches the
// sessions table and everything keyed off it (passkey credentials,
// challenges, recovery codes). Admission still sees the real address:
// Authorize answers a question about an address, and remapping it
// there would break every membership check written against one.
func TestSubjectForRemapsTheStoredSubject(t *testing.T) {
	var authorized string
	var gated sessions.Session
	a, m := newTestAuth(t, func(cfg *Config) {
		cfg.SubjectFor = func(address string) (string, error) {
			if address != "person@example.com" {
				t.Errorf("SubjectFor got %q, want the verified address", address)
			}
			return "ref_7f3a", nil
		}
		cfg.Authorize = func(address string) bool {
			authorized = address
			return true
		}
		cfg.SecondFactor = func(w http.ResponseWriter, r *http.Request, sess sessions.Session) (bool, error) {
			gated = sess
			return false, nil
		}
	})

	beginSignin(t, a, "person@example.com")
	link := linkRE.FindString(m.body)
	if link == "" {
		t.Fatalf("no verify link in mail body:\n%s", m.body)
	}
	w := httptest.NewRecorder()
	a.Verify(w, httptest.NewRequest("GET", link, nil))

	if authorized != "person@example.com" {
		t.Errorf("Authorize saw %q, want the address, not the remapped subject", authorized)
	}
	// The hook runs before the second factor, so a 2FA implementation
	// stores its pending half-session under the same subject the real
	// session will carry.
	if gated.Subject != "ref_7f3a" {
		t.Errorf("SecondFactor saw subject %q, want the remapped ref", gated.Subject)
	}

	var session *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == a.SessionCookie() {
			session = c
		}
	}
	if session == nil {
		t.Fatal("no session cookie after verify")
	}

	// The point of the exercise: the address is not in the sessions
	// table. An app whose gate greps the raw database file needs this
	// to be true of the bytes, not just of the API.
	var subject string
	if err := a.cfg.DB.QueryRow(`SELECT subject FROM sessions`).Scan(&subject); err != nil {
		t.Fatalf("read back the session row: %v", err)
	}
	if subject != "ref_7f3a" {
		t.Errorf("sessions.subject = %q, want the remapped ref", subject)
	}

	// And what the app reads back is the ref too — Identity.Address
	// carries whatever SubjectFor returned.
	var got Identity
	protected := a.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = From(r)
	}))
	r := httptest.NewRequest("GET", "http://app.test/private", nil)
	r.AddCookie(session)
	protected.ServeHTTP(httptest.NewRecorder(), r)
	if got.Address != "ref_7f3a" {
		t.Errorf("From().Address = %q, want the remapped ref", got.Address)
	}
}

// A SubjectFor that fails must not fall back to the address — a
// server-blind app would rather refuse the sign-in than write one.
func TestSubjectForErrorRefusesSignin(t *testing.T) {
	a, m := newTestAuth(t, func(cfg *Config) {
		cfg.SubjectFor = func(string) (string, error) {
			return "", errors.New("person store unreachable")
		}
	})

	beginSignin(t, a, "person@example.com")
	link := linkRE.FindString(m.body)
	w := httptest.NewRecorder()
	a.Verify(w, httptest.NewRequest("GET", link, nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("verify with a failing SubjectFor: %d, want 500", w.Code)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == a.SessionCookie() && c.Value != "" {
			t.Fatal("minted a session cookie despite the failing hook")
		}
	}
	var n int
	if err := a.cfg.DB.QueryRow(`SELECT count(*) FROM sessions`).Scan(&n); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if n != 0 {
		t.Errorf("sessions rows = %d, want 0 — no session may exist without a subject", n)
	}
}
