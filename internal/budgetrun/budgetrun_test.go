package budgetrun

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/internal/budgetfile"
)

// ev builds one go test -json line.
func ev(action, pkg, test, output string, elapsed float64) string {
	b, _ := json.Marshal(map[string]any{"Action": action, "Package": pkg, "Test": test, "Output": output, "Elapsed": elapsed})
	return string(b) + "\n"
}

// pkgRun is one complete, measured package with a passing TestA.
func pkgRun(pkg, ran string, cached bool) string {
	summary := "ok  \t" + pkg + "\t0.5s\n"
	if cached {
		summary = "ok  \t" + pkg + "\t(cached)\n"
	}
	return ev("start", pkg, "", "", 0) +
		ev("run", pkg, "TestA", "", 0) +
		ev("pass", pkg, "TestA", "", 0.1) +
		ev("output", pkg, "", "PASS\n", 0) +
		ev("output", pkg, "", "rastrillo-budget/v1 ran "+ran+"\n", 0) +
		ev("output", pkg, "", summary, 0) +
		ev("pass", pkg, "", "", 0.5)
}

func read(t *testing.T, stream string) *Report {
	t.Helper()
	r, err := Read(strings.NewReader(stream), new(bytes.Buffer))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func judge(t *testing.T, stream, records string, o Options) Verdict {
	t.Helper()
	f, err := budgetfile.Parse(strings.NewReader(records))
	if err != nil {
		t.Fatal(err)
	}
	o.File = f
	if o.ModulePath == "" {
		o.ModulePath = "example.com/app"
	}
	return Judge(read(t, stream), o)
}

func TestOverBudgetFailsOnlyWhenEnforcing(t *testing.T) {
	s := pkgRun("example.com/app/internal/a", "12.3s", false)
	if v := judge(t, s, "", Options{Enforce: true}); len(v.Problems) != 1 {
		t.Fatalf("enforcing: %+v", v)
	}
	v := judge(t, s, "", Options{})
	if len(v.Problems) != 0 || len(v.Notes) == 0 {
		t.Fatalf("reporting: %+v", v)
	}
}

// Review focus 1: a cached package is judged on the time it replays.
func TestCachedPackageIsJudgedOnItsReplayedTime(t *testing.T) {
	s := pkgRun("example.com/app/internal/a", "12.3s", true)
	if p := read(t, s).Packages["example.com/app/internal/a"]; !p.Cached || p.Ran != 12300*time.Millisecond {
		t.Fatalf("package = %+v", p)
	}
	if v := judge(t, s, "", Options{Enforce: true}); len(v.Problems) != 1 {
		t.Fatalf("a cached over-budget package passed: %+v", v)
	}
}

// Go keeps a coverage or [no tests to run] suffix after (cached).
func TestCachedWithASuffixIsStillCached(t *testing.T) {
	pkg := "example.com/app/x"
	s := strings.Replace(pkgRun(pkg, "1s", true), `(cached)\n`, `(cached)\tcoverage: 80.0% of statements\n`, 1)
	if p := read(t, s).Packages[pkg]; !p.Cached {
		t.Fatalf("%+v", p)
	}
}

func TestTimeRecordRaisesTheLimit(t *testing.T) {
	s := pkgRun("example.com/app/internal/a", "12.3s", false)
	if v := judge(t, s, "time internal/a 15s slow fixture\n", Options{Enforce: true}); len(v.Problems) != 0 {
		t.Fatalf("%+v", v)
	}
}

func TestMissingMeasurementFails(t *testing.T) {
	pkg := "example.com/app/x"
	s := ev("run", pkg, "TestA", "", 0) + ev("pass", pkg, "TestA", "", 0) + ev("pass", pkg, "", "", 0.1)
	v := judge(t, s, "", Options{})
	if len(v.Problems) != 1 || !strings.Contains(v.Problems[0], "budget.Main") {
		t.Fatalf("%+v", v)
	}
}

func TestTwoMeasurementsFail(t *testing.T) {
	pkg := "example.com/app/x"
	s := strings.Replace(pkgRun(pkg, "1s", false), ev("pass", pkg, "", "", 0.5),
		ev("output", pkg, "", "rastrillo-budget/v1 ran 1s\n", 0)+ev("pass", pkg, "", "", 0.5), 1)
	if v := judge(t, s, "", Options{}); len(v.Problems) != 1 {
		t.Fatalf("%+v", v)
	}
}

func TestFragmentedMeasurementIsReassembled(t *testing.T) {
	pkg := "example.com/app/x"
	s := ev("run", pkg, "TestA", "", 0) + ev("pass", pkg, "TestA", "", 0) +
		ev("output", pkg, "", "rastrillo-budget/v1 r", 0) + ev("output", pkg, "", "an 2s\n", 0) +
		ev("pass", pkg, "", "", 0.1)
	if p := read(t, s).Packages[pkg]; p.Ran != 2*time.Second {
		t.Fatalf("%+v", p)
	}
}

func TestNoTestFilesAndEmptyRunAreExempt(t *testing.T) {
	s := ev("start", "example.com/app/nofiles", "", "", 0) +
		ev("output", "example.com/app/nofiles", "", "?   \texample.com/app/nofiles\t[no test files]\n", 0) +
		ev("skip", "example.com/app/nofiles", "", "", 0) +
		ev("output", "example.com/app/none", "", "ok  \texample.com/app/none\t0.01s [no tests to run]\n", 0) +
		ev("pass", "example.com/app/none", "", "", 0.01)
	if v := judge(t, s, "", Options{}); len(v.Problems) != 0 {
		t.Fatalf("%+v", v)
	}
}

// Review focus 3: a lane that ran nothing must not pass.
func TestRequire(t *testing.T) {
	pkg := "example.com/app/x"
	s := pkgRun(pkg, "1s", false)
	if v := judge(t, s, "", Options{Require: []string{"TestA"}}); len(v.Problems) != 0 {
		t.Fatalf("present: %+v", v)
	}
	if v := judge(t, s, "", Options{Require: []string{"TestPerfScreens"}}); len(v.Problems) != 1 {
		t.Fatalf("missing: %+v", v)
	}
	// A complete, measured package in which the required test skipped:
	// the only problem must be the requirement.
	skipped := strings.Replace(s, ev("run", pkg, "TestA", "", 0)+ev("pass", pkg, "TestA", "", 0.1),
		ev("run", pkg, "TestPerfScreens", "", 0)+ev("skip", pkg, "TestPerfScreens", "", 0), 1)
	v := judge(t, skipped, "", Options{Require: []string{"TestPerfScreens"}})
	if len(v.Problems) != 1 || !strings.Contains(v.Problems[0], "TestPerfScreens") {
		t.Fatalf("skipped: %+v", v)
	}
}

func TestNoTimeTurnsJudgingOff(t *testing.T) {
	if v := judge(t, pkgRun("example.com/app/x", "99s", false), "", Options{NoTime: true, Enforce: true}); len(v.Problems) != 0 {
		t.Fatalf("%+v", v)
	}
}

func TestStaleTimeRecordOnlyOnWholeModule(t *testing.T) {
	s := pkgRun("example.com/app/x", "1s", false)
	rec := "time internal/gone 20s once slow\n"
	if v := judge(t, s, rec, Options{WholeModule: true}); len(v.Problems) != 1 {
		t.Fatalf("whole module: %+v", v)
	}
	if v := judge(t, s, rec, Options{}); len(v.Problems) != 0 {
		t.Fatalf("narrow run: %+v", v)
	}
}

func TestUnfinishedPackageFails(t *testing.T) {
	pkg := "example.com/app/x"
	s := ev("start", pkg, "", "", 0) + ev("run", pkg, "TestA", "", 0)
	v := judge(t, s, "", Options{})
	if len(v.Problems) != 1 || !strings.Contains(v.Problems[0], "never finished") {
		t.Fatalf("%+v", v)
	}
}

func TestMalformedLinesAreCountedAndEchoed(t *testing.T) {
	var raw bytes.Buffer
	r, err := Read(strings.NewReader("not json\n"+pkgRun("example.com/app/x", "1s", false)), &raw)
	if err != nil {
		t.Fatal(err)
	}
	if r.Malformed != 1 || !strings.Contains(raw.String(), "not json") {
		t.Fatalf("malformed=%d raw=%q", r.Malformed, raw.String())
	}
	if v := judge(t, "not json\n", "", Options{}); len(v.Problems) != 1 {
		t.Fatalf("%+v", v)
	}
}

type failingReader struct{ r io.Reader }

func (f failingReader) Read(p []byte) (int, error) {
	n, err := f.r.Read(p)
	if err == io.EOF {
		return n, errors.New("pipe broke")
	}
	return n, err
}

func TestReadErrorIsReturned(t *testing.T) {
	_, err := Read(failingReader{strings.NewReader(pkgRun("example.com/app/x", "1s", false))}, io.Discard)
	if err == nil {
		t.Fatal("a broken stream read as complete")
	}
}

func printed(t *testing.T, stream string) string {
	t.Helper()
	var out bytes.Buffer
	Print(&out, read(t, stream), Verdict{}, time.Second)
	return out.String()
}

func TestBuildFailureOutputIsKept(t *testing.T) {
	b, _ := json.Marshal(map[string]any{"Action": "build-output", "ImportPath": "example.com/app/x [example.com/app/x.test]", "Output": "x.go:3:1: syntax error\n"})
	f, _ := json.Marshal(map[string]any{"Action": "build-fail", "ImportPath": "example.com/app/x [example.com/app/x.test]"})
	if out := printed(t, string(b)+"\n"+string(f)+"\n"); !strings.Contains(out, "syntax error") {
		t.Fatalf("build output dropped:\n%s", out)
	}
}

func TestFailedTestOutputLeadsTheReport(t *testing.T) {
	pkg := "example.com/app/x"
	s := ev("run", pkg, "TestBad", "", 0) + ev("output", pkg, "TestBad", "    x_test.go:9: boom\n", 0) + ev("fail", pkg, "TestBad", "", 0.1) +
		ev("output", pkg, "", "rastrillo-budget/v1 ran 1s\n", 0) + ev("fail", pkg, "", "", 0.2)
	out := printed(t, s)
	if !strings.HasPrefix(strings.TrimSpace(out), "--- FAIL") || !strings.Contains(out, "boom") {
		t.Fatalf("report does not lead with the failure:\n%s", out)
	}
}

func TestFailedSubtestOutputIsKept(t *testing.T) {
	pkg := "example.com/app/x"
	s := ev("run", pkg, "TestA", "", 0) + ev("run", pkg, "TestA/sub", "", 0) +
		ev("output", pkg, "TestA/sub", "    x_test.go:12: sub boom\n", 0) +
		ev("fail", pkg, "TestA/sub", "", 0) + ev("fail", pkg, "TestA", "", 0) + ev("fail", pkg, "", "", 0.1)
	if out := printed(t, s); !strings.Contains(out, "sub boom") {
		t.Fatalf("subtest assertion lost:\n%s", out)
	}
}

// A panic or timeout inside a running test fails the package with no
// test-level fail; the whole ordered output is the only explanation.
func TestAbnormalFailurePrintsEverything(t *testing.T) {
	pkg := "example.com/app/x"
	s := ev("run", pkg, "TestA", "", 0) + ev("output", pkg, "TestA", "panic: test timed out after 10m0s\n", 0) +
		ev("output", pkg, "", "FAIL\texample.com/app/x\t600.1s\n", 0) + ev("fail", pkg, "", "", 600)
	if out := printed(t, s); !strings.Contains(out, "test timed out") {
		t.Fatalf("panic attributed to a running test was dropped:\n%s", out)
	}
}

func TestPerfLinesAreKept(t *testing.T) {
	pkg := "example.com/app/x"
	s := strings.Replace(pkgRun(pkg, "1s", false), ev("pass", pkg, "TestA", "", 0.1),
		ev("output", pkg, "TestA", "rastrillo-budget/v1 perf GET /: median 3ms, worst 5ms, budget 150ms\n", 0)+ev("pass", pkg, "TestA", "", 0.1), 1)
	if out := printed(t, s); !strings.Contains(out, "GET /: median 3ms") {
		t.Fatalf("perf result line dropped:\n%s", out)
	}
}
