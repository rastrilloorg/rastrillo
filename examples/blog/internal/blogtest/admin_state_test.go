package blogtest

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"blog/internal/blog"
)

func TestPublishMakesThePostPublic(t *testing.T) {
	app, db := newApp(t)
	id := seed(t, db, "Release notes", "Body.", false)

	rec := post(t, app, fmt.Sprintf("/admin/posts/%d/publish", id), nil)
	wantStatus(t, rec, http.StatusSeeOther)
	if got, want := rec.Header().Get("Location"), fmt.Sprintf("/admin/posts/%d/edit", id); got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}

	p, err := blog.Get(db, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !p.Published {
		t.Errorf("post is still a draft")
	}
	wantContains(t, get(t, app, "/").Body.String(), "Release notes")
	wantStatus(t, get(t, app, fmt.Sprintf("/posts/%d", id)), http.StatusOK)
}

func TestUnpublishTakesThePostBackOffTheBlog(t *testing.T) {
	app, db := newApp(t)
	id := seed(t, db, "Release notes", "Body.", true)

	rec := post(t, app, fmt.Sprintf("/admin/posts/%d/unpublish", id), nil)
	wantStatus(t, rec, http.StatusSeeOther)
	if got, want := rec.Header().Get("Location"), fmt.Sprintf("/admin/posts/%d/edit", id); got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}

	p, err := blog.Get(db, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if p.Published {
		t.Errorf("post is still published")
	}
	wantNotContains(t, get(t, app, "/").Body.String(), "Release notes")
	wantStatus(t, get(t, app, fmt.Sprintf("/posts/%d", id)), http.StatusNotFound)
}

func TestDeleteRemovesThePost(t *testing.T) {
	app, db := newApp(t)
	id := seed(t, db, "Release notes", "Body.", true)

	rec := post(t, app, fmt.Sprintf("/admin/posts/%d/delete", id), nil)
	wantStatus(t, rec, http.StatusSeeOther)
	if got, want := rec.Header().Get("Location"), "/admin/posts"; got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}

	n, err := blog.Count(db, "", "")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("%d posts left, want 0", n)
	}
	wantStatus(t, get(t, app, fmt.Sprintf("/posts/%d", id)), http.StatusNotFound)
	wantNotContains(t, get(t, app, "/admin/posts").Body.String(), "Release notes")
}

// The other three mutating actions carry the same 1 MiB body guard, and
// a non-numeric id 404s on all three. admin_form_test.go covers create
// and update; this covers publish, unpublish and delete, so all five
// POST handlers are proven.
func TestStateChangesGuardTheirBodiesAndTheirIds(t *testing.T) {
	app, db := newApp(t)
	id := seed(t, db, "Release notes", "Body.", false)

	for _, verb := range []string{"publish", "unpublish", "delete"} {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/posts/%d/%s", id, verb),
			strings.NewReader("title="+strings.Repeat("x", 2<<20)))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		wantStatus(t, rec, http.StatusBadRequest)

		if rec := post(t, app, "/admin/posts/abc/"+verb, nil); rec.Code != http.StatusNotFound {
			t.Errorf("POST /admin/posts/abc/%s = %d, want 404", verb, rec.Code)
		}
	}

	// Nothing got through: every one of those requests failed before its
	// handler touched the database.
	p, err := blog.Get(db, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if p.Published {
		t.Errorf("an oversized publish still flipped the flag")
	}
}
