package act_admin_posts_index_get

import (
	"net/http"
	"strings"

	"github.com/carlosframework/rastrillo"

	"blog/internal/blog"
)

// Handle is GET /admin/posts.
func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	status := blog.NormalizeStatus(r.URL.Query().Get("status"))
	page := blog.PageParam(r)

	all, err := blog.Count(ctx.DB, "", "")
	if err != nil {
		blog.Fail(ctx, w, "counting posts", err)
		return
	}
	total, err := blog.Count(ctx.DB, q, status)
	if err != nil {
		blog.Fail(ctx, w, "counting matching posts", err)
		return
	}
	posts, err := blog.List(ctx.DB, q, status, blog.Offset(page), blog.PageSize)
	if err != nil {
		blog.Fail(ctx, w, "loading posts", err)
		return
	}

	var carry [][2]string
	if status != "" {
		// A search from a filtered list keeps the filter.
		carry = [][2]string{{"status", status}}
	}

	blog.Render(ctx, w, "posts/list", http.StatusOK, blog.AdminListView{
		Head:        blog.Head{Title: "Posts"},
		Query:       q,
		Carry:       carry,
		Filter:      blog.BuildStatusFilter(q, status),
		NoMatchNote: blog.NoMatchNote(q, status),
		Rows:        blog.AdminRows(posts),
		Pagination:  blog.BuildPagination("/admin/posts", q, status, page, total),
		// The true blank state gets the empty-state card; a search or
		// filter that matched nothing gets a plain note instead. Telling
		// a writer with forty posts that their blog is empty is a lie.
		Empty:   all == 0,
		NoMatch: all > 0 && total == 0,
	})
}
