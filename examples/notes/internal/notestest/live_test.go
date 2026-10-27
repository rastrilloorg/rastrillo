package notestest

import (
	"bufio"
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

// liveAttrs is the notes list's region attributes: the owner's key,
// that topic's version and the hub's boot ID. The tests read the key
// off the page because the user's id is not otherwise visible to a
// client, which is also how live.js learns what to stream.
var liveAttrs = regexp.MustCompile(`id="notes-list" data-live="notes/([0-9]+)@([0-9]+)" data-live-boot="([0-9a-f]{12})"`)

// listToken returns the signed-in user's key and the boot ID from the
// list page.
func listToken(t *testing.T, cl *client) (key, boot string) {
	t.Helper()
	b := body(t, cl.get("/"))
	m := liveAttrs.FindStringSubmatch(b)
	if m == nil {
		t.Fatalf("list page has no live notes-list region; body=%s", b)
	}
	return m[1], m[3]
}

// send makes a request with live.js's headers, following redirects
// when follow is set, as fetch does, and keeping the headers across
// the redirect, as fetch does too.
func (cl *client) send(method, path string, vals url.Values, headers map[string]string, follow bool) *http.Response {
	cl.t.Helper()
	var req *http.Request
	var err error
	if method == http.MethodPost {
		req, err = http.NewRequest(method, cl.base+path, strings.NewReader(vals.Encode()))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Origin", cl.origin)
		}
	} else {
		req, err = http.NewRequest(method, cl.base+path, nil)
	}
	if err != nil {
		cl.t.Fatalf("NewRequest %s %s: %v", method, path, err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	c := cl.c
	if follow {
		c = &http.Client{Jar: cl.c.Jar}
	}
	resp, err := c.Do(req)
	if err != nil {
		cl.t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

// createNote posts a note without following the redirect and returns
// the note's path.
func createNote(t *testing.T, cl *client, title string) string {
	t.Helper()
	resp := cl.postForm("/notes", url.Values{"title": {title}, "body": {"Body of " + title}})
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create %q = %d, want 303", title, resp.StatusCode)
	}
	return resp.Header.Get("Location")
}

func TestTheListRegionRendersAloneOnAFragmentRequest(t *testing.T) {
	ts := newApp(t)
	cl := newClient(t, ts)
	cl.signup("lena@example.com", "hunter2222").Body.Close()
	createNote(t, cl, "Plant seeds")

	resp := cl.send(http.MethodGet, "/", nil, map[string]string{"Rastrillo-Fragment": "notes-list"}, false)
	b := body(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, b)
	}
	if !strings.Contains(b, `id="notes-list"`) || !strings.Contains(b, "Plant seeds") {
		t.Fatalf("fragment is missing the list; body=%s", b)
	}
	if strings.Contains(b, "<html") || strings.Contains(b, `id="notice"`) {
		t.Fatalf("fragment carries more than the list region; body=%s", b)
	}
	if got := resp.Header.Get("Rastrillo-Regions"); got != "notes-list" {
		t.Fatalf("Rastrillo-Regions = %q, want notes-list", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := strings.Join(resp.Header.Values("Vary"), ","); !strings.Contains(got, "Rastrillo-Fragment") {
		t.Fatalf("Vary = %q, want it to name Rastrillo-Fragment", got)
	}
}

// TestAStreamFetchDoesNotTakeTheFlash: the stream's refetch asks for
// the list alone, so it must leave the flash for the render that shows
// the notice. If it took it, the cookie would clear and the message
// would never be seen.
func TestAStreamFetchDoesNotTakeTheFlash(t *testing.T) {
	ts := newApp(t)
	cl := newClient(t, ts)
	cl.signup("milo@example.com", "hunter2222").Body.Close()
	path := createNote(t, cl, "Throw away")
	cl.get(path).Body.Close() // spend "Note created."

	del := cl.postForm(path+"/delete", url.Values{})
	del.Body.Close()
	if del.StatusCode != http.StatusSeeOther {
		t.Fatalf("delete = %d, want 303", del.StatusCode)
	}

	frag := cl.send(http.MethodGet, "/", nil, map[string]string{"Rastrillo-Fragment": "notes-list"}, false)
	fb := body(t, frag)
	for _, c := range frag.Header.Values("Set-Cookie") {
		if strings.HasPrefix(c, "rastrillo_flash=") {
			t.Fatalf("a list-only fetch touched the flash cookie: %q", c)
		}
	}
	if strings.Contains(fb, "Note deleted.") {
		t.Fatalf("a list-only fetch rendered the notice; body=%s", fb)
	}

	if full := body(t, cl.get("/")); strings.Count(full, "Note deleted.") != 1 {
		t.Fatalf("the full page after a list fetch lost the notice; body=%s", full)
	}
}

// TestDeletingFromTheListSwapsIt is the row's delete form as live.js
// sends it: the POST names the list and the notice, the 303 to / keeps
// the headers, and one response carries both regions.
func TestDeletingFromTheListSwapsIt(t *testing.T) {
	ts := newApp(t)
	cl := newClient(t, ts)
	cl.signup("nina@example.com", "hunter2222").Body.Close()
	keep := createNote(t, cl, "Keep this")
	gone := createNote(t, cl, "Drop this")

	list := body(t, cl.get("/"))
	for _, p := range []string{keep, gone} {
		if !strings.Contains(list, `action="`+p+`/delete"`) || !strings.Contains(list, "data-live-form") {
			t.Fatalf("list has no swapping delete form for %s; body=%s", p, list)
		}
	}

	resp := cl.send(http.MethodPost, gone+"/delete", url.Values{}, map[string]string{
		"Rastrillo-Fragment": "notes-list notice",
		"Rastrillo-Page":     "/",
	}, true)
	b := body(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, b)
	}
	if got := resp.Header.Get("Rastrillo-Regions"); got != "notes-list notice" {
		t.Fatalf("Rastrillo-Regions = %q, want %q", got, "notes-list notice")
	}
	if strings.Contains(b, "Drop this") || !strings.Contains(b, "Keep this") {
		t.Fatalf("the swapped list is wrong; body=%s", b)
	}
	if !strings.Contains(b, `id="notice"`) || !strings.Contains(b, "Note deleted.") {
		t.Fatalf("the swap is missing its notice; body=%s", b)
	}
	if strings.Contains(b, "<html") {
		t.Fatalf("the swap is a whole page; body=%s", b)
	}

	if after := body(t, cl.get("/")); strings.Contains(after, "Note deleted.") {
		t.Fatalf("the notice showed twice: the swap did not take the flash; body=%s", after)
	}
}

// TestAFormWhoseRedirectGoesElsewhereNavigatesWithItsNotice: create
// redirects to the new note, not back to the list, so the fetch is
// told to navigate, and the flash waits for the page it navigates to.
func TestAFormWhoseRedirectGoesElsewhereNavigatesWithItsNotice(t *testing.T) {
	ts := newApp(t)
	cl := newClient(t, ts)
	cl.signup("oscar@example.com", "hunter2222").Body.Close()

	resp := cl.send(http.MethodPost, "/notes", url.Values{"title": {"Elsewhere"}, "body": {"b"}}, map[string]string{
		"Rastrillo-Fragment": "notes-list notice",
		"Rastrillo-Page":     "/",
	}, true)
	b := body(t, resp)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", resp.StatusCode, b)
	}
	loc := resp.Header.Get("Rastrillo-Location")
	if !strings.HasPrefix(loc, "/notes/") {
		t.Fatalf("Rastrillo-Location = %q, want /notes/{id}", loc)
	}
	for _, c := range resp.Header.Values("Set-Cookie") {
		if strings.HasPrefix(c, "rastrillo_flash=") {
			t.Fatalf("the navigate answer touched the flash cookie: %q", c)
		}
	}

	page := body(t, cl.get(loc))
	if !strings.Contains(page, "Elsewhere") || strings.Count(page, "Note created.") != 1 {
		t.Fatalf("the page navigated to lost its notice; body=%s", page)
	}
}

func TestAnotherOwnersNotesStreamIsRefused(t *testing.T) {
	ts := newApp(t)
	alice := newClient(t, ts)
	alice.signup("alice4@example.com", "hunter2222").Body.Close()
	bob := newClient(t, ts)
	bob.signup("bob4@example.com", "hunter2222").Body.Close()
	bobKey, boot := listToken(t, bob)
	aliceKey, _ := listToken(t, alice)
	if aliceKey == bobKey {
		t.Fatalf("both users have key %s", aliceKey)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp := alice.getStream(ctx, "/live?boot="+boot+"&t=notes/"+bobKey+"@0")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Alice streaming Bob's notes = %d, want 403", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct == "text/event-stream" {
		t.Fatal("a refusal must not open a stream")
	}
}

// TestANoteAddedElsewhereReachesTheStream: the same person in another
// tab (here another client signed in as them) adds a note, and the
// stream this page holds says the list moved.
func TestANoteAddedElsewhereReachesTheStream(t *testing.T) {
	ts := newApp(t)
	tab1 := newClient(t, ts)
	tab1.signup("pia@example.com", "hunter2222").Body.Close()
	key, boot := listToken(t, tab1)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	resp := tab1.getStream(ctx, "/live?boot="+boot+"&t=notes/"+key+"@0")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d, want 200", resp.StatusCode)
	}

	tab2 := newClient(t, ts)
	tab2.signin("pia@example.com", "hunter2222").Body.Close()
	createNote(t, tab2, "From the other tab")

	want := "notes/" + key + "@1"
	var event string
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: ") && event == "change":
			if got := strings.TrimPrefix(line, "data: "); got != want {
				t.Fatalf("change data = %q, want %q", got, want)
			}
			return
		}
	}
	t.Fatalf("stream ended without a change event: %v", sc.Err())
}

// TestARefusedFormKeepsItsStatusWhenSwapped: a 422 answered as regions
// still says 422, and still names what it rendered. Writing the status
// before the regions would have committed the headers without
// Rastrillo-Regions.
func TestARefusedFormKeepsItsStatusWhenSwapped(t *testing.T) {
	ts := newApp(t)
	cl := newClient(t, ts)
	cl.signup("quinn@example.com", "hunter2222").Body.Close()

	resp := cl.send(http.MethodPost, "/notes", url.Values{"title": {""}, "body": {"b"}}, map[string]string{
		"Rastrillo-Fragment": "notice",
		"Rastrillo-Page":     "/notes/new",
	}, false)
	b := body(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", resp.StatusCode, b)
	}
	if got := resp.Header.Get("Rastrillo-Regions"); got != "notice" {
		t.Fatalf("Rastrillo-Regions = %q, want notice", got)
	}
	if !strings.HasPrefix(b, `<div id="notice"`) {
		t.Fatalf("body is not the notice region; body=%s", b)
	}
}
