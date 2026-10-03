package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// budgetFixture writes a module with one package whose TestMain calls
// budget.Main and whose test sleeps for sleep, replacing rastrillo with
// this checkout.
func budgetFixture(t *testing.T, sleep string, records string) string {
	t.Helper()
	setSandboxGoEnv(t)
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/fx\n\ngo 1.25.0\n\nrequire amadan.net/rastrillo/rastrillo v0.0.0\n\nreplace amadan.net/rastrillo/rastrillo => " + repoRoot(t) + "\n",
		"internal/a/a_test.go": `package a

import (
	"os"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/budget"
)

func TestMain(m *testing.M) { os.Exit(budget.Main(m)) }

func TestSleep(t *testing.T) { d, _ := time.ParseDuration("` + sleep + `"); time.Sleep(d) }
`,
	}
	if records != "" {
		files[".rastrillo/budgets.txt"] = records
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = dir
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, out)
	}
	return dir
}

func env(kv ...string) func(string) string {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return func(k string) string { return m[k] }
}

var tookOverBudget = regexp.MustCompile(`internal/a took 1\.2\d*s, over the 1s`)

// Review focus 1: the retry of an over-budget package, now served from
// the cache, fails exactly as the first run did, on the replayed time.
func TestBudgetTestCachedRetryStillFails(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test on a fixture module")
	}
	dir := budgetFixture(t, "1200ms", "time internal/a 1s fixture sleeps on purpose\n")
	for run := 1; run <= 2; run++ {
		var out bytes.Buffer
		err := budgetTest([]string{"./..."}, dir, &out, io.Discard, env("AMADAN_CI", "1"))
		if exitCode(t, err) != 1 || !tookOverBudget.MatchString(out.String()) {
			t.Fatalf("run %d: exit %d, want 1 with the time violation:\n%s", run, exitCode(t, err), out.String())
		}
		wantCached := "0 cached"
		if run == 2 {
			wantCached = "1 cached"
		}
		if !strings.Contains(out.String(), wantCached) {
			t.Fatalf("run %d: want %q in the receipt:\n%s", run, wantCached, out.String())
		}
	}
	var out bytes.Buffer
	if err := budgetTest([]string{"./..."}, dir, &out, io.Discard, env()); err != nil {
		t.Fatalf("without CI it must only report: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "reported only") {
		t.Fatalf("no report line:\n%s", out.String())
	}
}

func TestBudgetTestLoweredRecordFailsTheCachedPackage(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test on a fixture module")
	}
	dir := budgetFixture(t, "1200ms", "time internal/a 5s fixture\n")
	if err := budgetTest([]string{"./..."}, dir, io.Discard, io.Discard, env("CI", "true")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".rastrillo", "budgets.txt"), []byte("time internal/a 1s tightened\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := budgetTest([]string{"./..."}, dir, &out, io.Discard, env("CI", "true"))
	if exitCode(t, err) != 1 || !strings.Contains(out.String(), "1 cached") || !tookOverBudget.MatchString(out.String()) {
		t.Fatalf("a tightened record must fail the cached package: exit %d\n%s", exitCode(t, err), out.String())
	}
}

