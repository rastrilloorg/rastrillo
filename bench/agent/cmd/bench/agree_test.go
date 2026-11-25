package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/calibrate"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/counts"
)

// scoredPair is a pair whose left side has cognitive complexity left
// and whose right side has right, from two runs.
func scoredPair(
	id int, set string, left, right int, runs ...string,
) calibrate.Pair {
	side := func(run string, cognitive int) calibrate.Excerpt {
		return calibrate.Excerpt{Run: run, Job: "a job",
			Funcs: []counts.FuncCounts{{Cognitive: cognitive}}}
	}
	if len(runs) < 2 {
		runs = []string{fmt.Sprintf("r/left-%d", id),
			fmt.Sprintf("r/right-%d", id)}
	}
	return calibrate.Pair{ID: id, Set: set, Job: "a job",
		Canonical: fmt.Sprint(id), Left: side(runs[0], left),
		Right: side(runs[1], right)}
}

// judgedFixture is 2 tune and 13 check pairs. Paul picks the side with
// the lower count on every one but check pair 3, which he calls no
// difference: 12 check pairs have a side. The composite agrees on all
// 12; Astra on 9.
func judgedFixture() agreementReport {
	report := agreementReport{PairsPath: "results/pairs-x.json",
		CalibrationPath: "results/calibration-x.json",
		PicksPath:       "results/astra-picks-x.json", Seed: 7,
		Model: "gpt-6-astra", Effort: "high", PromptHash: "abc",
		Paul: map[int]calibrate.Verdict{}, Astra: map[int]calibrate.Verdict{}}
	for id := 1; id <= 15; id++ {
		set := calibrate.Check
		if id <= 2 {
			set = calibrate.Tune
		}
		report.Pairs = append(report.Pairs,
			scoredPair(id, set, 1, 5, "r/a", fmt.Sprintf("r/b%d", id)))
		report.Paul[id] = calibrate.LeftBetter
		report.Astra[id] = calibrate.LeftBetter
	}
	report.Paul[3] = calibrate.Same
	for _, id := range []int{4, 5, 6} {
		report.Astra[id] = calibrate.RightBetter
	}
	return report
}

func TestTheAgreementReportSaysWhichLayersAreTrusted(t *testing.T) {
	text := attemptText(judgedFixture())

	for _, want := range []string{
		"Cognitive 1, Nesting 1, Over limit 1, Duplicated 1, " +
			"Far short names 1",
		"| Composite, tuned weights | 12 of 12 | 100% | yes |",
		"| Astra | 9 of 12 | 75% | no |",
		"12 check pairs have a side",
		"results/calibration-x.json", "gpt-6-astra",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the report lacks %q:\n%s", want, text)
		}
	}
}

func TestTheAgreementReportShowsEachRunLeftOut(t *testing.T) {
	text := attemptText(judgedFixture())

	// Every check pair shows r/a: without it, none is left, so the
	// range is over the other runs, each in one pair.
	for _, want := range []string{
		"| Composite, tuned weights | 12 of 12 | 100% | yes | " +
			"100% (11 of 11, without r/b10) to 100% (11 of 11, without r/b10) |",
		"| Astra | 9 of 12 | 75% | no | " +
			"73% (8 of 11, without r/b10) to 82% (9 of 11, without r/b4) |",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the report lacks %q:\n%s", want, text)
		}
	}
}

func TestFewerThanTwelveDecidedCheckPairsTrustNoLayer(t *testing.T) {
	report := judgedFixture()
	report.Paul[7] = calibrate.NotSameJob

	text := attemptText(report)

	if !strings.Contains(text, "| Composite, tuned weights | 11 of 11 | "+
		"100% | no |") {
		t.Errorf("11 decided check pairs trusted the composite:\n%s", text)
	}
	if !strings.Contains(text, "only 11 check pairs have a side") {
		t.Errorf("the report does not say too few have a side:\n%s", text)
	}
}

func TestTheAgreementReportKeepsTheCheckpoint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agreement-x.md")
	os.WriteFile(path, []byte("old\n## Checkpoint 3\n\nNotes\n"), 0o644)

	if err := writeAgreement(path, "agreement-x", judgedFixture()); err != nil {
		t.Fatal(err)
	}

	text, _ := os.ReadFile(path)
	if !strings.HasSuffix(string(text), "\n## Checkpoint 3\n\nNotes\n") ||
		strings.HasPrefix(string(text), "old") {
		t.Errorf("the checkpoint was not kept:\n%s", text)
	}
}

func TestCachedPicksMadeOtherwiseAreRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "astra-picks.json")
	pairs := []calibrate.Pair{scoredPair(1, calibrate.Tune, 1, 2)}
	want := astraPicks{Pairs: "pairs-x.json", Seed: 7,
		Model: "gpt-6-astra", Effort: "high", PromptHash: "abc"}

	fresh, err := loadPicks(path, want, pairs)
	if err != nil || len(fresh.Picks) != 0 || fresh.PromptHash != "abc" {
		t.Fatalf("no file gave %+v, %v", fresh, err)
	}
	fresh.Picks = append(fresh.Picks, astraPick{PairID: 1, Canonical: "1",
		Verdict: calibrate.LeftBetter})
	if err := savePicks(path, fresh); err != nil {
		t.Fatal(err)
	}

	kept, err := loadPicks(path, want, pairs)
	if err != nil || len(kept.Picks) != 1 {
		t.Errorf("the same instructions gave %+v, %v", kept, err)
	}
	for name, other := range map[string]astraPicks{
		"prompt": {Pairs: "pairs-x.json", Seed: 7, Model: "gpt-6-astra",
			Effort: "high", PromptHash: "def"},
		"model": {Pairs: "pairs-x.json", Seed: 7, Model: "gpt-6",
			Effort: "high", PromptHash: "abc"},
		"pairs": {Pairs: "pairs-y.json", Seed: 7, Model: "gpt-6-astra",
			Effort: "high", PromptHash: "abc"},
	} {
		if _, err := loadPicks(path, other, pairs); err == nil {
			t.Errorf("picks made with another %s were reused", name)
		}
	}
	renamed := []calibrate.Pair{scoredPair(1, calibrate.Tune, 1, 2)}
	renamed[0].Canonical = "another pair"
	if _, err := loadPicks(path, want, renamed); err == nil {
		t.Error("a pick of another pair was reused")
	}
}

