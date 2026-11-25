package waste

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/transcript"
)

// fixture is one real Bash result kept from a groomer run, command and
// all: testdata/<name>.json holds what claude recorded.
func fixture(t *testing.T, name string) transcript.Tool {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var kept struct {
		Command string
		IsError bool `json:"is_error"`
		Result  string
	}
	if err := json.Unmarshal(raw, &kept); err != nil {
		t.Fatal(err)
	}
	return bash(kept.Command, kept.Result, kept.IsError)
}

func bash(command, result string, isError bool) transcript.Tool {
	return transcript.Tool{
		Name:    "Bash",
		Input:   map[string]any{"command": command},
		Result:  result,
		IsError: isError,
	}
}

// run is a transcript with one message per list of tools.
func run(messages ...[]transcript.Tool) transcript.Transcript {
	var run transcript.Transcript
	for _, tools := range messages {
		run.Messages = append(run.Messages, transcript.Message{Tools: tools})
	}
	return run
}

func only(t *testing.T, tool transcript.Tool) Mistake {
	t.Helper()
	found := Find(run([]transcript.Tool{tool}))
	if len(found) != 1 {
		t.Fatalf("found %d mistakes, want 1: %+v", len(found), found)
	}
	return found[0]
}

func TestGoCompileErrorsAreFound(t *testing.T) {
	inline := bash("go build ./...", "# groomer/internal/groomer\n"+
		"./bookings.go:12:3: undefined: x\n"+
		"FAIL\tgroomer/internal/groomer [build failed]\n", true)
	for name, tool := range map[string]transcript.Tool{
		"inline":            inline,
		"a real piped test": fixture(t, "piped-go-test-compile"),
	} {
		mistake := only(t, tool)
		if mistake.Kind != GoCompile {
			t.Errorf("%s: kind = %s, want %s", name, mistake.Kind, GoCompile)
		}
	}
	if got := only(t, inline).Detail; !strings.Contains(got, "undefined: x") {
		t.Errorf("detail = %q, want the compile error", got)
	}
}

// html/template names the file, line and column of what it cannot
// evaluate; Rastrillo's compiled templates say when they are stale.
func TestTemplateErrorsAreFound(t *testing.T) {
	for _, name := range []string{"template-field", "template-stale"} {
		if got := only(t, fixture(t, name)).Kind; got != Template {
			t.Errorf("%s: kind = %s, want %s", name, got, Template)
		}
	}
	other := bash("go test ./...", "tmplrt: the code was compiled for other "+
		"types; run rastrillo generate: .Name is string, was int\n", false)
	if got := only(t, other).Kind; got != Template {
		t.Errorf("tmplrt types message: kind = %s, want %s", got, Template)
	}
}

// Messages as Rastrillo's source prints them. The icon ones are real:
// the gate's from a kept run (icons-gate.json), Fiesta's from
// internal/iconsets/iconsets.go. The locale text is the format in
// internal/generate/catalogs/catalogs.go and cmd/rastrillo/generate.go,
// and the asset text the format in internal/designsystem/hashed.go
// (the only asset message there is, the gallery's); no kept run hit
// either, so their arguments, a locale code and a file name, are filled
// in here.
func TestMissingLocaleKeysIconsAndAssetsAreFound(t *testing.T) {
	locale := "rastrillo generate: incomplete locale catalogs " +
		"(default \"en\") —\n  locales/fr.toml is missing 3 rastrillo.ui.* " +
		"key(s):\n    rastrillo.ui.error_500_title\n" +
		"make: *** [Makefile:198: generate-check] Error 1\n"
	asset := "panic: designsystem: no asset \"tokenz.css\" in the tree\n" +
		"FAIL\tgroomer/internal/groomertest\t0.1s\n"
	for name, tool := range map[string]transcript.Tool{
		"locale key": bash("make ci", locale, true),
		"icon":       fixture(t, "icons-gate"),
		"fiesta icon": bash("go test ./...", "panic: iconsets: Fiesta ships "+
			"no icon paw\nFAIL\tgroomer/internal/groomer\t0.1s\n", false),
		"asset": bash("go test ./...", asset, false),
	} {
		if got := only(t, tool).Kind; got != NameMissing {
			t.Errorf("%s: kind = %s, want %s", name, got, NameMissing)
		}
	}
}

func TestARouteNothingMountsIsARouteMistake(t *testing.T) {
	if got := only(t, fixture(t, "route-unmounted")).Kind; got != Route {
		t.Errorf("kind = %s, want %s", got, Route)
	}
}

