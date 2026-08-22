package password_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/carlosframework/rastrillo/password"
	"github.com/carlosframework/rastrillo/sessions"
)

// errDuplicateEmail is the fake store's stand-in for whatever
// distinct error the app's real Create returns on a UNIQUE
// constraint violation — handlers.go's contract is: any error from
// Create re-renders the duplicate-email message, no sentinel type
// required.
var errDuplicateEmail = errors.New("email already exists")

// fakeUser is one row of the in-memory user store the tests fake
// Lookup/Create over.
type fakeUser struct {
	id   int64
	hash string
}

// userStore is a minimal in-memory stand-in for the app's real user
// table, keyed by lowercased email.
type userStore struct {
	mu          sync.Mutex
	nextID      int64
	byMail      map[string]fakeUser
	createCalls int
}

func newUserStore() *userStore {
	return &userStore{nextID: 1, byMail: map[string]fakeUser{}}
}

func (s *userStore) lookup(_ context.Context, email string) (int64, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.byMail[email]
	if !ok {
		return 0, "", sql.ErrNoRows
	}
	return u.id, u.hash, nil
}

func (s *userStore) create(_ context.Context, email, hash string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createCalls++
	if _, exists := s.byMail[email]; exists {
		return 0, errDuplicateEmail
	}
	id := s.nextID
	s.nextID++
	s.byMail[email] = fakeUser{id: id, hash: hash}
	return id, nil
}

func (s *userStore) createCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createCalls
}

// recordedPage is one call to a Render* callback.
type recordedPage struct {
	data   password.PageData
	status int
}

// renderRecorder records every Render* call, including the status
// code already written to the ResponseWriter by the time it's
// called, and writes a body so the handler's status sticks.
type renderRecorder struct {
	mu    sync.Mutex
	calls []recordedPage
}

func (r *renderRecorder) render(w http.ResponseWriter, req *http.Request, d password.PageData) {
	rec, ok := w.(*httptest.ResponseRecorder)
	status := http.StatusOK
	if ok {
		status = rec.Code
	}
	r.mu.Lock()
	r.calls = append(r.calls, recordedPage{data: d, status: status})
	r.mu.Unlock()
	w.Write([]byte("rendered"))
}

func (r *renderRecorder) last() (password.PageData, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.calls) == 0 {
		return password.PageData{}, false
	}
	return r.calls[len(r.calls)-1].data, true
}

