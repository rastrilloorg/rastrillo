// Package waste finds the mistakes an agent made while building an app:
// a check (a build, a test run, a gate) that failed, sorted by what
// went wrong and by how many turns passed before the same check passed.
// It reads only what the transcript kept of each tool result.
package waste

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/transcript"
)

// Kind is what a mistake was about.
type Kind string

const (
	// GoCompile is a Go build, vet or type error.
	GoCompile Kind = "go-compile"
	// Template is a template that would not parse or run, or compiled
	// templates that no longer match the app.
	Template Kind = "template"
	// NameMissing is a locale key, an icon or an asset the app names
	// and the framework does not have.
	NameMissing Kind = "name-missing"
	// Route is an action or a Routes function that is not wired up.
	Route Kind = "route"
	// Gate is a make lane that failed for none of the reasons above:
	// its Detail is the lane.
	Gate Kind = "gate"
	// Other is a failure none of the rest names, such as a test's
	// assertion.
	Other Kind = "other"
)

// Kinds lists the kinds in the order a report shows them.
var Kinds = []Kind{GoCompile, Template, NameMissing, Route, Gate, Other}

// Outcome is what a check's output says about how it ended.
type Outcome string

const (
	Passed  Outcome = "passed"
	Failed  Outcome = "failed"
	Unknown Outcome = "unknown"
)

// Mistake is one failed check. Message is the index of the message that
// ran it; FixedAt is the index of the later message where the same
// check next Passed, or -1.
type Mistake struct {
	Kind             Kind
	Detail           string
	Message, FixedAt int
}

// Rastrillo's own messages and Go's, as they are printed. Each comes
// from the source or from a kept run; the testdata holds the real ones.
var (
	// A pipe's exit status is its last command's, so these lines, not
	// the exit status, are how a failure shows. Claude Code's own
	// "Exit code N" is that status in words; see exitCodeLine.
	failureLines = regexp.MustCompile(`(?m)` +
		`^\s*--- FAIL|^FAIL\b|^panic: |^vet: |^make(\[\d+\])?: \*\*\*|` +
		`\[(build|setup) failed\]|\bexit status [1-9]|` +
		`\bexit[ =:]+[1-9]\d*\s*$|` +
		`^rastrillo: budget \w+: [1-9]\d* problem|` +
		`^rastrillo: (\d+ unknown icon slug|No route mounts|` +
		`\S+ is out of date with the app's templates)|` +
		`^rastrillo generate: incomplete locale catalogs|` +
		`^No Routes function was found|^tmplrt: |` +
		`^receipt: .*\b[1-9]\d* failed`)

	// Pass lines: go test's, and the last step of make ci, which
	// make only reaches when every step before it passed.
	passLines = regexp.MustCompile(`(?m)` +
		`^ok\s+\S+\s|^PASS$|^rastrillo generate --check: \d+ route\(s\)`)

	// A compile error names a column; a test's own message does not.
	compileLine = regexp.MustCompile(
		`^(?:vet: )?\S+\.go:\d+:\d+: .+`)
	lintCode = regexp.MustCompile(`\([A-Z]+\d+\)$`)

	templateLine = regexp.MustCompile(
		`\btemplate: \S+:\d+(:\d+)?: |html/template:|RENDERERR|` +
			`is out of date with the app's templates|` +
			`tmplrt: the code was compiled for other types`)
	missingLine = regexp.MustCompile(
		`unknown icon slug|locale catalog\(s\) incomplete|` +
			`incomplete locale catalogs|locales/\S+\.toml is missing|` +
			`designsystem: no asset|Fiesta (\S+ )?ships no `)
	routeLine = regexp.MustCompile(
		`No route mounts these actions|No Routes function was found|` +
			`Routes registered no routes|` +
			`This app declares operations\. Each route`)
	laneLine = regexp.MustCompile(
		`make(?:\[\d+\])?: \*\*\* \[[^\]:]*:\d+: ([^\]]+)\] Error`)
	budgetLine = regexp.MustCompile(`^rastrillo: budget (\w+): [1-9]\d* problem`)
	testLine   = regexp.MustCompile(`--- FAIL:? (?:\S+ )?(\w+)`)
)

// exitCodeLine is how Claude Code starts a failed command's result:
// the status of the last simple command in the list, so a failure of
// a check only when that command is the check.
var exitCodeLine = regexp.MustCompile(`^Exit code [1-9]`)

