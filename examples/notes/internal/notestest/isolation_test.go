package notestest

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestTwoUserIsolation is the permanent regression guard for the
// middle layer's one non-negotiable rule: a row that isn't yours is a
// row that doesn't exist. Every probe Bob makes at Alice's note comes
// back 404, never 403, and never touches Alice's data.
func TestTwoUserIsolation(t *testing.T) {
	ts := newApp(t)

	alice := newClient(t, ts)
	alice.signup("alice@example.com", "hunter2222").Body.Close()
	create := alice.postForm("/notes", url.Values{"title": {"Alices secret"}, "body": {"Only Alices"}})
	aliceNotePath := create.Header.Get("Location")
	create.Body.Close()
	if aliceNotePath == "" {
		t.Fatal("no Location header from Alice's create")
	}

	bob := newClient(t, ts)
	bob.signup("bob@example.com", "hunter2222").Body.Close()

	if resp := bob.get(aliceNotePath); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Bob GET %s = %d, want 404", aliceNotePath, resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	if resp := bob.get(aliceNotePath + "/edit"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Bob GET %s/edit = %d, want 404", aliceNotePath, resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	// The force-update probe: Bob POSTs a change straight at Alice's
	// note id.
	if resp := bob.postForm(aliceNotePath, url.Values{"title": {"Hijacked"}, "body": {"Hijacked"}}); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Bob POST %s = %d, want 404", aliceNotePath, resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	if resp := bob.postForm(aliceNotePath+"/delete", url.Values{}); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Bob POST %s/delete = %d, want 404", aliceNotePath, resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	// Alice's note must be entirely untouched by any of Bob's probes.
	aliceView := alice.get(aliceNotePath)
	aliceBody := body(t, aliceView)
	if !strings.Contains(aliceBody, "Alices secret") || !strings.Contains(aliceBody, "Only Alices") {
		t.Fatalf("Alice's note was altered or destroyed; body=%s", aliceBody)
	}
	if strings.Contains(aliceBody, "Hijacked") {
		t.Fatalf("Alice's note shows Bob's hijack attempt; body=%s", aliceBody)
	}

	// Bob's own index must never leak Alice's title.
	bobIndex := bob.get("/")
	bobBody := body(t, bobIndex)
	if strings.Contains(bobBody, "Alices secret") {
		t.Fatalf("Bob's index leaks Alice's note title; body=%s", bobBody)
	}
}