func TestAGateFailureNamesItsLane(t *testing.T) {
	literal := bash("make ci", "go vet ./...\n"+
		"make: *** [Makefile:58: code] Error 1\n", true)
	for lane, tool := range map[string]transcript.Tool{
		"code":        literal,
		"staticcheck": fixture(t, "piped-make-gate"),
	} {
		mistake := only(t, tool)
		if mistake.Kind != Gate || mistake.Detail != lane {
			t.Errorf("got %s %q, want %s %q",
				mistake.Kind, mistake.Detail, Gate, lane)
		}
	}
}

func TestATestAssertionIsOther(t *testing.T) {
	mistake := only(t, fixture(t, "test-assertion"))
	if mistake.Kind != Other ||
		mistake.Detail != "TestABookingHoldsTheTableForItsLength" {
		t.Errorf("got %s %q, want %s with the test's name",
			mistake.Kind, mistake.Detail, Other)
	}
}

// make's test lane fails whenever a test does; that is the test's
// mistake, not the gate's.
func TestAFailingTestBehindMakeIsNotAGateMistake(t *testing.T) {
	mistake := only(t, bash("make ci", "--- FAIL: TestX (0.00s)\nFAIL\n"+
		"make: *** [Makefile:110: test] Error 1\n", true))
	if mistake.Kind != Other || mistake.Detail != "TestX" {
		t.Errorf("got %s %q, want %s TestX", mistake.Kind, mistake.Detail, Other)
	}
}

func TestTurnsToFixCountToTheSameCommandsSuccess(t *testing.T) {
	failing := bash("make ci 2>&1 | tail -5",
		"make: *** [Makefile:123: fmt-check] Error 1\n", false)
	passing := fixture(t, "passes-make-ci")
	goodTest := bash("go test ./...", "ok  \tgroomer\t0.2s\n", false)
	unclear := bash("make ci 2>&1 | tail -1", "receipt: 0 failed\n", false)

	found := Find(run(
		[]transcript.Tool{failing},
		[]transcript.Tool{bash("ls", "a\n", false)},
		[]transcript.Tool{failing},
		[]transcript.Tool{goodTest}, // another command passing: no fix
		[]transcript.Tool{unclear},  // no evidence: never a fix
		[]transcript.Tool{passing},
	))
	if len(found) != 2 {
		t.Fatalf("found %d mistakes, want 2: %+v", len(found), found)
	}
	for _, mistake := range found {
		if mistake.FixedAt != 5 {
			t.Errorf("mistake at %d fixed at %d, want 5",
				mistake.Message, mistake.FixedAt)
		}
	}
	if found[0].Message != 0 || found[1].Message != 2 {
		t.Errorf("messages = %d, %d, want 0, 2",
			found[0].Message, found[1].Message)
	}

	never := Find(run([]transcript.Tool{failing}, []transcript.Tool{unclear}))
	if len(never) != 1 || never[0].FixedAt != -1 {
		t.Errorf("got %+v, want one mistake never fixed", never)
	}
}

func TestSummariseGivesTheMedianTurnsOfThoseFixed(t *testing.T) {
	got := Summarise([]Mistake{
		{Kind: Gate, Message: 0, FixedAt: 1},
		{Kind: Gate, Message: 0, FixedAt: 3},
		{Kind: Gate, Message: 4, FixedAt: 9},
		{Kind: Gate, Message: 5, FixedAt: -1},
		{Kind: Template, Message: 2, FixedAt: 4},
	})
	var gate Summary
	for _, summary := range got {
		if summary.Kind == Gate {
			gate = summary
		}
	}
	if gate.Count != 4 || gate.Fixed != 3 || gate.Median != 3 {
		t.Errorf("gate = %+v, want 4 found, 3 fixed, median 3 turns", gate)
	}
	if len(got) != 6 {
		t.Errorf("got %d rows, want one per kind (6)", len(got))
	}
}

// A pipe ends in the exit status of its last command, so a failed gate
// piped to tail reads as a success. These are real results from kept
// groomer runs: is_error false, and the gate or the compile failed.
func TestAPipedFailureIsAFailure(t *testing.T) {
	for _, name := range []string{
		"piped-make-gate", "piped-go-test-setup", "piped-go-test-compile",
		"icons-gate",
	} {
		tool := fixture(t, name)
		if tool.IsError {
			t.Fatalf("%s: fixture has is_error; it should be a pipe's 0", name)
		}
		if got := OutcomeOf(tool); got != Failed {
			t.Errorf("%s: outcome = %s, want %s", name, got, Failed)
		}
	}
}

