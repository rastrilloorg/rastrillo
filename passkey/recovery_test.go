// recovery_test.go proves the recovery-code escape hatch: minting and
// replacement, redemption at the sign-in gate only, single use, the
// survival of the half-session across a wrong code, and the subject
// wall between one user's codes and another's half-session.
package passkey_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/carlosframework/rastrillo/sessions"
	"github.com/carlosframework/rastrillo/webauthn/authtest"
)

var codeShape = regexp.MustCompile(`^[a-z2-7]{5}-[a-z2-7]{5}$`)

func TestRegenerateMintsTenWellFormedCodes(t *testing.T) {
	e := newEnv(t)
	codes, err := e.h.RegenerateRecoveryCodes("alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 10 {
		t.Fatalf("got %d codes, want 10", len(codes))
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if !codeShape.MatchString(c) {
			t.Fatalf("malformed code %q", c)
		}
		if seen[c] {
			t.Fatalf("duplicate code %q", c)
		}
		seen[c] = true
	}
	if n, err := e.h.RecoveryCodesRemaining("alice@example.com"); err != nil || n != 10 {
		t.Fatalf("remaining = %d, %v; want 10", n, err)
	}
	// Another subject's count is untouched.
	if n, err := e.h.RecoveryCodesRemaining("bob@example.com"); err != nil || n != 0 {
		t.Fatalf("bob remaining = %d, %v; want 0", n, err)
	}
}

func TestRegenerateReplacesTheOldSet(t *testing.T) {
	e := newEnv(t)
	old, err := e.h.RegenerateRecoveryCodes("alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.h.RegenerateRecoveryCodes("alice@example.com"); err != nil {
		t.Fatal(err)
	}
	if n, _ := e.h.RecoveryCodesRemaining("alice@example.com"); n != 10 {
		t.Fatalf("remaining after regenerate = %d, want 10 (not 20)", n)
	}
	// The old set is gone from storage, not just outnumbered: its
	// hashes (of the normalized, dashless form) no longer exist.
	var cnt int
	if err := e.db.QueryRow(
		`SELECT COUNT(*) FROM passkey_recovery_codes WHERE code_hash = ?`,
		sessions.HashToken(strings.ReplaceAll(old[0], "-", ""))).Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 0 {
		t.Fatal("an old code survived regeneration")
	}
}

// postRecovery POSTs the form the app's confirm page renders: one
// "code" field, the pending cookie riding along.
func postRecovery(t *testing.T, e env, pending *http.Cookie, code string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"code": {code}}
	r := httptest.NewRequest("POST", testOrigin+"/passkey/signin/recovery", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if pending != nil {
		r.AddCookie(pending)
	}
	w := httptest.NewRecorder()
	e.h.SignInRecovery(w, r)
	return w
}

// enrolledWithCodes signs subject in, enrolls a passkey, and mints a
// recovery set — the account state every redemption test starts from.
func enrolledWithCodes(t *testing.T, e env, subject string) []string {
	t.Helper()
	cookie := e.signIn(t, subject)
	a, err := authtest.New()
	if err != nil {
		t.Fatal(err)
	}
	enroll(t, e, cookie, a)
	codes, err := e.h.RegenerateRecoveryCodes(subject)
	if err != nil {
		t.Fatal(err)
	}
	return codes
}

