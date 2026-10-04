// Package perftest is the CI half of package perf: it fails a branch
// that makes a screen or a cold start slower than perf's production
// budgets, measured over real HTTP in a serial lane of its own.
package perftest

import (
	"fmt"
	"os"
	"testing"

	"amadan.net/rastrillo/rastrillo/internal/budgetfile"
	"amadan.net/rastrillo/rastrillo/internal/budgetrun"
)

// load reads the module's budgets.txt. go test runs a package with its
// directory as the working directory, so the module root is found from
// there.
func load(t testing.TB) *budgetfile.File {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := budgetfile.ModuleRoot(wd)
	if err != nil {
		t.Fatal(err)
	}
	f, err := budgetfile.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// say prints a result line rastrillo budget test keeps even from a
// passing test, so a local make perf shows every measurement rather than
// only failures.
func say(format string, a ...any) {
	fmt.Println(budgetrun.PerfPrefix + fmt.Sprintf(format, a...))
}
