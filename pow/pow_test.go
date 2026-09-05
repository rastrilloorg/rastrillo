package pow

import (
	"context"
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

func newTestGuard(t *testing.T, mut func(*Config)) *Guard {
	t.Helper()
	cfg := Config{
		InstanceKey: "test-instance-key",
		Nonces:      MemoryNonces(),
		Difficulty:  testDifficulty,
	}
	if mut != nil {
		mut(&cfg)
	}
	g, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return g
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

func TestSealRefusesEveryEditedField(t *testing.T) {
	g := newTestGuard(t, nil)
	issued := time.Now().Add(-time.Minute)
	c := g.Issue(issued)

	if reason, ok := verifySeal(g.key, c, time.Now(), g.minAge, g.maxAge); !ok {
		t.Fatalf("a freshly minted challenge did not verify: %s", reason)
	}

	edits := map[string]func(Challenge) Challenge{
		"nonce":      func(c Challenge) Challenge { c.Nonce = "0000"; return c },
		"issued_at":  func(c Challenge) Challenge { c.IssuedAt -= 3600; return c },
		"difficulty": func(c Challenge) Challenge { c.Difficulty = 1; return c },
		"seal":       func(c Challenge) Challenge { c.Seal = strings.Repeat("0", len(c.Seal)); return c },
	}
	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			reason, ok := verifySeal(g.key, edit(c), time.Now(), g.minAge, g.maxAge)
			if ok || reason != ReasonSealInvalid {
				t.Fatalf("edited %s: reason %q ok %v, want seal_invalid", name, reason, ok)
			}
		})
	}
}

func TestSealChecksTheSignatureBeforeTheClock(t *testing.T) {
	// An unsigned challenge claiming a plausible age must still come
	// back seal_invalid, not too_fast or too_old: until the signature
	// holds, IssuedAt is a number the submitter chose, and reporting an
	// age verdict on it would be answering a question about their own
	// input.
	g := newTestGuard(t, nil)
	forged := Challenge{
		Nonce:      "abcd",
		IssuedAt:   time.Now().Add(-24 * time.Hour).Unix(),
		Difficulty: testDifficulty,
		Seal:       strings.Repeat("0", 64),
	}
	if reason, _ := verifySeal(g.key, forged, time.Now(), g.minAge, g.maxAge); reason != ReasonSealInvalid {
		t.Fatalf("reason = %q, want seal_invalid: the clock was consulted before the signature", reason)
	}
}

func TestSealBoundsTheAge(t *testing.T) {
	g := newTestGuard(t, nil)
	now := time.Now()
	c := g.Issue(now)

	if reason, _ := verifySeal(g.key, c, now.Add(time.Second), g.minAge, g.maxAge); reason != ReasonTooFast {
		t.Errorf("one second old: reason = %q, want too_fast", reason)
	}
	if reason, _ := verifySeal(g.key, c, now.Add(g.maxAge+time.Minute), g.minAge, g.maxAge); reason != ReasonTooOld {
		t.Errorf("past MaxAge: reason = %q, want too_old", reason)
	}
	if _, ok := verifySeal(g.key, c, now.Add(g.minAge+time.Second), g.minAge, g.maxAge); !ok {
		t.Error("a challenge of a plausible age was refused")
	}
}

func TestSealsFromAnotherInstanceKeyDoNotVerify(t *testing.T) {
	a := newTestGuard(t, nil)
	b := newTestGuard(t, func(c *Config) { c.InstanceKey = "a-different-key" })
	c := a.Issue(time.Now().Add(-time.Minute))
	if _, ok := verifySeal(b.key, c, time.Now(), b.minAge, b.maxAge); ok {
		t.Fatal("a challenge minted under one instance key verified under another")
	}
}

func TestNewRefusesAMissingRequirement(t *testing.T) {
	if _, err := New(Config{Nonces: MemoryNonces()}); err != ErrEmptyInstanceKey {
		t.Errorf("no instance key: err = %v, want ErrEmptyInstanceKey", err)
	}
	if _, err := New(Config{InstanceKey: "k"}); err != ErrNoNonceStore {
		t.Errorf("no nonce store: err = %v, want ErrNoNonceStore", err)
	}
}

