package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/comprehend"
)

// savedApp is groomer-carlos-1 with one question, keyed 303, and a
// readings file made as today's would be.
func savedApp(t *testing.T) (*readApp, string) {
	t.Helper()
	cache := t.TempDir()
	app := &readApp{round: "1", brief: "groomer", name: "groomer-carlos-1",
		questions: []comprehend.Question{{ID: "booked", Kind: comprehend.Status}},
		keys:      []comprehend.Key{{ID: "booked", Answer: "303", Settled: true}},
		file: readingFile{Model: comprehend.ReaderModel,
			PromptHash: comprehend.ReaderPromptHash(), SourceHash: "abc"}}
	return app, cache
}

// Readings cost a reader each: an interrupted run keeps what it made,
// and running again reads only what is missing.
func TestASavedReadingIsNotReadAgain(t *testing.T) {
	app, cache := savedApp(t)
	reading := comprehend.Reading{QuestionID: "booked", Answer: "303",
		Tokens: 1000, Commands: 2}
	if err := app.save(cache, 1, reading); err != nil {
		t.Fatal(err)
	}

	again, _ := savedApp(t)
	if err := again.loadReadings(cache); err != nil {
		t.Fatal(err)
	}

	if !again.answered(1, "booked") {
		t.Error("the saved reading of read 2 is not known")
	}
	if again.answered(0, "booked") {
		t.Error("read 1 holds a reading it never made")
	}
}

func TestReadingsMadeOtherwiseAreRefused(t *testing.T) {
	app, cache := savedApp(t)
	app.file.PromptHash = "an-older-prompt"
	err := app.save(cache, 0, comprehend.Reading{QuestionID: "booked"})
	if err != nil {
		t.Fatal(err)
	}

	today, _ := savedApp(t)
	err = today.loadReadings(cache)

	if err == nil || !strings.Contains(err.Error(), "readings.json") {
		t.Errorf("err = %v, want a refusal naming readings.json", err)
	}
}

// A reading is judged by today's budget, whatever it was read under.
func TestReadingsAreGradedAtTodaysBudget(t *testing.T) {
	app, _ := savedApp(t)
	app.file.Reads = [][]comprehend.Reading{{{QuestionID: "booked",
		Answer: "303", OverBudget: true, Tokens: 150000}}}

	report := gradeAll([]*readApp{app}, 2, 200000)

	if got := report.Apps[0].Reads[0].Accuracy; got != 1 {
		t.Errorf("accuracy %v at a budget of 200000, want 1", got)
	}
	tighter := gradeAll([]*readApp{app}, 2, 100000)
	if got := tighter.Apps[0].Reads[0].Accuracy; got != 0 {
		t.Errorf("accuracy %v at a budget of 100000, want 0", got)
	}
}

// The budget is suggested from right answers before any budget made
// them wrong: answers cut by this run's budget are counted, not lost.
func TestTheBudgetIsTheNinetyFifthPercentileOfRightAnswers(t *testing.T) {
	app, _ := savedApp(t)
	var reads [][]comprehend.Reading
	for i := 1; i <= 19; i++ {
		reads = append(reads, []comprehend.Reading{{QuestionID: "booked",
			Answer: "303", Tokens: i*10000 + 1}})
	}
	reads = append(reads,
		[]comprehend.Reading{{QuestionID: "booked", Answer: "303",
			Tokens: 254321}},
		[]comprehend.Reading{{QuestionID: "booked", Answer: "302",
			Tokens: 900000}})
	app.file.Reads = reads

	report := gradeAll([]*readApp{app}, len(reads), 200000)

	want := budgeting{RightAnswers: 20, Percentile95: 190001,
		Suggested: 200000, Cut: 1}
	if report.Budgeting != want {
		t.Errorf("budgeting = %+v, want %+v", report.Budgeting, want)
	}
}

// Readings live beside the keys, apart from the runs.
func TestReadingsAreWrittenApartFromTheRuns(t *testing.T) {
	cache := t.TempDir()
	path := readingsPath(cache, "1", "groomer-carlos-1")

	if path != filepath.Join(cache, "scoring", "1", "groomer-carlos-1",
		"readings.json") {
		t.Errorf("readings go to %s", path)
	}
}

// Once interrupted, no more readings start: the queue is dropped, not
// run through to fail one by one.
func TestNoReadingStartsAfterAnInterrupt(t *testing.T) {
	app, cache := savedApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	failed := readerRun{cache: cache, budget: 1}.readAll(ctx,
		[]*readApp{app}, 3, 2)

	if len(failed) != 0 {
		t.Errorf("%d readings started after the interrupt: %v",
			len(failed), failed)
	}
	if app.stage.pending != 0 || app.stage.dir != "" {
		t.Errorf("the stage is left: %d pending, dir %q",
			app.stage.pending, app.stage.dir)
	}
}

// An app is complete when every read asked for holds a reading of every
// settled question; one that is not is marked, and left out of the gate.
func TestAppsWithMissingReadingsAreIncomplete(t *testing.T) {
	app, _ := savedApp(t)
	app.questions = append(app.questions,
		comprehend.Question{ID: "dog", Kind: comprehend.Word},
		comprehend.Question{ID: "open", Kind: comprehend.YesNo})
	app.keys = append(app.keys,
		comprehend.Key{ID: "dog", Answer: "DogName", Settled: true},
		comprehend.Key{ID: "open", Answer: "yes"})
	app.file.Reads = [][]comprehend.Reading{
		{{QuestionID: "booked", Answer: "303"}, {QuestionID: "dog",
			Answer: "DogName"}},
		{{QuestionID: "booked", Answer: "303"}},
	}
	whole, _ := savedApp(t)
	whole.name = "groomer-carlos-2"
	whole.file.Reads = [][]comprehend.Reading{
		{{QuestionID: "booked", Answer: "303"}},
		{{QuestionID: "booked", Answer: "303"}},
	}

	report := gradeAll([]*readApp{app, whole}, 3, 200000)

	if got := report.Apps[0].MissingReadings; got != 3 {
		t.Errorf("missing readings = %d, want 3 (dog in read 2, both in "+
			"read 3; open is unsettled)", got)
	}
	if got := report.Apps[1].MissingReadings; got != 1 {
		t.Errorf("missing readings = %d, want 1 (read 3)", got)
	}
	text := comprehensionMarkdown("test", report)
	if !strings.Contains(text, "groomer-carlos-1 (incomplete)") ||
		!strings.Contains(text, "Incomplete, so left out of the gate: "+
			"1/groomer-carlos-1 (3 missing)") {
		t.Errorf("the report does not mark the incomplete app:\n%s", text)
	}
}
