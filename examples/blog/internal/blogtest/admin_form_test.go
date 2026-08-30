// New, Show and Edit are the manifest system's generated actions now
// (manifest/posts.toml; task 10's adoption) — the generated create and
// edit-basics actions have no server-side required-field validation at
// all in this v1 (internal/generate/actions.go's package doc: "No
// server-side required-field validation exists anywhere in this slice
// — a deliberate v1 rule"). "empty title is 400" for both create and
// update no longer holds and stays retired below with its own note,
// per the task-10 report.
//
// The Edit screen's status pill and publish/unpublish/delete controls
// — task 10 dropped them when the old hand admin_edit.html was
// deleted, since the generated form.html the manifest replaced it with
// carries none of that (posts.toml declares no `published` field at
// all) — are back as of task 11: templates/posts/form.html is now an
// ejected, app-owned file (generation of it stopped the moment that
// file appeared under templates/posts/), and genrender.go's
// formStripData computes the strip's data from the app's own posts
// table before the template ever executes. See that function's own
// doc comment for how, and view.go's Render for the one seam it hooks.
package blogtest

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"blog/internal/blog"
)

func TestNewPostFormRenders(t *testing.T) {
	app, _ := newApp(t)

	rec := get(t, app, "/admin/posts/new")
	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()

	wantContains(t, body, `<form rst-form method="post" action="/admin/posts">`)
	wantContains(t, body, `rst-field`)
	wantContains(t, body, `<label rst-field-label for="Title">Title</label>`)
	wantContains(t, body, `<input rst-input id="Title" name="Title" type="text">`)
	wantContains(t, body, `<textarea rst-textarea id="Body" name="Body">`)
	wantContains(t, body, `<button rst-btn="primary" type="submit">Save</button>`)
	wantContains(t, body, `<header rst-page-header>`)
	wantContains(t, body, `<h1>New post</h1>`)
	// No required marker: the manifest declares no required fields. No
	// status pill or publish/unpublish/delete controls either — a post
	// that doesn't exist yet cannot be published, unpublished, deleted
	// or viewed; templates/posts/form.html (ejected) guards that whole
	// strip on !IsNew (see genrender.go's formStripData doc comment).
	wantNotContains(t, body, `rst-field-required`)
	wantNotContains(t, body, `required>`)
	wantNotContains(t, body, `rst-status`)
	wantNotContains(t, body, `/publish"`)
	wantNotContains(t, body, `/unpublish"`)
	wantNotContains(t, body, `/delete"`)
}

func TestCreateRedirectsToTheNewPostsShowPage(t *testing.T) {
	app, db := newApp(t)

	rec := post(t, app, "/admin/posts", url.Values{
		"Title": {"First post"},
		"Body":  {"Hello."},
	})
	wantStatus(t, rec, http.StatusSeeOther)

	posts, err := blog.List(db, "", "", 0, blog.PageSize)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(posts) != 1 {
		t.Fatalf("created %d posts, want 1", len(posts))
	}
	// The generated create action redirects to the show route
	// (r.Route+"/%d"), not the old hand action's edit route.
	want := fmt.Sprintf("/admin/posts/%d", posts[0].ID)
	if got := rec.Header().Get("Location"); got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}

	list := get(t, app, "/admin/posts")
	wantContains(t, list.Body.String(), "First post")
}

// Re-establishes TestCreateWithAnEmptyTitleIs400AndCreatesNothing: now
// that the manifest declares Title as required, the generated create action
// validates it server-side and returns 400 with a field error.
func TestCreateWithAnEmptyTitleIs400AgainstGeneratedValidation(t *testing.T) {
	app, db := newApp(t)

	rec := post(t, app, "/admin/posts", url.Values{
		"Title": {"   "},
		"Body":  {"Body the writer typed."},
	})
	wantStatus(t, rec, http.StatusBadRequest)
	body := rec.Body.String()

	// The error message contains "required" as specified in the manifest field validation
	wantContains(t, body, "required")
	// The submitted body is still in the field: a failed submission never
	// costs the writer what they typed.
	wantContains(t, body, "Body the writer typed.")

	n, err := blog.Count(db, "", "")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("created %d posts, want 0", n)
	}
}

