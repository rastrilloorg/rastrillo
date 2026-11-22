package route

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/op"
	"github.com/go-chi/chi/v5"
)

var (
	create = op.Descriptor{Operation: rastrillo.Operation{ID: "notes.create"}}
	remove = op.Descriptor{Operation: rastrillo.Operation{ID: "notes.remove"}}
)

// answering is a handler that says which route reached it.
func answering(name string) http.Handler {
	return http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			io.WriteString(response, name)
		})
}

func serve(t *testing.T, handler http.Handler, method, path string) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("%s %s answered %d, want 200", method, path, recorder.Code)
	}
	return recorder.Body.String()
}

func wantRoutes(t *testing.T, table *Table, want []Route) {
	t.Helper()
	got := table.List()
	if len(got) != len(want) {
		t.Fatalf("List() = %v, want %d routes: %v", got, len(want), want)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List() = %#v, want %#v", got, want)
	}
}

// panicking runs register and returns what it panicked with.
func panicking(t *testing.T, register func()) (message any) {
	t.Helper()
	defer func() { message = recover() }()
	register()
	t.Fatalf("did not panic")
	return nil
}

func wantPanic(t *testing.T, register func(), want string) {
	t.Helper()
	got := panicking(t, register)
	if got != want {
		t.Errorf("panicked with %v, want %q", got, want)
	}
}

func TestATableOverChiServesAndRecords(t *testing.T) {
	router := chi.NewRouter()
	table := New(Chi(router))
	table.Handle("POST /notes", answering("create"), Operation(create))

	if got := serve(t, router, "POST", "/notes"); got != "create" {
		t.Errorf("POST /notes reached %q, want create", got)
	}
	wantRoutes(t, table, []Route{
		{Method: "POST", Path: "/notes", Operations: []string{"notes.create"}},
	})
}

func TestATableOverAServeMuxServesAndRecords(t *testing.T) {
	mux := http.NewServeMux()
	table := New(Mux(mux))
	table.Handle("POST /notes", answering("create"), Operation(create))
	table.Handle("GET /style.css", answering("style"),
		PageOnly("the stylesheet"))

	if got := serve(t, mux, "POST", "/notes"); got != "create" {
		t.Errorf("POST /notes reached %q, want create", got)
	}
	if got := serve(t, mux, "GET", "/style.css"); got != "style" {
		t.Errorf("GET /style.css reached %q, want style", got)
	}
	wantRoutes(t, table, []Route{
		{Method: "POST", Path: "/notes", Operations: []string{"notes.create"}},
		{Method: "GET", Path: "/style.css", PageOnly: "the stylesheet"},
	})
}

func TestATableOnASubRouterRecordsTheFullPath(t *testing.T) {
	router := chi.NewRouter()
	table := New(Chi(router))
	table.Handle("POST /notes", answering("create"), Operation(create))
	router.Route("/admin", func(sub chi.Router) {
		admin := table.On(Chi(sub), "/admin")
		admin.HandleFunc("GET /people",
			answering("people").ServeHTTP, PageOnly("the people page"))
		sub.Route("/notes", func(inner chi.Router) {
			admin.On(Chi(inner), "/notes").Handle("DELETE /{id}",
				answering("remove"), Operation(remove))
		})
	})

	if got := serve(t, router, "GET", "/admin/people"); got != "people" {
		t.Errorf("GET /admin/people reached %q, want people", got)
	}
	if got := serve(t, router, "DELETE", "/admin/notes/7"); got != "remove" {
		t.Errorf("DELETE /admin/notes/7 reached %q, want remove", got)
	}
	wantRoutes(t, table, []Route{
		{Method: "POST", Path: "/notes", Operations: []string{"notes.create"}},
		{Method: "GET", Path: "/admin/people", PageOnly: "the people page"},
		{Method: "DELETE", Path: "/admin/notes/{id}",
			Operations: []string{"notes.remove"}},
	})
}

