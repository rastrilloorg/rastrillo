package live_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/live"
)

func allow(*http.Request, string) bool { return true }

func newHub(t *testing.T) *live.Hub {
	t.Helper()
	h, err := live.New(live.Config{
		Path:   "/events",
		Topics: map[string]live.Allow{"a": allow, "b": allow, "notes": allow},
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// render runs one request through the middleware and returns what the
// handler's View renders for the topics, after running between().
func render(h *live.Hub, between func(), topics ...string) string {
	var out string
	handler := h.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		between()
		out = string(h.View(r).Attrs(topics...))
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	return out
}

func TestVersionsNeverMoveForOtherKeys(t *testing.T) {
	h := newHub(t)
	h.Notify("notes/1", "notes/1", "notes/1")
	h.Notify("notes/1")
	h.Notify("notes/1")
	got := render(h, func() {}, "notes/2")
	want := `data-live="notes/2@0" data-live-boot="` + h.Boot() + `"`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestAWriteBeforeRequestStartReadsAsCurrent(t *testing.T) {
	h := newHub(t)
	h.Notify("a")
	h.Notify("a")
	got := render(h, func() {}, "a", "b")
	want := `data-live="a@2 b@0" data-live-boot="` + h.Boot() + `"`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestAWriteAfterRequestStartReadsAsOlder(t *testing.T) {
	h := newHub(t)
	got := render(h, func() { h.Notify("a") }, "a")
	want := `data-live="a@0" data-live-boot="` + h.Boot() + `"`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	// The write is not lost: the next request reads it.
	if got := render(h, func() {}, "a"); !strings.Contains(got, `a@1`) {
		t.Fatalf("next request = %s, want a@1", got)
	}
}

func TestViewWithoutMiddlewareIsConservative(t *testing.T) {
	h := newHub(t)
	h.Notify("a")
	r := httptest.NewRequest("GET", "/", nil)
	got := string(h.View(r).Attrs("a"))
	want := `data-live="a@0" data-live-boot="` + h.Boot() + `"`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestAttrsSkipsUndeclaredAndMalformedTopics(t *testing.T) {
	h := newHub(t)
	r := httptest.NewRequest("GET", "/", nil)
	if got := h.View(r).Attrs("nope", `a"b`); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
	got := string(h.View(r).Attrs("nope", "a"))
	if want := `data-live="a@0" data-live-boot="` + h.Boot() + `"`; got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	if got := h.View(r).Attrs(); got != "" {
		t.Fatalf("no topics: got %q", got)
	}
}

func TestAttrsAndHeadAreEmptyWithoutAHub(t *testing.T) {
	var v live.View
	if got := v.Attrs("a"); got != "" {
		t.Errorf("Attrs = %q", got)
	}
	if got := v.Head("x", "y"); got != "" {
		t.Errorf("Head = %q", got)
	}
	var h *live.Hub
	r := httptest.NewRequest("GET", "/", nil)
	vv := h.View(r)
	if vv.Attrs("a") != "" || vv.Head("x", "y") != "" {
		t.Error("a nil hub's View rendered something")
	}
	// A nil hub's middleware passes the request through untouched.
	called := false
	h.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).
		ServeHTTP(httptest.NewRecorder(), r)
	if !called {
		t.Error("nil hub's Middleware did not call next")
	}
}

func TestHeadRendersTheMeta(t *testing.T) {
	h := newHub(t)
	r := httptest.NewRequest("GET", "/", nil)
	got := string(h.View(r).Head("Paused", "Not saved"))
	want := `<meta name="rastrillo-live" content="/events" data-paused="Paused" data-unconfirmed="Not saved">`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestHeadEscapesItsText(t *testing.T) {
	h := newHub(t)
	r := httptest.NewRequest("GET", "/", nil)
	got := string(h.View(r).Head(`a"<b`, "c"))
	if strings.Contains(got, `a"<b`) || strings.Contains(got, "<b") {
		t.Fatalf("unescaped: %s", got)
	}
	if !strings.Contains(got, `data-paused="a&#34;&lt;b"`) {
		t.Fatalf("not escaped as expected: %s", got)
	}
}
