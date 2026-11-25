package comprehend

import (
	"math"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Normalise puts an answer in the form its kind compares in, so that
// two ways of writing one answer are equal: a status's name and number,
// a route's parameter names, a yes or no's case and full stop.
func Normalise(kind Kind, answer string) string {
	answer = trimmed(answer)
	switch kind {
	case Status:
		return statusCode(answer)
	case YesNo, Choice:
		return strings.ToLower(squeeze(answer))
	case Route:
		return route(answer)
	}
	return answer
}

// Correct grades a reader's answer against a settled key. An unsettled
// key grades nothing right; a Missing key is answered "not built", in
// any kind; a choice must be one of its choices, word for word.
func Correct(question Question, key Key, answer string) bool {
	if !key.Settled {
		return false
	}
	saysNotBuilt := strings.EqualFold(Normalise(Word, answer), NotBuilt)
	if key.Missing || saysNotBuilt {
		return key.Missing && saysNotBuilt
	}
	given := Normalise(question.Kind, answer)
	want := Normalise(question.Kind, key.Answer)
	switch question.Kind {
	case Choice:
		isChoice := slices.ContainsFunc(question.Choices,
			func(choice string) bool { return Normalise(Choice, choice) == given })
		return isChoice && given == want
	case Route:
		return sameRoute(given, want)
	}
	return given != "" && given == want
}

// trimmed is the answer without the space, quotes, backticks and
// trailing full stops around it, in whatever order they came.
func trimmed(answer string) string {
	for {
		shorter := strings.TrimRight(
			strings.Trim(strings.TrimSpace(answer), "`\"'"), ".")
		if shorter == answer {
			return answer
		}
		answer = shorter
	}
}

var statusNumber = regexp.MustCompile(`\b[1-5][0-9][0-9]\b`)

// statusCode is the status an answer gives: a code beside its name
// ("303 See Other"), else the last code that is not part of a path
// ("redirects to /bookings/123 with 303"), else the last number, else
// the code of the status the answer names ("See Other" is 303).
func statusCode(answer string) string {
	numbers := statusNumber.FindAllStringIndex(answer, -1)
	lower := strings.ToLower(answer)
	standalone := ""
	for _, at := range numbers {
		number := answer[at[0]:at[1]]
		code, _ := strconv.Atoi(number)
		name := strings.ToLower(http.StatusText(code))
		after := strings.TrimLeft(lower[at[1]:], " (")
		before := strings.TrimRight(lower[:at[0]], " (")
		if name != "" && (strings.HasPrefix(after, name) ||
			strings.HasSuffix(before, name)) {
			return number
		}
		if !strings.HasSuffix(before, "/") &&
			!strings.HasPrefix(lower[at[1]:], "/") {
			standalone = number
		}
	}
	if standalone != "" {
		return standalone
	}
	if len(numbers) > 0 {
		last := numbers[len(numbers)-1]
		return answer[last[0]:last[1]]
	}
	name := strings.ToLower(squeeze(answer))
	for code := 100; code < 600; code++ {
		if text := http.StatusText(code); text != "" &&
			strings.ToLower(text) == name {
			return strconv.Itoa(code)
		}
	}
	return name
}

var methods = []string{http.MethodGet, http.MethodHead, http.MethodPost,
	http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions}

// route is "METHOD /path" or "/path", every parameter written {}: a
// key's {reference} and a reader's :id are one parameter, and so is
// {rest...}. Go's {$} only anchors the end, so it is dropped.
func route(answer string) string {
	fields := strings.Fields(answer)
	method := ""
	if len(fields) == 2 &&
		slices.Contains(methods, strings.ToUpper(fields[0])) {
		method, fields = strings.ToUpper(fields[0])+" ", fields[1:]
	}
	if len(fields) != 1 {
		return squeeze(answer)
	}
	segments := strings.Split(strings.TrimSuffix(fields[0], "{$}"), "/")
	for i, segment := range segments {
		if strings.HasPrefix(segment, ":") ||
			strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
			segments[i] = "{}"
		}
	}
	return method + strings.Join(segments, "/")
}