func (r *renderRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// newTestSessions mirrors sessions_test.go's helper: temp SQLite DB,
// Migrations applied, a *sessions.Sessions over it.
func newTestSessions(t *testing.T) *sessions.Sessions {
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
	s, err := sessions.New(sessions.Config{DB: db, Origin: "http://app.test"})
	if err != nil {
		t.Fatalf("sessions.New: %v", err)
	}
	return s
}

// testEnv is everything one test needs: the handlers under test, the
// same *sessions.Sessions instance they were built with (so tests can
// resolve cookies via sess.From, exactly as an app would), the fake
// user store, and the two render recorders.
type testEnv struct {
	h      *password.Handlers
	sess   *sessions.Sessions
	store  *userStore
	signin *renderRecorder
	signup *renderRecorder
}

func newTestEnv(t *testing.T, mut func(*password.Config)) testEnv {
	t.Helper()
	store := newUserStore()
	sess := newTestSessions(t)
	signinRec := &renderRecorder{}
	signupRec := &renderRecorder{}
	cfg := password.Config{
		Sessions:     sess,
		Lookup:       store.lookup,
		Create:       store.create,
		RenderSignin: signinRec.render,
		RenderSignup: signupRec.render,
	}
	if mut != nil {
		mut(&cfg)
	}
	h, err := password.New(cfg)
	if err != nil {
		t.Fatalf("password.New: %v", err)
	}
	return testEnv{h: h, sess: sess, store: store, signin: signinRec, signup: signupRec}
}

func signinRequest(email, pw, returnTo string) *http.Request {
	form := url.Values{"email": {email}, "password": {pw}}
	if returnTo != "" {
		form.Set("return_to", returnTo)
	}
	r := httptest.NewRequest("POST", "http://app.test/signin", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func signupRequest(email, pw string) *http.Request {
	form := url.Values{"email": {email}, "password": {pw}}
	r := httptest.NewRequest("POST", "http://app.test/signup", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

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

func TestSigninSuccessMintsSession(t *testing.T) {
	env := newTestEnv(t, nil)
	hash, err := password.Hash("s3cretpw")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	wantID, err := env.store.create(context.Background(), "user@example.com", hash)
	if err != nil {
		t.Fatalf("seed create: %v", err)
	}

	w := httptest.NewRecorder()
	r := signinRequest("user@example.com", "s3cretpw", "")
	env.h.Signin(w, r)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body=%q", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/" {
		t.Errorf("Location = %q, want /", loc)
	}

	cookie := cookieFrom(t, w, env.sess.CookieName())
	follow := httptest.NewRequest("GET", "http://app.test/", nil)
	follow.AddCookie(cookie)
	sess, ok := env.sess.From(follow)
	if !ok {
		t.Fatalf("sess.From: ok = false after successful signin")
	}
	if sess.Subject != strconv.FormatInt(wantID, 10) {
		t.Errorf("Subject = %q, want %q", sess.Subject, strconv.FormatInt(wantID, 10))
	}
	if sess.Method != "password" {
		t.Errorf("Method = %q, want password", sess.Method)
	}
}

// TestSigninSecondFactorIntercepts pins the 2FA seam: a SecondFactor
// hook that reports done gets the response — no session is minted, no
// redirect to SignedInPath happens — and the hook sees the exact
// session that would have been minted. (passkey.Handlers.Gate is the
// shipped implementation; here a recorder stands in, since the seam —
// not the ceremony — is this package's contract.)
func TestSigninSecondFactorIntercepts(t *testing.T) {
	var saw sessions.Session
	env := newTestEnv(t, func(cfg *password.Config) {
		cfg.SecondFactor = func(w http.ResponseWriter, r *http.Request, sess sessions.Session) (bool, error) {
			saw = sess
			http.Redirect(w, r, "/passkey/confirm", http.StatusSeeOther)
			return true, nil
		}
	})
	hash, _ := password.Hash("s3cretpw")
	wantID, _ := env.store.create(context.Background(), "user@example.com", hash)

	w := httptest.NewRecorder()
	env.h.Signin(w, signinRequest("user@example.com", "s3cretpw", ""))

	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/passkey/confirm" {
		t.Fatalf("gated signin: %d -> %q, want 303 -> /passkey/confirm", w.Code, w.Header().Get("Location"))
	}
	if saw.Subject != strconv.FormatInt(wantID, 10) || saw.Method != "password" {
		t.Errorf("hook saw %+v, want the would-be password session", saw)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == env.sess.CookieName() {
			t.Fatal("gated signin minted a session cookie anyway")
		}
	}
}

func TestSigninHonorsReturnTo(t *testing.T) {
	env := newTestEnv(t, nil)
	hash, _ := password.Hash("s3cretpw")
	env.store.create(context.Background(), "user@example.com", hash)

	w := httptest.NewRecorder()
	r := signinRequest("user@example.com", "s3cretpw", "/notes/7")
	env.h.Signin(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/notes/7" {
		t.Errorf("Location = %q, want /notes/7", loc)
	}

	env2 := newTestEnv(t, nil)
	hash2, _ := password.Hash("s3cretpw")
	env2.store.create(context.Background(), "user2@example.com", hash2)
	w2 := httptest.NewRecorder()
	r2 := signinRequest("user2@example.com", "s3cretpw", "https://evil.example")
	env2.h.Signin(w2, r2)
	if w2.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", w2.Code)
	}
	if loc := w2.Header().Get("Location"); loc != "/" {
		t.Errorf("Location = %q, want / (SafeReturn fallback)", loc)
	}
}

func TestSigninWrongPasswordRerenders(t *testing.T) {
	env := newTestEnv(t, nil)
	hash, _ := password.Hash("s3cretpw")
	env.store.create(context.Background(), "user@example.com", hash)

	w := httptest.NewRecorder()
	r := signinRequest("user@example.com", "wrongpw", "")
	env.h.Signin(w, r)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", w.Code)
	}
	if len(w.Result().Cookies()) != 0 {
		t.Errorf("cookies set on failed signin: %v", w.Result().Cookies())
	}
	d, ok := env.signin.last()
	if !ok {
		t.Fatalf("RenderSignin not called")
	}
	if d.Error != "Wrong email or password." {
		t.Errorf("Error = %q, want %q", d.Error, "Wrong email or password.")
	}
	if d.Email != "user@example.com" {
		t.Errorf("Email = %q, want %q", d.Email, "user@example.com")
	}
}

func TestSigninUnknownEmailSameMessage(t *testing.T) {
	env1 := newTestEnv(t, nil)
	hash, _ := password.Hash("s3cretpw")
	env1.store.create(context.Background(), "user@example.com", hash)
	w1 := httptest.NewRecorder()
	env1.h.Signin(w1, signinRequest("user@example.com", "wrongpw", ""))
	d1, ok := env1.signin.last()
	if !ok {
		t.Fatalf("RenderSignin not called (wrong password case)")
	}

	env2 := newTestEnv(t, nil)
	w2 := httptest.NewRecorder()
	env2.h.Signin(w2, signinRequest("nobody@example.com", "whatever", ""))
	d2, ok := env2.signin.last()
	if !ok {
		t.Fatalf("RenderSignin not called (unknown email case)")
	}

	if w2.Code != http.StatusUnprocessableEntity {
		t.Errorf("unknown-email status = %d, want 422", w2.Code)
	}
	if d1.Error != d2.Error {
		t.Errorf("Error mismatch: wrong-password=%q unknown-email=%q, want identical", d1.Error, d2.Error)
	}
	if d2.Error != "Wrong email or password." {
		t.Errorf("unknown-email Error = %q, want %q", d2.Error, "Wrong email or password.")
	}
}

func TestSignupCreatesAndSignsIn(t *testing.T) {
	env := newTestEnv(t, nil)
	w := httptest.NewRecorder()
	r := signupRequest("fresh@example.com", "longenoughpw")
	env.h.Signup(w, r)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body=%q", w.Code, w.Body.String())
	}
	id, hash, err := env.store.lookup(context.Background(), "fresh@example.com")
	if err != nil {
		t.Fatalf("lookup after signup: %v", err)
	}
	if hash == "longenoughpw" {
		t.Fatalf("Create received plaintext password, not a hash")
	}
	if !password.Verify(hash, "longenoughpw") {
		t.Errorf("stored hash does not Verify against the submitted password")
	}
	if id == 0 {
		t.Errorf("id = 0, want nonzero")
	}
	cookie := cookieFrom(t, w, env.sess.CookieName())
	follow := httptest.NewRequest("GET", "http://app.test/", nil)
	follow.AddCookie(cookie)
	sess, ok := env.sess.From(follow)
	if !ok {
		t.Fatalf("sess.From: ok = false after signup")
	}
	if sess.Subject != strconv.FormatInt(id, 10) {
		t.Errorf("Subject = %q, want %q", sess.Subject, strconv.FormatInt(id, 10))
	}
}

func TestSignupNilCreateDisabled(t *testing.T) {
	env := newTestEnv(t, func(cfg *password.Config) { cfg.Create = nil })
	w := httptest.NewRecorder()
	r := signupRequest("fresh@example.com", "longenoughpw")
	env.h.Signup(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestSignupDuplicateEmailRerenders(t *testing.T) {
	env := newTestEnv(t, nil)
	hash, _ := password.Hash("s3cretpw")
	env.store.create(context.Background(), "taken@example.com", hash)

	w := httptest.NewRecorder()
	env.h.Signup(w, signupRequest("taken@example.com", "anotherlongpw"))

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", w.Code)
	}
	d, ok := env.signup.last()
	if !ok {
		t.Fatalf("RenderSignup not called")
	}
	if d.Error != "That email is already registered." {
		t.Errorf("Error = %q, want %q", d.Error, "That email is already registered.")
	}
}

func TestSignupValidatesEmailAndPasswordLength(t *testing.T) {
	env := newTestEnv(t, nil)

	w1 := httptest.NewRecorder()
	env.h.Signup(w1, signupRequest("notanemail", "longenoughpw"))
	if w1.Code != http.StatusUnprocessableEntity {
		t.Errorf("bad-email status = %d, want 422", w1.Code)
	}
	d1, ok := env.signup.last()
	if !ok || d1.Error == "" {
		t.Fatalf("RenderSignup not called with an error for a bad email")
	}

	w2 := httptest.NewRecorder()
	env.h.Signup(w2, signupRequest("ok@example.com", "short"))
	if w2.Code != http.StatusUnprocessableEntity {
		t.Errorf("short-password status = %d, want 422", w2.Code)
	}
	d2, ok := env.signup.last()
	if !ok || d2.Error == "" {
		t.Fatalf("RenderSignup not called with an error for a short password")
	}

	if _, _, err := env.store.lookup(context.Background(), "notanemail"); err == nil {
		t.Errorf("Create called for invalid email")
	}
	if _, _, err := env.store.lookup(context.Background(), "ok@example.com"); err == nil {
		t.Errorf("Create called for too-short password")
	}
}

func TestSignoutRevokes(t *testing.T) {
	env := newTestEnv(t, nil)
	hash, _ := password.Hash("s3cretpw")
	env.store.create(context.Background(), "user@example.com", hash)

	w := httptest.NewRecorder()
	env.h.Signin(w, signinRequest("user@example.com", "s3cretpw", ""))
	cookie := cookieFrom(t, w, env.sess.CookieName())

	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("POST", "http://app.test/signout", nil)
	r2.AddCookie(cookie)
	env.h.Signout(w2, r2)

	if w2.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", w2.Code)
	}
	cleared := cookieFrom(t, w2, env.sess.CookieName())
	if cleared.Value != "" || cleared.MaxAge >= 0 {
		t.Errorf("signout cookie = %+v, want cleared", cleared)
	}

	follow := httptest.NewRequest("GET", "http://app.test/", nil)
	follow.AddCookie(cookie)
	if _, ok := env.sess.From(follow); ok {
		t.Errorf("sess.From ok = true after Signout, want false (row must be revoked)")
	}
}

// TestMutatingHandlersRejectNonPost: Signin, Signup, and Signout are
// POST-only — a GET must be rejected with 405 before any side effect
// (no cookie set, no re-render call, no store write, no session
// revoked), since a GET route would put a plaintext password in the
// URL/referrer/logs (Signin, Signup) or escape the app-wide CSRF
// guard, which only gates POST/PUT/PATCH/DELETE (Signout).
func TestSigninRateLimitsPerEmail(t *testing.T) {
	env := newTestEnv(t, nil)
	hash, err := password.Hash("s3cretpw")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	env.store.create(context.Background(), "user@example.com", hash)

	for i := 0; i < 10; i++ {
		w := httptest.NewRecorder()
		env.h.Signin(w, signinRequest("user@example.com", "wrong-guess", ""))
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("attempt %d: status = %d, want 422", i+1, w.Code)
		}
	}

	// The 11th attempt is blocked even with the CORRECT password — the
	// gate runs before verification, so a guesser's budget can't be
	// stretched by finally guessing right.
	w := httptest.NewRecorder()
	env.h.Signin(w, signinRequest("user@example.com", "s3cretpw", ""))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked attempt: status = %d, want 429", w.Code)
	}
	if d, ok := env.signin.last(); !ok || !strings.Contains(d.Error, "Too many") {
		t.Errorf("blocked attempt PageData.Error = %q, want a too-many-attempts message", d.Error)
	}

	// Another email's budget is untouched.
	hash2, _ := password.Hash("s3cretpw")
	env.store.create(context.Background(), "other@example.com", hash2)
	w2 := httptest.NewRecorder()
	env.h.Signin(w2, signinRequest("other@example.com", "s3cretpw", ""))
	if w2.Code != http.StatusSeeOther {
		t.Errorf("unrelated email: status = %d, want 303", w2.Code)
	}
}