// environmental is output that says the machine, not the code, failed:
// a read-only cache, a full disk, a timeout, a kill, a missing tool, no
// network.
var environmental = regexp.MustCompile(`(?mi)read-only file system|` +
	`no space left on device|operation not permitted|` +
	`command timed out|signal: (killed|terminated)|` +
	`^Exit code (124|137|143)\b|command not found|` +
	`executable file not found|module lookup disabled|dial tcp`)

// summaryLines only say that something failed, which an environmental
// failure also says; they are not evidence of a mistake by themselves.
var summaryLines = regexp.MustCompile(`(?m)^FAIL\b|` +
	`\[(build|setup) failed\]|^make(\[\d+\])?: \*\*\*|` +
	`\bexit status [1-9]|\bexit[ =:]+[1-9]\d*\s*$|^Exit code [1-9]`)

// failed is true when the output shows a failure of the code. Lines
// that are environmental or only summarise a failure are set aside
// when an environmental one is present, so a real failure printed
// beside "operation not permitted" still counts, and a setup that
// failed on a read-only cache does not.
func failed(output string) bool {
	if environmental.MatchString(output) {
		var rest []string
		for _, line := range strings.Split(output, "\n") {
			if !environmental.MatchString(line) &&
				!summaryLines.MatchString(line) {
				rest = append(rest, line)
			}
		}
		output = strings.Join(rest, "\n")
	}
	return failureLines.MatchString(output) || hasCompileError(output)
}

// OutcomeOf reads how a tool call ended from its output. is_error is
// not enough: a gate piped to tail ends in tail's exit status 0, and
// a pipe to grep that finds nothing ends in 1. So the output speaks
// first:
//   - a failure line is Failed, and so is is_error when the command's
//     exit status is its check's own (no pipe, `;`, `||` or `&`), and
//     Claude Code's "Exit code N" when the list's last simple command
//     is a check;
//   - a failure of the machine (timeout, kill, read-only cache) is
//     Unknown, unless the output also shows a failure of the code;
//   - a pass line is Passed, and so is silence from such a command,
//     because `go vet` and `go build` print nothing when they pass;
//   - anything else is not known to have passed.
func OutcomeOf(tool transcript.Tool) Outcome {
	command := shapeOf(commandOf(tool))
	switch {
	case failed(tool.Result):
		return Failed
	case environmental.MatchString(tool.Result):
		return Unknown
	case tool.IsError && command.trusted:
		return Failed
	case exitCodeLine.MatchString(tool.Result) && command.lastIsCheck:
		return Failed
	case passLines.MatchString(tool.Result):
		return Passed
	case !tool.IsError && command.trusted && len(command.checks) > 0:
		return Passed
	}
	return Unknown
}

// BuildsOrTests is true when a Bash call runs go build, go test, go vet
// or make, the commands whose failures a round counts as failed builds.
func BuildsOrTests(tool transcript.Tool) bool {
	for _, check := range shapeOf(commandOf(tool)).checks {
		switch {
		case check == "go build", check == "go test", check == "go vet",
			check == "make", strings.HasPrefix(check, "make "):
			return true
		}
	}
	return false
}

func hasCompileError(output string) bool {
	return compileDetail(output) != ""
}

// compileDetail is the first compile error line, or "".
func compileDetail(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if compileLine.MatchString(line) && !lintCode.MatchString(line) &&
			!strings.Contains(line, "missing go.sum entry") {
			return line
		}
	}
	return ""
}

// Find lists the failed checks in a run, in order.
func Find(run transcript.Transcript) []Mistake {
	var found []Mistake
	for at, message := range run.Messages {
		for _, tool := range message.Tools {
			checks := shapeOf(commandOf(tool)).checks
			if len(checks) == 0 || OutcomeOf(tool) != Failed {
				continue
			}
			kind, detail := classify(tool.Result)
			found = append(found, Mistake{
				Kind: kind, Detail: detail, Message: at,
				FixedAt: fixedAt(run, at,
					blamed(checks, kind, tool.Result), kind),
			})
		}
	}
	return found
}

// blamed is the check, of those a command ran, that the failure points
// at: a compile error at the first go command, a test's assertion at
// go test, a lane or a rastrillo message at make or rastrillo. When
// nothing points anywhere, the last one run, where a chain stopped.
func blamed(checks []string, kind Kind, output string) string {
	pick := func(match func(string) bool) string {
		for _, check := range checks {
			if match(check) {
				return check
			}
		}
		return ""
	}
	var found string
	switch {
	case kind == GoCompile:
		found = pick(func(c string) bool { return strings.HasPrefix(c, "go ") })
	case testLine.MatchString(output):
		found = pick(func(c string) bool { return c == "go test" })
	default:
		found = pick(func(c string) bool {
			return strings.HasPrefix(c, "make") ||
				strings.HasPrefix(c, "rastrillo ")
		})
	}
	if found == "" {
		return checks[len(checks)-1]
	}
	return found
}

