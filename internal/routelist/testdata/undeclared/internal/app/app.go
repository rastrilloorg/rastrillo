// Package app has not adopted named operations: it calls no op.Define
// and marks no route, and every action is mounted, so routes --check
// passes as it did before the marking rule.
package app

import "amadan.net/rastrillo/rastrillo/action"

// App is what the handlers would need.
type App struct{}

// Routes registers two routes, neither marked.
func Routes(routes *action.Routes, app *App) {
	routes.Get("/notes", app.noteList)
	routes.Post("/notes", app.noteCreate)
}

func (app *App) noteList(visit *action.Visit) {}

func (app *App) noteCreate(visit *action.Visit) {}
