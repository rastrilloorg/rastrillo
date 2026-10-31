package action_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/action"
	"amadan.net/rastrillo/rastrillo/basepath"
	"github.com/go-chi/chi/v5"
)

// panicOf runs fill and returns what it panicked with, or "" when it
// returned instead.
func panicOf(fill func()) (message string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			message, _ = recovered.(string)
			if message == "" {
				message = "a panic that is not a string"
			}
		}
	}()
	fill()
	return ""
}

// wantPanic fails unless fill panics with a message naming each word.
func wantPanic(t *testing.T, what string, fill func(), words ...string) {
	t.Helper()
	message := panicOf(fill)
	if message == "" {
		t.Errorf("%s did not panic", what)
		return
	}
	for _, word := range words {
		if !strings.Contains(message, word) {
			t.Errorf("%s: panic %q does not mention %q", what, message, word)
		}
	}
}

func TestForFillsEachParameterInOrder(t *testing.T) {
	path := action.Path("/a/{x}/b/{y:[0-9]+}")
	if got := path.For("p q", 7); got != "/a/p%20q/b/7" {
		t.Errorf("For(\"p q\", 7) = %q, want /a/p%%20q/b/7", got)
	}
	if got := action.Path("/tags").For(); got != "/tags" {
		t.Errorf("For() on a path with no parameters = %q, want /tags", got)
	}
}

func TestForReadsChiParameters(t *testing.T) {
	for path, want := range map[action.Path]string{
		"/a/{id:[0-9]{2}}":  "/a/42",
		"/b/{name:[0-9]*}":  "/b/42",
		"/x-{id}.json":      "/x-42.json",
		"/e/{id:[0-9]{2}}/": "/e/42/",
	} {
		if got := path.For(42); got != want {
			t.Errorf("Path(%q).For(42) = %q, want %q", path, got, want)
		}
	}
	got := action.Path("/c/{one}-{two}/d").For("p", "q")
	if got != "/c/p-q/d" {
		t.Errorf("two parameters in one segment = %q, want /c/p-q/d", got)
	}
}

func TestForRefusesWhatLeavesTheRoute(t *testing.T) {
	path := action.Path("/things/{id}")
	for _, value := range []string{"", ".", ".."} {
		wantPanic(t, "For("+value+")", func() { path.For(value) },
			"/things/{id}", `"`+value+`"`, "{id}")
	}
}

func TestForPanicsOnTheWrongCount(t *testing.T) {
	path := action.Path("/a/{x}/b/{y:[0-9]+}")
	wantPanic(t, "one value for two parameters",
		func() { path.For("p q") },
		"/a/{x}/b/{y:[0-9]+}", "{x}, {y}", `"p q"`)
	wantPanic(t, "three values for two parameters",
		func() { path.For(1, 2, 3) }, `"1", "2", "3"`)
	wantPanic(t, "a value for no parameters",
		func() { action.Path("/tags").For(1) }, "no parameters", `"1"`)
}

func TestForRefusesACatchAll(t *testing.T) {
	wantPanic(t, "a catch-all", func() { action.Path("/files/*").For() },
		"/files/*", "/files/{name}")
	wantPanic(t, "a catch-all after a parameter",
		func() { action.Path("/files/{id}/*").For(1) }, "/files/{id}/*")
}

func TestForRefusesChisPrefixCatchAll(t *testing.T) {
	wantPanic(t, "a prefix catch-all",
		func() { action.Path("/files/prefix*").For() }, "/files/prefix*")
	if got := action.Path("/a/{b:x*}").For("x"); got != "/a/x" {
		t.Errorf("a * inside a parameter = %q, want /a/x", got)
	}
}

func TestForRefusesAnUnclosedParameter(t *testing.T) {
	wantPanic(t, "an unclosed parameter",
		func() { action.Path("/a/{id").For(1) }, "/a/{id")
}

// roundTripValues are the texts a value must come back as: a slash, a
// space, a percent sign, and escapes that must stay literal text.
var roundTripValues = []string{
	"a/b", "x y", "100%", "%2F", "%31", "%252F", "plain",
}

// tenant serves its app under example.com/acme, the way basepath
// serves a tenant on a hub's origin.
var tenant = basepath.Config{Origin: "https://example.com", Slug: "acme"}

// readsBack registers page on a fresh table and returns a handler that
// serves it, with the value PathValue("id") gave the last request.
func readsBack(page action.Path) (*chi.Mux, *string) {
	read := new(string)
	router := chi.NewRouter()
	routes := action.NewRoutes(router, emptyCtx)
	routes.Get(page, func(visit *action.Visit) {
		*read = visit.PathValue("id")
	})
	return router, read
}

func TestAValueRoundTrips(t *testing.T) {
	const page action.Path = "/things/{id}/view"
	router, read := readsBack(page)
	servers := map[string]http.Handler{
		"":      router,
		"/acme": tenant.Handler(router),
	}
	for base, server := range servers {
		for _, value := range roundTripValues {
			*read = ""
			target := "https://example.com" + base + page.For(value)
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder,
				httptest.NewRequest(http.MethodGet, target, nil))
			if recorder.Code != http.StatusOK || *read != value {
				t.Errorf("GET %s answered %d and read %q, want 200 and %q",
					target, recorder.Code, *read, value)
			}
		}
	}
}

// redirector serves POST /go, whose action redirects to page filled
// with whatever *value holds when the request arrives.
func redirector(page action.Path, value *string) *chi.Mux {
	router := chi.NewRouter()
	routes := action.NewRoutes(router, emptyCtx)
	routes.Post("/go", func(visit *action.Visit) {
		visit.Redirect(page.For(*value))
	})
	return router
}

func TestARedirectStaysOnTheRoute(t *testing.T) {
	const page action.Path = "/things/{id}"
	var value string
	router := redirector(page, &value)
	for base, server := range map[string]http.Handler{
		"": router, "/acme": tenant.Handler(router),
	} {
		for _, value = range roundTripValues {
			recorder := post(server, base+"/go")
			want := base + "/things/" + url.PathEscape(value)
			location := recorder.Header().Get("Location")
			if recorder.Code != http.StatusSeeOther || location != want {
				t.Errorf("redirect to %q answered %d %q, want 303 %q",
					value, recorder.Code, location, want)
			}
		}
		for _, value = range []string{"", ".."} {
			recorder := httptest.NewRecorder()
			wantPanic(t, base+" redirect to "+value, func() {
				server.ServeHTTP(recorder, postRequest(base+"/go"))
			}, "{id}")
			if len(recorder.Header()) != 0 || recorder.Code != 200 {
				t.Errorf("%s: a refused redirect wrote %d %v",
					base, recorder.Code, recorder.Header())
			}
		}
	}
}

func postRequest(path string) *http.Request {
	return httptest.NewRequest(
		http.MethodPost, "https://example.com"+path, nil)
}

func post(server http.Handler, path string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, postRequest(path))
	return recorder
}
