package action_test

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"amadan.net/rastrillo/rastrillo/action"
	"github.com/go-chi/chi/v5"
)

// readsEach serves page at the root and under the tenant, and records
// what PathValue gave each name on the last request.
func readsEach(
	page action.Path, names ...string,
) (map[string]http.Handler, map[string]string) {
	read := map[string]string{}
	router := chi.NewRouter()
	routes := action.NewRoutes(router, emptyCtx)
	routes.Get(page, func(visit *action.Visit) {
		for _, name := range names {
			read[name] = visit.PathValue(name)
		}
	})
	servers := map[string]http.Handler{
		"": router, "/acme": tenant.Handler(router),
	}
	return servers, read
}

// wantReadBack fills page with values, requests the URL at the root
// and under the tenant, and fails unless each name reads its value.
func wantReadBack(
	t *testing.T, page action.Path, want map[string]string,
	values ...any,
) {
	t.Helper()
	servers, read := readsEach(page, slices.Collect(maps.Keys(want))...)
	for base, server := range servers {
		clear(read)
		target := "https://example.com" + base + page.For(values...)
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder,
			httptest.NewRequest(http.MethodGet, target, nil))
		if recorder.Code != http.StatusOK || !maps.Equal(read, want) {
			t.Errorf("GET %s answered %d and read %v, want 200 and %v",
				target, recorder.Code, read, want)
		}
	}
}

func TestAnEmbeddedParameterReadsBackTheValueGiven(t *testing.T) {
	wantReadBack(t, "/c/{owner}-{name}/d",
		map[string]string{"owner": "a", "name": "b-c"}, "a", "b-c")
	wantReadBack(t, "/x-{id}.json", map[string]string{"id": "a-b"}, "a-b")
	wantReadBack(t, "/v/{id}.{format}",
		map[string]string{"id": "7", "format": "tar.gz"}, "7", "tar.gz")
}

func TestForRefusesAValueHoldingTheCharacterThatEndsIt(t *testing.T) {
	wantPanic(t, "a - in {owner}", func() {
		action.Path("/c/{owner}-{name}/d").For("a-b", "c")
	}, "/c/{owner}-{name}/d", `"a-b"`, "{owner}", `"-"`)
	wantPanic(t, "a . in {id}", func() {
		action.Path("/x-{id}.json").For("a.b")
	}, "/x-{id}.json", `"a.b"`, "{id}", `"."`)
}

func TestAValueItsPatternMatchesReadsBack(t *testing.T) {
	wantReadBack(t, "/tags/{id:[a-z]+}", map[string]string{"id": "abc"},
		"abc")
	wantReadBack(t, "/tags/{id:[a-z-]+}/x",
		map[string]string{"id": "a-b"}, "a-b")
}

func TestForRefusesAValueItsPatternDoesNotMatch(t *testing.T) {
	wantPanic(t, "digits for letters", func() {
		action.Path("/b/{name:[a-z]*}").For(42)
	}, "/b/{name:[a-z]*}", `"42"`, "{name}", "[a-z]*")
	// chi anchors the pattern, so one digit does not match "12".
	wantPanic(t, "two digits for one", func() {
		action.Path("/n/{id:[0-9]}").For(12)
	}, `"12"`, "{id}")
}

// chi checks the pattern against "x%20y" under a tenant and "x y" at
// the root, so no value that escapes can match at both.
func TestForRefusesAPatternedValueThatEscapes(t *testing.T) {
	for _, value := range []string{"x y", "a/b", "100%"} {
		wantPanic(t, "an escaping value", func() {
			action.Path("/tags/{id:.+}").For(value)
		}, "/tags/{id:.+}", "{id}", "escaped")
	}
}

// url.PathEscape turns "/" into "%2F", and chi matches that escaped
// text, so its F ends {id} as surely as a literal one would.
func TestForRefusesAValueWhoseEscapingHoldsTheEndingCharacter(t *testing.T) {
	wantPanic(t, "a / that escapes to %2F", func() {
		action.Path("/{id}F{rest}").For("/", "z")
	}, "/{id}F{rest}", `"/"`, `"%2F"`, "{id}", `"F"`)
	wantReadBack(t, "/{id}F{rest}",
		map[string]string{"id": "x y", "rest": "z"}, "x y", "z")
}
