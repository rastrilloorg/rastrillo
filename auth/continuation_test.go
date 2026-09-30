package auth

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/keymaildev/signin"

	"amadan.net/rastrillo/rastrillo/internal/sealedcookie"
	"amadan.net/rastrillo/rastrillo/lastsignin"
)

// flipByte tampers with a sealed value the only reliable way: decode it,
// flip one bit of the ciphertext, re-encode. Editing the base64 text can
// leave the decoded bytes unchanged.
func flipByte(t *testing.T, v string) string {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(v, "v1."))
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)/2] ^= 0x01
	return "v1." + base64.RawURLEncoding.EncodeToString(raw)
}

// newKeymailScreen is the screen with kay's keymail server wired in.
func newKeymailScreen(t *testing.T, mut func(*Config)) (*Auth, *keymailFake) {
	t.Helper()
	a, _ := newScreenAuth(t, mut)
	f := kayFake()
	wireKeymail(a, f)
	return a, f
}

// startKeymail begins kay's sign-in in b and returns the continuation
// id and the authorize URL's state.
func startKeymail(t *testing.T, a *Auth, b *browser) (id, state string) {
	t.Helper()
	w := b.do(a.Begin, http.MethodPost, "/signin", url.Values{"address": {"kay@example.org"}})
	id, ok := strings.CutPrefix(w.Header().Get("Location"), "/signin?continue=")
	if !ok || id == "" {
		t.Fatalf("keymail Begin → %q, want /signin?continue=<id>", w.Header().Get("Location"))
	}
	u, ok := a.continuationFor(b.request(http.MethodGet, "/signin?continue="+id, nil), id)
	if !ok {
		t.Fatal("the continuation Begin just wrote does not resolve")
	}
	return id, stateOf(t, u)
}

func callback(a *Auth, b *browser, state string) *httptest.ResponseRecorder {
	return b.do(a.Callback, http.MethodGet, "/auth/callback?code=c&state="+url.QueryEscape(state), nil)
}

// install copies the named cookies from one browser into another: how a
// test stages "the pair this browser installed last".
func install(to, from *browser, names ...string) {
	for _, n := range names {
		to.jar[n] = from.jar[n]
	}
}

var pair = []string{"rastrillo_pending", "rastrillo_continue"}

