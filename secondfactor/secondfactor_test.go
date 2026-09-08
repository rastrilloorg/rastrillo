package secondfactor_test

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/passkey"
	"amadan.net/rastrillo/rastrillo/secondfactor"
	"amadan.net/rastrillo/rastrillo/sessions"
)

const testOrigin = "http://app.test"

// factor is a second factor that holds no proof of its own: it says
// who is enrolled, and a test completes the half-session by hand the
// way a real factor would after verifying.
type factor struct{ enrolled map[string]bool }

func (f factor) Enrolled(subject string) (bool, error) { return f.enrolled[subject], nil }

type env struct {
	g    *secondfactor.Gate
	f    factor
	sess *sessions.Sessions
	db   *sql.DB
}

func newEnv(t *testing.T) env {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "s.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := migrate.Apply(context.Background(), d, migrate.Merge(sessions.Schema, secondfactor.Schema)); err != nil {
		t.Fatalf("migrate.Apply: %v", err)
	}
	sqlDB := d.Writer()
	sess, err := sessions.New(sessions.Config{DB: sqlDB, Origin: testOrigin})
	if err != nil {
		t.Fatal(err)
	}
	g, err := secondfactor.New(secondfactor.Config{Sessions: sess, DB: sqlDB, Origin: testOrigin})
	if err != nil {
		t.Fatal(err)
	}
	f := factor{enrolled: map[string]bool{}}
	g.Add(f)
	return env{g: g, f: f, sess: sess, db: sqlDB}
}

// hold invokes Hold as an identity plugin would, returning the
// pending cookie when the gate took over.
func hold(t *testing.T, e env, sess sessions.Session, returnTo string) (*httptest.ResponseRecorder, *http.Cookie) {
	t.Helper()
	form := url.Values{}
	if returnTo != "" {
		form.Set("return_to", returnTo)
	}
	r := httptest.NewRequest("POST", testOrigin+"/signin", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	done, err := e.g.Hold(w, r, sess)
	if err != nil {
		t.Fatalf("Hold: %v", err)
	}
	if !done {
		return w, nil
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "rastrillo_secondfactor" && c.Value != "" {
			return w, c
		}
	}
	t.Fatal("Hold reported done but set no pending cookie")
	return nil, nil
}

func withPending(pending *http.Cookie) *http.Request {
	r := httptest.NewRequest("POST", testOrigin+"/signin/confirm", nil)
	if pending != nil {
		r.AddCookie(pending)
	}
	return r
}

func sessionFrom(t *testing.T, e env, w *httptest.ResponseRecorder) (sessions.Session, bool) {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == e.sess.CookieName() && c.Value != "" {
			r := httptest.NewRequest("GET", testOrigin+"/", nil)
			r.AddCookie(c)
			return e.sess.From(r)
		}
	}
	return sessions.Session{}, false
}

func TestHoldPassesThroughWhenNothingEnrolled(t *testing.T) {
	e := newEnv(t)
	w, pending := hold(t, e, sessions.Session{Subject: "42", Method: "password"}, "")
	if pending != nil || w.Code == http.StatusSeeOther {
		t.Fatal("Hold took over for a subject with no factor; the plugin should have signed in as usual")
	}
}

func TestHoldThenCompleteMintsBothFactorSession(t *testing.T) {
	e := newEnv(t)
	e.f.enrolled["person@example.com"] = true

	w, pending := hold(t, e, sessions.Session{Subject: "person@example.com", Method: "magiclink"}, "/notes/7")
	if pending == nil {
		t.Fatal("Hold did not take over for an enrolled subject")
	}
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/signin/confirm" {
		t.Fatalf("Hold: %d -> %q, want 303 -> /signin/confirm", w.Code, w.Header().Get("Location"))
	}
	if len(w.Result().Cookies()) != 1 {
		t.Fatalf("Hold set %d cookies, want only the pending one", len(w.Result().Cookies()))
	}

	p, ok := e.g.Pending(withPending(pending))
	if !ok || p.Subject != "person@example.com" || p.Method != "magiclink" || p.ReturnTo != "/notes/7" {
		t.Fatalf("Pending = %+v, %v", p, ok)
	}

	fin := httptest.NewRecorder()
	if err := e.g.Complete(fin, withPending(pending), p, "totp"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	s, ok := sessionFrom(t, e, fin)
	if !ok || s.Method != "magiclink+totp" || s.AuthTime.IsZero() {
		t.Fatalf("minted session = %+v, %v; want Method magiclink+totp with an AuthTime", s, ok)
	}
	var cleared bool
	for _, c := range fin.Result().Cookies() {
		if c.Name == "rastrillo_secondfactor" && c.MaxAge == -1 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("Complete did not clear the pending cookie")
	}

	// Single use: the same half-session neither resolves nor completes again.
	if _, ok := e.g.Pending(withPending(pending)); ok {
		t.Fatal("a completed half-session still resolves")
	}
	if err := e.g.Complete(httptest.NewRecorder(), withPending(pending), p, "totp"); !errors.Is(err, secondfactor.ErrConsumed) {
		t.Fatalf("second Complete: %v, want ErrConsumed", err)
	}
}

