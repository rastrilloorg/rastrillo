package pow

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// testDifficulty is low on purpose. Every property under test here is
// agreement and ordering, not cost; a realistic 18 bits would make the
// suite take as long as a real submission for no extra information.
const testDifficulty = 10

// solveFor is the Go twin of browser/powcore.js's search, for tests
// that need a solution. It is not exported and never will be: shipping
// a Go solver would invite a caller to solve challenges server-side,
// which is the one thing the price is meant to prevent.
func solveFor(t *testing.T, nonce, binding string, difficulty int) string {
	t.Helper()
	for i := 0; i < 1<<24; i++ {
		c := strconv.Itoa(i)
		if Verify(nonce, binding, c, difficulty) {
			return c
		}
	}
	t.Fatalf("no solution for %q at difficulty %d in 2^24 tries", binding, difficulty)
	return ""
}

// t0 is a fixed clock: every Guard-level test sets g.now so ages are
// exact rather than racing the wall clock.
var t0 = time.UnixMilli(1_800_000_000_000)

func newTestGuard(t *testing.T, mut func(*Config)) *Guard {
	t.Helper()
	cfg := Config{
		InstanceKey: "test-instance-key",
		Nonces:      &memNonces{spent: map[string]time.Time{}, now: func() time.Time { return t0.Add(time.Minute) }},
		Difficulty:  testDifficulty,
		MinAge:      time.Second,
		ScriptURL:   "/pow/pow.js",
		WorkerURL:   "/pow/pow-worker.js",
	}
	if mut != nil {
		mut(&cfg)
	}
	g, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	g.now = func() time.Time { return t0.Add(time.Minute) }
	return g
}

// post builds the request a browser would send for f: every sealed
// field, a solved counter when f needs proof, and mut's edits.
func post(t *testing.T, f Form, binding string, mut func(url.Values)) *http.Request {
	t.Helper()
	v := valuesOf(f.Challenge)
	if f.Difficulty > 0 {
		v.Set(fieldCounter, solveFor(t, f.Nonce, binding, f.Difficulty))
	}
	if mut != nil {
		mut(v)
	}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}
func TestNormalizeOnlyFoldsASCII(t *testing.T) {
	// The Unicode cases are the point: Go's strings.ToLower would fold
	// them and JavaScript's toLowerCase would fold them differently, so
	// this leaves both alone and the two sides cannot disagree.
	cases := map[string]string{
		"  Alice@Example.COM  ": "alice@example.com",
		// The dotted capital I is the classic disagreement: Go folds it
		// to "i̇" (two runes), a Turkish locale folds it to "ı", and
		// JavaScript does its own thing. Left alone, nobody disagrees.
		"İSTANBUL": "İstanbul",
		"ǅ":        "ǅ",
		"STRASSE":  "strasse",
	}
	for in, want := range cases {
		if got := normalize(in); got != want {
			t.Errorf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestVerifyAcceptsASolution(t *testing.T) {
	const nonce = "0f1e2d3c4b5a69788796a5b4c3d2e1f0"
	counter := solveFor(t, nonce, "alice@example.com", testDifficulty)
	if !Verify(nonce, "alice@example.com", counter, testDifficulty) {
		t.Fatal("Verify rejected a solution it produced")
	}
	// Case and surrounding space are normalised on both sides, so the
	// same solution has to hold for the same address typed differently.
	if !Verify(nonce, "  Alice@Example.COM ", counter, testDifficulty) {
		t.Fatal("Verify is sensitive to case or space in the binding")
	}
}

func TestVerifyRefusesAnEmptyCounter(t *testing.T) {
	if Verify("n", "a@x.com", "", 1) {
		t.Fatal("an empty counter verified")
	}
}

func TestSolutionDoesNotTransferToAnotherBinding(t *testing.T) {
	// The binding is inside the hash, and this is what that buys. If it
	// were ever dropped, one solve would price a whole list.
	const nonce = "0f1e2d3c4b5a69788796a5b4c3d2e1f0"
	counter := solveFor(t, nonce, "alice@example.com", testDifficulty)
	if Verify(nonce, "bob@example.com", counter, testDifficulty) {
		t.Fatal("a solution for one address verified for another")
	}
}

func TestSolutionDoesNotTransferToAnotherNonce(t *testing.T) {
	counter := solveFor(t, "aaaa", "alice@example.com", testDifficulty)
	if Verify("bbbb", "alice@example.com", counter, testDifficulty) {
		t.Fatal("a solution for one challenge verified against another")
	}
}

func TestTrappedReadsTheHoneypot(t *testing.T) {
	form := url.Values{fieldHoneypot: {"anything"}}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if !Trapped(r) {
		t.Fatal("a filled honeypot was not reported")
	}
	empty := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))
	empty.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if Trapped(empty) {
		t.Fatal("an untouched honeypot reported a trap")
	}
}

func TestFieldsCarriesTheHoneypotContract(t *testing.T) {
	// Each of these keeps a real person out of the trap. Without
	// aria-hidden a screen reader announces it; without tabindex="-1"
	// a keyboard lands in it; without autocomplete="off" a password
	// manager fills it — and a filled honeypot silently discards the
	// submission behind a cheerful success page.
	html := string(newTestGuard(t, nil).Issue(t0, "s").Fields())
	for _, want := range []string{
		`aria-hidden="true"`,
		`tabindex="-1"`,
		`autocomplete="off"`,
		`name="` + fieldHoneypot + `"`,
		`data-pow-counter`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("Fields is missing %s:\n%s", want, html)
		}
	}
	if strings.Contains(html, "display:none") {
		t.Error("the honeypot uses display:none, which some bots skip")
	}
	for _, name := range []string{fieldScope, fieldNonce, fieldIssued, fieldExpires, fieldDifficulty, fieldFlags, fieldSeal, fieldCounter} {
		if !strings.Contains(html, `name="`+name+`"`) {
			t.Errorf("Fields does not render %s", name)
		}
	}
}