func TestAuthorizeURLPredicate(t *testing.T) {
	a, _ := newTestAuth(t, nil)
	build := func(origin, redirectPath string, stepUp bool) string {
		k := &signin.Keymail{Base: "https://keymail.test", Origin: origin, RedirectPath: redirectPath}
		return k.AuthorizeURL(strings.Repeat("s", 43), strings.Repeat("c", 43), stepUp)
	}
	good := build(a.cfg.Origin, callbackPath, false)
	for _, c := range []struct {
		name string
		url  string
		want bool
	}{
		{"what the library builds", good, true},
		{"with prompt=login", build(a.cfg.Origin, callbackPath, true), true},
		{"prompt=none", good + "&prompt=none", false},
		{"a duplicated state", good + "&state=" + strings.Repeat("s", 43), false},
		{"an extra parameter", good + "&next=%2F", false},
		{"userinfo", strings.Replace(good, "https://", "https://u@", 1), false},
		{"a fragment", good + "#x", false},
		{"an encoded path", strings.Replace(good, "/oauth/authorize", "/oauth%2Fauthorize", 1), false},
		{"another path", strings.Replace(good, "/oauth/authorize", "/oauth/authorise", 1), false},
		{"plain http", strings.Replace(good, "https://", "http://", 1), false},
		{"no host", strings.Replace(good, "keymail.test", "", 1), false},
		{"another redirect_uri", build(a.cfg.Origin, "/elsewhere", false), false},
		{"another client_id", build("http://evil.test", callbackPath, false), false},
		{"another scope", strings.Replace(good, "scope=identify", "scope=email", 1), false},
		{"plain PKCE", strings.Replace(good, "code_challenge_method=S256", "code_challenge_method=plain", 1), false},
		{"a short state", strings.Replace(good, "state="+strings.Repeat("s", 43), "state=sss", 1), false},
		{"a state that is not base64url", strings.Replace(good, "state="+strings.Repeat("s", 43), "state="+strings.Repeat("s", 42)+"%2B", 1), false},
		{"not a URL", "https://%zz", false},
		{"a query that does not decode", good + "&x=%zz", false},
		{"a query with a semicolon", good + ";x=1", false},
		{"no state", strings.Replace(good, "state="+strings.Repeat("s", 43), "", 1), false},
		{"an empty state", strings.Replace(good, "state="+strings.Repeat("s", 43), "state=", 1), false},
		{"no client_id", regexp.MustCompile(`client_id=[^&]*&?`).ReplaceAllString(good, ""), false},
		{"an empty code_challenge", strings.Replace(good, "code_challenge="+strings.Repeat("c", 43), "code_challenge=", 1), false},
		{"a short code_challenge", strings.Replace(good, "code_challenge="+strings.Repeat("c", 43), "code_challenge="+strings.Repeat("c", 42), 1), false},
		{"a code_challenge that is not base64url", strings.Replace(good, "code_challenge="+strings.Repeat("c", 43), "code_challenge="+strings.Repeat("c", 42)+"%2F", 1), false},
		{"a duplicated prompt", build(a.cfg.Origin, callbackPath, true) + "&prompt=login", false},
		{"an empty prompt", good + "&prompt=", false},
	} {
		if got := a.validAuthorizeURL(c.url); got != c.want {
			t.Errorf("%s: %v, want %v (%s)", c.name, got, c.want, c.url)
		}
	}

	// Every parameter the library writes, missing, empty and doubled —
	// the table above spot-checks these; this row is all of them, so a
	// parameter dropped from the predicate's required list is caught.
	u, err := url.Parse(build(a.cfg.Origin, callbackPath, true))
	if err != nil {
		t.Fatal(err)
	}
	with := func(mut func(url.Values)) string {
		q := u.Query()
		mut(q)
		return "https://keymail.test/oauth/authorize?" + q.Encode()
	}
	if !a.validAuthorizeURL(with(func(url.Values) {})) {
		t.Fatal("the re-encoded library URL is refused; the rows below would pass for the wrong reason")
	}
	for _, p := range []string{"client_id", "redirect_uri", "scope", "state", "code_challenge", "code_challenge_method", "prompt"} {
		if p != "prompt" && a.validAuthorizeURL(with(func(q url.Values) { q.Del(p) })) {
			t.Errorf("no %s: accepted", p)
		}
		if a.validAuthorizeURL(with(func(q url.Values) { q.Set(p, "") })) {
			t.Errorf("an empty %s: accepted", p)
		}
		if a.validAuthorizeURL(with(func(q url.Values) { q.Add(p, q.Get(p)) })) {
			t.Errorf("%s twice with the same value: accepted", p)
		}
	}
	for name, raw := range map[string]string{
		"an escaped letter in the path": strings.Replace(good, "/oauth/authorize", "/oauth/%61uthorize", 1),
		"a trailing slash on the path":  strings.Replace(good, "/oauth/authorize", "/oauth/authorize/", 1),
		"a path under a prefix":         strings.Replace(good, "/oauth/authorize", "/x/oauth/authorize", 1),
		"an opaque URL":                 strings.Replace(good, "https://", "https:", 1),
		"prompt in capitals":            good + "&prompt=LOGIN",
		"a redirect_uri with a slash":   build(a.cfg.Origin, callbackPath+"/", false),
		"an empty fragment":             good + "#",
	} {
		if a.validAuthorizeURL(raw) {
			t.Errorf("%s: accepted (%s)", name, raw)
		}
	}

	l, _ := newTestAuth(t, listed("keymail.test"))
	if !l.validAuthorizeURL(good) {
		t.Error("the list refused a listed host")
	}
	if !l.validAuthorizeURL(strings.Replace(good, "keymail.test", "KeyMail.Test", 1)) {
		t.Error("the list refused a listed host in other letter case; the guard compares lowercased, so must this")
	}
	if l.validAuthorizeURL(strings.Replace(good, "keymail.test", "rogue.test", 1)) {
		t.Error("the list admitted an unlisted host")
	}
	if l.validAuthorizeURL(strings.Replace(good, "keymail.test", "keymail.test:8443", 1)) {
		t.Error("the list admitted a listed host on a port nobody listed")
	}
}

