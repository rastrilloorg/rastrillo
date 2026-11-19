package handoff_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/basepath"
	"amadan.net/rastrillo/rastrillo/handoff"
	"amadan.net/rastrillo/rastrillo/sessions"
)

var renewNow = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

// renewed runs one request carrying a session of the given method and age
// through Renew and says whether the wrapped handler ran.
func renewed(
	t *testing.T, method, requestMethod, target string, age time.Duration,
) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	config := newConfig(t)
	config.Now = func() time.Time { return renewNow }
	handler, err := handoff.New(config)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	wrapped := handler.Renew(http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			called = true
		}))
	request := httptest.NewRequest(requestMethod, target, nil)
	request = sessions.WithSession(request, sessions.Session{
		Subject: "7", Method: method, At: renewNow.Add(-age),
	})
	recorder := httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, request)
	return recorder, called
}

func TestRenewPassesAFreshHandoffSession(t *testing.T) {
	_, called := renewed(t, handoff.Method, "GET", "/events/1",
		59*time.Minute)
	if !called {
		t.Error("a fresh handoff session did not reach the handler")
	}
}

func TestRenewPassesASessionExactlyMaxAgeOld(t *testing.T) {
	_, called := renewed(t, handoff.Method, "GET", "/events/1", time.Hour)
	if !called {
		t.Error("a session exactly MaxAge old was treated as stale")
	}
}

func TestRenewSendsAStaleGETThroughTheRoot(t *testing.T) {
	recorder, called := renewed(t, handoff.Method, "GET",
		"/events/1?x=2", 61*time.Minute)
	if called {
		t.Error("a stale session reached the handler")
	}
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", recorder.Code)
	}
	want := "/_carlos/enter?next=%2Fevents%2F1%3Fx%3D2"
	if got := recorder.Header().Get("Location"); got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
}

func TestRenewRefusesAStalePOST(t *testing.T) {
	recorder, called := renewed(t, handoff.Method, "POST", "/events/1",
		61*time.Minute)
	if called {
		t.Error("a stale POST reached the handler")
	}
	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", recorder.Code)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestRenewLeavesEnterPathAloneWhateverTheMethod(t *testing.T) {
	for _, method := range []string{"GET", "POST"} {
		_, called := renewed(t, handoff.Method, method,
			handoff.EnterPath+"?next=%2Fx", 48*time.Hour)
		if !called {
			t.Errorf("a stale %s to EnterPath did not reach the handler",
				method)
		}
	}
}

func TestRenewIgnoresOtherMethods(t *testing.T) {
	_, called := renewed(t, "magic", "GET", "/events/1", 48*time.Hour)
	if !called {
		t.Error("a non-handoff session was renewed")
	}
}

func TestRenewPassesRequestsWithNoSession(t *testing.T) {
	handler, err := handoff.New(newConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	called := false
	wrapped := handler.Renew(http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			called = true
		}))
	wrapped.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest("GET", "/", nil))
	if !called {
		t.Error("a request with no session did not reach the handler")
	}
}

func TestRenewLeavesPathRoutedRequestsAlone(t *testing.T) {
	config := newConfig(t)
	config.Now = func() time.Time { return renewNow }
	handler, err := handoff.New(config)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	renewing := handler.Renew(http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			called = true
		}))
	staleSession := http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			renewing.ServeHTTP(response, sessions.WithSession(request,
				sessions.Session{
					Method: handoff.Method,
					At:     renewNow.Add(-48 * time.Hour),
				}))
		})
	served := basepath.Config{
		Slug: "acme", Origin: "https://hub.example.com",
	}.Handler(staleSession)
	request := httptest.NewRequest("GET", "/acme/events/1", nil)
	request.Host = "hub.example.com"
	served.ServeHTTP(httptest.NewRecorder(), request)
	if !called {
		t.Error("a path-routed request was renewed")
	}
}

func TestSweepDeletesExpiredReplayRows(t *testing.T) {
	config := newConfig(t)
	handler, err := handoff.New(config)
	if err != nil {
		t.Fatal(err)
	}
	for jti, expires := range map[string]int64{
		"past": renewNow.Unix() - 1, "now": renewNow.Unix(),
		"future": renewNow.Unix() + 1,
	} {
		_, err := config.DB.Exec(`INSERT INTO handoff_used
			(jti, expires_at) VALUES (?, ?)`, jti, expires)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := handler.Sweep(renewNow); err != nil {
		t.Fatal(err)
	}
	var kept string
	err = config.DB.QueryRow(
		`SELECT group_concat(jti) FROM handoff_used`).Scan(&kept)
	if err != nil {
		t.Fatal(err)
	}
	if kept != "future" {
		t.Errorf("rows kept = %q, want only future", kept)
	}
}

func TestRenewStillRenewsAVeryLongURL(t *testing.T) {
	long := "/events/1?q=" + strings.Repeat("x", 5000)
	recorder, _ := renewed(t, handoff.Method, "GET", long, 61*time.Minute)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", recorder.Code)
	}
	value, next := enterNext(t, newHandoff(t),
		recorder.Header().Get("Location"))
	if len(value) > 2048 {
		t.Errorf("nonce cookie is %d bytes, over 2048", len(value))
	}
	if next != "/" {
		t.Errorf("next is %d bytes, want /", len(next))
	}
}
