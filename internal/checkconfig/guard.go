// Package checkconfig holds the RASTRILLO_CHECK_CONFIG guard that every
// database entry point runs first. It imports only the standard library
// so db and the root package can both call it without a cycle.
package checkconfig

import (
	"fmt"
	"os"
)

// Env is the variable that asks an app binary to check its locale
// configuration and exit without serving.
const Env = "RASTRILLO_CHECK_CONFIG"

// Guard is the first statement of every entry point that opens the
// database (Serve, Handler, OpenDB, db.Open). The probe exits before
// any of them runs, so reaching one with the variable set means the app
// opened its database before probing. Exiting 2 here, before anything
// is opened, keeps doctor's probe from ever creating, migrating or
// serving a live database, and stops an instance that inherited the
// variable where its supervisor shows it rather than letting it serve.
func Guard(entry string) {
	if os.Getenv(Env) == "" {
		return
	}
	fmt.Fprintf(os.Stderr, "RASTRILLO_CHECK_CONFIG is set but %s was reached before the probe ran: call rastrillo.ProbeConfig (or Resolve, or Run) before opening the database; nothing was opened\n", entry)
	os.Exit(2)
}
