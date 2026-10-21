package ui

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
)

// light.js holds to the contract every scaffolded script does, with a
// 12 KiB cap of its own (select.js's precedent; the header comment is
// its contract and is a third of the file): the properties it writes,
// named; reduced motion and forced colours read as live queries; one
// frame loop; a WeakMap for the cards; the hour; the hook; and nothing
// it must never do. Startup order is behaviour, not text:
// TestReducedMotionIsTotal proves nothing is scheduled under reduce.
func TestLightContract(t *testing.T) {
	js := string(LightJS())
	for _, want := range []string{
		"--rst-light-x", "--rst-light-y", "--rst-mx", "--rst-my", "--rst-px", "--rst-py", "--rst-card-x", "--rst-card-y", "--rst-card-lit",
		`matchMedia("(prefers-reduced-motion: reduce)")`, `matchMedia("(forced-colors: active)")`, `.addEventListener("change"`,
		"new WeakMap()", "requestAnimationFrame", "visibilitychange", "5 * 60 * 1000", "clearTimeout",
		`"rst:light"`, "pointerType", "isConnected", `querySelectorAll('[style*="--rst-"]')`,
		"var FOLLOW = ",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("light.js does not mention %q", want)
		}
	}
	for _, bad := range []string{"setPointerCapture", "AmbientLightSensor", "deviceorientation", "touchstart", "rst-switch-track", "rst-switch-x"} {
		if strings.Contains(js, bad) {
			t.Errorf("light.js mentions %q, which it must never do", bad)
		}
	}
	if !strings.HasPrefix(strings.TrimSpace(js), "/*") || !strings.Contains(js, "(function () {") || !strings.HasSuffix(strings.TrimSpace(js), "})();") {
		t.Error("light.js should be its contract comment and a single IIFE")
	}
	if strings.Contains(js, "eval(") || strings.Contains(js, "new Function") || strings.Contains(js, "\t") {
		t.Error("light.js must stay CSP-clean and use two-space indentation")
	}
	if n := len(js); n > 12*1024 {
		t.Fatalf("light.js is %d bytes; keep it readable in one sitting (12 KiB)", n)
	}
}

