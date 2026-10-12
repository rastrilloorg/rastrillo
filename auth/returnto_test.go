package auth

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/sessions"
)

// hiddenReturnTo finds the return_to a rendered sign-in form posts.
var hiddenReturnTo = regexp.MustCompile(`<input type="hidden" name="return_to" value="([^"]*)">`)

// signInFromPage is the whole trip a refused visitor makes: the sign-in
// page at target, its form posted with whatever return_to the rendered
// page carries, and the emailed link redeemed in the same browser.
func signInFromPage(t *testing.T, a *Auth, m *captureMailer, b *browser, target string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"address": {"person@example.com"}, "force": {"1"}}
	if f := hiddenReturnTo.FindStringSubmatch(renderSignin(t, stateAt(a, b, target))); f != nil {
		form.Set("return_to", html.UnescapeString(f[1]))
	}
	if w := b.do(a.Begin, http.MethodPost, "/signin", form); w.Code != http.StatusSeeOther {
		t.Fatalf("Begin: %d, want 303", w.Code)
	}
	link := linkRE.FindString(m.sentBody())
	if link == "" {
		t.Fatalf("no verify link in mail body:\n%s", m.sentBody())
	}
	return b.redeem(a, link)
}

// TestSignInLandsOnReturnTo is the bug report: a guarded page sends the
// visitor to sign in, and the emailed link, opened minutes later as a
// request carrying nothing of the sign-in page, lands them back on that
// page rather than SignedInPath. Driven through the rendered page, so a
// partial that stops posting return_to fails here.
func TestSignInLandsOnReturnTo(t *testing.T) {
	for _, screen := range []bool{false, true} {
		a, m := newTestAuth(t, func(c *Config) { c.SigninScreen = screen })
		b := newBrowser()
		refused := httptest.NewRecorder()
		a.RequireSession(http.NotFoundHandler()).ServeHTTP(refused, b.request(http.MethodGet, "/t/accounts?tab=2", nil))

		w := signInFromPage(t, a, m, b, refused.Header().Get("Location"))
		if got := w.Header().Get("Location"); w.Code != http.StatusSeeOther || got != "/t/accounts?tab=2" {
			t.Fatalf("screen=%v: redeem → %d %q, want 303 → /t/accounts?tab=2", screen, w.Code, got)
		}
		if c := returnCookieSet(w); c == nil || c.MaxAge >= 0 {
			t.Fatalf("screen=%v: redemption did not delete the return_to cookie: %+v", screen, c)
		}
	}
}

// TestSignInWithoutReturnToLandsOnSignedInPath is the control.
func TestSignInWithoutReturnToLandsOnSignedInPath(t *testing.T) {
	a, m := newScreenAuth(t, nil)
	if w := signInFromPage(t, a, m, newBrowser(), "/signin"); w.Header().Get("Location") != "/" {
		t.Fatalf("redeem → %q, want SignedInPath /", w.Header().Get("Location"))
	}
}

// Go drops ';' and '"' from a raw cookie value, so an unescaped cookie
// would land "/reports;v=2" on "/reportsv=2".
func TestReturnToSurvivesBytesACookieCannotHold(t *testing.T) {
	a, m := newScreenAuth(t, nil)
	w := signInFromPage(t, a, m, newBrowser(), "/signin?return_to="+url.QueryEscape(`/reports;v=2&q="x"`))
	if got := w.Header().Get("Location"); got != `/reports;v=2&q="x"` {
		t.Fatalf("redeem → %q", got)
	}
}

// An abandoned sign-in's destination must not catch the next one: the
// second post names none, so it forgets the first's.
func TestAPostWithoutReturnToForgetsTheLastOne(t *testing.T) {
	a, m := newScreenAuth(t, nil)
	b := newBrowser()
	b.do(a.Begin, http.MethodPost, "/signin", url.Values{"address": {"person@example.com"}, "force": {"1"}, "return_to": {"/accounts"}})
	w := signInFromPage(t, a, m, b, "/signin")
	if got := w.Header().Get("Location"); got != "/" {
		t.Fatalf("a plain sign-in after an abandoned one → %q, want /", got)
	}
}

func TestSignoutForgetsTheDestination(t *testing.T) {
	a, _ := newScreenAuth(t, nil)
	b := newBrowser()
	b.do(a.Begin, http.MethodPost, "/signin", url.Values{"address": {"person@example.com"}, "force": {"1"}, "return_to": {"/accounts"}})
	if c := returnCookieSet(b.do(a.Signout, http.MethodPost, "/signout", url.Values{})); c == nil || c.MaxAge >= 0 {
		t.Fatalf("Signout left the destination: %+v", c)
	}
}

// A link that has expired sends the visitor back to sign in; the page
// must post the destination again, or keepReturnTo forgets it.
func TestAFailedCompletionKeepsTheDestination(t *testing.T) {
	a, _ := newScreenAuth(t, nil)
	b := newBrowser()
	b.do(a.Begin, http.MethodPost, "/signin", url.Values{"address": {"person@example.com"}, "force": {"1"}, "return_to": {"/t/accounts"}})
	w := b.do(a.Verify, http.MethodPost, "/auth/verify", url.Values{"token": {"not-a-token"}})
	loc := w.Header().Get("Location")
	if loc != "/signin?err=expired&return_to=%2Ft%2Faccounts" {
		t.Fatalf("a dead link → %q", loc)
	}
	if st := stateAt(a, b, loc); st.Problem != ProblemExpired || st.ReturnTo != "/t/accounts" {
		t.Fatalf("the page after a dead link: problem %q, ReturnTo %q", st.Problem, st.ReturnTo)
	}
}

