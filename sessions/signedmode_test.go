package sessions_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/sessions"
)

const origin = "https://ppl.test"

// pair is a hub that mints and a tenant that verifies, sharing one
// clock, as a product and its path-routed instances share one box.
type pair struct {
	hub, tenant *sessions.Sessions
	now         time.Time
	epoch       int64
	epochErr    error
	hubKey      *sessions.SigningKey
	logs        bytes.Buffer // the hub's log, kept out of the test output
	epochCalls  int          // how many times the hub read the epoch
}

func newPair(t *testing.T) *pair {
	t.Helper()
	return newPairOn(t, origin)
}

// newPairOn is newPair on another origin, for a test whose redirects
// must name the product origin a base path is served on.
func newPairOn(t *testing.T, origin string) *pair {
	t.Helper()
	p := &pair{now: time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC), epoch: 1}
	k, _ := sessions.NewSigningKey("k1")
	p.hubKey = k
	keys, _ := sessions.ParsePublicKeys(k.Public())
	clock := func() time.Time { return p.now }
	var err error
	p.hub, err = sessions.New(sessions.Config{Origin: origin, Now: clock, Logger: slog.New(slog.NewTextHandler(&p.logs, nil)), Signed: &sessions.Signed{Name: "ppl", Key: k,
		Epoch: func(context.Context, string) (int64, error) { p.epochCalls++; return p.epoch, p.epochErr }}})
	if err != nil {
		t.Fatal(err)
	}
	p.tenant, err = sessions.New(sessions.Config{Origin: origin, Now: clock, Signed: &sessions.Signed{Name: "ppl", Keys: keys}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (p *pair) signIn(t *testing.T, sub string) *http.Cookie {
	t.Helper()
	w := httptest.NewRecorder()
	if err := p.hub.SignIn(w, httptest.NewRequest("POST", origin+"/auth/verify", nil), sessions.Session{Subject: sub, Method: "magiclink"}); err != nil {
		t.Fatal(err)
	}
	cs := w.Result().Cookies()
	if len(cs) == 0 {
		t.Fatal("SignIn set no cookie")
	}
	return cs[0]
}

// underSlug mounts h as Plan 5 will: the tenant sees its path without
// /acme, while r.RequestURI keeps what the browser asked for.
func underSlug(h http.Handler) http.Handler { return http.StripPrefix("/acme", h) }

var ok = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	s, _ := sessions.Current(r)
	w.Write([]byte("hello " + s.Subject))
})

func TestSignedHubMintsAndTenantVerifies(t *testing.T) {
	p := newPair(t)
	c := p.signIn(t, "u1")
	if c.Name != "__Host-ppl" || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.MaxAge != 30*24*3600 {
		t.Fatalf("cookie attributes: %+v", c)
	}
	r := httptest.NewRequest("GET", origin+"/acme/", nil)
	r.AddCookie(c)
	w := httptest.NewRecorder()
	underSlug(p.tenant.Require(ok)).ServeHTTP(w, r)
	if w.Code != 200 || w.Body.String() != "hello u1" {
		t.Fatalf("%d %q", w.Code, w.Body)
	}
}

func TestSignedTenantCannotMint(t *testing.T) {
	p := newPair(t)
	err := p.tenant.SignIn(httptest.NewRecorder(), httptest.NewRequest("POST", origin+"/", nil), sessions.Session{Subject: "u1"})
	if !errors.Is(err, sessions.ErrVerifyOnly) {
		t.Fatalf("got %v", err)
	}
}

func TestSignedSubjectMustBeOpaque(t *testing.T) {
	p := newPair(t)
	if err := p.hub.SignIn(httptest.NewRecorder(), httptest.NewRequest("POST", origin+"/", nil), sessions.Session{Subject: "ana@example.com"}); err == nil {
		t.Fatal("an address as the subject was signed into a cookie")
	}
}

