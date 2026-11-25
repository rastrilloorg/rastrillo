package counts

import (
	"math"
	"slices"
)

// Spread is the middle and the bad end of one count across functions.
// P90 is the nearest-rank 90th percentile.
type Spread struct{ Median, P90 float64 }

// measures name each count Summarise reports, and read it off a function.
var measures = map[string]func(FuncCounts) int{
	"lines":           func(f FuncCounts) int { return f.Lines },
	"cognitive":       func(f FuncCounts) int { return f.Cognitive },
	"nesting":         func(f FuncCounts) int { return f.Nesting },
	"far-short-names": func(f FuncCounts) int { return f.FarShortNames },
	"duplicated":      func(f FuncCounts) int { return f.DuplicatedLines },
	"over-limit":      func(f FuncCounts) int { return f.OverLimitLines },
}

// Summarise is the spread of each count over the app's own functions.
// Tests are left out: SummariseTests reads those.
func Summarise(run RunCounts) map[string]Spread {
	return summarise(run.Funcs, false)
}

// SummariseTests is Summarise for test functions only.
func SummariseTests(run RunCounts) map[string]Spread {
	return summarise(run.Funcs, true)
}

func summarise(funcs []FuncCounts, tests bool) map[string]Spread {
	var chosen []FuncCounts
	for _, one := range funcs {
		if one.Test == tests {
			chosen = append(chosen, one)
		}
	}
	spreads := map[string]Spread{}
	for name, read := range measures {
		values := make([]float64, len(chosen))
		for index, one := range chosen {
			values[index] = float64(read(one))
		}
		spreads[name] = spreadOf(values)
	}
	return spreads
}

func spreadOf(values []float64) Spread {
	if len(values) == 0 {
		return Spread{}
	}
	slices.Sort(values)
	middle := len(values) / 2
	median := values[middle]
	if len(values)%2 == 0 {
		median = (values[middle-1] + values[middle]) / 2
	}
	rank := int(math.Ceil(0.9*float64(len(values)))) - 1
	return Spread{Median: median, P90: values[rank]}
}
