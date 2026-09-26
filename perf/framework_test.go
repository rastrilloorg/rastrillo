package perf_test

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/perf"
)

// Mounted the way an app mounts it, through Options.Wrap: the framework
// builds the router (slowly, here) and only then applies Wrap, so the
// cold request's startup includes the building.
func TestThroughTheFrameworkStartupIncludesBuildingTheApp(t *testing.T) {
	var c perf.Recorder
	started := time.Now()
	h, closeAll, err := rastrillo.Handler(rastrillo.Options{
		Router: func(*sql.DB) (*http.ServeMux, error) {
			time.Sleep(300 * time.Millisecond) // migrations, templates, a slow boot
			mux := http.NewServeMux()
			mux.HandleFunc("GET /notes/{id}", func(w http.ResponseWriter, r *http.Request) {})
			return mux, nil
		},
		Wrap:   perf.Middleware(&c, perf.Options{Started: started, Logger: slog.New(slog.NewTextHandler(new(bytes.Buffer), nil))}),
		Logger: slog.New(slog.NewTextHandler(new(bytes.Buffer), nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer closeAll()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/notes/7", nil))
	s := c.Snapshot()[0]
	if !s.Cold || s.Startup < 300 {
		t.Fatalf("cold sample %+v: startup should include the 300ms the app took to build", s)
	}
	if s.Screen != "GET /notes/{id}" {
		t.Fatalf("screen = %q, want the app mux's pattern", s.Screen)
	}
}

// The framework's own mux sets Pattern to "/" before Wrap runs. Middleware
// that copies the request between perf and the app mux must not leave
// every route grouped under that inherited "/" in silence.
func TestThroughTheFrameworkALostRouteIsNotHiddenByTheOuterPattern(t *testing.T) {
	var c perf.Recorder
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /notes/{id}", func(w http.ResponseWriter, r *http.Request) {})
	type key struct{}
	h, closeAll, err := rastrillo.Handler(rastrillo.Options{
		Mux: mux,
		Wrap: func(app http.Handler) http.Handler {
			copying := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				app.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), key{}, 1)))
			})
			return perf.Middleware(&c, perf.Options{Logger: log})(copying)
		},
		Logger: log,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer closeAll()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/notes/7", nil))
	if s := c.Snapshot()[0]; s.Screen == "/" {
		t.Fatalf("screen = %q: the framework's outer pattern stood in for the lost route", s.Screen)
	}
	if !strings.Contains(logs.String(), "route pattern never reached") {
		t.Fatalf("no lost-route warning; log:\n%s", logs.String())
	}
}