func TestSignedNewRefusesBadConfig(t *testing.T) {
	k1, _ := sessions.NewSigningKey("k1")
	k2, _ := sessions.NewSigningKey("k2")
	k3, _ := sessions.NewSigningKey("k3")
	other, _ := sessions.NewSigningKey("k1")
	two, _ := sessions.ParsePublicKeys(k2.Public() + "," + k3.Public())
	clash, _ := sessions.ParsePublicKeys(other.Public())
	epoch := func(context.Context, string) (int64, error) { return 0, nil }
	for name, s := range map[string]*sessions.Signed{
		"no name":           {Keys: two},
		"bad name":          {Name: "Ppl!", Keys: two},
		"no keys":           {Name: "ppl"},
		"key without epoch": {Name: "ppl", Key: k1},
		"three kids":        {Name: "ppl", Key: k1, Keys: two, Epoch: epoch},
		"kid clash":         {Name: "ppl", Key: k1, Keys: clash, Epoch: epoch},
		"relative refresh":  {Name: "ppl", Keys: two, RefreshPath: "_carlos/refresh"},
		// Each of these decodes or cleans to a path other than itself,
		// so r.URL.Path never equals it and the route redirects to itself.
		"escaped refresh": {Name: "ppl", Keys: two, RefreshPath: "/_carlos/%72efresh"},
		"dot segment":     {Name: "ppl", Keys: two, RefreshPath: "/_carlos/./refresh"},
		"dot-dot segment": {Name: "ppl", Keys: two, RefreshPath: "/x/../_carlos/refresh"},
		"empty segment":   {Name: "ppl", Keys: two, RefreshPath: "/_carlos//refresh"},
		"double slash":    {Name: "ppl", Keys: two, RefreshPath: "//_carlos/refresh"},
		"trailing slash":  {Name: "ppl", Keys: two, RefreshPath: "/_carlos/refresh/"},
	} {
		if _, err := sessions.New(sessions.Config{Origin: origin, Signed: s}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := sessions.New(sessions.Config{Origin: origin, DB: newDB(t), Signed: &sessions.Signed{Name: "ppl", Keys: two}}); err == nil {
		t.Error("a DB with signed mode was accepted")
	}
	for _, o := range []string{"https://ppl.test/", "https://ppl.test/acme", "https://ppl.test?x"} {
		if _, err := sessions.New(sessions.Config{Origin: o, Signed: &sessions.Signed{Name: "ppl", Keys: two}}); err == nil {
			t.Errorf("Origin %q accepted: redirects built on it would miss the hub", o)
		}
	}
	byHand := map[string][]byte{"k9": make([]byte, 65)}
	if _, err := sessions.New(sessions.Config{Origin: origin, Signed: &sessions.Signed{Name: "ppl", Keys: byHand}}); err == nil {
		t.Error("a hand-built map holding no valid point was accepted")
	}
}

func TestSignedConfigIsCopied(t *testing.T) {
	p := newPair(t)
	c := p.signIn(t, "u1")
	keys, _ := sessions.ParsePublicKeys(p.hubKey.Public())
	tenant, _ := sessions.New(sessions.Config{Origin: origin, Now: func() time.Time { return p.now },
		Signed: &sessions.Signed{Name: "ppl", Keys: keys}})
	clear(keys["k1"]) // edit the caller's map after New
	delete(keys, "k1")
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(c)
	if _, ok := tenant.From(r); !ok {
		t.Fatal("editing the caller's map after New changed what the tenant trusts")
	}
}

// A hub with TTL 5*time.Minute: the cookie's exp is sign-in + 5m, not
// + 15m, so the tenant accepts it at +4m59s and refuses it at +5m.
func TestSignedCookieNeverOutlivesTheSession(t *testing.T) {
	p := newPair(t)
	hub, err := sessions.New(sessions.Config{Origin: origin, TTL: 5 * time.Minute, Now: func() time.Time { return p.now },
		Signed: &sessions.Signed{Name: "ppl", Key: p.hubKey,
			Epoch: func(context.Context, string) (int64, error) { return p.epoch, nil }}})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	if err := hub.SignIn(w, httptest.NewRequest("POST", origin+"/auth/verify", nil), sessions.Session{Subject: "u1", Method: "magiclink"}); err != nil {
		t.Fatal(err)
	}
	c := w.Result().Cookies()[0]
	if c.MaxAge != 5*60 {
		t.Errorf("Max-Age %d, want the session's 300", c.MaxAge)
	}
	start := p.now
	for _, tc := range []struct {
		after time.Duration
		valid bool
	}{{5*time.Minute - time.Second, true}, {5 * time.Minute, false}} {
		p.now = start.Add(tc.after)
		r := httptest.NewRequest("GET", "/acme/", nil)
		r.AddCookie(c)
		if _, ok := p.tenant.From(r); ok != tc.valid {
			t.Errorf("at sign-in + %v the tenant said valid=%v, want %v: the cookie must end with the session", tc.after, ok, tc.valid)
		}
	}
}

func TestSignedExpiredGETGoesToRefreshWithTheFullPath(t *testing.T) {
	p := newPair(t)
	c := p.signIn(t, "u1")
	p.now = p.now.Add(16 * time.Minute)
	r := httptest.NewRequest("GET", "/acme/posts?x=1", nil)
	r.AddCookie(c)
	w := httptest.NewRecorder()
	underSlug(p.tenant.Require(ok)).ServeHTTP(w, r)
	want := origin + "/_carlos/refresh?next=" + url.QueryEscape("/acme/posts?x=1")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != want {
		t.Fatalf("%d %q, want 303 %q", w.Code, w.Header().Get("Location"), want)
	}
}

func TestSignedExpiredPOSTKeepsItsBody(t *testing.T) {
	p := newPair(t)
	c := p.signIn(t, "u1")
	p.now = p.now.Add(16 * time.Minute)
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		r := httptest.NewRequest(method, "/acme/posts", strings.NewReader("body=draft"))
		r.AddCookie(c)
		w := httptest.NewRecorder()
		underSlug(p.tenant.Middleware(ok)).ServeHTTP(w, r)
		if w.Code != http.StatusTemporaryRedirect {
			t.Errorf("%s: %d, want 307 so the browser repeats the request with its body", method, w.Code)
		}
	}
}

func TestSignedTenantNeverClearsAnUnknownKeyCookie(t *testing.T) {
	p := newPair(t)
	newer, _ := sessions.NewSigningKey("k2")
	rotated, _ := sessions.New(sessions.Config{Origin: origin, Now: func() time.Time { return p.now },
		Signed: &sessions.Signed{Name: "ppl", Key: newer, Epoch: func(context.Context, string) (int64, error) { return 1, nil }}})
	w := httptest.NewRecorder()
	rotated.SignIn(w, httptest.NewRequest("POST", origin+"/", nil), sessions.Session{Subject: "u1"})
	c := w.Result().Cookies()[0]
	for _, h := range []http.Handler{p.tenant.Middleware(ok), p.tenant.Require(ok)} {
		r := httptest.NewRequest("GET", "/acme/", nil)
		r.AddCookie(c)
		w := httptest.NewRecorder()
		underSlug(h).ServeHTTP(w, r)
		if len(w.Result().Cookies()) != 0 {
			t.Fatal("the tenant cleared a cookie it could not verify: that signs the person out of the whole product")
		}
		if strings.Contains(w.Header().Get("Location"), "/_carlos/refresh") {
			t.Fatal("an unverifiable cookie was sent to refresh: that loops")
		}
	}
}

// A hub that mounts Refresh behind its own Middleware must reach it:
// the handler here stands in for Refresh, which Task 3 writes.
func TestSignedRefreshRouteIsNotSentToItself(t *testing.T) {
	p := newPair(t)
	c := p.signIn(t, "u1")
	p.now = p.now.Add(16 * time.Minute)
	r := httptest.NewRequest("GET", "/_carlos/refresh?next=/", nil)
	r.AddCookie(c)
	w := httptest.NewRecorder()
	p.hub.Middleware(ok).ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("%d %q: Middleware redirected the refresh route to itself", w.Code, w.Header().Get("Location"))
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("Middleware cleared the expired cookie on its way to Refresh, which then has nothing to reissue")
	}
}

