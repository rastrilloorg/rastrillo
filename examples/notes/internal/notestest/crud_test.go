package notestest

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestNoteCRUDThroughForms drives the whole lifecycle through the real
// forms: create, see it listed, edit, see the change, delete, gone.
func TestNoteCRUDThroughForms(t *testing.T) {
	ts := newApp(t)
	cl := newClient(t, ts)
	cl.signup("erin@example.com", "hunter2222").Body.Close()

	create := cl.postForm("/notes", url.Values{"title": {"Groceries"}, "body": {"Milk, eggs"}})
	defer create.Body.Close()
	if create.StatusCode != http.StatusSeeOther {
		t.Fatalf("create status = %d, want %d", create.StatusCode, http.StatusSeeOther)
	}
	showPath := create.Header.Get("Location")
	if !strings.HasPrefix(showPath, "/notes/") {
		t.Fatalf("create redirect = %q, want /notes/{id}", showPath)
	}

	idx := cl.get("/")
	defer idx.Body.Close()
	idxBody := body(t, idx)
	if !strings.Contains(idxBody, "Groceries") {
		t.Fatalf("index missing created title; body=%s", idxBody)
	}

	editPath := showPath + "/edit"
	edit := cl.get(editPath)
	defer edit.Body.Close()
	editBody := body(t, edit)
	if !strings.Contains(editBody, "Groceries") || !strings.Contains(editBody, "Milk, eggs") {
		t.Fatalf("edit form missing current values; body=%s", editBody)
	}

	update := cl.postForm(showPath, url.Values{"title": {"Groceries v2"}, "body": {"Milk, eggs, bread"}})
	defer update.Body.Close()
	if update.StatusCode != http.StatusSeeOther {
		t.Fatalf("update status = %d, want %d", update.StatusCode, http.StatusSeeOther)
	}

	show := cl.get(showPath)
	defer show.Body.Close()
	showBody := body(t, show)
	if !strings.Contains(showBody, "Groceries v2") || !strings.Contains(showBody, "Milk, eggs, bread") {
		t.Fatalf("show missing updated values; body=%s", showBody)
	}

	del := cl.postForm(showPath+"/delete", url.Values{})
	defer del.Body.Close()
	if del.StatusCode != http.StatusSeeOther {
		t.Fatalf("delete status = %d, want %d", del.StatusCode, http.StatusSeeOther)
	}

	gone := cl.get(showPath)
	defer gone.Body.Close()
	if gone.StatusCode != http.StatusNotFound {
		t.Fatalf("GET deleted note = %d, want 404", gone.StatusCode)
	}
}

// TestValidationRerendersAt422: a blank title 422s and re-renders the
// form with both the error and the submitted body preserved — no
// retyping.
func TestValidationRerendersAt422(t *testing.T) {
	ts := newApp(t)
	cl := newClient(t, ts)
	cl.signup("frank@example.com", "hunter2222").Body.Close()

	resp := cl.postForm("/notes", url.Values{"title": {""}, "body": {"Remember this body"}})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
	b := body(t, resp)
	if !strings.Contains(b, "Title is required") {
		t.Fatalf("body missing validation error; body=%s", b)
	}
	if !strings.Contains(b, "Remember this body") {
		t.Fatalf("body missing preserved submitted body; body=%s", b)
	}
}

// TestUpdateValidationRerendersAt422: the update path holds the same
// contract create does — blank title 422s, the submitted body survives
// into the re-rendered edit form, and the stored note is untouched.
func TestUpdateValidationRerendersAt422(t *testing.T) {
	ts := newApp(t)
	cl := newClient(t, ts)
	cl.signup("henry@example.com", "hunter2222").Body.Close()

	create := cl.postForm("/notes", url.Values{"title": {"Keep me"}, "body": {"Original"}})
	showPath := create.Header.Get("Location")
	create.Body.Close()

	resp := cl.postForm(showPath, url.Values{"title": {""}, "body": {"Edited but invalid"}})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("update status = %d, want 422", resp.StatusCode)
	}
	b := body(t, resp)
	if !strings.Contains(b, "Title is required") || !strings.Contains(b, "Edited but invalid") {
		t.Fatalf("422 re-render missing error or preserved input; body=%s", b)
	}

	show := cl.get(showPath)
	defer show.Body.Close()
	if sb := body(t, show); !strings.Contains(sb, "Keep me") || !strings.Contains(sb, "Original") {
		t.Fatalf("rejected update mutated the note; body=%s", sb)
	}
}

// TestFlashClearedOn422: a 422 re-render consumes the pending flash
// like any other render. The regression it pins: the clearing
// Set-Cookie used to be added after WriteHeader(422) — silently
// dropped — so the flash showed on the 422 page AND again on the next
// page.
func TestFlashClearedOn422(t *testing.T) {
	ts := newApp(t)
	cl := newClient(t, ts)
	cl.signup("iris@example.com", "hunter2222").Body.Close()

	create := cl.postForm("/notes", url.Values{"title": {"Note"}, "body": {"Body"}})
	showPath := create.Header.Get("Location")
	create.Body.Close()

	// The flash is pending; spend it on a 422 re-render.
	invalid := cl.postForm("/notes", url.Values{"title": {""}, "body": {"nope"}})
	invalidBody := body(t, invalid)
	invalid.Body.Close()
	if strings.Count(invalidBody, "Note created.") != 1 {
		t.Fatalf("422 render flash count = %d, want 1; body=%s", strings.Count(invalidBody, "Note created."), invalidBody)
	}

	after := cl.get(showPath)
	defer after.Body.Close()
	if ab := body(t, after); strings.Contains(ab, "Note created.") {
		t.Fatalf("flash survived the 422 render — its clearing Set-Cookie was dropped; body=%s", ab)
	}
}

// TestFlashShownOnce: the notice set on create appears exactly once,
// on the very next render, and never again after.
func TestFlashShownOnce(t *testing.T) {
	ts := newApp(t)
	cl := newClient(t, ts)
	cl.signup("grace@example.com", "hunter2222").Body.Close()

	create := cl.postForm("/notes", url.Values{"title": {"Note"}, "body": {"Body"}})
	showPath := create.Header.Get("Location")
	create.Body.Close()

	first := cl.get(showPath)
	firstBody := body(t, first)
	if strings.Count(firstBody, "Note created.") != 1 {
		t.Fatalf("first render flash count = %d, want 1; body=%s", strings.Count(firstBody, "Note created."), firstBody)
	}

	second := cl.get(showPath)
	secondBody := body(t, second)
	if strings.Count(secondBody, "Note created.") != 0 {
		t.Fatalf("second render flash count = %d, want 0; body=%s", strings.Count(secondBody, "Note created."), secondBody)
	}
}