func TestAContinuationOpensOnlyWhereItWasSealed(t *testing.T) {
	a, _ := newScreenAuth(t, nil)
	good := (&signin.Keymail{Base: "https://keymail.test", Origin: a.cfg.Origin, RedirectPath: callbackPath}).
		AuthorizeURL(strings.Repeat("s", 43), strings.Repeat("c", 43), false)
	id, value, err := a.sealContinuation(good, "pending-value")
	if err != nil {
		t.Fatal(err)
	}
	open := func(v string) cookieState {
		r := httptest.NewRequest(http.MethodGet, "http://app.test/signin", nil)
		r.AddCookie(&http.Cookie{Name: a.continueCookie(), Value: v})
		_, st := a.openContinuation(r)
		return st
	}
	if st := open(value); st != cookieValid {
		t.Fatalf("a fresh continuation is %v", st)
	}
	now := a.now().Unix()
	p := continuation{O: a.cfg.Origin, ID: id, IAT: now, EXP: now + 600, U: good, ST: digest("s"), PH: digest("p")}
	seal := func(key []byte, v any) string {
		s, err := sealedcookie.Seal(key, v)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	other, expired, future, long := p, p, p, p
	other.O = "http://other.test"
	expired.IAT, expired.EXP = now-601, now-1
	future.IAT, future.EXP = now+120, now+600
	long.EXP = now + 601
	for name, v := range map[string]string{
		"another origin":          seal(a.continueKey, other),
		"expired":                 seal(a.continueKey, expired),
		"issued in the future":    seal(a.continueKey, future),
		"longer than ten minutes": seal(a.continueKey, long),
		"under the attempt's key": seal(a.attemptKey, p),
		"unknown version":         "v2." + strings.TrimPrefix(value, "v1."),
		"an attempt, not this":    seal(a.continueKey, attempt{O: a.cfg.Origin, ID: "x", IAT: now, EXP: now + 60, K: attemptLink}),
		"tampered":                flipByte(t, value),
	} {
		if st := open(v); st != cookieInvalid {
			t.Errorf("%s: %v, want invalid", name, st)
		}
	}
}

func TestBeginContinuesOnTheSigninPage(t *testing.T) {
	a, _ := newKeymailScreen(t, nil)
	b := newBrowser()
	w := b.do(a.Begin, http.MethodPost, "/signin", url.Values{"address": {"kay@example.org"}})
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/signin?continue=") {
		t.Fatalf("keymail Begin with the screen on → %d %q; the form must stay on this origin so form-action 'self' allows it",
			w.Code, w.Header().Get("Location"))
	}
	if got, want := setCookies(w), []string{"rastrillo_attempt", "rastrillo_continue", "rastrillo_pending"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Set-Cookie %v, want %v", got, want)
	}
	if at, _ := a.openAttempt(b.request(http.MethodGet, "/signin", nil)); at.K != attemptKeymail || at.A != "kay@example.org" {
		t.Fatalf("attempt = %+v, want kind keymail with the address", at)
	}
}

func TestBeginRefusesAnAuthorizeURLThatFailsThePredicate(t *testing.T) {
	a, _ := newKeymailScreen(t, nil)
	a.flow.Keymail = func(server string) *signin.Keymail {
		return &signin.Keymail{Base: "https://" + server + "/evil", Origin: a.cfg.Origin, RedirectPath: callbackPath}
	}
	w := newBrowser().do(a.Begin, http.MethodPost, "/signin", url.Values{"address": {"kay@example.org"}})
	if w.Header().Get("Location") != "/signin?err=1" || len(setCookies(w)) != 0 {
		t.Fatalf("a bad authorize URL → %q with %v; want ?err=1 and no cookie at all", w.Header().Get("Location"), setCookies(w))
	}
}

func TestAContinuationResolvesOnlyForThisBrowser(t *testing.T) {
	a, _ := newKeymailScreen(t, nil)
	b := newBrowser()
	id, _ := startKeymail(t, a, b)
	resolves := func(b *browser, id string) bool {
		_, ok := a.continuationFor(b.request(http.MethodGet, "/signin?continue="+url.QueryEscape(id), nil), id)
		return ok
	}
	if resolves(b, "https://evil.test/") {
		t.Error("a URL in ?continue= resolved; the URL must only ever come from the cookie")
	}
	if resolves(b, "another-id") {
		t.Error("a wrong id resolved")
	}
	noPending := b.clone()
	delete(noPending.jar, "rastrillo_pending")
	if resolves(noPending, id) {
		t.Error("a continuation resolved with no pending cookie")
	}
	otherPending := b.clone()
	otherPending.jar["rastrillo_pending"] = &http.Cookie{Name: "rastrillo_pending", Value: "someone-else"}
	if resolves(otherPending, id) {
		t.Error("a continuation resolved against a different pending cookie")
	}
	a.now = func() time.Time { return time.Now().Add(11 * time.Minute) }
	if resolves(b, id) {
		t.Error("a continuation resolved after its ten minutes")
	}
	a.now = time.Now

	bad, value, err := a.sealContinuation("https://keymail.test/evil/oauth/authorize?x=1", b.cookie("rastrillo_pending").Value)
	if err != nil {
		t.Fatal(err)
	}
	b.jar["rastrillo_continue"] = &http.Cookie{Name: "rastrillo_continue", Value: value}
	if resolves(b, bad) {
		t.Error("a sealed URL failing the predicate resolved; it is checked again on the way out")
	}
}

