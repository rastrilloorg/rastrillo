package rastrillo

import (
	"fmt"
	"os"

	"amadan.net/rastrillo/rastrillo/tmpl"
)

// captureExit ends the process once a capture is written. A test
// replaces it, and then Serve returns what it would have exited over.
var captureExit = os.Exit

// captureAndExit closes what Handler opened, writes the template sets
// to path, and exits: 0, or 1 when the write failed. It exits rather
// than returns so that nothing after Serve in the app's main, such as
// a migration or a mail-out, runs during rastrillo generate.
func captureAndExit(path string, closeHandler func() error) error {
	if err := closeHandler(); err != nil {
		fmt.Fprintln(os.Stderr, "rastrillo: capture: closing the app:", err)
	}
	if err := tmpl.WriteCapture(path); err != nil {
		fmt.Fprintln(os.Stderr, "rastrillo: capture:", err)
		captureExit(1)
		return err
	}
	captureExit(0)
	return nil
}
