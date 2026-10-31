// Command app serves the app. It is never run by the tests; it is here
// so the module has the commands rastrillo new gives it.
package main

import (
	"net/http"

	"amadan.net/rastrillo/rastrillo/action"
	"example.com/app/internal/app"
	"github.com/go-chi/chi/v5"
)

func main() {
	router := chi.NewRouter()
	app.Routes(action.NewRoutes(router, nil), &app.App{})
	http.ListenAndServe("127.0.0.1:8080", router)
}
