// Package notestest is the permanent regression host for the notes
// example: every test here drives the real app (notes.App) through
// httptest — the same wiring cmd/notes/main.go serves — with real
// cookie-jar HTTP clients over the real sign-up/sign-in forms, never
// a shortcut around sessions or the owner scope.
package notestest

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/background"
	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/pow/powtest"

	"notes/internal/notes"
)

// newApp boots a whole app per test: a fresh file-backed SQLite
// database and the real notes.App, exactly as cmd/notes/main.go wires
// it. The server's own URL becomes the app's origin, so a same-origin
// POST that sets Origin: ts.URL passes csrf.Protect.
func newApp(t *testing.T) *httptest.Server {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "notes.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })

	// The server's origin has to be known before notes.App can build
	// its csrf.Protect/sessions wiring, and the app's mux has to exist
	// before the server can serve it — so resolve the URL from the
	// still-unstarted listener (identical to what Start() computes)
	// and only assign Config.Handler before Start() runs, never after.
	ts := httptest.NewUnstartedServer(nil)
	origin := "http://" + ts.Listener.Addr().String()
	bg := &background.Group{}
	t.Cleanup(bg.Stop)
	mux, err := notes.App(d, origin, "notes-test-instance-key", bg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("notes.App: %v", err)
	}
	ts.Config.Handler = mux
	ts.Start()
	t.Cleanup(ts.Close)
	return ts
}

// client is one visitor's HTTP session: a cookie jar (so signed-in
// state persists across requests, exactly as a browser would) plus
// the origin every POST must carry to pass csrf.Protect.
type client struct {
	t      *testing.T
	ts     *httptest.Server
	c      *http.Client
	origin string
}

func newClient(t *testing.T, ts *httptest.Server) *client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	return &client{
		t: t, ts: ts,
		c:      &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		origin: ts.URL,
	}
}

// get issues a GET, following redirects (net/http's default policy —
// only postForm overrides it, since a test usually wants to inspect a
// POST's redirect target explicitly).
func (cl *client) get(path string) *http.Response {
	cl.t.Helper()
	c := &http.Client{Jar: cl.c.Jar}
	resp, err := c.Get(cl.ts.URL + path)
	if err != nil {
		cl.t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

// postForm submits an application/x-www-form-urlencoded POST with the
// Origin header a browser form submission carries, WITHOUT following
// the redirect — so a test can assert on the redirect itself (target,
// status) before choosing whether to follow it.
func (cl *client) postForm(path string, vals url.Values) *http.Response {
	cl.t.Helper()
	req, err := http.NewRequest(http.MethodPost, cl.ts.URL+path, strings.NewReader(vals.Encode()))
	if err != nil {
		cl.t.Fatalf("NewRequest POST %s: %v", path, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", cl.origin)
	resp, err := cl.c.Do(req)
	if err != nil {
		cl.t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

// filled GETs path and returns form with the page's challenge solved,
// as the browser would post it. It waits out the example's 500ms
// minimum age first; a person always has.
func (cl *client) filled(path string, form url.Values) url.Values {
	cl.t.Helper()
	resp, err := cl.c.Get(cl.ts.URL + path)
	if err != nil {
		cl.t.Fatalf("GET %s: %v", path, err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	time.Sleep(600 * time.Millisecond)
	return powtest.Fill(cl.t, body, form)
}

// signup drives the real HTTP signup flow and returns the client
// signed in as the new user.
func (cl *client) signup(email, password string) *http.Response {
	cl.t.Helper()
	return cl.postForm("/signup", cl.filled("/signup", url.Values{"email": {email}, "password": {password}}))
}

// signin drives the real HTTP signin flow.
func (cl *client) signin(email, password string) *http.Response {
	cl.t.Helper()
	return cl.postForm("/signin", cl.filled("/signin", url.Values{"email": {email}, "password": {password}}))
}

// body reads and closes a response's body, for tests that just want
// the text.
func body(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}
