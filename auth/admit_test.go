package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/lastsignin"
	"amadan.net/rastrillo/rastrillo/sessions"
)

func admitRequest(t *testing.T) *http.Request {
	t.Helper()
	return httptest.NewRequest(http.MethodPost, "/signin/totp", nil)
}

func sessionFromResponse(
	t *testing.T, app *Auth, response *httptest.ResponseRecorder,
) (sessions.Session, bool) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	cookie := sessionCookieSet(app, response)
	if cookie == nil {
		return sessions.Session{}, false
	}
	request.AddCookie(cookie)
	return app.sessions.From(request)
}

func TestAdmitSignsInAndRemembers(t *testing.T) {
	app, _ := newScreenAuth(t, nil)
	response := httptest.NewRecorder()
	app.Admit(response, admitRequest(t),
		Identity{Address: "ada@example.com", Method: "magiclink"}, "")
	if response.Header().Get("Location") != "/" {
		t.Fatalf("landed on %q, want SignedInPath",
			response.Header().Get("Location"))
	}
	remembered := httptest.NewRequest(http.MethodGet, "/signin", nil)
	for _, cookie := range response.Result().Cookies() {
		remembered.AddCookie(cookie)
	}
	record, result := app.RememberJar().Read(remembered)
	want := lastsignin.Record{Method: "magiclink", Address: "ada@example.com"}
	if result != lastsignin.Valid || record != want {
		t.Fatalf("remembered %+v, %v; want %+v", record, result, want)
	}

	response = httptest.NewRecorder()
	app.Admit(response, admitRequest(t),
		Identity{Address: "ada@example.com", Method: "totp"}, "")
	session, ok := sessionFromResponse(t, app, response)
	if !ok || session.Method != "totp" ||
		session.Subject != "ada@example.com" {
		t.Fatalf("session %+v, %v; want a totp session for the address",
			session, ok)
	}
	remembered = httptest.NewRequest(http.MethodGet, "/signin", nil)
	for _, cookie := range response.Result().Cookies() {
		remembered.AddCookie(cookie)
	}
	record, result = app.RememberJar().Read(remembered)
	want = lastsignin.Record{Method: "totp", Address: "ada@example.com"}
	if result != lastsignin.Valid || record != want {
		t.Fatalf("remembered %+v, %v; want %+v", record, result, want)
	}
}

func TestAdmitRefusesAnotherSubject(t *testing.T) {
	app, _ := newScreenAuth(t, func(config *Config) {
		config.SubjectFor = func(string) (string, error) { return "7", nil }
	})
	response := httptest.NewRecorder()
	app.Admit(response, admitRequest(t),
		Identity{Address: "ada@example.com", Method: "totp"}, "8")
	if response.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", response.Code)
	}
	if _, ok := sessionFromResponse(t, app, response); ok {
		t.Fatal("a session was minted for a different subject")
	}
}

func TestAdmitAcceptsTheSameSubject(t *testing.T) {
	app, _ := newScreenAuth(t, func(config *Config) {
		config.SubjectFor = func(string) (string, error) { return "7", nil }
	})
	response := httptest.NewRecorder()
	app.Admit(response, admitRequest(t),
		Identity{Address: "ada@example.com", Method: "totp"}, "7")
	session, ok := sessionFromResponse(t, app, response)
	if !ok || session.Subject != "7" {
		t.Fatalf("session %+v, %v; want subject 7", session, ok)
	}
}

func TestAdmitHonoursAuthorize(t *testing.T) {
	app, _ := newScreenAuth(t, func(config *Config) {
		config.Authorize = func(string) bool { return false }
	})
	response := httptest.NewRecorder()
	app.Admit(response, admitRequest(t),
		Identity{Address: "ada@example.com", Method: "totp"}, "")
	if response.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", response.Code)
	}
	if _, ok := sessionFromResponse(t, app, response); ok {
		t.Fatal("a refused address was given a session")
	}
}

func TestSubjectIsTheAddress(t *testing.T) {
	app, _ := newScreenAuth(t, nil)
	subject, err := app.Subject(context.Background(), "ada@example.com")
	if err != nil || subject != "ada@example.com" {
		t.Fatalf("Subject = %q, %v; want the address", subject, err)
	}
}

func TestSubjectNeedsLookupWithSubjectFor(t *testing.T) {
	app, _ := newScreenAuth(t, func(config *Config) {
		config.SubjectFor = func(string) (string, error) { return "7", nil }
	})
	_, err := app.Subject(context.Background(), "a@example.com")
	if !errors.Is(err, ErrNeedsLookup) {
		t.Fatalf("err = %v, want ErrNeedsLookup", err)
	}
}

func TestSubjectNeedsLookupWithRecords(t *testing.T) {
	app := &Auth{users: &users{}}
	_, err := app.Subject(context.Background(), "a@example.com")
	if !errors.Is(err, ErrNeedsLookup) {
		t.Fatalf("err = %v, want ErrNeedsLookup", err)
	}
}

