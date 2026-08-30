package blogtest

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"blog/internal/blog"
)

// TestCoexistenceRoundTrip is task 11's proof: one story that walks
// every screen this resource has, whether the file backing it is
// hand-owned by file-level skip (the admin list — actions/admin/posts
// /index.GET.go), fully generated (posts/show), or ejected app-owned
// (posts/list and posts/form — templates/posts/{list,form}.html), all
// resolving through view.go's one merged pages map and rendering
// inside the one shared layout (see view.go's Render/buildPages doc
// comments). Nothing here is new behavior — every step is already
// covered by its own file's tests — but no other test exercises all
// three kinds of screen back to back the way an actual writer would.
func TestCoexistenceRoundTrip(t *testing.T) {
	app, db := newApp(t)
	published := seed(t, db, "Shipping the manifest system", "Body one.", true)
	seed(t, db, "Unrelated draft", "Body two.", false)

	// 1. Hand list (file-level skip): search finds the one post it
	// should, and the status filter narrows to drafts alone.
	list := get(t, app, "/admin/posts?q=manifest")
	wantStatus(t, list, http.StatusOK)
	wantContains(t, list.Body.String(), "Shipping the manifest system")
	wantNotContains(t, list.Body.String(), "Unrelated draft")

	drafts := get(t, app, "/admin/posts?status=draft")
	wantStatus(t, drafts, http.StatusOK)
	wantContains(t, drafts.Body.String(), "Unrelated draft")
	wantNotContains(t, drafts.Body.String(), "Shipping the manifest system")

	// 2. Generated show screen for the published post.
	show := get(t, app, fmt.Sprintf("/admin/posts/%d", published))
	wantStatus(t, show, http.StatusOK)
	wantContains(t, show.Body.String(), "<h1>Shipping the manifest system</h1>")
	wantContains(t, show.Body.String(), "Body one.")
	editHref := fmt.Sprintf("/admin/posts/%d/edit", published)
	wantContains(t, show.Body.String(), fmt.Sprintf(`href="%s"`, editHref))

	// 3. Ejected edit form: current values plus the revived status
	// strip (already published, from seeding).
	edit := get(t, app, editHref)
	wantStatus(t, edit, http.StatusOK)
	wantContains(t, edit.Body.String(), `value="Shipping the manifest system"`)
	wantContains(t, edit.Body.String(), `<span rst-status rst-tone="positive">Published</span>`)
	unpublishHref := fmt.Sprintf("/admin/posts/%d/unpublish", published)
	wantContains(t, edit.Body.String(), fmt.Sprintf(`action="%s"`, unpublishHref))

	// Save basics through the ejected form, back to the generated show
	// screen.
	saved := post(t, app, fmt.Sprintf("/admin/posts/%d/edit-basics", published), url.Values{
		"Title": {"Shipping the manifest system, for real"},
		"Body":  {"Updated body."},
	})
	wantStatus(t, saved, http.StatusSeeOther)
	showHref := saved.Header().Get("Location")
	if want := fmt.Sprintf("/admin/posts/%d", published); showHref != want {
		t.Fatalf("Location = %q, want %q", showHref, want)
	}
	backAtShow := get(t, app, showHref)
	wantStatus(t, backAtShow, http.StatusOK)
	wantContains(t, backAtShow.Body.String(), "<h1>Shipping the manifest system, for real</h1>")
	wantContains(t, backAtShow.Body.String(), "Updated body.")

	// 4. Create a new post via the generated New form (the same
	// ejected template's IsNew branch — no strip, since the post
	// doesn't exist yet).
	newForm := get(t, app, "/admin/posts/new")
	wantStatus(t, newForm, http.StatusOK)
	wantNotContains(t, newForm.Body.String(), `rst-status`)

	created := post(t, app, "/admin/posts", url.Values{
		"Title": {"A brand new post"},
		"Body":  {"Fresh body."},
	})
	wantStatus(t, created, http.StatusSeeOther)
	newShowHref := created.Header().Get("Location")

	// 5. Publish the new post through the strip on its own ejected
	// edit screen — the href asserted here is the one actually posted
	// to, so this both proves the strip renders it and follows it.
	newEditHref := newShowHref + "/edit"
	newEdit := get(t, app, newEditHref)
	wantStatus(t, newEdit, http.StatusOK)
	wantContains(t, newEdit.Body.String(), `<span rst-status rst-tone="neutral">Draft</span>`)
	newPublishHref := newShowHref + "/publish"
	wantContains(t, newEdit.Body.String(), fmt.Sprintf(`action="%s"`, newPublishHref))

	publishRec := post(t, app, newPublishHref, nil)
	wantStatus(t, publishRec, http.StatusSeeOther)

	// 6. The post now appears on the public site.
	home := get(t, app, "/")
	wantStatus(t, home, http.StatusOK)
	wantContains(t, home.Body.String(), "A brand new post")

	publicHref := newShowHref[len("/admin"):] // "/admin/posts/{id}" -> "/posts/{id}"
	public := get(t, app, publicHref)
	wantStatus(t, public, http.StatusOK)
	wantContains(t, public.Body.String(), "Fresh body.")

	// Sanity: the store agrees too.
	n, err := blog.Count(db, "", "")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 3 {
		t.Errorf("%d posts, want 3 (two seeded, one created)", n)
	}
}
