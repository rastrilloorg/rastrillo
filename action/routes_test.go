package action_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/action"
	"github.com/go-chi/chi/v5"
)

// app stands for an app struct whose routes are registered before the
// app is wired. Recording a route needs only the method's name, never a
// working receiver.
type app struct{}

func (app *app) tagUpdate(visit *action.Visit) {}

func handlerA(visit *action.Visit) {}
func handlerB(visit *action.Visit) {}

// passThrough is a guard that lets every request on.
func passThrough(next http.Handler) http.Handler { return next }

// mountEverything registers one route of every kind, the way an app's
// own "register everything" function will.
func mountEverything(routes *action.Routes, api http.Handler) {
	plain := http.HandlerFunc(func(
		response http.ResponseWriter, request *http.Request,
	) {
	})
	routes.Get("/a", handlerA)
	routes.Route("/admin", func(admin *action.Routes) {
		admin.Guard("admin", passThrough)
		admin.Post("/admin/b", handlerB)
		admin.Handle("GET", "/admin/c", plain, "plain")
	})
	routes.Mount("/api", api, "api")
}

func wantedRoutes() []action.Route {
	return []action.Route{
		{Method: "GET", Path: "/a", Handler: "handlerA",
			Action: testPackage + ".handlerA"},
		{Method: "POST", Path: "/admin/b", Handler: "handlerB",
			Action: testPackage + ".handlerB", Guards: []string{"admin"}},
		{Method: "GET", Path: "/admin/c", Handler: "plain",
			Guards: []string{"admin"}},
		{Method: "*", Path: "/api/*", Handler: "api"},
	}
}

func neverCalled(t *testing.T) action.CtxFor {
	return func(request *http.Request) (*rastrillo.Ctx, error) {
		t.Error("CtxFor was called")
		return nil, errors.New("not wired")
	}
}

func TestRoutesRecordEveryRouteWithItsGuards(t *testing.T) {
	routes := action.NewRoutes(chi.NewRouter(), neverCalled(t))
	mountEverything(routes, http.NotFoundHandler())
	if got := routes.List(); !reflect.DeepEqual(got, wantedRoutes()) {
		t.Errorf("List() = %+v, want %+v", got, wantedRoutes())
	}
}

func TestAMethodValueIsRecordedByItsMethodName(t *testing.T) {
	routes := action.NewRoutes(chi.NewRouter(), neverCalled(t))
	routes.Post("/tags/{id}", (&app{}).tagUpdate)
	got := routes.List()
	if len(got) != 1 || got[0].Handler != "tagUpdate" {
		t.Errorf("List() = %+v, want one route named tagUpdate", got)
	}
}

func TestRegisteringNeedsNoDependencies(t *testing.T) {
	calls := 0
	failing := func(request *http.Request) (*rastrillo.Ctx, error) {
		calls++
		return nil, errors.New("not wired")
	}
	routes := action.NewRoutes(chi.NewRouter(), failing)
	routes.Get("/tags", (&app{}).tagUpdate)
	mountEverything(routes, http.NotFoundHandler())
	if len(routes.List()) != 5 {
		t.Errorf("List() has %d routes, want 5", len(routes.List()))
	}
	if calls != 0 {
		t.Errorf("CtxFor was called %d times while registering", calls)
	}
}

func TestOneRequestsActorNeverReachesTheNext(t *testing.T) {
	shared := &rastrillo.Ctx{}
	sharedCtxFor := func(request *http.Request) (*rastrillo.Ctx, error) {
		return shared, nil
	}
	var seen []rastrillo.Actor
	router := chi.NewRouter()
	routes := action.NewRoutes(router, sharedCtxFor)
	routes.Get("/who", func(visit *action.Visit) {
		seen = append(seen, visit.Ctx().Actor)
	})
	actor := rastrillo.Actor{Subject: "ana", Access: rastrillo.Write}
	first := rastrillo.WithActor(
		httptest.NewRequest(http.MethodGet, "/who", nil), actor,
	)
	second := httptest.NewRequest(http.MethodGet, "/who", nil)
	router.ServeHTTP(httptest.NewRecorder(), first)
	router.ServeHTTP(httptest.NewRecorder(), second)
	want := []rastrillo.Actor{actor, {}}
	if !reflect.DeepEqual(seen, want) {
		t.Errorf("handlers saw %+v, want %+v", seen, want)
	}
	if shared.Actor != (rastrillo.Actor{}) {
		t.Errorf("the shared Ctx's Actor became %+v", shared.Actor)
	}
}

func TestAContextThatCannotBeBuiltIsAFiveHundred(t *testing.T) {
	failing := func(request *http.Request) (*rastrillo.Ctx, error) {
		return nil, errors.New("no database")
	}
	ran := false
	router := chi.NewRouter()
	routes := action.NewRoutes(router, failing)
	routes.Get("/who", func(visit *action.Visit) { ran = true })
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/who", nil))
	body := strings.TrimSpace(recorder.Body.String())
	if recorder.Code != 500 || body != "Something went wrong." {
		t.Errorf("answered %d %q, want 500 %q",
			recorder.Code, body, "Something went wrong.")
	}
	if ran {
		t.Error("the handler ran without a context")
	}
}

func TestAGuardAfterARouteFailsAtOnce(t *testing.T) {
	routes := action.NewRoutes(chi.NewRouter(), neverCalled(t))
	routes.Post("/b", handlerB)
	defer func() {
		message, _ := recover().(string)
		for _, word := range []string{"admin", "/b", "first"} {
			if !strings.Contains(message, word) {
				t.Errorf("panic %q does not mention %q", message, word)
			}
		}
	}()
	routes.Guard("admin", passThrough)
	t.Error("Guard after a route did not panic")
}

func TestGuardsRunBeforeTheHandler(t *testing.T) {
	ran := false
	refuse := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(
			response http.ResponseWriter, request *http.Request,
		) {
			http.Error(response, "No.", http.StatusForbidden)
		})
	}
	router := chi.NewRouter()
	routes := action.NewRoutes(router, func(
		request *http.Request,
	) (*rastrillo.Ctx, error) {
		return &rastrillo.Ctx{}, nil
	})
	routes.Group(func(guarded *action.Routes) {
		guarded.Guard("members", refuse)
		guarded.Get("/inside", func(visit *action.Visit) { ran = true })
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/inside", nil))
	if recorder.Code != http.StatusForbidden || ran {
		t.Errorf("answered %d, handler ran = %v; want 403 and false",
			recorder.Code, ran)
	}
}