func TestRegisteringAMethodAndPathTwicePanics(t *testing.T) {
	table := New(Mux(http.NewServeMux()))
	table.Handle("POST /notes/{id}", answering("a"), Operation(create))
	table.Handle("DELETE /notes/{id}", answering("b"), Operation(remove))

	wantPanic(t, func() {
		table.Handle("POST /notes/{key}", answering("c"),
			Operation(create))
	}, fmt.Sprintf(msgTwice, "POST", "/notes/{key}"))

	// The check is the whole table's, so a sub-table's full path counts.
	admin := table.On(Mux(http.NewServeMux()), "/admin")
	admin.Handle("GET /people/{id}", answering("d"), PageOnly("a page"))
	wantPanic(t, func() {
		table.Handle("GET /admin/people/{name}", answering("e"),
			PageOnly("a page"))
	}, fmt.Sprintf(msgTwice, "GET", "/admin/people/{name}"))
}

func TestParametersThatMatchDifferentlyAreNotTwice(t *testing.T) {
	mux := New(Mux(http.NewServeMux()))
	mux.Handle("GET /files/{id}", answering("a"), PageOnly("a page"))
	mux.Handle("GET /files/{rest...}", answering("b"), PageOnly("a page"))
	mux.Handle("GET /files/{$}", answering("c"), PageOnly("a page"))

	router := New(Chi(chi.NewRouter()))
	router.Handle("GET /tags/{id:[0-9]+}", answering("d"), PageOnly("a"))
	router.Handle("GET /tags/{name:[a-z]+}", answering("e"), PageOnly("a"))
	wantPanic(t, func() {
		router.Handle("GET /tags/{key:[0-9]+}", answering("f"), PageOnly("a"))
	}, fmt.Sprintf(msgTwice, "GET", "/tags/{key:[0-9]+}"))
}

func TestAPatternMustNameAKnownMethod(t *testing.T) {
	table := New(Mux(http.NewServeMux()))
	mark := PageOnly("a page")
	wantPanic(t, func() { table.Handle("/notes", answering("a"), mark) },
		fmt.Sprintf(msgNoMethod, "/notes"))
	for _, pattern := range []string{"HEAD /notes", "post /notes", "* /a"} {
		wantPanic(t, func() { table.Handle(pattern, answering("a"), mark) },
			fmt.Sprintf(msgUnknownMethod, pattern))
	}
	if routes := table.List(); len(routes) != 0 {
		t.Errorf("List() = %v after refused patterns, want none", routes)
	}
}

func TestAMarkMustSaySomething(t *testing.T) {
	nameless := op.Descriptor{}
	wantPanic(t, func() { Operation(nameless) }, msgNoOperation)
	wantPanic(t, func() { Operation(create, nameless) }, msgNoOperation)
	wantPanic(t, func() { PageOnly("") }, msgNoReason)
	wantPanic(t, func() { PageOnly(" \t") }, msgNoReason)

	both := Operation(create, remove)
	want := []string{"notes.create", "notes.remove"}
	if got := both.Operations(); !reflect.DeepEqual(got, want) {
		t.Errorf("Operations() = %v, want %v", got, want)
	}
	if got := PageOnly("the stylesheet").Reason(); got != "the stylesheet" {
		t.Errorf("Reason() = %q, want the stylesheet", got)
	}
}

func TestAZeroMarkIsRecordedForTheParityCheck(t *testing.T) {
	table := New(Mux(http.NewServeMux()))
	table.Handle("POST /notes", answering("create"), Mark{})
	wantRoutes(t, table, []Route{{Method: "POST", Path: "/notes"}})
}

func TestListReturnsACopy(t *testing.T) {
	table := New(Mux(http.NewServeMux()))
	mark := Operation(create)
	table.Handle("POST /notes", answering("create"), mark)

	listed := table.List()
	if len(listed) != 1 {
		t.Fatalf("List() = %v, want 1 route", listed)
	}
	listed[0].Path = "/elsewhere"
	listed[0].Operations[0] = "notes.remove"
	mark.Operations()[0] = "notes.remove"

	wantRoutes(t, table, []Route{
		{Method: "POST", Path: "/notes", Operations: []string{"notes.create"}},
	})
	if got := mark.Operations(); !reflect.DeepEqual(got,
		[]string{"notes.create"}) {
		t.Errorf("Operations() = %v after editing a copy", got)
	}
}
