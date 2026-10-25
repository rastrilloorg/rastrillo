package auth

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/basepath"
	"amadan.net/rastrillo/rastrillo/sessions"
)

const product = "https://example.com"

// refusedAt is every place a tenant's sign-in must refuse: under its
// base path and on its direct host. prefix goes in front of a route's
// usual path, and base in front of the path the log names.
var refusedAt = []struct {
	name         string
	cfg          basepath.Config
	prefix, base string
}{
	{"under /acme", basepath.Config{Origin: product, Slug: "acme"}, product + "/acme", "/acme"},
	{"on the direct host", basepath.Config{Origin: product, Slug: "acme", DirectHost: basepath.ServeDirect}, "http://cv.paulca.com", ""},
}

func serveAt(cfg basepath.Config, h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	cfg.Handler(h).ServeHTTP(w, r)
	return w
}

// entry is one call to an entry point, with what would succeed at the
// root.
type entry struct {
	name, method, path string
	h                  http.HandlerFunc
	form               url.Values
	cookies            []*http.Cookie
}

func (e entry) request(prefix string) *http.Request {
	var body io.Reader
	if e.form != nil {
		body = strings.NewReader(e.form.Encode())
	}
	r := httptest.NewRequest(e.method, prefix+e.path, body)
	if e.form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	for _, c := range e.cookies {
		r.AddCookie(c)
	}
	return r
}

// checkRefused holds one answer to the refusal: 404, no cookie, and one
// error that names the package and the public path.
func checkRefused(t *testing.T, e entry, w *httptest.ResponseRecorder, logs *bytes.Buffer, base string) {
	t.Helper()
	if w.Code != http.StatusNotFound {
		t.Errorf("%s: %d %q, want 404", e.name, w.Code, w.Header().Get("Location"))
	}
	if cs := w.Result().Cookies(); len(cs) != 0 {
		t.Errorf("%s set %v: on a shared origin that cookie is every tenant's", e.name, cs)
	}
	path, _, _ := strings.Cut(e.path, "?")
	if n := strings.Count(logs.String(), "level=ERROR"); n != 1 || !strings.Contains(logs.String(), "rastrillo/auth") || !strings.Contains(logs.String(), base+path) {
		t.Errorf("%s logged %q, want one error naming rastrillo/auth and %s", e.name, logs.String(), base+path)
	}
}

