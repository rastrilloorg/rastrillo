package budgetrun

import (
	"fmt"
	"sort"
	"strings"

	"amadan.net/rastrillo/rastrillo/internal/budgetfile"
)

type Options struct {
	File        *budgetfile.File
	ModulePath  string
	NoTime      bool
	Enforce     bool
	Require     []string
	WholeModule bool
}

type Verdict struct {
	Problems []string
	Notes    []string
}

// dirOf turns an import path into the budgets.txt key: the directory
// relative to the module root, "." for the root package.
func dirOf(modulePath, importPath string) string {
	if importPath == modulePath {
		return "."
	}
	return strings.TrimPrefix(importPath, modulePath+"/")
}

func Judge(r *Report, o Options) Verdict {
	var v Verdict
	if r.Malformed > 0 {
		v.Problems = append(v.Problems, fmt.Sprintf("%d line(s) on stdout were not go test events: something else is writing to stdout", r.Malformed))
	}
	seen := map[string]bool{}
	for _, name := range r.Order {
		p := r.Packages[name]
		if p.Result == "" {
			v.Problems = append(v.Problems, fmt.Sprintf("%s never finished: the run stopped part way, so nothing about it can be judged", name))
			continue
		}
		if !p.HasTests {
			continue // no test files, or -run matched nothing: nothing ran to measure
		}
		dir := dirOf(o.ModulePath, name)
		seen[dir] = true
		if p.BadLine != "" {
			v.Problems = append(v.Problems, fmt.Sprintf("%s: unrecognised budget line %q: is this rastrillo newer than the CLI running it?", name, p.BadLine))
			continue
		}
		if p.Result == "fail" {
			continue // already a failure; go test's own exit status carries it
		}
		switch {
		case p.Measured == 0:
			v.Problems = append(v.Problems, fmt.Sprintf("%s ran tests but printed no time: add func TestMain(m *testing.M) { os.Exit(budget.Main(m)) }", name))
			continue
		case p.Measured > 1:
			v.Problems = append(v.Problems, fmt.Sprintf("%s printed %d time lines; budget.Main must run once per binary", name, p.Measured))
			continue
		}
		if o.NoTime {
			continue
		}
		limit, why := budgetfile.DefaultTime, "the default"
		if rec, ok := o.File.Times[dir]; ok {
			limit, why = rec.Limit, fmt.Sprintf("its record on line %d", rec.Line)
		}
		if p.Ran > limit {
			msg := fmt.Sprintf("%s took %s, over the %s %s allows", dir, p.Ran, limit, why)
			if p.Cached {
				msg += " (measured on the run this cached result came from)"
			}
			if o.Enforce {
				v.Problems = append(v.Problems, msg)
			} else {
				v.Notes = append(v.Notes, msg+"; reported only, timing fails on CI")
			}
		}
	}
	if o.WholeModule && !o.NoTime {
		for dir, rec := range o.File.Times {
			if !seen[dir] {
				v.Problems = append(v.Problems, fmt.Sprintf("line %d: time record for %s, but no package there ran tests: delete it", rec.Line, dir))
			}
		}
	}
	for _, name := range o.Require {
		if !r.Passed[name] {
			v.Problems = append(v.Problems, fmt.Sprintf("required test %s did not pass in this run (missing, skipped or failed)", name))
		}
	}
	sort.Strings(v.Problems)
	return v
}