func TestSignedSigninRedirectIsAbsoluteAndKeepsTheSlug(t *testing.T) {
	p := newPair(t)
	w := httptest.NewRecorder()
	underSlug(p.tenant.Require(ok)).ServeHTTP(w, httptest.NewRequest("GET", "/acme/settings", nil))
	want := origin + "/signin?return_to=" + url.QueryEscape("/acme/settings")
	if w.Header().Get("Location") != want {
		t.Fatalf("got %q, want %q", w.Header().Get("Location"), want)
	}
}

func TestSignedRowMethodsSayNoRows(t *testing.T) {
	p := newPair(t)
	if _, err := p.hub.Mint(sessions.Session{Subject: "u1"}); !errors.Is(err, sessions.ErrSigned) {
		t.Errorf("Mint: %v", err)
	}
	if _, err := p.hub.List("u1"); !errors.Is(err, sessions.ErrSigned) {
		t.Errorf("List: %v", err)
	}
	if _, err := p.hub.RevokeAll("u1"); !errors.Is(err, sessions.ErrSigned) {
		t.Errorf("RevokeAll: %v", err)
	}
	if err := p.hub.Revoke("u1", "x"); !errors.Is(err, sessions.ErrSigned) {
		t.Errorf("Revoke: %v", err)
	}
	if _, err := p.hub.RevokeOthers(httptest.NewRequest("GET", "/", nil), "u1"); !errors.Is(err, sessions.ErrSigned) {
		t.Errorf("RevokeOthers: %v", err)
	}
	if _, ok := p.hub.Adopt(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil), "x"); ok {
		t.Error("Adopt adopted a token in signed mode")
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(p.signIn(t, "u1"))
	if h, ok := p.hub.Hash(r); ok || h != "" {
		t.Errorf("Hash returned %q, %v for a signed cookie: there is no row to find", h, ok)
	}
	if err := p.hub.Sweep(p.now); err != nil {
		t.Errorf("Sweep: %v", err)
	}
}

