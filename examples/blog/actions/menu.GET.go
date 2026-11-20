//go:build rastrillo_actions

package actions

import (
	"net/http"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/ui"
	"amadan.net/rastrillo/rastrillo/view"

	"blog/internal/blog"
)

// Handle is GET /menu: the topbar's Menu page, one level up from every
// page. On a phone the bar's nav is its rows; on a desktop the page
// lists them itself.
func Handle(ctx *rastrillo.Ctx, response http.ResponseWriter,
	request *http.Request) {
	view.Render(ctx, response, "menu", http.StatusOK, blog.MenuView{
		Localized: ui.ForRequest(request),
		Head:      blog.Head{Title: "Menu"},
	})
}
