package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
)

// TestVerifyGetConsumesNothing is the whole point of the confirm page:
// a mail-security gateway that fetches the emailed link — repeatedly,
// as they do — must leave it redeemable for the person it was sent to.
func TestVerifyGetConsumesNothing(t *testing.T) {
	a, m := newTestAuth(t, nil)
	beginSignin(t, a, "person@example.com")
	link := linkRE.FindString(m.body)
	if link == "" {
		t.Fatalf("no verify link in mail body:\n%s", m.body)
	}

	// Three scanner fetches, the way a forwarded message gets scanned
	// more than once.
	for i := range 3 {
		w := httptest.NewRecorder()
		a.Verify(w, httptest.NewRequest("GET", link, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("scanner GET %d: %d, want 200 and no redemption", i, w.Code)
		}
		for _, c := range w.Result().Cookies() {
			if c.Name == a.SessionCookie() && c.Value != "" {
				t.Fatalf("scanner GET %d minted a session cookie", i)
			}
		}
	}

	// The person's click still works.
	w := httptest.NewRecorder()
	a.Verify(w, redeemRequest(t, link))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" {
		t.Fatalf("confirm POST: %d → %q, want 303 → /", w.Code, w.Header().Get("Location"))
	}
	var signed bool
	for _, c := range w.Result().Cookies() {
		if c.Name == a.SessionCookie() && c.Value != "" {
			signed = true
		}
	}
	if !signed {
		t.Fatal("confirm POST set no session cookie")
	}
}

// TestConfirmPageCarriesToken pins the contract between the two halves
// of Verify: the page must post the token back, or the POST has
// nothing to redeem.
func TestConfirmPageCarriesToken(t *testing.T) {
	a, m := newTestAuth(t, nil)
	beginSignin(t, a, "person@example.com")
	link := linkRE.FindString(m.body)
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse link: %v", err)
	}
	token := u.Query().Get("token")

	w := httptest.NewRecorder()
	a.Verify(w, httptest.NewRequest("GET", link, nil))
	body := w.Body.String()
	for _, want := range []string{
		`method="post"`,
		`action="/auth/verify"`,
		`name="token"`,
		`value="` + token + `"`,
		"app.test",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("confirm page missing %q:\n%s", want, body)
		}
	}
}

// TestConfirmPageIsNotCachedOrFramed: the page holds a live credential
// in its markup, and its button is a one-click sign-in. no-store and
// no-referrer keep the token from travelling; frame-ancestors is
// login-CSRF defence — without it an attacker could frame this page
// bearing a token minted for their own address and coax a click,
// landing the viewer in the attacker's account.
func TestConfirmPageIsNotCachedOrFramed(t *testing.T) {
	a, m := newTestAuth(t, nil)
	beginSignin(t, a, "person@example.com")
	link := linkRE.FindString(m.body)

	w := httptest.NewRecorder()
	a.Verify(w, httptest.NewRequest("GET", link, nil))
	want := map[string]string{
		"Cache-Control":   "no-store",
		"Referrer-Policy": "no-referrer",
		"X-Robots-Tag":    "noindex, nofollow",
		"X-Frame-Options": "DENY",
	}
	for h, v := range want {
		if got := w.Header().Get(h); got != v {
			t.Errorf("%s = %q, want %q", h, got, v)
		}
	}
	if !slices.Contains(w.Header().Values("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Errorf("Content-Security-Policy = %q, want a frame-ancestors 'none' entry",
			w.Header().Values("Content-Security-Policy"))
	}
}

// TestConfirmKeepsTheAppsCSP: serve.go sets the app's real policy on
// every response, so the confirm page must add its one directive rather
// than trade the whole policy for it.
func TestConfirmKeepsTheAppsCSP(t *testing.T) {
	a, m := newTestAuth(t, nil)
	beginSignin(t, a, "person@example.com")
	link := linkRE.FindString(m.body)

	w := httptest.NewRecorder()
	w.Header().Set("Content-Security-Policy", "default-src 'self'")
	a.Verify(w, httptest.NewRequest("GET", link, nil))
	got := w.Header().Values("Content-Security-Policy")
	if !slices.Contains(got, "default-src 'self'") || !slices.Contains(got, "frame-ancestors 'none'") {
		t.Fatalf("Content-Security-Policy = %q, want the app's policy kept and frame-ancestors added", got)
	}
}

// TestConfirmPageEscapesToken: the token is untrusted query input
// echoed into markup, so anyone can aim it at the page.
func TestConfirmPageEscapesToken(t *testing.T) {
	a, _ := newTestAuth(t, nil)
	w := httptest.NewRecorder()
	a.Verify(w, httptest.NewRequest("GET",
		`http://app.test/auth/verify?token="><script>alert(1)</script>`, nil))
	if body := w.Body.String(); strings.Contains(body, "<script>alert(1)") {
		t.Fatalf("token echoed unescaped:\n%s", body)
	}
}

