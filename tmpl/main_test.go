package tmpl

import (
	"os"
	"testing"
)

// TestMain clears the suite's shadow mode (hack/gotest.sh turns it on
// for every test). These tests register compiled code that renders
// something html/template would not, to see which path ran, and shadow
// mode would report each as a difference. A test of shadow mode sets
// it itself.
func TestMain(tests *testing.M) {
	os.Unsetenv(ShadowEnv)
	os.Exit(tests.Run())
}
