// bookmarks_test.go proves the declarative half of this app the same
// way isolation_test.go proves the hand-written half: through the
// real HTTP surface, real sign-in, real cookies. manifest/
// bookmarks.toml declares scope = "user", and these tests are the
// two-user proof that the GENERATED store and actions enforce the
// same rule the hand handlers do — a row that isn't yours is a row
// that doesn't exist.
package notestest

import (
	"bytes"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestBookmarksRoundTrip drives the generated screens end to end as
// one signed-in user: empty list, create, show, edit, and the
// confirm-page delete flow — all rendered inside the app's own chrome
// (genlayout.html via GenRender's per-request closure).
func TestBookmarksRoundTrip(t *testing.T) {
	ts := newApp(t)
	alice := newClient(t, ts)
	alice.signup("alice@example.com", "hunter2222").Body.Close()

	// Empty list renders (and carries the shared nav — the closure
	// seam's proof: a generated screen sees this request's signed-in
	// state).
	list := alice.get("/bookmarks")
	listBody := body(t, list)
	if list.StatusCode != http.StatusOK {
		t.Fatalf("GET /bookmarks = %d, want 200", list.StatusCode)
	}
	if !strings.Contains(listBody, "/signout") {
		t.Fatalf("generated list is missing the signed-in chrome; body=%s", listBody)
	}

	// Create. The required check is server-side: a blank Title 400s
	// with the field's own message and the echo of what was typed.
	bad := alice.postForm("/bookmarks", url.Values{"Title": {""}, "Link": {"https://example.com"}})
	badBody := body(t, bad)
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("blank Title = %d, want 400", bad.StatusCode)
	}
	if !strings.Contains(badBody, "Title is required") {
		t.Fatalf("400 re-render is missing the required message; body=%s", badBody)
	}

	create := alice.postForm("/bookmarks", url.Values{
		"Title": {"CARLOS docs"},
		"Link":  {"https://carlosframework.com"},
		"Notes": {"read the platform half"},
	})
	create.Body.Close()
	if create.StatusCode != http.StatusSeeOther {
		t.Fatalf("create = %d, want 303", create.StatusCode)
	}
	loc := create.Header.Get("Location")
	if !strings.HasPrefix(loc, "/bookmarks/") {
		t.Fatalf("Location = %q, want /bookmarks/<id>", loc)
	}

	show := body(t, alice.get(loc))
	if !strings.Contains(show, "CARLOS docs") || !strings.Contains(show, "read the platform half") {
		t.Fatalf("show is missing the created values; body=%s", show)
	}

	// Edit basics, then confirm the change landed.
	save := alice.postForm(loc+"/edit-basics", url.Values{
		"Title": {"CARLOS docs (read)"},
		"Link":  {"https://carlosframework.com"},
	})
	save.Body.Close()
	if save.StatusCode != http.StatusSeeOther {
		t.Fatalf("edit-basics = %d, want 303", save.StatusCode)
	}
	if got := body(t, alice.get(loc)); !strings.Contains(got, "CARLOS docs (read)") {
		t.Fatalf("edit did not land; body=%s", got)
	}

	// Delete: its own confirm URL first (a GET that never mutates),
	// then the POST, then the list no longer shows the row.
	confirm := alice.get(loc + "/delete")
	if confirm.StatusCode != http.StatusOK {
		t.Fatalf("GET %s/delete = %d, want 200", loc, confirm.StatusCode)
	}
	confirm.Body.Close()
	del := alice.postForm(loc+"/delete", url.Values{})
	del.Body.Close()
	if del.StatusCode != http.StatusSeeOther {
		t.Fatalf("delete = %d, want 303", del.StatusCode)
	}
	if got := body(t, alice.get("/bookmarks")); strings.Contains(got, "CARLOS docs") {
		t.Fatalf("deleted bookmark still listed; body=%s", got)
	}
}