// TestTwoTabsTheInstalledPairWins is §1.3's guarantee: whichever pair
// of cookies the browser installed last is the only attempt that can
// continue or complete, and a callback for any other attempt ends at
// Expired without touching that pair.
func TestTwoTabsTheInstalledPairWins(t *testing.T) {
	t.Run("A then B: A is stale", func(t *testing.T) {
		a, _ := newKeymailScreen(t, nil)
		b := newBrowser()
		idA, stateA := startKeymail(t, a, b)
		_, stateB := startKeymail(t, a, b)
		if _, ok := a.continuationFor(b.request(http.MethodGet, "/signin?continue="+idA, nil), idA); ok {
			t.Fatal("tab A's continuation still resolves after B's pair was installed")
		}
		w := callback(a, b, stateA)
		if w.Header().Get("Location") != "/signin?err=expired" || len(setCookies(w)) != 0 {
			t.Fatalf("A's late callback → %q with %v; want Expired, B's pair untouched", w.Header().Get("Location"), setCookies(w))
		}
		if w := callback(a, b, stateB); w.Header().Get("Location") != "/" {
			t.Fatalf("B's callback → %q, want signed in", w.Header().Get("Location"))
		}
	})
	t.Run("B's pair then A's, out of order: B is stale", func(t *testing.T) {
		a, _ := newKeymailScreen(t, nil)
		b := newBrowser()
		_, stateA := startKeymail(t, a, b)
		pairA := b.clone()
		_, stateB := startKeymail(t, a, b)
		install(b, pairA, pair...)
		if w := callback(a, b, stateB); w.Header().Get("Location") != "/signin?err=expired" || len(setCookies(w)) != 0 {
			t.Fatalf("B's callback against A's pair → %q with %v", w.Header().Get("Location"), setCookies(w))
		}
		if w := callback(a, b, stateA); w.Header().Get("Location") != "/" {
			t.Fatalf("A's callback → %q, want signed in", w.Header().Get("Location"))
		}
	})
	t.Run("keymail A, keymail B, then a magic link: A still cannot spoil B", func(t *testing.T) {
		a, _ := newKeymailScreen(t, nil)
		b := newBrowser()
		_, stateA := startKeymail(t, a, b)
		_, stateB := startKeymail(t, a, b)
		b.do(a.Begin, http.MethodPost, "/signin", url.Values{"address": {"ada@example.com"}, "force": {"1"}})
		if w := callback(a, b, stateA); w.Header().Get("Location") != "/signin?err=expired" || len(setCookies(w)) != 0 {
			t.Fatalf("A's callback after a magic link → %q with %v", w.Header().Get("Location"), setCookies(w))
		}
		if w := callback(a, b, stateB); w.Header().Get("Location") != "/" {
			t.Fatalf("B's callback → %q", w.Header().Get("Location"))
		}
	})
	t.Run("a continuation that describes another pending cookie does not block", func(t *testing.T) {
		a, _ := newKeymailScreen(t, nil)
		b := newBrowser()
		startKeymail(t, a, b)
		pairA := b.clone()
		_, stateB := startKeymail(t, a, b)
		install(b, pairA, "rastrillo_continue")
		w := callback(a, b, stateB)
		if w.Header().Get("Location") != "/" {
			t.Fatalf("B's callback beside A's leftover continuation → %q, want today's path", w.Header().Get("Location"))
		}
		got := strings.Join(setCookies(w), " ")
		if !strings.Contains(got, "rastrillo_pending:clear") || !strings.Contains(got, "rastrillo_continue:clear") {
			t.Fatalf("today's path clears pending and continuation; got %s", got)
		}
	})
	t.Run("a third party's callback URL leaves the attempt alone", func(t *testing.T) {
		a, _ := newKeymailScreen(t, nil)
		b := newBrowser()
		startKeymail(t, a, b)
		if w := callback(a, b, "forged"); w.Header().Get("Location") != "/signin?err=expired" || len(setCookies(w)) != 0 {
			t.Fatalf("a forged callback → %q with %v; today it throws the visitor's attempt away", w.Header().Get("Location"), setCookies(w))
		}
	})
}

