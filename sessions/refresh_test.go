package sessions_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/sessions"
)

func refresh(t *testing.T, p *pair, method, next string, c *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "/_carlos/refresh?next="+url.QueryEscape(next), strings.NewReader("body=draft"))
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	if c != nil {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	p.hub.Refresh(w, r)
	return w
}

// cookieNamed finds a cookie by name rather than position, so a test
// against an answer that set none fails on its own message instead of
// an index panic.
func cookieNamed(t *testing.T, w *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("%d %q: no %s cookie in %v", w.Code, w.Header().Get("Location"), name, w.Result().Cookies())
	return nil
}

const counterName = "__Host-ppl_rf"

// signedOut checks the answer clears the session cookie and sends the
// browser to sign in, coming back to next.
func signedOut(t *testing.T, w *httptest.ResponseRecorder, next string) {
	t.Helper()
	want := "/signin?return_to=" + url.QueryEscape(next)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != want {
		t.Fatalf("%d %q, want 303 %q", w.Code, w.Header().Get("Location"), want)
	}
	if c := cookieNamed(t, w, "__Host-ppl"); c.MaxAge >= 0 {
		t.Fatalf("%+v: want the cookie cleared", c)
	}
}

func TestRefreshReissuesWhenTheEpochMatches(t *testing.T) {
	p := newPair(t)
	c := p.signIn(t, "u1")
	p.now = p.now.Add(16 * time.Minute)
	w := refresh(t, p, "GET", "/acme/posts", c)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/acme/posts" {
		t.Fatalf("%d %q", w.Code, w.Header().Get("Location"))
	}
	fresh := w.Result().Cookies()[0]
	r := httptest.NewRequest("GET", "/acme/", nil)
	r.AddCookie(fresh)
	if s, ok := p.tenant.From(r); !ok || s.Subject != "u1" {
		t.Fatal("the tenant does not accept the reissued cookie")
	}
}

func TestRefreshKeepsAPOSTAPOST(t *testing.T) {
	p := newPair(t)
	c := p.signIn(t, "u1")
	p.now = p.now.Add(16 * time.Minute)
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		w := refresh(t, p, method, "/acme/posts", c)
		if w.Code != http.StatusTemporaryRedirect || w.Header().Get("Location") != "/acme/posts" {
			t.Errorf("%s: %d %q, want 307 back to the action", method, w.Code, w.Header().Get("Location"))
		}
		p.now = p.now.Add(16 * time.Minute)
		c = cookieNamed(t, w, "__Host-ppl")
	}
}

func TestRefreshAfterAnEpochBumpSignsOut(t *testing.T) {
	p := newPair(t)
	c := p.signIn(t, "u1")
	p.now, p.epoch = p.now.Add(16*time.Minute), 2
	w := refresh(t, p, "GET", "/acme/", c)
	if w.Header().Get("Location") != "/signin?return_to=%2Facme%2F" || w.Result().Cookies()[0].MaxAge >= 0 {
		t.Fatalf("%q %+v: want sign-in and a cleared cookie", w.Header().Get("Location"), w.Result().Cookies())
	}
}

func TestRefreshForADeletedUserSignsOut(t *testing.T) {
	p := newPair(t)
	c := p.signIn(t, "u1")
	p.now, p.epochErr = p.now.Add(16*time.Minute), sessions.ErrNoUser
	signedOut(t, refresh(t, p, "GET", "/acme/", c), "/acme/")
}

func TestRefreshOnAStoreErrorKeepsTheCookie(t *testing.T) {
	p := newPair(t)
	c := p.signIn(t, "u1")
	p.now, p.epochErr = p.now.Add(16*time.Minute), errors.New("store down")
	w := refresh(t, p, "GET", "/acme/", c)
	if w.Code != http.StatusServiceUnavailable || len(w.Result().Cookies()) != 0 {
		t.Fatalf("%d %v: an outage must answer 503 and leave the cookie alone", w.Code, w.Result().Cookies())
	}
	if !strings.Contains(p.logs.String(), "store down") {
		t.Errorf("the store error was not logged: %q", p.logs.String())
	}
}

func TestRefreshPastTheSessionLifeSignsOut(t *testing.T) {
	p := newPair(t)
	c := p.signIn(t, "u1")
	p.now = p.now.Add(30*24*time.Hour + time.Second)
	signedOut(t, refresh(t, p, "GET", "/acme/", c), "/acme/")
}