func TestShowPageRendersFields(t *testing.T) {
	app, db := newApp(t)
	id := seed(t, db, "Release notes", "The body.", false)

	rec := get(t, app, fmt.Sprintf("/admin/posts/%d", id))
	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()

	wantContains(t, body, `<h1>Release notes</h1>`)
	wantContains(t, body, "The body.")
	wantContains(t, body, fmt.Sprintf(`href="/admin/posts/%d/edit"`, id))
	wantContains(t, body, `<header rst-page-header>`)
}

func TestShowWithAMissingIdIs404(t *testing.T) {
	app, _ := newApp(t)

	rec := get(t, app, "/admin/posts/9999")
	wantStatus(t, rec, http.StatusNotFound)
}

func TestEditShowsCurrentValues(t *testing.T) {
	app, db := newApp(t)
	id := seed(t, db, "Release notes", "The body.", false)

	rec := get(t, app, fmt.Sprintf("/admin/posts/%d/edit", id))
	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()

	wantContains(t, body, `rst-field`)
	wantContains(t, body, `value="Release notes"`)
	wantContains(t, body, "The body.")
	wantContains(t, body, fmt.Sprintf(`action="/admin/posts/%d/edit-basics"`, id))
	wantContains(t, body, `<header rst-page-header>`)
	wantContains(t, body, `<h1>Release notes</h1>`)
}

// Restores the pill/button half of task 10's retired
// TestEditShowsCurrentValuesAndTheDraftPill, now that templates/posts/
// form.html (ejected — task 11) carries the status strip again.
func TestEditShowsTheDraftPillAndAPublishControl(t *testing.T) {
	app, db := newApp(t)
	id := seed(t, db, "Release notes", "The body.", false)

	rec := get(t, app, fmt.Sprintf("/admin/posts/%d/edit", id))
	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()

	wantContains(t, body, `<span rst-status rst-tone="neutral">Draft</span>`)
	wantContains(t, body, fmt.Sprintf(`action="/admin/posts/%d/publish"`, id))
	wantContains(t, body, fmt.Sprintf(`action="/admin/posts/%d/delete"`, id))
	wantNotContains(t, body, fmt.Sprintf(`action="/admin/posts/%d/unpublish"`, id))
	wantNotContains(t, body, fmt.Sprintf(`href="/posts/%d"`, id))
}

// Restores the pill/button half of task 10's retired
// TestEditShowsThePublishedPillAfterPublishing.
func TestEditShowsThePublishedPillAndAnUnpublishControl(t *testing.T) {
	app, db := newApp(t)
	id := seed(t, db, "Release notes", "The body.", true)

	rec := get(t, app, fmt.Sprintf("/admin/posts/%d/edit", id))
	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()

	wantContains(t, body, `<span rst-status rst-tone="positive">Published</span>`)
	wantContains(t, body, fmt.Sprintf(`action="/admin/posts/%d/unpublish"`, id))
	wantContains(t, body, fmt.Sprintf(`href="/posts/%d"`, id))
	wantContains(t, body, fmt.Sprintf(`action="/admin/posts/%d/delete"`, id))
	wantNotContains(t, body, fmt.Sprintf(`action="/admin/posts/%d/publish"`, id))
}

func TestEditWithAMissingIdIs404(t *testing.T) {
	app, _ := newApp(t)

	rec := get(t, app, "/admin/posts/9999/edit")
	wantStatus(t, rec, http.StatusNotFound)
}

