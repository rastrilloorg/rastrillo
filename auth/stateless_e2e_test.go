package auth

import (
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/csrf"
	"amadan.net/rastrillo/rastrillo/sessions"
)

// hop is one redirect an originBrowser followed.
type hop struct {
	code     int
	location string
}

// originBrowser is one browser on https://ppl.test: a cookie jar, and
// redirects followed the way a browser follows them. Unlike browser, it
// drives a whole mux, because what is under test is how the hub's and
// the tenant's routes hand the browser to each other.
type originBrowser struct {
	t   *testing.T
	h   http.Handler
	jar map[string]*http.Cookie
}

func newOriginBrowser(t *testing.T, h http.Handler) *originBrowser {
	return &originBrowser{t: t, h: h, jar: map[string]*http.Cookie{}}
}

func (b *originBrowser) apply(w *httptest.ResponseRecorder) {
	for _, c := range w.Result().Cookies() {
		if c.MaxAge < 0 {
			delete(b.jar, c.Name)
			continue
		}
		b.jar[c.Name] = c
	}
}

// local reduces a Location to the origin-form target it names, failing
// on any host but ppl.test. Origin-form matters: a request built from
// an absolute URL has an absolute RequestURI, and behind
// http.StripPrefix the refresh redirect would then lose /acme from next.
func (b *originBrowser) local(from, loc string) string {
	b.t.Helper()
	base, err := url.Parse("https://ppl.test" + from)
	if err != nil {
		b.t.Fatalf("target %q: %v", from, err)
	}
	u, err := url.Parse(loc)
	if err != nil {
		b.t.Fatalf("Location %q: %v", loc, err)
	}
	abs := base.ResolveReference(u)
	if abs.Scheme != "https" || abs.Host != "ppl.test" {
		b.t.Fatalf("redirected off the origin to %q", loc)
	}
	return abs.RequestURI()
}

// request is one origin-form request from this browser, with its
// cookies and the same-origin evidence a browser sends.
func (b *originBrowser) request(method, target, body string) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	r.Host = "ppl.test"
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	for _, c := range b.jar {
		r.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	}
	return r
}

// visit sends method to target and follows redirects: a 303 as a GET
// without the body, a 307 with the same method and body. It returns the
// last answer, the redirects followed, and the target that answered.
func (b *originBrowser) visit(method, target string, form url.Values) (*httptest.ResponseRecorder, []hop, string) {
	b.t.Helper()
	body := ""
	if form != nil {
		body = form.Encode()
	}
	var hops []hop
	for {
		w := httptest.NewRecorder()
		b.h.ServeHTTP(w, b.request(method, target, body))
		b.apply(w)
		switch w.Code {
		case http.StatusSeeOther:
			method, body = http.MethodGet, ""
		case http.StatusTemporaryRedirect:
		default:
			return w, hops, target
		}
		loc := w.Header().Get("Location")
		hops = append(hops, hop{w.Code, loc})
		if len(hops) > 10 {
			b.t.Fatalf("more than 10 redirects: %v", hops)
		}
		target = b.local(target, loc)
	}
}

// signIn is ana@example.com signing in through Begin and the emailed
// link, landing on the hub's home page.
func (b *originBrowser) signIn(m *captureMailer, cookieName string) {
	b.t.Helper()
	m.forget()
	if w, _, at := b.visit(http.MethodPost, "/signin", url.Values{"address": {"ana@example.com"}, "force": {"1"}}); w.Code != http.StatusOK || !strings.HasPrefix(at, "/signin?sent=1") {
		b.t.Fatalf("Begin ended at %q with %d", at, w.Code)
	}
	link := recordsLinkRE.FindString(m.sentBody())
	if link == "" {
		b.t.Fatalf("no link in the mail: %q", m.sentBody())
	}
	target := b.local("/", link)
	if w, _, _ := b.visit(http.MethodGet, target, nil); w.Code != http.StatusOK {
		b.t.Fatalf("GET Verify: %d", w.Code)
	}
	u, _ := url.Parse(target)
	if w, _, at := b.visit(http.MethodPost, u.Path, url.Values{"token": {u.Query().Get("token")}}); w.Code != http.StatusOK || at != "/" {
		b.t.Fatalf("POST Verify ended at %q with %d", at, w.Code)
	}
	if b.jar[cookieName] == nil {
		b.t.Fatal("signed in, but the jar holds no session cookie")
	}
}

// jarValues is the jar as name=value pairs, for comparing two moments.
func (b *originBrowser) jarValues() map[string]string {
	out := map[string]string{}
	for name, c := range b.jar {
		out[name] = c.Value
	}
	return out
}

