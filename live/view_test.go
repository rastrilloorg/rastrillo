package live_test

import (
	"bytes"
	"log/slog"
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

// Attrs runs on every render of a region, so a typo in a template
// would otherwise write one warning per page view, for as long as the
// app runs.
func TestAttrsWarnsOncePerUndeclaredTopic(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	h := newHub(t)
	r := httptest.NewRequest("GET", "/", nil)
	for i := 0; i < 3; i++ {
		h.View(r).Attrs("nope", "nope/1", "nope/2", "a")
	}
	if n := strings.Count(buf.String(), "topic=nope"); n != 1 {
		t.Errorf("three renders of an undeclared topic logged %d warnings, want 1:\n%s", n, buf.String())
	}
	h.View(r).Attrs("other")
	if !strings.Contains(buf.String(), "topic=other") {
		t.Errorf("a second undeclared topic was not logged:\n%s", buf.String())
	}
	// Per hub: another hub has not warned about it.
	buf.Reset()
	newHub(t).View(r).Attrs("nope")
	if !strings.Contains(buf.String(), "topic=nope") {
		t.Errorf("a new hub did not log the undeclared topic:\n%s", buf.String())
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

func TestHeadPointsAtTheStreamUnderTheBase(t *testing.T) {
	// Under /acme the app mounts the stream at /events, but the browser
	// must ask for /acme/events: a bare /events is the hub's path, or
	// another tenant's, and the page would never go live.
	h := newHub(t)
	r := live.WithBase(httptest.NewRequest("GET", "/notes", nil), "/acme")
	got := string(h.View(r).Head("Paused", "Not saved"))
	want := `<meta name="rastrillo-live" content="/acme/events" data-paused="Paused" data-unconfirmed="Not saved">`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	// Attrs carry no path, so a base changes nothing in them.
	if got, want := string(h.View(r).Attrs("a")), string(h.View(httptest.NewRequest("GET", "/", nil)).Attrs("a")); got != want {
		t.Errorf("Attrs under a base = %s, want %s", got, want)
	}
}

func TestWithAnEmptyBaseHeadIsUnchanged(t *testing.T) {
	h := newHub(t)
	r := httptest.NewRequest("GET", "/notes", nil)
	if got := live.WithBase(r, ""); got != r {
		t.Error("WithBase with no base returned a new request")
	}
	if got := string(h.View(live.WithBase(r, "")).Head("p", "u")); !strings.Contains(got, `content="/events"`) {
		t.Errorf("Head = %s, want content=\"/events\"", got)
	}
}

func TestHeadLeavesAStreamOffTheAppAlone(t *testing.T) {
	// A Path that is not one of the app's own root-absolute paths, such
	// as another origin's stream, would be broken by a prefix.
	for _, path := range []string{"https://events.example/s", "//events.example/s", `/\events.example/s`} {
		h, err := live.New(live.Config{Path: path, Topics: map[string]live.Allow{"a": allow}})
		if err != nil {
			t.Fatal(err)
		}
		r := live.WithBase(httptest.NewRequest("GET", "/", nil), "/acme")
		got := string(h.View(r).Head("p", "u"))
		if want := `content="` + path + `"`; !strings.Contains(got, want) {
			t.Errorf("Path %q: Head = %s, want %s", path, got, want)
		}
	}
}
