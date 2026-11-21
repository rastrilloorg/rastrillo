package handoff_test

import (
	"bytes"
	"context"
	"errors"
	"html"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/assertion"
	"amadan.net/rastrillo/rastrillo/basepath"
	"amadan.net/rastrillo/rastrillo/crypto"
	"amadan.net/rastrillo/rastrillo/handoff"
	"amadan.net/rastrillo/rastrillo/sessions"
)

const (
	refusalTitle   = "We couldn't verify that link"
	refusalText    = "Go back and open your account again."
	refusalLink    = "Go to the main site"
	suspendedTitle = "This account is suspended"
	suspendedText  = "No one can sign in to this account right now. " +
		"If this is your account, contact the person who runs the service."
)

// consumer is an instance whose Keys trust root, the key the tests sign
// assertions with in the root's place.
type consumer struct {
	handler  *handoff.Handoff
	sessions *sessions.Sessions
	root     *crypto.Keypair
	log      *bytes.Buffer
}

func newConsumer(
	t *testing.T, change func(*handoff.Config),
) consumer {
	t.Helper()
	root, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	log := &bytes.Buffer{}
	config := newConfig(t)
	config.Keys = map[string][]byte{"k1": root.SignPub()}
	config.Logger = slog.New(slog.NewTextHandler(log, nil))
	if change != nil {
		change(&config)
	}
	handler, err := handoff.New(config)
	if err != nil {
		t.Fatal(err)
	}
	return consumer{handler, config.Sessions, root, log}
}

// start runs Enter and returns the nonce cookie and the nonce in it.
func (instance consumer) start(
	t *testing.T, next string,
) (*http.Cookie, string) {
	t.Helper()
	target := handoff.EnterPath + "?next=" + url.QueryEscape(next)
	cookies := enter(instance.handler, target).Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("Enter set %d cookies, want 1", len(cookies))
	}
	nonce, _, _ := strings.Cut(cookies[0].Value, ".")
	return cookies[0], nonce
}

func (instance consumer) sign(
	t *testing.T, nonce string, change func(*assertion.Claims),
) string {
	t.Helper()
	id, err := assertion.RandomID()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	claims := assertion.Claims{
		Version: 1, Issuer: rootOrigin, Audience: instanceOrigin,
		Subject: "u1", KeyID: "k1", ID: id, Nonce: nonce,
		IssuedAt: now, ExpiresAt: now + 60, AuthTime: now - 600,
	}
	if change != nil {
		change(&claims)
	}
	token, err := assertion.Sign(instance.root, claims)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func (instance consumer) post(
	token string, cookies ...*http.Cookie,
) *httptest.ResponseRecorder {
	body := url.Values{"assertion": {token}}.Encode()
	request := httptest.NewRequest("POST", instanceOrigin+handoff.EnterPath,
		strings.NewReader(body))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	instance.handler.Consume(recorder, request)
	return recorder
}

func (instance consumer) sessionCookie(
	recorder *httptest.ResponseRecorder,
) (*http.Cookie, bool) {
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == instance.sessions.CookieName() && cookie.MaxAge > 0 {
			return cookie, true
		}
	}
	return nil, false
}

func (instance consumer) sessionFrom(
	t *testing.T, cookie *http.Cookie,
) sessions.Session {
	t.Helper()
	request := httptest.NewRequest("GET", "/", nil)
	request.AddCookie(cookie)
	session, ok := instance.sessions.From(request)
	if !ok {
		t.Fatal("the session cookie resolves to no session")
	}
	return session
}

func (instance consumer) signOut(cookie *http.Cookie) {
	request := httptest.NewRequest("POST", "/signout", nil)
	request.AddCookie(cookie)
	instance.sessions.SignOut(httptest.NewRecorder(), request)
}

func assertSignedIn(
	t *testing.T, instance consumer, recorder *httptest.ResponseRecorder,
) {
	t.Helper()
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; log: %s",
			recorder.Code, instance.log)
	}
	if _, ok := instance.sessionCookie(recorder); !ok {
		t.Fatal("no session cookie")
	}
}

