package action_test

import (
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/action"
	"amadan.net/rastrillo/rastrillo/op"
	"amadan.net/rastrillo/rastrillo/route"
)

// create stands in for a declared operation: a mark reads only its ID.
var create = op.Descriptor{
	Operation: rastrillo.Operation{ID: "notes.create"},
}

func TestAMarkIsRecordedWithTheRoute(t *testing.T) {
	recorder := action.NewRecorder()
	recorder.Post("/notes", handlerA, route.Operation(create))
	recorder.Get("/style.css", handlerB, route.PageOnly("the stylesheet"))
	recorder.Handle("GET", "/healthz", http.NotFoundHandler(), "health",
		route.PageOnly("the load balancer's probe"))

	listing := recorder.List()
	if len(listing) != 3 {
		t.Fatalf("List() has %d routes, want 3: %+v", len(listing), listing)
	}
	if got, want := listing[0].Operations, []string{"notes.create"}; !reflect.
		DeepEqual(got, want) || listing[0].PageOnly != "" {
		t.Errorf("POST /notes is recorded with %q and page-only %q, "+
			"want %q", got, listing[0].PageOnly, want)
	}
	if got := listing[1]; got.PageOnly != "the stylesheet" ||
		got.Operations != nil {
		t.Errorf("GET /style.css is recorded as %+v, want page-only "+
			"%q", got, "the stylesheet")
	}
	if got := listing[2].PageOnly; got != "the load balancer's probe" {
		t.Errorf("GET /healthz is recorded page-only %q", got)
	}
}

func TestARouteWithNoMarkRegistersAsBefore(t *testing.T) {
	recorder := action.NewRecorder()
	recorder.Get("/who", handlerA)
	want := []action.Route{{
		Method: "GET", Path: "/who", Handler: "handlerA",
		Action: testPackage + ".handlerA",
	}}
	if got := recorder.List(); !reflect.DeepEqual(got, want) {
		t.Errorf("List() is %+v, want %+v", got, want)
	}
}

func TestTwoMarksOnOneRoutePanics(t *testing.T) {
	registrations := []struct {
		name, method, path string
		register           func(routes *action.Routes)
	}{
		{"Get", "GET", "/notes", func(routes *action.Routes) {
			routes.Get("/notes", handlerA, route.Operation(create),
				route.PageOnly("the list"))
		}},
		{"Post", "POST", "/notes", func(routes *action.Routes) {
			routes.Post("/notes", handlerA, route.Operation(create),
				route.Operation(create))
		}},
		{"Handle", "GET", "/style.css", func(routes *action.Routes) {
			routes.Handle("get", "/style.css", http.NotFoundHandler(),
				"style", route.PageOnly("a"), route.PageOnly("b"))
		}},
	}
	for _, each := range registrations {
		t.Run(each.name, func(t *testing.T) {
			recorder := action.NewRecorder()
			message := panicOf(func() { each.register(recorder) })
			want := fmt.Sprintf(action.MsgTwoMarks, each.method, each.path)
			if message != want {
				t.Errorf("%s with two marks panicked with %q, want %q",
					each.name, message, want)
			}
			if listed := recorder.List(); len(listed) != 0 {
				t.Errorf("the refused route was recorded: %+v", listed)
			}
		})
	}
}
