package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	a.Verify(w, httptest.NewRequest("GET", link, nil))
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
	a.Verify(w, httptest.NewRequest("GET", linkRE.FindString(m.sentBody()), nil))
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "not admitted") {
		t.Fatalf("no hook: %d %q, want the plugin's own 403", w.Code, w.Body.String())
	}
}
