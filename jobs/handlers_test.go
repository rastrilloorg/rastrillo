package jobs

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/carlosframework/rastrillo/sessions"
)

func testHandlers(t *testing.T, j *Jobs) *Handlers {
	t.Helper()
	h, err := NewHandlers(Config{
		Jobs: j,
		Render: func(w http.ResponseWriter, r *http.Request, d PageData) {
			w.Write([]byte("PAGE " + string(d.Job.Status) + " " + d.FragmentPath))
		},
		RenderFragment: func(w http.ResponseWriter, r *http.Request, d PageData) {
			w.Write([]byte("FRAG " + string(d.Job.Status)))
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func get(h http.HandlerFunc, id, subject string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/jobs/"+id, nil)
	req.SetPathValue("id", id)
	if subject != "" {
		req = sessions.WithSession(req, sessions.Session{Subject: subject})
	}
	w := httptest.NewRecorder()
	h(w, req)
	return w
}

func startBlocked(j *Jobs, owner, location string) (Job, chan struct{}) {
	release := make(chan struct{})
	job := j.Start(owner, "Export notes", location, func(ctx context.Context, progress func(string)) error {
		<-release
		return nil
	})
	return job, release
}

func TestStatusPageRunning(t *testing.T) {
	j := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := testHandlers(t, j)
	job, release := startBlocked(j, "alice", "/exports/x1")
	defer close(release)

	w := get(h.StatusPage, job.ID, "alice")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "PAGE running") {
		t.Fatalf("body should contain 'PAGE running', got: %s", body)
	}
	if !strings.Contains(body, "/jobs/"+job.ID+"/fragment") {
		t.Fatalf("body should contain fragment path '/jobs/%s/fragment', got: %s", job.ID, body)
	}
}

func TestStatusPageDoneRedirects(t *testing.T) {
	j := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := testHandlers(t, j)
	job, release := startBlocked(j, "alice", "/exports/x1")
	close(release)
	wait(t, j, job.ID, "alice")

	w := get(h.StatusPage, job.ID, "alice")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/exports/x1" {
		t.Fatalf("expected Location: /exports/x1, got: %s", loc)
	}
}

func TestStatusPageDoneWithoutLocationRenders(t *testing.T) {
	j := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := testHandlers(t, j)
	job, release := startBlocked(j, "alice", "")
	close(release)
	wait(t, j, job.ID, "alice")

	w := get(h.StatusPage, job.ID, "alice")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "PAGE done") {
		t.Fatalf("body should contain 'PAGE done', got: %s", body)
	}
}

func TestStatusPageFailedRenders(t *testing.T) {
	j := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := testHandlers(t, j)
	job := j.Start("alice", "Export notes", "", func(ctx context.Context, progress func(string)) error {
		return errors.New("export failed")
	})
	wait(t, j, job.ID, "alice")

	w := get(h.StatusPage, job.ID, "alice")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "PAGE failed") {
		t.Fatalf("body should contain 'PAGE failed', got: %s", body)
	}
}

func TestFragmentRunning(t *testing.T) {
	j := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := testHandlers(t, j)
	job, release := startBlocked(j, "alice", "/exports/x1")
	defer close(release)

	w := get(h.Fragment, job.ID, "alice")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "FRAG running") {
		t.Fatalf("body should contain 'FRAG running', got: %s", body)
	}
}

func TestFragmentDoneSetsRastrilloLocation(t *testing.T) {
	j := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := testHandlers(t, j)
	job, release := startBlocked(j, "alice", "/exports/x1")
	close(release)
	wait(t, j, job.ID, "alice")

	w := get(h.Fragment, job.ID, "alice")
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	if loc := w.Header().Get("Rastrillo-Location"); loc != "/exports/x1" {
		t.Fatalf("expected Rastrillo-Location: /exports/x1, got: %s", loc)
	}
	if body := w.Body.String(); body != "" {
		t.Fatalf("expected empty body, got: %s", body)
	}
}

func TestForeignJobIs404(t *testing.T) {
	j := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := testHandlers(t, j)
	job, release := startBlocked(j, "alice", "/exports/x1")
	defer close(release)

	// Alice's job fetched with subject "bob"
	w1 := get(h.StatusPage, job.ID, "bob")
	if w1.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w1.Code)
	}

	// Unknown job ID
	w2 := get(h.StatusPage, "no-such-id", "bob")
	if w2.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w2.Code)
	}

	// Bodies should be identical
	body1 := strings.TrimSpace(w1.Body.String())
	body2 := strings.TrimSpace(w2.Body.String())
	if body1 != body2 {
		t.Fatalf("bodies should be identical:\n  foreign job: %q\n  unknown id: %q", body1, body2)
	}
}

func TestSignedOutIs403(t *testing.T) {
	j := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := testHandlers(t, j)
	job, release := startBlocked(j, "alice", "/exports/x1")
	defer close(release)

	// StatusPage without session
	w1 := get(h.StatusPage, job.ID, "")
	if w1.Code != http.StatusForbidden {
		t.Fatalf("StatusPage expected 403, got %d", w1.Code)
	}

	// Fragment without session
	w2 := get(h.Fragment, job.ID, "")
	if w2.Code != http.StatusForbidden {
		t.Fatalf("Fragment expected 403, got %d", w2.Code)
	}
}
