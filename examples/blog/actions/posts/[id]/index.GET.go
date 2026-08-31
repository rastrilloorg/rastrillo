//go:build rastrillo_actions

package actions

import (
	"database/sql"
	"errors"
	"net/http"

	"amadan.net/rastrillo/rastrillo"

	"blog/internal/blog"
)

// Handle is GET /posts/{id}.
func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
	id, ok := blog.ParseID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	post, err := blog.Get(ctx.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		blog.Fail(ctx, w, "loading post", err)
		return
	}
	// A draft has no public page. Not 403: the reader is not being told
	// a post exists.
	if !post.Published {
		http.NotFound(w, r)
		return
	}

	blog.Render(ctx, w, "post", http.StatusOK, blog.PostView{
		Head:       blog.Head{Title: post.Title},
		Title:      post.Title,
		Date:       blog.PublishedLine(post.CreatedAt),
		Paragraphs: blog.Paragraphs(post.Body),
	})
}
