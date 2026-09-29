package auth

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/crypto"
	"amadan.net/rastrillo/rastrillo/internal/sealedcookie"
	"amadan.net/rastrillo/rastrillo/lastsignin"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// rememberAs gives b a remembered-method cookie from a's jar.
func rememberAs(a *Auth, b *browser, method, address string) {
	w := httptest.NewRecorder()
	a.jar.Remember(w, lastsignin.Record{Method: method, Address: address})
	b.apply(w)
}

func stateAt(a *Auth, b *browser, target string) SigninState {
	return a.SigninState(b.request(http.MethodGet, target, nil))
}

// prepared is what PrepareSigninResponse writes for st, as setCookies
// names it.
func prepared(a *Auth, st SigninState) []string {
	w := httptest.NewRecorder()
	a.PrepareSigninResponse(w, st)
	return setCookies(w)
}

func TestSigninStateMapsTheQuery(t *testing.T) {
	a, _ := newScreenAuth(t, nil)
	for _, c := range []struct {
		query   string
		step    SigninStep
		problem SigninProblem
	}{
		{"", StepAsk, ProblemNone},
		{"?sent=1", StepSent, ProblemNone},
		{"?err=rate", StepAsk, ProblemRate},
		{"?err=address", StepAsk, ProblemAddress},
		{"?err=expired", StepAsk, ProblemExpired},
		{"?force=1&err=keymail", StepAsk, ProblemKeymail},
		{"?err=1", StepAsk, ProblemGeneric},
		{"?err=nonsense", StepAsk, ProblemNone},
		{"?reauth=1", StepAsk, ProblemReauth},
		{"?continue=x", StepAsk, ProblemExpired},
		{"?continue=", StepAsk, ProblemExpired},
		{"?continue=x&sent=1", StepAsk, ProblemExpired},
		{"?sent=1&err=rate", StepSent, ProblemNone},
		{"?err=rate&reauth=1", StepAsk, ProblemRate},
		{"?err=nonsense&reauth=1", StepAsk, ProblemReauth},
	} {
		st := stateAt(a, newBrowser(), "/signin"+c.query)
		if st.Step != c.step || st.Problem != c.problem {
			t.Errorf("%q → %s/%q, want %s/%q", c.query, st.Step, st.Problem, c.step, c.problem)
		}
		if st.BeginPath != "/signin" || st.ForgetPath != "/signin/forget" {
			t.Errorf("%q: paths %q %q", c.query, st.BeginPath, st.ForgetPath)
		}
	}

	b := newBrowser()
	rememberAs(a, b, "magiclink", "ada@example.com")
	for _, c := range []struct {
		query   string
		step    SigninStep
		problem SigninProblem
	}{
		{"", StepReturning, ProblemNone},
		{"?reauth=1", StepReturning, ProblemReauth},
		{"?err=rate", StepAsk, ProblemRate},
	} {
		if st := stateAt(a, b, "/signin"+c.query); st.Step != c.step || st.Problem != c.problem ||
			st.Remembered == nil || *st.Remembered != (Remembered{"magiclink", "ada@example.com"}) {
			t.Errorf("remembered, %q → %+v", c.query, st)
		}
	}
}

func TestContinueComesFromTheCookieOnly(t *testing.T) {
	a, _ := newKeymailScreen(t, nil)
	b := newBrowser()
	id, _ := startKeymail(t, a, b)
	st := stateAt(a, b, "/signin?continue="+id)
	if st.Step != StepContinue || !strings.HasPrefix(st.ContinueURL, "https://keymail.test/oauth/authorize?") {
		t.Fatalf("a live continuation → %+v", st)
	}
	expired := func(what string, b *browser, id string) {
		t.Helper()
		if st := stateAt(a, b, "/signin?continue="+url.QueryEscape(id)); st.Step != StepAsk || st.Problem != ProblemExpired || st.ContinueURL != "" {
			t.Fatalf("%s → %+v, want Ask/Expired with no URL", what, st)
		}
	}
	expired("?continue=<a URL>", b, "https://evil.test/")

	// The page resolves through the same binding Callback relies on: a
	// continuation whose pending cookie is gone describes an attempt
	// that can no longer complete, so offering its URL would only send
	// the visitor to Keymail and back to Expired.
	noPending := b.clone()
	delete(noPending.jar, "rastrillo_pending")
	expired("a continuation without its pending cookie", noPending, id)

	// And through the predicate, on the way out as well as the way in.
	bad, value, err := a.sealContinuation("https://keymail.test/evil/oauth/authorize?x=1", b.cookie("rastrillo_pending").Value)
	if err != nil {
		t.Fatal(err)
	}
	sealedBad := b.clone()
	sealedBad.jar["rastrillo_continue"] = &http.Cookie{Name: "rastrillo_continue", Value: value}
	expired("a sealed URL failing the predicate", sealedBad, bad)
}

