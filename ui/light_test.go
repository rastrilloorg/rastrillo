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

// traceWindow is what traceTotals reads from one marked window.
type traceWindow struct {
	Thread float64            // the named events' thread (CPU) time, µs
	Wall   float64            // the same events' wall-clock time, µs, for the log
	Counts map[string]int     // the named events, by name
	Sums   map[string]float64 // their thread time by name, ms, for the log
	Tasks  int                // RunTask slices on the page's main thread
	Long   int                // of those, the ones over 50ms of thread time
}

// longTaskUS is the long-task line, 50ms, applied to a task's thread
// time rather than its wall time.
const longTaskUS = 50000

// traceTotals sums the thread time of the named events that begin
// inside the window between two console.timeStamp() marks (TimeStamp
// events in devtools.timeline, on the trace's own clock): complete
// events ("X") by their tdur, and begin/end pairs ("B"/"E") by the
// difference of their tts. Thread time, not wall time, because wall
// time counts the moments the renderer waited for a CPU: on a fleet
// runner beside thirty other shards a 2.3ms frame read 6 to 12ms and a
// task of 2 to 11ms of work read 50 to 123ms, so the budget and the
// long-task rule failed on the machine's load, never the page's (#118).
// An event that carries no thread clock is an error rather than a
// zero, so a trace without one never passes as a cheap figure.
//
// The pairs are matched on a stack per thread (pid and tid), the way
// the trace nests them (a Layout inside an UpdateLayoutTree), and an
// "E" that carries no name closes whichever "B" is open on its thread.
// Inside the window the accounting has to close: a named "B" with no
// "E", an "E" with nothing open, or an "E" whose name is not the open
// "B"'s is an error rather than a smaller total, so an undercount never
// passes as a figure. Outside the window nothing is summed or checked:
// Tracing.end lands wherever it lands, and this Chromium reports
// whatever slice it cut as a lone "B". It also counts each name, so a
// trace that carried none of them fails rather than measuring nothing.
// A slice nested inside one of the same name is summed twice, which
// errs toward a larger figure.
//
// It also counts the RunTask slices ("X", disabled-by-default-
// devtools.timeline) that begin inside the window on the page's main
// thread, the one that carried the most named events, and the long
// ones among them by thread time: a task that did 50ms of work is a
// long task on any machine, and one that only waited is not.
func traceTotals(events []json.RawMessage, from, to string, names ...string) (w traceWindow, err error) {
	type event struct {
		Name string          `json:"name"`
		Ph   string          `json:"ph"`
		Dur  float64         `json:"dur"`
		Tdur *float64        `json:"tdur"`
		Ts   float64         `json:"ts"`
		Tts  *float64        `json:"tts"`
		Pid  json.RawMessage `json:"pid"`
		Tid  json.RawMessage `json:"tid"`
		Args struct {
			Data struct {
				Message string `json:"message"`
			} `json:"data"`
		} `json:"args"`
	}
	var evs []event
	marks := map[string]float64{}
	for _, raw := range events {
		var e event
		if json.Unmarshal(raw, &e) != nil {
			continue
		}
		evs = append(evs, e)
		if e.Name == "TimeStamp" {
			marks[e.Args.Data.Message] = e.Ts
		}
	}
	lo, hi := marks[from], marks[to]
	if lo == 0 || hi == 0 || hi <= lo {
		return w, fmt.Errorf("the trace carries no window %q..%q (marks %v)", from, to, marks)
	}
	inside := func(ts float64) bool { return ts >= lo && ts <= hi }
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	w.Counts, w.Sums = map[string]int{}, map[string]float64{}
	perThread := map[string]int{}
	add := func(name, thread string, wall, cpu float64) {
		w.Wall += wall
		w.Thread += cpu
		w.Counts[name]++
		w.Sums[name] += cpu / 1000
		perThread[thread]++
	}
	type span struct {
		name string
		ts   float64
		tts  *float64
	}
	open := map[string][]span{}
	for _, e := range evs {
		thread := string(e.Pid) + "/" + string(e.Tid)
		switch e.Ph {
		case "X":
			if want[e.Name] && inside(e.Ts) {
				if e.Tdur == nil {
					return w, fmt.Errorf("an X event %q at %v on thread %s carries no tdur, so its thread time is unknown", e.Name, e.Ts, thread)
				}
				add(e.Name, thread, e.Dur, *e.Tdur)
			}
		case "B":
			open[thread] = append(open[thread], span{e.Name, e.Ts, e.Tts})
		case "E":
			n := len(open[thread])
			if n == 0 {
				if inside(e.Ts) && (e.Name == "" || want[e.Name]) {
					return w, fmt.Errorf("an E event %q at %v on thread %s has no B", e.Name, e.Ts, thread)
				}
				continue
			}
			top := open[thread][n-1]
			if e.Name != "" && e.Name != top.name {
				if inside(e.Ts) {
					return w, fmt.Errorf("an E event %q at %v on thread %s closes a B named %q", e.Name, e.Ts, thread, top.name)
				}
				continue
			}
			open[thread] = open[thread][:n-1]
			if want[top.name] && inside(top.ts) {
				if top.tts == nil || e.Tts == nil {
					return w, fmt.Errorf("a %q pair at %v on thread %s carries no tts, so its thread time is unknown", top.name, top.ts, thread)
				}
				add(top.name, thread, e.Ts-top.ts, *e.Tts-*top.tts)
			}
		}
	}
	for thread, spans := range open {
		for _, s := range spans {
			if want[s.name] && inside(s.ts) {
				return w, fmt.Errorf("a B event %q at %v on thread %s has no E", s.name, s.ts, thread)
			}
		}
	}
	for n, ms := range w.Sums {
		w.Sums[n] = math.Round(ms*10) / 10
	}
	main, most := "", 0
	for thread, n := range perThread {
		if n > most {
			main, most = thread, n
		}
	}
	for _, e := range evs {
		if e.Ph != "X" || e.Name != "RunTask" || !inside(e.Ts) || string(e.Pid)+"/"+string(e.Tid) != main {
			continue
		}
		if e.Tdur == nil {
			return w, fmt.Errorf("a RunTask at %v on the main thread carries no tdur, so a long task would go unseen", e.Ts)
		}
		w.Tasks++
		if *e.Tdur > longTaskUS {
			w.Long++
		}
	}
	return w, nil
}