func commandOf(tool transcript.Tool) string {
	if tool.Name != "Bash" {
		return ""
	}
	command, _ := tool.Input["command"].(string)
	return command
}

// fixedAt is the first message after one where the same check passed.
// A go test that passes fixes any go mistake, as it compiles what build
// and vet do; a silent build or vet fixes only a compile error, since
// a test that failed on an assertion is not mended by building.
func fixedAt(run transcript.Transcript, after int, check string,
	kind Kind) int {
	for at := after + 1; at < len(run.Messages); at++ {
		for _, tool := range run.Messages[at].Tools {
			if OutcomeOf(tool) != Passed {
				continue
			}
			for _, passed := range shapeOf(commandOf(tool)).checks {
				if fixes(passed, check, kind) {
					return at
				}
			}
		}
	}
	return -1
}

func fixes(passed, failed string, kind Kind) bool {
	if passed == failed {
		return true
	}
	bothGo := strings.HasPrefix(passed, "go ") &&
		strings.HasPrefix(failed, "go ") &&
		passed != "go run" && failed != "go run"
	return bothGo && (passed == "go test" || kind == GoCompile)
}

// classify sorts a failed result by the most specific thing it says.
func classify(output string) (Kind, string) {
	lines := strings.Split(output, "\n")
	for _, candidate := range []struct {
		kind    Kind
		pattern *regexp.Regexp
	}{
		{Route, routeLine}, {NameMissing, missingLine},
		{Template, templateLine},
	} {
		if line := firstMatch(lines, candidate.pattern); line != "" {
			return candidate.kind, line
		}
	}
	if line := compileDetail(output); line != "" {
		return GoCompile, short(line)
	}
	// A failing test fails make's test lane too; the test is the news.
	if match := testLine.FindStringSubmatch(output); match != nil {
		return Other, match[1]
	}
	if lane := laneOf(output); lane != "" {
		return Gate, lane
	}
	return Other, short(firstFailure(lines))
}

func firstMatch(lines []string, pattern *regexp.Regexp) string {
	for _, line := range lines {
		if pattern.MatchString(line) {
			return short(strings.TrimSpace(line))
		}
	}
	return ""
}

func firstFailure(lines []string) string {
	var kept []string
	for _, line := range lines {
		if !strings.HasPrefix(line, "Exit code") {
			kept = append(kept, line)
		}
	}
	if line := firstMatch(kept, failureLines); line != "" {
		return line
	}
	for _, line := range kept {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// laneOf is the make lane that failed, or the budget the gate named.
func laneOf(output string) string {
	if match := laneLine.FindStringSubmatch(output); match != nil {
		return match[1]
	}
	for _, line := range strings.Split(output, "\n") {
		if match := budgetLine.FindStringSubmatch(line); match != nil {
			return "budget " + match[1]
		}
	}
	return ""
}

func short(line string) string {
	runes := []rune(line)
	if len(runes) > 160 {
		return string(runes[:160]) + "..."
	}
	return line
}

// Summary is one kind's row of the report.
type Summary struct {
	Kind  Kind
	Count int
	Fixed int
	// Median is the middle of the turns taken, for the mistakes that
	// were fixed; zero when none was.
	Median float64
}

// Summarise gives a row for every kind, in report order.
func Summarise(mistakes []Mistake) []Summary {
	var rows []Summary
	for _, kind := range Kinds {
		row := Summary{Kind: kind}
		var turns []int
		for _, mistake := range mistakes {
			if mistake.Kind != kind {
				continue
			}
			row.Count++
			if mistake.FixedAt >= 0 {
				turns = append(turns, mistake.FixedAt-mistake.Message)
			}
		}
		row.Fixed = len(turns)
		slices.Sort(turns)
		row.Median = median(turns)
		rows = append(rows, row)
	}
	return rows
}

func median(sorted []int) float64 {
	switch n := len(sorted); {
	case n == 0:
		return 0
	case n%2 == 1:
		return float64(sorted[n/2])
	default:
		return float64(sorted[n/2-1]+sorted[n/2]) / 2
	}
}

// String is how a mistake reads in a report.
func (mistake Mistake) String() string {
	return fmt.Sprintf("%s at message %d: %s",
		mistake.Kind, mistake.Message, mistake.Detail)
}