// TestStatelessHubAndTenant is spec §6.3 on one origin: a hub with no
// database at the root and a tenant under /acme, which only verifies
// the hub's cookie. The tenant is mounted with http.StripPrefix, as
// Plan 5's path routing will mount it.
func TestStatelessHubAndTenant(t *testing.T) {
	var hubKey *sessions.SigningKey
	a, m, store := newRecordsAuth(t, func(c *Config) { hubKey = c.Signed.Key })
	clock := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	// One clock for hub and tenant: a tenant whose clock disagrees with
	// the hub's is the refresh loop, not what this test is about.
	a.now = func() time.Time { return clock }

	keys, err := sessions.ParsePublicKeys(hubKey.Public())
	if err != nil {
		t.Fatal(err)
	}
	tenant, err := sessions.New(sessions.Config{
		Origin: "https://ppl.test",
		Signed: &sessions.Signed{Name: "ppl", Keys: keys},
		Now:    func() time.Time { return clock },
	})
	if err != nil {
		t.Fatal(err)
	}
	tenantMux := http.NewServeMux()
	tenantMux.Handle("GET /{$}", tenant.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, _ := sessions.Current(r)
		fmt.Fprintf(w, "hello %s", s.Subject)
	})))
	tenantMux.Handle("POST /posts", tenant.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, r.FormValue("body"))
	})))

	mux := http.NewServeMux()
	// Stand-ins for the hub's pages, so a redirect to them ends in a 200
	// the test can name rather than a 404 or 405.
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "hub home") })
	mux.HandleFunc("GET /signin", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "sign-in page") })
	mux.HandleFunc("POST /signin", a.Begin)
	mux.HandleFunc("/auth/verify", a.Verify)
	mux.HandleFunc("POST /signout-everywhere", a.SignoutEverywhere)
	mux.HandleFunc("/_carlos/refresh", a.Refresh)
	mux.Handle("/acme/", http.StripPrefix("/acme", tenantMux))
	origin := csrf.Protect("https://ppl.test")(mux)

	cookie := a.SessionCookie()
	b := newOriginBrowser(t, origin)
	wantPage := func(b *originBrowser, method, target string, form url.Values, body string, hops []hop) {
		t.Helper()
		w, got, _ := b.visit(method, target, form)
		if w.Code != http.StatusOK || w.Body.String() != body {
			t.Fatalf("%s %s: %d %q, want 200 %q", method, target, w.Code, w.Body.String(), body)
		}
		if !slices.Equal(got, hops) {
			t.Fatalf("%s %s redirected %v, want %v", method, target, got, hops)
		}
	}

	// 1. Sign in at the hub; the tenant reads the same cookie.
	b.signIn(m, cookie)
	uid := subjectOf(t, a, b.jar[cookie])
	wantPage(b, http.MethodGet, "/acme/", nil, "hello "+uid, nil)

	// 2. The cookie expires: the tenant sends the browser to the hub's
	//    refresh route and the hub sends it back with a new one.
	clock = clock.Add(16 * time.Minute)
	wantPage(b, http.MethodGet, "/acme/", nil, "hello "+uid, []hop{
		{http.StatusSeeOther, "https://ppl.test/_carlos/refresh?next=%2Facme%2F"},
		{http.StatusSeeOther, "/acme/"},
	})

	// 3. A form posted on an expired cookie survives the refresh.
	clock = clock.Add(16 * time.Minute)
	wantPage(b, http.MethodPost, "/acme/posts", url.Values{"body": {"draft"}}, "draft", []hop{
		{http.StatusTemporaryRedirect, "https://ppl.test/_carlos/refresh?next=%2Facme%2Fposts"},
		{http.StatusTemporaryRedirect, "/acme/posts"},
	})

	// 4. Sign out everywhere reaches a copy of the cookie at its next
	//    refresh, which is within 15 minutes.
	stolen := newOriginBrowser(t, origin)
	stolen.jar[cookie] = b.jar[cookie]
	if w, _, at := b.visit(http.MethodPost, "/signout-everywhere", nil); w.Code != http.StatusOK || at != "/signin" {
		t.Fatalf("sign out everywhere ended at %q with %d", at, w.Code)
	}
	if b.jar[cookie] != nil {
		t.Fatal("this browser still holds a session cookie after signing out everywhere")
	}
	// The copy still works until it expires: nothing tells a tenant
	// about the epoch, so this is the window the spec allows.
	wantPage(stolen, http.MethodGet, "/acme/", nil, "hello "+uid, nil)
	clock = clock.Add(16 * time.Minute)
	if w, _, at := stolen.visit(http.MethodGet, "/acme/", nil); w.Code != http.StatusOK || at != "/signin?return_to=%2Facme%2F" {
		t.Fatalf("the copied cookie ended at %q with %d, want the sign-in page", at, w.Code)
	}
	if stolen.jar[cookie] != nil {
		t.Fatal("the copied cookie was not cleared at the refresh")
	}

	// 5. Two sign-ins, two refreshes and an epoch move made one user.
	if u, e := keysUnder(t, store, "users/"), keysUnder(t, store, "emails/"); u != 1 || e != 1 {
		t.Fatalf("%d users/ and %d emails/ keys, want 1 each", u, e)
	}

	// 6. Cross-site posts to the two hub routes that can clear the
	//    cookie are refused by the handlers themselves. They are mounted
	//    without csrf.Protect, which would refuse them on its own and
	//    so pass this step with the handlers' checks deleted.
	b.signIn(m, cookie)
	bare := http.NewServeMux()
	bare.HandleFunc("/_carlos/refresh", a.Refresh)
	bare.HandleFunc("/signout-everywhere", a.SignoutEverywhere)
	before := b.jarValues()
	for _, target := range []string{"/_carlos/refresh?next=%2Facme%2F", "/signout-everywhere"} {
		// No cookie on the request: SameSite=Lax withholds it from a
		// cross-site POST, but the browser still applies the answer.
		r := httptest.NewRequest(http.MethodPost, target, nil)
		r.Host = "ppl.test"
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		w := httptest.NewRecorder()
		bare.ServeHTTP(w, r)
		b.apply(w)
		if w.Code != http.StatusForbidden {
			t.Errorf("cross-site POST %s: %d, want 403", target, w.Code)
		}
	}
	if after := b.jarValues(); !maps.Equal(before, after) {
		t.Fatalf("cross-site posts changed the jar: %v, then %v", before, after)
	}
	wantPage(b, http.MethodGet, "/acme/", nil, "hello "+uid, nil)
}
