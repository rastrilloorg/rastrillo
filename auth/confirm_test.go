package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/csrf"
)

// TestVerifyGetConsumesNothing is the whole point of the confirm page:
// a mail-security gateway that fetches the emailed link — repeatedly,
// as they do — must leave it redeemable for the person it was sent to.
func TestVerifyGetConsumesNothing(t *testing.T) {
	a, m := newTestAuth(t, nil)
	beginSignin(t, a, "person@example.com")
	link := linkRE.FindString(m.sentBody())
	if link == "" {
		t.Fatalf("no verify link in mail body:\n%s", m.sentBody())
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
	link := linkRE.FindString(m.sentBody())
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
		">Sign in</button>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("confirm page missing %q:\n%s", want, body)
		}
	}
}

// TestConfirmPageIsNotCachedOrFramed: the page holds a live credential
// in its URL and its markup, and its button is a one-click sign-in.
// no-store keeps the token out of caches; frame-ancestors is login-CSRF
// defence — without it an attacker could frame this page bearing a
// token minted for their own address and coax a click, landing the
// viewer in the attacker's account.
//
// Referrer-Policy is strict-origin, over the app's baseline: the
// baseline sends the full URL, token and all, as the Referer of every
// same-origin request the page makes, so an app layout's CSS, scripts
// and images would copy a still-redeemable link into asset and proxy
// logs. strict-origin sends the origin alone, and still lets the
// button's POST carry a real Origin, where no-referrer would make it
// "null" and fail csrf.SameOrigin in a browser without Sec-Fetch-Site.
// Both renderers, and the screen's PrepareSigninResponse inside one,
// must leave it in place.
func TestConfirmPageIsNotCachedOrFramed(t *testing.T) {
	for _, screen := range []bool{false, true} {
		name := "the default page"
		if screen {
			name = "the sign-in screen"
		}
		t.Run(name, func(t *testing.T) {
			var a *Auth
			a, m := newTestAuth(t, func(c *Config) {
				if screen {
					c.SigninScreen = true
					c.RenderConfirm = func(w http.ResponseWriter, r *http.Request, _ ConfirmPageData) {
						a.PrepareSigninResponse(w, a.SigninState(r))
					}
				}
			})
			beginSignin(t, a, "person@example.com")
			link := linkRE.FindString(m.sentBody())

			w := httptest.NewRecorder()
			// What serve.go's securityHeaders has already written.
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			a.Verify(w, httptest.NewRequest("GET", link, nil))
			want := map[string]string{
				"Cache-Control":   "no-store",
				"Referrer-Policy": "strict-origin",
				"X-Robots-Tag":    "noindex, nofollow",
				"X-Frame-Options": "DENY",
			}
			for h, v := range want {
				if got := w.Header().Values(h); len(got) != 1 || got[0] != v {
					t.Errorf("%s = %q, want exactly %q", h, got, v)
				}
			}
			if strings.Contains(w.Body.String(), "referrer") {
				t.Errorf("the page sets a referrer policy in its markup, which would override the header:\n%s", w.Body.String())
			}
			if !slices.Contains(w.Header().Values("Content-Security-Policy"), "frame-ancestors 'none'") {
				t.Errorf("Content-Security-Policy = %q, want a frame-ancestors 'none' entry",
					w.Header().Values("Content-Security-Policy"))
			}
		})
	}
}

