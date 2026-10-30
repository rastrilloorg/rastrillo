package rastrillo

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/basepath"
	"amadan.net/rastrillo/rastrillo/live"
)

// liveBPServe is a live app behind bpOptions' base path: the stream at
// /live, and a live page at /notes and /notes/{id} that answers the way
// a renderer does (Elsewhere first, then the page with the stream's
// meta).
func liveBPServe(t *testing.T, mode basepath.DirectHost) (http.Handler, *live.Hub) {
	t.Helper()
	hub := newTestHub(t)
	m := http.NewServeMux()
	m.Handle("GET /live", hub.Handler())
	page := func(w http.ResponseWriter, r *http.Request) {
		if live.Elsewhere(w, r) {
			return
		}
		live.NoStore(w)
		io.WriteString(w, string(hub.View(r).Head("Paused", "Not saved")))
	}
	m.HandleFunc("GET /notes", page)
	m.HandleFunc("GET /notes/{id}", page)
	c := &bpCalls{}
	opts := bpOptions(m, c, mode)
	opts.Live = hub
	return bpServe(t, opts), hub
}

func TestALivePageUnderTheBasePointsAtItsStream(t *testing.T) {
	// The app mounts the stream at /live, but a page at /acme/notes that
	// pointed EventSource at /live would ask the hub, or another tenant,
	// and never go live.
	h, _ := liveBPServe(t, basepath.ServeDirect)
	for _, target := range []string{"/acme/notes", "/acme/fr/notes"} {
		w := bpDo(h, "GET", "example.com", target, nil, nil)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `content="/acme/live"`) {
			t.Errorf("GET %s = %d %s, want 200 and the meta's content=\"/acme/live\"", target, w.Code, w.Body.String())
		}
	}
	// The direct host serves at the root, so its stream is at the root.
	w := bpDo(h, "GET", "acme-abc234.bdf.oncarlos.com", "/notes", nil, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `content="/live"`) {
		t.Errorf("direct host GET /notes = %d %s, want 200 and content=\"/live\"", w.Code, w.Body.String())
	}
}

func TestASwappingFormUnderTheBaseSwaps(t *testing.T) {
	// live.js sends Rastrillo-Page as location.pathname, base (and any
	// locale prefix) included. The follow-up GET of the form's own page
	// must be answered as a fragment, not with a navigate.
	h, _ := liveBPServe(t, basepath.RedirectDirect)
	for _, target := range []string{"/acme/notes", "/acme/fr/notes"} {
		w := bpDo(h, "GET", "example.com", target, nil, http.Header{
			"Rastrillo-Page":     {target},
			"Rastrillo-Fragment": {"list"},
		})
		if w.Code != 200 {
			t.Errorf("GET %s from its own page = %d (Rastrillo-Location %q), want 200: every swap would navigate",
				target, w.Code, w.Header().Get("Rastrillo-Location"))
		}
	}
}

func TestALiveNavigateUnderTheBaseEndsUnderTheBaseOnce(t *testing.T) {
	// A form on /acme/notes whose redirect went to /acme/notes/5: the
	// 204 says where, and the middleware adds the base to the app's
	// /notes/5 exactly once.
	h, _ := liveBPServe(t, basepath.RedirectDirect)
	for target, want := range map[string]string{
		"/acme/notes/5":     "/acme/notes/5",
		"/acme/notes/5?x=1": "/acme/notes/5?x=1",
	} {
		w := bpDo(h, "GET", "example.com", target, nil, http.Header{
			"Rastrillo-Page":     {"/acme/notes"},
			"Rastrillo-Fragment": {"list"},
		})
		if got := w.Header().Get("Rastrillo-Location"); w.Code != http.StatusNoContent || got != want {
			t.Errorf("GET %s from /acme/notes = %d, Rastrillo-Location %q; want 204 and %q", target, w.Code, got, want)
		}
	}
}

func TestTheStreamAnswersUnderTheBase(t *testing.T) {
	// A real server, so the stream's flushes go through the base path's
	// rewriting writer to net/http's own: a writer that hid Flush would
	// hold every event until the stream ended.
	h, hub := liveBPServe(t, basepath.RedirectDirect)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	t.Cleanup(hub.Close)

	req, err := http.NewRequest("GET", srv.URL+"/acme/live?boot="+hub.Boot()+"&t=notes@0", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "example.com"
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); resp.StatusCode != 200 || ct != "text/event-stream" {
		t.Fatalf("GET /acme/live = %d %q, want 200 text/event-stream", resp.StatusCode, ct)
	}
	hub.Notify("notes")
	got := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "data: ") {
				got <- sc.Text()
				return
			}
		}
		close(got)
	}()
	select {
	case line := <-got:
		if line != "data: notes@1" {
			t.Errorf("event %q, want data: notes@1", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no change event within 5s under the base")
	}
}
