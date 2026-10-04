package password_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/password"
	"amadan.net/rastrillo/rastrillo/pow"
	"amadan.net/rastrillo/rastrillo/pow/powtest"
)

func newProofEnv(t *testing.T) testEnv {
	t.Helper()
	return newProofEnvWith(t, nil)
}

func newProofEnvWith(t *testing.T, mut func(*password.Config)) testEnv {
	t.Helper()
	g, err := pow.New(pow.Config{InstanceKey: "k", Nonces: pow.MemoryNonces(), Difficulty: 8,
		MinAge: time.Millisecond, ScriptURL: "/pow/pow.js", WorkerURL: "/pow/pow-worker.js"})
	if err != nil {
		t.Fatal(err)
	}
	return newTestEnv(t, func(c *password.Config) {
		c.Proof, c.ProofOff = g, false
		if mut != nil {
			mut(c)
		}
	})
}

func lastPage(t *testing.T, r *renderRecorder) recordedPage {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.calls) == 0 {
		t.Fatal("nothing rendered")
	}
	return r.calls[len(r.calls)-1]
}

// page GETs path and fills its challenge into form, as a browser would.
func page(t *testing.T, env testEnv, h http.HandlerFunc, rec *renderRecorder, form url.Values) url.Values {
	t.Helper()
	w := httptest.NewRecorder()
	h(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q on a page carrying a single-use token, want no-store", cc)
	}
	d := lastPage(t, rec).data
	if d.Proof == nil {
		t.Fatal("PageData carries no challenge although Config.Proof is set")
	}
	time.Sleep(2 * time.Millisecond)
	return powtest.Fill(t, []byte(d.Proof.Fields()), form)
}

func postTo(h http.HandlerFunc, form url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

func TestNewRequiresAProofDecision(t *testing.T) {
	base := password.Config{Sessions: newTestSessions(t), Lookup: newUserStore().lookup, RenderSignin: (&renderRecorder{}).render}
	if _, err := password.New(base); !errors.Is(err, password.ErrProofUnset) {
		t.Fatalf("no decision = %v, want ErrProofUnset", err)
	}
	bound, _ := pow.New(pow.Config{InstanceKey: "k", Nonces: pow.MemoryNonces(), Bind: true, ScriptURL: "/p", WorkerURL: "/w"})
	c := base
	c.Proof = bound
	if _, err := password.New(c); !errors.Is(err, password.ErrProofMode) {
		t.Fatalf("bound = %v, want ErrProofMode", err)
	}
}

func TestSignupWithoutAChallengeCreatesNoRow(t *testing.T) {
	env := newProofEnv(t)
	w := postTo(env.h.Signup, url.Values{"email": {"new@example.com"}, "password": {"long enough pw"}})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422", w.Code)
	}
	if _, _, err := env.store.lookup(context.Background(), "new@example.com"); err == nil {
		t.Fatal("a refused signup created a user")
	}
	if p := lastPage(t, env.signup); p.data.Proof == nil || strings.Contains(string(p.data.Proof.Fields()), `name="hp"`) {
		t.Fatal("a refused signup re-rendered without a trapless recovery challenge")
	}
}

func TestSignupWithAChallengeSucceeds(t *testing.T) {
	env := newProofEnv(t)
	form := page(t, env, env.h.SignupPage, env.signup, url.Values{"email": {"new@example.com"}, "password": {"long enough pw"}})
	if w := postTo(env.h.Signup, form); w.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303", w.Code)
	}
}

// TestWrongPasswordRerendersAFreshChallengeAndKeepsRecovery: both
// solved posts must get past the front door and reach Lookup. A refusal
// also answers 422 with a fresh trapless challenge, so without the
// message and the Lookup count this would pass with recovery broken.
func TestWrongPasswordRerendersAFreshChallengeAndKeepsRecovery(t *testing.T) {
	var lookups atomic.Int32
	env := newProofEnvWith(t, func(c *password.Config) {
		inner := c.Lookup
		c.Lookup = func(ctx context.Context, email string) (int64, string, error) {
			lookups.Add(1)
			return inner(ctx, email)
		}
	})
	const wrong, refused = "Wrong email or password.", "Your browser couldn't finish a security check. Try again."
	admitted := func(step string, w *httptest.ResponseRecorder, wantLookups int32) password.PageData {
		t.Helper()
		d := lastPage(t, env.signin).data
		if w.Code != http.StatusUnprocessableEntity || d.Error != wrong || lookups.Load() != wantLookups {
			t.Fatalf("%s: status %d, Error %q, %d lookups; want 422, %q, %d: the solved post was not admitted",
				step, w.Code, d.Error, lookups.Load(), wrong, wantLookups)
		}
		return d
	}
	hash, err := password.Hash("right password")
	if err != nil {
		t.Fatal(err)
	}
	env.store.create(context.Background(), "amy@example.com", hash)
	form := page(t, env, env.h.SigninPage, env.signin, url.Values{"email": {"amy@example.com"}, "password": {"wrong"}})
	w := postTo(env.h.Signin, form)
	d := admitted("wrong password", w, 1)
	if d.Proof == nil || d.Proof.Nonce == form.Get("pow_nonce") {
		t.Fatalf("wrong password: fresh challenge %v", d.Proof != nil)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("a re-render carrying a token is cacheable")
	}
	// A refusal hands back a recovery form; a wrong password on THAT
	// form must be admitted and hand back a recovery form again.
	postTo(env.h.Signin, url.Values{"email": {"amy@example.com"}, "password": {"x"}})
	if d := lastPage(t, env.signin).data; d.Error != refused || lookups.Load() != 1 {
		t.Fatalf("a proofless post: Error %q, %d lookups; want the front door's refusal before Lookup", d.Error, lookups.Load())
	}
	time.Sleep(2 * time.Millisecond)
	rec := lastPage(t, env.signin).data.Proof
	again := powtest.Fill(t, []byte(rec.Fields()), url.Values{"email": {"amy@example.com"}, "password": {"wrong again"}})
	d = admitted("wrong password on the recovery form", postTo(env.h.Signin, again), 2)
	if strings.Contains(string(d.Proof.Fields()), `name="hp"`) {
		t.Fatal("a wrong password after a recovery brought the trap back")
	}
}