func TestPendingExpires(t *testing.T) {
	e := newEnv(t)
	e.f.enrolled["42"] = true
	_, pending := hold(t, e, sessions.Session{Subject: "42", Method: "password"}, "")
	if _, err := e.db.Exec(`UPDATE secondfactor_pending SET expires_at = ?`,
		time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.g.Pending(withPending(pending)); ok {
		t.Fatal("an expired half-session still resolves")
	}
	if _, ok := e.g.Pending(withPending(nil)); ok {
		t.Fatal("no cookie resolved to a half-session")
	}
}

func TestStrikeBudgetEndsTheHalfSession(t *testing.T) {
	e := newEnv(t)
	e.f.enrolled["42"] = true
	_, pending := hold(t, e, sessions.Session{Subject: "42", Method: "password"}, "")
	p, _ := e.g.Pending(withPending(pending))

	for i := 1; i < 5; i++ {
		if err := e.g.Strike(httptest.NewRecorder(), p); err != nil {
			t.Fatalf("strike %d: %v, want nil", i, err)
		}
		if _, ok := e.g.Pending(withPending(pending)); !ok {
			t.Fatalf("half-session gone after %d strikes", i)
		}
	}
	w := httptest.NewRecorder()
	if err := e.g.Strike(w, p); !errors.Is(err, secondfactor.ErrExhausted) {
		t.Fatalf("fifth strike: %v, want ErrExhausted", err)
	}
	if _, ok := e.g.Pending(withPending(pending)); ok {
		t.Fatal("an exhausted half-session still resolves")
	}
	var cleared bool
	for _, c := range w.Result().Cookies() {
		if c.Name == "rastrillo_secondfactor" && c.MaxAge == -1 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("exhaustion did not clear the pending cookie")
	}
	if err := e.g.Complete(httptest.NewRecorder(), withPending(pending), p, "totp"); !errors.Is(err, secondfactor.ErrConsumed) {
		t.Fatalf("Complete after exhaustion: %v, want ErrConsumed", err)
	}
}

func TestEnrolledIsAnyFactor(t *testing.T) {
	e := newEnv(t)
	other := factor{enrolled: map[string]bool{"bob": true}}
	e.g.Add(other)
	e.f.enrolled["alice"] = true
	for subject, want := range map[string]bool{"alice": true, "bob": true, "carol": false} {
		if got, err := e.g.Enrolled(subject); err != nil || got != want {
			t.Errorf("Enrolled(%s) = %v, %v; want %v", subject, got, err, want)
		}
	}
}

func TestNewValidatesConfig(t *testing.T) {
	e := newEnv(t)
	for name, cfg := range map[string]secondfactor.Config{
		"no sessions": {DB: e.db, Origin: testOrigin},
		"no db":       {Sessions: e.sess, Origin: testOrigin},
		"bad origin":  {Sessions: e.sess, DB: e.db, Origin: "app.test"},
	} {
		if _, err := secondfactor.New(cfg); err == nil {
			t.Errorf("%s: New accepted a bad config", name)
		}
	}
	g, err := secondfactor.New(secondfactor.Config{Sessions: e.sess, DB: e.db, Origin: "https://app.test", ConfirmPath: "/confirm"})
	if err != nil || g.ConfirmPath() != "/confirm" {
		t.Fatalf("New = %v, %v", g, err)
	}
}

func TestSweepClearsExpiredPending(t *testing.T) {
	e := newEnv(t)
	stamp := func(d time.Duration) string { return time.Now().Add(d).UTC().Format(time.RFC3339) }
	for hash, exp := range map[string]string{"dead": stamp(-time.Minute), "live": stamp(time.Minute)} {
		if _, err := e.db.Exec(
			`INSERT INTO secondfactor_pending (token_hash, subject, expires_at) VALUES (?, 's', ?)`,
			hash, exp); err != nil {
			t.Fatal(err)
		}
	}
	if err := secondfactor.Sweep(e.db, time.Now()); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM secondfactor_pending`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("after Sweep: %d pending rows, want the 1 live one", n)
	}
}

// The adoption migration carries recovery codes out of the tables the
// passkey package owned before this package existed, and drops them.
// A database that already ran passkey's first migration is the case
// that matters: those codes are on paper in someone's drawer.
func TestSchemaAdoptsPasskeyRecoveryCodes(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "old.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := migrate.Apply(context.Background(), d, migrate.Merge(sessions.Schema, passkey.Schema)); err != nil {
		t.Fatal(err)
	}
	if err := d.G.Exec(`INSERT INTO passkey_recovery_codes (code_hash, subject, created_at) VALUES ('h1', 'alice', 'now'), ('h2', 'alice', 'now')`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.Apply(context.Background(), d, migrate.Merge(sessions.Schema, passkey.Schema, secondfactor.Schema)); err != nil {
		t.Fatalf("second boot with secondfactor: %v", err)
	}
	var n int
	if err := d.G.Raw(`SELECT COUNT(*) FROM secondfactor_recovery_codes WHERE subject = 'alice'`).Scan(&n).Error; err != nil || n != 2 {
		t.Fatalf("adopted codes = %d, %v; want 2", n, err)
	}
	for _, table := range []string{"passkey_recovery_codes", "passkey_pending"} {
		if err := d.G.Raw(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&n).Error; err != nil || n != 0 {
			t.Errorf("%s still exists after adoption", table)
		}
	}
	// Idempotent: a third boot applies nothing and fails nothing.
	r, err := migrate.Apply(context.Background(), d, migrate.Merge(sessions.Schema, passkey.Schema, secondfactor.Schema))
	if err != nil || len(r.Applied) != 0 {
		t.Fatalf("third boot: %+v, %v", r, err)
	}
}