// TestAStaleContinueReadsExpiredBesideALiveOne is §5's keymail A,
// keymail B, magic link C at the page, not only at the callback: tab A
// reloading its ?continue= must not navigate to A's authorize URL once
// B's pair is installed, and C — a link, which replaces the attempt but
// not the pair — must not take B's continuation away.
func TestAStaleContinueReadsExpiredBesideALiveOne(t *testing.T) {
	a, _ := newKeymailScreen(t, nil)
	b := newBrowser()
	idA, stateA := startKeymail(t, a, b)
	idB, stateB := startKeymail(t, a, b)
	w := b.do(a.Begin, http.MethodPost, "/signin", url.Values{"address": {"ada@example.com"}, "force": {"1"}})
	if !strings.HasPrefix(w.Header().Get("Location"), "/signin?sent=1&attempt=") {
		t.Fatalf("magic link C → %q", w.Header().Get("Location"))
	}

	if st := stateAt(a, b, "/signin?continue="+idA); st.Step != StepAsk || st.Problem != ProblemExpired || st.ContinueURL != "" {
		t.Fatalf("A's stale ?continue= → %+v, want Ask/Expired with no URL", st)
	}
	st := stateAt(a, b, "/signin?continue="+idB)
	if st.Step != StepContinue || stateOf(t, st.ContinueURL) != stateB || stateB == stateA {
		t.Fatalf("B's ?continue= after C → %+v, want B's own authorize URL", st)
	}
	if got := prepared(a, st); len(got) != 0 {
		t.Fatalf("B's live Continue page deleted %v", got)
	}
}

func TestTheAddressIsNeverFromTheQuery(t *testing.T) {
	a, _ := newScreenAuth(t, nil)
	b := newBrowser()
	if st := stateAt(a, b, "/signin?address=mallory@example.com&err=address"); st.Address != "" {
		t.Fatalf("Address %q came from the query", st.Address)
	}
	rememberAs(a, b, "magiclink", "ada@example.com")
	if st := stateAt(a, b, "/signin"); st.Address != "ada@example.com" {
		t.Fatalf("with only a remembered address, Address = %q", st.Address)
	}
	b.do(a.Begin, http.MethodPost, "/signin", url.Values{"address": {"not an address"}, "force": {"1"}})
	if st := stateAt(a, b, "/signin?err=address"); st.Address != "not an address" {
		t.Fatalf("the attempt did not win over the remembered address: %q", st.Address)
	}
}

func TestSentNamesOnlyTheBoundAttempt(t *testing.T) {
	a, _ := newScreenAuth(t, nil)
	b := newBrowser()
	rememberAs(a, b, "magiclink", "remembered@example.com")
	w := b.do(a.Begin, http.MethodPost, "/signin", url.Values{"address": {"ada@example.com"}, "force": {"1"}, "expect": {"keymail"}})
	loc := w.Header().Get("Location")
	if st := stateAt(a, b, loc); st.Step != StepSent || st.SentTo != "ada@example.com" || !st.SentInstead {
		t.Fatalf("bound Sent → %+v", st)
	}
	if st := stateAt(a, b, "/signin?sent=1&attempt=someone-elses"); st.SentTo != "" || st.SentInstead {
		t.Fatalf("an unbound Sent named %q; Sent never guesses", st.SentTo)
	}
	if st := stateAt(a, b, "/signin?sent=1"); st.SentTo != "" {
		t.Fatalf("a Sent page with no attempt named %q — the remembered address is no evidence of where this link went", st.SentTo)
	}
	b.do(a.Begin, http.MethodPost, "/signin", url.Values{"address": {"not an address"}, "force": {"1"}})
	if st := stateAt(a, b, loc); st.SentTo != "" {
		t.Fatalf("a problem attempt bound a Sent page: %q", st.SentTo)
	}
}