func assertRefused(
	t *testing.T, instance consumer, recorder *httptest.ResponseRecorder,
	title, text string,
) {
	t.Helper()
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	body := recorder.Body.String()
	for _, want := range []string{title, text} {
		if !strings.Contains(body, html.EscapeString(want)) {
			t.Errorf("page lacks %q:\n%s", want, body)
		}
	}
	if _, ok := instance.sessionCookie(recorder); ok {
		t.Error("a refused handoff set a session cookie")
	}
}

func assertNonceCleared(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name != "__Host-carlos-enter" {
			continue
		}
		if cookie.MaxAge >= 0 || cookie.Path != "/" || !cookie.Secure ||
			!cookie.HttpOnly || cookie.SameSite != http.SameSiteNoneMode {
			t.Errorf("nonce cookie not cleared like it was set: %+v",
				cookie)
		}
		return
	}
	t.Error("the nonce cookie was not cleared")
}

func TestConsumeSignsThePersonIn(t *testing.T) {
	instance := newConsumer(t, nil)
	cookie, nonce := instance.start(t, "/events/1")
	authTime := time.Now().Unix() - 900
	token := instance.sign(t, nonce, func(claims *assertion.Claims) {
		claims.AuthTime = authTime
	})
	recorder := instance.post(token, cookie)
	assertSignedIn(t, instance, recorder)
	if got := recorder.Header().Get("Location"); got != "/events/1" {
		t.Errorf("Location = %q, want /events/1", got)
	}
	sessionCookie, _ := instance.sessionCookie(recorder)
	session := instance.sessionFrom(t, sessionCookie)
	if session.Subject != "u1" || session.Method != "handoff" {
		t.Errorf("session = %+v", session)
	}
	if !session.AuthTime.Equal(time.Unix(authTime, 0)) {
		t.Errorf("AuthTime = %v, want %v",
			session.AuthTime, time.Unix(authTime, 0))
	}
	assertNonceCleared(t, recorder)
}

func TestConsumeRefusesAMismatchedNonce(t *testing.T) {
	instance := newConsumer(t, nil)
	cookie, nonce := instance.start(t, "/")
	otherCookie, _ := instance.start(t, "/")
	token := instance.sign(t, nonce, nil)
	recorder := instance.post(token, otherCookie)
	assertRefused(t, instance, recorder, refusalTitle, refusalText)
	// The browser that posted may be mid-handoff of its own.
	for _, set := range recorder.Result().Cookies() {
		if set.Name == "__Host-carlos-enter" {
			t.Errorf("a mismatched nonce changed the cookie: %+v", set)
		}
	}
	// The refusal must not spend the assertion, or a forged POST could
	// burn someone's handoff before their own browser posts it.
	assertSignedIn(t, instance, instance.post(token, cookie))
}

func TestConsumeRefusesWithoutTheNonceCookie(t *testing.T) {
	instance := newConsumer(t, nil)
	_, nonce := instance.start(t, "/")
	recorder := instance.post(instance.sign(t, nonce, nil))
	assertRefused(t, instance, recorder, refusalTitle, refusalText)
}

func TestConsumeRefusalLinksToTheRoot(t *testing.T) {
	instance := newConsumer(t, nil)
	recorder := instance.post("not-an-assertion")
	assertRefused(t, instance, recorder, refusalTitle, refusalText)
	link := `<a href="` + rootOrigin + `">` + refusalLink + `</a>`
	if !strings.Contains(recorder.Body.String(), link) {
		t.Errorf("page lacks %s:\n%s", link, recorder.Body)
	}
}

func TestConsumeRefusesAReplayAfterSignOut(t *testing.T) {
	instance := newConsumer(t, nil)
	cookie, nonce := instance.start(t, "/")
	token := instance.sign(t, nonce, nil)
	first := instance.post(token, cookie)
	assertSignedIn(t, instance, first)
	sessionCookie, _ := instance.sessionCookie(first)
	instance.signOut(sessionCookie)
	again := instance.post(token, cookie)
	assertRefused(t, instance, again, refusalTitle, refusalText)
	if !strings.Contains(instance.log.String(), "replay") {
		t.Errorf("refused for another reason; log: %s", instance.log)
	}
}

func TestConsumeRefusesAWrongAudience(t *testing.T) {
	instance := newConsumer(t, nil)
	cookie, nonce := instance.start(t, "/")
	token := instance.sign(t, nonce, func(claims *assertion.Claims) {
		claims.Audience = "https://other.example.com"
	})
	recorder := instance.post(token, cookie)
	assertRefused(t, instance, recorder, refusalTitle, refusalText)
	if !strings.Contains(instance.log.String(), "assertion: audience") {
		t.Errorf("log lacks the assertion error: %s", instance.log)
	}
}

