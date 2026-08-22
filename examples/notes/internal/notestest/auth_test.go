package notestest

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestSignupSigninSignout drives the full identity lifecycle through
// real HTTP forms: sign up, sign out, sign back in.
func TestSignupSigninSignout(t *testing.T) {
	ts := newApp(t)
	cl := newClient(t, ts)

	resp := cl.signup("alice@example.com", "hunter2222")
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("signup status = %d, want %d; body=%s", resp.StatusCode, http.StatusSeeOther, body(t, resp))
	}
	resp.Body.Close()

	// Signed in: the index loads without a redirect to /signin.
	idx := cl.get("/")
	if idx.StatusCode != http.StatusOK {
		t.Fatalf("GET / after signup = %d, want 200", idx.StatusCode)
	}
	idx.Body.Close()

	resp = cl.postForm("/signout", url.Values{})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("signout status = %d, want %d", resp.StatusCode, http.StatusSeeOther)
	}
	resp.Body.Close()

	// Signed out: the index now redirects to sign-in.
	idx = cl.get("/")
	if idx.StatusCode != http.StatusOK || idx.Request.URL.Path != "/signin" {
		t.Fatalf("GET / after signout landed at %s (status %d), want /signin", idx.Request.URL.Path, idx.StatusCode)
	}
	idx.Body.Close()

	resp = cl.signin("alice@example.com", "hunter2222")
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("signin status = %d, want %d; body=%s", resp.StatusCode, http.StatusSeeOther, body(t, resp))
	}
	resp.Body.Close()

	idx = cl.get("/")
	if idx.StatusCode != http.StatusOK {
		t.Fatalf("GET / after signin = %d, want 200", idx.StatusCode)
	}
	idx.Body.Close()
}

// TestRequireRedirectsAnonymous checks the exact redirect an anonymous
// GET / gets: sessions.Require's return_to shape.
func TestRequireRedirectsAnonymous(t *testing.T) {
	ts := newApp(t)
	cl := newClient(t, ts)

	resp := cl.get("/")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (after following the redirect)", resp.StatusCode)
	}
	if got, want := resp.Request.URL.Path, "/signin"; got != want {
		t.Fatalf("landed at %s, want %s", got, want)
	}
	if got, want := resp.Request.URL.Query().Get("return_to"), "/"; got != want {
		t.Fatalf("return_to = %q, want %q", got, want)
	}
}

// TestReturnToAfterSignin: an anonymous visit to a deep page bounces
// through sign-in and back to exactly where they started.
func TestReturnToAfterSignin(t *testing.T) {
	ts := newApp(t)
	cl := newClient(t, ts)

	resp := cl.get("/notes/new")
	defer resp.Body.Close()
	if got, want := resp.Request.URL.Path, "/signin"; got != want {
		t.Fatalf("landed at %s, want %s", got, want)
	}
	returnTo := resp.Request.URL.Query().Get("return_to")
	if returnTo != "/notes/new" {
		t.Fatalf("return_to = %q, want /notes/new", returnTo)
	}

	// Sign up carrying that return_to along, exactly as the hidden
	// form field in signin.html/signup.html would.
	signup := cl.postForm("/signup", url.Values{
		"email": {"bob@example.com"}, "password": {"hunter2222"}, "return_to": {returnTo},
	})
	defer signup.Body.Close()
	loc := signup.Header.Get("Location")
	if loc != "/notes/new" {
		t.Fatalf("post-signup redirect = %q, want /notes/new", loc)
	}
}

// TestCrossOriginPostRefused: csrf.Protect refuses a mutating request
// whose Origin doesn't match the app's.
func TestCrossOriginPostRefused(t *testing.T) {
	ts := newApp(t)
	cl := newClient(t, ts)
	cl.signup("carol@example.com", "hunter2222").Body.Close()

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/notes", strings.NewReader("title=x&body=y"))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://evil.example")
	resp, err := cl.c.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

// TestSignoutRevokesServerSide: sign-out deletes the session row, not
// just the cookie — replaying the old cookie value must fail even if
// a client held onto it.
func TestSignoutRevokesServerSide(t *testing.T) {
	ts := newApp(t)
	cl := newClient(t, ts)
	cl.signup("dave@example.com", "hunter2222").Body.Close()

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	var sessionCookie *http.Cookie
	for _, c := range cl.c.Jar.Cookies(u) {
		if strings.Contains(c.Name, "session") {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("no session cookie found after signup")
	}

	cl.postForm("/signout", url.Values{}).Body.Close()

	// Replay the pre-signout cookie value on a fresh client — the
	// server-side row is gone, so it must not authenticate.
	replay := newClient(t, ts)
	replay.c.Jar.SetCookies(u, []*http.Cookie{sessionCookie})
	resp := replay.get("/")
	defer resp.Body.Close()
	if got, want := resp.Request.URL.Path, "/signin"; got != want {
		t.Fatalf("replayed cookie landed at %s, want %s (session should be revoked)", got, want)
	}
}
