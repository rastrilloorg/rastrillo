package calibrate

import (
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/brief"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/counts"
)

// excerptFuncs makes one function per count set: cognitive, nesting,
// over-limit lines, duplicated lines and far short names.
func excerptFuncs(sets ...[5]int) []counts.FuncCounts {
	var funcs []counts.FuncCounts
	for index, set := range sets {
		funcs = append(funcs, counts.FuncCounts{
			Path: "app.go", Key: fmt.Sprintf("F%d", index), Lines: 5,
			Cognitive: set[0], Nesting: set[1], OverLimitLines: set[2],
			DuplicatedLines: set[3], FarShortNames: set[4],
		})
	}
	return funcs
}

var keptRounds = []string{"1", "accept-1", "cost-1", "weight-1"}

var groomerJobs = []string{"showing free slots", "booking",
	"owner sign-in", "editing a service", "editing opening hours",
	"listing bookings", "cancelling a booking", "seeding demo data",
	"validating a booking"}

// kept is twelve runs of nine jobs, as the kept groomer runs are, with
// composites spread from 0 to 40. One run does not seed demo data and
// another's booking is too long to show.
func kept() []Excerpt {
	random := rand.New(rand.NewPCG(7, 7))
	var excerpts []Excerpt
	for _, round := range keptRounds {
		for n := 1; n <= 3; n++ {
			run := fmt.Sprintf("%s/groomer-carlos-%d", round, n)
			for _, job := range groomerJobs {
				excerpt := Excerpt{Run: run, Job: job, Arm: brief.Carlos,
					Text:  "func F0() {}\n",
					Funcs: excerptFuncs([5]int{random.IntN(41), 0, 0, 0, 0})}
				switch {
				case run == "cost-1/groomer-carlos-2" &&
					job == "seeding demo data":
					excerpt.Funcs, excerpt.Text = nil, ""
				case run == "1/groomer-carlos-3" && job == "booking":
					excerpt.Text = strings.Repeat("x\n", 75)
				}
				excerpts = append(excerpts, excerpt)
			}
		}
	}
	return excerpts
}

func inSet(pairs []Pair, sets ...string) []Pair {
	var found []Pair
	for _, pair := range pairs {
		if slices.Contains(sets, pair.Set) {
			found = append(found, pair)
		}
	}
	return found
}

func runsOf(pairs []Pair) map[string]bool {
	runs := map[string]bool{}
	for _, pair := range pairs {
		runs[pair.Left.Run], runs[pair.Right.Run] = true, true
	}
	return runs
}

func difference(pair Pair) float64 {
	return math.Abs(Composite(pair.Left, Even) - Composite(pair.Right, Even))
}

func TestThereAreFifteenTuneFifteenCheckAndTenSpares(t *testing.T) {
	pairs := Choose(kept(), 20261122, nil)

	want := map[string]int{"tune": 15, "check": 15, "spare": 10}
	for set, want := range want {
		if got := len(inSet(pairs, set)); got != want {
			t.Errorf("%d %s pairs, want %d", got, set, want)
		}
	}
	if len(pairs) != 40 {
		t.Errorf("%d pairs, want 40", len(pairs))
	}
	ids := map[int]bool{}
	for _, pair := range pairs {
		ids[pair.ID] = true
	}
	if len(ids) != len(pairs) {
		t.Errorf("%d distinct IDs for %d pairs", len(ids), len(pairs))
	}
}

func TestTuneAndCheckPairsComeFromDifferentRuns(t *testing.T) {
	pairs := Choose(kept(), 20261122, nil)

	tune := runsOf(inSet(pairs, "tune"))
	for run := range runsOf(inSet(pairs, "check", "spare")) {
		if tune[run] {
			t.Errorf("%s gives both tune and check or spare pairs", run)
		}
	}
	if len(tune) == 0 {
		t.Error("no tune pairs")
	}
}

func TestPairsAreTheSameJobFromDifferentRuns(t *testing.T) {
	canonicals := map[string]bool{}
	for _, pair := range Choose(kept(), 20261122, nil) {
		if pair.Left.Job != pair.Job || pair.Right.Job != pair.Job {
			t.Errorf("pair %d mixes %q and %q as %q", pair.ID,
				pair.Left.Job, pair.Right.Job, pair.Job)
		}
		if pair.Left.Run == pair.Right.Run {
			t.Errorf("pair %d is %s against itself", pair.ID, pair.Left.Run)
		}
		for _, side := range []Excerpt{pair.Left, pair.Right} {
			if len(side.Funcs) == 0 || side.Lines() > MaxLines {
				t.Errorf("pair %d shows %s's %s, which is missing or too long",
					pair.ID, side.Run, side.Job)
			}
		}
		if canonicals[pair.Canonical] {
			t.Errorf("%s is chosen twice", pair.Canonical)
		}
		canonicals[pair.Canonical] = true
	}
}

