//go:build rastrillo_actions

package actions

import (
	"net/http"

	"amadan.net/rastrillo/rastrillo"

	"blog/internal/blog"
)

// Handle is GET /{$}: exactly the homepage. The generator anchors the
// root index, so unmatched paths 404 without the hand guard this
// action used to carry (friction log F6).
func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
	page := blog.PageParam(r)
	total, err := blog.CountPublished(ctx.DB)
	if err != nil {
		blog.Fail(ctx, w, "counting published posts", err)
		return
	}
	posts, err := blog.ListPublished(ctx.DB, blog.Offset(page), blog.PageSize)
	if err != nil {
		blog.Fail(ctx, w, "loading published posts", err)
		return
	}

	blog.Render(ctx, w, "index", http.StatusOK, blog.HomeView{
		Head:       blog.Head{Title: "The blog"},
		Rows:       blog.PublicRows(posts),
		Pagination: blog.BuildPagination("/", "", "", page, total),
	})
}
