package calibrate

// Weights say how much each count adds to the composite.
type Weights struct {
	Cognitive, Nesting, OverLimit, Duplicated, FarShortNames float64
}

// Even weighs every count alike.
var Even = Weights{1, 1, 1, 1, 1}

// Composite is how hard the excerpt is to read by the counts: their
// weighted sum over every function in it, helpers included. Lower reads
// better. It is never divided by lines or functions, so splitting code
// into more pieces, or padding it, cannot dilute it; the excerpt's size
// is reported beside it (Lines), never folded in. A unit inside another
// adds only its over-limit lines: the rest of its counts are already in
// the unit around it, but counts gives an offence to the innermost unit
// alone.
func Composite(excerpt Excerpt, weights Weights) float64 {
	total := 0.0
	for _, one := range excerpt.Funcs {
		total += weights.Cognitive*float64(one.Cognitive) +
			weights.Nesting*float64(one.Nesting) +
			weights.OverLimit*float64(one.OverLimitLines) +
			weights.Duplicated*float64(one.DuplicatedLines) +
			weights.FarShortNames*float64(one.FarShortNames)
	}
	for _, one := range excerpt.Inner {
		total += weights.OverLimit * float64(one.OverLimitLines)
	}
	return total
}