// TestSentConsultsNothing is the enumeration argument (§2): the Sent
// page is computed from the query and this browser's own cookies, so it
// cannot depend on whether an address is known, admitted or keymail.
func TestSentConsultsNothing(t *testing.T) {
	a, _ := newScreenAuth(t, func(c *Config) {
		c.Authorize = func(string) bool { t.Error("SigninState called Authorize"); return false }
	})
	a.flow.Classifier.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Errorf("SigninState reached the classifier: %s", r.URL)
		return nil, errors.New("no")
	})}
	a.flow.Mailer = failingMailer{}
	b := newBrowser()
	w := b.do(a.AnswerAsSent, http.MethodPost, "/signin", url.Values{"address": {"ada@example.com"}})
	a.cfg.DB.Close()
	if st := stateAt(a, b, w.Header().Get("Location")); st.Step != StepSent || st.SentTo != "ada@example.com" {
		t.Fatalf("Sent with nothing behind it → %+v", st)
	}
}

// freshAdvisory gives a test its own process-wide once, and puts the
// real one back after: other tests in this binary call SigninState with
// the screen off and would otherwise have spent it already.
func freshAdvisory(t *testing.T) {
	t.Helper()
	saved := advisoryOnce
	advisoryOnce = new(sync.Once)
	t.Cleanup(func() { advisoryOnce = saved })
}

func TestTheAdvisoryFiresOnceAndOnlyWithTheScreenOff(t *testing.T) {
	freshAdvisory(t)
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	off, _ := newTestAuth(t, func(c *Config) { c.Logger = logger })
	another, _ := newTestAuth(t, func(c *Config) { c.Logger = logger })
	off.SigninState(newBrowser().request(http.MethodGet, "/signin", nil))
	off.SigninState(newBrowser().request(http.MethodGet, "/signin", nil))
	another.SigninState(newBrowser().request(http.MethodGet, "/signin", nil))
	if n := strings.Count(logs.String(), "SigninScreen is off"); n != 1 {
		t.Fatalf("the advisory logged %d times across two Auths, want once per process:\n%s", n, logs.String())
	}
	// The spec's sentence, whole: it names the condition under which
	// keymail is stuck, so an operator whose app handles keymail itself
	// can read it and know it does not apply.
	const condition = "SigninScreen is off: under the default CSP, with no continuation of your own, " +
		"a keymail address cannot leave the sign-in form, and nothing is remembered"
	if !strings.Contains(logs.String(), condition) || !strings.Contains(logs.String(), "level=WARN") {
		t.Fatalf("the advisory is not the spec's warning:\n%s", logs.String())
	}
	if strings.Contains(logs.String(), "misconfigured") {
		t.Fatal("the advisory must state a condition, never call the app misconfigured: it cannot know")
	}
	logs.Reset()
	freshAdvisory(t)
	on, _ := newScreenAuth(t, func(c *Config) { c.Logger = logger })
	on.SigninState(newBrowser().request(http.MethodGet, "/signin", nil))
	if strings.Contains(logs.String(), "SigninScreen is off") {
		t.Fatal("the advisory fired with the screen on")
	}
}

