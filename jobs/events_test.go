package jobs

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/sessions"
)

// eventsServer mounts h.Events behind a session-injecting wrapper on a
// real server: a stream needs a live connection, because httptest's
// recorder can neither deliver flushed bytes to a reading client nor
// observe a disconnect. handlerDone, when non-nil, is closed the
// moment the handler returns — the disconnect test's probe.
func eventsServer(t *testing.T, h *Handlers, subject string, handlerDone chan struct{}) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /jobs/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		if subject != "" {
			r = sessions.WithSession(r, sessions.Session{Subject: subject})
		}
		h.Events(w, r)
		if handlerDone != nil {
			close(handlerDone)
		}
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// openStream GETs the events URL bounded by the test's own deadline —
// a stuck stream fails the read with a context error instead of
// hanging the run.
func openStream(t *testing.T, ctx context.Context, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// sseReader parses a stream one unit at a time: an event returns its
// name and data, a heartbeat comment returns ("comment", text), and a
// closed stream returns ok=false.
type sseReader struct{ r *bufio.Reader }

func newSSEReader(body io.Reader) *sseReader {
	return &sseReader{r: bufio.NewReader(body)}
}

func (s *sseReader) next() (name, data string, ok bool) {
	for {
		line, err := s.r.ReadString('\n')
		if err != nil {
			return "", "", false
		}
		line = strings.TrimSuffix(line, "\n")
		switch {
		case strings.HasPrefix(line, ":"):
			return "comment", strings.TrimSpace(strings.TrimPrefix(line, ":")), true
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		case line == "" && name != "":
			return name, data, true
		}
	}
}

// nextEvent skips heartbeats and returns the next real event.
func (s *sseReader) nextEvent(t *testing.T) (string, string, bool) {
	t.Helper()
	for {
		name, data, ok := s.next()
		if !ok || name != "comment" {
			return name, data, ok
		}
	}
}

// fastStream shortens the handler's internal clocks so a test observes
// ticks and heartbeats inside its own deadline instead of real time.
func fastStream(h *Handlers) {
	h.tick = 2 * time.Millisecond
	h.heartbeat = 15 * time.Millisecond
	h.ttl = 30 * time.Second
}

// progressJob starts a job the test steers: each send on the returned
// progress channel becomes a progress update, and closing release ends
// the job successfully.
func progressJob(t *testing.T, j *Jobs, owner, location string) (Job, chan string, chan struct{}) {
	t.Helper()
	progressCh := make(chan string)
	release := make(chan struct{})
	job, err := j.Start(owner, "Export notes", location, func(ctx context.Context, progress func(string)) error {
		for {
			select {
			case p := <-progressCh:
				progress(p)
			case <-release:
				return nil
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	return job, progressCh, release
}

func TestEventsStreamsUpdatesHeartbeatsAndDone(t *testing.T) {
	j := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := testHandlers(t, j)
	fastStream(h)
	job, progressCh, release := progressJob(t, j, "alice", "/exports/x1")
	defer close(release)

	ts := eventsServer(t, h, "alice", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	resp := openStream(t, ctx, ts.URL+"/jobs/"+job.ID+"/events")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cc)
	}

	sse := newSSEReader(resp.Body)

	// A quiet stream heartbeats: nothing changed yet, so the first unit
	// must be a ping comment, not an event.
	name, data, ok := sse.next()
	if !ok || name != "comment" || data != "ping" {
		t.Fatalf("first unit = (%q, %q, %v), want a ping comment", name, data, ok)
	}

	// A progress change becomes an update event whose data is the
	// (advisory) status.
	progressCh <- "1 of 5"
	name, data, ok = sse.nextEvent(t)
	if !ok || name != "update" || data != "running" {
		t.Fatalf("after progress: (%q, %q, %v), want update/running", name, data, ok)
	}

	// The job finishing becomes a done event carrying its Location, and
	// then the stream closes.
	progressCh <- "5 of 5"
	if name, data, ok = sse.nextEvent(t); !ok || name != "update" {
		t.Fatalf("second progress: (%q, %q, %v), want update", name, data, ok)
	}
	release <- struct{}{}
	name, data, ok = sse.nextEvent(t)
	if !ok || name != "done" || data != "/exports/x1" {
		t.Fatalf("after finish: (%q, %q, %v), want done//exports/x1", name, data, ok)
	}
	if name, data, ok = sse.next(); ok {
		t.Fatalf("stream should close after done, read (%q, %q)", name, data)
	}
}

// The events endpoint answers with the same lookup contract as the
// page and the fragment: signed-out is 403, a foreign or unknown id is
// a plain 404, and none of them start a stream.
func TestEventsLookupContract(t *testing.T) {
	j := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := testHandlers(t, j)
	job, release := startBlocked(t, j, "alice", "/exports/x1")
	defer close(release)

	if w := get(h.Events, job.ID, ""); w.Code != http.StatusForbidden {
		t.Fatalf("signed out = %d, want 403", w.Code)
	}
	if w := get(h.Events, job.ID, "bob"); w.Code != http.StatusNotFound {
		t.Fatalf("foreign job = %d, want 404", w.Code)
	}
	if w := get(h.Events, "no-such-id", "alice"); w.Code != http.StatusNotFound {
		t.Fatalf("unknown id = %d, want 404", w.Code)
	}
}

// streamTTL bounds any one connection: when it elapses mid-run the
// stream closes cleanly without pretending the job finished.
func TestEventsTTLClosesStreamWithoutDone(t *testing.T) {
	j := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := testHandlers(t, j)
	fastStream(h)
	h.ttl = 50 * time.Millisecond
	job, release := startBlocked(t, j, "alice", "/exports/x1")
	defer close(release)

	ts := eventsServer(t, h, "alice", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	resp := openStream(t, ctx, ts.URL+"/jobs/"+job.ID+"/events")

	sse := newSSEReader(resp.Body)
	for {
		name, _, ok := sse.next()
		if !ok {
			return // closed, and never claimed done or gone
		}
		if name == "done" || name == "gone" {
			t.Fatalf("TTL close must not send %q for a still-running job", name)
		}
	}
}

// A client that disconnects releases its handler promptly — this is
// what keeps a graceful-shutdown drain safe with streams open.
func TestEventsClientDisconnectEndsHandler(t *testing.T) {
	j := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := testHandlers(t, j)
	fastStream(h)
	job, release := startBlocked(t, j, "alice", "/exports/x1")
	defer close(release)

	handlerDone := make(chan struct{})
	ts := eventsServer(t, h, "alice", handlerDone)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resp := openStream(t, ctx, ts.URL+"/jobs/"+job.ID+"/events")

	// One ping proves the handler is inside its loop before we vanish.
	sse := newSSEReader(resp.Body)
	if name, _, ok := sse.next(); !ok || name != "comment" {
		t.Fatalf("expected a heartbeat before disconnecting, got %q ok=%v", name, ok)
	}
	cancel()
	select {
	case <-handlerDone:
	case <-time.After(10 * time.Second):
		t.Fatal("handler did not return within 10s of the client disconnecting")
	}
}

// A job swept mid-stream (finished long ago, or expired) becomes a
// gone event and a close — the shim's signal to stop entirely.
func TestEventsGoneWhenJobVanishes(t *testing.T) {
	j := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := testHandlers(t, j)
	fastStream(h)
	job, release := startBlocked(t, j, "alice", "/exports/x1")
	defer close(release)

	ts := eventsServer(t, h, "alice", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	resp := openStream(t, ctx, ts.URL+"/jobs/"+job.ID+"/events")

	sse := newSSEReader(resp.Body)
	if name, _, ok := sse.next(); !ok || name != "comment" {
		t.Fatalf("expected a heartbeat before the sweep, got %q ok=%v", name, ok)
	}
	// Remove the job out from under the stream — the deterministic
	// stand-in for the sweeper (same package, same lock).
	j.mu.Lock()
	delete(j.jobs, job.ID)
	j.mu.Unlock()

	name, data, ok := sse.nextEvent(t)
	if !ok || name != "gone" || data != "" {
		t.Fatalf("after sweep: (%q, %q, %v), want gone with empty data", name, data, ok)
	}
	if name, data, ok = sse.next(); ok {
		t.Fatalf("stream should close after gone, read (%q, %q)", name, data)
	}
}

// noFlush hides the recorder's Flusher: a ResponseWriter that cannot
// stream is answered 500 before any stream headers go out.
type noFlush struct{ w http.ResponseWriter }

func (n noFlush) Header() http.Header         { return n.w.Header() }
func (n noFlush) Write(b []byte) (int, error) { return n.w.Write(b) }
func (n noFlush) WriteHeader(code int)        { n.w.WriteHeader(code) }

func TestEventsWithoutFlusherIs500(t *testing.T) {
	j := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := testHandlers(t, j)
	job, release := startBlocked(t, j, "alice", "/exports/x1")
	defer close(release)

	req := httptest.NewRequest("GET", "/jobs/"+job.ID+"/events", nil)
	req.SetPathValue("id", job.ID)
	req = sessions.WithSession(req, sessions.Session{Subject: "alice"})
	w := httptest.NewRecorder()
	h.Events(noFlush{w}, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct == "text/event-stream" {
		t.Fatal("refusal must not carry stream headers")
	}
}

// pageData advertises the events endpoint beside the fragment path, so
// a renderer can hand the partial its PushURL without building URLs of
// its own.
func TestPageDataCarriesEventsPath(t *testing.T) {
	d := pageData(Job{ID: "abc"})
	if d.EventsPath != "/jobs/abc/events" {
		t.Fatalf("EventsPath = %q, want /jobs/abc/events", d.EventsPath)
	}
}