func TestRefreshRefusesAHostileNext(t *testing.T) {
	p := newPair(t)
	c := p.signIn(t, "u1")
	for _, next := range []string{"//evil.example", "/\\evil.example", "https://evil.example/", "/\t/evil.example",
		"/_carlos/refresh?next=/x", "", "evil"} {
		p.now = p.now.Add(16 * time.Minute)
		w := refresh(t, p, "GET", next, c)
		if w.Header().Get("Location") != "/" {
			t.Errorf("next %q: went to %q, want /", next, w.Header().Get("Location"))
		}
		c = cookieNamed(t, w, "__Host-ppl")
	}
}

// A next that only becomes the refresh route once decoded or cleaned
// would still send the browser back here.
func TestRefreshRefusesANextThatIsRefreshInDisguise(t *testing.T) {
	p := newPair(t)
	c := p.signIn(t, "u1")
	for _, next := range []string{"/_carlos/%72efresh?next=/x", "/x/../_carlos/refresh?next=/x", "/_carlos//refresh"} {
		p.now = p.now.Add(16 * time.Minute)
		w := refresh(t, p, "GET", next, c)
		if w.Header().Get("Location") != "/" {
			t.Errorf("next %q: went to %q, want /", next, w.Header().Get("Location"))
		}
		c = cookieNamed(t, w, "__Host-ppl")
	}
}

// Two tabs both sent to refresh with one expired cookie: the first
// reissues it, the second arrives with the new one and must go through.
func TestRefreshOfAFreshCookieJustGoesBack(t *testing.T) {
	p := newPair(t)
	c := p.signIn(t, "u1")
	p.now = p.now.Add(16 * time.Minute)
	fresh := cookieNamed(t, refresh(t, p, "GET", "/acme/a", c), "__Host-ppl")
	calls := p.epochCalls
	w := refresh(t, p, "GET", "/acme/b", fresh)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/acme/b" {
		t.Fatalf("second tab: %d %q", w.Code, w.Header().Get("Location"))
	}
	// A fresh cookie needs no store read and no new cookie. Reading the
	// epoch here would put a store read on every bounce of a loop, and
	// reissuing would hide the loop the counter is there to catch.
	if p.epochCalls != calls {
		t.Errorf("second tab: %d epoch reads, want none", p.epochCalls-calls)
	}
	for _, sc := range w.Result().Cookies() {
		if sc.Name == "__Host-ppl" {
			t.Errorf("second tab: the session cookie was set again: %+v", sc)
		}
	}
}

func TestRefreshBreaksALoop(t *testing.T) {
	p := newPair(t)
	c := p.signIn(t, "u1")
	visit := func(counter *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/_carlos/refresh?next=%2Facme%2F", nil)
		r.AddCookie(c)
		if counter != nil {
			r.AddCookie(counter)
		}
		w := httptest.NewRecorder()
		p.hub.Refresh(w, r)
		return w
	}

	// Four tabs arriving with a fresh cookie, as overlapping refreshes
	// do: each one goes back.
	var counter *http.Cookie
	for i := 1; i <= 4; i++ {
		w := visit(counter)
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/acme/" {
			t.Fatalf("tab %d: %d %q, want 303 back", i, w.Code, w.Header().Get("Location"))
		}
		counter = cookieNamed(t, w, counterName)
		want := strconv.Itoa(i) + "." + strconv.FormatInt(p.now.Unix(), 10)
		if !counter.HttpOnly || counter.MaxAge != 10 || counter.Value != want {
			t.Fatalf("tab %d: counter %+v, want HttpOnly, Max-Age 10, value %q", i, counter, want)
		}
	}

	// A tenant bouncing the same fresh cookie: nine go back, the tenth
	// in 10 seconds stops with an error and clears the counter.
	counter = nil
	for i := 1; i <= 10; i++ {
		p.now = p.now.Add(100 * time.Millisecond)
		w := visit(counter)
		if i < 10 {
			if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/acme/" {
				t.Fatalf("bounce %d: %d %q, want 303 back", i, w.Code, w.Header().Get("Location"))
			}
			counter = cookieNamed(t, w, counterName)
			continue
		}
		if w.Code != http.StatusInternalServerError || w.Header().Get("Location") != "" {
			t.Fatalf("bounce 10: %d %q, want 500 and no redirect", w.Code, w.Header().Get("Location"))
		}
		if !strings.Contains(w.Body.String(), "Check that the hub and its apps agree on the time.") {
			t.Errorf("bounce 10 said %q", w.Body)
		}
		if cleared := cookieNamed(t, w, counterName); cleared.MaxAge >= 0 {
			t.Fatalf("bounce 10: counter %+v, want it cleared so a reload starts over", cleared)
		}
		if !strings.Contains(p.logs.String(), "level=ERROR") || !strings.Contains(p.logs.String(), "refresh loop") {
			t.Errorf("the loop left no error in the log: %q", p.logs.String())
		}
	}
	if w := visit(nil); w.Code != http.StatusSeeOther {
		t.Fatalf("after the loop was broken, a reload answered %d", w.Code)
	}
}

