package api

import (
	"net/http"
	"testing"
)

func TestServedListsWhatNewRegistered(t *testing.T) {
	handler := mustNew(t, config(), getThing(ok), createThing(ok))
	routes, err := Served(handler)
	if err != nil {
		t.Fatalf("Served: %v", err)
	}
	want := []struct{ method, pattern, id string }{
		{http.MethodGet, "/api/v1/things/{id}", "things.get"},
		{http.MethodPost, "/api/v1/things", "things.create"},
	}
	if len(routes) != len(want) {
		t.Fatalf("Served = %v, want %d routes", routes, len(want))
	}
	for index, route := range routes {
		got := want[index]
		if route.Method != got.method || route.Pattern != got.pattern ||
			route.Op.ID != got.id || route.Handle != nil {
			t.Errorf("route %d = %s %s %s (Handle nil: %v), want %s %s %s "+
				"with Handle nil", index, route.Method, route.Pattern,
				route.Op.ID, route.Handle == nil, got.method, got.pattern,
				got.id)
		}
	}
	if _, err := Served(http.NotFoundHandler()); err == nil {
		t.Error("Served of another handler = nil error, want an error")
	}
}

func TestAMatchedRequestNamesItsOperationEvenWhenRefused(t *testing.T) {
	handler := mustNew(t, config(), getThing(ok), createThing(ok))
	const header = "Rastrillo-Operation"
	cases := []struct {
		name, method, target, want string
		status                     int
	}{
		{"no token", http.MethodGet, "/api/v1/things/1", "things.get",
			http.StatusUnauthorized},
		{"another method", http.MethodDelete, "/api/v1/things",
			"", http.StatusMethodNotAllowed},
		{"unknown path", http.MethodGet, "/api/v1/nothing", "",
			http.StatusNotFound},
	}
	for _, tc := range cases {
		recorder := serve(handler, tc.method, tc.target, "", "")
		if recorder.Code != tc.status {
			t.Errorf("%s: status %d, want %d", tc.name, recorder.Code,
				tc.status)
		}
		if got := recorder.Header().Get(header); got != tc.want {
			t.Errorf("%s: %s = %q, want %q", tc.name, header, got, tc.want)
		}
	}
}
