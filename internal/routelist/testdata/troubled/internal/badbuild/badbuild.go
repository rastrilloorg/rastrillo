// Package badbuild has a Routes of the right shape and a type error
// elsewhere, so listing fails at the build.
package badbuild

import "amadan.net/rastrillo/rastrillo/action"

// App is what the handlers would need.
type App struct{}

// Routes is the right shape.
func Routes(routes *action.Routes, app *App) {}

var broken int = "not a number"
