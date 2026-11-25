package main

import (
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/counts"
	"amadan.net/rastrillo/rastrillo/internal/codelimits"
)

// The table labelled app code only holds app code only; the tests have
// a table of their own.
func TestTheAppTableHoldsNoTests(t *testing.T) {
	row := scoreRow{Round: "1", Run: "groomer-carlos-1", Version: "v1",
		AppCode: counts.Totals{Files: 7, Lines: 700, CommentShare: 0.25,
			Offences: map[codelimits.Kind]int{codelimits.Columns: 3}},
		TestCode: counts.Totals{Files: 2, Lines: 400, CommentShare: 0.05,
			Offences: map[codelimits.Kind]int{codelimits.Columns: 11}},
		Funcs: []counts.FuncCounts{{Key: "Serve"}, {Key: "TestServe",
			Test: true}, {Key: "TestBook", Test: true}}}

	text := scoreMarkdown("test", []scoreRow{row})

	app, tests, found := strings.Cut(text, "## Tests")
	if !found {
		t.Fatalf("no tests table:\n%s", text)
	}
	if !strings.Contains(app, "| 1/groomer-carlos-1 | v1 | 7 | 700 | 1 |") ||
		!strings.Contains(app, "| 3 | 0 | 0 | 0 | 0.25 |") {
		t.Errorf("the app table does not hold the app's totals:\n%s", app)
	}
	if !strings.Contains(tests,
		"| 1/groomer-carlos-1 | 2 | 400 | 2 | 11 | 0 | 0 | 0 | 0.05 |") {
		t.Errorf("the tests table does not hold the tests' totals:\n%s", tests)
	}
}
