package jobs

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/basepath"
	"amadan.net/rastrillo/rastrillo/perf"
	"amadan.net/rastrillo/rastrillo/sessions"
)

var acme = basepath.Config{Origin: "https://example.com", Slug: "acme"}

// jobRoutes mounts the three job routes, signed in as alice, as an app
// mounts them at its root.
func jobRoutes(h *Handlers) *http.ServeMux {
	mux := http.NewServeMux()
	signed := func(f http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			f(w, sessions.WithSession(r, sessions.Session{Subject: "alice"}))
		}
	}
	mux.HandleFunc("GET /jobs/{id}", signed(h.StatusPage))
	mux.HandleFunc("GET /jobs/{id}/fragment", signed(h.Fragment))
	mux.HandleFunc("GET /jobs/{id}/events", signed(h.Events))
	return mux
}

// The polled URLs go in the page body, so they carry the base; the
// redirects to a finished job's result are headers, which the
// middleware prefixes, once.
func TestJobPathsCarryTheBase(t *testing.T) {
	j := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	var page PageData
	h, err := NewHandlers(Config{
		Jobs:           j,
		Render:         func(_ http.ResponseWriter, _ *http.Request, d PageData) { page = d },
		RenderFragment: func(http.ResponseWriter, *http.Request, PageData) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := acme.Handler(jobRoutes(h))
	do := func(target string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, httptest.NewRequest("GET", target, nil))
		return w
	}

	running, release := startBlocked(t, j, "alice", "/orders/7")
	if w := do("/acme/jobs/" + running.ID); w.Code != http.StatusOK {
		t.Fatalf("GET running job: %d", w.Code)
	}
	if want := "/acme/jobs/" + running.ID + "/fragment"; page.FragmentPath != want {
		t.Errorf("FragmentPath = %q, want %q", page.FragmentPath, want)
	}
	if want := "/acme/jobs/" + running.ID + "/events"; page.EventsPath != want {
		t.Errorf("EventsPath = %q, want %q", page.EventsPath, want)
	}

	close(release)
	wait(t, j, running.ID, "alice")
	if w := do("/acme/jobs/" + running.ID); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/acme/orders/7" {
		t.Errorf("GET done job: %d Location %q, want 303 /acme/orders/7", w.Code, w.Header().Get("Location"))
	}
	if w := do("/acme/jobs/" + running.ID + "/fragment"); w.Code != http.StatusNoContent || w.Header().Get("Rastrillo-Location") != "/acme/orders/7" {
		t.Errorf("GET done fragment: %d Rastrillo-Location %q, want 204 /acme/orders/7", w.Code, w.Header().Get("Rastrillo-Location"))
	}
}

// The done event's data is a URL the shim navigates the browser to, a
// body and not a header, so it carries the base; and a stream still
// flushes through both wrapping writers.
func TestJobEventsStreamUnderABasePath(t *testing.T) {
	j := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := testHandlers(t, j)
	fastStream(h)
	job, progressCh, release := progressJob(t, j, "alice", "/orders/7")
	defer close(release)

	var rec perf.Recorder
	mw := perf.Middleware(&rec, perf.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	ts := httptest.NewServer(acme.Handler(mw(jobRoutes(h))))
	t.Cleanup(ts.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", ts.URL+"/acme/jobs/"+job.ID+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "example.com"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	sse := newSSEReader(resp.Body)
	progressCh <- "1 of 2"
	if name, _, ok := sse.nextEvent(t); !ok || name != "update" {
		t.Fatalf("while running: (%q, %v), want an update before the job finishes", name, ok)
	}
	release <- struct{}{}
	if name, data, ok := sse.nextEvent(t); !ok || name != "done" || data != "/acme/orders/7" {
		t.Fatalf("after finish: (%q, %q, %v), want done /acme/orders/7", name, data, ok)
	}
}