func TestNewFillsInTheDefaults(t *testing.T) {
	g, err := New(Config{InstanceKey: "k", Nonces: MemoryNonces()})
	if err != nil {
		t.Fatal(err)
	}
	if g.difficulty != DefaultDifficulty || g.minAge != DefaultMinAge || g.maxAge != DefaultMaxAge {
		t.Fatalf("defaults not applied: %d %v %v", g.difficulty, g.minAge, g.maxAge)
	}
}

// submit builds a POST carrying a challenge, a solution for binding,
// and whatever the caller wants changed about it.
func submit(t *testing.T, g *Guard, c Challenge, binding string, mut func(url.Values)) *http.Request {
	t.Helper()
	form := url.Values{
		fieldNonce:      {c.Nonce},
		fieldIssuedAt:   {strconv.FormatInt(c.IssuedAt, 10)},
		fieldDifficulty: {strconv.Itoa(c.Difficulty)},
		fieldSeal:       {c.Seal},
		fieldCounter:    {solveFor(t, c.Nonce, binding, c.Difficulty)},
	}
	if mut != nil {
		mut(form)
	}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func TestCheckAcceptsAGoodSubmission(t *testing.T) {
	g := newTestGuard(t, nil)
	c := g.Issue(time.Now().Add(-time.Minute))
	if reason, ok := g.Check(submit(t, g, c, "alice@example.com", nil), "alice@example.com"); !ok {
		t.Fatalf("a good submission was refused: %s", reason)
	}
}

func TestCheckSpendsTheNonceOnce(t *testing.T) {
	// One solved challenge is otherwise replayable for its whole
	// MaxAge window, and every replay costs the app whatever the form
	// spends.
	g := newTestGuard(t, nil)
	c := g.Issue(time.Now().Add(-time.Minute))
	if _, ok := g.Check(submit(t, g, c, "alice@example.com", nil), "alice@example.com"); !ok {
		t.Fatal("the first submission was refused")
	}
	reason, ok := g.Check(submit(t, g, c, "alice@example.com", nil), "alice@example.com")
	if ok || reason != ReasonNonceSpent {
		t.Fatalf("replay: reason %q ok %v, want nonce_spent", reason, ok)
	}
}

func TestCheckRefusesTheHoneypotBeforeAnythingElse(t *testing.T) {
	// A filled honeypot must be answered without touching the seal, the
	// clock or the store — it is free, and it must not sit behind
	// anything that writes.
	g := newTestGuard(t, nil)
	form := url.Values{fieldHoneypot: {"http://spam.example"}}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if reason, ok := g.Check(r, "alice@example.com"); ok || reason != ReasonHoneypot {
		t.Fatalf("reason %q ok %v, want honeypot", reason, ok)
	}
}

func TestCheckRefusesASolutionForAnotherBinding(t *testing.T) {
	g := newTestGuard(t, nil)
	c := g.Issue(time.Now().Add(-time.Minute))
	r := submit(t, g, c, "alice@example.com", nil)
	if reason, ok := g.Check(r, "bob@example.com"); ok || reason != ReasonShort {
		t.Fatalf("reason %q ok %v, want pow_short", reason, ok)
	}
}

func TestCheckRefusesAClaimedLowerDifficulty(t *testing.T) {
	// The difficulty is inside the seal, so a submitter lowering it
	// breaks the signature. This holds the pair together: without the
	// comparison, a seal minted under an old, lower setting would keep
	// buying cheap submissions after the setting was raised.
	g := newTestGuard(t, nil)
	easy := newTestGuard(t, func(c *Config) { c.Difficulty = 1 })
	c := easy.Issue(time.Now().Add(-time.Minute))
	r := submit(t, easy, c, "alice@example.com", nil)
	if reason, ok := g.Check(r, "alice@example.com"); ok || reason != ReasonSealInvalid {
		t.Fatalf("reason %q ok %v, want seal_invalid", reason, ok)
	}
}

func TestCheckRefusesWhenTheStoreIsUnreachable(t *testing.T) {
	// An unreachable store must not become unlimited replay.
	g := newTestGuard(t, func(c *Config) { c.Nonces = brokenNonces{} })
	c := g.Issue(time.Now().Add(-time.Minute))
	r := submit(t, g, c, "alice@example.com", nil)
	if reason, ok := g.Check(r, "alice@example.com"); ok || reason != ReasonUnavailable {
		t.Fatalf("reason %q ok %v, want unavailable", reason, ok)
	}
}

type brokenNonces struct{}

func (brokenNonces) Spend(context.Context, string, time.Time) (bool, error) {
	return false, context.DeadlineExceeded
}
func (brokenNonces) Sweep(time.Time) error { return context.DeadlineExceeded }

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
	html := string(newTestGuard(t, nil).Issue(time.Now()).Fields())
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
	for _, name := range []string{fieldNonce, fieldIssuedAt, fieldDifficulty, fieldSeal, fieldCounter} {
		if !strings.Contains(html, `name="`+name+`"`) {
			t.Errorf("Fields does not render %s", name)
		}
	}
}