// TestConfirmKeepsTheAppsCSP: serve.go sets the app's real policy on
// every response, so the confirm page must add its one directive rather
// than trade the whole policy for it.
func TestConfirmKeepsTheAppsCSP(t *testing.T) {
	a, m := newTestAuth(t, nil)
	beginSignin(t, a, "person@example.com")
	link := linkRE.FindString(m.sentBody())

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
	link := linkRE.FindString(m.sentBody())

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
	link := linkRE.FindString(m.sentBody())
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

// TestVerifyGetTwiceStillOffersTheLink: a scanner's fetch and then the
// person's own open are two GETs, and the second must draw the same
// working form as the first, not an "expired" page.
func TestVerifyGetTwiceStillOffersTheLink(t *testing.T) {
	a, m := newTestAuth(t, nil)
	beginSignin(t, a, "person@example.com")
	link := linkRE.FindString(m.sentBody())
	u, _ := url.Parse(link)
	token := u.Query().Get("token")

	for i := range 2 {
		w := httptest.NewRecorder()
		a.Verify(w, httptest.NewRequest("GET", link, nil))
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `value="`+token+`"`) {
			t.Fatalf("GET %d: %d, want 200 and a form carrying the token:\n%s", i+1, w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	a.Verify(w, redeemRequest(t, link))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" {
		t.Fatalf("POST after two GETs: %d → %q, want 303 → /", w.Code, w.Header().Get("Location"))
	}
}

// TestRedeemReplayFails: the POST spends the link, so the same form
// posted again — a double click, the back button, or someone who copied
// the page — gets today's expired answer and no session.
func TestRedeemReplayFails(t *testing.T) {
	a, m := newTestAuth(t, nil)
	beginSignin(t, a, "person@example.com")
	link := linkRE.FindString(m.sentBody())

	w := httptest.NewRecorder()
	a.Verify(w, redeemRequest(t, link))
	if w.Header().Get("Location") != "/" {
		t.Fatalf("first POST: %q, want /", w.Header().Get("Location"))
	}
	w = httptest.NewRecorder()
	a.Verify(w, redeemRequest(t, link))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/signin?err=expired" {
		t.Fatalf("replayed POST: %d → %q, want 303 → /signin?err=expired", w.Code, w.Header().Get("Location"))
	}
	if hasSession(w, a) {
		t.Fatal("replayed POST minted a session")
	}

	// The link itself still draws the page — it reveals nothing — and
	// the page's button still gets the same answer.
	w = httptest.NewRecorder()
	a.Verify(w, httptest.NewRequest("GET", link, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET of a spent link: %d, want the same confirm page", w.Code)
	}
}

// TestRedeemChecksOriginWithoutSecFetch: a browser that sends no
// Sec-Fetch-Site is judged on Origin, as csrf.SameOrigin judges every
// other form here. This is the case the page's referrer policy must not
// break.
func TestRedeemChecksOriginWithoutSecFetch(t *testing.T) {
	a, m := newTestAuth(t, nil)
	beginSignin(t, a, "person@example.com")
	link := linkRE.FindString(m.sentBody())

	for _, origin := range []string{"https://evil.example", "null"} {
		r := redeemRequest(t, link)
		r.Header.Del("Sec-Fetch-Site")
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		a.Verify(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("Origin %s: %d, want 403", origin, w.Code)
		}
	}

	r := redeemRequest(t, link)
	r.Header.Del("Sec-Fetch-Site")
	r.Header.Set("Origin", "http://app.test")
	w := httptest.NewRecorder()
	a.Verify(w, r)
	if w.Header().Get("Location") != "/" {
		t.Fatalf("same Origin: %d → %q, want a session; the refusals must not have spent the link", w.Code, w.Header().Get("Location"))
	}
}

// TestRedeemBehindCSRFProtect: the app-wide middleware an app mounts
// lets the confirm page's GET through and judges its POST, so the
// route needs nothing of its own to be protected twice over.
func TestRedeemBehindCSRFProtect(t *testing.T) {
	a, m := newTestAuth(t, nil)
	beginSignin(t, a, "person@example.com")
	link := linkRE.FindString(m.sentBody())
	h := csrf.Protect("http://app.test")(http.HandlerFunc(a.Verify))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", link, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET through Protect: %d, want the confirm page", w.Code)
	}

	cross := redeemRequest(t, link)
	cross.Header.Set("Sec-Fetch-Site", "cross-site")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, cross)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-site POST through Protect: %d, want 403", w.Code)
	}

	w = httptest.NewRecorder()
	h.ServeHTTP(w, redeemRequest(t, link))
	if w.Header().Get("Location") != "/" {
		t.Fatalf("same-origin POST through Protect: %d → %q, want a session", w.Code, w.Header().Get("Location"))
	}
}

