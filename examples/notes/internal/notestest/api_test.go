// api_test.go proves the third door of this app, the JSON API, the
// way bookmarks_test.go proves the generated screens: through the real
// notes.App, with a token minted on the real token page by a person
// who signed up through the real form. Nothing here reaches into
// apitoken.Store or the database: if the page cannot mint, or the
// tree is not mounted, or the generated door does not honour the
// owner scope, this file is what fails.
package notestest

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"

	genlocales "notes/gen/locales"
)

// tokenRE is the token grammar with this app's prefix: notes, which
// apitoken.DerivePrefix makes from the app name. The reveal is the
// only place the whole token is ever written, so it is read out of
// the create POST's own body, as a person would copy it.
var tokenRE = regexp.MustCompile(`notes_([0-9A-Za-z]{12})_[0-9A-Za-z]{43}`)

// mint drives the token page's create form and returns the token and
// its public ID. The page answers the POST itself (no redirect): the
// response is the one copy of the secret.
func (cl *client) mint(agent, access string) (token, id string) {
	cl.t.Helper()
	form := url.Values{"access": {access}, "expiry": {"90"}}
	if agent == "" {
		form.Set("for_self", "on")
	} else {
		form.Set("agent", agent)
	}
	resp := cl.postForm("/tokens", form)
	b := body(cl.t, resp)
	if resp.StatusCode != http.StatusOK {
		cl.t.Fatalf("POST /tokens = %d, want 200; body=%s", resp.StatusCode, b)
	}
	if !strings.Contains(b, "Copy this token now.") {
		cl.t.Fatalf("the create response has no reveal; body=%s", b)
	}
	m := tokenRE.FindStringSubmatch(b)
	if m == nil {
		cl.t.Fatalf("the create response shows no token; body=%s", b)
	}
	return m[0], m[1]
}