func TestSignedSignOutClearsThisBrowsersCookie(t *testing.T) {
	p := newPair(t)
	for _, s := range []*sessions.Sessions{p.hub, p.tenant} {
		r := httptest.NewRequest("POST", "/signout", nil)
		r.AddCookie(p.signIn(t, "u1"))
		w := httptest.NewRecorder()
		s.SignOut(w, r)
		cs := w.Result().Cookies()
		if len(cs) != 1 || cs[0].Name != "__Host-ppl" || cs[0].MaxAge >= 0 {
			t.Fatalf("SignOut set %+v, want __Host-ppl cleared", cs)
		}
	}
}

// RequireFresh reads auth_time from the cookie, so a step-up gate works
// on a tenant as it does on a row-mode app.
func TestSignedRequireFreshReadsAuthTime(t *testing.T) {
	p := newPair(t)
	w := httptest.NewRecorder()
	if err := p.hub.SignIn(w, httptest.NewRequest("POST", origin+"/", nil), sessions.Session{Subject: "u1", Method: "magiclink", AuthTime: p.now.Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	c := w.Result().Cookies()[0]
	start := p.now
	// Code and body as well as Location: an empty Location alone is
	// also what a refusal that redirects nowhere would give.
	for _, tc := range []struct {
		after time.Duration
		code  int
		body  string
		want  string
	}{
		{3 * time.Minute, http.StatusOK, "hello u1", ""},
		{5 * time.Minute, http.StatusSeeOther, "", origin + "/signin?return_to=" + url.QueryEscape("/acme/keys") + "&reauth=1"},
	} {
		p.now = start.Add(tc.after)
		r := httptest.NewRequest("GET", "/acme/keys", nil)
		r.AddCookie(c)
		w := httptest.NewRecorder()
		underSlug(p.tenant.RequireFresh(5*time.Minute)(ok)).ServeHTTP(w, r)
		if w.Code != tc.code || w.Header().Get("Location") != tc.want || tc.body != "" && w.Body.String() != tc.body {
			t.Errorf("credential verified %v ago: %d %q, body %q; want %d %q, body %q", tc.after+time.Minute, w.Code, w.Header().Get("Location"), w.Body, tc.code, tc.want, tc.body)
		}
	}
}

// Task 5 calls RefreshIfExpired in row mode too; there it must do nothing.
func TestRowModeRefreshIfExpiredIsANoOp(t *testing.T) {
	s, _ := newTestSessions(t, nil)
	r := httptest.NewRequest("GET", "/posts", nil)
	r.AddCookie(&http.Cookie{Name: s.CookieName(), Value: "x.y"})
	w := httptest.NewRecorder()
	if s.RefreshIfExpired(w, r) || w.Code != http.StatusOK || w.Header().Get("Location") != "" || len(w.Result().Cookies()) != 0 {
		t.Fatalf("row mode answered %d %q", w.Code, w.Header().Get("Location"))
	}
}
