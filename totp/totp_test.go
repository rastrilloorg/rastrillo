package totp_test

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

	"amadan.net/rastrillo/rastrillo/crypto"
	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/secondfactor"
	"amadan.net/rastrillo/rastrillo/sessions"
	"amadan.net/rastrillo/rastrillo/totp"
)

const testOrigin = "http://app.test"

// period is one RFC 6238 step; a test wanting a code the person has
// not yet spent asks for the next step, which the ±1 window accepts.
const period = 30 * time.Second

type env struct {
	h    *totp.Handlers
	g    *secondfactor.Gate
	sess *sessions.Sessions
	db   *sql.DB
}

func newEnv(t *testing.T) env {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := migrate.Apply(context.Background(), d, migrate.Merge(sessions.Schema, secondfactor.Schema, totp.Schema)); err != nil {
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
	h, err := totp.New(totp.Config{DB: sqlDB, Sessions: sess, Gate: g, Key: crypto.Derive([]byte("instance"), "rastrillo/totp"), Issuer: "Acme"})
	if err != nil {
		t.Fatal(err)
	}
	g.Add(h)
	return env{h: h, g: g, sess: sess, db: sqlDB}
}

// enrol walks the set-up: Begin, then Confirm with the code the
// authenticator would show now. Returns the key the phone holds.
func enrol(t *testing.T, e env, subject string) string {
	t.Helper()
	en, err := e.h.Begin(subject, subject)
	if err != nil {
		t.Fatal(err)
	}
	c, err := totp.Code(en.Key, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := e.h.Confirm(subject, c); err != nil || !ok {
		t.Fatalf("Confirm with the phone's code: %v, %v", ok, err)
	}
	return en.Key
}

func hold(t *testing.T, e env, sess sessions.Session, returnTo string) *http.Cookie {
	t.Helper()
	form := url.Values{}
	if returnTo != "" {
		form.Set("return_to", returnTo)
	}
	r := httptest.NewRequest("POST", testOrigin+"/signin", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	done, err := e.g.Hold(w, r, sess)
	if err != nil || !done {
		t.Fatalf("Hold = %v, %v; want done", done, err)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "rastrillo_secondfactor" && c.Value != "" {
			return c
		}
	}
	t.Fatal("no pending cookie")
	return nil
}

func postForm(t *testing.T, h http.HandlerFunc, cookie *http.Cookie, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", testOrigin+"/totp", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h(w, r)
	return w
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

// signIn mints a session whose credential verified at authTime.
func signIn(t *testing.T, e env, subject string, authTime time.Time) *http.Cookie {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", testOrigin+"/signin", nil)
	if err := e.sess.SignIn(w, r, sessions.Session{Subject: subject, Method: "password", AuthTime: authTime}); err != nil {
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

// RFC 6238 Appendix B, SHA-1, the ASCII secret "12345678901234567890":
// the eight-digit vectors, of which a six-digit code is the tail.
func TestCodeMatchesRFC6238Vectors(t *testing.T) {
	key := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	for at, want := range map[int64]string{
		59: "287082", 1111111109: "081804", 1111111111: "050471",
		1234567890: "005924", 2000000000: "279037", 20000000000: "353130",
	} {
		got, err := totp.Code(key, time.Unix(at, 0))
		if err != nil || got != want {
			t.Errorf("Code at %d = %q, %v; want %q", at, got, err, want)
		}
	}
	if _, err := totp.Code("not base32!", time.Now()); err == nil {
		t.Error("Code accepted a key that is not base32")
	}
}

func TestEnrolmentIsLiveOnlyOnceConfirmed(t *testing.T) {
	e := newEnv(t)
	en, err := e.h.Begin("alice", "alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(en.Key) != 32 || !strings.HasPrefix(en.URI, "otpauth://totp/Acme:alice@example.com?") ||
		!strings.Contains(en.URI, "secret="+en.Key) || !strings.Contains(en.URI, "issuer=Acme") {
		t.Fatalf("Enrolment = %+v", en)
	}
	if !strings.HasPrefix(en.QR, "<svg ") || !strings.Contains(en.QR, "<path ") || strings.Contains(en.QR, en.Key) {
		t.Fatalf("QR is not an inline SVG of the URI: %.80s", en.QR)
	}
	if ok, _ := e.h.Enrolled("alice"); ok {
		t.Fatal("enrolled before any code confirmed the scan")
	}
	// A refresh of the set-up page sees the same enrolment again.
	if again, ok, err := e.h.Pending("alice", "alice@example.com"); err != nil || !ok || again.Key != en.Key || again.QR != en.QR {
		t.Fatalf("Pending = %v, %v, %v; want the enrolment Begin minted", again.Key == en.Key, ok, err)
	}
	if _, ok, _ := e.h.Pending("nobody", "x"); ok {
		t.Fatal("Pending found an enrolment for a subject with none")
	}
	if ok, err := e.h.Confirm("alice", "000000"); err != nil || ok {
		t.Fatalf("Confirm with a wrong code = %v, %v", ok, err)
	}
	c, _ := totp.Code(en.Key, time.Now())
	if ok, err := e.h.Confirm("alice", c[:3]+" "+c[3:]); err != nil || !ok {
		t.Fatalf("Confirm with the right code (spaced) = %v, %v", ok, err)
	}
	if ok, _ := e.h.Enrolled("alice"); !ok {
		t.Fatal("not enrolled after confirming")
	}
	if _, ok, _ := e.h.Pending("alice", "alice@example.com"); ok {
		t.Fatal("a confirmed enrolment still reads as pending")
	}
	if _, err := e.h.Begin("alice", "alice@example.com"); !errors.Is(err, totp.ErrEnrolled) {
		t.Fatalf("Begin over a live authenticator: %v, want ErrEnrolled", err)
	}
	if err := e.h.Disable("alice"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := e.h.Enrolled("alice"); ok {
		t.Fatal("still enrolled after Disable")
	}
	// Confirming against nothing pending is a miss, not a crash.
	if ok, err := e.h.Confirm("nobody", c); err != nil || ok {
		t.Fatalf("Confirm for an unknown subject = %v, %v", ok, err)
	}
}

func TestSignInCompletesTheHalfSessionOnce(t *testing.T) {
	e := newEnv(t)
	key := enrol(t, e, "person@example.com")
	pending := hold(t, e, sessions.Session{Subject: "person@example.com", Method: "magiclink"}, "/notes/7")

	// The next step, not this one: confirming the scan spent the
	// current step, and a code is single use.
	c, _ := totp.Code(key, time.Now().Add(period))
	w := postForm(t, e.h.SignIn, pending, url.Values{"code": {c}})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/notes/7" {
		t.Fatalf("SignIn: %d -> %q, want 303 -> /notes/7", w.Code, w.Header().Get("Location"))
	}
	s, ok := sessionFrom(t, e, w)
	if !ok || s.Method != "magiclink+totp" || s.AuthTime.IsZero() {
		t.Fatalf("minted session = %+v, %v; want magiclink+totp", s, ok)
	}
	if again := postForm(t, e.h.SignIn, pending, url.Values{"code": {c}}); again.Code != http.StatusForbidden {
		t.Fatalf("SignIn after completion: %d, want 403", again.Code)
	}

	// The step is spent: the same code (or an older one) misses on a
	// fresh half-session even though its digits still agree.
	pending2 := hold(t, e, sessions.Session{Subject: "person@example.com", Method: "magiclink"}, "")
	replay := postForm(t, e.h.SignIn, pending2, url.Values{"code": {c}})
	if replay.Code != http.StatusSeeOther || replay.Header().Get("Location") != "/signin/confirm?totp=failed" {
		t.Fatalf("replayed code: %d -> %q, want 303 -> /signin/confirm?totp=failed", replay.Code, replay.Header().Get("Location"))
	}
	if _, ok := sessionFrom(t, e, replay); ok {
		t.Fatal("a replayed code minted a session")
	}
}

func TestSignInMissesStrikeThenExhaust(t *testing.T) {
	e := newEnv(t)
	enrol(t, e, "42")
	pending := hold(t, e, sessions.Session{Subject: "42", Method: "password"}, "")
	for i := 1; i < 5; i++ {
		w := postForm(t, e.h.SignIn, pending, url.Values{"code": {"000000"}})
		if w.Header().Get("Location") != "/signin/confirm?totp=failed" {
			t.Fatalf("miss %d: -> %q", i, w.Header().Get("Location"))
		}
	}
	w := postForm(t, e.h.SignIn, pending, url.Values{"code": {"000000"}})
	if w.Header().Get("Location") != "/signin/confirm?totp=exhausted" {
		t.Fatalf("fifth miss: -> %q, want ?totp=exhausted", w.Header().Get("Location"))
	}
	if again := postForm(t, e.h.SignIn, pending, url.Values{"code": {"000000"}}); again.Code != http.StatusForbidden {
		t.Fatalf("after exhaustion: %d, want 403 (no half-session)", again.Code)
	}
	if w := postForm(t, e.h.SignIn, nil, url.Values{"code": {"000000"}}); w.Code != http.StatusForbidden {
		t.Fatalf("no pending: %d, want 403", w.Code)
	}
}

func TestStepUpRotatesAStaleSession(t *testing.T) {
	e := newEnv(t)
	key := enrol(t, e, "42")
	stale := signIn(t, e, "42", time.Now().Add(-time.Hour))

	miss := postForm(t, e.h.StepUp, stale, url.Values{"code": {"000000"}, "return_to": {"/settings"}})
	if miss.Code != http.StatusSeeOther || miss.Header().Get("Location") != "/signin?reauth=1&totp=failed&return_to=%2Fsettings" {
		t.Fatalf("wrong code: %d -> %q", miss.Code, miss.Header().Get("Location"))
	}
	c, _ := totp.Code(key, time.Now().Add(period))
	hit := postForm(t, e.h.StepUp, stale, url.Values{"code": {c}, "return_to": {"/settings"}})
	if hit.Code != http.StatusSeeOther || hit.Header().Get("Location") != "/settings" {
		t.Fatalf("right code: %d -> %q, want 303 -> /settings", hit.Code, hit.Header().Get("Location"))
	}
	s, ok := sessionFrom(t, e, hit)
	if !ok || s.Method != "totp" || !sessions.Fresh(s, time.Minute, time.Now()) {
		t.Fatalf("rotated session = %+v, %v; want Method totp, fresh", s, ok)
	}
	if w := postForm(t, e.h.StepUp, nil, url.Values{"code": {c}}); w.Code != http.StatusForbidden {
		t.Fatalf("step-up with no session: %d, want 403", w.Code)
	}
	r := httptest.NewRequest("GET", testOrigin+"/totp/stepup", nil)
	w := httptest.NewRecorder()
	e.h.StepUp(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET step-up: %d, want 405", w.Code)
	}
}

func TestStepUpBudgetBlocksAfterTenMisses(t *testing.T) {
	e := newEnv(t)
	key := enrol(t, e, "42")
	stale := signIn(t, e, "42", time.Now().Add(-time.Hour))
	for i := 0; i < 10; i++ {
		postForm(t, e.h.StepUp, stale, url.Values{"code": {"000000"}})
	}
	c, _ := totp.Code(key, time.Now().Add(period))
	w := postForm(t, e.h.StepUp, stale, url.Values{"code": {c}})
	if _, ok := sessionFrom(t, e, w); ok {
		t.Fatal("a correct code got through a spent budget")
	}
}

func TestNewValidatesConfig(t *testing.T) {
	e := newEnv(t)
	key := crypto.Derive([]byte("k"), "t")
	for name, cfg := range map[string]totp.Config{
		"no db":       {Sessions: e.sess, Key: key, Issuer: "Acme"},
		"no sessions": {DB: e.db, Key: key, Issuer: "Acme"},
		"short key":   {DB: e.db, Sessions: e.sess, Key: key[:16], Issuer: "Acme"},
		"no issuer":   {DB: e.db, Sessions: e.sess, Key: key},
	} {
		if _, err := totp.New(cfg); err == nil {
			t.Errorf("%s: New accepted a bad config", name)
		}
	}
	// No Gate: enrolment and step-up work, sign-in refuses.
	h, err := totp.New(totp.Config{DB: e.db, Sessions: e.sess, Key: key, Issuer: "Acme"})
	if err != nil {
		t.Fatal(err)
	}
	if w := postForm(t, h.SignIn, nil, url.Values{"code": {"000000"}}); w.Code != http.StatusForbidden {
		t.Fatalf("SignIn with no Gate: %d, want 403", w.Code)
	}
}
