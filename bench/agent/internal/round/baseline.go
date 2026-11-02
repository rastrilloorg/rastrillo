package round

import (
	"fmt"
	"sort"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/brief"
)

// Tolerance is how far a later round may drift above the baseline, as a
// fraction. Cost and context vary between runs of the same commit, so
// they get more room than page weight, which barely moves.
type Tolerance struct{ Cost, Context, Weight float64 }

// Limits are the carlos arm's medians for one brief at the baseline.
type Limits struct {
	BuildCostUSD, FirstWriteContext, HomeWireBytes float64
}

// Baseline is what Rastrillo's efficiency is held to between rounds. It
// is written from a round on purpose (bench baseline) and committed, so
// moving it is a reviewed change, never a side effect of a bad week.
type Baseline struct {
	Round     string
	Tolerance Tolerance
	Briefs    map[string]Limits
}

func BaselineFrom(r Round, tolerance Tolerance) Baseline {
	baseline := Baseline{
		Round: r.Label, Tolerance: tolerance, Briefs: map[string]Limits{},
	}
	for _, name := range briefNames(r) {
		runs := runsOf(r, name, brief.Carlos)
		if len(runs) == 0 {
			continue
		}
		baseline.Briefs[name] = limitsOf(runs)
	}
	return baseline
}

func limitsOf(runs []RunRecord) Limits {
	var cost, context, weight []float64
	for _, run := range runs {
		if run.Build.Complete {
			cost = append(cost, run.Build.CostUSD)
		}
		if run.Build.FirstWriteContext > 0 {
			context = append(context, float64(run.Build.FirstWriteContext))
		}
		for _, page := range run.Measured[0].Pages {
			if page.Role == "home" && page.Failure == "" {
				weight = append(weight, float64(page.Weight.WireTotal()))
			}
		}
	}
	return Limits{
		BuildCostUSD:      median(cost),
		FirstWriteContext: median(context),
		HomeWireBytes:     median(weight),
	}
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sort.Float64s(values)
	return spreadOf(values).Median
}

// Check names each measure of each brief where the round's carlos
// median is above the baseline by more than the tolerance. A brief the
// round did not run, or a measure the baseline has no value for, is not
// checked.
func Check(r Round, b Baseline) []string {
	var problems []string
	names := make([]string, 0, len(b.Briefs))
	for name := range b.Briefs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		runs := runsOf(r, name, brief.Carlos)
		if len(runs) == 0 {
			continue
		}
		now, then := limitsOf(runs), b.Briefs[name]
		for _, measure := range []struct {
			label     string
			now, then float64
			tolerance float64
		}{
			{"build cost ($)", now.BuildCostUSD, then.BuildCostUSD, b.Tolerance.Cost},
			{"context at first write", now.FirstWriteContext, then.FirstWriteContext, b.Tolerance.Context},
			{"home page bytes after build", now.HomeWireBytes, then.HomeWireBytes, b.Tolerance.Weight},
		} {
			limit := measure.then * (1 + measure.tolerance)
			// No sample is a failure, not a pass: every measure is
			// positive when it exists, so zero means nothing was measured.
			if measure.then > 0 && measure.now == 0 {
				problems = append(problems, fmt.Sprintf(
					"%s: %s was not measured in round %s", name, measure.label, r.Label))
				continue
			}
			if measure.then > 0 && measure.now > limit {
				problems = append(problems, fmt.Sprintf(
					"%s: %s is %.4g, over the baseline's %.4g by more than %.0f%% (round %s)",
					name, measure.label, measure.now, measure.then,
					measure.tolerance*100, b.Round))
			}
		}
	}
	return problems
}
