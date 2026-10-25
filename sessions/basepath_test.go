package sessions_test

import (
	"bytes"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/basepath"
	"amadan.net/rastrillo/rastrillo/sessions"
)

const product = "https://example.com"

var (
	underAcme = basepath.Config{Origin: product, Slug: "acme"}
	direct    = basepath.Config{Origin: product, Slug: "acme", DirectHost: basepath.ServeDirect}
)

// rowStore is a row-backed store on the product origin, so its cookie
// is the __Host-rastrillo_session every tenant there would share. Its
// log goes to the returned buffer.
func rowStore(t *testing.T) (*sessions.Sessions, *sql.DB, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	s, db := newTestSessions(t, func(c *sessions.Config) {
		c.Origin = product
		c.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	})
	return s, db, &logs
}

func rows(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// serve runs h behind cfg's handler for one request and returns the
// response.
func serve(cfg basepath.Config, h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	cfg.Handler(h).ServeHTTP(w, r)
	return w
}

func withCookie(r *http.Request, c *http.Cookie) *http.Request {
	if c != nil {
		r.AddCookie(c)
	}
	return r
}

func TestSignInIsRefusedUnderABasePath(t *testing.T) {
	s, db, logs := rowStore(t)
	token, err := s.Mint(sessions.Session{Subject: "42"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		cfg  basepath.Config
		url  string
		path string
	}{
		{"under /acme", underAcme, product + "/acme/x", "/acme/x"},
		{"on the direct host", direct, "http://cv.paulca.com/x", "/x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := rows(t, db)
			logs.Reset()
			var signInErr error
			w := serve(tc.cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				signInErr = s.SignIn(w, r, sessions.Session{Subject: "42", Method: "password"})
			}), httptest.NewRequest("GET", tc.url, nil))
			if !errors.Is(signInErr, sessions.ErrPathRouted) {
				t.Errorf("SignIn: %v, want ErrPathRouted", signInErr)
			}
			if cs := w.Result().Cookies(); len(cs) != 0 {
				t.Errorf("SignIn set %v: on a shared origin that cookie signs every other tenant out", cs)
			}
			if n := rows(t, db); n != before {
				t.Errorf("SignIn left %d rows, want %d", n, before)
			}
			if !strings.Contains(logs.String(), "level=ERROR") || !strings.Contains(logs.String(), tc.path) {
				t.Errorf("SignIn logged %q, want an error naming %s", logs.String(), tc.path)
			}

			logs.Reset()
			var adopted bool
			w = serve(tc.cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, adopted = s.Adopt(w, r, token)
			}), httptest.NewRequest("GET", tc.url, nil))
			if adopted {
				t.Error("Adopt adopted a live token under a base path")
			}
			if cs := w.Result().Cookies(); len(cs) != 0 {
				t.Errorf("Adopt set %v", cs)
			}
			if !strings.Contains(logs.String(), "level=ERROR") || !strings.Contains(logs.String(), tc.path) {
				t.Errorf("Adopt logged %q, want an error naming %s", logs.String(), tc.path)
			}
		})
	}
}

func TestTheStoreNeverTouchesASharedCookie(t *testing.T) {
	s, db, _ := rowStore(t)
	// The hub's own: a token this tenant's table has never seen.
	hubs := &http.Cookie{Name: "__Host-rastrillo_session", Value: "the-hubs-own-token"}
	// And one it has, to show SignOut revokes nothing either.
	token, err := s.Mint(sessions.Session{Subject: "42"})
	if err != nil {
		t.Fatal(err)
	}
	known := &http.Cookie{Name: s.CookieName(), Value: token}

	for name, c := range map[string]*http.Cookie{"the hub's cookie": hubs, "a known token": known} {
		var found bool
		serve(underAcme, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, found = s.From(r)
		}), withCookie(httptest.NewRequest("GET", product+"/acme/x", nil), c))
		if found {
			t.Errorf("From read a session from %s under /acme", name)
		}
	}

	w := serve(underAcme, s.Middleware(ok), withCookie(httptest.NewRequest("GET", product+"/acme/x", nil), hubs))
	if cs := w.Result().Cookies(); len(cs) != 0 {
		t.Errorf("Middleware set %v: clearing the hub's cookie signs the reader out of the product", cs)
	}

	before := rows(t, db)
	for _, c := range []*http.Cookie{hubs, known} {
		w := serve(underAcme, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s.SignOut(w, r)
		}), withCookie(httptest.NewRequest("POST", product+"/acme/signout", nil), c))
		if cs := w.Result().Cookies(); len(cs) != 0 {
			t.Errorf("SignOut set %v", cs)
		}
	}
	if n := rows(t, db); n != before {
		t.Errorf("SignOut under /acme left %d rows, want %d: it revoked one", n, before)
	}
}

