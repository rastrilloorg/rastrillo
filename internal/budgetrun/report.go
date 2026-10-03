package budgetrun

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// Print writes what failed before anything else, because a runner's log
// is read from the top and Tito Go once printed 200 lines of PASS above
// its only FAIL. Passing output is dropped, except perftest's result
// lines, which are the point of a perf run.
func Print(w io.Writer, r *Report, v Verdict, wall time.Duration) {
	for _, ip := range r.BuildFail {
		fmt.Fprintf(w, "--- BUILD FAILED %s\n%s\n", ip, r.Builds[ip])
	}
	for _, name := range r.Order {
		p := r.Packages[name]
		if p.Result != "fail" && len(p.Failed) == 0 {
			continue
		}
		// Every test that failed, and every test that never finished: a
		// timeout or panic is reported inside whichever test was running,
		// and a package can lose one test to an assertion and another to
		// the timeout in the same run.
		for _, test := range p.Tests {
			switch p.Results[test] {
			case "fail":
				fmt.Fprintf(w, "--- FAIL %s %s\n%s\n", name, test, r.TestOut[name+" "+test])
			case "":
				fmt.Fprintf(w, "--- FAIL %s %s (never finished)\n%s\n", name, test, r.TestOut[name+" "+test])
			}
		}
		// Then the package's own output: a setup failure in TestMain, or
		// the timeout panic, can sit outside every test.
		if strings.TrimSpace(p.Output) != "" {
			fmt.Fprintf(w, "--- FAIL %s (package output)\n%s\n", name, p.Output)
		}
	}
	for _, msg := range v.Problems {
		fmt.Fprintf(w, "budget: %s\n", msg)
	}
	for _, msg := range v.Notes {
		fmt.Fprintf(w, "budget note: %s\n", msg)
	}
	for _, l := range r.PerfLines {
		fmt.Fprintf(w, "perf: %s\n", l)
	}

	var ran, cached, skipped, failed int
	type slow struct {
		name string
		d    time.Duration
	}
	var pkgs []slow
	for _, name := range r.Order {
		p := r.Packages[name]
		switch {
		case p.Result == "fail":
			failed++
		case p.Result == "skip" || !p.HasTests:
			skipped++
		case p.Cached:
			cached++
		default:
			ran++
		}
		if p.Measured == 1 {
			pkgs = append(pkgs, slow{name, p.Ran})
		}
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].d > pkgs[j].d })
	tests := append([]TestTime(nil), r.Tests...)
	sort.Slice(tests, func(i, j int) bool { return tests[i].Elapsed > tests[j].Elapsed })

	fmt.Fprintf(w, "\nreceipt: test step %s against a 60s target (the gate's other steps are not in this figure)\n", wall.Round(time.Millisecond))
	fmt.Fprintf(w, "receipt: %d packages ran, %d cached, %d without tests, %d failed\n", ran, cached, skipped, failed)
	for i := 0; i < len(pkgs) && i < 5; i++ {
		fmt.Fprintf(w, "receipt: slow package %s %s\n", pkgs[i].d, pkgs[i].name)
	}
	for i := 0; i < len(tests) && i < 5; i++ {
		fmt.Fprintf(w, "receipt: slow test %s %s\n", tests[i].Elapsed.Round(time.Millisecond), strings.TrimSpace(tests[i].Name))
	}
}
