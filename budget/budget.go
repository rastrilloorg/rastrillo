// Package budget measures how long each test binary takes, so
// `rastrillo budget test` can hold every package to its time budget.
//
//	func TestMain(m *testing.M) { os.Exit(budget.Main(m)) }
//
// Main judges nothing and reads no file. Go records a test's file and
// environment reads in its cache key only between m.Run's start and end;
// a verdict computed out here would leave budgets.txt and the CI switch
// out of that key, and a cached pass could then be replayed under a
// tighter budget. So the binary only prints its time. Go caches that
// line with the rest of a passing package's output and replays it on a
// hit, which is what lets the wrapper judge a cached package on the time
// it took when it really ran: a retry cannot turn a slow package green.
package budget

import (
	"fmt"
	"os"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/internal/budgetfile"
)

// Prefix starts the one line Main prints. The version is there so the
// wrapper can refuse a format it does not understand instead of
// misreading it.
const Prefix = "rastrillo-budget/v1 ran "

// Main runs the package's tests and returns m.Run's code unchanged.
func Main(m *testing.M) int {
	start := time.Now()
	code := m.Run()
	// perftest.Boot's child is the same binary: its line would be judged
	// as a second run of this package, so it stays silent.
	if os.Getenv(budgetfile.BootChildEnv) == "" {
		fmt.Printf("%s%s\n", Prefix, time.Since(start).Round(time.Millisecond))
	}
	return code
}
