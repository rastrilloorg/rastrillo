// Package app marks one route with its operation and one as page-only,
// and leaves one unmarked, which routes --check must name. Every action
// is mounted, so the marking rule is the only thing --check finds.
package app

import (
	"amadan.net/rastrillo/rastrillo/action"
	"amadan.net/rastrillo/rastrillo/route"
	"example.com/declares/internal/notes"
)

// App is what the handlers would need.
type App struct{}

// Routes registers the three routes.
func Routes(routes *action.Routes, app *App) {
	routes.Post("/notes", app.noteCreate,
		route.Operation(notes.Create.Descriptor))
	routes.Get("/style.css", app.stylesheet,
		route.PageOnly("the stylesheet"))
	routes.Get("/notes", app.noteList)
}

func (app *App) noteCreate(visit *action.Visit) {}

func (app *App) stylesheet(visit *action.Visit) {}

func (app *App) noteList(visit *action.Visit) {}
