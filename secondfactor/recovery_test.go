package secondfactor_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/sessions"
)

var codeShape = regexp.MustCompile(`^[a-z2-7]{5}-[a-z2-7]{5}$`)

func TestRegenerateMintsTenWellFormedCodes(t *testing.T) {
	e := newEnv(t)
	codes, err := e.g.RegenerateRecoveryCodes("alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 10 {
		t.Fatalf("got %d codes, want 10", len(codes))
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if !codeShape.MatchString(c) {
			t.Errorf("code %q is not xxxxx-xxxxx over the recovery alphabet", c)
		}
		if seen[c] {
			t.Errorf("code %q minted twice in one set", c)
		}
		seen[c] = true
	}
	if n, err := e.g.RecoveryCodesRemaining("alice@example.com"); err != nil || n != 10 {
		t.Fatalf("remaining = %d, %v; want 10", n, err)
	}
	if n, err := e.g.RecoveryCodesRemaining("bob@example.com"); err != nil || n != 0 {
		t.Fatalf("bob's remaining = %d, %v; want 0", n, err)
	}
}

func TestRegenerateReplacesTheOldSet(t *testing.T) {
	e := newEnv(t)
	old, err := e.g.RegenerateRecoveryCodes("alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.g.RegenerateRecoveryCodes("alice@example.com"); err != nil {
		t.Fatal(err)
	}
	if n, _ := e.g.RecoveryCodesRemaining("alice@example.com"); n != 10 {
		t.Fatalf("remaining after regeneration = %d, want 10, not 20", n)
	}
	var cnt int
	if err := e.db.QueryRow(
		`SELECT COUNT(*) FROM secondfactor_recovery_codes WHERE code_hash = ?`,
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
	r := httptest.NewRequest("POST", testOrigin+"/signin/recovery", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if pending != nil {
		r.AddCookie(pending)
	}
	w := httptest.NewRecorder()
	e.g.SignInRecovery(w, r)
	return w
}

// enrolledWithCodes enrols subject in the test factor and mints a
// recovery set — the account state every redemption test starts from.
func enrolledWithCodes(t *testing.T, e env, subject string) []string {
	t.Helper()
	e.f.enrolled[subject] = true
	codes, err := e.g.RegenerateRecoveryCodes(subject)
	if err != nil {
		t.Fatal(err)
	}
	return codes
}

func TestRecoveryCodeCompletesTheSignIn(t *testing.T) {
	e := newEnv(t)
	codes := enrolledWithCodes(t, e, "person@example.com")

	_, pending := hold(t, e, sessions.Session{Subject: "person@example.com", Method: "magiclink", AuthTime: time.Now()}, "/notes/7")
	if pending == nil {
		t.Fatal("Hold did not take over for an enrolled subject")
	}

	w := postRecovery(t, e, pending, codes[0])
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/notes/7" {
		t.Fatalf("recovery: %d -> %q, want 303 -> /notes/7", w.Code, w.Header().Get("Location"))
	}
	s, ok := sessionFrom(t, e, w)
	if !ok || s.Method != "magiclink+recovery" || s.AuthTime.IsZero() {
		t.Fatalf("minted session = %+v, %v; want Method magiclink+recovery with an AuthTime", s, ok)
	}

	// The half-session was single use: the same pending cookie opens
	// nothing further, even holding nine more valid codes.
	if again := postRecovery(t, e, pending, codes[1]); again.Code != http.StatusForbidden {
		t.Fatalf("recovery after completion: %d, want 403", again.Code)
	}
	if n, _ := e.g.RecoveryCodesRemaining("person@example.com"); n != 9 {
		t.Fatalf("remaining after one redemption = %d, want 9", n)
	}

	// The code itself was single use too: a fresh half-session cannot
	// replay it.
	_, pending2 := hold(t, e, sessions.Session{Subject: "person@example.com", Method: "magiclink"}, "")
	if w := postRecovery(t, e, pending2, codes[0]); w.Code != http.StatusSeeOther ||
		w.Header().Get("Location") != "/signin/confirm?recovery=failed" {
		t.Fatalf("burned code: %d -> %q, want 303 -> /signin/confirm?recovery=failed", w.Code, w.Header().Get("Location"))
	}
}

func TestRecoveryWrongCodeKeepsHalfSessionAlive(t *testing.T) {
	e := newEnv(t)
	codes := enrolledWithCodes(t, e, "42")
	_, pending := hold(t, e, sessions.Session{Subject: "42", Method: "password"}, "")

	w := postRecovery(t, e, pending, "aaaaa-aaaaa")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/signin/confirm?recovery=failed" {
		t.Fatalf("wrong code: %d -> %q, want 303 -> /signin/confirm?recovery=failed", w.Code, w.Header().Get("Location"))
	}
	if _, ok := sessionFrom(t, e, w); ok {
		t.Fatal("wrong code minted a session cookie")
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
	_, pending := hold(t, e, sessions.Session{Subject: "alice@example.com", Method: "magiclink"}, "")
	if w := postRecovery(t, e, pending, bobs[0]); w.Code != http.StatusSeeOther ||
		w.Header().Get("Location") != "/signin/confirm?recovery=failed" {
		t.Fatalf("cross-subject code: %d -> %q, want 303 -> ?recovery=failed", w.Code, w.Header().Get("Location"))
	}
	if n, _ := e.g.RecoveryCodesRemaining("bob@example.com"); n != 10 {
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
	_, pending := hold(t, e, sessions.Session{Subject: "42", Method: "password"}, "")
	if _, err := e.db.Exec(`UPDATE secondfactor_pending SET expires_at = ?`,
		time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if w := postRecovery(t, e, pending, codes[0]); w.Code != http.StatusForbidden {
		t.Fatalf("recovery on expired pending: %d, want 403", w.Code)
	}
}

func TestRecoveryGetRefused(t *testing.T) {
	e := newEnv(t)
	r := httptest.NewRequest("GET", testOrigin+"/signin/recovery", nil)
	w := httptest.NewRecorder()
	e.g.SignInRecovery(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET recovery: %d, want 405", w.Code)
	}
}
