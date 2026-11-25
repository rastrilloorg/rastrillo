package comprehend

import (
	"fmt"
	"math"
	"testing"
)

func settled(id, answer string) Key {
	return Key{ID: id, Answer: answer, Settled: true}
}

func TestStatusAnswersNormaliseNamesAndNumbers(t *testing.T) {
	question := Question{ID: "invalid", Kind: Status}
	for _, answer := range []string{"422", "Unprocessable Entity",
		"422 Unprocessable Entity", "HTTP 422", " unprocessable entity. "} {
		if !Correct(question, settled("invalid", "422"), answer) {
			t.Errorf("%q is not 422", answer)
		}
	}
	if Normalise(Status, "See Other") != Normalise(Status, "303") {
		t.Errorf("See Other = %q, 303 = %q",
			Normalise(Status, "See Other"), Normalise(Status, "303"))
	}
	for _, answer := range []string{"302", "Found", "", "a redirect"} {
		if Correct(question, settled("invalid", "303"), answer) {
			t.Errorf("%q passed for 303", answer)
		}
	}
}

func TestRoutesNormaliseTheirParameters(t *testing.T) {
	question := Question{ID: "after", Kind: Route}
	key := settled("after", "/bookings/{id}")
	for _, answer := range []string{"/bookings/:id", "/bookings/{id}",
		"/bookings/{bookingID}", "GET /bookings/{bookingID}",
		"`/bookings/{reference}`"} {
		if !Correct(question, key, answer) {
			t.Errorf("%q is not %s", answer, key.Answer)
		}
	}
	for _, answer := range []string{"/booking/{id}", "/bookings",
		"/bookings/{id}/edit", "/bookings/new"} {
		if Correct(question, key, answer) {
			t.Errorf("%q passed for %s", answer, key.Answer)
		}
	}

	// The method is kept only when both sides give one.
	withMethod := settled("after", "POST /bookings/{id}")
	if !Correct(question, withMethod, "/bookings/{ref}") {
		t.Error("an answer without a method failed a key with one")
	}
	if !Correct(question, withMethod, "post /bookings/:ref") {
		t.Error("the same method in lower case failed")
	}
	if Correct(question, withMethod, "GET /bookings/{id}") {
		t.Error("GET passed for POST")
	}
}

func TestYesNoIgnoresCaseAndFullStops(t *testing.T) {
	question := Question{ID: "phone", Kind: YesNo}
	for _, answer := range []string{"yes", "Yes.", "YES", " yes. "} {
		if !Correct(question, settled("phone", "yes"), answer) {
			t.Errorf("%q is not yes", answer)
		}
	}
	for _, answer := range []string{"no", "No.", "maybe", ""} {
		if Correct(question, settled("phone", "yes"), answer) {
			t.Errorf("%q passed for yes", answer)
		}
	}
}

func TestAChoiceMustBeOneOfItsChoices(t *testing.T) {
	question := Question{ID: "remove", Kind: Choice, Choices: []string{
		"they are kept", "they are deleted", "they are cancelled"}}
	key := settled("remove", "they are kept")
	for _, answer := range []string{"they are kept", "They are kept."} {
		if !Correct(question, key, answer) {
			t.Errorf("%q is not %q", answer, key.Answer)
		}
	}
	for _, answer := range []string{"kept", "they are kept, mostly",
		"they are deleted"} {
		if Correct(question, key, answer) {
			t.Errorf("%q passed, but is not the key or not a choice", answer)
		}
	}
}

// A key for something the app does not do is answered "not built", in
// any kind, and only that answer is right.
func TestAMissingKeyIsGradedLikeAnyOther(t *testing.T) {
	missing := Key{ID: "export", Answer: NotBuilt, Settled: true,
		Missing: true}
	for _, kind := range kinds {
		question := Question{ID: "export", Kind: kind,
			Choices: []string{"a", "b"}}
		if !Correct(question, missing, "Not built.") {
			t.Errorf("%s: not built failed a missing key", kind)
		}
		if Correct(question, missing, "a") {
			t.Errorf("%s: an answer passed a missing key", kind)
		}
		if Correct(question, settled("export", "a"), "not built") {
			t.Errorf("%s: not built passed a key with an answer", kind)
		}
	}
}

