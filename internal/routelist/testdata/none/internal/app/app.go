// Package app registers its routes some other way, so there is no
// Routes function to find.
package app

import "amadan.net/rastrillo/rastrillo/action"

// App is what the handlers would need.
type App struct{}

// Register is not called Routes.
func Register(routes *action.Routes, app *App) {}
