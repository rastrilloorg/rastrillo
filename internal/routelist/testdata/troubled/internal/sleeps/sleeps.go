// Package sleeps never finishes registering, and starts a copy of the
// program that does not either, so cancelling must stop both.
package sleeps

import (
	"os"
	"os/exec"
	"time"

	"amadan.net/rastrillo/rastrillo/action"
)

// App is what the handlers would need.
type App struct{}

// Routes starts the copy, then waits for an hour. Not select {}: with
// nothing else running, Go would stop it as a deadlock.
func Routes(routes *action.Routes, app *App) {
	if os.Getenv("ROUTELIST_SLEEPS_COPY") == "" {
		copy := exec.Command(os.Args[0], os.Args[1:]...)
		copy.Env = append(os.Environ(), "ROUTELIST_SLEEPS_COPY=1")
		if err := copy.Start(); err != nil {
			panic(err)
		}
	}
	time.Sleep(time.Hour)
}