// Ten visits 9 seconds apart never put ten in one 10-second window, so
// they are not a loop, even when the browser still sends the counter.
func TestRefreshLoopWindowDoesNotSlide(t *testing.T) {
	p := newPair(t)
	c := p.signIn(t, "u1")
	var counter *http.Cookie
	for i := 1; i <= 10; i++ {
		r := httptest.NewRequest("GET", "/_carlos/refresh?next=%2Facme%2F", nil)
		r.AddCookie(c)
		if counter != nil {
			r.AddCookie(counter)
		}
		w := httptest.NewRecorder()
		p.hub.Refresh(w, r)
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/acme/" {
			t.Fatalf("visit %d, %v in: %d %q, want 303 back", i, time.Duration(i-1)*9*time.Second, w.Code, w.Header().Get("Location"))
		}
		counter = cookieNamed(t, w, counterName)
		// Odd visits open a window, even ones land 9 seconds into it.
		if want := 10 - 9*((i+1)%2); counter.MaxAge != want {
			t.Errorf("visit %d: Max-Age %d, want %d, what is left of the window", i, counter.MaxAge, want)
		}
		p.now = p.now.Add(9 * time.Second)
	}
	if strings.Contains(p.logs.String(), "refresh loop") {
		t.Errorf("spaced visits were logged as a loop: %q", p.logs.String())
	}

	// A counter that does not parse starts a fresh window.
	r := httptest.NewRequest("GET", "/_carlos/refresh?next=%2F", nil)
	r.AddCookie(c)
	r.AddCookie(&http.Cookie{Name: counterName, Value: "999"})
	w := httptest.NewRecorder()
	p.hub.Refresh(w, r)
	if want := "1." + strconv.FormatInt(p.now.Unix(), 10); w.Code != http.StatusSeeOther || cookieNamed(t, w, counterName).Value != want {
		t.Fatalf("garbled counter: %d %v, want 303 and a counter of %q", w.Code, w.Result().Cookies(), want)
	}
}

// A refresh that reissues ends any count a fresh-cookie bounce began.
func TestRefreshReissueClearsTheCounter(t *testing.T) {
	p := newPair(t)
	c := p.signIn(t, "u1")
	p.now = p.now.Add(16 * time.Minute)
	r := httptest.NewRequest("GET", "/_carlos/refresh?next=%2F", nil)
	r.AddCookie(c)
	r.AddCookie(&http.Cookie{Name: counterName, Value: "3"})
	w := httptest.NewRecorder()
	p.hub.Refresh(w, r)
	if cleared := cookieNamed(t, w, counterName); cleared.MaxAge >= 0 {
		t.Fatalf("counter %+v, want it cleared", cleared)
	}
	if fresh := w.Result().Cookies()[0]; fresh.Name != "__Host-ppl" || fresh.MaxAge <= 0 {
		t.Fatalf("first cookie %+v, want the reissued session", fresh)
	}
}

func TestRefreshRefusesACrossSitePost(t *testing.T) {
	p := newPair(t)
	r := httptest.NewRequest("POST", "/_carlos/refresh?next=/", nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	p.hub.Refresh(w, r)
	if w.Code != http.StatusForbidden || len(w.Result().Cookies()) != 0 {
		t.Fatalf("%d %v: a cross-site post must change nothing", w.Code, w.Result().Cookies())
	}
}

func TestRefreshWithNoCookieSetsNoCookie(t *testing.T) {
	p := newPair(t)
	w := refresh(t, p, "GET", "/acme/", nil)
	if !strings.HasPrefix(w.Header().Get("Location"), "/signin") || len(w.Result().Cookies()) != 0 {
		t.Fatalf("%q %v", w.Header().Get("Location"), w.Result().Cookies())
	}
}

func TestRefreshNeverOutlivesTheSession(t *testing.T) {
	p := newPair(t)
	c := p.signIn(t, "u1")
	end := p.now.Add(30 * 24 * time.Hour)
	p.now = end.Add(-time.Minute)
	w := refresh(t, p, "GET", "/acme/", c)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/acme/" {
		t.Fatalf("a minute before the end: %d %q, want a reissue", w.Code, w.Header().Get("Location"))
	}
	fresh := cookieNamed(t, w, "__Host-ppl")
	if fresh.MaxAge != 60 {
		t.Errorf("Max-Age %d, want the 60 seconds the session has left", fresh.MaxAge)
	}
	for _, tc := range []struct {
		at    time.Time
		valid bool
	}{{end.Add(-time.Second), true}, {end, false}} {
		p.now = tc.at
		r := httptest.NewRequest("GET", "/acme/", nil)
		r.AddCookie(fresh)
		if _, ok := p.tenant.From(r); ok != tc.valid {
			t.Errorf("at the end %+v: valid=%v, want %v", tc.at.Sub(end), ok, tc.valid)
		}
	}
	p.now = end
	signedOut(t, refresh(t, p, "GET", "/acme/", fresh), "/acme/")
}