func TestSubjectNeedsLookupWithBlindLinks(t *testing.T) {
	app := &Auth{cfg: Config{BlindLinks: &BlindLinks{}}}
	_, err := app.Subject(context.Background(), "a@example.com")
	if !errors.Is(err, ErrNeedsLookup) {
		t.Fatalf("err = %v, want ErrNeedsLookup", err)
	}
}

func TestAssemblesRefusesWhatSigninCannotSupport(t *testing.T) {
	app, _ := newScreenAuth(t, nil)
	if err := app.Assembles(); err != nil {
		t.Fatalf("plain Auth: %v", err)
	}
	cases := map[string]*Auth{
		"Records":      {users: &users{}},
		"BlindLinks":   {cfg: Config{BlindLinks: &BlindLinks{}}},
		"SecondFactor": {cfg: Config{SecondFactor: holdNothing}},
	}
	for field, candidate := range cases {
		err := candidate.Assembles()
		if err == nil || !strings.Contains(err.Error(), field) {
			t.Errorf("%s: err = %v, want one naming the field", field, err)
		}
	}
}

func holdNothing(
	http.ResponseWriter, *http.Request, sessions.Session,
) (bool, error) {
	return false, nil
}

func TestAssemblesRefusesSecondFactor(t *testing.T) {
	app, _ := newScreenAuth(t, nil)
	app.cfg.SecondFactor = holdNothing
	err := app.Assembles()
	if err == nil || !strings.Contains(err.Error(), "SecondFactor") {
		t.Fatalf("err = %v, want one naming SecondFactor", err)
	}
}

func TestSigninPathIsTheConfiguredPage(t *testing.T) {
	app, _ := newTestAuth(t, nil)
	if got := app.SigninPath(); got != "/signin" {
		t.Errorf("default SigninPath() = %q, want /signin", got)
	}
	app, _ = newTestAuth(t, func(config *Config) {
		config.SigninPath = "/enter"
	})
	if got := app.SigninPath(); got != "/enter" {
		t.Errorf("SigninPath() = %q, want /enter", got)
	}
}

func TestAuthorizedAsksTheAppsHook(t *testing.T) {
	open, _ := newTestAuth(t, nil)
	if !open.Authorized("ada@example.com") {
		t.Error("with no Authorize hook, Authorized refused an address")
	}
	roster, _ := newTestAuth(t, func(config *Config) {
		config.Authorize = func(address string) bool {
			return address == "ada@example.com"
		}
	})
	if !roster.Authorized("ada@example.com") ||
		roster.Authorized("eve@example.com") {
		t.Error("Authorized did not follow the Authorize hook")
	}
}

// Sessions is the store Admit mints into, so a session auth made is the
// one a caller of Sessions reads back.
func TestSessionsReadsWhatAuthMints(t *testing.T) {
	app, _ := newScreenAuth(t, nil)
	response := httptest.NewRecorder()
	app.Admit(response, admitRequest(t),
		Identity{Address: "ada@example.com", Method: "magiclink"}, "")
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	if cookie := sessionCookieSet(app, response); cookie != nil {
		request.AddCookie(cookie)
	}
	session, ok := app.Sessions().From(request)
	if !ok || session.Subject != "ada@example.com" {
		t.Fatalf("Sessions().From = %+v, %v; want ada's session",
			session, ok)
	}
}

// A redeemed link proves the address at that moment, so the session
// records it, and a check of freshness never has to fall back on when
// the session row was made: a row a refresh remints says nothing about
// when the person last proved who they are.
func TestRedeemingALinkStampsTheGesture(t *testing.T) {
	app, mailer := newTestAuth(t, nil)
	beginSignin(t, app, "person@example.com")
	before := time.Now().Add(-time.Second)
	response := httptest.NewRecorder()
	app.Verify(response, redeemRequest(t, linkRE.FindString(
		mailer.sentBody())))
	session, ok := sessionFromResponse(t, app, response)
	if !ok || session.Method != "magiclink" ||
		session.AuthTime.Before(before) ||
		session.AuthTime.After(time.Now()) {
		t.Fatalf("session %+v, %v; want a magiclink AuthTime of now",
			session, ok)
	}
}

// Keymail's server says when the person last proved themselves; auth
// keeps its answer, and none stays none, so signin can ask for a link.
func TestAdmitKeepsKeymailsAuthTime(t *testing.T) {
	app, _ := newScreenAuth(t, nil)
	earlier := time.Now().Add(-time.Hour).Truncate(time.Second)
	for _, sent := range []time.Time{{}, earlier} {
		response := httptest.NewRecorder()
		app.Admit(response, admitRequest(t), Identity{
			Address: "ada@example.com", Method: "keymail", AuthTime: sent,
		}, "")
		session, ok := sessionFromResponse(t, app, response)
		if !ok || !session.AuthTime.Equal(sent) {
			t.Errorf("sent %v: session %+v, %v", sent, session, ok)
		}
	}
}