func TestACalibrationRecordsItsSeedSoPairsCanSkipIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calibration-x.json")
	if err := startCalibration(path, "results/pairs-x.json", 7); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var started calibrate.Calibration
	json.Unmarshal(raw, &started)
	if started.Seed != 7 || started.Pairs != "results/pairs-x.json" {
		t.Errorf("started %+v", started)
	}
	started.Judgements = append(started.Judgements, calibrate.Judgement{
		PairID: 1, Canonical: "a: x | b: x", Verdict: calibrate.Same})
	raw, _ = json.Marshal(started)
	os.WriteFile(path, raw, 0o644)

	// Started again, it resumes; with another seed or pairs, it refuses.
	if err := startCalibration(path, "results/pairs-x.json", 7); err != nil {
		t.Errorf("resuming: %v", err)
	}
	if err := startCalibration(path, "results/pairs-x.json", 8); err == nil {
		t.Error("a calibration was resumed with another seed")
	}
	if err := startCalibration(path, "results/pairs-y.json", 7); err == nil {
		t.Error("a calibration was resumed on another pairs file")
	}
	judged, err := judgedPairs(path, 7)
	if err != nil || len(judged) != 1 || judged[0] != "a: x | b: x" {
		t.Errorf("bench pairs -judged read %v, %v", judged, err)
	}
}

func TestEveryAttemptIsKeptInTheReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agreement-x.md")
	first := judgedFixture()
	revised := judgedFixture()
	revised.PairsPath = "results/pairs-y.json"
	revised.CalibrationPath = "results/calibration-y.json"
	revised.Fixed = &calibrate.Weights{Cognitive: 1, Nesting: 3,
		OverLimit: 1, Duplicated: 1, FarShortNames: 1}

	for _, report := range []agreementReport{first, revised, first} {
		if err := writeAgreement(path, "agreement-x", report); err != nil {
			t.Fatal(err)
		}
	}

	raw, _ := os.ReadFile(path)
	text := string(raw)
	// Writing the first attempt again replaces it, in its place.
	if strings.Count(text, "\n## Attempt ") != 2 {
		t.Fatalf("want two attempts:\n%s", text)
	}
	one := strings.Index(text, "## Attempt 1: results/pairs-x.json, "+
		"tuned weights")
	two := strings.Index(text, "## Attempt 2: results/pairs-y.json, "+
		"fixed weights")
	if one < 0 || two < one {
		t.Errorf("the attempts are not listed in order:\n%s", text)
	}
	if !strings.Contains(text[two:], "| Composite, fixed weights | ") ||
		!strings.Contains(text[two:], "Cognitive 1, Nesting 3, Over limit 1") {
		t.Errorf("the revision does not report its fixed weights:\n%s",
			text[two:])
	}
}

// Checking other weights against the same pairs is another attempt: it
// is listed beside the first, so a repeated check on the held-out pairs
// shows, and the same weights again replace their own attempt.
func TestEachSetOfWeightsIsItsOwnAttempt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agreement-x.md")
	weighed := func(nesting float64) agreementReport {
		report := judgedFixture()
		report.Fixed = &calibrate.Weights{Cognitive: 1, Nesting: nesting,
			OverLimit: 1, Duplicated: 1, FarShortNames: 1}
		return report
	}

	for _, report := range []agreementReport{weighed(3), weighed(0),
		weighed(3)} {
		if err := writeAgreement(path, "agreement-x", report); err != nil {
			t.Fatal(err)
		}
	}

	raw, _ := os.ReadFile(path)
	text := string(raw)
	if strings.Count(text, "\n## Attempt ") != 2 {
		t.Fatalf("want two attempts, one per set of weights:\n%s", text)
	}
	if !strings.Contains(text, "## Attempt 1: results/pairs-x.json, "+
		"fixed weights 1,3,1,1,1") || !strings.Contains(text,
		"## Attempt 2: results/pairs-x.json, fixed weights 1,0,1,1,1") {
		t.Errorf("the attempts are not named by their weights:\n%s", text)
	}
}

func TestFixedWeightsAreParsed(t *testing.T) {
	weights, err := parseWeights("1,2,0,1,3")
	if err != nil || weights != (calibrate.Weights{Cognitive: 1,
		Nesting: 2, Duplicated: 1, FarShortNames: 3}) {
		t.Errorf("parseWeights = %+v, %v", weights, err)
	}
	for _, bad := range []string{"1,2,3", "1,2,0,1,x", "1,2,0,1,-1", ""} {
		if _, err := parseWeights(bad); err == nil {
			t.Errorf("parseWeights(%q) succeeded", bad)
		}
	}
}

func TestLabelsFollowThePairsFilesDate(t *testing.T) {
	for path, want := range map[string]string{
		"results/pairs-2026-11-22.json": "calibration-2026-11-22",
		"pairs-2026-11-23.json":         "calibration-2026-11-23",
		"results/revision.json":         "calibration-revision",
	} {
		if got := labelFor(path, "calibration"); got != want {
			t.Errorf("labelFor(%q) = %q, want %q", path, got, want)
		}
	}
}