// TestVerifyGetWithoutToken: a bare visit to the landing path is not a
// sign-in attempt at all, so it goes back to the sign-in page rather
// than drawing a form with nothing in it.
func TestVerifyGetWithoutToken(t *testing.T) {
	a, _ := newTestAuth(t, nil)
	w := httptest.NewRecorder()
	a.Verify(w, httptest.NewRequest("GET", "http://app.test/auth/verify", nil))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/signin?err=expired" {
		t.Fatalf("tokenless GET: %d → %q, want 303 → /signin?err=expired", w.Code, w.Header().Get("Location"))
	}
}

// TestRedeemRefusesCrossOrigin: the token is a bearer credential and
// this is a browser-form endpoint, so a post with no browser evidence
// of same-origin submission — a scanner that decided to submit forms,
// or a cross-site page — is refused, and crucially does not spend the
// link on the way to being refused.
func TestRedeemRefusesCrossOrigin(t *testing.T) {
	a, m := newTestAuth(t, nil)
	beginSignin(t, a, "person@example.com")
	link := linkRE.FindString(m.body)

	bare := redeemRequest(t, link)
	bare.Header.Del("Sec-Fetch-Site")
	w := httptest.NewRecorder()
	a.Verify(w, bare)
	if w.Code != http.StatusForbidden {
		t.Fatalf("headerless POST: %d, want 403", w.Code)
	}

	cross := redeemRequest(t, link)
	cross.Header.Set("Sec-Fetch-Site", "cross-site")
	w = httptest.NewRecorder()
	a.Verify(w, cross)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-site POST: %d, want 403", w.Code)
	}

	// Refused twice, and the link is still the person's to use.
	w = httptest.NewRecorder()
	a.Verify(w, redeemRequest(t, link))
	if w.Header().Get("Location") != "/" {
		t.Fatalf("after refusals: %q, want the link still redeemable", w.Header().Get("Location"))
	}
}

// TestRedeemSpentToken: one error for unknown, used and expired alike.
func TestRedeemSpentToken(t *testing.T) {
	a, _ := newTestAuth(t, nil)
	w := httptest.NewRecorder()
	a.Verify(w, redeemRequest(t, "http://app.test/auth/verify?token=nosuchtoken"))
	if w.Header().Get("Location") != "/signin?err=expired" {
		t.Fatalf("unknown token: %q, want /signin?err=expired", w.Header().Get("Location"))
	}
}

// TestVerifyRejectsOtherMethods keeps the route honest for an app that
// mounts Verify method-agnostically.
func TestVerifyRejectsOtherMethods(t *testing.T) {
	a, _ := newTestAuth(t, nil)
	w := httptest.NewRecorder()
	a.Verify(w, httptest.NewRequest("PUT", "http://app.test/auth/verify?token=x", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT: %d, want 405", w.Code)
	}
	if got := w.Header().Get("Allow"); got != "GET, HEAD, POST" {
		t.Errorf("Allow = %q", got)
	}
}

// TestRenderConfirmOverride: an app can draw the page in its own
// layout, and gets everything the form needs.
func TestRenderConfirmOverride(t *testing.T) {
	var saw ConfirmPageData
	a, m := newTestAuth(t, func(c *Config) {
		c.RenderConfirm = func(w http.ResponseWriter, r *http.Request, d ConfirmPageData) {
			saw = d
			w.WriteHeader(http.StatusTeapot)
		}
	})
	beginSignin(t, a, "person@example.com")
	link := linkRE.FindString(m.body)
	u, _ := url.Parse(link)

	w := httptest.NewRecorder()
	a.Verify(w, httptest.NewRequest("GET", link, nil))
	if w.Code != http.StatusTeapot {
		t.Fatalf("RenderConfirm did not own the response: %d", w.Code)
	}
	if saw.Token != u.Query().Get("token") {
		t.Errorf("Token = %q, want the emailed token", saw.Token)
	}
	if saw.Action != "/auth/verify" {
		t.Errorf("Action = %q, want /auth/verify", saw.Action)
	}
	if saw.Host != "app.test" {
		t.Errorf("Host = %q, want app.test", saw.Host)
	}
}

// TestConfirmActionFollowsMount: the form posts back to wherever the
// GET landed, so an app that mounted Verify off the default path is
// not sent to a route that does not exist.
func TestConfirmActionFollowsMount(t *testing.T) {
	a, _ := newTestAuth(t, nil)
	w := httptest.NewRecorder()
	a.Verify(w, httptest.NewRequest("GET", "http://app.test/enter?token=abc", nil))
	if !strings.Contains(w.Body.String(), `action="/enter"`) {
		t.Fatalf("form does not post back to the mounted path:\n%s", w.Body.String())
	}
}

// TestHostStripsScheme covers the https origin too, since the confirm
// page names the host and http is only ever a dev origin.
func TestHostStripsScheme(t *testing.T) {
	for origin, want := range map[string]string{
		"https://app.example.com": "app.example.com",
		"http://app.test":         "app.test",
	} {
		a, _ := newTestAuth(t, func(c *Config) { c.Origin = origin })
		if got := a.host(); got != want {
			t.Errorf("host(%q) = %q, want %q", origin, got, want)
		}
	}
}
