package action_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"amadan.net/rastrillo/rastrillo/action"
	"github.com/go-chi/chi/v5"
)

func TestPathsInsideARouteAreFull(t *testing.T) {
	table := newServed()
	var read string
	table.routes.Route("/admin", func(admin *action.Routes) {
		admin.Guard("admin", table.passed.guard("admin"))
		admin.Get("/admin/tags/{id}", func(visit *action.Visit) {
			read, table.hit = visit.PathValue("id"), true
		})
		admin.Handle("GET", "/admin/plain", reached(&table.hit), "plain")
	})
	table.wantGuards(t, map[string][]string{
		"/admin/tags/{id}": {"admin"},
		"/admin/plain":     {"admin"},
	})

	table.passed = nil
	table.router.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/admin/tags/7", nil))
	if read != "7" || len(table.passed) != 1 {
		t.Errorf("GET /admin/tags/7 read %q through guards %v, "+
			"want 7 through admin", read, []string(table.passed))
	}
}

// insideAdmin is each way to register a path inside Route("/admin").
func insideAdmin(path action.Path) map[string]func(admin *action.Routes) {
	plain := http.NotFoundHandler()
	return map[string]func(admin *action.Routes){
		"Get":  func(admin *action.Routes) { admin.Get(path, handlerA) },
		"Post": func(admin *action.Routes) { admin.Post(path, handlerB) },
		"Handle": func(admin *action.Routes) {
			admin.Handle("GET", path, plain, "x")
		},
		"Mount": func(admin *action.Routes) { admin.Mount(path, plain, "x") },
		"Route": func(admin *action.Routes) {
			admin.Route(path, func(*action.Routes) {})
		},
	}
}

func TestARelativePathInsideARoutePanics(t *testing.T) {
	for kind, register := range insideAdmin("/x") {
		routes := action.NewRoutes(chi.NewRouter(), emptyCtx)
		wantPanic(t, kind+" /x inside /admin", func() {
			routes.Route("/admin/", register)
		}, `Route("/admin")`, `"/x"`, `"/admin/x"`)
	}
	routes := action.NewRoutes(chi.NewRouter(), emptyCtx)
	wantPanic(t, "/adminx inside /admin", func() {
		routes.Route("/admin", func(admin *action.Routes) {
			admin.Get("/adminx", handlerA)
		})
	}, `"/adminx"`, `"/admin/adminx"`)
	wantPanic(t, "x at the root", func() {
		routes.Get("x", handlerA)
	}, `"x"`, `"/x"`)
	wantPanic(t, "a nested Route", func() {
		routes.Route("/a", func(outer *action.Routes) {
			outer.Route("/a/b", func(inner *action.Routes) {
				inner.Get("/a/c", handlerA)
			})
		})
	}, `Route("/a/b")`, `"/a/c"`, `"/a/b/a/c"`)
}

// named answers a request by noting the handler's name.
func named(hit *string, name string) http.Handler {
	return http.HandlerFunc(func(
		response http.ResponseWriter, request *http.Request,
	) {
		*hit = name
	})
}

func TestRootAndTrailingSlashPrefixes(t *testing.T) {
	var hit string
	router := chi.NewRouter()
	routes := action.NewRoutes(router, emptyCtx)
	routes.Route("/", func(root *action.Routes) {
		root.Handle("GET", "/", named(&hit, "home"), "home")
		root.Handle("GET", "/about", named(&hit, "about"), "about")
	})
	routes.Route("/admin/", func(admin *action.Routes) {
		admin.Handle("GET", "/admin", named(&hit, "admin"), "admin")
		admin.Handle("GET", "/admin/users", named(&hit, "users"), "users")
	})
	paths := []string{"/", "/about", "/admin", "/admin/users"}
	listed := routes.List()
	if len(listed) != len(paths) {
		t.Fatalf("List() = %+v, want the paths %v", listed, paths)
	}
	for position, route := range listed {
		if route.Path != paths[position] {
			t.Errorf("route %d recorded %s, want %s",
				position, route.Path, paths[position])
		}
		hit = ""
		router.ServeHTTP(httptest.NewRecorder(), requestFor(route.Path))
		if hit != route.Handler {
			t.Errorf("GET %s reached %q, want %q",
				route.Path, hit, route.Handler)
		}
	}
}

func TestAGroupInsideAPrefixedRoute(t *testing.T) {
	table := newServed()
	table.routes.Route("/admin", func(admin *action.Routes) {
		admin.Handle("GET", "/admin/open", reached(&table.hit), "open")
		admin.Group(func(staff *action.Routes) {
			staff.Guard("staff", table.passed.guard("staff"))
			staff.Handle("GET", "/admin/staff", reached(&table.hit), "staff")
		})
	})
	table.wantGuards(t, map[string][]string{
		"/admin/open":  nil,
		"/admin/staff": {"staff"},
	})

	routes := action.NewRoutes(chi.NewRouter(), emptyCtx)
	wantPanic(t, "a relative path in a group", func() {
		routes.Route("/admin", func(admin *action.Routes) {
			admin.Group(func(staff *action.Routes) {
				staff.Get("/staff", handlerA)
			})
		})
	}, `Route("/admin")`, `"/staff"`, `"/admin/staff"`)
}