// Tune and check pairs come from different halves of the runs, so each
// set alone is too few to spread; together they must.
func TestPairsSpreadAcrossTheCompositeDifference(t *testing.T) {
	pairs := inSet(Choose(kept(), 20261122, nil), "tune", "check")
	var differences []float64
	for _, pair := range pairs {
		differences = append(differences, difference(pair))
	}
	low, high := slices.Min(differences), slices.Max(differences)
	if high-low < 20 {
		t.Fatalf("differences run only from %v to %v", low, high)
	}

	var quintiles [5]int
	for _, value := range differences {
		quintiles[min(4, int(5*(value-low)/(high-low)))]++
	}

	for index, count := range quintiles {
		if count < 5 || count > 7 {
			t.Errorf("quintile %d of %v..%v holds %d pairs, want 5 to 7: %v",
				index+1, low, high, count, quintiles)
		}
	}
}

func TestTheSameSeedChoosesTheSamePairs(t *testing.T) {
	first := Choose(kept(), 20261122, nil)
	again := Choose(kept(), 20261122, nil)
	other := Choose(kept(), 20261123, nil)

	if !reflect.DeepEqual(first, again) {
		t.Error("the same seed chose different pairs")
	}
	if reflect.DeepEqual(first, other) {
		t.Error("another seed chose the same pairs")
	}
	swapped := 0
	for _, pair := range first {
		if pair.Left.Run > pair.Right.Run {
			swapped++
		}
	}
	if swapped == 0 || swapped == len(first) {
		t.Errorf("%d of %d pairs put the later run on the left: "+
			"left and right are not shuffled", swapped, len(first))
	}
}

func TestAJudgedPairIsNeverChosenAgainInEitherOrder(t *testing.T) {
	first := Choose(kept(), 20261122, nil)
	var judged []string
	for _, pair := range first {
		if canonical(pair.Left, pair.Right) !=
			canonical(pair.Right, pair.Left) {
			t.Fatalf("%s differs when its sides swap", pair.Canonical)
		}
		if pair.Canonical != canonical(pair.Left, pair.Right) {
			t.Fatalf("pair %d's Canonical is %q", pair.ID, pair.Canonical)
		}
		judged = append(judged, pair.Canonical)
	}

	for _, seed := range []int64{20261122, 20261123} {
		for _, pair := range Choose(kept(), seed, judged) {
			if slices.Contains(judged, pair.Canonical) {
				t.Errorf("seed %d chose judged pair %s again", seed,
					pair.Canonical)
			}
		}
	}
}

func across() []Excerpt {
	var excerpts []Excerpt
	for _, arm := range []brief.Arm{"carlos", "carlos-gopls"} {
		for n := 1; n <= 3; n++ {
			run := fmt.Sprintf("read-1/groomer-%s-%d", arm, n)
			for index, job := range groomerJobs[:3] {
				excerpt := Excerpt{Run: run, Job: job, Arm: arm,
					Text:  "func F0() {}\n",
					Funcs: excerptFuncs([5]int{index + n, 0, 0, 0, 0})}
				switch {
				case arm == "carlos-gopls" && n == 2 && index == 1:
					excerpt.Funcs, excerpt.Text = nil, ""
				case arm == "carlos" && n == 3 && index == 0:
					excerpt.Text = strings.Repeat("x\n", 61)
				}
				excerpts = append(excerpts, excerpt)
			}
		}
	}
	// Another arm's runs are never paired.
	return append(excerpts, Excerpt{Run: "read-1/groomer-control-1",
		Job: groomerJobs[0], Arm: "control", Text: "func F0() {}\n",
		Funcs: excerptFuncs([5]int{1, 0, 0, 0, 0})})
}