func TestOnlyPositiveEvidencePasses(t *testing.T) {
	for _, name := range []string{"passes-go-test", "passes-make-ci"} {
		if got := OutcomeOf(fixture(t, name)); got != Passed {
			t.Errorf("%s: outcome = %s, want %s", name, got, Passed)
		}
	}
	mixed := bash("go test ./...", "ok  \ta\t0.1s\n--- FAIL: TestX (0s)\n"+
		"FAIL\tb\t0.1s\n", false)
	if got := OutcomeOf(mixed); got != Failed {
		t.Errorf("a failure beside an ok = %s, want %s", got, Failed)
	}
	if got := OutcomeOf(bash("go test ./...", "ok  \ta\t0.1s\n", true)); got != Failed {
		t.Errorf("an ok with is_error = %s, want %s", got, Failed)
	}
}

func TestOutputWithNoEvidenceIsUnknown(t *testing.T) {
	for _, output := range []string{
		"", "go vet ./...\n", "receipt: 1 packages ran, 0 failed\n",
		"?   \tgroomer/internal/groomer\t[no test files]\n",
	} {
		piped := bash("go vet ./... 2>&1 | tail -5", output, false)
		if got := OutcomeOf(piped); got != Unknown {
			t.Errorf("%q: outcome = %s, want %s", output, got, Unknown)
		}
	}
}

// go vet and go build print nothing when they pass, so without a pipe
// to hide the exit status, silence is a pass.
func TestSilenceFromAnUnpipedCheckIsAPass(t *testing.T) {
	for _, command := range []string{
		"go vet ./...", "go build ./...", "cd app && go vet ./... && go build ./...",
		"CGO_ENABLED=0 go build ./...", "timeout 300 go vet ./...",
	} {
		if got := OutcomeOf(bash(command, "", false)); got != Passed {
			t.Errorf("%q: outcome = %s, want %s", command, got, Passed)
		}
	}
	for _, command := range []string{
		"go vet ./... | tail", "go vet ./...; echo done", "go vet ./... || true",
		"go vet ./... &", "go vet ./...\nls", "ls",
	} {
		if got := OutcomeOf(bash(command, "", false)); got != Unknown {
			t.Errorf("%q: outcome = %s, want %s", command, got, Unknown)
		}
	}
}

func TestASilentBuildFixesACompileErrorButNotAnAssertion(t *testing.T) {
	build := bash("go build ./...", "", false)
	compile := bash("go test ./...", "x.go:1:2: undefined: y\n", false)
	assertion := bash("go test ./...", "--- FAIL: TestX (0s)\nFAIL\n", false)

	found := Find(run(
		[]transcript.Tool{compile}, []transcript.Tool{assertion},
		[]transcript.Tool{build}))
	if len(found) != 2 || found[0].FixedAt != 2 || found[1].FixedAt != -1 {
		t.Errorf("got %+v, want the compile fixed at 2, the assertion never", found)
	}
}

// A pipe's exit status is its last command's: a grep that finds nothing
// is not a failed build.
func TestIsErrorFromAPipeNeedsAFailureLine(t *testing.T) {
	grep := bash("go test ./... 2>&1 | grep -c panic", "0\n", true)
	if got := OutcomeOf(grep); got != Unknown {
		t.Errorf("grep finding nothing = %s, want %s", got, Unknown)
	}
	if found := Find(run([]transcript.Tool{grep})); len(found) != 0 {
		t.Errorf("a quiet grep is a mistake: %+v", found)
	}
	failed := bash("go test ./... 2>&1 | tail -3", "FAIL\tp\t0.1s\n", true)
	if got := OutcomeOf(failed); got != Failed {
		t.Errorf("a failure line in a pipe = %s, want %s", got, Failed)
	}
}

// Claude Code starts a failed command's result with "Exit code N": the
// status of the last simple command in the list. It is a check's failure
// only when that last command is the check.
func TestExitCodeIsTheLastCommandsStatus(t *testing.T) {
	afterAGate := bash("make ci 2>&1 | tail -8; which chromium google-chrome",
		"Exit code 1\nok  \thello/internal/hello\t0.046s\n"+
			"budget code: within the limits.\n/usr/bin/chromium", true)
	if got := OutcomeOf(afterAGate); got == Failed {
		t.Errorf("a passing gate before a failed which = %s", got)
	}
	if found := Find(run([]transcript.Tool{afterAGate})); len(found) != 0 {
		t.Errorf("a failed which after a gate is a mistake: %+v", found)
	}
	lastIsTheTest := bash("cd app; go test ./...", "Exit code 1\n", true)
	if got := OutcomeOf(lastIsTheTest); got != Failed {
		t.Errorf("cd; go test exiting 1 = %s, want %s", got, Failed)
	}
	// A failure line still counts, whatever ends the list.
	piped := bash("go test ./... 2>&1 | tail -3; echo done",
		"Exit code 1\n--- FAIL: TestHome\nFAIL\tapp\t0.1s\ndone", true)
	if got := OutcomeOf(piped); got != Failed {
		t.Errorf("a FAIL line before an echo = %s, want %s", got, Failed)
	}
}

