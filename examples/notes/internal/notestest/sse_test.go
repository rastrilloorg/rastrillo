package notestest

import (
	"bufio"
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// getStream GETs an SSE endpoint with the client's cookie jar and no
// client timeout (a stream is supposed to stay open); the context is
// the test's own deadline, so a stuck stream fails the read instead of
// hanging the run.
func (cl *client) getStream(ctx context.Context, path string) *http.Response {
	cl.t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cl.ts.URL+path, nil)
	if err != nil {
		cl.t.Fatalf("NewRequest GET %s: %v", path, err)
	}
	resp, err := cl.c.Do(req)
	if err != nil {
		cl.t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

// TestExportEventsStream is the SSE round trip: the events endpoint
// streams at least one update while the export runs and a done event
// carrying the export's location when it finishes, then closes. Five
// notes' 300ms-apiece simulated pace keeps the job running ~1.5s, so
// the 1-second server tick observes progress changing at least once
// before done.
func TestExportEventsStream(t *testing.T) {
	ts := newApp(t)
	cl := newClient(t, ts)
	cl.signup("erin@example.com", "hunter2222").Body.Close()
	for _, title := range exportTitles {
		cl.postForm("/notes", url.Values{"title": {title}, "body": {"Body of " + title}}).Body.Close()
	}

	start := cl.postForm("/export", url.Values{})
	jobPath := start.Header.Get("Location")
	start.Body.Close()
	if !strings.HasPrefix(jobPath, "/jobs/") {
		t.Fatalf("export redirect = %q, want /jobs/{id}", jobPath)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp := cl.getStream(ctx, jobPath+"/events")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("events status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("events Content-Type = %q, want text/event-stream", ct)
	}

	// Walk the stream event by event: updates until done, done's data
	// the finished export's path. The context above bounds every read.
	var sawUpdate bool
	var event, data, doneData string
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		case line == "" && event != "":
			switch event {
			case "update":
				sawUpdate = true
				if data != "running" {
					t.Fatalf("update data = %q, want running", data)
				}
			case "done":
				doneData = data
			default:
				t.Fatalf("unexpected event %q (data %q)", event, data)
			}
			event, data = "", ""
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("stream read: %v", err)
	}
	if !sawUpdate {
		t.Fatal("stream closed without a single update event")
	}
	if !strings.HasPrefix(doneData, "/exports/") {
		t.Fatalf("done data = %q, want the /exports/{id} location", doneData)
	}

	exp := cl.get(doneData)
	defer exp.Body.Close()
	if exp.StatusCode != http.StatusOK {
		t.Fatalf("export at done location = %d, want 200", exp.StatusCode)
	}
}

// TestExportEventsIsolation: Bob probing Alice's events URL gets the
// same 404 as every other jobs endpoint — refused before any stream
// starts, no headers, no events.
func TestExportEventsIsolation(t *testing.T) {
	ts := newApp(t)
	alice := newClient(t, ts)
	alice.signup("alice3@example.com", "hunter2222").Body.Close()
	alice.postForm("/notes", url.Values{"title": {"Secret"}, "body": {"Shh"}}).Body.Close()

	start := alice.postForm("/export", url.Values{})
	jobPath := start.Header.Get("Location")
	start.Body.Close()

	bob := newClient(t, ts)
	bob.signup("bob3@example.com", "hunter2222").Body.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp := bob.getStream(ctx, jobPath+"/events")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Bob GET %s/events = %d, want 404", jobPath, resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct == "text/event-stream" {
		t.Fatal("a refusal must not open a stream")
	}

	pollJobDone(t, alice, jobPath).Body.Close()
}

// TestExportStatusPageOffersPush: the running status page carries
// data-poll-push with the job's events path beside data-poll — the
// attribute the shim upgrades on.
func TestExportStatusPageOffersPush(t *testing.T) {
	ts := newApp(t)
	cl := newClient(t, ts)
	cl.signup("fern@example.com", "hunter2222").Body.Close()
	for _, title := range exportTitles {
		cl.postForm("/notes", url.Values{"title": {title}, "body": {"Body of " + title}}).Body.Close()
	}

	start := cl.postForm("/export", url.Values{})
	jobPath := start.Header.Get("Location")
	start.Body.Close()

	statusResp := cl.getNoRedirect(jobPath)
	statusBody := body(t, statusResp)
	if statusResp.StatusCode != http.StatusOK {
		t.Fatalf("status page = %d, want 200; body=%s", statusResp.StatusCode, statusBody)
	}
	if !strings.Contains(statusBody, `data-poll-push="`+jobPath+`/events"`) {
		t.Fatalf("running status page missing data-poll-push; body=%s", statusBody)
	}

	pollJobDone(t, cl, jobPath).Body.Close()
}