func TestConsumeRefusesAnExpiredAssertion(t *testing.T) {
	instance := newConsumer(t, nil)
	cookie, nonce := instance.start(t, "/")
	token := instance.sign(t, nonce, func(claims *assertion.Claims) {
		now := time.Now().Unix()
		claims.IssuedAt, claims.ExpiresAt = now-120, now-60
	})
	recorder := instance.post(token, cookie)
	assertRefused(t, instance, recorder, refusalTitle, refusalText)
}

func TestConsumeRefusesASuspendedAccount(t *testing.T) {
	suspended := true
	instance := newConsumer(t, func(config *handoff.Config) {
		config.Suspended = func(context.Context) (bool, error) {
			return suspended, nil
		}
	})
	cookie, nonce := instance.start(t, "/")
	token := instance.sign(t, nonce, nil)
	recorder := instance.post(token, cookie)
	assertRefused(t, instance, recorder, suspendedTitle, suspendedText)
	if strings.Contains(recorder.Body.String(), "<a ") {
		t.Errorf("the suspended page has a link:\n%s", recorder.Body)
	}
	assertNonceCleared(t, recorder)

	suspended = false
	cookie, nonce = instance.start(t, "/")
	assertSignedIn(t, instance,
		instance.post(instance.sign(t, nonce, nil), cookie))
}

func TestConsumeHandsTheRefusalToTheAppsRenderer(t *testing.T) {
	var got handoff.RefusalPage
	instance := newConsumer(t, func(config *handoff.Config) {
		config.RenderRefusal = func(
			response http.ResponseWriter, _ *http.Request,
			page handoff.RefusalPage,
		) {
			got = page
			response.WriteHeader(http.StatusForbidden)
		}
	})
	recorder := instance.post("not-an-assertion")
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
	want := handoff.RefusalPage{
		Title: refusalTitle, Message: refusalText,
		LinkText: refusalLink, LinkURL: rootOrigin,
	}
	if got != want {
		t.Errorf("page = %+v, want %+v", got, want)
	}
}

