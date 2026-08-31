package act_admin_posts_id_unpublish_post

import (
	"fmt"
	"net/http"

	"amadan.net/rastrillo/rastrillo"

	"blog/internal/blog"
)

// Handle is POST /admin/posts/{id}/unpublish. The mirror of publish, and
// a separate route for the same reason: its control is its own <form>,
// and a form nested inside the edit form would be invalid HTML.
func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request.", http.StatusBadRequest)
		return
	}
	id, ok := blog.ParseID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := blog.SetPublished(ctx.DB, id, false); err != nil {
		blog.Fail(ctx, w, "unpublishing post", err)
		return
	}
	// A missing id affects zero rows here and 404s on the redirect target
	// instead. Deliberate asymmetry with POST /admin/posts/{id}, which
	// 404s directly because it must load the post anyway to re-render.
	http.Redirect(w, r, fmt.Sprintf("/admin/posts/%d/edit", id), http.StatusSeeOther)
}
