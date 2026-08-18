package blogtest

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestIndexListsPublishedPostsNewestFirst(t *testing.T) {
	app, db := newApp(t)
	seed(t, db, "Older published", "One.", true)
	seed(t, db, "Newer published", "Two.", true)
	seed(t, db, "A draft", "Three.", false)

	rec := get(t, app, "/")
	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()

	wantContains(t, body, "Newer published")
	wantContains(t, body, "Older published")
	wantNotContains(t, body, "A draft")
	if strings.Index(body, "Newer published") > strings.Index(body, "Older published") {
		t.Errorf("newest post is not first:\n%s", body)
	}
}

func TestIndexEmptyState(t *testing.T) {
	app, _ := newApp(t)

	rec := get(t, app, "/")
	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()

	wantContains(t, body, `<div class="rst-empty">`)
	wantContains(t, body, "Nothing published yet. Check back soon.")
	// A reader cannot act on an empty blog, so no call to action.
	wantNotContains(t, body, "rst-empty__cta")
}

func TestIndexSecondPageShowsTheEleventhPost(t *testing.T) {
	app, db := newApp(t)
	for i := 1; i <= 11; i++ {
		seed(t, db, fmt.Sprintf("Post %02d", i), "Body.", true)
	}

	first := get(t, app, "/")
	wantStatus(t, first, http.StatusOK)
	wantNotContains(t, first.Body.String(), "Post 01")
	wantContains(t, first.Body.String(), `<nav class="rst-pagination"`)

	second := get(t, app, "/?page=2")
	wantStatus(t, second, http.StatusOK)
	wantContains(t, second.Body.String(), "Post 01")
	wantNotContains(t, second.Body.String(), "Post 11")
}

// The generator anchors the root index to GET /{$}, so unmatched GETs
// — including the trailing-slash forms, which no route registers —
// fall to the mux's own 404 (F6).
func TestUnmatchedGETsAre404NotTheHomepage(t *testing.T) {
	app, db := newApp(t)
	seed(t, db, "Published", "Body.", true)

	for _, target := range []string{"/nonsense", "/admin/posts/", "/posts/1/"} {
		rec := get(t, app, target)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404\n%s", target, rec.Code, rec.Body.String())
		}
	}
}

// GET /{$} is an exact match for just "/", so POST to an unmatched path
// has no route at all and gets 404 from the mux, not 405. The generator's
// exact pattern anchors the root and eliminates the prefix-match ambiguity.
func TestPostToAnUnmatchedPathIs404(t *testing.T) {
	app, _ := newApp(t)

	rec := post(t, app, "/nonsense", nil)
	wantStatus(t, rec, http.StatusNotFound)
}

// POST to "/" — a path with a registered GET handler but no POST — returns
// 405 with the allowed methods. Ensuring this isn't regressed by a
// reappearing catch-all guard is important for the mux's error handling
// contract.
func TestPostToAnExistingGETRouteIs405(t *testing.T) {
	app, _ := newApp(t)

	rec := post(t, app, "/", nil)
	wantStatus(t, rec, http.StatusMethodNotAllowed)
	if got, want := rec.Header().Get("Allow"), "GET, HEAD"; got != want {
		t.Errorf("Allow = %q, want %q", got, want)
	}
}

func TestPostPageRendersParagraphs(t *testing.T) {
	app, db := newApp(t)
	id := seed(t, db, "Release notes", "First paragraph.\n\nSecond paragraph.", true)

	rec := get(t, app, fmt.Sprintf("/posts/%d", id))
	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()

	wantContains(t, body, "<p>First paragraph.</p>")
	wantContains(t, body, "<p>Second paragraph.</p>")
	wantContains(t, body, `<article class="blog-article">`)
}

func TestPostPageEscapesBodyText(t *testing.T) {
	app, db := newApp(t)
	id := seed(t, db, "Escaping", "<script>alert(1)</script>", true)

	rec := get(t, app, fmt.Sprintf("/posts/%d", id))
	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()

	wantNotContains(t, body, "<script>")
	wantContains(t, body, "&lt;script&gt;")
}

func TestPostPage404s(t *testing.T) {
	app, db := newApp(t)
	draft := seed(t, db, "A draft", "Body.", false)

	cases := map[string]string{
		"a draft has no public page": fmt.Sprintf("/posts/%d", draft),
		"a missing id":               "/posts/9999",
		"a non-numeric id":           "/posts/abc",
	}
	for name, target := range cases {
		rec := get(t, app, target)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: GET %s = %d, want 404", name, target, rec.Code)
		}
	}
}
