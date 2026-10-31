package main

import (
	"os"
	"testing"
)

// TestMain turns off rastrillo new's template step for this package's
// tests. Dozens of them scaffold an app to read its files, and the step
// runs go mod tidy against the published module and builds the app.
// The tests that make a real app (scaffoldWithReplace, scaffoldGate)
// run it themselves once the app points at this checkout, and
// TestNewCompilesTheAppsTemplates checks that new calls it.
func TestMain(tests *testing.M) {
	newTemplates = func(string) {}
	os.Exit(tests.Run())
}