func TestSignupRateLimitsPerEmail(t *testing.T) {
	env := newTestEnv(t, nil)
	hash, err := password.Hash("s3cretpw")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	env.store.create(context.Background(), "taken@example.com", hash)

	// Ten duplicate-email attempts: each an honest 422, each burning
	// one unit of the same budget Signin spends.
	for i := 0; i < 10; i++ {
		w := httptest.NewRecorder()
		env.h.Signup(w, signupRequest("taken@example.com", "anotherlongpw"))
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("attempt %d: status = %d, want 422", i+1, w.Code)
		}
	}

	// The 11th is blocked before any work — signup was the one
	// endpoint that would confirm an address without ever touching
	// the limiter.
	w := httptest.NewRecorder()
	env.h.Signup(w, signupRequest("taken@example.com", "anotherlongpw"))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked attempt: status = %d, want 429", w.Code)
	}
	if d, ok := env.signup.last(); !ok || !strings.Contains(d.Error, "Too many") {
		t.Errorf("blocked attempt PageData.Error = %q, want a too-many-attempts message", d.Error)
	}

	// The budget is shared with Signin: the same probed email is
	// blocked there too, so the two doors can't be alternated.
	w2 := httptest.NewRecorder()
	env.h.Signin(w2, signinRequest("taken@example.com", "s3cretpw", ""))
	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("signin after signup probes: status = %d, want 429", w2.Code)
	}

	// An unrelated email's signup is untouched.
	w3 := httptest.NewRecorder()
	env.h.Signup(w3, signupRequest("fresh@example.com", "longenoughpw"))
	if w3.Code != http.StatusSeeOther {
		t.Errorf("unrelated email: status = %d, want 303", w3.Code)
	}
}