// TestWithTheScreenOffCookiesAreIgnoredNotCleared covers all three of
// the screen's cookies (§5), each one valid for a screen-on Auth with
// the same key and origin — so being ignored here is the switch's
// doing, not the cookie's.
func TestWithTheScreenOffCookiesAreIgnoredNotCleared(t *testing.T) {
	on, _ := newKeymailScreen(t, nil)
	b := newBrowser()
	id, _ := startKeymail(t, on, b)
	w := b.do(on.Begin, http.MethodPost, "/signin", url.Values{"address": {"ada@example.com"}, "force": {"1"}})
	sent := w.Header().Get("Location")
	rememberAs(on, b, "magiclink", "ada@example.com")
	for _, name := range []string{"rastrillo_attempt", "rastrillo_continue", "rastrillo_pending", "rastrillo_last_signin"} {
		if b.cookie(name) == nil {
			t.Fatalf("setup: the browser holds no %s", name)
		}
	}
	if st := stateAt(on, b, "/signin?continue="+id); st.Step != StepContinue {
		t.Fatalf("control: the screen-on Auth does not resolve the continuation: %+v", st)
	}
	if st := stateAt(on, b, sent); st.SentTo != "ada@example.com" || st.Remembered == nil {
		t.Fatalf("control: the screen-on Auth does not read the attempt and remembered cookies: %+v", st)
	}

	off, _ := newTestAuth(t, nil)
	for _, target := range []string{sent, "/signin?continue=" + id, "/signin"} {
		st := stateAt(off, b, target)
		if st.Address != "" || st.SentTo != "" || st.SentInstead || st.Remembered != nil || st.ContinueURL != "" ||
			st.Step == StepContinue || len(st.clear) != 0 {
			t.Fatalf("%s: screen off read the screen's cookies: %+v", target, st)
		}
		if got := prepared(off, st); len(got) != 0 {
			t.Fatalf("%s: screen off wrote %v", target, got)
		}
	}
}

func TestPrepareSigninResponse(t *testing.T) {
	a, _ := newScreenAuth(t, nil)
	for _, step := range []SigninStep{StepAsk, StepReturning, StepSent, StepContinue} {
		w := httptest.NewRecorder()
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		a.PrepareSigninResponse(w, SigninState{Step: step})
		if got := w.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s: Cache-Control %q; a page carrying an address must not be cached", step, got)
		}
		want := "strict-origin-when-cross-origin"
		if step == StepContinue {
			want = "no-referrer"
		}
		if got := w.Header().Get("Referrer-Policy"); got != want {
			t.Errorf("%s: Referrer-Policy %q, want %q", step, got, want)
		}
	}
}

// TestCookiesFromAnotherKeyAreIgnoredAndCleared is Review Focus 3: a
// rotated InstanceKey, or a changed Origin on the same host (cookies do
// not isolate by port), turns every screen cookie into noise, and noise
// is cleared quietly — no 500, no error callout, no stale one-tap.
func TestCookiesFromAnotherKeyAreIgnoredAndCleared(t *testing.T) {
	for name, mut := range map[string]func(*Config){
		"a rotated InstanceKey": func(c *Config) { c.InstanceKey = "the-old-key" },
		"a changed Origin":      func(c *Config) { c.Origin = "http://app.test:8080" },
	} {
		t.Run(name, func(t *testing.T) {
			old, _ := newKeymailScreen(t, mut)
			b := newBrowser()
			id, _ := startKeymail(t, old, b)
			rememberAs(old, b, "magiclink", "ada@example.com")

			a, _ := newScreenAuth(t, nil)
			st := stateAt(a, b, "/signin")
			if st.Step != StepAsk || st.Problem != ProblemNone || st.Remembered != nil || st.Address != "" ||
				st.Door() != "ask" || st.Focus() != "field" {
				t.Fatalf("→ %+v (door %s, focus %s), want a plain Ask", st, st.Door(), st.Focus())
			}
			// pending is signin's blob, sealed by the library under its own
			// key; the page neither judges nor deletes it — Callback does.
			want := []string{"rastrillo_attempt:clear", "rastrillo_continue:clear", "rastrillo_last_signin:clear"}
			if got := prepared(a, st); !reflect.DeepEqual(got, want) {
				t.Fatalf("PrepareSigninResponse wrote %v, want exactly %v", got, want)
			}
			// The old tab reloading its ?continue= is Expired, not a
			// navigation to the old authorize URL, and still cleans up.
			st = stateAt(a, b, "/signin?continue="+id)
			if st.Step != StepAsk || st.Problem != ProblemExpired || st.ContinueURL != "" {
				t.Fatalf("the old ?continue= → %+v", st)
			}
			if got := prepared(a, st); !reflect.DeepEqual(got, want) {
				t.Fatalf("on the old ?continue=, PrepareSigninResponse wrote %v, want %v", got, want)
			}
		})
	}
}