// A non-numeric id is a URL that was never ours, on the admin side as
// much as the public one: parseID fails and the action answers 404, not
// 400.
func TestANonNumericIdIs404OnTheAdminSideToo(t *testing.T) {
	app, _ := newApp(t)

	if rec := get(t, app, "/admin/posts/abc/edit"); rec.Code != http.StatusNotFound {
		t.Errorf("GET /admin/posts/abc/edit = %d, want 404", rec.Code)
	}
	if rec := get(t, app, "/admin/posts/abc"); rec.Code != http.StatusNotFound {
		t.Errorf("GET /admin/posts/abc = %d, want 404", rec.Code)
	}
	if rec := post(t, app, "/admin/posts/abc/edit-basics", nil); rec.Code != http.StatusNotFound {
		t.Errorf("POST /admin/posts/abc/edit-basics = %d, want 404", rec.Code)
	}
}

// The 1 MiB MaxBytesReader guard opens every mutating action; this is the
// test that proves it is wired. A body over the cap makes ParseForm fail,
// and a ParseForm failure is a 400. The three state-change routes get the
// same check in admin_state_test.go.
func TestAnOversizedPostBodyIs400(t *testing.T) {
	app, db := newApp(t)
	id := seed(t, db, "T", "B", false)
	for _, target := range []string{"/admin/posts", fmt.Sprintf("/admin/posts/%d/edit-basics", id)} {
		req := httptest.NewRequest(http.MethodPost, target,
			strings.NewReader("Title="+strings.Repeat("x", 2<<20)))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		wantStatus(t, rec, http.StatusBadRequest)
	}
}

func TestUpdateChangesTheFieldsAndRedirectsBack(t *testing.T) {
	app, db := newApp(t)
	id := seed(t, db, "Before", "Old body.", false)
	before, err := blog.Get(db, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	// Timestamps are RFC3339 seconds, so wait for the clock to cross a
	// second boundary — otherwise "updated_at moved" is unobservable.
	time.Sleep(time.Until(time.Now().Truncate(time.Second).Add(time.Second)) + 20*time.Millisecond)

	rec := post(t, app, fmt.Sprintf("/admin/posts/%d/edit-basics", id), url.Values{
		"Title": {"After"},
		"Body":  {"New body."},
	})
	wantStatus(t, rec, http.StatusSeeOther)
	// The generated edit-basics action redirects to the show route
	// (r.Route+"/%d"), not the old hand action's edit route.
	if got, want := rec.Header().Get("Location"), fmt.Sprintf("/admin/posts/%d", id); got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}

	after, err := blog.Get(db, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if after.Title != "After" || after.Body != "New body." {
		t.Errorf("post = %q/%q, want After/New body.", after.Title, after.Body)
	}
	if !after.UpdatedAt.After(before.UpdatedAt) {
		t.Errorf("updated_at did not move: %v then %v", before.UpdatedAt, after.UpdatedAt)
	}
	if !after.CreatedAt.Equal(before.CreatedAt) {
		t.Errorf("created_at moved: %v then %v", before.CreatedAt, after.CreatedAt)
	}
}

// Re-establishes TestUpdateWithAnEmptyTitleIs400AndChangesNothing: now
// that the manifest declares Title as required, the generated edit-basics
// action validates it server-side and returns 400 with a field error.
func TestUpdateWithAnEmptyTitleIs400AgainstGeneratedValidation(t *testing.T) {
	app, db := newApp(t)
	id := seed(t, db, "Before", "Old body.", false)

	rec := post(t, app, fmt.Sprintf("/admin/posts/%d/edit-basics", id), url.Values{
		"Title": {""},
		"Body":  {"New body."},
	})
	wantStatus(t, rec, http.StatusBadRequest)
	body := rec.Body.String()

	// The error message contains "required" as specified in the manifest field validation
	wantContains(t, body, "required")
	// The re-render is the edit screen
	wantContains(t, body, "New body.")

	after, err := blog.Get(db, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if after.Title != "Before" || after.Body != "Old body." {
		t.Errorf("post changed: %q/%q", after.Title, after.Body)
	}
}
