package calibrate

import (
	"strings"
	"testing"
)

// countedPair is a pair whose sides have the counts given, one function
// each: cognitive, nesting, over-limit, duplicated, far short names.
func countedPair(id int, set string, left, right [5]int) Pair {
	return Pair{ID: id, Set: set, Job: "a job",
		Left: Excerpt{Run: "r/left", Job: "a job",
			Funcs: excerptFuncs(left)},
		Right: Excerpt{Run: "r/right", Job: "a job",
			Funcs: excerptFuncs(right)}}
}

func TestAgreementCountsOnlyDecidedPairs(t *testing.T) {
	var pairs []Pair
	for id := 1; id <= 6; id++ {
		pairs = append(pairs, countedPair(id, Check, [5]int{}, [5]int{}))
	}
	pairs = append(pairs, countedPair(7, Spare, [5]int{}, [5]int{}),
		countedPair(8, Tune, [5]int{}, [5]int{}))
	paul := map[int]Verdict{1: LeftBetter, 2: RightBetter, 3: Same,
		4: NotSameJob, 5: LeftBetter, 7: RightBetter, 8: LeftBetter}
	other := map[int]Verdict{1: LeftBetter, 2: LeftBetter, 3: Same,
		4: NotSameJob, 5: Same, 6: LeftBetter, 7: RightBetter, 8: LeftBetter}

	agreed, decided := Agreement(pairs, paul, other, Check)

	// 1, 2, 5 and the spare 7 have a side; 1 and 7 agree. A "no
	// difference" against Paul's side is a disagreement; the tune pair
	// and the pairs Paul gave no side are not counted.
	if agreed != 2 || decided != 4 {
		t.Errorf("Agreement = %d of %d, want 2 of 4", agreed, decided)
	}
	agreed, decided = Agreement(pairs, paul, other, Tune)
	if agreed != 1 || decided != 1 {
		t.Errorf("on tune pairs = %d of %d, want 1 of 1", agreed, decided)
	}
}

func TestTheCompositePicksTheLowerSide(t *testing.T) {
	pairs := []Pair{
		countedPair(1, Check, [5]int{2}, [5]int{5}),
		countedPair(2, Check, [5]int{5}, [5]int{2}),
		countedPair(3, Check, [5]int{0, 3}, [5]int{3}),
	}

	picks := CompositeVerdicts(pairs, Even)

	want := map[int]Verdict{1: LeftBetter, 2: RightBetter, 3: Same}
	for id, verdict := range want {
		if picks[id] != verdict {
			t.Errorf("pair %d: %s, want %s", id, picks[id], verdict)
		}
	}
}

// nestingMatters is a tune pair Paul decides against Even: five of
// cognitive complexity reads better to him than three levels of
// nesting, so nesting must weigh more.
func nestingMatters() Pair {
	return countedPair(1, Tune, [5]int{5}, [5]int{0, 3})
}

func TestTuneFindsWeightsThatAgree(t *testing.T) {
	pairs := []Pair{nestingMatters()}
	paul := map[int]Verdict{1: LeftBetter}
	if agreed, _ := Agreement(pairs, paul, CompositeVerdicts(pairs, Even),
		Tune); agreed != 0 {
		t.Fatal("Even already agrees: the test shows nothing")
	}

	weights := TuneWeights(pairs, paul)

	agreed, decided := Agreement(pairs, paul,
		CompositeVerdicts(pairs, weights), Tune)
	if agreed != 1 || decided != 1 {
		t.Errorf("tuned %+v agrees on %d of %d", weights, agreed, decided)
	}
	for _, weight := range []float64{weights.Cognitive, weights.Nesting,
		weights.OverLimit, weights.Duplicated, weights.FarShortNames} {
		if weight != 0 && weight != 1 && weight != 2 && weight != 3 {
			t.Errorf("weight %v is not one of 0, 1, 2, 3", weight)
		}
	}
	// Weights 0 and 1, or 1 and 2, for cognitive and nesting agree and
	// are as near Even: a weight of 0 would drop a count, so 1 and 2.
	if want := (Weights{1, 2, 1, 1, 1}); weights != want {
		t.Errorf("tuned %+v, want %+v", weights, want)
	}
}