// api sends one request to the tree as an agent would: a bearer token,
// JSON when there is a body, no cookies, and any extra headers as
// name/value pairs.
func api(t *testing.T, ts *httptest.Server, method, path, token, reqBody string, header ...string) *http.Response {
	t.Helper()
	var r io.Reader
	if reqBody != "" {
		r = strings.NewReader(reqBody)
	}
	req, err := http.NewRequest(method, ts.URL+path, r)
	if err != nil {
		t.Fatalf("NewRequest %s %s: %v", method, path, err)
	}
	if reqBody != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	// No redirects followed: a redirect from the tree would itself be
	// the bug (the outer mux answering a bare /api/v1 with a 301).
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

// wantJSON checks the tree's invariants on every answer: the status,
// a JSON body, and Cache-Control: no-store, so a token's answer is
// never cached by a proxy between the agent and the app.
func wantJSON(t *testing.T, resp *http.Response, status int) string {
	t.Helper()
	b := body(t, resp)
	if resp.StatusCode != status {
		t.Fatalf("%s %s = %d, want %d; body=%s", resp.Request.Method, resp.Request.URL.Path, resp.StatusCode, status, b)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("%s %s: Content-Type %q, want application/json; body=%s", resp.Request.Method, resp.Request.URL.Path, ct, b)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("%s %s: Cache-Control %q, want no-store", resp.Request.Method, resp.Request.URL.Path, cc)
	}
	return b
}

// TestTokensAndAPIEndToEnd is the whole path a person and their agent
// walk: sign up, open the token page, mint a token, and use it on the
// API; then the two refusals that matter most (another person's token,
// a read-only token), and the kill switch.
func TestTokensAndAPIEndToEnd(t *testing.T) {
	var logs syncBuffer
	ts := newAppLogging(t, genlocales.DefaultLocale, genlocales.Locales, &logs)
	alice := newClient(t, ts)
	alice.signup("alice@example.com", "hunter2222").Body.Close()

	// The page, empty, inside the app's own chrome. A credential page
	// is never cached.
	page := alice.get("/tokens")
	pageBody := body(t, page)
	if page.StatusCode != http.StatusOK {
		t.Fatalf("GET /tokens = %d, want 200; body=%s", page.StatusCode, pageBody)
	}
	for _, want := range []string{"API tokens", "No tokens yet.", "/signout"} {
		if !strings.Contains(pageBody, want) {
			t.Fatalf("GET /tokens is missing %q; body=%s", want, pageBody)
		}
	}
	if cc := page.Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("GET /tokens: Cache-Control %q, want no-store", cc)
	}

	// Mint for Claude: the sign-up a moment ago is a fresh sign-in, so
	// RequireFresh lets the create through.
	claude, claudeID := alice.mint("Claude", "write")

	// The page now lists the token by its public ID and as "Claude for
	// alice@example.com", the actor phrase with the app's own Name
	// lookup, and the secret is nowhere on it.
	pageBody = body(t, alice.get("/tokens"))
	if !strings.Contains(pageBody, claudeID) || !strings.Contains(pageBody, "Claude for alice@example.com") {
		t.Fatalf("the page does not list the new token as Claude for alice; body=%s", pageBody)
	}
	if strings.Contains(pageBody, claude) {
		t.Fatal("the token page shows the whole token again after the reveal")
	}

	// The API, as an agent uses it.
	create := api(t, ts, http.MethodPost, "/api/v1/bookmarks", claude,
		`{"title":"CARLOS docs","link":"https://carlosframework.com","notes":"read the platform half"}`,
		"Agent-Session", "sess-1")
	created := wantJSON(t, create, http.StatusCreated)
	loc := create.Header.Get("Location")
	if !strings.HasPrefix(loc, "/api/v1/bookmarks/") {
		t.Fatalf("Location = %q, want /api/v1/bookmarks/<id>", loc)
	}
	if !strings.Contains(created, `"title":"CARLOS docs"`) {
		t.Fatalf("the 201 body is not the bookmark; body=%s", created)
	}

	// Attribution. This app has no eventlog, so the request log is the
	// audit trail: the write is the agent's, for alice, on this token,
	// with the session the agent claimed. The subject is whatever the
	// session plugin issued; the test does not assume its form.
	line := logs.String()
	actorRE := regexp.MustCompile(`actor="agent:Claude for user:[^ "]+ token:` + claudeID + `"`)
	if !actorRE.MatchString(line) {
		t.Fatalf("the request log does not attribute the write to Claude for alice on token %s; log=%s", claudeID, line)
	}
	if !strings.Contains(line, "agent_session=sess-1") {
		t.Fatalf("the request log does not carry the agent's session; log=%s", line)
	}
	if strings.Contains(line, claude) {
		t.Fatal("the whole token reached the log")
	}

	list := wantJSON(t, api(t, ts, http.MethodGet, "/api/v1/bookmarks", claude, ""), http.StatusOK)
	if !strings.Contains(list, "CARLOS docs") {
		t.Fatalf("GET /api/v1/bookmarks does not list the bookmark; body=%s", list)
	}
	got := wantJSON(t, api(t, ts, http.MethodGet, loc, claude, ""), http.StatusOK)
	if !strings.Contains(got, "read the platform half") {
		t.Fatalf("GET %s does not show the bookmark; body=%s", loc, got)
	}

	// The tree sits outside sessions and CSRF: no token is a JSON 401
	// with the challenge, not a redirect to /signin. The bare mount
	// answers too, so the outer mux never 301s /api/v1 to /api/v1/.
	noToken := api(t, ts, http.MethodGet, "/api/v1/bookmarks", "", "")
	wantJSON(t, noToken, http.StatusUnauthorized)
	if noToken.Header.Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("401 without WWW-Authenticate: Bearer; headers=%v", noToken.Header)
	}
	bare := wantJSON(t, api(t, ts, http.MethodGet, "/api/v1", "", ""), http.StatusNotFound)
	if !strings.Contains(bare, `"not_found"`) {
		t.Fatalf("GET /api/v1 is not the tree's 404; body=%s", bare)
	}

	// Another person's token: her bookmark does not exist for him. The
	// generated operation filters by the token's subject, never by
	// anything the request says.
	bob := newClient(t, ts)
	bob.signup("bob@example.com", "hunter2222").Body.Close()
	bobsClaude, _ := bob.mint("Claude", "write")
	wantJSON(t, api(t, ts, http.MethodGet, loc, bobsClaude, ""), http.StatusNotFound)
	wantJSON(t, api(t, ts, http.MethodPatch, loc, bobsClaude, `{"title":"mine now"}`), http.StatusNotFound)
	wantJSON(t, api(t, ts, http.MethodDelete, loc, bobsClaude, ""), http.StatusNotFound)
	if bobs := wantJSON(t, api(t, ts, http.MethodGet, "/api/v1/bookmarks", bobsClaude, ""), http.StatusOK); strings.Contains(bobs, "CARLOS docs") {
		t.Fatalf("bob's list shows alice's bookmark; body=%s", bobs)
	}
	if mine := wantJSON(t, api(t, ts, http.MethodGet, loc, claude, ""), http.StatusOK); !strings.Contains(mine, "CARLOS docs") {
		t.Fatalf("alice's bookmark changed under bob's attempts; body=%s", mine)
	}

	// A read-only token reads and nothing else.
	reader, _ := alice.mint("Nightly report", "read")
	forbidden := wantJSON(t, api(t, ts, http.MethodPost, "/api/v1/bookmarks", reader, `{"title":"x"}`), http.StatusForbidden)
	if !strings.Contains(forbidden, `"forbidden"`) {
		t.Fatalf("read-only POST is not the forbidden code; body=%s", forbidden)
	}
	wantJSON(t, api(t, ts, http.MethodGet, "/api/v1/bookmarks", reader, ""), http.StatusOK)

	// The kill switch: revoke on the page, and the very next call is
	// refused. The page marks the token revoked and keeps it listed.
	revoke := alice.postForm("/tokens/revoke", url.Values{"id": {claudeID}})
	revoke.Body.Close()
	if revoke.StatusCode != http.StatusSeeOther || revoke.Header.Get("Location") != "/tokens" {
		t.Fatalf("revoke = %d to %q, want 303 to /tokens", revoke.StatusCode, revoke.Header.Get("Location"))
	}
	wantJSON(t, api(t, ts, http.MethodPost, "/api/v1/bookmarks", claude, `{"title":"after revoke"}`), http.StatusUnauthorized)
	wantJSON(t, api(t, ts, http.MethodGet, "/api/v1/bookmarks", claude, ""), http.StatusUnauthorized)
	pageBody = body(t, alice.get("/tokens"))
	if !strings.Contains(pageBody, claudeID) || !strings.Contains(pageBody, "Revoked") {
		t.Fatalf("the page does not show the revoked token as revoked; body=%s", pageBody)
	}

	// Revoke all: the read token goes too.
	all := alice.postForm("/tokens/revoke-all", nil)
	all.Body.Close()
	if all.StatusCode != http.StatusSeeOther {
		t.Fatalf("revoke-all = %d, want 303", all.StatusCode)
	}
	wantJSON(t, api(t, ts, http.MethodGet, "/api/v1/bookmarks", reader, ""), http.StatusUnauthorized)
	// Bob's token is bob's: alice's kill switch does not reach it.
	wantJSON(t, api(t, ts, http.MethodGet, "/api/v1/bookmarks", bobsClaude, ""), http.StatusOK)
}