// traceTotals sums the duration of the named events that begin inside
// the window between two console.timeStamp() marks (TimeStamp events
// in devtools.timeline, on the trace's own clock): complete events
// ("X") by their dur, and begin/end pairs ("B"/"E") by their
// timestamps. The pairs are matched on a stack per thread (pid and
// tid), the way the trace nests them (a Layout inside an
// UpdateLayoutTree), and an "E" that carries no name closes whichever
// "B" is open on its thread. Inside the window the accounting has to
// close: a named "B" with no "E", an "E" with nothing open, or an "E"
// whose name is not the open "B"'s is an error rather than a smaller
// total, so an undercount never passes as a figure. Outside the window
// nothing is summed or checked: Tracing.end lands wherever it lands,
// and this Chromium reports whatever slice it cut as a lone "B". It
// also counts each name, so a trace that carried none of them fails
// rather than measuring nothing. A slice nested inside one of the same
// name is summed twice, which errs toward a larger figure. The
// per-name sums are in milliseconds, for the log.
func traceTotals(events []json.RawMessage, from, to string, names ...string) (total float64, counts map[string]int, sums map[string]float64, err error) {
	marks := map[string]float64{}
	for _, raw := range events {
		var e struct {
			Name string  `json:"name"`
			Ts   float64 `json:"ts"`
			Args struct {
				Data struct {
					Message string `json:"message"`
				} `json:"data"`
			} `json:"args"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Name == "TimeStamp" {
			marks[e.Args.Data.Message] = e.Ts
		}
	}
	lo, hi := marks[from], marks[to]
	if lo == 0 || hi == 0 || hi <= lo {
		return 0, nil, nil, fmt.Errorf("the trace carries no window %q..%q (marks %v)", from, to, marks)
	}
	inside := func(ts float64) bool { return ts >= lo && ts <= hi }
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	counts, sums = map[string]int{}, map[string]float64{}
	type span struct {
		name string
		ts   float64
	}
	open := map[string][]span{}
	for _, raw := range events {
		var e struct {
			Name string          `json:"name"`
			Ph   string          `json:"ph"`
			Dur  float64         `json:"dur"`
			Ts   float64         `json:"ts"`
			Pid  json.RawMessage `json:"pid"`
			Tid  json.RawMessage `json:"tid"`
		}
		if json.Unmarshal(raw, &e) != nil {
			continue
		}
		thread := string(e.Pid) + "/" + string(e.Tid)
		switch e.Ph {
		case "X":
			if want[e.Name] && inside(e.Ts) {
				total += e.Dur
				counts[e.Name]++
				sums[e.Name] += e.Dur / 1000
			}
		case "B":
			open[thread] = append(open[thread], span{e.Name, e.Ts})
		case "E":
			n := len(open[thread])
			if n == 0 {
				if inside(e.Ts) && (e.Name == "" || want[e.Name]) {
					return 0, nil, nil, fmt.Errorf("an E event %q at %v on thread %s has no B", e.Name, e.Ts, thread)
				}
				continue
			}
			top := open[thread][n-1]
			if e.Name != "" && e.Name != top.name {
				if inside(e.Ts) {
					return 0, nil, nil, fmt.Errorf("an E event %q at %v on thread %s closes a B named %q", e.Name, e.Ts, thread, top.name)
				}
				continue
			}
			open[thread] = open[thread][:n-1]
			if want[top.name] && inside(top.ts) {
				total += e.Ts - top.ts
				counts[top.name]++
				sums[top.name] += (e.Ts - top.ts) / 1000
			}
		}
	}
	for thread, spans := range open {
		for _, s := range spans {
			if want[s.name] && inside(s.ts) {
				return 0, nil, nil, fmt.Errorf("a B event %q at %v on thread %s has no E", s.name, s.ts, thread)
			}
		}
	}
	for n, ms := range sums {
		sums[n] = math.Round(ms*10) / 10
	}
	return total, counts, sums, nil
}

// TestTraceTotalsAccounting holds traceTotals to its rules on synthetic
// events, since the real traces only ever show it one shape: two
// threads interleaved, an unnamed E, a lone B inside the window and
// one outside it, a mismatched E, an orphan E, and a missing mark.
func TestTraceTotalsAccounting(t *testing.T) {
	ev := func(items ...string) []json.RawMessage {
		var out []json.RawMessage
		for _, s := range items {
			out = append(out, json.RawMessage(s))
		}
		return out
	}
	mark := func(name string, ts float64) string {
		return fmt.Sprintf(`{"name":"TimeStamp","ph":"I","ts":%v,"pid":1,"tid":1,"args":{"data":{"message":%q}}}`, ts, name)
	}
	marks := []string{mark("a", 1000), mark("b", 2000)}
	ok := append(marks,
		`{"name":"Paint","ph":"X","ts":1100,"dur":300,"pid":1,"tid":1}`,
		`{"name":"Paint","ph":"X","ts":900,"dur":300,"pid":1,"tid":1}`,
		`{"name":"Layout","ph":"B","ts":1200,"pid":1,"tid":1}`,
		`{"name":"Layout","ph":"B","ts":1210,"pid":1,"tid":2}`,
		`{"name":"","ph":"E","ts":1250,"pid":1,"tid":2}`,
		`{"name":"Layout","ph":"E","ts":1300,"pid":1,"tid":1}`,
		`{"name":"Other","ph":"B","ts":1400,"pid":1,"tid":1}`,
		`{"name":"","ph":"E","ts":1500,"pid":1,"tid":1}`,
		`{"name":"Layout","ph":"B","ts":2100,"pid":1,"tid":1}`,
		`{"name":"Paint","ph":"E","ts":800,"pid":1,"tid":3}`,
	)
	total, counts, sums, err := traceTotals(ev(ok...), "a", "b", "Paint", "Layout")
	if err != nil {
		t.Fatalf("a closed window errored: %v", err)
	}
	if total != 440 || counts["Paint"] != 1 || counts["Layout"] != 2 || sums["Layout"] != 0.1 {
		t.Errorf("total %v counts %v sums %v; want 440 (one Paint of 300 inside the window, Layouts of 100 and 40 on two threads), Paint 1, Layout 2, Layout 0.1ms", total, counts, sums)
	}
	for name, bad := range map[string][]string{
		"a lone B inside the window": append(marks, `{"name":"Layout","ph":"B","ts":1500,"pid":1,"tid":1}`),
		"an orphan named E inside":   append(marks, `{"name":"Layout","ph":"E","ts":1500,"pid":1,"tid":1}`),
		"an orphan unnamed E inside": append(marks, `{"name":"","ph":"E","ts":1500,"pid":1,"tid":1}`),
		"a mismatched E inside":      append(marks, `{"name":"Other","ph":"B","ts":1400,"pid":1,"tid":1}`, `{"name":"Layout","ph":"E","ts":1500,"pid":1,"tid":1}`),
		"no end mark":                {mark("a", 1000)},
	} {
		if _, _, _, err := traceTotals(ev(bad...), "a", "b", "Paint", "Layout"); err == nil {
			t.Errorf("%s: no error; the figure would undercount", name)
		}
	}
}