func TestRecoveryCodeCompletesTheSignIn(t *testing.T) {
	e := newEnv(t)
	codes := enrolledWithCodes(t, e, "person@example.com")

	_, pending := gate(t, e, sessions.Session{Subject: "person@example.com", Method: "magiclink", AuthTime: time.Now()}, "/notes/7")
	if pending == nil {
		t.Fatal("Gate did not take over for an enrolled subject")
	}

	w := postRecovery(t, e, pending, codes[0])
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/notes/7" {
		t.Fatalf("recovery: %d -> %q, want 303 -> /notes/7", w.Code, w.Header().Get("Location"))
	}
	var minted, cleared *http.Cookie
	for _, c := range w.Result().Cookies() {
		switch c.Name {
		case e.sess.CookieName():
			minted = c
		case "rastrillo_passkey_pending":
			cleared = c
		}
	}
	if minted == nil || minted.Value == "" {
		t.Fatal("recovery minted no session cookie")
	}
	if cleared == nil || cleared.MaxAge != -1 {
		t.Fatal("recovery did not clear the pending cookie")
	}

	// The minted session names the first factor plus the escape hatch.
	r := httptest.NewRequest("GET", testOrigin+"/", nil)
	r.AddCookie(minted)
	s, ok := e.sess.From(r)
	if !ok || s.Method != "magiclink+recovery" {
		t.Fatalf("minted session = %+v, %v; want Method magiclink+recovery", s, ok)
	}
	if s.AuthTime.IsZero() {
		t.Fatal("minted session has no AuthTime; RequireFresh would mis-age it")
	}

	// The half-session was single use: the same pending cookie opens
	// nothing further, even holding nine more valid codes.
	if again := postRecovery(t, e, pending, codes[1]); again.Code != http.StatusForbidden {
		t.Fatalf("recovery after completion: %d, want 403", again.Code)
	}
	if n, _ := e.h.RecoveryCodesRemaining("person@example.com"); n != 9 {
		t.Fatalf("remaining after one redemption = %d, want 9", n)
	}

	// The code itself was single use too: a fresh half-session cannot
	// replay it.
	_, pending2 := gate(t, e, sessions.Session{Subject: "person@example.com", Method: "magiclink"}, "")
	if w := postRecovery(t, e, pending2, codes[0]); w.Code != http.StatusSeeOther ||
		w.Header().Get("Location") != "/passkey/confirm?recovery=failed" {
		t.Fatalf("burned code: %d -> %q, want 303 -> /passkey/confirm?recovery=failed", w.Code, w.Header().Get("Location"))
	}
}

func TestRecoveryWrongCodeKeepsHalfSessionAlive(t *testing.T) {
	e := newEnv(t)
	codes := enrolledWithCodes(t, e, "42")
	_, pending := gate(t, e, sessions.Session{Subject: "42", Method: "password"}, "")

	w := postRecovery(t, e, pending, "aaaaa-aaaaa")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/passkey/confirm?recovery=failed" {
		t.Fatalf("wrong code: %d -> %q, want 303 -> /passkey/confirm?recovery=failed", w.Code, w.Header().Get("Location"))
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == e.sess.CookieName() {
			t.Fatal("wrong code minted a session cookie")
		}
	}

	// A typo does not burn the between-factors window: the same
	// half-session still accepts a correct code — retyped from paper
	// in any casing, with grouping dashes and spaces intact.
	if w := postRecovery(t, e, pending, "  "+strings.ToUpper(codes[0])+" "); w.Code != http.StatusSeeOther ||
		w.Header().Get("Location") != "/" {
		t.Fatalf("correct code after a miss: %d -> %q, want 303 -> /", w.Code, w.Header().Get("Location"))
	}
}

func TestRecoveryIsolation(t *testing.T) {
	e := newEnv(t)
	enrolledWithCodes(t, e, "alice@example.com")
	bobs := enrolledWithCodes(t, e, "bob@example.com")

	// Bob's perfectly valid code redeems nothing against Alice's
	// half-session: redemption keys on hash AND subject.
	_, pending := gate(t, e, sessions.Session{Subject: "alice@example.com", Method: "magiclink"}, "")
	if w := postRecovery(t, e, pending, bobs[0]); w.Code != http.StatusSeeOther ||
		w.Header().Get("Location") != "/passkey/confirm?recovery=failed" {
		t.Fatalf("cross-subject code: %d -> %q, want 303 -> ?recovery=failed", w.Code, w.Header().Get("Location"))
	}
	if n, _ := e.h.RecoveryCodesRemaining("bob@example.com"); n != 10 {
		t.Fatalf("bob's set shrank to %d from someone else's half-session", n)
	}
}

func TestRecoveryWithoutPendingRefused(t *testing.T) {
	e := newEnv(t)
	if w := postRecovery(t, e, nil, "aaaaa-aaaaa"); w.Code != http.StatusForbidden {
		t.Fatalf("recovery without pending: %d, want 403", w.Code)
	}
}

func TestRecoveryExpiredPendingRefused(t *testing.T) {
	e := newEnv(t)
	codes := enrolledWithCodes(t, e, "42")
	_, pending := gate(t, e, sessions.Session{Subject: "42", Method: "password"}, "")
	if _, err := e.db.Exec(`UPDATE passkey_pending SET expires_at = ?`,
		time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if w := postRecovery(t, e, pending, codes[0]); w.Code != http.StatusForbidden {
		t.Fatalf("recovery on expired pending: %d, want 403", w.Code)
	}
}

func TestRecoveryGetRefused(t *testing.T) {
	e := newEnv(t)
	r := httptest.NewRequest("GET", testOrigin+"/passkey/signin/recovery", nil)
	w := httptest.NewRecorder()
	e.h.SignInRecovery(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET recovery: %d, want 405", w.Code)
	}
}
