package notestest

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// getNoRedirect issues a GET without following redirects — postForm's
// sibling for GET requests, needed here because a test wants to
// observe a status page's 303 to /exports/{id} directly instead of
// silently landing on the export past it.
func (cl *client) getNoRedirect(path string) *http.Response {
	cl.t.Helper()
	req, err := http.NewRequest(http.MethodGet, cl.ts.URL+path, nil)
	if err != nil {
		cl.t.Fatalf("NewRequest GET %s: %v", path, err)
	}
	resp, err := cl.c.Do(req)
	if err != nil {
		cl.t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

// pollJobDone polls GET /jobs/{id} without following redirects until
// the status page 303s to the job's Location (the job finished with
// somewhere to go) or the deadline passes — the same observation a
// browser's own polling makes, driven from a test instead of a shim.
func pollJobDone(t *testing.T, cl *client, jobPath string) *http.Response {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp := cl.getNoRedirect(jobPath)
		if resp.StatusCode == http.StatusSeeOther {
			return resp
		}
		resp.Body.Close()
		if time.Now().After(deadline) {
			t.Fatalf("job at %s did not finish within 10s", jobPath)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// exportTitles seeds the two tests that have to observe an export
// mid-flight. The export handler sleeps 300ms per note purely so a
// demo's status page is visible, which makes note count the length of
// the running window: five notes buy ~1.5s, and the assertions here
// have to land inside it.
var exportTitles = []string{
	"First note", "Second note", "Third note", "Fourth note", "Fifth note",
}

// TestExportRoundTrip drives the whole background-export flow through
// the real forms: the button's POST starts the job and 303s to its
// status page (which polls-ready markup while running), the status
// page itself 303s to the finished export once the job is done, and
// the export serves markdown containing every note.
func TestExportRoundTrip(t *testing.T) {
	ts := newApp(t)
	cl := newClient(t, ts)
	cl.signup("alice@example.com", "hunter2222").Body.Close()
	// Five notes, not two: the export sleeps 300ms per note, so this is
	// a ~1.5s running window. The status page has to still be running
	// when the assertions below read it, and a loaded runner eats a
	// 600ms window whole.
	for _, title := range exportTitles {
		cl.postForm("/notes", url.Values{"title": {title}, "body": {"Body of " + title}}).Body.Close()
	}

	start := cl.postForm("/export", url.Values{})
	if start.StatusCode != http.StatusSeeOther {
		t.Fatalf("export start status = %d, want 303", start.StatusCode)
	}
	jobPath := start.Header.Get("Location")
	start.Body.Close()
	if !strings.HasPrefix(jobPath, "/jobs/") {
		t.Fatalf("export redirect = %q, want /jobs/{id}", jobPath)
	}

	statusResp := cl.getNoRedirect(jobPath)
	statusBody := body(t, statusResp)
	if statusResp.StatusCode != http.StatusOK {
		t.Fatalf("status page = %d, want 200; body=%s", statusResp.StatusCode, statusBody)
	}
	if !strings.Contains(statusBody, "data-poll") {
		t.Fatalf("status page missing data-poll; body=%s", statusBody)
	}
	if !strings.Contains(statusBody, `<meta http-equiv="refresh"`) {
		t.Fatalf("status page missing noscript meta refresh; body=%s", statusBody)
	}

	done := pollJobDone(t, cl, jobPath)
	exportPath := done.Header.Get("Location")
	done.Body.Close()
	if !strings.HasPrefix(exportPath, "/exports/") {
		t.Fatalf("finished job redirect = %q, want /exports/{id}", exportPath)
	}

	exp := cl.get(exportPath)
	defer exp.Body.Close()
	if exp.StatusCode != http.StatusOK {
		t.Fatalf("export status = %d, want 200", exp.StatusCode)
	}
	if ct := exp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Fatalf("export Content-Type = %q, want text/markdown", ct)
	}
	expBody := body(t, exp)
	for _, title := range exportTitles {
		if !strings.Contains(expBody, title) {
			t.Fatalf("export body missing note title %q; body=%s", title, expBody)
		}
	}
}

// TestExportFragmentSignalsDone: once the job is done, the polled
// fragment endpoint the shim hits answers 204 with Rastrillo-Location
// instead of rendering — the signal that stops the poll and navigates.
func TestExportFragmentSignalsDone(t *testing.T) {
	ts := newApp(t)
	cl := newClient(t, ts)
	cl.signup("bella@example.com", "hunter2222").Body.Close()
	cl.postForm("/notes", url.Values{"title": {"Only note"}, "body": {"Body"}}).Body.Close()

	start := cl.postForm("/export", url.Values{})
	jobPath := start.Header.Get("Location")
	start.Body.Close()

	done := pollJobDone(t, cl, jobPath)
	exportPath := done.Header.Get("Location")
	done.Body.Close()

	frag := cl.get(jobPath + "/fragment")
	defer frag.Body.Close()
	if frag.StatusCode != http.StatusNoContent {
		t.Fatalf("fragment status = %d, want 204", frag.StatusCode)
	}
	if loc := frag.Header.Get("Rastrillo-Location"); loc != exportPath {
		t.Fatalf("Rastrillo-Location = %q, want %q", loc, exportPath)
	}
}

// TestExportFragmentWhileRunning: polling the fragment endpoint before
// the job finishes renders the "job-status" partial (200, data-poll
// present) instead of the 204-signals-done branch — the other half of
// Fragment's two outcomes. Five notes' 300ms-apiece simulated pace
// gives this a ~1.5s window to observe before the job completes.
func TestExportFragmentWhileRunning(t *testing.T) {
	ts := newApp(t)
	cl := newClient(t, ts)
	cl.signup("cara@example.com", "hunter2222").Body.Close()
	for _, title := range exportTitles {
		cl.postForm("/notes", url.Values{"title": {title}, "body": {"Body of " + title}}).Body.Close()
	}

	start := cl.postForm("/export", url.Values{})
	jobPath := start.Header.Get("Location")
	start.Body.Close()

	frag := cl.get(jobPath + "/fragment")
	defer frag.Body.Close()
	fragBody := body(t, frag)
	if frag.StatusCode != http.StatusOK {
		t.Fatalf("fragment status = %d, want 200; body=%s", frag.StatusCode, fragBody)
	}
	if !strings.Contains(fragBody, "data-poll") {
		t.Fatalf("running fragment missing data-poll; body=%s", fragBody)
	}

	pollJobDone(t, cl, jobPath).Body.Close()
}

// TestExportIsolation: Bob probing Alice's finished job or export gets
// the same 404 the notes themselves give — a row that isn't yours is a
// row that doesn't exist, jobs and exports included.
func TestExportIsolation(t *testing.T) {
	ts := newApp(t)
	alice := newClient(t, ts)
	alice.signup("alice2@example.com", "hunter2222").Body.Close()
	alice.postForm("/notes", url.Values{"title": {"Secret"}, "body": {"Shh"}}).Body.Close()

	start := alice.postForm("/export", url.Values{})
	jobPath := start.Header.Get("Location")
	start.Body.Close()

	done := pollJobDone(t, alice, jobPath)
	exportPath := done.Header.Get("Location")
	done.Body.Close()

	bob := newClient(t, ts)
	bob.signup("bob2@example.com", "hunter2222").Body.Close()

	if resp := bob.getNoRedirect(jobPath); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Bob GET %s = %d, want 404", jobPath, resp.StatusCode)
	} else {
		resp.Body.Close()
	}
	if resp := bob.get(exportPath); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Bob GET %s = %d, want 404", exportPath, resp.StatusCode)
	} else {
		resp.Body.Close()
	}
	if resp := bob.getNoRedirect(jobPath + "/fragment"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Bob GET %s = %d, want 404", jobPath+"/fragment", resp.StatusCode)
	} else {
		resp.Body.Close()
	}
}

// TestExportCapRefusesFifth: the jobs registry caps an owner at four
// Running jobs, and the handler turns the refusal into a flash on the
// notes list instead of a fifth status page. Ten notes' simulated pace
// (300ms apiece) keeps all four exports running for ~3s — the window
// the fifth POST and its assertions have to land in.
func TestExportCapRefusesFifth(t *testing.T) {
	ts := newApp(t)
	cl := newClient(t, ts)
	cl.signup("dora@example.com", "hunter2222").Body.Close()
	for i := 0; i < 10; i++ {
		cl.postForm("/notes", url.Values{"title": {fmt.Sprintf("Note %d", i)}, "body": {"Body"}}).Body.Close()
	}

	var jobPaths []string
	for i := 0; i < 4; i++ {
		start := cl.postForm("/export", url.Values{})
		loc := start.Header.Get("Location")
		start.Body.Close()
		if !strings.HasPrefix(loc, "/jobs/") {
			t.Fatalf("export %d redirect = %q, want /jobs/{id}", i, loc)
		}
		jobPaths = append(jobPaths, loc)
	}

	fifth := cl.postForm("/export", url.Values{})
	loc := fifth.Header.Get("Location")
	fifth.Body.Close()
	if fifth.StatusCode != http.StatusSeeOther || loc != "/" {
		t.Fatalf("fifth export = %d -> %q, want 303 -> /", fifth.StatusCode, loc)
	}
	home := cl.get("/")
	homeBody := body(t, home)
	if !strings.Contains(homeBody, "You already have exports running") {
		t.Fatalf("refusal flash missing from /; body=%s", homeBody)
	}

	// Drain the four running exports so the test does not end with
	// goroutines still writing into a closing app.
	for _, jobPath := range jobPaths {
		pollJobDone(t, cl, jobPath).Body.Close()
	}
}

// TestShimServed: the fragment shim is outside the Require group (a
// signed-out visitor never sees it run, but the file itself is public,
// same as any other static asset) and comes back byte-identical to
// ui.ShimJS's data-poll contract.
func TestShimServed(t *testing.T) {
	ts := newApp(t)
	resp, err := http.Get(ts.URL + "/static/rastrillo.js")
	if err != nil {
		t.Fatalf("GET /static/rastrillo.js: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	b := body(t, resp)
	if !strings.Contains(b, "data-poll") {
		t.Fatalf("shim missing data-poll; body=%s", b)
	}
}
