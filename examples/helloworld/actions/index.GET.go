// actions/ is generator input, never compiled in place: rastrillo
// generate copies each file under gen/ (stripping this constraint).
// The tag keeps `go build ./...` and friends off the originals.
//go:build rastrillo_actions

package actions

import (
	"fmt"
	"net/http"

	"amadan.net/rastrillo/rastrillo"
)

func Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "Hello, World — this is a rastrillo app.")
}