func TestHoneypotStyleHashMatchesTheStyle(t *testing.T) {
	// A CSP hash admits one exact byte string. Edit the style without
	// the hash and the honeypot renders in plain view under the default
	// policy — a visible "Leave this field empty" box on every public
	// form, and nothing in a Go test that would otherwise say so.
	sum := sha256.Sum256([]byte(honeypotStyle))
	want := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
	if HoneypotStyleHash != want {
		t.Errorf("HoneypotStyleHash = %s, want %s", HoneypotStyleHash, want)
	}
	html := string(newTestGuard(t, nil).Issue(t0, "s").Fields())
	if !strings.Contains(html, `style="`+honeypotStyle+`"`) {
		t.Errorf("Fields does not render the hashed style byte for byte:\n%s", html)
	}
}

func TestAssetsCarriesBothHalvesOfTheBrowserSide(t *testing.T) {
	// The package's whole claim is that it ships the solver alongside
	// the verifier. A missing file breaks that silently at runtime.
	fsys := Assets()
	for _, name := range []string{"pow.js", "pow-worker.js", "powcore.js", "sha256.js"} {
		if _, err := fsys.Open(name); err != nil {
			t.Errorf("Assets is missing %s: %v", name, err)
		}
	}
}

func TestNewFillsInTheDefaults(t *testing.T) {
	g, err := New(Config{InstanceKey: "k", Nonces: MemoryNonces(), ScriptURL: "/a.js", WorkerURL: "/w.js"})
	if err != nil {
		t.Fatal(err)
	}
	if g.difficulty != DefaultDifficulty || g.minAge != DefaultMinAge || g.maxAge != DefaultMaxAge {
		t.Fatalf("defaults not applied: %d %v %v", g.difficulty, g.minAge, g.maxAge)
	}
	if g.attempts.limit != DefaultAttempts || g.attempts.max != DefaultTracked {
		t.Fatalf("attempt defaults not applied: %d %d", g.attempts.limit, g.attempts.max)
	}
}

func TestMemoryNoncesRefuseAnExpiredSpendAndSweepPastTheMargin(t *testing.T) {
	// The memory store follows the SQL store's rules, or a test that
	// passes on it proves nothing about production.
	now := t0
	s := &memNonces{spent: map[string]time.Time{}, now: func() time.Time { return now }}
	ctx := context.Background()
	if fresh, _ := s.Spend(ctx, nil, "late", now.Add(-time.Second)); fresh {
		t.Fatal("a spend past its expiry was accepted")
	}
	s.Spend(ctx, nil, "old", now.Add(-time.Hour))
	s.Spend(ctx, nil, "recent", now.Add(-5*time.Minute))
	s.spent["old"] = now.Add(-time.Hour) // the spend above was refused as expired
	s.spent["recent"] = now.Add(-5 * time.Minute)
	if err := s.Sweep(now); err != nil {
		t.Fatal(err)
	}
	if spent, _ := s.Spent(ctx, "old"); spent {
		t.Error("a row past the margin survived a sweep")
	}
	if spent, _ := s.Spent(ctx, "recent"); !spent {
		t.Error("a row inside the margin was swept")
	}
}
