package action_test

import (
	"net/http"
	"reflect"
	"testing"

	"amadan.net/rastrillo/rastrillo/action"
	"github.com/go-chi/chi/v5"
)

const testPackage = "amadan.net/rastrillo/rastrillo/action_test"

// panicsWhenBuilt is a guard whose middleware must never run: a
// recorder that built it would run whatever the guard reads from an
// app with no dependencies behind it.
func panicsWhenBuilt(t *testing.T) func(http.Handler) http.Handler {
	return func(http.Handler) http.Handler {
		t.Error("a recorder built a guard")
		panic("the guard was built")
	}
}

func TestARecorderListsWhatATableWould(t *testing.T) {
	table := action.NewRoutes(chi.NewRouter(), neverCalled(t))
	mountEverything(table, http.NotFoundHandler())
	table.Get("/tags/{id}", (&app{}).tagUpdate)

	recorder := action.NewRecorder()
	mountEverything(recorder, http.NotFoundHandler())
	recorder.Get("/tags/{id}", (&app{}).tagUpdate)

	if got, want := recorder.List(), table.List(); !reflect.DeepEqual(
		got, want,
	) {
		t.Errorf("the recorder listed %+v, a table %+v", got, want)
	}
	if got := recorder.List()[:4]; !reflect.DeepEqual(got, wantedRoutes()) {
		t.Errorf("the recorder listed %+v, want %+v", got, wantedRoutes())
	}
}

func TestARecorderBuildsNoGuard(t *testing.T) {
	recorder := action.NewRecorder()
	recorder.Guard("members", panicsWhenBuilt(t))
	recorder.Get("/who", handlerA)
	recorder.Route("/admin", func(admin *action.Routes) {
		admin.Guard("admin", panicsWhenBuilt(t))
		admin.Post("/admin/b", handlerB)
	})
	recorder.Group(func(group *action.Routes) {
		group.Guard("group", panicsWhenBuilt(t))
		group.Get("/g", handlerA)
	})
	want := [][]string{{"members"}, {"members", "admin"}, {"members", "group"}}
	listing := recorder.List()
	if len(listing) != len(want) {
		t.Fatalf("List() has %d routes, want %d", len(listing), len(want))
	}
	for position, route := range listing {
		if !reflect.DeepEqual(route.Guards, want[position]) {
			t.Errorf("route %d has guards %v, want %v",
				position, route.Guards, want[position])
		}
	}
}

func TestARecorderAcceptsANilMount(t *testing.T) {
	recorder := action.NewRecorder()
	recorder.Mount("/api/", nil, "api")
	recorder.Handle("GET", "/plain", nil, "plain")
	want := []action.Route{
		{Method: "*", Path: "/api/*", Handler: "api"},
		{Method: "GET", Path: "/plain", Handler: "plain"},
	}
	if got := recorder.List(); !reflect.DeepEqual(got, want) {
		t.Errorf("List() = %+v, want %+v", got, want)
	}
}

func TestARecorderStillRefusesAPathOutsideItsPrefix(t *testing.T) {
	recorder := action.NewRecorder()
	defer func() {
		if recover() == nil {
			t.Error("a path outside Route(\"/admin\") did not panic")
		}
	}()
	recorder.Route("/admin", func(admin *action.Routes) {
		admin.Get("/other", handlerA)
	})
}

func TestRouteRecordsAnActionsFullIdentity(t *testing.T) {
	routes := action.NewRoutes(chi.NewRouter(), neverCalled(t))
	plain := http.NotFoundHandler()
	routes.Get("/a", handlerA)
	routes.Post("/tags/{id}", (&app{}).tagUpdate)
	routes.Handle("GET", "/handled", plain, "handlerA")
	routes.Mount("/api", plain, "handlerA")
	want := []string{
		testPackage + ".handlerA",
		testPackage + ".(*app).tagUpdate",
		"",
		"",
	}
	listing := routes.List()
	if len(listing) != len(want) {
		t.Fatalf("List() has %d routes, want %d", len(listing), len(want))
	}
	for position, route := range listing {
		if route.Action != want[position] {
			t.Errorf("%s %s has Action %q, want %q",
				route.Method, route.Path, route.Action, want[position])
		}
	}
}

// registrationsThatPanic are the registrations a real table refuses,
// some by chi itself. A recorder that skipped them would list an app
// that then fails to start.
func registrationsThatPanic() map[string]func(routes *action.Routes) {
	plain := http.NotFoundHandler()
	return map[string]func(routes *action.Routes){
		"an unsupported Handle method": func(routes *action.Routes) {
			routes.Handle("FETCH", "/x", plain, "x")
		},
		"a second Mount on one prefix": func(routes *action.Routes) {
			routes.Mount("/api", plain, "api")
			routes.Mount("/api", plain, "api again")
		},
		"a second Route on one prefix": func(routes *action.Routes) {
			routes.Route("/admin", func(*action.Routes) {})
			routes.Route("/admin", func(*action.Routes) {})
		},
		"a * that is not last": func(routes *action.Routes) {
			routes.Get("/files/*/more", handlerA)
		},
		"a parameter without its }": func(routes *action.Routes) {
			routes.Get("/tags/{id", handlerA)
		},
		"an invalid regular expression": func(routes *action.Routes) {
			routes.Get("/tags/{id:[0-9}", handlerA)
		},
		"a parameter key used twice": func(routes *action.Routes) {
			routes.Get("/tags/{id}/{id}", handlerA)
		},
		"a Guard after a Get": func(routes *action.Routes) {
			routes.Get("/a", handlerA)
			routes.Guard("late", passThrough)
		},
		"a Guard after a Get in a Group": func(routes *action.Routes) {
			routes.Group(func(group *action.Routes) {
				group.Get("/a", handlerA)
				group.Guard("late", passThrough)
			})
		},
		"a Guard after a Get in a Route": func(routes *action.Routes) {
			routes.Route("/admin", func(admin *action.Routes) {
				admin.Get("/admin/a", handlerA)
				admin.Guard("late", passThrough)
			})
		},
	}
}

func TestARecorderPanicsWhereATableWould(t *testing.T) {
	for name, register := range registrationsThatPanic() {
		builders := map[string]func() *action.Routes{
			"a table": func() *action.Routes {
				return action.NewRoutes(chi.NewRouter(), neverCalled(t))
			},
			"a recorder": action.NewRecorder,
		}
		for builderName, build := range builders {
			t.Run(builderName+"/"+name, func(t *testing.T) {
				defer func() {
					if recover() == nil {
						t.Errorf("%s did not panic on %s", name, builderName)
					}
				}()
				register(build())
			})
		}
	}
}
