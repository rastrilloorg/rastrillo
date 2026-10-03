package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/pow"
	"amadan.net/rastrillo/rastrillo/pow/powtest"
)

func newProofAuth(t *testing.T, mut func(*Config)) (*Auth, *captureMailer) {
	t.Helper()
	return newTestAuth(t, func(c *Config) {
		g, err := pow.New(pow.Config{InstanceKey: "test-instance-key", Nonces: pow.SQLNonces(c.DB),
			Difficulty: 8, MinAge: time.Millisecond, ScriptURL: "/pow/pow.js", WorkerURL: "/pow/pow-worker.js"})
		if err != nil {
			t.Fatalf("pow.New: %v", err)
		}
		c.Proof, c.ProofOff = g, false
		if mut != nil {
			mut(c)
		}
	})
}

// filled is a Begin form as the browser would post it after loading
// /signin<query>: the challenge SigninState hands the page, solved.
func filled(t *testing.T, a *Auth, query string, form url.Values) url.Values {
	t.Helper()
	st := a.SigninState(httptest.NewRequest(http.MethodGet, "/signin"+query, nil))
	if st.Proof == nil {
		t.Fatal("SigninState carries no challenge although Config.Proof is set")
	}
	time.Sleep(2 * time.Millisecond) // past MinAge, as a person always is
	return powtest.Fill(t, []byte(st.Proof.Fields()), form)
}

func location(t *testing.T, w *httptest.ResponseRecorder) url.Values {
	t.Helper()
	u, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return u.Query()
}

func TestNewRequiresAProofDecision(t *testing.T) {
	d := newTestAuthDB(t)
	base := Config{DB: d, Origin: "http://app.test", InstanceKey: "k", Mailer: &captureMailer{}}
	if _, err := New(base); !errors.Is(err, ErrProofUnset) {
		t.Fatalf("no decision = %v, want ErrProofUnset", err)
	}
	bound, err := pow.New(pow.Config{InstanceKey: "k", Nonces: pow.MemoryNonces(), Bind: true, ScriptURL: "/p", WorkerURL: "/w"})
	if err != nil {
		t.Fatal(err)
	}
	c := base
	c.Proof = bound
	if _, err := New(c); !errors.Is(err, ErrProofMode) {
		t.Fatalf("bound guard = %v, want ErrProofMode", err)
	}
	unbound, _ := pow.New(pow.Config{InstanceKey: "k", Nonces: pow.MemoryNonces(), ScriptURL: "/p", WorkerURL: "/w"})
	c = base
	c.Proof, c.ProofOff = unbound, true
	if _, err := New(c); err == nil {
		t.Fatal("Proof and ProofOff both set was accepted")
	}
	c = base
	c.ProofOff = true
	if _, err := New(c); err != nil {
		t.Fatalf("ProofOff = %v", err)
	}
}

func TestBeginRefusesWithoutAChallengeBeforeAnythingElse(t *testing.T) {
	a, m := newProofAuth(t, nil)
	lookups := 0
	a.flow.Classifier.LookupTXT = func(context.Context, string) ([]string, error) {
		lookups++
		return nil, nil
	}
	b := newBrowser()
	for i := 0; i < 30; i++ {
		w := b.do(a.Begin, http.MethodPost, "/signin", url.Values{"address": {"amy@example.com"}})
		if q := location(t, w); q.Get("err") != "check" || q.Get("rec") != "1" {
			t.Fatalf("Begin without a challenge → %s, want err=check&rec=1", w.Header().Get("Location"))
		}
	}
	if m.lastTo != "" || lookups != 0 {
		t.Fatalf("a refused Begin sent mail to %q or made %d DNS lookups", m.lastTo, lookups)
	}
	// Thirty refusals and the per-IP budget is twenty: if refusals spent
	// it, this would be err=rate.
	w := b.do(a.Begin, http.MethodPost, "/signin", filled(t, a, "", url.Values{"address": {"amy@example.com"}, "force": {"1"}}))
	if q := location(t, w); q.Get("err") != "" {
		t.Fatalf("a good Begin after refusals → %s: refusals spent the rate budget", w.Header().Get("Location"))
	}
	if m.lastTo != "amy@example.com" {
		t.Fatalf("mail went to %q", m.lastTo)
	}
}

