package auth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/sessions"
)

// TestRefusedHookRendersTheRefusal: an app that sets Refused answers a
// verified-but-not-admitted address itself — its own sign-in page with
// an error and a way to try a different address — instead of the
// plugin's bare-text 403. The hook gets the verified identity, and no
// session is minted either way.
func TestRefusedHookRendersTheRefusal(t *testing.T) {
	var got Identity
	a, m := newTestAuth(t, func(c *Config) {
		c.Authorize = func(address string) bool { return address == "member@example.com" }
		c.Refused = func(w http.ResponseWriter, r *http.Request, id Identity) {
			got = id
			http.Redirect(w, r, "/signin?err=refused", http.StatusSeeOther)
		}
	})

	beginSignin(t, a, "stranger@example.com")
	link := linkRE.FindString(m.sentBody())
	w := httptest.NewRecorder()
	a.Verify(w, redeemRequest(t, link))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/signin?err=refused" {
		t.Fatalf("refused address with a Refused hook: %d %q, want the app's own answer", w.Code, w.Header().Get("Location"))
	}
	if got.Address != "stranger@example.com" || got.Method == "" {
		t.Fatalf("Refused saw %+v, want the verified identity", got)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == a.SessionCookie() && c.MaxAge >= 0 && c.Value != "" {
			t.Fatal("a refused address was given a session cookie")
		}
	}
}

// TestRefusedNilKeepsTheBareRefusal: without the hook, nothing changes.
func TestRefusedNilKeepsTheBareRefusal(t *testing.T) {
	a, m := newTestAuth(t, func(c *Config) {
		c.Authorize = func(string) bool { return false }
	})
	beginSignin(t, a, "stranger@example.com")
	w := httptest.NewRecorder()
	a.Verify(w, redeemRequest(t, linkRE.FindString(m.sentBody())))
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "not admitted") {
		t.Fatalf("no hook: %d %q, want the plugin's own 403", w.Code, w.Body.String())
	}
}

// noUserSubject is a SubjectFor that answers the way a person store does
// for a person it no longer holds, with the sentinel wrapped.
func noUserSubject(string) (string, error) {
	return "", fmt.Errorf("person store: %w", sessions.ErrNoUser)
}

// verifyGone signs "gone@example.com" in through a fresh Auth and returns
// the answer to the link, and the session cookie it set, if any.
func verifyGone(
	t *testing.T, tweak func(*Config),
) (*httptest.ResponseRecorder, *http.Cookie) {
	t.Helper()
	app, mailer := newTestAuth(t, tweak)
	beginSignin(t, app, "gone@example.com")
	link := linkRE.FindString(mailer.sentBody())
	response := httptest.NewRecorder()
	app.Verify(response, redeemRequest(t, link))
	cookie := sessionCookieSet(app, response)
	if cookie != nil && cookie.Value == "" {
		cookie = nil
	}
	return response, cookie
}

// TestWrappedNoUserReachesRefused: a SubjectFor that wraps ErrNoUser
// refuses through Config.Refused, with the verified address and no cookie.
func TestWrappedNoUserReachesRefused(t *testing.T) {
	var got Identity
	response, cookie := verifyGone(t, func(config *Config) {
		config.SubjectFor = noUserSubject
		config.Refused = func(
			response http.ResponseWriter, _ *http.Request, id Identity,
		) {
			got = id
			http.Error(response, "refused", http.StatusForbidden)
		}
	})
	if response.Code != http.StatusForbidden ||
		got.Address != "gone@example.com" {
		t.Fatalf("%d, Refused saw %+v; want a 403 through Refused",
			response.Code, got)
	}
	if cookie != nil {
		t.Fatalf("a refused person was given a session cookie: %v", cookie)
	}
}

// TestWrappedNoUserGetsTheDefaultRefusal: without Refused it is the
// plugin's 403, not the 500 an ordinary SubjectFor error gets.
func TestWrappedNoUserGetsTheDefaultRefusal(t *testing.T) {
	response, cookie := verifyGone(t, func(config *Config) {
		config.SubjectFor = noUserSubject
	})
	body := response.Body.String()
	if response.Code != http.StatusForbidden ||
		!strings.Contains(body, "not admitted") {
		t.Fatalf("%d %q; want the default 403", response.Code, body)
	}
	if cookie != nil {
		t.Fatalf("a refused person was given a session cookie: %v", cookie)
	}
}
