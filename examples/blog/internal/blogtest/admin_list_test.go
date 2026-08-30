package blogtest

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"blog/internal/blog"
)

func TestAdminListShowsDraftsAndPublished(t *testing.T) {
	app, db := newApp(t)
	seed(t, db, "A draft", "Body.", false)
	seed(t, db, "Live post", "Body.", true)

	rec := get(t, app, "/admin/posts")
	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()

	wantContains(t, body, "A draft")
	wantContains(t, body, "Live post")
	// Status now rides in the pill.
	wantContains(t, body, `rst-tone="neutral">Draft<`)
	wantContains(t, body, "Edited")
	wantContains(t, body, `rst-tone="positive">Published<`)
	// The published row gets a pill; the draft, having no public page, does not.
	if n := strings.Count(body, `rst-row-action`); n != 1 {
		t.Errorf("%d action pills, want exactly 1 (the published row)", n)
	}
}

func TestAdminSearchFiltersByTitleCaseInsensitively(t *testing.T) {
	app, db := newApp(t)
	seed(t, db, "Going to production", "Body.", true)
	seed(t, db, "Unrelated", "Body.", true)

	rec := get(t, app, "/admin/posts?q=GOING")
	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()

	wantContains(t, body, "Going to production")
	wantNotContains(t, body, "Unrelated")
	// The search field echoes the query back, so a search survives paging.
	wantContains(t, body, `value="GOING"`)
}

// A search that matches nothing is not an empty state: a dashed
// blank-state card telling a writer with forty posts that their blog is
// empty is a lie the component would happily tell.
func TestAdminSearchWithNoMatchRendersANoteNotTheEmptyState(t *testing.T) {
	app, db := newApp(t)
	seed(t, db, "Going to production", "Body.", true)

	rec := get(t, app, "/admin/posts?q=zzz")
	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()

	wantContains(t, body, `<p class="blog-note">`)
	wantContains(t, body, "No posts match")
	wantNotContains(t, body, `<div rst-empty>`)
}

func TestAdminWithNoPostsRendersTheEmptyStateNotTheNote(t *testing.T) {
	app, _ := newApp(t)

	rec := get(t, app, "/admin/posts")
	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()

	wantContains(t, body, `<div rst-empty>`)
	wantContains(t, body, "No posts yet")
	wantContains(t, body, "Write your first post")
	wantNotContains(t, body, `<p class="blog-note">`)
}

func TestAdminPaginationAppearsOnlyPastOnePage(t *testing.T) {
	app, db := newApp(t)
	for i := 1; i <= 10; i++ {
		seed(t, db, fmt.Sprintf("Post %02d", i), "Body.", false)
	}

	ten := get(t, app, "/admin/posts")
	wantStatus(t, ten, http.StatusOK)
	// The partial emits its <nav> even with no items, so an unguarded call
	// would leave an empty landmark on every single-page list.
	wantNotContains(t, ten.Body.String(), `<nav rst-pagination`)

	seed(t, db, "Post 11", "Body.", false)

	eleven := get(t, app, "/admin/posts")
	wantStatus(t, eleven, http.StatusOK)
	body := eleven.Body.String()
	wantContains(t, body, `<nav rst-pagination`)
	wantContains(t, body, `<span aria-current="page">1</span>`)
	wantContains(t, body, `<a href="/admin/posts?page=2">2</a>`)
	// Previous is present but not actionable on page 1 — and visibly
	// so: the class is what tokens.css styles (friction log F10).
	wantContains(t, body, `<span rst-pagination-disabled>Previous</span>`)
}

// html/template escapes & inside an attribute value, so the preserved
// query looks like q=go&amp;page=2 in the source. The rendered page is
// correct either way — a browser unescapes it — but a test asserting the
// raw ampersand fails against working output.
func TestAdminPaginationCarriesTheQueryIntoEveryPageLink(t *testing.T) {
	app, db := newApp(t)
	for i := 1; i <= 11; i++ {
		seed(t, db, fmt.Sprintf("go %02d", i), "Body.", false)
	}

	rec := get(t, app, "/admin/posts?q=go")
	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()

	wantContains(t, body, `href="/admin/posts?q=go&amp;page=2"`)
	wantNotContains(t, body, `href="/admin/posts?page=2"`)
}

func TestAdminSecondPageShowsTheEleventhPost(t *testing.T) {
	app, db := newApp(t)
	for i := 1; i <= 11; i++ {
		seed(t, db, fmt.Sprintf("Post %02d", i), "Body.", false)
	}

	rec := get(t, app, "/admin/posts?page=2")
	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()

	wantContains(t, body, "Post 01")
	wantNotContains(t, body, "Post 11")
}

func TestAdminListFiltersByStatus(t *testing.T) {
	h, db := newApp(t)
	seed(t, db, "Draft post", "b", false)
	seed(t, db, "Published post", "b", true)

	rec := get(t, h, "/admin/posts?status=draft")
	body := rec.Body.String()
	if !strings.Contains(body, "Draft post") || strings.Contains(body, "Published post") {
		t.Errorf("draft filter listed the wrong rows: %s", body)
	}
	// The applied choice is marked, and the summary names it.
	if !strings.Contains(body, `aria-current="true"`) {
		t.Errorf("current filter item not marked: %s", body)
	}
	if !strings.Contains(body, `aria-label="Filter by status: Drafts"`) {
		t.Errorf("summary does not name the applied filter: %s", body)
	}
	// Searching from a filtered list keeps the filter.
	if !strings.Contains(body, `<input type="hidden" name="status" value="draft">`) {
		t.Errorf("search form does not carry the filter: %s", body)
	}
}

func TestAdminListFilterComposesWithSearchAndPaging(t *testing.T) {
	h, db := newApp(t)
	for i := 0; i < blog.PageSize+1; i++ {
		seed(t, db, fmt.Sprintf("Note %02d", i), "b", false)
	}
	rec := get(t, h, "/admin/posts?q=Note&status=draft")
	body := rec.Body.String()
	// Pagination carries both q and status, page last — checked on the
	// page-2 link itself so the filter dropdown's own href (which also
	// contains "q=Note" and "status=draft", but never "page=") can't
	// satisfy this assertion by accident.
	if !strings.Contains(body, `href="/admin/posts?q=Note&amp;status=draft&amp;page=2"`) {
		t.Errorf("pagination dropped a parameter: %s", body)
	}
}

func TestAdminListFilterWithNoMatchesSaysSo(t *testing.T) {
	h, db := newApp(t)
	seed(t, db, "Only draft", "b", false)

	rec := get(t, h, "/admin/posts?status=published")
	body := rec.Body.String()
	if !strings.Contains(body, "No published posts yet.") {
		t.Errorf("missing the filtered no-match note: %s", body)
	}
	if strings.Contains(body, "Every blog starts empty") {
		t.Errorf("empty-state card shown for a filter miss: %s", body)
	}
}
