package action_test

import (
	"net/http"
	"testing"

	"amadan.net/rastrillo/rastrillo/action"
	"github.com/go-chi/chi/v5"
)

// tables is a real table and a recorder, so each registration is tried
// on both: a recorder that let one through would list an action chi
// never serves, and --check would pass it.
func tables() map[string]func() *action.Routes {
	return map[string]func() *action.Routes{
		"a table": func() *action.Routes {
			return action.NewRoutes(chi.NewRouter(), emptyCtx)
		},
		"a recorder": action.NewRecorder,
	}
}

// registeredTwice are registrations chi would let the second overwrite
// without a word. Each names the earlier path the panic must give.
func registeredTwice() map[string]func(routes *action.Routes) {
	plain := http.NotFoundHandler()
	return map[string]func(routes *action.Routes){
		"one path twice": func(routes *action.Routes) {
			routes.Get("/x", handlerA)
			routes.Get("/x", handlerB)
		},
		"a parameter renamed, in a group": func(routes *action.Routes) {
			routes.Get("/tags/{id}", handlerA)
			routes.Group(func(group *action.Routes) {
				group.Get("/tags/{name}", handlerB)
			})
		},
		"inside a Route and outside it": func(routes *action.Routes) {
			routes.Route("/admin", func(admin *action.Routes) {
				admin.Post("/admin/x", handlerA)
			})
			routes.Post("/admin/x", handlerB)
		},
		"a Route's prefix with and without its slash": func(
			routes *action.Routes,
		) {
			routes.Route("/admin", func(admin *action.Routes) {
				admin.Get("/admin", handlerA)
				admin.Get("/admin/", handlerB)
			})
		},
		"a pattern with chi's anchors written out": func(
			routes *action.Routes,
		) {
			routes.Get("/tags/{id:[0-9]+}", handlerA)
			routes.Get("/tags/{id:^[0-9]+$}", handlerB)
		},
		"a Handle over a Get": func(routes *action.Routes) {
			routes.Get("/healthz", handlerA)
			routes.Handle("get", "/healthz", plain, "health")
		},
	}
}

func TestASecondRegistrationOfOneRoutePanics(t *testing.T) {
	for name, register := range registeredTwice() {
		for kind, build := range tables() {
			wantPanic(t, kind+": "+name, func() { register(build()) },
				"registered twice", "Register each method and path once")
		}
	}
	routes := action.NewRecorder()
	wantPanic(t, "a renamed parameter", func() {
		routes.Get("/tags/{id}", handlerA)
		routes.Get("/tags/{name}", handlerB)
	}, `GET "/tags/{name}"`, `as "/tags/{id}"`)
}

// Each pair below is two routes to chi, so neither may panic.
func TestRoutesThatOnlyLookAlikeAreBothRegistered(t *testing.T) {
	for kind, build := range tables() {
		routes := build()
		message := panicOf(func() {
			routes.Get("/tags/{id}", handlerA)
			routes.Get("/tags/{id:[0-9]+}", handlerB)
			routes.Post("/tags/{id}", handlerA)
			routes.Get("/x", handlerA)
			routes.Get("/x/", handlerB)
		})
		if message != "" {
			t.Errorf("%s: two different routes panicked: %s", kind, message)
		}
	}
}
