package round

import (
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/brief"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/measure"
)

func TestBaselineFromTakesTheCarlosMedians(t *testing.T) {
	baseline := BaselineFrom(sample(), Tolerance{Cost: 0.15, Context: 0.15, Weight: 0.05})

	limits := baseline.Briefs["groomer"]
	if limits.BuildCostUSD != 7 || limits.FirstWriteContext != 100000 {
		t.Errorf("groomer = %+v, want cost 7 and context 100000", limits)
	}
	if limits.HomeWireBytes != 22000 {
		t.Errorf("home = %v, want the median 22000", limits.HomeWireBytes)
	}
}

func TestCheckNamesARegressionBeyondTolerance(t *testing.T) {
	baseline := BaselineFrom(sample(), Tolerance{Cost: 0.15, Context: 0.15, Weight: 0.05})

	if problems := Check(sample(), baseline); len(problems) != 0 {
		t.Errorf("the baseline's own round regressed: %v", problems)
	}
	worse := sample()
	for i := range worse.Runs {
		if worse.Runs[i].Arm == brief.Carlos {
			worse.Runs[i].Build.CostUSD *= 1.3
		}
	}
	problems := Check(worse, baseline)
	if len(problems) != 1 || !strings.Contains(problems[0], "groomer") ||
		!strings.Contains(problems[0], "build cost") {
		t.Errorf("problems = %v, want one naming groomer's build cost", problems)
	}
	slightly := sample()
	for i := range slightly.Runs {
		slightly.Runs[i].Build.CostUSD *= 1.1
	}
	if problems := Check(slightly, baseline); len(problems) != 0 {
		t.Errorf("a 10%% move within a 15%% tolerance failed: %v", problems)
	}
}

func TestCheckSkipsABriefTheRoundDidNotRun(t *testing.T) {
	baseline := BaselineFrom(sample(), Tolerance{Cost: 0.15})
	baseline.Briefs["lending"] = Limits{BuildCostUSD: 1}

	if problems := Check(sample(), baseline); len(problems) != 0 {
		t.Errorf("a brief the round did not run was checked: %v", problems)
	}
}

func TestCheckFailsARoundWithNothingMeasured(t *testing.T) {
	baseline := BaselineFrom(sample(), Tolerance{Cost: 0.15, Context: 0.15, Weight: 0.05})
	blank := sample()
	for i := range blank.Runs {
		blank.Runs[i].Build = Cost{}
		blank.Runs[i].Measured = [2]measure.Measurement{}
	}

	if problems := Check(blank, baseline); len(problems) != 3 {
		t.Errorf("problems = %v, want one per unmeasured measure", problems)
	}
}
