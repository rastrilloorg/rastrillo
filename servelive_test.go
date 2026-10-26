package rastrillo

import (
	"context"
	"database/sql"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/background"
	"amadan.net/rastrillo/rastrillo/live"
)

func newTestHub(t *testing.T) *live.Hub {
	t.Helper()
	hub, err := live.New(live.Config{
		Path:   "/live",
		Topics: map[string]live.Allow{"notes": func(*http.Request, string) bool { return true }},
	})
	if err != nil {
		t.Fatal(err)
	}
	return hub
}

// Shutdown waits for in-flight requests and does not cancel their
// contexts, so an open live stream would hold the drain until its
// lifetime ran out, past Serve's 10 seconds. The server newServer
// builds, the one Serve runs, must end the stream when shutdown starts.
func TestServeShutdownEndsLiveStreamsInsideTheDrain(t *testing.T) {
	hub := newTestHub(t)
	opts := Options{Live: hub}
	mux := http.NewServeMux()
	mux.Handle("/live", hub.Handler())
	srv := newServer(opts, mux)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()

	resp, err := http.Get("http://" + ln.Addr().String() + "/live?boot=" + hub.Boot() + "&t=notes@0")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status %d, want 200", resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("shutdown took %v with a live stream open, want under 2s", d)
	}
	<-served
}

// An app without live must not pay for it: no hub, no registration,
// and Shutdown has nothing extra to run.
func TestNewServerWithoutLiveRegistersNothing(t *testing.T) {
	srv := newServer(Options{}, http.NewServeMux())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// The close func Handler returns is what a test harness runs, and what
// Serve defers. It runs after the drain, so a harness must end streams
// itself before its own Shutdown; the close func still closes the hub,
// and does so BEFORE it waits on background work, because a stream
// held open would otherwise be the last thing keeping that work alive.
func TestHandlerCloseClosesTheHubBeforeBackground(t *testing.T) {
	hub := newTestHub(t)
	ts := httptest.NewServer(hub.Handler())
	defer ts.Close()

	var g background.Group
	_, closeAll, err := Handler(Options{
		DBPath:     filepath.Join(t.TempDir(), "app.db"),
		Background: &g,
		Live:       hub,
		Router: func(*sql.DB) (*http.ServeMux, error) {
			return http.NewServeMux(), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	started, release := make(chan struct{}), make(chan struct{})
	g.Go(func() {
		close(started)
		<-release
	})
	<-started

	closed := make(chan error, 1)
	go func() { closed <- closeAll() }()

	// Background.Stop is now blocked on the work above. If the hub was
	// closed first, a new stream is already refused.
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(ts.URL + "?boot=" + hub.Boot() + "&t=notes@0")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusServiceUnavailable {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("hub still admits streams (status %d) while Background.Stop is waiting", resp.StatusCode)
		}
		time.Sleep(5 * time.Millisecond)
	}
	select {
	case <-closed:
		t.Fatal("close returned while background work was still running")
	default:
	}
	close(release)
	if err := <-closed; err != nil {
		t.Fatalf("close: %v", err)
	}
}