func TestFieldsAndCheckAgreeOnEveryName(t *testing.T) {
	// The one thing that would break silently: Fields renaming a field
	// Check still reads. A form that renders would then always be
	// refused, with a seal_invalid nobody can explain.
	g := newTestGuard(t, nil)
	c := g.Issue(time.Now().Add(-time.Minute))
	html := string(c.Fields())
	form := url.Values{}
	for _, name := range fieldNamesIn(html) {
		switch name {
		case fieldNonce:
			form.Set(name, c.Nonce)
		case fieldIssuedAt:
			form.Set(name, strconv.FormatInt(c.IssuedAt, 10))
		case fieldDifficulty:
			form.Set(name, strconv.Itoa(c.Difficulty))
		case fieldSeal:
			form.Set(name, c.Seal)
		case fieldCounter:
			form.Set(name, solveFor(t, c.Nonce, "alice@example.com", c.Difficulty))
		}
	}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if reason, ok := g.Check(r, "alice@example.com"); !ok {
		t.Fatalf("a form built only from Fields's own names was refused: %s", reason)
	}
}

// fieldNamesIn pulls every name="..." out of rendered HTML.
func fieldNamesIn(html string) []string {
	var out []string
	for rest := html; ; {
		i := strings.Index(rest, `name="`)
		if i < 0 {
			return out
		}
		rest = rest[i+len(`name="`):]
		j := strings.IndexByte(rest, '"')
		if j < 0 {
			return out
		}
		out = append(out, rest[:j])
		rest = rest[j:]
	}
}

func TestFormAttrsCarriesWhatTheModuleReads(t *testing.T) {
	c := newTestGuard(t, nil).Issue(time.Now())
	attrs := string(c.FormAttrs("/pow/pow-worker.abc123.js"))
	for _, want := range []string{
		"data-pow-form",
		`data-pow-nonce="` + c.Nonce + `"`,
		`data-pow-difficulty="` + strconv.Itoa(c.Difficulty) + `"`,
		`data-pow-worker="/pow/pow-worker.abc123.js"`,
	} {
		if !strings.Contains(attrs, want) {
			t.Errorf("FormAttrs is missing %s: %s", want, attrs)
		}
	}
}

func TestFormAttrsEscapesTheWorkerURL(t *testing.T) {
	attrs := string(newTestGuard(t, nil).Issue(time.Now()).FormAttrs(`" onload="alert(1)`))
	if strings.Contains(attrs, `onload="alert(1)`) {
		t.Fatalf("FormAttrs let an attribute out of its quotes: %s", attrs)
	}
}

func TestMemoryNoncesSweepsWhatHasExpired(t *testing.T) {
	s := MemoryNonces()
	now := time.Now()
	if fresh, _ := s.Spend(context.Background(), "old", now.Add(-time.Hour)); !fresh {
		t.Fatal("first spend was not fresh")
	}
	if fresh, _ := s.Spend(context.Background(), "live", now.Add(time.Hour)); !fresh {
		t.Fatal("first spend was not fresh")
	}
	if err := s.Sweep(now); err != nil {
		t.Fatal(err)
	}
	if fresh, _ := s.Spend(context.Background(), "old", now.Add(time.Hour)); !fresh {
		t.Error("an expired nonce was still remembered after a sweep")
	}
	if fresh, _ := s.Spend(context.Background(), "live", now.Add(time.Hour)); fresh {
		t.Error("a live nonce was swept")
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