func TestAnUnsettledKeyGradesNothing(t *testing.T) {
	question := Question{ID: "demo", Kind: Word}
	if Correct(question, Key{ID: "demo", Answer: "owner"}, "owner") {
		t.Error("an answer passed an unsettled key")
	}
}

func TestAccuracyHasAFixedDenominator(t *testing.T) {
	var questions []Question
	var keys []Key
	var readings []Reading
	for i := range 10 {
		id := fmt.Sprintf("q%d", i)
		questions = append(questions, Question{ID: id, Kind: YesNo})
		switch {
		case i < 2:
			keys = append(keys, Key{ID: id, Answer: "yes"}) // unsettled
		case i < 9:
			keys = append(keys, settled(id, "yes"))
			readings = append(readings, Reading{QuestionID: id, Answer: "yes",
				Tokens: 100 * i, Commands: i})
		default:
			keys = append(keys, settled(id, "yes"))
			readings = append(readings, Reading{QuestionID: id, Answer: "no"})
		}
	}

	scored := Score("groomer-carlos-1", questions, keys, readings)

	if scored.Accuracy != 0.7 || scored.Unsettled != 2 || scored.Missing != 0 {
		t.Errorf("accuracy %v, unsettled %d, missing %d; want 0.7, 2, 0",
			scored.Accuracy, scored.Unsettled, scored.Missing)
	}
	// Effort is over the right answers: 200 to 800 tokens, 2 to 8 commands.
	if scored.MedianTokens != 500 || scored.MedianCommands != 5 {
		t.Errorf("median tokens %v, commands %v; want 500, 5",
			scored.MedianTokens, scored.MedianCommands)
	}
	correct := 0
	for _, reading := range scored.Readings {
		if reading.Correct {
			correct++
		}
	}
	if correct != 7 || len(scored.Readings) != 8 {
		t.Errorf("%d of %d readings correct, want 7 of 8", correct,
			len(scored.Readings))
	}
}

func TestOverBudgetGradesWrong(t *testing.T) {
	questions := []Question{{ID: "status", Kind: Status}}
	keys := []Key{settled("status", "303")}
	readings := []Reading{{QuestionID: "status", Answer: "303",
		OverBudget: true, Tokens: 250000}}

	scored := Score("groomer-carlos-1", questions, keys, readings)

	if scored.Accuracy != 0 || scored.Readings[0].Correct {
		t.Errorf("an answer over budget was graded right: %+v", scored)
	}
}

func TestEffortIsComparedOnlyOnQuestionsBothGotRight(t *testing.T) {
	first := []Reading{
		{QuestionID: "q1", Correct: true, Tokens: 100, Commands: 2},
		{QuestionID: "q2", Correct: true, Tokens: 300, Commands: 6},
		{QuestionID: "q3", Correct: false, Tokens: 9000, Commands: 40},
		{QuestionID: "q4", Correct: true, Tokens: 500, Commands: 4},
	}
	second := []Reading{
		{QuestionID: "q1", Correct: true, Tokens: 200, Commands: 3},
		{QuestionID: "q2", Correct: false, Tokens: 50, Commands: 1},
		{QuestionID: "q3", Correct: true, Tokens: 10, Commands: 1},
		{QuestionID: "q4", Correct: true, Tokens: 700, Commands: 5},
	}

	effort := CompareEffort(first, second)

	want := Effort{Questions: 2, Tokens: [2]float64{300, 450},
		Commands: [2]float64{3, 4}}
	if effort != want {
		t.Errorf("effort = %+v, want %+v", effort, want)
	}
}

// read is one read of an app: its accuracy, and every answer right at
// the given tokens and commands.
func read(accuracy float64, tokens, commands int) RunReading {
	return RunReading{Accuracy: accuracy, AccuracyBeforeBudget: accuracy,
		Readings: []Reading{
			{QuestionID: "q1", Correct: true, Tokens: tokens, Commands: commands},
		}}
}

func app(run, version string, reads ...RunReading) AppReads {
	return AppReads{Run: run, Brief: "groomer", Version: version,
		Reads: reads}
}