func TestBeginRecoversAReplayedForm(t *testing.T) {
	// Review focus 1: Back after a successful sign-in restores a page
	// whose token is spent. Continue again must lead somewhere.
	a, m := newProofAuth(t, nil)
	b := newBrowser()
	form := filled(t, a, "", url.Values{"address": {"amy@example.com"}, "force": {"1"}})
	if w := b.do(a.Begin, http.MethodPost, "/signin", form); location(t, w).Get("err") != "" {
		t.Fatal("first submission refused")
	}
	w := b.do(a.Begin, http.MethodPost, "/signin", form)
	q := location(t, w)
	if q.Get("err") != "check" || q.Get("rec") != "1" || q.Get("force") != "1" {
		t.Fatalf("replayed form → %s, want err=check&force=1&rec=1", w.Header().Get("Location"))
	}
	m.lastTo = ""
	if st := a.SigninState(httptest.NewRequest(http.MethodGet, "/signin?err=check&force=1&rec=1", nil)); strings.Contains(string(st.Proof.Fields()), `name="hp"`) {
		t.Fatal("the recovery form carries the honeypot")
	}
	again := filled(t, a, "?err=check&force=1&rec=1", url.Values{"address": {"amy@example.com"}, "force": {"1"}})
	if w := b.do(a.Begin, http.MethodPost, "/signin", again); location(t, w).Get("err") != "" {
		t.Fatalf("recovery submission → %s", w.Header().Get("Location"))
	}
	if m.lastTo != "amy@example.com" {
		t.Fatal("the recovery submission sent no link")
	}
}

func TestBeginKeepsRecAndForceOnLaterErrors(t *testing.T) {
	a, _ := newProofAuth(t, nil)
	b := newBrowser()
	form := filled(t, a, "?rec=1", url.Values{"address": {"not an address"}, "force": {"1"}})
	w := b.do(a.Begin, http.MethodPost, "/signin", form)
	q := location(t, w)
	if q.Get("err") != "address" || q.Get("rec") != "1" || q.Get("force") != "1" {
		t.Fatalf("recovered token, bad address → %s, want err=address with rec and force kept", w.Header().Get("Location"))
	}
}

func TestBeginHoneypotRefusalLeadsToATraplessFormThatWorks(t *testing.T) {
	a, m := newProofAuth(t, nil)
	b := newBrowser()
	form := filled(t, a, "", url.Values{"address": {"amy@example.com"}, "force": {"1"}})
	form.Set("hp", "filled by a password manager")
	w := b.do(a.Begin, http.MethodPost, "/signin", form)
	if q := location(t, w); q.Get("err") != "check" || q.Get("rec") != "1" {
		t.Fatalf("honeypot → %s", w.Header().Get("Location"))
	}
	if m.lastTo != "" {
		t.Fatal("a trapped Begin sent mail")
	}
	again := filled(t, a, "?err=check&rec=1&force=1", url.Values{"address": {"amy@example.com"}, "force": {"1"}})
	if w := b.do(a.Begin, http.MethodPost, "/signin", again); location(t, w).Get("err") != "" {
		t.Fatalf("trapless retry → %s", w.Header().Get("Location"))
	}
}

func TestSigninStateCarriesProofAndForce(t *testing.T) {
	for _, screen := range []bool{false, true} {
		a, _ := newProofAuth(t, func(c *Config) { c.SigninScreen = screen })
		st := a.SigninState(httptest.NewRequest(http.MethodGet, "/signin?err=keymail&force=1", nil))
		if st.Proof == nil || !st.Force {
			t.Fatalf("screen=%v: Proof=%v Force=%v", screen, st.Proof != nil, st.Force)
		}
		rec := a.SigninState(httptest.NewRequest(http.MethodGet, "/signin?err=check&rec=1", nil))
		if rec.Problem != ProblemCheck {
			t.Fatalf("screen=%v: problem = %q, want check", screen, rec.Problem)
		}
	}
	off, _ := newTestAuth(t, nil)
	if st := off.SigninState(httptest.NewRequest(http.MethodGet, "/signin", nil)); st.Proof != nil {
		t.Fatal("ProofOff still renders a challenge")
	}
}