func TestChooseAcrossPairsTheTwoArmsByJobAndReportsCoverage(t *testing.T) {
	pairs, coverage := ChooseAcross(across(), "carlos", "carlos-gopls", 5)

	// Nine job and run slots, less one missing and one too long.
	if len(pairs) != 7 {
		t.Fatalf("%d pairs, want 7", len(pairs))
	}
	baselineLeft := 0
	for _, pair := range pairs {
		arms := []brief.Arm{pair.Left.Arm, pair.Right.Arm}
		slices.Sort(arms)
		if !reflect.DeepEqual(arms, []brief.Arm{"carlos", "carlos-gopls"}) {
			t.Errorf("pair %d is %v", pair.ID, arms)
		}
		if pair.Left.Job != pair.Job || pair.Right.Job != pair.Job {
			t.Errorf("pair %d mixes jobs", pair.ID)
		}
		index := func(run string) string { return run[len(run)-1:] }
		if index(pair.Left.Run) != index(pair.Right.Run) {
			t.Errorf("pair %d is %s against %s", pair.ID, pair.Left.Run,
				pair.Right.Run)
		}
		if pair.Left.Arm == "carlos" {
			baselineLeft++
		}
	}
	if baselineLeft == 0 || baselineLeft == len(pairs) {
		t.Errorf("the baseline is on the left in %d of %d pairs: "+
			"left and right are not shuffled", baselineLeft, len(pairs))
	}
	want := Coverage{
		"carlos":       {Excerpts: 9, Missing: 0, Oversized: 1},
		"carlos-gopls": {Excerpts: 9, Missing: 1, Oversized: 0},
	}
	if !reflect.DeepEqual(coverage, want) {
		t.Errorf("coverage = %+v, want %+v", coverage, want)
	}
}

// One app whose excerpt alone reaches the far differences would sit in
// every distant pair, and one judgement of it would count many times.
func TestNoExcerptIsShownMoreThanTwiceInASet(t *testing.T) {
	var excerpts []Excerpt
	for n := range 20 {
		excerpts = append(excerpts, Excerpt{
			Run: fmt.Sprintf("1/groomer-carlos-%d", n), Job: "booking",
			Arm: brief.Carlos, Text: "func F0() {}\n",
			Funcs: excerptFuncs([5]int{n % 2, 0, 0, 0, 0})})
	}
	excerpts[0].Funcs = excerptFuncs([5]int{10, 0, 0, 0, 0})
	pool := candidates(excerpts, nil, rand.New(rand.NewPCG(1, 1)))
	low, high := commonRange(pool)

	chosen, _ := spread(pool, 15, low, high)

	if len(chosen) != 15 {
		t.Fatalf("%d pairs chosen, want 15", len(chosen))
	}
	shown := map[string]int{}
	for _, one := range chosen {
		shown[excerptName(one.left)]++
		shown[excerptName(one.right)]++
	}
	for name, times := range shown {
		if times > 2 {
			t.Errorf("%s is in %d of the set's pairs, want at most 2",
				name, times)
		}
	}
	if shown[excerptName(excerpts[0])] != 2 {
		t.Errorf("the far excerpt is in %d pairs, want 2: the far end "+
			"is still reached while it is under the cap",
			shown[excerptName(excerpts[0])])
	}
}

// The cap is hard: when every pair left would show an excerpt a third
// time, the set stops short, and the caller's insufficient-pool error
// says so. Eight excerpts of one job make 28 pairs but only 8 under it.
func TestTheCapIsHard(t *testing.T) {
	var excerpts []Excerpt
	for n := range 8 {
		excerpts = append(excerpts, Excerpt{
			Run: fmt.Sprintf("1/groomer-carlos-%d", n), Job: "booking",
			Arm: brief.Carlos, Text: "func F0() {}\n",
			Funcs: excerptFuncs([5]int{n, 0, 0, 0, 0})})
	}
	pool := candidates(excerpts, nil, rand.New(rand.NewPCG(1, 1)))

	chosen, rest := spread(pool, 15, 0, 7)

	if len(chosen) > 8 || len(chosen)+len(rest) != len(pool) {
		t.Errorf("chose %d and left %d of %d pairs, want at most 8 chosen",
			len(chosen), len(rest), len(pool))
	}
	shown := map[string]int{}
	for _, one := range chosen {
		shown[excerptName(one.left)]++
		shown[excerptName(one.right)]++
	}
	for name, times := range shown {
		if times > maxShown {
			t.Errorf("%s is in %d of the set's pairs", name, times)
		}
	}
}

func TestEverySetOfTheKeptRunsShowsAnExcerptAtMostTwice(t *testing.T) {
	shown := map[string]int{}
	for _, pair := range Choose(kept(), 20261122, nil) {
		for _, side := range []Excerpt{pair.Left, pair.Right} {
			key := pair.Set + " " + excerptName(side)
			if shown[key]++; shown[key] > 2 {
				t.Errorf("%s is shown %d times", key, shown[key])
			}
		}
	}
}