// Review focus 2: go test's own status wins over whatever the stream
// managed to say before it stopped. The same stream with a clean exit
// passes first, so the failure below can only come from the kill.
func TestBudgetTestKilledGoTestIsNotGreen(t *testing.T) {
	events := []string{
		`{"Action":"start","Package":"example.com/fx/a"}`,
		`{"Action":"run","Package":"example.com/fx/a","Test":"TestA"}`,
		`{"Action":"pass","Package":"example.com/fx/a","Test":"TestA","Elapsed":0.01}`,
		`{"Action":"output","Package":"example.com/fx/a","Output":"rastrillo-budget/v1 ran 10ms\n"}`,
		`{"Action":"pass","Package":"example.com/fx/a","Elapsed":0.02}`,
	}
	script := func(tail string) string {
		s := "#!/bin/sh\n"
		for _, e := range events {
			s += "printf '%s\\n' '" + e + "'\n"
		}
		return s + tail + "\n"
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/fx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(t.TempDir(), "go")
	old := goCommand
	goCommand = fake
	t.Cleanup(func() { goCommand = old })

	if err := os.WriteFile(fake, []byte(script("exit 0")), 0o755); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := budgetTest([]string{"./..."}, dir, &out, io.Discard, env()); err != nil {
		t.Fatalf("the clean stream must pass: %v\n%s", err, out.String())
	}
	if err := os.WriteFile(fake, []byte(script("kill -9 $$")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := budgetTest([]string{"./..."}, dir, io.Discard, io.Discard, env()); err == nil {
		t.Fatal("a go test killed mid-run read as green")
	}
}

func TestBudgetTestMissingMainFails(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test on a fixture module")
	}
	dir := budgetFixture(t, "1ms", "")
	src := filepath.Join(dir, "internal", "a", "a_test.go")
	b, _ := os.ReadFile(src)
	b = []byte(strings.Replace(string(b), "func TestMain(m *testing.M) { os.Exit(budget.Main(m)) }", "var _ = budget.Prefix\nvar _ = os.Exit", 1))
	if err := os.WriteFile(src, b, 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := budgetTest([]string{"./..."}, dir, &out, io.Discard, env()); exitCode(t, err) != 1 || !strings.Contains(out.String(), "budget.Main") {
		t.Fatalf("err=%v\n%s", err, out.String())
	}
}

func TestPackageArgs(t *testing.T) {
	cases := []struct {
		args []string
		want []string
	}{
		{[]string{"./..."}, []string{"./..."}},
		{[]string{"-count=1", "./..."}, []string{"./..."}},
		{[]string{"-count", "1", "./..."}, []string{"./..."}},
		{[]string{"-tags", "perf", "-v", "-run", "^TestPerf", "./..."}, []string{"./..."}},
		{[]string{"-short", "./internal/a"}, []string{"./internal/a"}},
		{nil, nil},
	}
	for _, c := range cases {
		if got := packageArgs(c.args); strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("packageArgs(%q) = %q, want %q", c.args, got, c.want)
		}
	}
}

func TestBudgetSizeExitCodes(t *testing.T) {
	root := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/a\n"), 0o644))
	must(os.MkdirAll(filepath.Join(root, "internal", "a"), 0o755))
	must(os.WriteFile(filepath.Join(root, "internal", "a", "a.go"), []byte(strings.Repeat("//\n", 10)), 0o644))

	var out bytes.Buffer
	if err := budgetSize([]string{root}, &out); err != nil {
		t.Fatalf("clean module: %v\n%s", err, out.String())
	}

	must(os.WriteFile(filepath.Join(root, "internal", "a", "b.go"), []byte(strings.Repeat("//\n", 5000)), 0o644))
	out.Reset()
	var ex exitError
	if err := budgetSize([]string{root}, &out); !errors.As(err, &ex) || ex.code != 1 {
		t.Fatalf("over budget: err = %v, want exit 1\n%s", err, out.String())
	}

	must(os.MkdirAll(filepath.Join(root, ".rastrillo"), 0o755))
	must(os.WriteFile(filepath.Join(root, ".rastrillo", "budgets.txt"), []byte("size ./internal/a 6000 - x\n"), 0o644))
	out.Reset()
	if err := budgetSize([]string{root}, &out); !errors.As(err, &ex) || ex.code != 2 {
		t.Fatalf("malformed file: err = %v, want exit 2", err)
	}
}

// writeModule writes a fixture module replacing rastrillo with this
// checkout, and tidies it.
func writeModule(t *testing.T, files map[string]string) string {
	t.Helper()
	setSandboxGoEnv(t)
	dir := t.TempDir()
	files["go.mod"] = "module example.com/fx\n\ngo 1.25.0\n\nrequire amadan.net/rastrillo/rastrillo v0.0.0\n\nreplace amadan.net/rastrillo/rastrillo => " + repoRoot(t) + "\n"
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = dir
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, out)
	}
	return dir
}

const measuredMain = `
func TestMain(m *testing.M) { os.Exit(budget.Main(m)) }
`

const testImports = `import (
	"fmt"
	"os"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/budget"
)

var _, _ = fmt.Sprint, time.Second
`

// The failures go test really produces, not hand-written events: a red
// run must lead with each one's own explanation, whatever the event
// attribution of the Go release doing the run.
func TestBudgetTestReportsRealFailures(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test on a fixture module")
	}
	dir := writeModule(t, map[string]string{
		"vetbad/v_test.go": "package vetbad\n\n" + testImports + measuredMain +
			"func TestV(t *testing.T) { t.Log(fmt.Sprintf(\"%d\", \"not a number\")) }\n",
		"setup/s_test.go": "package setup\n\n" + testImports +
			"func TestMain(m *testing.M) { fmt.Println(\"setup boom\"); _ = budget.Prefix; os.Exit(1) }\n\nfunc TestS(t *testing.T) {}\n",
		"panics/p_test.go": "package panics\n\n" + testImports + measuredMain +
			"func TestP(t *testing.T) { panic(\"kaboom\") }\n",
		"slow/w_test.go": "package slow\n\n" + testImports + measuredMain +
			"func TestA(t *testing.T) { t.Error(\"first failure\") }\n\nfunc TestB(t *testing.T) { time.Sleep(time.Minute) }\n",
	})
	var out bytes.Buffer
	err := budgetTest([]string{"-timeout", "5s", "./..."}, dir, &out, &out, env())
	if err == nil {
		t.Fatalf("a run with four failing packages passed:\n%s", out.String())
	}
	for _, want := range []string{"Sprintf format %d has arg", "setup boom", "kaboom", "first failure", "test timed out"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the report lost %q:\n%s", want, out.String())
		}
	}
}