// The machine failing is not the agent's mistake.
func TestTimeoutsKillsAndAReadOnlyCacheAreNotMistakes(t *testing.T) {
	for name, output := range map[string]string{
		"timeout": "Exit code 124\nCommand timed out\n",
		"kill":    "Exit code 137\nsignal: killed\n",
		"read-only cache": "cmd/groomer/main.go:21:2: open /home/u/.cache/" +
			"go-build/40/4082-d: read-only file system\n",
	} {
		tool := bash("go build ./...", output, true)
		if got := OutcomeOf(tool); got != Unknown {
			t.Errorf("%s: outcome = %s, want %s", name, got, Unknown)
		}
		if found := Find(run([]transcript.Tool{tool})); len(found) != 0 {
			t.Errorf("%s is a mistake: %+v", name, found)
		}
	}
}

func TestOnlyChecksAreMistakes(t *testing.T) {
	for _, command := range []string{
		"grep -rn FAIL notes.txt",
		`grep "go test" README.md`,
		`git commit -m "make ci"`,
		`echo make ci`,
	} {
		tool := bash(command, "notes.txt:3:--- FAIL: TestX\n", true)
		if found := Find(run([]transcript.Tool{tool})); len(found) != 0 {
			t.Errorf("%q is a mistake: %+v", command, found)
		}
	}
}

// A chain can run several checks; what passes fixes what any of its
// checks could fix, and what fails is blamed on the check the output
// points at.
func TestAChainOfChecksFixesWhatAnyOfThemCould(t *testing.T) {
	assertion := bash("go test ./...", "--- FAIL: TestX (0s)\nFAIL\n", false)
	chain := bash("go vet ./... && go test ./...", "", false)

	found := Find(run([]transcript.Tool{assertion}, []transcript.Tool{chain}))
	if len(found) != 1 || found[0].FixedAt != 1 {
		t.Errorf("got %+v, want the assertion fixed at 1 by the chain's go test",
			found)
	}
}

func TestAFailedChainIsBlamedOnTheCheckTheOutputNames(t *testing.T) {
	failing := bash("go build ./... && go test ./...",
		"--- FAIL: TestX (0s)\nFAIL\n", true)
	build := bash("go build ./...", "", false)
	test := bash("go test ./...", "ok  \tp\t0.1s\n", false)

	// The assertion came from go test, so a silent build does not fix it.
	found := Find(run([]transcript.Tool{failing}, []transcript.Tool{build},
		[]transcript.Tool{test}))
	if len(found) != 1 || found[0].FixedAt != 2 {
		t.Errorf("got %+v, want fixed at 2, by go test", found)
	}

	compile := bash("go vet ./... && go test ./...", "x.go:1:2: undefined: y\n", true)
	found = Find(run([]transcript.Tool{compile}, []transcript.Tool{build}))
	if len(found) != 1 || found[0].FixedAt != 1 {
		t.Errorf("got %+v, want a compile error fixed at 1 by a build", found)
	}
}

func TestARealFailureBesideAMachineErrorStillFails(t *testing.T) {
	output := "--- FAIL: TestX (0s)\n    x_test.go:9: write: operation not permitted\nFAIL\n"
	if got := OutcomeOf(bash("go test ./...", output, true)); got != Failed {
		t.Errorf("outcome = %s, want %s", got, Failed)
	}
}

func TestAMissingToolOrNoNetworkIsNotAMistake(t *testing.T) {
	for name, output := range map[string]string{
		"no tool":    "bash: staticcheck: command not found\n",
		"no go":      "exec: \"gcc\": executable file not found in $PATH\n",
		"no module":  "go: example.com/x@v1: module lookup disabled by GOPROXY=off\nFAIL\tp [setup failed]\nFAIL\n",
		"no network": "Get \"https://proxy.golang.org/x\": dial tcp: lookup proxy.golang.org: no such host\nFAIL\tp [setup failed]\n",
	} {
		tool := bash("go test ./...", output, true)
		if got := OutcomeOf(tool); got != Unknown {
			t.Errorf("%s: outcome = %s, want %s", name, got, Unknown)
		}
	}
}