func gate(t *testing.T, gates []Gate, measure string) Gate {
	t.Helper()
	for _, found := range gates {
		if found.Measure == measure {
			return found
		}
	}
	t.Fatalf("no %s gate in %+v", measure, gates)
	return Gate{}
}

func TestTheNoiseGateFailsWhenReadsVaryAsMuchAsRuns(t *testing.T) {
	// Two reads of each app differ as much as the apps do.
	noisy := NoiseGates([]AppReads{
		app("groomer-carlos-1", "v1", read(0.5, 1000, 5), read(0.9, 2000, 10)),
		app("groomer-carlos-2", "v1", read(0.9, 2000, 10), read(0.5, 1000, 5)),
	})
	for _, measure := range []string{"accuracy", "tokens", "commands"} {
		if found := gate(t, noisy, measure); found.Passes {
			t.Errorf("%s passed: %+v", measure, found)
		}
	}

	// Reads agree closely; runs differ. A run on another version is
	// never compared with these, however far it is from them.
	steady := NoiseGates([]AppReads{
		app("groomer-carlos-1", "v1", read(0.9, 1000, 5), read(0.9, 1050, 5)),
		app("groomer-carlos-2", "v1", read(0.5, 3000, 12), read(0.6, 3100, 13)),
		app("groomer-carlos-3", "v2", read(0.0, 90000, 90)),
	})
	accuracy := gate(t, steady, "accuracy")
	if !accuracy.Passes || !near(accuracy.Reads, 0.05) ||
		!near(accuracy.Runs, 0.35) {
		t.Errorf("accuracy gate = %+v, want reads 0.05, runs 0.35, passing",
			accuracy)
	}
	tokens := gate(t, steady, "tokens")
	if !tokens.Passes || tokens.Reads != 75 || tokens.Runs != 2025 {
		t.Errorf("tokens gate = %+v, want reads 75, runs 2025, passing",
			tokens)
	}
	if !gate(t, steady, "commands").Passes {
		t.Errorf("commands gate failed: %+v", gate(t, steady, "commands"))
	}
}

func near(got, want float64) bool {
	return math.Abs(got-want) < 1e-9
}

// With nothing to compare, a gate cannot pass.
func TestTheNoiseGateNeedsTwoReadsAndTwoRuns(t *testing.T) {
	gates := NoiseGates([]AppReads{
		app("groomer-carlos-1", "v1", read(0.9, 1000, 5)),
	})
	if found := gate(t, gates, "accuracy"); found.Passes {
		t.Errorf("a gate passed with one read of one app: %+v", found)
	}
}

// Runs that do not differ at all leave nothing for reads to be smaller
// than: the measure cannot separate them, and is inconclusive, never a
// pass, whatever the reads do.
func TestAGateWithNoSpreadBetweenRunsCannotSeparateThem(t *testing.T) {
	gates := NoiseGates([]AppReads{
		app("groomer-carlos-1", "v1", read(1, 1000, 3), read(1, 1000, 3)),
		app("groomer-carlos-2", "v1", read(1, 1000, 3), read(1, 1000, 3)),
	})

	for _, measure := range Measures {
		found := gate(t, gates, measure)
		if found.Passes || found.Verdict != CannotSeparate {
			t.Errorf("%s: %+v, want %q and not passing", measure, found,
				CannotSeparate)
		}
	}
}

func TestGateVerdictsAreNamed(t *testing.T) {
	steady := NoiseGates([]AppReads{
		app("groomer-carlos-1", "v1", read(0.9, 1000, 5), read(0.9, 1050, 5)),
		app("groomer-carlos-2", "v1", read(0.5, 3000, 12), read(0.6, 3100, 13)),
	})
	noisy := NoiseGates([]AppReads{
		app("groomer-carlos-1", "v1", read(0.5, 1000, 5), read(0.9, 2000, 10)),
		app("groomer-carlos-2", "v1", read(0.9, 2000, 10), read(0.5, 1000, 5)),
	})

	if found := gate(t, steady, "tokens"); found.Verdict != GatePasses {
		t.Errorf("steady tokens: %+v, want %q", found, GatePasses)
	}
	if found := gate(t, noisy, "tokens"); found.Verdict != GateFails {
		t.Errorf("noisy tokens: %+v, want %q", found, GateFails)
	}
}

