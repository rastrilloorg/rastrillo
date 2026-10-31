// Package app is laid out as rastrillo new lays one out. Its Routes
// mounts every kind of route, and it holds actions no route mounts, so
// the listing and the check can be read against what is written here.
package app

import (
	"fmt"
	"net/http"

	"amadan.net/rastrillo/rastrillo/action"
	"example.com/sibling/greeting"
)

// App is what the handlers need. Every field is empty when routes are
// listed, so nothing here may be read while registering.
type App struct {
	RequireAdmin func(http.Handler) http.Handler
	API          http.Handler
	Tags         *Tags
	Tickets      *Tickets
	Report       Report
}

// The JSON is what a listing would be, so a parser reading stdout
// would take it for the routes.
func init() {
	fmt.Println(`[{"Method":"FROM-INIT","Path":"/printed"}]`)
}

// Routes registers every route, given what the handlers need.
func Routes(routes *action.Routes, app *App) {
	routes.Get("/", app.index)
	routes.Route("/admin", func(admin *action.Routes) {
		admin.Guard("admin", app.RequireAdmin)
		admin.Get("/admin", app.adminHome)
		admin.Post("/admin/tags/{id}", app.Tags.Show)
	})
	routes.Get("/report", app.Report.Show)
	routes.Handle("GET", "/healthz", http.NotFoundHandler(), "tagEdit")
	routes.Mount("/api", app.API, "api")
}

func (app *App) index(visit *action.Visit) {
	visit.Response().Write([]byte(greeting.Hello()))
}

func (app *App) adminHome(visit *action.Visit) {}

// tagEdit is mounted by no route. The Handle above is named after it
// and mounts something else, which must not count.
func (app *App) tagEdit(visit *action.Visit) {}