func TestForgetKeepsTheDestination(t *testing.T) {
	a, _ := newScreenAuth(t, nil)
	w := newBrowser().do(a.Forget, http.MethodPost, "/signin/forget", url.Values{"return_to": {"/t/accounts"}})
	if got := w.Header().Get("Location"); got != "/signin?return_to=%2Ft%2Faccounts" {
		t.Fatalf("Forget → %q", got)
	}
}

// SecondFactor reads return_to the ordinary way, through the request's
// form; admit puts the kept one there before the hook runs.
func TestSecondFactorSeesReturnTo(t *testing.T) {
	var saw string
	a, m := newScreenAuth(t, func(c *Config) {
		c.SecondFactor = func(w http.ResponseWriter, r *http.Request, _ sessions.Session) (bool, error) {
			saw = sessions.SafeReturn(r, "fallback")
			http.Redirect(w, r, "/passkey/confirm", http.StatusSeeOther)
			return true, nil
		}
	})
	signInFromPage(t, a, m, newBrowser(), "/signin?return_to=%2Ft%2Faccounts")
	if saw != "/t/accounts" {
		t.Fatalf("SecondFactor saw return_to = %q, want /t/accounts", saw)
	}
}

func TestSigninStateReturnTo(t *testing.T) {
	a, _ := newScreenAuth(t, nil)
	for target, want := range map[string]string{
		"/signin?return_to=%2Ft%2Faccounts":      "/t/accounts",
		"/signin?return_to=%2F%2Fevil.example":   "",
		"/signin?return_to=https%3A%2F%2Fevil.x": "",
		"/signin":                                "",
	} {
		if got := stateAt(a, newBrowser(), target).ReturnTo; got != want {
			t.Errorf("%s: ReturnTo %q, want %q", target, got, want)
		}
	}
}

// The finish request's body is JSON, so the destination rides the URL.
func TestThePasskeyDoorFinishesAtReturnTo(t *testing.T) {
	door := &PasskeyDoor{BeginPath: "/pk/begin", FinishPath: "/pk/finish", ModuleURL: "/m.mjs", ScriptURL: "/pk.js"}
	for _, c := range []struct {
		st   SigninState
		want string
	}{
		{SigninState{}, ""},
		{SigninState{Passkey: door}, "/pk/finish"},
		{SigninState{Passkey: door, ReturnTo: "/t/a b"}, "/pk/finish?return_to=%2Ft%2Fa+b"},
		{SigninState{Passkey: &PasskeyDoor{FinishPath: "/pk/finish?v=1"}, ReturnTo: "/t"}, "/pk/finish?return_to=%2Ft&v=1"},
		{SigninState{Passkey: &PasskeyDoor{FinishPath: "/pk/finish?return_to=%2Fhome"}, ReturnTo: "/t"}, "/pk/finish?return_to=%2Ft"},
	} {
		if got := c.st.PasskeyFinish(); got != c.want {
			t.Errorf("PasskeyFinish(%+v) = %q, want %q", c.st, got, c.want)
		}
	}
	page := renderSignin(t, SigninState{Step: StepAsk, Passkey: door, ReturnTo: "/t/accounts"})
	if !strings.Contains(page, `data-rst-passkey-finish="/pk/finish?return_to=%2Ft%2Faccounts"`) {
		t.Fatalf("the passkey door does not finish at return_to:\n%s", page)
	}
}

// Every form on a page with a destination posts it: the three Begin
// doors and the way out of a remembered address.
func TestEveryFormPostsReturnTo(t *testing.T) {
	ada := &Remembered{"magiclink", "ada@example.com"}
	kay := &Remembered{"keymail", "kay@example.org"}
	for name, c := range map[string]struct {
		st    SigninState
		forms int
	}{
		"ask":     {SigninState{Step: StepAsk}, 1},
		"link":    {SigninState{Step: StepReturning, Remembered: ada}, 2},
		"keymail": {SigninState{Step: StepReturning, Remembered: kay}, 2},
		"sent":    {SigninState{Step: StepSent}, 1},
	} {
		c.st.ReturnTo = "/t/accounts"
		page := renderSignin(t, c.st)
		if got := len(hiddenReturnTo.FindAllString(page, -1)); got != c.forms || got != strings.Count(page, "<form") {
			t.Errorf("%s: %d return_to fields for %d forms, want %d:\n%s", name, got, strings.Count(page, "<form"), c.forms, page)
		}
		c.st.ReturnTo = ""
		if page := renderSignin(t, c.st); hiddenReturnTo.MatchString(page) {
			t.Errorf("%s: a return_to field with no destination:\n%s", name, page)
		}
	}
}

// "Use a different email" on the Sent page is a second attempt for the
// same page. Every step posts only what the rendered page carries, so a
// redirect that drops return_to anywhere in the loop fails here.
func TestUseADifferentEmailKeepsTheDestination(t *testing.T) {
	a, m := newScreenAuth(t, nil)
	b := newBrowser()
	post := func(h http.HandlerFunc, path, target string, form url.Values) string {
		t.Helper()
		if f := hiddenReturnTo.FindStringSubmatch(renderSignin(t, stateAt(a, b, target))); f != nil {
			form.Set("return_to", html.UnescapeString(f[1]))
		}
		return b.do(h, http.MethodPost, path, form).Header().Get("Location")
	}
	sent := post(a.Begin, "/signin", "/signin?return_to=%2Faccounts", url.Values{"address": {"first@example.com"}, "force": {"1"}})
	back := post(a.Forget, "/signin/forget", sent, url.Values{})
	m.forget()
	if got := signInFromPage(t, a, m, b, back).Header().Get("Location"); got != "/accounts" {
		t.Fatalf("sent %q → different email %q → redeem %q, want /accounts", sent, back, got)
	}
}