func TestSigninSuccessResetsRateLimit(t *testing.T) {
	env := newTestEnv(t, nil)
	hash, err := password.Hash("s3cretpw")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	env.store.create(context.Background(), "user@example.com", hash)

	for i := 0; i < 9; i++ {
		w := httptest.NewRecorder()
		env.h.Signin(w, signinRequest("user@example.com", "wrong-guess", ""))
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("attempt %d: status = %d, want 422", i+1, w.Code)
		}
	}
	w := httptest.NewRecorder()
	env.h.Signin(w, signinRequest("user@example.com", "s3cretpw", ""))
	if w.Code != http.StatusSeeOther {
		t.Fatalf("success under budget: status = %d, want 303", w.Code)
	}

	// The success cleared the count: nine fresh typos fit again.
	for i := 0; i < 9; i++ {
		w := httptest.NewRecorder()
		env.h.Signin(w, signinRequest("user@example.com", "wrong-guess", ""))
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("post-reset attempt %d: status = %d, want 422 (not blocked)", i+1, w.Code)
		}
	}
}

func TestConfiguredLoggerReceivesErrors(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	env := newTestEnv(t, func(cfg *password.Config) {
		cfg.Logger = logger
		cfg.Lookup = func(context.Context, string) (int64, string, error) {
			return 0, "", errors.New("db is on fire")
		}
	})

	w := httptest.NewRecorder()
	env.h.Signin(w, signinRequest("a@example.com", "whatever0", ""))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if !strings.Contains(buf.String(), "db is on fire") {
		t.Errorf("configured logger did not receive the lookup error; log output: %q", buf.String())
	}
}