// refusals checks what Require does on the direct host, where no
// session lives: a page goes to the product path with its slug, a form
// gets 403, and the platform's own call gets 403 rather than a redirect
// its client would follow into TLS.
func refusals(t *testing.T, h http.Handler, c *http.Cookie) {
	t.Helper()
	w := serve(direct, h, withCookie(httptest.NewRequest("GET", "http://cv.paulca.com/es/private?x=1", nil), c))
	if want := product + "/acme/es/private?x=1"; w.Code != http.StatusSeeOther || w.Header().Get("Location") != want {
		t.Errorf("GET: %d %q, want 303 %q", w.Code, w.Header().Get("Location"), want)
	}
	w = serve(direct, h, withCookie(httptest.NewRequest("POST", "http://cv.paulca.com/private", nil), c))
	if w.Code != http.StatusForbidden {
		t.Errorf("POST: %d %q, want 403", w.Code, w.Header().Get("Location"))
	}
	w = serve(underAcme, h, httptest.NewRequest("GET", "http://carlos/", nil))
	if w.Code != http.StatusForbidden || w.Header().Get("Location") != "" {
		t.Errorf("wake check: %d %q, want 403 and no Location", w.Code, w.Header().Get("Location"))
	}
}

func TestADirectHostHasNoSession(t *testing.T) {
	s, _, _ := rowStore(t)
	w := httptest.NewRecorder()
	if err := s.SignIn(w, httptest.NewRequest("POST", product+"/signin", nil), sessions.Session{Subject: "42"}); err != nil {
		t.Fatal(err)
	}
	c := cookieFrom(t, w, s.CookieName())

	var found bool
	serve(direct, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, found = s.From(r)
	}), withCookie(httptest.NewRequest("GET", "http://cv.paulca.com/x", nil), c))
	if found {
		t.Error("From read a session on the direct host")
	}
	refusals(t, s.Require(ok), c)
}

// Plan 2 already does this, so it passes before the change: it pins what
// the change must keep.
func TestASignedTenantVerifiesUnderABasePath(t *testing.T) {
	p := newPairOn(t, product)
	c := p.signIn(t, "u1")

	var (
		sess    sessions.Session
		found   bool
		signErr error
		adopted bool
	)
	w := serve(underAcme, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, found = p.tenant.From(r)
		signErr = p.tenant.SignIn(httptest.NewRecorder(), r, sessions.Session{Subject: "u1"})
		_, adopted = p.tenant.Adopt(httptest.NewRecorder(), r, "x")
		p.tenant.SignOut(w, r)
	}), withCookie(httptest.NewRequest("GET", product+"/acme/x", nil), c))
	if !found || sess.Subject != "u1" {
		t.Errorf("From = %+v, %v; want the hub's u1", sess, found)
	}
	if !errors.Is(signErr, sessions.ErrVerifyOnly) || errors.Is(signErr, sessions.ErrPathRouted) {
		t.Errorf("SignIn: %v, want signed mode's ErrVerifyOnly", signErr)
	}
	if adopted {
		t.Error("Adopt adopted a token in signed mode")
	}
	if cs := w.Result().Cookies(); len(cs) != 1 || cs[0].Name != "__Host-ppl" || cs[0].MaxAge >= 0 {
		t.Errorf("SignOut set %v, want signed mode's clearing of __Host-ppl", cs)
	}

	w = serve(underAcme, p.tenant.Require(ok), withCookie(httptest.NewRequest("GET", product+"/acme/x", nil), c))
	if w.Code != 200 || w.Body.String() != "hello u1" {
		t.Errorf("Require: %d %q", w.Code, w.Body)
	}
}

func TestASignedTenantOnADirectHostIsSignedOut(t *testing.T) {
	p := newPairOn(t, product)
	c := p.signIn(t, "u1")

	var found bool
	serve(direct, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, found = p.tenant.From(r)
	}), withCookie(httptest.NewRequest("GET", "http://cv.paulca.com/x", nil), c))
	if found {
		t.Error("From verified the hub's session on the direct host, which carries none")
	}
	refusals(t, p.tenant.Require(ok), c)

	// An expired cookie there is no cookie either: a refresh from the
	// direct host would carry a next with no slug.
	p.now = p.now.Add(16 * time.Minute)
	w := serve(direct, p.tenant.Middleware(ok), withCookie(httptest.NewRequest("GET", "http://cv.paulca.com/x", nil), c))
	if w.Code != 200 {
		t.Errorf("Middleware: %d %q, want the page", w.Code, w.Header().Get("Location"))
	}
}

// Plan 2 already does this, so it passes before the change: an absolute
// Location is never rewritten.
func TestAnExpiredSignedCookieStillRefreshesThroughTheBase(t *testing.T) {
	p := newPairOn(t, product)
	c := p.signIn(t, "u1")
	p.now = p.now.Add(16 * time.Minute)
	// Origin-form, as a browser sends it: next is built from RequestURI,
	// which keeps the slug only in that form.
	r := httptest.NewRequest("GET", "/acme/orders", nil)
	r.Host = "example.com"
	w := serve(underAcme, p.tenant.Require(ok), withCookie(r, c))
	want := product + "/_carlos/refresh?next=" + url.QueryEscape("/acme/orders")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != want {
		t.Errorf("%d %q, want 303 %q", w.Code, w.Header().Get("Location"), want)
	}
}