// A keymail sign-in with the screen on ends like a magic link does:
// the attempt is over, the way in is remembered with the address, and
// the continuation goes with its pending cookie — on the held path too.
func TestAKeymailSignInIsRememberedAndEndsTheAttempt(t *testing.T) {
	for _, held := range []bool{false, true} {
		a, _ := newKeymailScreen(t, nil)
		want, dest := []string{"rastrillo_attempt:clear", "rastrillo_continue:clear", "rastrillo_last_signin", "rastrillo_pending:clear", "rastrillo_session"}, "/"
		if held {
			a.cfg.SecondFactor = holdingGate(t, a).Hold
			want, dest = []string{"rastrillo_attempt:clear", "rastrillo_continue:clear", "rastrillo_last_signin", "rastrillo_pending:clear", "rastrillo_secondfactor"}, "/signin/confirm"
		}
		b := newBrowser()
		_, state := startKeymail(t, a, b)
		w := callback(a, b, state)
		if w.Header().Get("Location") != dest || !reflect.DeepEqual(setCookies(w), want) {
			t.Fatalf("held=%v: → %q with %v, want %q with %v", held, w.Header().Get("Location"), setCookies(w), dest, want)
		}
		if rec, res := a.jar.Read(b.request(http.MethodGet, "/signin", nil)); res != lastsignin.Valid || rec != (lastsignin.Record{Method: "keymail", Address: "kay@example.org"}) {
			t.Fatalf("held=%v: remembered %+v, %v", held, rec, res)
		}
	}
}

// TestScreenOffKeymailIsToday is TestScreenOffIsToday's keymail half.
func TestScreenOffKeymailIsToday(t *testing.T) {
	for _, remember := range []*bool{nil, ptr(true), ptr(false)} {
		name := "nil"
		if remember != nil {
			name = strconv.FormatBool(*remember)
		}
		t.Run("Remember="+name, func(t *testing.T) {
			a, _ := newTestAuth(t, func(c *Config) { c.Remember = remember })
			wireKeymail(a, kayFake())
			b := newBrowser()
			begin := func() string {
				w := b.do(a.Begin, http.MethodPost, "/signin", url.Values{"address": {"kay@example.org"}})
				loc := w.Header().Get("Location")
				if !strings.HasPrefix(loc, "https://keymail.test/oauth/authorize?") || !reflect.DeepEqual(setCookies(w), []string{"rastrillo_pending"}) {
					t.Fatalf("keymail Begin, screen off → %q with %v; want today's 303 and pending cookie", loc, setCookies(w))
				}
				return stateOf(t, loc)
			}
			check := func(what string, w *httptest.ResponseRecorder, loc string, cookies []string) {
				t.Helper()
				if w.Header().Get("Location") != loc || !reflect.DeepEqual(setCookies(w), cookies) {
					t.Errorf("%s → %q with %v, want %q with %v", what, w.Header().Get("Location"), setCookies(w), loc, cookies)
				}
			}
			begin()
			check("a callback for no attempt", callback(a, b, "forged"), "/signin?err=expired", []string{"rastrillo_pending:clear"})
			check("the callback", callback(a, b, begin()), "/", []string{"rastrillo_pending:clear", "rastrillo_session"})

			// A continuation cookie left by a screen-on configuration with
			// the same key — describing this very pending cookie, for some
			// other state — is not read with the screen off: the callback
			// completes as today and the cookie is not touched.
			state := begin()
			on, _ := newScreenAuth(t, nil)
			other := (&signin.Keymail{Base: "https://keymail.test", Origin: a.cfg.Origin, RedirectPath: callbackPath}).
				AuthorizeURL(strings.Repeat("o", 43), strings.Repeat("c", 43), false)
			_, leftover, err := on.sealContinuation(other, b.cookie("rastrillo_pending").Value)
			if err != nil {
				t.Fatal(err)
			}
			b.jar["rastrillo_continue"] = &http.Cookie{Name: "rastrillo_continue", Value: leftover}
			check("the callback beside a leftover continuation", callback(a, b, state), "/", []string{"rastrillo_pending:clear", "rastrillo_session"})
			a.cfg.SecondFactor = holdingGate(t, a).Hold
			check("the callback, held", callback(a, b, begin()), "/signin/confirm", []string{"rastrillo_pending:clear", "rastrillo_secondfactor"})
		})
	}
}