// TestBookmarksTwoUserIsolation is isolation_test.go's rule applied
// to the generated half: every probe Bob makes at Alice's bookmark
// answers 404 — the owner-filtered Get turning someone else's id into
// sql.ErrNoRows — and his own list never leaks her title.
func TestBookmarksTwoUserIsolation(t *testing.T) {
	ts := newApp(t)

	alice := newClient(t, ts)
	alice.signup("alice@example.com", "hunter2222").Body.Close()
	create := alice.postForm("/bookmarks", url.Values{"Title": {"Alices reading list"}, "Link": {"https://example.com/alice"}})
	alicePath := create.Header.Get("Location")
	create.Body.Close()
	if alicePath == "" {
		t.Fatal("no Location header from Alice's create")
	}

	bob := newClient(t, ts)
	bob.signup("bob@example.com", "hunter2222").Body.Close()

	for _, probe := range []string{alicePath, alicePath + "/edit", alicePath + "/delete"} {
		if resp := bob.get(probe); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("Bob GET %s = %d, want 404", probe, resp.StatusCode)
		} else {
			resp.Body.Close()
		}
	}
	if resp := bob.postForm(alicePath+"/edit-basics", url.Values{"Title": {"Hijacked"}, "Link": {""}}); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Bob POST %s/edit-basics = %d, want 404", alicePath, resp.StatusCode)
	} else {
		resp.Body.Close()
	}
	if resp := bob.postForm(alicePath+"/delete", url.Values{}); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Bob POST %s/delete = %d, want 404", alicePath, resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	// Alice's bookmark is untouched; Bob's list is empty of her data.
	if got := body(t, alice.get(alicePath)); !strings.Contains(got, "Alices reading list") || strings.Contains(got, "Hijacked") {
		t.Fatalf("Alice's bookmark was altered; body=%s", got)
	}
	if got := body(t, bob.get("/bookmarks")); strings.Contains(got, "Alices reading list") {
		t.Fatalf("Bob's list leaks Alice's bookmark; body=%s", got)
	}
}

// TestBookmarksRequireSignin pins the mounting contract: the
// generated routes sit inside the same Require group as the hand
// routes, so a signed-out page request redirects to /signin rather
// than reaching the generated action's own 403 backstop.
func TestBookmarksRequireSignin(t *testing.T) {
	ts := newApp(t)
	visitor := newClient(t, ts)

	resp, err := visitor.c.Get(ts.URL + "/bookmarks")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("signed-out GET /bookmarks = %d, want 303", resp.StatusCode)
	}
	if to := resp.Header.Get("Location"); !strings.HasPrefix(to, "/signin") {
		t.Fatalf("redirect = %q, want /signin...", to)
	}
}

// TestGenerateCheckIsGreen runs the real `rastrillo generate --check`
// against this app's committed gen/ — the byte-identity proof for the
// declarative half, exactly as examples/tickets runs it (see that
// module's generatecheck_test.go for what --check does and doesn't
// diff).
func TestGenerateCheckIsGreen(t *testing.T) {
	cmd := exec.Command("go", "run", "amadan.net/rastrillo/rastrillo/cmd/rastrillo", "generate", "--check", ".")
	cmd.Dir = notesAppRoot(t)
	cmd.Env = withModMode(os.Environ())

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("rastrillo generate --check .: %v\n%s", err, out.String())
	}
}

// notesAppRoot resolves examples/notes's own absolute path from this
// test file's location, independent of `go test`'s working directory.
func notesAppRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// withModMode strips any inherited GOFLAGS and sets -mod=mod: the
// sqlc tool directive needs it the same way the module's own regen
// commands do.
func withModMode(env []string) []string {
	out := make([]string, 0, len(env)+1)
	for _, e := range env {
		if strings.HasPrefix(e, "GOFLAGS=") {
			continue
		}
		out = append(out, e)
	}
	return append(out, "GOFLAGS=-mod=mod")
}