// TestTokenPageNeedsASession: the page and every door on it sit
// behind the session router, so a visitor who is not signed in is sent
// to sign in, and a POST from one mints and revokes nothing.
func TestTokenPageNeedsASession(t *testing.T) {
	ts := newApp(t)
	anon := newClient(t, ts)

	resp, err := anon.c.Get(ts.URL + "/tokens")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(resp.Header.Get("Location"), "/signin") {
		t.Fatalf("anonymous GET /tokens = %d to %q, want 303 to /signin", resp.StatusCode, resp.Header.Get("Location"))
	}
	for _, path := range []string{"/tokens", "/tokens/revoke", "/tokens/revoke-agent", "/tokens/revoke-all"} {
		resp := anon.postForm(path, url.Values{"agent": {"Claude"}, "access": {"write"}, "expiry": {"90"}, "id": {"x"}})
		b := body(t, resp)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("anonymous POST %s = %d, want 403; body=%s", path, resp.StatusCode, b)
		}
		if tokenRE.MatchString(b) {
			t.Fatalf("anonymous POST %s minted a token", path)
		}
	}
}

// syncBuffer is a bytes.Buffer safe to write from the server's
// goroutines and read from the test's: a response arriving is not a
// happens-before the race detector can see, so a plain buffer would
// be a reported race even once the line had landed.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
