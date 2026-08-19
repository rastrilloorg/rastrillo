package rastrillo

import (
	"net/http"
	"testing"
	"time"
)

func TestNewServerDefaultsBoundHeaderReadAndIdle(t *testing.T) {
	srv := newServer(Options{}, http.NewServeMux())

	if srv.ReadHeaderTimeout != defaultReadHeaderTimeout {
		t.Errorf("ReadHeaderTimeout = %v, want %v", srv.ReadHeaderTimeout, defaultReadHeaderTimeout)
	}
	if srv.IdleTimeout != defaultIdleTimeout {
		t.Errorf("IdleTimeout = %v, want %v", srv.IdleTimeout, defaultIdleTimeout)
	}
}

// The whole point of the default: a request that streams for minutes —
// a git pack, a large upload, an SSE feed, a WebSocket — must not be cut
// by the framework. ReadTimeout and WriteTimeout are TOTAL deadlines, not
// idle ones, so leaving them at zero is the only safe default.
func TestNewServerLeavesTotalDeadlinesOffByDefault(t *testing.T) {
	srv := newServer(Options{}, http.NewServeMux())

	if srv.ReadTimeout != 0 {
		t.Errorf("ReadTimeout = %v, want 0 (off): a total deadline would cut legitimate long uploads", srv.ReadTimeout)
	}
	if srv.WriteTimeout != 0 {
		t.Errorf("WriteTimeout = %v, want 0 (off): a total deadline would cut legitimate long responses", srv.WriteTimeout)
	}
}

func TestNewServerHonoursExplicitOptions(t *testing.T) {
	srv := newServer(Options{
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       7 * time.Second,
		ReadTimeout:       11 * time.Second,
		WriteTimeout:      13 * time.Second,
	}, http.NewServeMux())

	for _, tc := range []struct {
		name      string
		got, want time.Duration
	}{
		{"ReadHeaderTimeout", srv.ReadHeaderTimeout, 5 * time.Second},
		{"IdleTimeout", srv.IdleTimeout, 7 * time.Second},
		{"ReadTimeout", srv.ReadTimeout, 11 * time.Second},
		{"WriteTimeout", srv.WriteTimeout, 13 * time.Second},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

func TestNewServerServesTheGivenHandler(t *testing.T) {
	mux := http.NewServeMux()
	srv := newServer(Options{}, mux)
	if srv.Handler != http.Handler(mux) {
		t.Errorf("Handler = %v, want the mux passed in", srv.Handler)
	}
}
