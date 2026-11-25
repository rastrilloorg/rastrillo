package calibrate

import (
	"cmp"
	"maps"
	"math"
	"math/rand/v2"
	"slices"
	"strings"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/brief"
)

// The sets a pair can be in. Tune pairs settle the composite's weights
// and check pairs test them; spares are shown while too few check pairs
// have a side. Across pairs compare two arms (ChooseAcross).
const (
	Tune   = "tune"
	Check  = "check"
	Spare  = "spare"
	Across = "across"
)

// How many pairs of each set Choose makes.
const (
	tunePairs  = 15
	checkPairs = 15
	sparePairs = 10
)

// Pair is two excerpts of the same job from two runs, in the order they
// are shown. Canonical names the two excerpts whichever side each is
// on, so a pair judged once is known in either order.
type Pair struct {
	ID          int
	Canonical   string
	Job         string
	Left, Right Excerpt
	Set         string
}

// canonical is the two excerpts' run and job, sorted.
func canonical(a, b Excerpt) string {
	sides := []string{a.Run + ": " + a.Job, b.Run + ": " + b.Job}
	slices.Sort(sides)
	return strings.Join(sides, " | ")
}

// candidate is a pair that could be chosen.
type candidate struct {
	left, right Excerpt
	canonical   string
	difference  float64
}

// Choose picks 15 tune, 15 check and 10 spare pairs. The seed splits
// the runs in half: tune pairs come from one half and check and spare
// pairs from the other, so the weights are never checked on the apps
// they were tuned on. Each pair is one job in two runs, never one whose
// Canonical is in judged. Each set is spread evenly over the composite
// difference under Even, across the range both halves reach, so that
// some pairs are close and some far apart, and shows no excerpt in more
// than two pairs. Left and right are shuffled. A pool too small for
// that gives a set fewer pairs.
func Choose(excerpts []Excerpt, seed int64, judged []string) []Pair {
	random := rand.New(rand.NewPCG(uint64(seed), uint64(seed)))
	runs := runsIn(excerpts)
	random.Shuffle(len(runs), func(i, j int) {
		runs[i], runs[j] = runs[j], runs[i]
	})
	half := len(runs) / 2
	tunePool := candidates(within(excerpts, runs[:half]), judged, random)
	otherPool := candidates(within(excerpts, runs[half:]), judged, random)

	low, high := commonRange(tunePool, otherPool)
	tune, _ := spread(tunePool, tunePairs, low, high)
	check, rest := spread(otherPool, checkPairs, low, high)
	spare, _ := spread(rest, sparePairs, low, high)

	var pairs []Pair
	for _, set := range []struct {
		name   string
		chosen []candidate
	}{{Tune, tune}, {Check, check}, {Spare, spare}} {
		for _, one := range set.chosen {
			pairs = append(pairs, shown(one, set.name, len(pairs)+1, random))
		}
	}
	return pairs
}

// shown makes a candidate a pair, its sides in random order.
func shown(one candidate, set string, id int, random *rand.Rand) Pair {
	left, right := one.left, one.right
	if random.IntN(2) == 1 {
		left, right = right, left
	}
	return Pair{ID: id, Canonical: one.canonical, Job: left.Job,
		Left: left, Right: right, Set: set}
}

func runsIn(excerpts []Excerpt) []string {
	var runs []string
	for _, excerpt := range excerpts {
		runs = append(runs, excerpt.Run)
	}
	slices.Sort(runs)
	return slices.Compact(runs)
}

func within(excerpts []Excerpt, runs []string) []Excerpt {
	var found []Excerpt
	for _, excerpt := range excerpts {
		if slices.Contains(runs, excerpt.Run) && excerpt.pairable() {
			found = append(found, excerpt)
		}
	}
	return found
}

// candidates are every pair of one job in two runs not yet judged, in
// an order the seed shuffles, which settles ties.
func candidates(
	excerpts []Excerpt, judged []string, random *rand.Rand,
) []candidate {
	slices.SortFunc(excerpts, func(a, b Excerpt) int {
		return cmp.Or(cmp.Compare(a.Job, b.Job), cmp.Compare(a.Run, b.Run))
	})
	var found []candidate
	for index, left := range excerpts {
		for _, right := range excerpts[index+1:] {
			name := canonical(left, right)
			if left.Job != right.Job || left.Run == right.Run ||
				slices.Contains(judged, name) {
				continue
			}
			found = append(found, candidate{left: left, right: right,
				canonical: name, difference: math.Abs(
					Composite(left, Even) - Composite(right, Even))})
		}
	}
	random.Shuffle(len(found), func(i, j int) {
		found[i], found[j] = found[j], found[i]
	})
	return found
}

// commonRange is the span of composite difference every pool reaches,
// or, when they do not overlap, the span of them all.
func commonRange(pools ...[]candidate) (low, high float64) {
	low, high = math.Inf(-1), math.Inf(1)
	lowest, highest := math.Inf(1), math.Inf(-1)
	for _, pool := range pools {
		if len(pool) == 0 {
			continue
		}
		least, most := math.Inf(1), math.Inf(-1)
		for _, one := range pool {
			least, most = min(least, one.difference), max(most, one.difference)
		}
		low, high = max(low, least), min(high, most)
		lowest, highest = min(lowest, least), max(highest, most)
	}
	if low > high || math.IsInf(low, 0) {
		return lowest, highest
	}
	return low, high
}