func TestMutatingHandlersRejectNonPost(t *testing.T) {
	env := newTestEnv(t, nil)
	hash, _ := password.Hash("s3cretpw")
	env.store.create(context.Background(), "user@example.com", hash)

	w := httptest.NewRecorder()
	getSignin := httptest.NewRequest("GET", "http://app.test/signin?email=user@example.com&password=s3cretpw", nil)
	env.h.Signin(w, getSignin)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET Signin status = %d, want 405", w.Code)
	}
	if len(w.Result().Cookies()) != 0 {
		t.Errorf("GET Signin set cookies: %v", w.Result().Cookies())
	}
	if env.signin.count() != 0 {
		t.Errorf("GET Signin called RenderSignin %d times, want 0", env.signin.count())
	}

	// GET Signup, on a config with Create wired: 405, no cookies,
	// Create never invoked, RenderSignup never invoked.
	beforeCreates := env.store.createCallCount()
	w3 := httptest.NewRecorder()
	getSignup := httptest.NewRequest("GET", "http://app.test/signup?email=fresh@example.com&password=longenoughpw", nil)
	env.h.Signup(w3, getSignup)
	if w3.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET Signup status = %d, want 405", w3.Code)
	}
	if len(w3.Result().Cookies()) != 0 {
		t.Errorf("GET Signup set cookies: %v", w3.Result().Cookies())
	}
	if got := env.store.createCallCount(); got != beforeCreates {
		t.Errorf("GET Signup called Create %d times, want %d (unchanged)", got, beforeCreates)
	}
	if env.signup.count() != 0 {
		t.Errorf("GET Signup called RenderSignup %d times, want 0", env.signup.count())
	}

	// A live session must survive a GET-mounted Signout attempt: sign
	// in first, then confirm GET Signout doesn't revoke it.
	signinW := httptest.NewRecorder()
	env.h.Signin(signinW, signinRequest("user@example.com", "s3cretpw", ""))
	cookie := cookieFrom(t, signinW, env.sess.CookieName())

	w2 := httptest.NewRecorder()
	getSignout := httptest.NewRequest("GET", "http://app.test/signout", nil)
	getSignout.AddCookie(cookie)
	env.h.Signout(w2, getSignout)
	if w2.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET Signout status = %d, want 405", w2.Code)
	}
	if len(w2.Result().Cookies()) != 0 {
		t.Errorf("GET Signout set/cleared cookies: %v", w2.Result().Cookies())
	}

	follow := httptest.NewRequest("GET", "http://app.test/", nil)
	follow.AddCookie(cookie)
	if _, ok := env.sess.From(follow); !ok {
		t.Errorf("session revoked by a GET Signout attempt, want it to survive")
	}
}
