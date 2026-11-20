package instance_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/dbtest"
	"amadan.net/rastrillo/rastrillo/handoff/handofftest"
	"amadan.net/rastrillo/rastrillo/instance"
	"amadan.net/rastrillo/rastrillo/roster"
)

var schema = dbtest.FromSet(roster.Schema)

func TestMain(main *testing.M) {
	code := main.Run()
	schema.Remove()
	os.Exit(code)
}

const (
	platformHost = "https://acme.oncarlos.test"
	customOrigin = "https://blog.example.org"
)

// noMembers is a Store for a roster that never lists anyone.
type noMembers struct{}

func (noMembers) Members(context.Context) ([]roster.Member, error) {
	return nil, nil
}
func (noMembers) Put(context.Context, roster.Member) error { return nil }
func (noMembers) Remove(context.Context, string) error     { return nil }

func TestServingOriginFallsBack(t *testing.T) {
	ctx := context.Background()
	db := schema.Open(t)
	origin, err := instance.ServingOrigin(ctx, db, platformHost)
	if err != nil || origin != platformHost {
		t.Fatalf("before any snapshot: %q, %v", origin, err)
	}

	root := handofftest.New(t)
	setup := root.Setup()
	// The roster always arrives on the platform host, whatever the
	// instance serves on.
	verifier, err := roster.NewVerifier(setup.Root, platformHost, setup.Keys)
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := roster.New(roster.Config{
		Verifier: verifier, Store: noMembers{}, DB: db,
		Roles: []string{"owner"},
	})
	if err != nil {
		t.Fatal(err)
	}
	deliver := func(token string) {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost,
			platformHost+roster.Path, strings.NewReader(token))
		response := httptest.NewRecorder()
		receiver.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("roster status = %d", response.Code)
		}
	}

	deliver(root.RosterOrigin(t, platformHost, 1, roster.Active,
		customOrigin))
	origin, err = instance.ServingOrigin(ctx, db, platformHost)
	if err != nil || origin != customOrigin {
		t.Fatalf("with a custom origin: %q, %v", origin, err)
	}

	deliver(root.Roster(t, platformHost, 2, roster.Active))
	origin, err = instance.ServingOrigin(ctx, db, platformHost)
	if err != nil || origin != platformHost {
		t.Fatalf("after going back: %q, %v", origin, err)
	}
}

func TestRedirectToOriginSparesPlatformPaths(t *testing.T) {
	served := http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			response.WriteHeader(http.StatusTeapot)
		})
	handler, err := instance.RedirectToOrigin(customOrigin, served)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		method, target string
		status         int
		location       string
	}{
		{"GET", customOrigin + "/posts/1?page=2", 418, ""},
		{"GET", "https://BLOG.example.org./posts", 418, ""},
		{"GET", platformHost + "/posts/1?page=2&x=%2F", 307,
			customOrigin + "/posts/1?page=2&x=%2F"},
		{"HEAD", platformHost + "/", 307, customOrigin + "/"},
		{"POST", platformHost + "/comments", 307,
			customOrigin + "/comments"},
		{"GET", platformHost + "/a%2Fb", 307, customOrigin + "/a%2Fb"},
		{"POST", platformHost + roster.Path, 418, ""},
		{"GET", platformHost + "/_carlos/enter?next=/", 418, ""},
		{"GET", platformHost + "/api/version", 418, ""},
		{"GET", platformHost + "/healthz", 418, ""},
		// Only the platform paths themselves, spelt plainly.
		{"GET", platformHost + "/_carlos/../admin", 307,
			customOrigin + "/_carlos/../admin"},
		{"GET", platformHost + "/_carlosx", 307, customOrigin + "/_carlosx"},
		{"GET", platformHost + "/api/version/x", 307,
			customOrigin + "/api/version/x"},
	}
	for _, test := range cases {
		request := httptest.NewRequest(test.method, test.target, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		location := response.Header().Get("Location")
		if response.Code != test.status || location != test.location {
			t.Errorf("%s %s = %d to %q, want %d to %q", test.method,
				test.target, response.Code, location, test.status,
				test.location)
		}
		if test.status == 307 &&
			response.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s %s: a redirect a browser may keep", test.method,
				test.target)
		}
	}
}

func TestRedirectToOriginWithoutOneServesEveryHost(t *testing.T) {
	served := http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			response.WriteHeader(http.StatusTeapot)
		})
	handler, err := instance.RedirectToOrigin("", served)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", platformHost+"/posts", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want the app's", response.Code)
	}
}

func TestRedirectToOriginRefusesABadOrigin(t *testing.T) {
	for _, origin := range []string{
		"http://blog.example.org",
		"http://localhost:8080",
		"https://blog.example.org/",
		"https://blog.example.org/path",
		"https://user@blog.example.org",
		"https://Blog.example.org",
		"blog.example.org",
		"https://",
	} {
		handler, err := instance.RedirectToOrigin(origin,
			http.NotFoundHandler())
		if err == nil || handler != nil {
			t.Errorf("RedirectToOrigin(%q) = %v, %v", origin, handler, err)
		}
	}
}