// Each way a remembered cookie can be unbelievable, through the page
// path the app actually runs: SigninState ignores it — no Returning, no
// prefill — and PrepareSigninResponse deletes it and nothing else.
func TestABadRememberedCookieIsIgnoredAndCleared(t *testing.T) {
	a, _ := newScreenAuth(t, nil)
	now := time.Now()
	jarKey := crypto.Derive([]byte("test-instance-key"), "rastrillo/lastsignin/v1")
	seal := func(key []byte, p any) string {
		v, err := sealedcookie.Seal(key, p)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	// The jar's payload shape, restated: lastsignin keeps its own
	// unexported, and a test that could only use the public API could
	// not seal a well-formed record for the wrong origin or method.
	type payload struct {
		O   string `json:"o"`
		M   string `json:"m"`
		A   string `json:"a,omitempty"`
		IAT int64  `json:"iat"`
		EXP int64  `json:"exp"`
	}
	good := payload{O: a.cfg.Origin, M: "magiclink", A: "ada@example.com", IAT: now.Unix(), EXP: now.Unix() + 3600}
	expired, wrongOrigin, future, unknown := good, good, good, good
	expired.IAT, expired.EXP = now.Unix()-7200, now.Unix()-3600
	wrongOrigin.O = "http://other.test"
	future.IAT = now.Unix() + 600
	unknown.M = "password"
	for name, value := range map[string]string{
		"tampered":                     flipByte(t, seal(jarKey, good)),
		"expired":                      seal(jarKey, expired),
		"for another origin":           seal(jarKey, wrongOrigin),
		"issued in the future":         seal(jarKey, future),
		"an unknown method":            seal(jarKey, unknown),
		"under another InstanceKey":    seal(crypto.Derive([]byte("another-key"), "rastrillo/lastsignin/v1"), good),
		"under the attempt's key":      seal(a.attemptKey, good),
		"of an unknown version":        "v2." + strings.TrimPrefix(seal(jarKey, good), "v1."),
		"not an envelope":              "remember-me",
		"a passkey with an address":    seal(jarKey, payload{O: a.cfg.Origin, M: "passkey", A: "ada@example.com", IAT: now.Unix(), EXP: now.Unix() + 3600}),
		"a magic link with no address": seal(jarKey, payload{O: a.cfg.Origin, M: "magiclink", IAT: now.Unix(), EXP: now.Unix() + 3600}),
	} {
		b := newBrowser()
		b.jar["rastrillo_last_signin"] = &http.Cookie{Name: "rastrillo_last_signin", Value: value}
		st := stateAt(a, b, "/signin")
		if st.Step != StepAsk || st.Problem != ProblemNone || st.Remembered != nil || st.Address != "" {
			t.Errorf("%s: → %+v, want a plain Ask", name, st)
		}
		if got := prepared(a, st); !reflect.DeepEqual(got, []string{"rastrillo_last_signin:clear"}) {
			t.Errorf("%s: Set-Cookie %v, want exactly the remembered cookie deleted", name, got)
		}
	}
	// The control: a good one reads as Returning and is left alone.
	b := newBrowser()
	b.jar["rastrillo_last_signin"] = &http.Cookie{Name: "rastrillo_last_signin", Value: seal(jarKey, good)}
	st := stateAt(a, b, "/signin")
	if got := prepared(a, st); st.Step != StepReturning || len(got) != 0 {
		t.Fatalf("a good remembered cookie → %+v, Set-Cookie %v", st, got)
	}
}

func TestRememberFalseClearsOnTheSigninPage(t *testing.T) {
	on, _ := newScreenAuth(t, nil)
	b := newBrowser()
	rememberAs(on, b, "magiclink", "ada@example.com")
	off, _ := newScreenAuth(t, func(c *Config) { c.Remember = ptr(false) })
	st := stateAt(off, b, "/signin")
	if st.Remembered != nil || st.Step != StepAsk || st.Address != "" {
		t.Fatalf("Remember=false read a remembered cookie: %+v", st)
	}
	if got := prepared(off, st); !reflect.DeepEqual(got, []string{"rastrillo_last_signin:clear"}) {
		t.Fatalf("Set-Cookie %v, want the remembered cookie deleted", got)
	}
}

// TestARememberedPasskeyWithNoDoorIsTheEmailForm is Review Focus 5,
// from the cookie up: an app that has not wired passkey discovery must
// not show a dead passkey door to a browser that last used one.
func TestARememberedPasskeyWithNoDoorIsTheEmailForm(t *testing.T) {
	a, _ := newScreenAuth(t, nil)
	b := newBrowser()
	rememberAs(a, b, "passkey", "")
	st := stateAt(a, b, "/signin")
	if st.Step != StepReturning || st.Remembered == nil || *st.Remembered != (Remembered{Method: "passkey"}) || st.Address != "" {
		t.Fatalf("a remembered passkey → %+v", st)
	}
	if st.Door() != "ask" || st.Focus() != "field" {
		t.Fatalf("no passkey door wired → door %q focus %q, want the email form, focused", st.Door(), st.Focus())
	}
	st.Passkey = &PasskeyDoor{BeginPath: "/b", FinishPath: "/f", ModuleURL: "/m.mjs", ScriptURL: "/s.mjs"}
	if st.Door() != "passkey" || st.Focus() != "" {
		t.Fatalf("with a door → door %q focus %q", st.Door(), st.Focus())
	}
}

func TestForget(t *testing.T) {
	a, _ := newScreenAuth(t, nil)
	b := newBrowser()
	b.do(a.Begin, http.MethodPost, "/signin", url.Values{"address": {"ada@example.com"}, "force": {"1"}})
	rememberAs(a, b, "magiclink", "ada@example.com")

	w := b.do(a.Forget, http.MethodGet, "/signin/forget", nil)
	if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != http.MethodPost || len(setCookies(w)) != 0 {
		t.Fatalf("GET Forget → %d, Allow %q, %v; a state change on GET is prefetchable", w.Code, w.Header().Get("Allow"), setCookies(w))
	}
	r := b.request(http.MethodPost, "/signin/forget", url.Values{})
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	cw := httptest.NewRecorder()
	a.Forget(cw, r)
	if cw.Code != http.StatusForbidden || len(setCookies(cw)) != 0 {
		t.Fatalf("cross-site Forget → %d, %v", cw.Code, setCookies(cw))
	}
	w = b.do(a.Forget, http.MethodPost, "/signin/forget", url.Values{})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/signin" {
		t.Fatalf("Forget → %d %q", w.Code, w.Header().Get("Location"))
	}
	if got, want := setCookies(w), []string{"rastrillo_attempt:clear", "rastrillo_last_signin:clear"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Forget wrote %v, want exactly %v", got, want)
	}
	if st := stateAt(a, b, "/signin"); st.Step != StepAsk || st.Address != "" || st.Remembered != nil {
		t.Fatalf("after Forget → %+v, want a blank Ask", st)
	}

	off, _ := newTestAuth(t, nil)
	if w := b.do(off.Forget, http.MethodPost, "/signin/forget", url.Values{}); w.Code != http.StatusSeeOther || len(setCookies(w)) != 0 {
		t.Fatalf("screen-off Forget → %d, %v; with the screen off auth writes none of its cookies", w.Code, setCookies(w))
	}
}