// tableRows is the row count of every table in d.
func tableRows(t *testing.T, d *sql.DB) int {
	t.Helper()
	rows, err := d.Query(`SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	rows.Close()
	total := 0
	for _, n := range names {
		var c int
		if err := d.QueryRow(`SELECT COUNT(*) FROM "` + n + `"`).Scan(&c); err != nil {
			t.Fatal(err)
		}
		total += c
	}
	return total
}

var tokenRE = regexp.MustCompile(`/auth/verify\?token=([A-Za-z0-9_-]+)`)

// linkAt mails a link at the root and returns its token.
func linkAt(t *testing.T, a *Auth, m *captureMailer) string {
	t.Helper()
	beginSignin(t, a, "ana@example.com")
	match := tokenRE.FindStringSubmatch(m.sentBody())
	if match == nil {
		t.Fatalf("no link in %q", m.sentBody())
	}
	m.forget()
	return match[1]
}

func TestSignInRefusesUnderABasePath(t *testing.T) {
	var logs bytes.Buffer
	a, m := newTestAuth(t, func(c *Config) {
		c.Origin = product
		c.SigninScreen = true
		c.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	})
	w := httptest.NewRecorder()
	if err := a.sessions.SignIn(w, httptest.NewRequest("POST", product+"/", nil), sessions.Session{Subject: "ana@example.com"}); err != nil {
		t.Fatal(err)
	}
	session := w.Result().Cookies()[0]
	pending := &http.Cookie{Name: a.pendingCookie(), Value: "a-pending-attempt"}

	for _, at := range refusedAt {
		t.Run(at.name, func(t *testing.T) {
			token := linkAt(t, a, m)
			for _, e := range []entry{
				{name: "Begin", method: "POST", path: "/signin", h: a.Begin, form: url.Values{"address": {"ana@example.com"}, "force": {"1"}}},
				{name: "AnswerAsSent", method: "POST", path: "/signin", h: a.AnswerAsSent, form: url.Values{"address": {"ana@example.com"}}},
				{name: "Callback", method: "GET", path: "/auth/callback?code=c&state=s", h: a.Callback, cookies: []*http.Cookie{pending}},
				{name: "Verify GET", method: "GET", path: "/auth/verify?token=" + token, h: a.Verify},
				{name: "Verify POST", method: "POST", path: "/auth/verify", h: a.Verify, form: url.Values{"token": {token}}},
				{name: "Signout", method: "POST", path: "/signout", h: a.Signout, cookies: []*http.Cookie{session}},
				{name: "Forget", method: "POST", path: "/signin/forget", h: a.Forget, form: url.Values{}},
			} {
				logs.Reset()
				m.forget()
				before := tableRows(t, a.cfg.DB)
				w := serveAt(at.cfg, e.h, e.request(at.prefix))
				checkRefused(t, e, w, &logs, at.base)
				if n := tableRows(t, a.cfg.DB); n != before {
					t.Errorf("%s: %d rows, want %d", e.name, n, before)
				}
				if to := m.sentTo(); to != "" {
					t.Errorf("%s mailed %s", e.name, to)
				}
			}
		})
	}
}

// The records-mode doors: SignoutEverywhere would move the reader's
// epoch, and so sign them out of the hub and every tenant, and Refresh
// would set the product's cookie from under a tenant.
func TestRecordsDoorsRefuseUnderABasePath(t *testing.T) {
	var logs bytes.Buffer
	a, m, store := newRecordsAuth(t, func(c *Config) {
		c.Origin = product
		c.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	})
	frozen := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return frozen }
	w := httptest.NewRecorder()
	a.Verify(w, redeemRequest(t, product+"/auth/verify?token="+linkAt(t, a, m)))
	session := sessionCookieSet(a, w)
	if session == nil {
		t.Fatalf("no session at the root: %d %q", w.Code, w.Header().Get("Location"))
	}
	uid := subjectOf(t, a, session)

	for _, at := range refusedAt {
		t.Run(at.name, func(t *testing.T) {
			for _, e := range []entry{
				{name: "SignoutEverywhere", method: "POST", path: "/signout-everywhere", h: a.SignoutEverywhere, cookies: []*http.Cookie{session}},
				{name: "Refresh", method: "GET", path: "/_carlos/refresh?next=/", h: a.Refresh, cookies: []*http.Cookie{session}},
			} {
				// Expired for Refresh, which would reissue it; valid for
				// SignoutEverywhere, which would end it.
				a.now = func() time.Time { return frozen }
				if e.name == "Refresh" {
					a.now = func() time.Time { return frozen.Add(16 * time.Minute) }
				}
				logs.Reset()
				keys := keysUnder(t, store, "")
				before, _ := a.User(context.Background(), uid)
				w := serveAt(at.cfg, e.h, e.request(at.prefix))
				checkRefused(t, e, w, &logs, at.base)
				if n := keysUnder(t, store, ""); n != keys {
					t.Errorf("%s: %d records, want %d", e.name, n, keys)
				}
				if after, _ := a.User(context.Background(), uid); after.Epoch != before.Epoch {
					t.Errorf("%s moved the epoch from %d to %d", e.name, before.Epoch, after.Epoch)
				}
			}
		})
	}

	// The hub runs Refresh at the origin's root, outside any base path,
	// and the guard must never stand in its way.
	a.now = func() time.Time { return frozen.Add(16 * time.Minute) }
	r := httptest.NewRequest("GET", product+"/_carlos/refresh?next=/", nil)
	r.AddCookie(session)
	w = httptest.NewRecorder()
	a.Refresh(w, r)
	if c := sessionCookieSet(a, w); c == nil || c.Value == "" {
		t.Errorf("Refresh at the root: %d %q and no cookie, want the session reissued", w.Code, w.Header().Get("Location"))
	}
}

// The direct host carries no session (spec §7.3), so a signed-out page
// goes to the same page on the product path, where the hub's session
// lives; the platform's own call gets 403, because its client would
// follow a redirect into TLS on a plain socket.
func TestRequireSessionOnADirectHostGoesToTheProduct(t *testing.T) {
	a, _ := newTestAuth(t, func(c *Config) { c.Origin = product })
	direct := refusedAt[1].cfg
	ok := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("a signed-out request got through") })
	for name, guard := range map[string]http.Handler{
		"RequireSession":      a.RequireSession(ok),
		"RequireFreshSession": a.RequireFreshSession(time.Minute)(ok),
	} {
		w := serveAt(direct, guard, httptest.NewRequest("GET", "http://cv.paulca.com/es/private?x=1", nil))
		if want := product + "/acme/es/private?x=1"; w.Code != http.StatusSeeOther || w.Header().Get("Location") != want {
			t.Errorf("%s: %d %q, want 303 to %s", name, w.Code, w.Header().Get("Location"), want)
		}
		w = serveAt(direct, guard, httptest.NewRequest("GET", "http://carlos/", nil))
		if w.Code != http.StatusForbidden || w.Header().Get("Location") != "" {
			t.Errorf("%s, Host carlos: %d %q, want 403 and no Location", name, w.Code, w.Header().Get("Location"))
		}
	}
}

// SigninState has no response to refuse with, so under a base path it
// reads nothing and offers nothing: the cookies it would clear may be
// the hub's.
func TestSigninStateIsEmptyUnderABasePath(t *testing.T) {
	a, _ := newProofAuth(t, func(c *Config) {
		c.Origin = product
		c.SigninScreen = true
	})
	request := func(target string) *http.Request {
		r := httptest.NewRequest("GET", target, nil)
		r.AddCookie(&http.Cookie{Name: a.jar.CookieName(), Value: "not-a-record"})
		r.AddCookie(&http.Cookie{Name: a.attemptCookie(), Value: "not-an-attempt"})
		return r
	}
	if st := a.SigninState(request(product + "/signin")); len(st.clear) != 2 {
		t.Fatalf("at the root SigninState clears %v, want both cookies: the test needs cookies it would clear", st.clear)
	}
	for _, at := range refusedAt {
		var st SigninState
		w := serveAt(at.cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			st = a.SigninState(r)
			a.PrepareSigninResponse(w, st)
		}), request(at.prefix+"/signin"))
		if !st.Unavailable || len(st.clear) != 0 || st.Proof != nil || st.BeginPath != "" || st.ForgetPath != "" {
			t.Errorf("%s: Unavailable %v, clear %v, proof %v, BeginPath %q, ForgetPath %q; want an unavailable state with nothing in it",
				at.name, st.Unavailable, st.clear, st.Proof != nil, st.BeginPath, st.ForgetPath)
		}
		if cs := w.Result().Cookies(); len(cs) != 0 {
			t.Errorf("%s: PrepareSigninResponse set %v", at.name, cs)
		}
		// The app sets Passkey on whatever state it gets.
		st.Passkey = &PasskeyDoor{BeginPath: "/passkey/discover/begin", FinishPath: "/passkey/discover/finish"}
		if f := st.PasskeyFinish(); f != "" {
			t.Errorf("%s: PasskeyFinish %q, want none", at.name, f)
		}
	}
}
