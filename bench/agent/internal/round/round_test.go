package round

import (
	"encoding/json"
	"flag"
	"os"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/brief"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/measure"
)

var update = flag.Bool("update", false, "rewrite testdata/round.md")

func run(arm brief.Arm, n int, cost float64, failures ...string) RunRecord {
	home := measure.Page{Role: "home", URL: "https://x.example/",
		Weight: measure.Weight{
			Wire:    map[measure.Kind]int{measure.HTML: 2000, measure.CSS: 10000 * n, measure.JS: 0},
			Decoded: map[measure.Kind]int{measure.HTML: 6000, measure.CSS: 40000 * n},
		},
		Axe:        map[string]int{"serious": n - 1},
		Lighthouse: map[string]int{"performance": 90 + n, "accessibility": 100},
	}
	return RunRecord{
		Brief: "groomer", Arm: arm, N: n,
		Build: Cost{Complete: true, NumTurns: 40 * n, Messages: 30 * n, CostUSD: cost,
			FirstWriteContext: 50000 * n, DeployMessages: 5},
		Followup: Cost{Complete: true, NumTurns: 10, CostUSD: 1},
		Measured: [2]measure.Measurement{
			{Pages: []measure.Page{home}, DeployOK: len(failures) == 0},
			{Pages: []measure.Page{home}, DeployOK: true},
		},
		AgentFailures: failures,
	}
}

func sample() Round {
	return Round{
		Label: "1", Date: "2026-10-30", Model: "claude-opus-5-5",
		ClaudeVersion: "2.1.312", Rastrillo: "abc123 (v0.30.0-bench.gabc123)",
		Platform: "Fly.io",
		Notes:    []string{"A note."},
		Runs: []RunRecord{
			run(brief.Carlos, 1, 7),
			run(brief.Carlos, 2, 5, "build: the deploy does not work"),
			run(brief.Carlos, 3, 9),
			run(brief.Control, 1, 3),
			run(brief.Control, 2, 3),
			run(brief.Control, 3, 4),
		},
	}
}

func TestSummariseTakesMedianAndCountsFailures(t *testing.T) {
	summary := Summarise(sample())

	cost := summary["groomer"][brief.Carlos]["build cost ($)"]
	if cost != (Spread{Median: 7, Min: 5, Max: 9, Failures: 1, Runs: 3, Samples: 3}) {
		t.Errorf("carlos build cost = %+v", cost)
	}
	css := summary["groomer"][brief.Carlos]["build home: CSS over the wire (kB)"]
	if css.Median != 20 || css.Min != 10 || css.Max != 30 {
		t.Errorf("carlos home CSS = %+v", css)
	}
	control := summary["groomer"][brief.Control]["build cost ($)"]
	if control.Median != 3 || control.Failures != 0 {
		t.Errorf("control build cost = %+v", control)
	}
}

func TestMarkdownMatchesGolden(t *testing.T) {
	got := Markdown(sample())
	if *update {
		os.WriteFile("testdata/round.md", []byte(got), 0o644)
	}
	want, err := os.ReadFile("testdata/round.md")
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("Markdown differs from testdata/round.md (-update to "+
			"rewrite after reading the diff):\n%s", got)
	}
}

func TestRoundSurvivesJSON(t *testing.T) {
	encoded, err := json.Marshal(sample())
	if err != nil {
		t.Fatal(err)
	}
	var decoded Round
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if Markdown(decoded) != Markdown(sample()) {
		t.Error("a round read back from JSON renders differently")
	}
}

func TestAnUnfinishedPhaseHasNoCostRatherThanZero(t *testing.T) {
	r := sample()
	r.Runs[1].Build.Complete = false // carlos 2 timed out: $5 unknown
	r.Runs[1].Build.CostUSD = 0

	cost := Summarise(r)["groomer"][brief.Carlos]["build cost ($)"]

	if cost.Samples != 2 || cost.Median != 8 || cost.Min != 7 {
		t.Errorf("cost = %+v, want the two finished runs only", cost)
	}
	if !strings.Contains(Markdown(r), "8.00 (7.00–9.00; 2 of 3)") {
		t.Errorf("a cell from fewer samples than runs does not say so:\n%s",
			Markdown(r))
	}
}

func TestSecurityHeadersCountFrameProtectionOnce(t *testing.T) {
	r := sample()
	r.Runs[0].Measured[0].Headers = map[string]string{
		"content-security-policy": "frame-ancestors 'none'",
		"frame-ancestors":         "frame-ancestors 'none'",
		"x-frame-options":         "DENY",
	}

	headers := Summarise(r)["groomer"][brief.Carlos]["build home: security headers present"]

	if headers.Median != 2 {
		t.Errorf("headers = %+v, want 2: the CSP, and frame protection once", headers)
	}
}

func TestMarkdownListsMeasurementNotes(t *testing.T) {
	r := sample()
	r.Runs[0].Measured[0].Failures = []string{"lighthouse: not installed, so not run"}

	if !strings.Contains(Markdown(r), "lighthouse: not installed") {
		t.Error("a measurement note is not in the report")
	}
}

func TestFingerprintChangesWithAnyInput(t *testing.T) {
	base := map[string]string{"arms/control.txt": "deploy it", "platform": "Fly.io"}
	same := map[string]string{"platform": "Fly.io", "arms/control.txt": "deploy it"}
	changed := map[string]string{"arms/control.txt": "deploy it!", "platform": "Fly.io"}

	if Fingerprint(base) != Fingerprint(same) {
		t.Error("the fingerprint depends on map order")
	}
	if Fingerprint(base) == Fingerprint(changed) {
		t.Error("a changed framing kept the same fingerprint")
	}
	if Fingerprint(map[string]string{"a": "bc"}) == Fingerprint(map[string]string{"ab": "c"}) {
		t.Error("keys and values run together")
	}
}

func TestASpecArmGetsItsOwnColumn(t *testing.T) {
	r := sample()
	r.Runs = append(r.Runs, run(brief.Spec, 1, 4), run(brief.Spec, 2, 6))

	if got := Summarise(r)["groomer"][brief.Spec]["build cost ($)"]; got.Median != 5 {
		t.Errorf("spec build cost = %+v, want median 5", got)
	}
	if !strings.Contains(Markdown(r), "| | carlos | control | spec |") {
		t.Errorf("no spec column:\n%s", Markdown(r)[:400])
	}
	if strings.Contains(Markdown(sample()), "spec |") {
		t.Error("a round without a spec arm grew a spec column")
	}
}

func TestFingerprintIncludesTheSpecWhenGiven(t *testing.T) {
	inputs := map[string]string{"arms/spec.txt": "meet the spec"}
	withSpec := map[string]string{"arms/spec.txt": "meet the spec",
		"spec.carlosframework.com": "rule 1"}
	changed := map[string]string{"arms/spec.txt": "meet the spec",
		"spec.carlosframework.com": "rule 1, revised"}
	if Fingerprint(inputs) == Fingerprint(withSpec) ||
		Fingerprint(withSpec) == Fingerprint(changed) {
		t.Error("a changed specification kept the round's fingerprint")
	}
}