// TestTraceTotalsAccounting holds traceTotals to its rules on synthetic
// events, since the real traces only ever show it one shape: two
// threads interleaved, an unnamed E, a lone B inside the window and
// one outside it, a mismatched E, an orphan E, a missing mark, an event
// with no thread clock, and tasks that waited against tasks that worked.
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
		`{"name":"Paint","ph":"X","ts":1100,"dur":300,"tdur":200,"pid":1,"tid":1}`,
		`{"name":"Paint","ph":"X","ts":900,"dur":300,"pid":1,"tid":1}`,
		`{"name":"Layout","ph":"B","ts":1200,"tts":50,"pid":1,"tid":1}`,
		`{"name":"Layout","ph":"B","ts":1210,"tts":500,"pid":1,"tid":2}`,
		`{"name":"","ph":"E","ts":1250,"tts":530,"pid":1,"tid":2}`,
		`{"name":"Layout","ph":"E","ts":1300,"tts":110,"pid":1,"tid":1}`,
		`{"name":"Other","ph":"B","ts":1400,"pid":1,"tid":1}`,
		`{"name":"","ph":"E","ts":1500,"pid":1,"tid":1}`,
		`{"name":"Layout","ph":"B","ts":2100,"pid":1,"tid":1}`,
		`{"name":"Paint","ph":"E","ts":800,"pid":1,"tid":3}`,
		// Tasks: on the main thread (tid 1, two named events) one that
		// waited 120ms but worked 9, one that worked 60, one outside the
		// window; on tid 2 a long one that is not the page's.
		`{"name":"RunTask","ph":"X","ts":1050,"dur":120000,"tdur":9000,"pid":1,"tid":1}`,
		`{"name":"RunTask","ph":"X","ts":1600,"dur":61000,"tdur":60000,"pid":1,"tid":1}`,
		`{"name":"RunTask","ph":"X","ts":2500,"dur":90000,"tdur":90000,"pid":1,"tid":1}`,
		`{"name":"RunTask","ph":"X","ts":1700,"dur":90000,"tdur":90000,"pid":1,"tid":2}`,
	)
	w, err := traceTotals(ev(ok...), "a", "b", "Paint", "Layout")
	if err != nil {
		t.Fatalf("a closed window errored: %v", err)
	}
	if w.Thread != 290 || w.Wall != 440 || w.Counts["Paint"] != 1 || w.Counts["Layout"] != 2 || w.Sums["Layout"] != 0.1 {
		t.Errorf("thread %v wall %v counts %v sums %v; want thread 290 (a Paint of 200 inside the window, Layouts of 60 and 30 on two threads), wall 440, Paint 1, Layout 2, Layout 0.1ms", w.Thread, w.Wall, w.Counts, w.Sums)
	}
	if w.Tasks != 2 || w.Long != 1 {
		t.Errorf("tasks %d, long %d; want 2 tasks on the main thread inside the window, 1 long by thread time (the 120ms wait that worked 9ms is not)", w.Tasks, w.Long)
	}
	for name, bad := range map[string][]string{
		"a lone B inside the window":  append(marks, `{"name":"Layout","ph":"B","ts":1500,"pid":1,"tid":1}`),
		"an orphan named E inside":    append(marks, `{"name":"Layout","ph":"E","ts":1500,"pid":1,"tid":1}`),
		"an orphan unnamed E inside":  append(marks, `{"name":"","ph":"E","ts":1500,"pid":1,"tid":1}`),
		"a mismatched E inside":       append(marks, `{"name":"Other","ph":"B","ts":1400,"pid":1,"tid":1}`, `{"name":"Layout","ph":"E","ts":1500,"pid":1,"tid":1}`),
		"no end mark":                 {mark("a", 1000)},
		"an X with no thread clock":   append(marks, `{"name":"Paint","ph":"X","ts":1100,"dur":300,"pid":1,"tid":1}`),
		"a pair with no thread clock": append(marks, `{"name":"Layout","ph":"B","ts":1200,"pid":1,"tid":1}`, `{"name":"Layout","ph":"E","ts":1300,"pid":1,"tid":1}`),
		"a main-thread task with no thread clock": append(marks, `{"name":"Paint","ph":"X","ts":1100,"dur":300,"tdur":200,"pid":1,"tid":1}`,
			`{"name":"RunTask","ph":"X","ts":1050,"dur":120000,"pid":1,"tid":1}`),
	} {
		if _, err := traceTotals(ev(bad...), "a", "b", "Paint", "Layout"); err == nil {
			t.Errorf("%s: no error; the figure would undercount", name)
		}
	}
}
