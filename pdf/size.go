package pdf

import "math"

// Size is a page size in points, portrait orientation (W is the
// narrower edge for every size this package names). [Options.Size]
// takes one of the named sizes below, or a caller's own: nothing about
// this package requires a standard size, so a Size can equally well be
// the exact dimensions of a pre-printed form or a window envelope.
// [Options.Landscape] swaps W and H at render time regardless of which
// Size was given, so there is no separate landscape constant for each.
type Size struct{ W, H float64 }

// Standard page sizes, in points (1pt = 1/72in). A4 is the zero value
// of [Options.Size], so a caller that never sets it gets A4 without
// needing to name it.
var (
	A4      = Size{595.28, 841.89}
	A3      = Size{841.89, 1190.55}
	A5      = Size{419.53, 595.28}
	Letter  = Size{612, 792}
	Legal   = Size{612, 1008}
	Tabloid = Size{792, 1224}
)

// finite reports whether s's own dimensions are usable at all:
// positive and finite. "> 0" already excludes NaN — a NaN comparison
// is false against everything, including zero — so only +Inf needs
// its own check. It does not know about margins — [newGeom] checks
// separately that what is left over, once they are subtracted, still
// leaves room for something — because margins are this package's own
// fixed concern and a Size is just a rectangle.
func (s Size) finite() bool {
	return s.W > 0 && s.H > 0 && !math.IsInf(s.W, 0) && !math.IsInf(s.H, 0)
}