func TestTuneUsesOnlyTunePairs(t *testing.T) {
	pairs := []Pair{nestingMatters()}
	paul := map[int]Verdict{1: LeftBetter}
	want := TuneWeights(pairs, paul)
	// Check and spare pairs Paul decides the other way: were they used,
	// nesting would weigh less than cognitive complexity.
	for id := 2; id <= 9; id++ {
		set := Check
		if id > 6 {
			set = Spare
		}
		pairs = append(pairs, countedPair(id, set, [5]int{5}, [5]int{0, 3}))
		paul[id] = RightBetter
	}

	if got := TuneWeights(pairs, paul); got != want {
		t.Errorf("with check verdicts Tune = %+v, want %+v", got, want)
	}
}

func TestTuneTiesGoToEvenWeights(t *testing.T) {
	agreeing := []Pair{
		countedPair(1, Tune, [5]int{1, 1, 1, 1, 1}, [5]int{2, 2, 2, 2, 2}),
		countedPair(2, Tune, [5]int{0, 0, 4}, [5]int{0, 0, 1}),
	}
	for _, test := range []struct {
		name  string
		pairs []Pair
		paul  map[int]Verdict
	}{
		{"no verdicts", agreeing, nil},
		{"no side picked", agreeing, map[int]Verdict{1: Same, 2: NotSameJob}},
		{"Even agrees already", agreeing,
			map[int]Verdict{1: LeftBetter, 2: RightBetter}},
	} {
		if got := TuneWeights(test.pairs, test.paul); got != Even {
			t.Errorf("%s: Tune = %+v, want Even", test.name, got)
		}
	}
}

func TestLeavingEachRunOutGivesTheRange(t *testing.T) {
	pair := func(id int, left, right string) Pair {
		return Pair{ID: id, Set: Check, Left: Excerpt{Run: left},
			Right: Excerpt{Run: right}}
	}
	pairs := []Pair{pair(1, "r/a", "r/b"), pair(2, "r/a", "r/c"),
		pair(3, "r/c", "r/d")}
	paul := map[int]Verdict{1: LeftBetter, 2: LeftBetter, 3: LeftBetter}
	other := map[int]Verdict{1: LeftBetter, 2: RightBetter, 3: LeftBetter}

	low, high := LeaveOneRunOut(pairs, paul, other, Check)

	// Without r/b: pairs 2 and 3, one agrees. Without r/a: pair 3 only.
	if low != (Share{Without: "r/b", Agreed: 1, Decided: 2}) {
		t.Errorf("low = %+v, want 1 of 2 without r/b", low)
	}
	if high != (Share{Without: "r/a", Agreed: 1, Decided: 1}) {
		t.Errorf("high = %+v, want 1 of 1 without r/a", high)
	}
}

func TestAstraSeesTheSameInstructionAndSides(t *testing.T) {
	pairs := judgingPairs(1, 0, 0)

	prompt := astraPrompt(pairs[0])

	if !strings.Contains(prompt, Instruction+"\n\n"+NotSameJobHint) {
		t.Error("Astra's prompt lacks the page's instruction and hint")
	}
	left := strings.Index(prompt, pairs[0].Left.Text)
	right := strings.Index(prompt, pairs[0].Right.Text)
	if left < 0 || right < 0 || left > right {
		t.Error("Astra does not see left then right")
	}
	for _, hidden := range []string{"zebra-round", "trimming", "4711",
		"carlos"} {
		if strings.Contains(prompt, hidden) {
			t.Errorf("Astra's prompt shows %q", hidden)
		}
	}
}

func TestAstrasVerdictIsReadFromItsReply(t *testing.T) {
	for reply, want := range map[string]Verdict{
		`{"verdict": "left"}`: LeftBetter,
		"Right is plainer.\n```json\n{\"verdict\":\"right\"}\n```": RightBetter,
		`{"verdict": "same"}`:         Same,
		`{"verdict": "not-same-job"}`: NotSameJob,
	} {
		got, err := verdictFrom(reply)
		if err != nil || got != want {
			t.Errorf("verdictFrom(%q) = %q, %v; want %q", reply, got, err,
				want)
		}
	}
	for _, reply := range []string{"left", `{"verdict": "both"}`, ""} {
		if _, err := verdictFrom(reply); err == nil {
			t.Errorf("verdictFrom(%q) found a verdict", reply)
		}
	}
}