// TestAKeymailOneTapThatEndsInALinkSaysSo is §1.3's "honest surprise"
// with round 4's correction: the Sent line states the outcome — a link
// went out this time — never a cause the screen cannot know.
func TestAKeymailOneTapThatEndsInALinkSaysSo(t *testing.T) {
	oneTap := url.Values{"address": {"kay@example.org"}, "expect": {"keymail"}}
	sent := func(t *testing.T, a *Auth, b *browser, w *httptest.ResponseRecorder) SigninState {
		t.Helper()
		loc := w.Header().Get("Location")
		if !strings.HasPrefix(loc, "/signin?sent=1&attempt=") {
			t.Fatalf("→ %q, want a bound Sent page", loc)
		}
		return stateAt(a, b, loc)
	}
	t.Run("the server stopped answering", func(t *testing.T) {
		a, f := newKeymailScreen(t, nil)
		f.servers["keymail.test"] = false
		b := newBrowser()
		if st := sent(t, a, b, b.do(a.Begin, http.MethodPost, "/signin", oneTap)); !st.SentInstead || st.SentTo != "kay@example.org" {
			t.Fatalf("%+v", st)
		}
	})
	t.Run("the list now excludes it", func(t *testing.T) {
		a, _ := newKeymailScreen(t, listed("other.test"))
		b := newBrowser()
		if st := sent(t, a, b, b.do(a.Begin, http.MethodPost, "/signin", oneTap)); !st.SentInstead {
			t.Fatalf("%+v", st)
		}
	})
	t.Run("an admission wrapper refused it", func(t *testing.T) {
		a, _ := newKeymailScreen(t, nil)
		b := newBrowser()
		if st := sent(t, a, b, b.do(a.AnswerAsSent, http.MethodPost, "/signin", oneTap)); !st.SentInstead {
			t.Fatalf("%+v; the wrapper's refusal must read exactly like Begin's link", st)
		}
	})
	t.Run("typed, with no expect", func(t *testing.T) {
		a, f := newKeymailScreen(t, nil)
		f.servers["keymail.test"] = false
		b := newBrowser()
		if st := sent(t, a, b, b.do(a.Begin, http.MethodPost, "/signin", url.Values{"address": {"kay@example.org"}})); st.SentInstead {
			t.Fatalf("%+v", st)
		}
	})
	t.Run("expect never chooses the path", func(t *testing.T) {
		a, _ := newKeymailScreen(t, nil)
		w := newBrowser().do(a.Begin, http.MethodPost, "/signin", oneTap)
		if !strings.HasPrefix(w.Header().Get("Location"), "/signin?continue=") {
			t.Fatalf("a keymail address with expect → %q, want keymail", w.Header().Get("Location"))
		}
	})
}

