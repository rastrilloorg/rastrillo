package action_test

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/action"
	"github.com/go-chi/chi/v5"
)

// emptyCtx gives every request a context, so a route can be served.
func emptyCtx(request *http.Request) (*rastrillo.Ctx, error) {
	return &rastrillo.Ctx{}, nil
}

// reached answers every request and notes that it did.
func reached(hit *bool) http.Handler {
	return http.HandlerFunc(func(
		response http.ResponseWriter, request *http.Request,
	) {
		*hit = true
	})
}

// requestFor turns a recorded path into one a client could send: a
// wildcard stands for any rest of the path.
func requestFor(path string) *http.Request {
	return httptest.NewRequest(
		http.MethodGet, strings.Replace(path, "*", "anything", 1), nil,
	)
}

// serveRecorded sends a GET to the path List recorded and reports
// whether the mounted handler was reached.
func serveRecorded(router http.Handler, path string, hit *bool) bool {
	*hit = false
	router.ServeHTTP(httptest.NewRecorder(), requestFor(path))
	return *hit
}

type pathCase struct {
	name  string
	mount func(routes *action.Routes, hit *bool)
	want  string
}

func pathCases() []pathCase {
	return []pathCase{
		{"trailing slash prefix", func(routes *action.Routes, hit *bool) {
			routes.Route("/admin/", func(admin *action.Routes) {
				admin.Handle("GET", "/admin/x", reached(hit), "x")
			})
		}, "/admin/x"},
		{"root prefix", func(routes *action.Routes, hit *bool) {
			routes.Route("/", func(root *action.Routes) {
				root.Handle("GET", "/x", reached(hit), "x")
			})
		}, "/x"},
		{"nested trailing slashes", func(routes *action.Routes, hit *bool) {
			routes.Route("/a/", func(outer *action.Routes) {
				outer.Route("/a/b/", func(inner *action.Routes) {
					inner.Handle("GET", "/a/b/c", reached(hit), "c")
				})
			})
		}, "/a/b/c"},
		{"mount at root", func(routes *action.Routes, hit *bool) {
			routes.Mount("/", reached(hit), "everything")
		}, "/*"},
		{"mount with trailing slash", func(routes *action.Routes, hit *bool) {
			routes.Mount("/api/", reached(hit), "api")
		}, "/api/*"},
		{"mount inside a prefix", func(routes *action.Routes, hit *bool) {
			routes.Route("/admin/", func(admin *action.Routes) {
				admin.Mount("/admin/api", reached(hit), "api")
			})
		}, "/admin/api/*"},
	}
}

func TestRecordedPathsAreTheOnesChiServes(t *testing.T) {
	for _, scenario := range pathCases() {
		router := chi.NewRouter()
		routes := action.NewRoutes(router, emptyCtx)
		var hit bool
		scenario.mount(routes, &hit)
		listed := routes.List()
		if len(listed) != 1 || listed[0].Path != scenario.want {
			t.Errorf("%s: List() = %+v, want path %s",
				scenario.name, listed, scenario.want)
			continue
		}
		if !serveRecorded(router, listed[0].Path, &hit) {
			t.Errorf("%s: chi does not serve the recorded path %s",
				scenario.name, listed[0].Path)
		}
	}
}

// trace is the guards a request passed, in order.
type trace []string

func (passed *trace) guard(name string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(
			response http.ResponseWriter, request *http.Request,
		) {
			*passed = append(*passed, name)
			next.ServeHTTP(response, request)
		})
	}
}

// nestedRoutes registers a guard above a group, a route prefix, a
// prefix inside a group and an unguarded sibling group.
func nestedRoutes(routes *action.Routes, passed *trace, hit *bool) {
	routes.Guard("outer", passed.guard("outer"))
	routes.Group(func(inside *action.Routes) {
		inside.Guard("inner", passed.guard("inner"))
		inside.Handle("GET", "/in-group", reached(hit), "inGroup")
		inside.Route("/deep", func(deep *action.Routes) {
			deep.Handle("GET", "/deep/x", reached(hit), "deep")
		})
	})
	routes.Group(func(sibling *action.Routes) {
		sibling.Handle("GET", "/sibling", reached(hit), "sibling")
	})
	routes.Route("/admin", func(admin *action.Routes) {
		admin.Handle("GET", "/admin/in-route", reached(hit), "inRoute")
		admin.Route("/admin/nested", func(nested *action.Routes) {
			nested.Guard("nested", passed.guard("nested"))
			nested.Handle("GET", "/admin/nested/y", reached(hit), "nested")
		})
	})
	routes.Mount("/api", reached(hit), "api")
}

// served is a route table on its own router, with the guards the last
// request passed and whether it reached a handler.
type served struct {
	router *chi.Mux
	routes *action.Routes
	passed trace
	hit    bool
}

func newServed() *served {
	router := chi.NewRouter()
	return &served{router: router, routes: action.NewRoutes(router, emptyCtx)}
}

// wantGuards checks that List records exactly the wanted paths, each
// with its guards, and that a request to each is served through them.
func (table *served) wantGuards(t *testing.T, want map[string][]string) {
	t.Helper()
	listed := table.routes.List()
	if len(listed) != len(want) {
		t.Fatalf("List() has %d routes, want %d", len(listed), len(want))
	}
	for _, route := range listed {
		guards, known := want[route.Path]
		if !known {
			t.Errorf("List() has the unexpected path %s", route.Path)
			continue
		}
		if !reflect.DeepEqual(route.Guards, guards) {
			t.Errorf("%s: recorded guards %v, want %v",
				route.Path, route.Guards, guards)
		}
		table.passed = nil
		if !serveRecorded(table.router, route.Path, &table.hit) {
			t.Errorf("%s: chi does not serve the recorded path", route.Path)
		}
		if !reflect.DeepEqual([]string(table.passed), guards) {
			t.Errorf("%s: request passed guards %v, want %v",
				route.Path, []string(table.passed), guards)
		}
	}
}

func TestGuardsAndPrefixesAreInheritedAndStayInTheirGroup(t *testing.T) {
	table := newServed()
	nestedRoutes(table.routes, &table.passed, &table.hit)
	table.wantGuards(t, map[string][]string{
		"/in-group":       {"outer", "inner"},
		"/deep/x":         {"outer", "inner"},
		"/sibling":        {"outer"},
		"/admin/in-route": {"outer"},
		"/admin/nested/y": {"outer", "nested"},
		"/api/*":          {"outer"},
	})
}

func TestChangingAListingLeavesTheNextOneUnchanged(t *testing.T) {
	routes := action.NewRoutes(chi.NewRouter(), emptyCtx)
	routes.Guard("members", passThrough)
	routes.Get("/a", handlerA)
	first := routes.List()
	first[0].Guards[0] = "other"
	first[0].Path = "/changed"
	got := routes.List()
	if got[0].Guards[0] != "members" || got[0].Path != "/a" {
		t.Errorf("List() after a change = %+v, want members on /a", got[0])
	}
}
