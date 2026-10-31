// Package first is one of two packages with a Routes function.
package first

import "amadan.net/rastrillo/rastrillo/action"

// App is what the handlers would need.
type App struct{}

// Routes registers this package's one route.
func Routes(routes *action.Routes, app App) {
	routes.Get("/first", app.show)
}

func (app App) show(visit *action.Visit) {}
