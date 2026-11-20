package handoff_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/basepath"
	"amadan.net/rastrillo/rastrillo/dbtest"
	"amadan.net/rastrillo/rastrillo/handoff"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/sessions"
)

const (
	rootOrigin     = "https://root.example.com"
	instanceOrigin = "https://acme.example.com"
)

var schema = dbtest.FromSet(
	migrate.Merge(sessions.Schema, handoff.Schema))

func TestMain(tests *testing.M) {
	code := tests.Run()
	schema.Remove()
	os.Exit(code)
}

func rootKeys(t *testing.T) map[string][]byte {
	t.Helper()
	key, err := sessions.NewSigningKey("k1")
	if err != nil {
		t.Fatal(err)
	}
	keys, err := sessions.ParsePublicKeys(key.Public())
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

func rowSessions(t *testing.T, db *sql.DB) *sessions.Sessions {
	t.Helper()
	store, err := sessions.New(
		sessions.Config{DB: db, Origin: instanceOrigin})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func newConfig(t *testing.T) handoff.Config {
	t.Helper()
	db := schema.Open(t)
	return handoff.Config{
		Root:     rootOrigin,
		Keys:     rootKeys(t),
		Origin:   instanceOrigin,
		Sessions: rowSessions(t, db),
		DB:       db,
	}
}

func newHandoff(t *testing.T) *handoff.Handoff {
	t.Helper()
	handler, err := handoff.New(newConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func enter(
	handler *handoff.Handoff, target string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest("GET", instanceOrigin+target, nil)
	recorder := httptest.NewRecorder()
	handler.Enter(recorder, request)
	return recorder
}

func TestNewRefusesABadConfig(t *testing.T) {
	signedKey, err := sessions.NewSigningKey("s1")
	if err != nil {
		t.Fatal(err)
	}
	signedSessions, err := sessions.New(sessions.Config{
		Origin: instanceOrigin,
		Signed: &sessions.Signed{
			Name: "ppl", Key: signedKey,
			Epoch: func(context.Context, string) (int64, error) {
				return 1, nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(*handoff.Config){
		"no Root":     func(config *handoff.Config) { config.Root = "" },
		"no Keys":     func(config *handoff.Config) { config.Keys = nil },
		"no Origin":   func(config *handoff.Config) { config.Origin = "" },
		"no Sessions": func(config *handoff.Config) { config.Sessions = nil },
		"no DB":       func(config *handoff.Config) { config.DB = nil },
		"Root equals Origin": func(config *handoff.Config) {
			config.Root = config.Origin
		},
		"a Root with a path": func(config *handoff.Config) {
			config.Root = rootOrigin + "/app"
		},
		"signed Sessions": func(config *handoff.Config) {
			config.Sessions = signedSessions
		},
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			config := newConfig(t)
			change(&config)
			if _, err := handoff.New(config); err == nil {
				t.Fatal("New accepted the config")
			}
		})
	}
}

func TestEnterSetsANonceCookieTheRootsPostWillCarry(t *testing.T) {
	recorder := enter(newHandoff(t), handoff.EnterPath)
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != "__Host-carlos-enter" {
		t.Errorf("name = %q", cookie.Name)
	}
	if cookie.SameSite != http.SameSiteNoneMode {
		t.Errorf("SameSite = %v, want None", cookie.SameSite)
	}
	if !cookie.Secure || !cookie.HttpOnly {
		t.Errorf("Secure=%v HttpOnly=%v, want both",
			cookie.Secure, cookie.HttpOnly)
	}
	if cookie.Path != "/" {
		t.Errorf("Path = %q", cookie.Path)
	}
	if cookie.MaxAge != 300 {
		t.Errorf("MaxAge = %d, want 300", cookie.MaxAge)
	}
}

func TestEnterRedirectsToTheRootsAssert(t *testing.T) {
	recorder := enter(newHandoff(t), handoff.EnterPath+"?next=/events/1")
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", recorder.Code)
	}
	target, err := url.Parse(recorder.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if target.Scheme+"://"+target.Host != rootOrigin {
		t.Errorf("redirect goes to %s://%s", target.Scheme, target.Host)
	}
	if target.Path != "/assert" {
		t.Errorf("path = %q, want /assert", target.Path)
	}
	query := target.Query()
	if query.Get("aud") != instanceOrigin {
		t.Errorf("aud = %q", query.Get("aud"))
	}
	if query.Get("next") != "/events/1" {
		t.Errorf("next = %q", query.Get("next"))
	}
	nonce := query.Get("nonce")
	cookie := recorder.Result().Cookies()[0]
	if nonce == "" || !strings.HasPrefix(cookie.Value, nonce+".") {
		t.Errorf("nonce %q is not the cookie's %q", nonce, cookie.Value)
	}
}

func TestEnterKeepsOnlyALocalNext(t *testing.T) {
	tests := map[string]string{
		"//evil.example":        "/",
		"https://evil.example/": "/",
		`/\evil`:                "/",
		"/events/1?x=2":         "/events/1?x=2",
	}
	for next, want := range tests {
		recorder := enter(newHandoff(t),
			handoff.EnterPath+"?next="+url.QueryEscape(next))
		target, err := url.Parse(recorder.Header().Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		if got := target.Query().Get("next"); got != want {
			t.Errorf("next %q became %q, want %q", next, got, want)
		}
	}
}

func TestEnterIsNotFoundUnderABasePath(t *testing.T) {
	handler := newHandoff(t)
	config := basepath.Config{Slug: "acme", Origin: "https://hub.example.com"}
	served := config.Handler(http.HandlerFunc(handler.Enter))
	request := httptest.NewRequest("GET", "/acme"+handoff.EnterPath, nil)
	request.Host = "hub.example.com"
	recorder := httptest.NewRecorder()
	served.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	if len(recorder.Result().Cookies()) != 0 {
		t.Error("a path-routed request got a cookie")
	}
}

func TestEnterAnswersOtherMethodsWith405(t *testing.T) {
	request := httptest.NewRequest("POST", handoff.EnterPath, nil)
	recorder := httptest.NewRecorder()
	newHandoff(t).Enter(recorder, request)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", recorder.Code)
	}
	if got := recorder.Header().Get("Allow"); got != "GET, HEAD" {
		t.Errorf("Allow = %q", got)
	}
}

func TestEnterOnAnotherHostRedirectsToTheOrigin(t *testing.T) {
	query := "?next=%2Fevents%2F1&x=2"
	request := httptest.NewRequest("GET",
		"https://other.example.com"+handoff.EnterPath+query, nil)
	recorder := httptest.NewRecorder()
	newHandoff(t).Enter(recorder, request)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", recorder.Code)
	}
	want := instanceOrigin + handoff.EnterPath + query
	if got := recorder.Header().Get("Location"); got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
	if len(recorder.Result().Cookies()) != 0 {
		t.Error("another host got a nonce cookie")
	}
}

func TestEnterMatchesItsHostTheWayBrowsersSendIt(t *testing.T) {
	request := httptest.NewRequest("GET", handoff.EnterPath, nil)
	request.Host = "ACME.example.com:443"
	recorder := httptest.NewRecorder()
	newHandoff(t).Enter(recorder, request)
	location := recorder.Header().Get("Location")
	if !strings.HasPrefix(location, rootOrigin+"/assert?") {
		t.Errorf("Location = %q, want the root's /assert", location)
	}
	if len(recorder.Result().Cookies()) != 1 {
		t.Error("the origin's own host got no nonce cookie")
	}
}

// enterNext runs Enter for target and returns the nonce cookie's value and
// the next it sent to the root.
func enterNext(
	t *testing.T, handler *handoff.Handoff, target string,
) (string, string) {
	t.Helper()
	recorder := enter(handler, target)
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("Enter set %d cookies, want 1", len(cookies))
	}
	location, err := url.Parse(recorder.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return cookies[0].Value, location.Query().Get("next")
}

func TestEnterKeepsANextThatJustFitsTheCookie(t *testing.T) {
	handler := newHandoff(t)
	value, _ := enterNext(t, handler, handoff.EnterPath)
	nonce, _, _ := strings.Cut(value, ".")
	// The longest next whose unpadded base64 fits beside the nonce and dot
	// in a 2048-byte value.
	longest := (2048 - len(nonce) - 1) * 6 / 8
	for length, kept := range map[int]bool{longest: true, longest + 1: false} {
		next := "/" + strings.Repeat("a", length-1)
		value, sent := enterNext(t, handler,
			handoff.EnterPath+"?next="+url.QueryEscape(next))
		if len(value) > 2048 {
			t.Errorf("a %d-byte next made a %d-byte cookie",
				length, len(value))
		}
		want := "/"
		if kept {
			want = next
		}
		if sent != want {
			t.Errorf("a %d-byte next sent %d bytes, want %d",
				length, len(sent), len(want))
		}
	}
}