// sameRoute compares two routes' methods only when both give one.
func sameRoute(given, want string) bool {
	givenMethod, givenPath, givenHasMethod := strings.Cut(given, " ")
	wantMethod, wantPath, wantHasMethod := strings.Cut(want, " ")
	if !givenHasMethod {
		givenPath = givenMethod
	}
	if !wantHasMethod {
		wantPath = wantMethod
	}
	if givenHasMethod && wantHasMethod && givenMethod != wantMethod {
		return false
	}
	return strings.HasPrefix(givenPath, "/") && givenPath == wantPath
}

// Reading is one reader's answer to one question and what it spent.
// Tokens are codex's input (cached included) and output tokens.
type Reading struct {
	QuestionID          string
	Answer              string
	Correct, OverBudget bool
	Tokens, Commands    int
}

// RunReading is one read of one app: every question asked once, graded.
// Accuracy is right answers over all the brief's questions, so an
// unsettled question counts against no app but is never dropped. The
// medians are of the right answers' effort. AccuracyBeforeBudget grades
// an over-budget answer on what it says: the noise gate judges accuracy
// by it, so token noise cannot pass for a difference in understanding.
type RunReading struct {
	Run                                    string
	Readings                               []Reading
	Accuracy, MedianTokens, MedianCommands float64
	AccuracyBeforeBudget                   float64
	Unsettled, Missing                     int
}

// Score grades one read of an app against its keys. Readings of a
// question with no settled key are kept, graded wrong.
func Score(
	run string, questions []Question, keys []Key, readings []Reading,
) RunReading {
	byID := map[string]Key{}
	for _, key := range keys {
		byID[key.ID] = key
	}
	scored := RunReading{Run: run}
	asked := map[string]Question{}
	for _, question := range questions {
		asked[question.ID] = question
		key := byID[question.ID]
		switch {
		case !key.Settled:
			scored.Unsettled++
		case key.Missing:
			scored.Missing++
		}
	}
	var tokens, commands []float64
	right, rightOnContent := 0, 0
	for _, reading := range readings {
		question, isAsked := asked[reading.QuestionID]
		onContent := isAsked &&
			Correct(question, byID[reading.QuestionID], reading.Answer)
		if onContent {
			rightOnContent++
		}
		reading.Correct = onContent && !reading.OverBudget
		if reading.Correct {
			right++
			tokens = append(tokens, float64(reading.Tokens))
			commands = append(commands, float64(reading.Commands))
		}
		scored.Readings = append(scored.Readings, reading)
	}
	if len(questions) > 0 {
		scored.Accuracy = float64(right) / float64(len(questions))
		scored.AccuracyBeforeBudget = float64(rightOnContent) /
			float64(len(questions))
	}
	scored.MedianTokens, scored.MedianCommands = Median(tokens),
		Median(commands)
	return scored
}

// Effort is two sets of readings' median tokens and commands over the
// questions both answered right, and how many those were.
type Effort struct {
	Questions        int
	Tokens, Commands [2]float64
}

// CompareEffort compares effort only where both got the answer: a
// wrong answer reached quickly is not an easier read.
func CompareEffort(first, second []Reading) Effort {
	rightInSecond := map[string]Reading{}
	for _, reading := range second {
		if reading.Correct {
			rightInSecond[reading.QuestionID] = reading
		}
	}
	var tokens, commands [2][]float64
	for _, reading := range first {
		other, bothRight := rightInSecond[reading.QuestionID]
		if !reading.Correct || !bothRight {
			continue
		}
		tokens[0] = append(tokens[0], float64(reading.Tokens))
		tokens[1] = append(tokens[1], float64(other.Tokens))
		commands[0] = append(commands[0], float64(reading.Commands))
		commands[1] = append(commands[1], float64(other.Commands))
	}
	return Effort{Questions: len(tokens[0]),
		Tokens:   [2]float64{Median(tokens[0]), Median(tokens[1])},
		Commands: [2]float64{Median(commands[0]), Median(commands[1])}}
}

// AppReads is every read of one app. Version is the Rastrillo version
// its go.mod requires: effort is compared only within one version.
// MissingReadings is how many readings of its settled questions, over
// every read asked for, it lacks: an app with any is incomplete, and
// kept out of the noise gates.
type AppReads struct {
	Round, Run, Brief, Version string
	Reads                      []RunReading
	MissingReadings            int `json:",omitempty"`
}

// Complete is whether every read holds every settled question's reading.
func (app AppReads) Complete() bool { return app.MissingReadings == 0 }