// TestAdmissionRunsOnPostOnly: Authorize decides at the POST, where the
// link is spent, and the GET asks it nothing — a page that answered
// differently for a member would be an oracle anyone with a link could
// read, and a scanner's fetch would be a lookup.
func TestAdmissionRunsOnPostOnly(t *testing.T) {
	asked := 0
	a, m := newTestAuth(t, func(c *Config) {
		c.Authorize = func(string) bool { asked++; return false }
	})
	beginSignin(t, a, "stranger@example.com")
	link := linkRE.FindString(m.sentBody())

	w := httptest.NewRecorder()
	a.Verify(w, httptest.NewRequest("GET", link, nil))
	if asked != 0 || w.Code != http.StatusOK {
		t.Fatalf("GET: Authorize asked %d times, status %d; want 0 and the confirm page", asked, w.Code)
	}
	w = httptest.NewRecorder()
	a.Verify(w, redeemRequest(t, link))
	if asked != 1 || w.Code != http.StatusForbidden {
		t.Fatalf("POST: Authorize asked %d times, status %d; want 1 and today's 403", asked, w.Code)
	}
	if hasSession(w, a) {
		t.Fatal("a refused address got a session")
	}
}

// TestRenderConfirmGetsTheScreensConfirmStep: inside RenderConfirm,
// SigninState answers StepConfirm with what the button posts, so an app
// with the shipped screen draws this page with the handler it already
// has. Nothing from the browser's cookies or the proof rides along.
func TestRenderConfirmGetsTheScreensConfirmStep(t *testing.T) {
	var st SigninState
	var a *Auth
	a, m := newTestAuth(t, func(c *Config) {
		c.SigninScreen = true
		c.RenderConfirm = func(w http.ResponseWriter, r *http.Request, _ ConfirmPageData) {
			st = a.SigninState(r)
		}
	})
	beginSignin(t, a, "person@example.com")
	link := linkRE.FindString(m.sentBody())
	u, _ := url.Parse(link)

	w := httptest.NewRecorder()
	a.Verify(w, httptest.NewRequest("GET", link, nil))
	if st.Step != StepConfirm || st.Door() != "confirm" || st.Focus() != "onetap" {
		t.Fatalf("SigninState in RenderConfirm: step %q door %q focus %q, want confirm/confirm/onetap", st.Step, st.Door(), st.Focus())
	}
	if st.Confirm == nil || st.Confirm.Token != u.Query().Get("token") || st.Confirm.Action != "/auth/verify" {
		t.Fatalf("SigninState.Confirm = %+v, want the link's token and path", st.Confirm)
	}
	if st.Proof != nil || st.Address != "" || st.Remembered != nil {
		t.Fatalf("the confirm step carries this browser's state: %+v", st)
	}
	if w.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("RenderConfirm's response lost the framing headers")
	}
}

// TestSigninStateIgnoresATokenInTheQuery: the confirm step comes from
// Verify alone. A sign-in page opened with ?token= — a link anyone can
// craft — is the ordinary page.
func TestSigninStateIgnoresATokenInTheQuery(t *testing.T) {
	a, _ := newTestAuth(t, func(c *Config) { c.SigninScreen = true })
	st := a.SigninState(httptest.NewRequest("GET", "http://app.test/signin?token=abc", nil))
	if st.Step == StepConfirm || st.Confirm != nil {
		t.Fatalf("?token= on the sign-in page drew the confirm step: %+v", st)
	}
}

func hasSession(w *httptest.ResponseRecorder, a *Auth) bool {
	for _, c := range w.Result().Cookies() {
		if c.Name == a.SessionCookie() && c.Value != "" && c.MaxAge >= 0 {
			return true
		}
	}
	return false
}