// Go's {$} anchors the end of a pattern and is no segment; {rest...}
// is a parameter like any other.
func TestRoutesReadGoPatterns(t *testing.T) {
	question := Question{ID: "home", Kind: Route}
	for key, answer := range map[string]string{
		"/{$}":              "/",
		"GET /bookings/{$}": "/bookings/",
		"/files/{rest...}":  "/files/{path}",
		"/static/{file...}": "/static/:name",
	} {
		if !Correct(question, settled("home", key), answer) {
			t.Errorf("%q is not %q", answer, key)
		}
	}
	if Correct(question, settled("home", "/{$}"), "/bookings") {
		t.Error("/bookings passed for /{$}")
	}
}

// A status in a sentence is the one standing alone or beside its name,
// not a number in a path.
func TestStatusIsTheCodeNotANumberInAPath(t *testing.T) {
	for answer, want := range map[string]string{
		"redirects to /bookings/123 with 303": "303",
		"/bookings/404 answers 303 See Other": "303",
		"See Other (303), to /bookings/200":   "303",
		"303":                                 "303",
		"HTTP 422":                            "422",
	} {
		if got := Normalise(Status, answer); got != want {
			t.Errorf("Normalise(Status, %q) = %q, want %q", answer, got, want)
		}
	}
}

// Quotes and full stops come off in any order.
func TestAnswersAreTrimmedUntilNothingChanges(t *testing.T) {
	for _, answer := range []string{`"yes".`, `'yes.'`, "`yes`.", ` "yes." `} {
		if got := Normalise(YesNo, answer); got != "yes" {
			t.Errorf("Normalise(YesNo, %q) = %q, want yes", answer, got)
		}
	}
}

// Ruling R19: token noise fails its own gate, so it must not make an
// accuracy pass. Two reads that differ only in one reading going over
// the budget agree on accuracy as far as the gate is concerned, while
// the reported accuracy still counts it wrong.
func TestTheAccuracyGateIgnoresTheBudget(t *testing.T) {
	questions := []Question{{ID: "q1", Kind: YesNo}, {ID: "q2", Kind: YesNo}}
	keys := []Key{settled("q1", "yes"), settled("q2", "yes")}
	within := []Reading{{QuestionID: "q1", Answer: "yes", Tokens: 100},
		{QuestionID: "q2", Answer: "yes", Tokens: 100}}
	over := []Reading{{QuestionID: "q1", Answer: "yes", Tokens: 100},
		{QuestionID: "q2", Answer: "yes", Tokens: 900, OverBudget: true}}
	wrong := []Reading{{QuestionID: "q1", Answer: "no", Tokens: 100},
		{QuestionID: "q2", Answer: "no", Tokens: 100}}
	first := Score("groomer-carlos-1", questions, keys, within)
	second := Score("groomer-carlos-1", questions, keys, over)
	other := Score("groomer-carlos-2", questions, keys, wrong)

	gates := NoiseGates([]AppReads{
		app("groomer-carlos-1", "v1", first, second),
		app("groomer-carlos-2", "v1", other, other),
	})

	if second.Accuracy != 0.5 {
		t.Errorf("reported accuracy %v, want 0.5: over budget is wrong",
			second.Accuracy)
	}
	accuracy := gate(t, gates, "accuracy")
	if accuracy.Reads != 0 || accuracy.Runs != 1 || !accuracy.Passes {
		t.Errorf("accuracy gate = %+v, want reads 0, runs 1, passing",
			accuracy)
	}
}

// An app whose reads lack answers, the same ones each time, reads as
// steady and far from a complete app: harness failures, not a
// difference in understanding. It is kept out of the gate.
func TestAnIncompleteAppIsKeptOutOfTheGate(t *testing.T) {
	partial := app("groomer-carlos-1", "v1", read(0.5, 1000, 5),
		read(0.5, 1000, 5))
	partial.MissingReadings = 10
	gates := NoiseGates([]AppReads{partial,
		app("groomer-carlos-2", "v1", read(1, 1000, 5), read(1, 1000, 5)),
	})

	for _, measure := range Measures {
		if found := gate(t, gates, measure); found.Passes || found.RunPairs != 0 {
			t.Errorf("%s: %+v, want no runs to compare and no pass", measure,
				found)
		}
	}
}
