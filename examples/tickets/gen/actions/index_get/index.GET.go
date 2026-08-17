package act_index_get

import (
	"net/http"

	"github.com/carlosframework/rastrillo"
)

// Handle is GET / — a hand-written action living happily beside the
// manifest's generated screens: the mixing §3 promises.
func Handle(_ *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/admin/ticket_types", http.StatusSeeOther)
}
