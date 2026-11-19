//go:build routelist_extra

package app

import "amadan.net/rastrillo/rastrillo/action"

// Routes is built only with -tags routelist_extra.
func Routes(routes *action.Routes, app *App) {}
