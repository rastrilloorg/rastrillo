package action

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/flash"
	"amadan.net/rastrillo/rastrillo/op"
	"github.com/go-chi/chi/v5"
)

// visitAt serves one GET through a chi router with /things/{id} and
// returns the Visit built inside it, so the test reads path values the
// way an action does: from a request chi has routed.
func visitAt(t *testing.T, path string) *Visit {
	t.Helper()
	var visit *Visit
	router := chi.NewRouter()
	router.Get("/things/{id}", func(
		response http.ResponseWriter, request *http.Request,
	) {
		visit = NewVisit(response, request, &rastrillo.Ctx{})
	})
	request := httptest.NewRequest(http.MethodGet, path, nil)
	router.ServeHTTP(httptest.NewRecorder(), request)
	if visit == nil {
		t.Fatalf("GET %s did not reach the route", path)
	}
	return visit
}

func TestPathIDRefusesWhatIsNotAnID(t *testing.T) {
	for _, raw := range []string{
		"0", "-1", "abc", "99999999999999999999",
	} {
		visit := visitAt(t, "/things/"+raw)
		_, err := visit.PathID("id")
		if !errors.As(err, new(op.NotFound)) {
			t.Errorf("PathID(%q) = %v, want op.NotFound", raw, err)
		}
	}

	id, err := visitAt(t, "/things/42").PathID("id")
	if id != 42 || err != nil {
		t.Errorf("PathID(42) = %d, %v, want 42, nil", id, err)
	}
}

func TestPathValueReadsChiThenTheServeMux(t *testing.T) {
	if got := visitAt(t, "/things/9").PathValue("id"); got != "9" {
		t.Errorf("PathValue(id) under chi = %q, want 9", got)
	}

	var got string
	mux := http.NewServeMux()
	mux.HandleFunc("/things/{id}", func(
		response http.ResponseWriter, request *http.Request,
	) {
		visit := NewVisit(response, request, &rastrillo.Ctx{})
		got = visit.PathValue("id")
	})
	request := httptest.NewRequest(http.MethodGet, "/things/7", nil)
	mux.ServeHTTP(httptest.NewRecorder(), request)
	if got != "7" {
		t.Errorf("PathValue(id) under a ServeMux = %q, want 7", got)
	}
}

func TestFlashAndRedirectSendTheNoticeAndASeeOther(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/things", nil)
	visit := NewVisit(recorder, request, &rastrillo.Ctx{})

	visit.Flash("Saved.")
	visit.Redirect("/things")

	result := recorder.Result()
	if result.StatusCode != http.StatusSeeOther {
		t.Errorf("status = %d, want 303", result.StatusCode)
	}
	if location := result.Header.Get("Location"); location != "/things" {
		t.Errorf("Location = %q, want /things", location)
	}

	next := httptest.NewRequest(http.MethodGet, "/things", nil)
	for _, cookie := range result.Cookies() {
		next.AddCookie(cookie)
	}
	got, ok := flash.Take(httptest.NewRecorder(), next)
	if !ok || got.Kind != "notice" || got.Message != "Saved." {
		t.Errorf("flash.Take = %+v, %v, want notice \"Saved.\"", got, ok)
	}
}

func TestRenderGoesThroughTheAppsRenderSeam(t *testing.T) {
	var gotPage string
	var gotStatus int
	var gotData any
	ctx := &rastrillo.Ctx{
		Render: func(
			_ *rastrillo.Ctx, _ http.ResponseWriter,
			page string, status int, data any,
		) {
			gotPage, gotStatus, gotData = page, status, data
		},
	}
	request := httptest.NewRequest(http.MethodGet, "/things", nil)
	visit := NewVisit(httptest.NewRecorder(), request, ctx)

	visit.Render("x", 422, 7)

	if gotPage != "x" || gotStatus != 422 || gotData != 7 {
		t.Errorf("Render reached the seam with %q, %d, %v, want x, 422, 7",
			gotPage, gotStatus, gotData)
	}
}

func TestAccessorsReturnWhatWasGiven(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/things", nil)
	ctx := &rastrillo.Ctx{}
	visit := NewVisit(recorder, request, ctx)

	if visit.Response() != recorder || visit.Request() != request ||
		visit.Ctx() != ctx || visit.Context() != request.Context() {
		t.Error("an accessor did not return the value NewVisit was given")
	}
}
