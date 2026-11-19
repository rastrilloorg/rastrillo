// Package app has a Routes function of the shape apps had before the
// route table: it builds and returns a router.
package app

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Routes is not the shape rastrillo routes can call.
func Routes() http.Handler {
	return chi.NewRouter()
}