// TestAnAdmissionWrapperIsNoOracle composes a wrapper the way fichas
// does and checks what a visitor sees is identical, page included, for
// an admitted and a refused address, in both modes.
func TestAnAdmissionWrapperIsNoOracle(t *testing.T) {
	for _, screen := range []bool{false, true} {
		a, m := newTestAuth(t, func(c *Config) { c.SigninScreen = screen })
		f := newKeymailFake()
		wireKeymail(a, f)
		wrapper := func(admit bool) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				if !admit {
					a.AnswerAsSent(w, r)
					return
				}
				a.Begin(w, r)
			}
		}
		form := url.Values{"address": {"ada@example.com"}, "expect": {"keymail"}}
		ab, rb := newBrowser(), newBrowser()
		admitted := ab.do(wrapper(true), http.MethodPost, "/signin", form)
		m.forget()
		f.seen = nil
		refused := rb.do(wrapper(false), http.MethodPost, "/signin", form)
		if m.sentTo() != "" || len(f.seen) != 0 {
			t.Fatalf("screen=%v: AnswerAsSent sent mail (%q) or classified (%v)", screen, m.sentTo(), f.seen)
		}
		sa := stateAt(a, ab, admitted.Header().Get("Location"))
		sr := stateAt(a, rb, refused.Header().Get("Location"))
		if admitted.Code != refused.Code || redirectShape(admitted) != redirectShape(refused) ||
			strings.Join(cookieShape(admitted), "|") != strings.Join(cookieShape(refused), "|") ||
			sa.Step != sr.Step || sa.SentTo != sr.SentTo || sa.SentInstead != sr.SentInstead {
			t.Fatalf("screen=%v: admitted %+v / %q vs refused %+v / %q", screen, sa, redirectShape(admitted), sr, redirectShape(refused))
		}
		if screen && sa.SentTo != "ada@example.com" {
			t.Fatalf("screen on: SentTo %q", sa.SentTo)
		}
		if !screen && (sa.SentTo != "" || admitted.Header().Get("Location") != "/signin?sent=1") {
			t.Fatalf("screen off: %+v %q, want today's plain ?sent=1", sa, admitted.Header().Get("Location"))
		}
	}
}

