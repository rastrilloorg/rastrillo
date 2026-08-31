package act_admin_posts_id_delete_post

import (
	"net/http"

	"amadan.net/rastrillo/rastrillo"

	"blog/internal/blog"
)

// Handle is POST /admin/posts/{id}/delete.
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
	if err := blog.Delete(ctx.DB, id); err != nil {
		blog.Fail(ctx, w, "deleting post", err)
		return
	}
	// A missing id affects zero rows here and the list renders either way.
	// Deliberate asymmetry with POST /admin/posts/{id}, which 404s
	// directly because it must load the post anyway to re-render.
	http.Redirect(w, r, "/admin/posts", http.StatusSeeOther)
}
