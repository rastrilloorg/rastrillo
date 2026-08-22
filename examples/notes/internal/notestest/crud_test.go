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