// spread picks up to count pairs from the pool whose differences sit
// nearest evenly spaced targets from low to high, and returns them and
// the pairs left. A pair showing an excerpt the set already shows twice
// is never taken, so one app that alone reaches the far differences
// cannot carry a set's verdicts; when no pair is left under that cap,
// the set stops short. Of two as near, the one whose excerpts the set
// has shown fewer times wins.
func spread(
	pool []candidate, count int, low, high float64,
) (chosen, rest []candidate) {
	set := setChoice{taken: make([]bool, len(pool)), shown: map[string]int{}}
	for step := range min(count, len(pool)) {
		target := low
		if count > 1 {
			target += (high - low) * float64(step) / float64(count-1)
		}
		best := set.nearest(pool, target)
		if best < 0 {
			break
		}
		set.taken[best] = true
		chosen = append(chosen, pool[best])
		set.shown[excerptName(pool[best].left)]++
		set.shown[excerptName(pool[best].right)]++
	}
	for index, one := range pool {
		if !set.taken[index] {
			rest = append(rest, one)
		}
	}
	return chosen, rest
}

// setChoice is one set being chosen: which of the pool it has taken and
// how often it has shown each excerpt.
type setChoice struct {
	taken []bool
	shown map[string]int
}

// maxShown is how many of a set's pairs may show one excerpt.
const maxShown = 2

// nearest is the untaken pair nearest target that shows no excerpt
// maxShown times already, or -1 when there is none.
func (set setChoice) nearest(pool []candidate, target float64) int {
	best := -1
	for index, one := range pool {
		if set.taken[index] || set.full(one) {
			continue
		}
		if best < 0 || set.nearer(one, pool[best], target) {
			best = index
		}
	}
	return best
}

func (set setChoice) full(one candidate) bool {
	return set.shown[excerptName(one.left)] >= maxShown ||
		set.shown[excerptName(one.right)] >= maxShown
}

func (set setChoice) nearer(one, best candidate, target float64) bool {
	distance := math.Abs(one.difference - target)
	bestDistance := math.Abs(best.difference - target)
	if distance != bestDistance {
		return distance < bestDistance
	}
	return set.times(one) < set.times(best)
}

func (set setChoice) times(one candidate) int {
	return set.shown[excerptName(one.left)] + set.shown[excerptName(one.right)]
}

func excerptName(excerpt Excerpt) string {
	return excerpt.Run + ": " + excerpt.Job
}

// Gaps counts one arm's excerpts, and those that cannot be paired: the
// jobs a run does not do in its own code (Missing) and those too long
// to show (Oversized).
type Gaps struct{ Excerpts, Missing, Oversized int }

// Coverage is each arm's Gaps.
type Coverage map[brief.Arm]Gaps

// ChooseAcross pairs the baseline arm with the other on every job, run
// for run: a round's first run of a brief in one arm against its first
// in the other, and so on. There is no sampling by difference: every
// job both runs can show is a pair, its sides shuffled, in set Across.
// Coverage says, per arm, what could not be paired.
func ChooseAcross(
	excerpts []Excerpt, baseline, other brief.Arm, seed int64,
) ([]Pair, Coverage) {
	random := rand.New(rand.NewPCG(uint64(seed), uint64(seed)))
	coverage := Coverage{baseline: {}, other: {}}
	slots := map[string]map[brief.Arm]Excerpt{}
	for _, excerpt := range excerpts {
		gaps, counted := coverage[excerpt.Arm]
		if !counted {
			continue
		}
		gaps.Excerpts++
		switch {
		case len(excerpt.Funcs) == 0:
			gaps.Missing++
		case excerpt.Lines() > MaxLines:
			gaps.Oversized++
		}
		coverage[excerpt.Arm] = gaps
		if !excerpt.pairable() {
			continue
		}
		slot := slotOf(excerpt) + "\x00" + excerpt.Job
		if slots[slot] == nil {
			slots[slot] = map[brief.Arm]Excerpt{}
		}
		slots[slot][excerpt.Arm] = excerpt
	}
	var pairs []Pair
	for _, slot := range slices.Sorted(maps.Keys(slots)) {
		sides := slots[slot]
		first, hasFirst := sides[baseline]
		second, hasSecond := sides[other]
		if hasFirst && hasSecond {
			pairs = append(pairs, shown(candidate{left: first, right: second,
				canonical: canonical(first, second)}, Across, len(pairs)+1,
				random))
		}
	}
	return pairs, coverage
}

// slotOf is the excerpt's run without its arm: read-1/groomer-carlos-2
// and read-1/groomer-carlos-gopls-2 are both read-1/groomer-2.
func slotOf(excerpt Excerpt) string {
	cut := strings.LastIndex(excerpt.Run, "-")
	if cut < 0 {
		return excerpt.Run
	}
	return strings.TrimSuffix(excerpt.Run[:cut], "-"+string(excerpt.Arm)) +
		excerpt.Run[cut:]
}
