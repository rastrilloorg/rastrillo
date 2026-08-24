package carlos

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// controlSocket stands in for the agent's control listener: a real unix
// socket, served by h, with both environment variables set the way a
// spawned instance sees them. The directory is os.MkdirTemp rather than
// t.TempDir because a socket path is capped near 108 bytes and t.TempDir
// spells the (long) test name into the path.
func controlSocket(t *testing.T, h http.Handler) {
	t.Helper()
	dir, err := os.MkdirTemp("", "carlosctl")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	path := filepath.Join(dir, "control.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen on %s: %v", path, err)
	}
	srv := &httptest.Server{Listener: l, Config: &http.Server{Handler: h}}
	srv.Start()
	t.Cleanup(srv.Close)

	t.Setenv(socketEnv, path)
	t.Setenv(tokenEnv, "sekrit")
}

// capture records the one request the helper made.
type capture struct {
	method string
	path   string
	auth   string
	body   map[string]string
}

func recorder(t *testing.T, status int, got *capture) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.path, got.auth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		got.body = map[string]string{}
		json.NewDecoder(r.Body).Decode(&got.body)
		w.WriteHeader(status)
		if status >= 400 {
			w.Write([]byte("the agent's complaint\n"))
		}
	})
}

func TestScheduleAt(t *testing.T) {
	at := time.Date(2026, 9, 1, 8, 0, 0, 0, time.FixedZone("IST", 3600))

	t.Run("PUTs the timer over the control socket", func(t *testing.T) {
		var got capture
		controlSocket(t, recorder(t, http.StatusOK, &got))
		if err := ScheduleAt(context.Background(), "remind", at, "/jobs/remind"); err != nil {
			t.Fatalf("ScheduleAt: %v", err)
		}
		if got.method != http.MethodPut {
			t.Errorf("method = %s, want PUT", got.method)
		}
		if got.path != "/schedule/remind" {
			t.Errorf("path = %s, want /schedule/remind", got.path)
		}
		if got.auth != "Bearer sekrit" {
			t.Errorf("Authorization = %q, want the instance token", got.auth)
		}
		// UTC and RFC3339: the agent parses exactly that, and an app's
		// time.Time is as likely as not to carry a local zone.
		if got.body["at"] != "2026-09-01T07:00:00Z" {
			t.Errorf("at = %q, want 2026-09-01T07:00:00Z", got.body["at"])
		}
		if got.body["path"] != "/jobs/remind" {
			t.Errorf("path in body = %q, want /jobs/remind", got.body["path"])
		}
	})

	t.Run("maps the platform's refusals", func(t *testing.T) {
		for _, tc := range []struct {
			status int
			want   error
		}{
			{http.StatusConflict, ErrDeclaredSchedule},
			{http.StatusTooManyRequests, ErrTooManyTimers},
			{http.StatusUnauthorized, ErrUnauthorized},
		} {
			var got capture
			controlSocket(t, recorder(t, tc.status, &got))
			err := ScheduleAt(context.Background(), "remind", at, "/jobs/remind")
			if !errors.Is(err, tc.want) {
				t.Errorf("%d gave %v, want %v", tc.status, err, tc.want)
			}
		}
	})

	t.Run("carries the agent's reason on an unmapped status", func(t *testing.T) {
		// A 400 is the only description of what was wrong with the at
		// or the path; a bare status code would strand the caller.
		var got capture
		controlSocket(t, recorder(t, http.StatusBadRequest, &got))
		err := ScheduleAt(context.Background(), "remind", at, "/jobs/remind")
		if err == nil {
			t.Fatal("400 returned no error")
		}
		if want := "the agent's complaint"; !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to carry %q", err, want)
		}
	})

	t.Run("says so when there is no control socket", func(t *testing.T) {
		t.Setenv(socketEnv, "")
		t.Setenv(tokenEnv, "sekrit")
		if err := ScheduleAt(context.Background(), "remind", at, "/jobs/remind"); !errors.Is(err, ErrNotOnCarlos) {
			t.Fatalf("off CARLOS = %v, want ErrNotOnCarlos", err)
		}
	})

	t.Run("says so when there is no token", func(t *testing.T) {
		// A pre-rollout instance that is already running has the socket
		// but not the secret; it is a 401 waiting to happen, so name it
		// without a round trip.
		var got capture
		controlSocket(t, recorder(t, http.StatusOK, &got))
		t.Setenv(tokenEnv, "")
		if err := ScheduleAt(context.Background(), "remind", at, "/jobs/remind"); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("no token = %v, want ErrUnauthorized", err)
		}
	})

	t.Run("refuses a name that would address another path", func(t *testing.T) {
		var got capture
		controlSocket(t, recorder(t, http.StatusOK, &got))
		for _, name := range []string{"../purge", "a/b", "Remind", "", "with space", "-leading"} {
			if err := ScheduleAt(context.Background(), name, at, "/jobs/remind"); err == nil {
				t.Errorf("name %q was accepted", name)
			}
			if got.method != "" {
				t.Fatalf("name %q reached the socket as %s %s", name, got.method, got.path)
			}
		}
	})
}

func TestScheduleCancel(t *testing.T) {
	t.Run("DELETEs the timer", func(t *testing.T) {
		var got capture
		controlSocket(t, recorder(t, http.StatusNoContent, &got))
		if err := ScheduleCancel(context.Background(), "remind"); err != nil {
			t.Fatalf("ScheduleCancel: %v", err)
		}
		if got.method != http.MethodDelete || got.path != "/schedule/remind" {
			t.Errorf("request = %s %s, want DELETE /schedule/remind", got.method, got.path)
		}
		if got.auth != "Bearer sekrit" {
			t.Errorf("Authorization = %q, want the instance token", got.auth)
		}
	})

	t.Run("refuses to cancel a declared schedule", func(t *testing.T) {
		var got capture
		controlSocket(t, recorder(t, http.StatusConflict, &got))
		if err := ScheduleCancel(context.Background(), "sync"); !errors.Is(err, ErrDeclaredSchedule) {
			t.Fatalf("409 gave %v, want ErrDeclaredSchedule", err)
		}
	})
}