func TestRefreshMovesARetiredKeyOntoTheCurrentOne(t *testing.T) {
	p := newPair(t)
	c := p.signIn(t, "u1") // under k1
	k2, _ := sessions.NewSigningKey("k2")
	retired, _ := sessions.ParsePublicKeys(p.hubKey.Public())
	clock := func() time.Time { return p.now }
	rotated, err := sessions.New(sessions.Config{Origin: origin, Now: clock, Signed: &sessions.Signed{Name: "ppl", Key: k2, Keys: retired,
		Epoch: func(context.Context, string) (int64, error) { return p.epoch, nil }}})
	if err != nil {
		t.Fatal(err)
	}
	onlyK2, _ := sessions.ParsePublicKeys(k2.Public())
	tenant, err := sessions.New(sessions.Config{Origin: origin, Now: clock, Signed: &sessions.Signed{Name: "ppl", Keys: onlyK2}})
	if err != nil {
		t.Fatal(err)
	}

	p.now = p.now.Add(16 * time.Minute)
	r := httptest.NewRequest("GET", "/_carlos/refresh?next=%2Facme%2F", nil)
	r.AddCookie(c)
	w := httptest.NewRecorder()
	rotated.Refresh(w, r)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/acme/" {
		t.Fatalf("%d %q: a cookie under the retired key was not refreshed", w.Code, w.Header().Get("Location"))
	}
	fresh := cookieNamed(t, w, "__Host-ppl")
	r = httptest.NewRequest("GET", "/acme/", nil)
	r.AddCookie(fresh)
	if s, ok := tenant.From(r); !ok || s.Subject != "u1" {
		t.Fatal("a tenant holding only k2 refuses the reissued cookie: it was not moved onto the current key")
	}
	if _, ok := p.tenant.From(r); ok {
		t.Fatal("a tenant holding only k1 accepts the reissued cookie: it is still under the retired key")
	}
}

func TestRefreshOnATenantIsNotFound(t *testing.T) {
	p := newPair(t)
	w := httptest.NewRecorder()
	p.tenant.Refresh(w, httptest.NewRequest("GET", "/_carlos/refresh?next=/", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("%d", w.Code)
	}
}

func TestRefreshInRowModeIsNotFound(t *testing.T) {
	s, _ := newTestSessions(t, nil)
	w := httptest.NewRecorder()
	s.Refresh(w, httptest.NewRequest("GET", "/_carlos/refresh?next=/", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("%d", w.Code)
	}
}

// A hub that mounts Refresh behind its own Middleware must let an
// expired cookie through to Refresh. If Middleware does not recognise
// the route it redirects the request to itself, and Refresh, whose
// counter is what stops a loop, never runs.
func TestRefreshBehindMiddlewareReissues(t *testing.T) {
	for _, refreshPath := range []string{"", "/hub/refresh"} {
		p := newPair(t)
		hub, err := sessions.New(sessions.Config{Origin: origin, Now: func() time.Time { return p.now }, Signed: &sessions.Signed{
			Name: "ppl", Key: p.hubKey, RefreshPath: refreshPath,
			Epoch: func(context.Context, string) (int64, error) { return p.epoch, nil },
		}})
		if err != nil {
			t.Fatal(err)
		}
		if refreshPath == "" {
			refreshPath = sessions.DefaultRefreshPath
		}
		mux := http.NewServeMux()
		mux.HandleFunc(refreshPath, hub.Refresh)
		mux.Handle("/", ok)
		c := p.signIn(t, "u1")
		p.now = p.now.Add(16 * time.Minute)
		r := httptest.NewRequest("GET", origin+refreshPath+"?next=%2Facme%2F", nil)
		r.AddCookie(c)
		w := httptest.NewRecorder()
		hub.Middleware(mux).ServeHTTP(w, r)
		fresh := cookieNamed(t, w, "__Host-ppl")
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/acme/" || fresh.Value == c.Value || fresh.MaxAge <= 0 {
			t.Fatalf("%s: %d %q, cookie %+v; want 303 to /acme/ with the cookie reissued", refreshPath, w.Code, w.Header().Get("Location"), fresh)
		}
	}
}