// TestAKeymailProblemKeepsTheAddressForTheEscapeHatch is Review Focus 4.
func TestAKeymailProblemKeepsTheAddressForTheEscapeHatch(t *testing.T) {
	a, f := newKeymailScreen(t, nil)
	b := newBrowser()
	_, state := startKeymail(t, a, b)
	f.servers["keymail.test"] = false
	w := callback(a, b, state)
	loc := w.Header().Get("Location")
	if loc != "/signin?force=1&err=keymail" {
		t.Fatalf("a failed exchange → %q", loc)
	}
	st := stateAt(a, b, loc)
	if st.Problem != ProblemKeymail || st.Address != "kay@example.org" || st.Door() != "ask" || st.Focus() != "callout" {
		t.Fatalf("after a failed keymail approval → %+v (door %s, focus %s)", st, st.Door(), st.Focus())
	}
}

func TestDoorAndFocusFollowTheMatrix(t *testing.T) {
	door := &PasskeyDoor{BeginPath: "/b", FinishPath: "/f", ModuleURL: "/m.mjs"}
	kay := &Remembered{"keymail", "kay@example.org"}
	ada := &Remembered{"magiclink", "ada@example.com"}
	pk := &Remembered{"passkey", ""}
	for _, c := range []struct {
		name        string
		st          SigninState
		door, focus string
	}{
		{"ask", SigninState{Step: StepAsk}, "ask", "field"},
		{"ask, passkey door", SigninState{Step: StepAsk, Passkey: door}, "ask", "field"},
		{"returning keymail", SigninState{Step: StepReturning, Remembered: kay}, "keymail", "onetap"},
		{"returning link", SigninState{Step: StepReturning, Remembered: ada}, "link", "onetap"},
		{"returning passkey", SigninState{Step: StepReturning, Remembered: pk, Passkey: door}, "passkey", ""},
		{"returning passkey, no door", SigninState{Step: StepReturning, Remembered: pk}, "ask", "field"},
		{"sent", SigninState{Step: StepSent}, "sent", ""},
		{"continue", SigninState{Step: StepContinue}, "continue", ""},
		{"address", SigninState{Step: StepAsk, Problem: ProblemAddress}, "ask", "field"},
		{"rate", SigninState{Step: StepAsk, Problem: ProblemRate}, "ask", "callout"},
		{"expired", SigninState{Step: StepAsk, Problem: ProblemExpired}, "ask", "callout"},
		{"keymail", SigninState{Step: StepAsk, Problem: ProblemKeymail}, "ask", "callout"},
		{"generic", SigninState{Step: StepAsk, Problem: ProblemGeneric}, "ask", "callout"},
		{"reauth, ask", SigninState{Step: StepAsk, Problem: ProblemReauth}, "ask", "field"},
		{"reauth, returning keymail", SigninState{Step: StepReturning, Problem: ProblemReauth, Remembered: kay}, "keymail", "onetap"},
		{"reauth, returning passkey", SigninState{Step: StepReturning, Problem: ProblemReauth, Remembered: pk, Passkey: door}, "passkey", ""},
	} {
		if d, f := c.st.Door(), c.st.Focus(); d != c.door || f != c.focus {
			t.Errorf("%s: door %q focus %q, want %q %q", c.name, d, f, c.door, c.focus)
		}
	}
}