func TestConsumeIsNotFoundUnderABasePath(t *testing.T) {
	instance := newConsumer(t, nil)
	config := basepath.Config{Slug: "acme", Origin: "https://hub.example.com"}
	served := config.Handler(http.HandlerFunc(instance.handler.Consume))
	request := httptest.NewRequest("POST", "/acme"+handoff.EnterPath,
		strings.NewReader("assertion=x"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Host = "hub.example.com"
	recorder := httptest.NewRecorder()
	served.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}

func TestConsumeAnswersOtherMethodsWith405(t *testing.T) {
	request := httptest.NewRequest(
		"PUT", instanceOrigin+handoff.EnterPath, nil)
	recorder := httptest.NewRecorder()
	newConsumer(t, nil).handler.Consume(recorder, request)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", recorder.Code)
	}
	if got := recorder.Header().Get("Allow"); got != "POST" {
		t.Errorf("Allow = %q", got)
	}
}

func TestConsumeIsNotFoundOnAnotherHost(t *testing.T) {
	instance := newConsumer(t, nil)
	cookie, nonce := instance.start(t, "/")
	token := instance.sign(t, nonce, nil)
	body := url.Values{"assertion": {token}}.Encode()
	request := httptest.NewRequest("POST",
		"https://other.example.com"+handoff.EnterPath,
		strings.NewReader(body))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(cookie)
	recorder := httptest.NewRecorder()
	instance.handler.Consume(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	if len(recorder.Result().Cookies()) != 0 {
		t.Error("another host set or cleared a cookie")
	}
	// Nothing was spent, so the same assertion still works on the origin.
	assertSignedIn(t, instance, instance.post(token, cookie))
}

func TestConsumeSignsInWithTheSubjectSubjectForReturns(t *testing.T) {
	var rootSubjects []string
	instance := newConsumer(t, func(config *handoff.Config) {
		config.SubjectFor = func(
			_ context.Context, rootSubject string,
		) (string, error) {
			rootSubjects = append(rootSubjects, rootSubject)
			return "42", nil
		}
	})
	cookie, nonce := instance.start(t, "/")
	recorder := instance.post(instance.sign(t, nonce, nil), cookie)
	assertSignedIn(t, instance, recorder)
	sessionCookie, _ := instance.sessionCookie(recorder)
	if got := instance.sessionFrom(t, sessionCookie).Subject; got != "42" {
		t.Errorf("session subject = %q, want 42", got)
	}
	if len(rootSubjects) != 1 || rootSubjects[0] != "u1" {
		t.Errorf("SubjectFor got %q, want [u1]", rootSubjects)
	}
}

// A lookup that fails, say because the roster has not delivered the
// member yet, must not cost the person their handoff: the same POST
// works once the lookup does.
func TestConsumeRefusesWhenSubjectForFailsAndLeavesTheAssertion(
	t *testing.T,
) {
	failure := errors.New("member not in the roster yet")
	instance := newConsumer(t, func(config *handoff.Config) {
		config.SubjectFor = func(context.Context, string) (string, error) {
			if failure != nil {
				return "", failure
			}
			return "42", nil
		}
	})
	cookie, nonce := instance.start(t, "/")
	token := instance.sign(t, nonce, nil)
	recorder := instance.post(token, cookie)
	assertRefused(t, instance, recorder, refusalTitle, refusalText)
	if !strings.Contains(instance.log.String(), failure.Error()) {
		t.Errorf("log lacks the SubjectFor error: %s", instance.log)
	}
	for _, set := range recorder.Result().Cookies() {
		if set.Name == "__Host-carlos-enter" {
			t.Errorf("the refusal changed the nonce cookie: %+v", set)
		}
	}

	failure = nil
	retry := instance.post(token, cookie)
	assertSignedIn(t, instance, retry)
	sessionCookie, _ := instance.sessionCookie(retry)
	if got := instance.sessionFrom(t, sessionCookie).Subject; got != "42" {
		t.Errorf("session subject = %q, want 42", got)
	}
	assertNonceCleared(t, retry)
}

func TestConsumeRefusesAnEmptySubjectFromSubjectFor(t *testing.T) {
	instance := newConsumer(t, func(config *handoff.Config) {
		config.SubjectFor = func(context.Context, string) (string, error) {
			return "", nil
		}
	})
	cookie, nonce := instance.start(t, "/")
	recorder := instance.post(instance.sign(t, nonce, nil), cookie)
	assertRefused(t, instance, recorder, refusalTitle, refusalText)
	if !strings.Contains(instance.log.String(), "empty subject") {
		t.Errorf("refused for another reason; log: %s", instance.log)
	}
}

// SubjectFor may touch the app's own tables, so a handoff the checks
// refuse never reaches it.
func TestConsumeCallsSubjectForOnlyAfterTheChecksPass(t *testing.T) {
	called := false
	instance := newConsumer(t, func(config *handoff.Config) {
		config.Suspended = func(context.Context) (bool, error) {
			return true, nil
		}
		config.SubjectFor = func(context.Context, string) (string, error) {
			called = true
			return "42", nil
		}
	})
	cookie, nonce := instance.start(t, "/")
	recorder := instance.post(instance.sign(t, nonce, nil), cookie)
	assertRefused(t, instance, recorder, suspendedTitle, suspendedText)
	if called {
		t.Error("SubjectFor ran for a suspended account")
	}
}

// SubjectFor is app code, so a replayed assertion is refused before it
// runs again.
func TestConsumeRefusesAReplayWithoutCallingSubjectFor(t *testing.T) {
	calls := 0
	instance := newConsumer(t, func(config *handoff.Config) {
		config.SubjectFor = func(context.Context, string) (string, error) {
			calls++
			return "42", nil
		}
	})
	cookie, nonce := instance.start(t, "/")
	token := instance.sign(t, nonce, nil)
	assertSignedIn(t, instance, instance.post(token, cookie))
	again := instance.post(token, cookie)
	assertRefused(t, instance, again, refusalTitle, refusalText)
	if !strings.Contains(instance.log.String(), "replay") {
		t.Errorf("refused for another reason; log: %s", instance.log)
	}
	if calls != 1 {
		t.Errorf("SubjectFor ran %d times, want once", calls)
	}
}
