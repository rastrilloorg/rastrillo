package act_index_get

import (
	"fmt"
	"net/http"

	"amadan.net/rastrillo/rastrillo"
)

func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "Hello, World — this is a rastrillo app.")
}
