package chart

import (
	"math/big"
	"strings"
)

// marks are a series drawn in a viewBox: a polyline's points for each
// run of values with no gap, a dot for a value alone between gaps, and
// the end dot on the last value.
type marks struct {
	runs, lone []string
	end        string
}

// drawMarks puts the values evenly from the inline start to width, and
// each from low at the bottom to high at the top. A missing value is
// never drawn, so the line breaks there rather than dropping to zero.
func drawMarks(
	values []*big.Rat, low, high *big.Rat, width, height int64,
) marks {
	var drawn marks
	var run []string
	last := lastPresent(values)
	for index, value := range values {
		if value == nil {
			drawn.close(run)
			run = nil
			continue
		}
		across := coordinate(spaced(index, len(values), width))
		top := new(big.Rat).Sub(big.NewRat(height, 1),
			along(value, low, high, height))
		down := coordinate(top)
		run = append(run, across+","+down)
		if index == last {
			drawn.end = "M" + across + " " + down + "h0"
		}
	}
	drawn.close(run)
	return drawn
}

// close ends a run: a line through two or more values, or a dot for
// one, unless the end dot already marks it.
func (drawn *marks) close(run []string) {
	switch {
	case len(run) > 1:
		drawn.runs = append(drawn.runs, strings.Join(run, " "))
	case len(run) == 1:
		across, down, _ := strings.Cut(run[0], ",")
		if dot := "M" + across + " " + down + "h0"; dot != drawn.end {
			drawn.lone = append(drawn.lone, dot)
		}
	}
}

// spaced is point index's x of count points spread from 0 to width.
func spaced(index, count int, width int64) *big.Rat {
	if count < 2 {
		return new(big.Rat)
	}
	return big.NewRat(int64(index)*width, int64(count-1))
}

func lastPresent(values []*big.Rat) int {
	for index := len(values) - 1; index >= 0; index-- {
		if values[index] != nil {
			return index
		}
	}
	return -1
}

func present(value *big.Rat) bool { return value != nil }
