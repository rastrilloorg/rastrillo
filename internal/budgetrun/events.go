// Package budgetrun reads a `go test -json` stream and judges it: each
// package's time against its budget, the tests a lane requires, and the
// measurement every package must print.
package budgetrun

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

// measureLine is budget.Prefix plus a duration, matched as a whole line.
// The prefix is repeated here, not imported, so the CLI does not link
// package testing.
var measureLine = regexp.MustCompile(`^rastrillo-budget/v1 ran (\S+)$`)

// PerfPrefix starts the result lines perftest prints. They come from
// passing tests, whose output the report otherwise drops, so they are
// collected by prefix: a local make perf still shows every measurement.
const PerfPrefix = "rastrillo-budget/v1 perf "

type Package struct {
	ImportPath string
	Result     string // "pass", "fail", "skip", or "" if it never finished
	Cached     bool
	Ran        time.Duration
	Measured   int
	BadLine    string
	HasTests   bool
	Output     string   // package-level output, reassembled
	All        string   // every output event of the package, in order
	Failed     []string // every failed test identity, subtests included
}

type TestTime struct {
	Name    string
	Elapsed time.Duration
}

type Report struct {
	Packages  map[string]*Package
	Order     []string
	TestOut   map[string]string // "pkg TestName" -> that test's own output
	Passed    map[string]bool   // top-level test name -> passed somewhere
	Builds    map[string]string // ImportPath -> build-output
	BuildFail []string
	Malformed int
	Tests     []TestTime
	PerfLines []string
}

type event struct {
	Action     string
	Package    string
	Test       string
	Output     string
	Elapsed    float64
	ImportPath string
}

// Read consumes the whole stream. A read error is returned rather than
// treated as the end: a stream that broke after a valid prefix must not
// be judged as if it were complete.
func Read(r io.Reader, raw io.Writer) (*Report, error) {
	rep := &Report{
		Packages: map[string]*Package{},
		TestOut:  map[string]string{},
		Passed:   map[string]bool{},
		Builds:   map[string]string{},
	}
	outs := map[string]*strings.Builder{}
	alls := map[string]*strings.Builder{}
	testOuts := map[string]*strings.Builder{}
	var testOrder []string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		var e event
		if err := json.Unmarshal(line, &e); err != nil || (e.Action == "" && e.ImportPath == "") {
			// Something other than go test wrote to stdout. Pass it on so
			// nothing is hidden, and count it so the step fails: a stray
			// writer could just as well have printed a fake event.
			rep.Malformed++
			fmt.Fprintf(raw, "%s\n", line)
			continue
		}
		switch e.Action {
		case "build-output":
			rep.Builds[e.ImportPath] += e.Output
			continue
		case "build-fail":
			rep.BuildFail = append(rep.BuildFail, e.ImportPath)
			continue
		}
		if e.Package == "" {
			continue
		}
		p := rep.Packages[e.Package]
		if p == nil {
			p = &Package{ImportPath: e.Package}
			rep.Packages[e.Package] = p
			rep.Order = append(rep.Order, e.Package)
			outs[e.Package] = new(strings.Builder)
			alls[e.Package] = new(strings.Builder)
		}
		if e.Action == "output" {
			alls[e.Package].WriteString(e.Output)
		}
		if e.Test == "" {
			switch e.Action {
			case "output":
				outs[e.Package].WriteString(e.Output)
			case "pass", "fail", "skip":
				p.Result = e.Action
			}
			continue
		}
		p.HasTests = true
		key := e.Package + " " + e.Test
		top := !strings.Contains(e.Test, "/")
		switch e.Action {
		case "output":
			if testOuts[key] == nil {
				testOuts[key] = new(strings.Builder)
				testOrder = append(testOrder, key)
			}
			testOuts[key].WriteString(e.Output)
		case "pass":
			if top {
				rep.Passed[e.Test] = true
				rep.Tests = append(rep.Tests, TestTime{key, seconds(e.Elapsed)})
			}
		case "fail":
			p.Failed = append(p.Failed, e.Test)
			if top {
				rep.Tests = append(rep.Tests, TestTime{key, seconds(e.Elapsed)})
			}
		}
	}
	if err := sc.Err(); err != nil {
		return rep, fmt.Errorf("reading go test output: %w", err)
	}
	for _, key := range testOrder {
		s := testOuts[key].String()
		rep.TestOut[key] = s
		rep.PerfLines = append(rep.PerfLines, perfLines(s)...)
	}
	for _, pkg := range rep.Order {
		p := rep.Packages[pkg]
		p.Output = outs[pkg].String()
		p.All = alls[pkg].String()
		rep.PerfLines = append(rep.PerfLines, perfLines(p.Output)...)
		// test2json may split a long line across events, so the
		// measurement is read from the reassembled output, a whole line
		// at a time, never from one event's fragment.
		for _, l := range strings.Split(p.Output, "\n") {
			if m := measureLine.FindStringSubmatch(l); m != nil {
				d, err := time.ParseDuration(m[1])
				if err != nil {
					p.BadLine = l
					continue
				}
				p.Ran = d
				p.Measured++
			} else if strings.HasPrefix(l, "rastrillo-budget/") && !strings.HasPrefix(l, "rastrillo-budget/v1 ") {
				p.BadLine = l
			}
			// The (cached) field of the package summary is the only
			// reliable signal: on a hit, Elapsed measures the replay. A
			// coverage or [no tests to run] suffix may follow it.
			if strings.HasPrefix(l, "ok  \t") && strings.Contains(l, "\t(cached)") {
				p.Cached = true
			}
		}
	}
	return rep, nil
}

func perfLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), PerfPrefix) {
			out = append(out, strings.TrimPrefix(strings.TrimSpace(l), PerfPrefix))
		}
	}
	return out
}

func seconds(f float64) time.Duration { return time.Duration(f * float64(time.Second)) }
