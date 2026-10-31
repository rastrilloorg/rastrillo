// Package panics registers a path that is not a full path, which the
// route table refuses with a panic, so listing fails at the run.
package panics

import (
	"fmt"
	"os"

	"amadan.net/rastrillo/rastrillo/action"
)

// App is what the handlers would need.
type App struct{}

// Routes registers a path without its leading slash.
func Routes(routes *action.Routes, app *App) {
	fmt.Fprintln(os.Stderr, "registering")
	routes.Get("relative", app.show)
}

func (app *App) show(visit *action.Visit) {}