// Gate is the noise check for one measure: the median absolute
// difference between two reads of one app (Reads) must be at most half
// the median absolute difference between runs of one brief (Runs).
// Accuracy is taken before the budget (AccuracyBeforeBudget).
// Pairs with no question both got right have no effort difference and
// are left out; ReadPairs and RunPairs are the pairs counted. Passes is
// true only when Verdict is GatePasses: anything else is inconclusive.
type Gate struct {
	Measure             string
	Reads, Runs         float64
	ReadPairs, RunPairs int
	Verdict             string
	Passes              bool
}

// A gate's verdicts. Runs that do not differ at all cannot be told
// apart by any read, however steady: that is no pass.
const (
	GatePasses     = "passes"
	GateFails      = "fails"
	CannotSeparate = "cannot separate runs"
)

// Measures are the comprehension measures the noise gate checks.
var Measures = []string{"accuracy", "tokens", "commands"}

// difference is how far apart two reads are on a measure, and whether
// they can be compared on it at all.
func difference(measure string, first, second RunReading) (float64, bool) {
	if measure == "accuracy" {
		return math.Abs(first.AccuracyBeforeBudget -
			second.AccuracyBeforeBudget), true
	}
	effort := CompareEffort(first.Readings, second.Readings)
	if effort.Questions == 0 {
		return 0, false
	}
	if measure == "tokens" {
		return math.Abs(effort.Tokens[0] - effort.Tokens[1]), true
	}
	return math.Abs(effort.Commands[0] - effort.Commands[1]), true
}

// ReadDifferences are the differences between every two reads of one
// app on a measure.
func ReadDifferences(measure string, app AppReads) []float64 {
	var differences []float64
	for i, first := range app.Reads {
		for _, second := range app.Reads[i+1:] {
			if found, comparable := difference(measure, first, second); comparable {
				differences = append(differences, found)
			}
		}
	}
	return differences
}

// RunDifferences are the differences between runs of one brief on one
// Rastrillo version, each read of one set against the same read of the
// other: one read's view of each, as one read's view of one app is
// compared with another's.
func RunDifferences(measure string, apps []AppReads) []float64 {
	var differences []float64
	for i, first := range apps {
		for _, second := range apps[i+1:] {
			if first.Brief != second.Brief || first.Version != second.Version {
				continue
			}
			for read := range min(len(first.Reads), len(second.Reads)) {
				found, comparable := difference(measure, first.Reads[read],
					second.Reads[read])
				if comparable {
					differences = append(differences, found)
				}
			}
		}
	}
	return differences
}

// NoiseGates checks each measure on the complete apps only: a reading
// that failed would otherwise count as a wrong answer, steadily, and
// could pass a gate on the harness's failures. A measure with no two
// reads of an app to compare fails; one with no two runs that differ
// cannot separate them.
func NoiseGates(all []AppReads) []Gate {
	var apps []AppReads
	for _, app := range all {
		if app.Complete() {
			apps = append(apps, app)
		}
	}
	var gates []Gate
	for _, measure := range Measures {
		var reads []float64
		for _, app := range apps {
			reads = append(reads, ReadDifferences(measure, app)...)
		}
		runs := RunDifferences(measure, apps)
		gate := Gate{Measure: measure, Reads: Median(reads),
			Runs: Median(runs), ReadPairs: len(reads), RunPairs: len(runs)}
		gate.Verdict = gateVerdict(gate)
		gate.Passes = gate.Verdict == GatePasses
		gates = append(gates, gate)
	}
	return gates
}

func gateVerdict(gate Gate) string {
	switch {
	case gate.RunPairs == 0 || gate.Runs == 0:
		return CannotSeparate
	case gate.ReadPairs > 0 && gate.Reads <= gate.Runs/2:
		return GatePasses
	}
	return GateFails
}

// Median is the middle value, or the mean of the middle two; 0 for none.
func Median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := slices.Sorted(slices.Values(values))
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}
	return (sorted[middle-1] + sorted[middle]) / 2
}

// Percentile is the nearest-rank percentile: the smallest value with at
// least percent of the values at or below it. 0 for none.
func Percentile(values []float64, percent float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := slices.Sorted(slices.Values(values))
	rank := int(math.Ceil(percent / 100 * float64(len(sorted))))
	return sorted[max(rank, 1)-1]
}
