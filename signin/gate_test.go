package signin

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/action"
	"amadan.net/rastrillo/rastrillo/auth"
	"amadan.net/rastrillo/rastrillo/sessions"
)

// needs is a step that is needed until done is set.
type needs struct{ done bool }

func (step *needs) needed(context.Context, string) (bool, error) {
	return !step.done, nil
}

func gateRig(t *testing.T, steps ...Step) (*rig, *http.Cookie) {
	t.Helper()
	test := newRig(t, func(config *Config) { config.Steps = steps })
	token, err := test.signins.config.Sessions.Mint(sessions.Session{
		Subject: "ana@example.com", Method: "link",
		AuthTime: test.clock.now, At: test.clock.now,
	})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	name := test.signins.config.Sessions.CookieName()
	return test, &http.Cookie{Name: name, Value: token}
}

// reaches serves request through the gate and says whether the app's
// handler ran.
func reaches(test *rig, request *http.Request) (
	*httptest.ResponseRecorder, bool,
) {
	reached := false
	handler := test.signins.Gate(http.HandlerFunc(
		func(response http.ResponseWriter, _ *http.Request) {
			reached = true
		}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response, reached
}

func page(target string, cookie *http.Cookie) *http.Request {
	request := httptest.NewRequest(http.MethodGet, testOrigin+target, nil)
	request.Header.Set("Sec-Fetch-Mode", "navigate")
	request.Header.Set("Accept", "text/html")
	if cookie != nil {
		request.AddCookie(cookie)
	}
	return request
}

func stepsAt(first, second *needs) []Step {
	return []Step{
		{Path: action.Path("/welcome/name"), Needed: first.needed},
		{Path: action.Path("/welcome/company"), Needed: second.needed},
	}
}

func TestGateSendsToTheFirstNeededStep(t *testing.T) {
	first, second := &needs{done: true}, &needs{}
	test, cookie := gateRig(t, stepsAt(first, second)...)
	response, reached := reaches(test, page("/reports?year=2026", cookie))
	if reached || response.Code != http.StatusSeeOther {
		t.Fatalf("got %d, reached %v; want 303", response.Code, reached)
	}
	want := "/welcome/company?return_to=%2Freports%3Fyear%3D2026"
	if got := response.Header().Get("Location"); got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
}

func TestGateLetsThroughWhenNothingIsNeeded(t *testing.T) {
	test, cookie := gateRig(t, stepsAt(&needs{done: true},
		&needs{done: true})...)
	if _, reached := reaches(test, page("/reports", cookie)); !reached {
		t.Fatal("the gate stopped a person with nothing needed")
	}
}

func TestGateIgnoresNonNavigation(t *testing.T) {
	test, cookie := gateRig(t, stepsAt(&needs{}, &needs{})...)
	post := page("/reports", cookie)
	post.Method = http.MethodPost
	cors := page("/reports", cookie)
	cors.Header.Set("Sec-Fetch-Mode", "cors")
	data := page("/reports", cookie)
	data.Header.Del("Sec-Fetch-Mode")
	data.Header.Set("Accept", "application/json")
	asset := page("/assets/app.css", cookie)
	asset.Header.Del("Sec-Fetch-Mode")
	asset.Header.Set("Accept", "text/css")
	for name, request := range map[string]*http.Request{
		"post": post, "cors": cors, "json": data, "asset": asset,
	} {
		response, reached := reaches(test, request)
		if !reached || response.Code != http.StatusOK {
			t.Errorf("%s: reached %v, code %d", name, reached,
				response.Code)
		}
	}
}

func TestGateIgnoresSignedOut(t *testing.T) {
	test, _ := gateRig(t, stepsAt(&needs{}, &needs{})...)
	if _, reached := reaches(test, page("/reports", nil)); !reached {
		t.Fatal("the gate stopped a signed-out visitor")
	}
}

func TestGateNeverLoopsOnAStepPath(t *testing.T) {
	test, cookie := gateRig(t, stepsAt(&needs{}, &needs{})...)
	for _, target := range []string{
		"/welcome/name", "/welcome/company", "/signin", "/signin/code",
		"/signin/faster", "/signin/add/passkey", "/auth/link",
		"/passkey/register/begin", "/signout", "/account/signin",
	} {
		response, reached := reaches(test, page(target, cookie))
		if !reached || response.Code != http.StatusOK {
			t.Errorf("%s: reached %v, code %d", target, reached,
				response.Code)
		}
	}
}

func submit(
	test *rig, cookie *http.Cookie, to string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, testOrigin+"/welcome/x",
		strings.NewReader(url.Values{"return_to": {to}}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	test.signins.Next(response, request)
	return response
}

func TestNextGoesToTheNextStepThenReturnTo(t *testing.T) {
	first, second := &needs{}, &needs{}
	test, cookie := gateRig(t, stepsAt(first, second)...)
	first.done = true
	response := submit(test, cookie, "/reports")
	want := "/welcome/company?return_to=%2Freports"
	if response.Code != http.StatusSeeOther ||
		response.Header().Get("Location") != want {
		t.Fatalf("got %d %q, want 303 %q", response.Code,
			response.Header().Get("Location"), want)
	}
	second.done = true
	response = submit(test, cookie, "/reports")
	if response.Code != http.StatusSeeOther ||
		response.Header().Get("Location") != "/reports" {
		t.Fatalf("got %d %q, want 303 /reports", response.Code,
			response.Header().Get("Location"))
	}
}

func TestReturnToSurvivesTwoStepsAndRefusesOffSite(t *testing.T) {
	first, second := &needs{}, &needs{}
	test, cookie := gateRig(t, stepsAt(first, second)...)
	gated, _ := reaches(test, page("/reports?year=2026", cookie))
	location, err := url.Parse(gated.Header().Get("Location"))
	if err != nil || location.Path != "/welcome/name" {
		t.Fatalf("Location = %q, %v", gated.Header().Get("Location"), err)
	}
	carried := location.Query().Get("return_to")
	first.done = true
	moved := submit(test, cookie, carried).Header().Get("Location")
	next, err := url.Parse(moved)
	if err != nil || next.Path != "/welcome/company" ||
		next.Query().Get("return_to") != carried {
		t.Fatalf("second step lost return_to: %v, %v", next, err)
	}
	second.done = true
	done := submit(test, cookie, carried).Header().Get("Location")
	if done != "/reports?year=2026" {
		t.Fatalf("finished at %q, want /reports?year=2026", done)
	}
	for _, evil := range []string{
		"https://evil.example/", "//evil.example/", `/\evil.example`, "",
	} {
		got := submit(test, cookie, evil).Header().Get("Location")
		if got != "/" {
			t.Errorf("return_to %q went to %q, want /", evil, got)
		}
	}
}

func TestNeededErrorIs500AndLogged(t *testing.T) {
	broken := Step{Path: "/welcome/name",
		Needed: func(context.Context, string) (bool, error) {
			return false, errors.New("database is gone")
		}}
	var logged bytes.Buffer
	test := newRig(t, func(config *Config) {
		config.Steps = []Step{broken}
		config.Logger = slog.New(slog.NewTextHandler(&logged, nil))
	})
	token, err := test.signins.config.Sessions.Mint(sessions.Session{
		Subject: "ana@example.com", Method: "link",
		AuthTime: test.clock.now, At: test.clock.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	cookie := &http.Cookie{
		Name: test.signins.config.Sessions.CookieName(), Value: token}
	response, reached := reaches(test, page("/reports", cookie))
	if reached || response.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, reached %v; want 500", response.Code, reached)
	}
	if !strings.Contains(logged.String(), "level=ERROR") ||
		!strings.Contains(logged.String(), "database is gone") {
		t.Fatalf("not logged at error level: %q", logged.String())
	}
	if strings.Contains(response.Body.String(), "database is gone") {
		t.Fatal("the error text reached the page")
	}
}

// authSession is a link session minted by auth's own store, the one New
// falls back to when Config.Sessions is nil.
func (test *rig) authSession() *http.Cookie {
	test.t.Helper()
	store := test.auth.Sessions()
	token, err := store.Mint(sessions.Session{Subject: ana,
		Method: "magiclink", AuthTime: test.clock.now})
	if err != nil {
		test.t.Fatalf("Mint: %v", err)
	}
	return &http.Cookie{Name: store.CookieName(), Value: token}
}

// An app with sign-up steps and no added ways may leave Sessions unset;
// the gate must still hold a signed-in person at the first step.
func TestGateWithNoWaysUsesAuthsSessions(t *testing.T) {
	test := newRig(t, func(config *Config) {
		config.Ways, config.Sessions = 0, nil
		config.Steps = stepsAt(&needs{}, &needs{})
	})
	cookie := test.authSession()
	response, reached := reaches(test, page("/reports", cookie))
	want := "/welcome/name?return_to=%2Freports"
	if reached || response.Header().Get("Location") != want {
		t.Fatalf("gate: reached %v, Location %q; want %q", reached,
			response.Header().Get("Location"), want)
	}
	if got := submit(test, cookie, "/reports").Header().
		Get("Location"); got != want {
		t.Fatalf("Next went to %q, want %q", got, want)
	}
}

// With the sign-in page moved, a person the offer is waiting for must
// still reach it to sign in again, or adding a way bounces them between
// the two pages.
func TestGateLeavesAMovedSigninPageOpen(t *testing.T) {
	database := schema.Open(t)
	authority := newAuth(t, database, func(config *auth.Config) {
		config.SigninPath = "/enter"
	})
	moment := &clock{now: time.Now()}
	signins, err := New(baseConfig(t, database, authority, moment))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	test := &rig{t: t, database: database, auth: authority,
		signins: signins, clock: moment}
	cookie := test.authSession()
	for target, open := range map[string]bool{
		"/enter?reauth=1": true, "/enter/code": true, "/enterprise": false,
	} {
		_, reached := reaches(test, page(target, cookie))
		if reached != open {
			t.Errorf("%s: reached %v, want %v", target, reached, open)
		}
	}
}
