package calibrate

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/codex"
)

// countsAs is whether the pair counts as one of set. A spare is a check
// pair held back, shown only when too few check pairs had a side, so
// it counts as a check pair.
func countsAs(pair Pair, set string) bool {
	return pair.Set == set || set == Check && pair.Set == Spare
}

// Agreement counts, of the set's pairs Paul gave a side, how many the
// other judge gave the same side. A pair the other judge called no
// difference, or not the same job, disagrees; a pair Paul gave no side
// is not counted at all.
func Agreement(
	pairs []Pair, paul, other map[int]Verdict, set string,
) (agreed, decided int) {
	for _, pair := range pairs {
		verdict := paul[pair.ID]
		if !countsAs(pair, set) || !verdict.Decided() {
			continue
		}
		decided++
		if other[pair.ID] == verdict {
			agreed++
		}
	}
	return agreed, decided
}

// CompositeVerdicts is the composite's verdict on each pair: the side
// with the lower composite reads better, and equal composites make no
// difference.
func CompositeVerdicts(pairs []Pair, weights Weights) map[int]Verdict {
	verdicts := map[int]Verdict{}
	for _, pair := range pairs {
		left, right := Composite(pair.Left, weights),
			Composite(pair.Right, weights)
		switch {
		case left < right:
			verdicts[pair.ID] = LeftBetter
		case right < left:
			verdicts[pair.ID] = RightBetter
		default:
			verdicts[pair.ID] = Same
		}
	}
	return verdicts
}

// TuneWeights is the weights, each 0, 1, 2 or 3, whose composite
// agrees with Paul on the most tune pairs he gave a side. Of weights
// that agree as often, the nearest Even wins (by the sum of squared
// differences); then the one with fewer zero weights, since a zero
// drops a count altogether; then the larger sum; then the first in
// counting order, from all zeros up, Cognitive counted slowest. (Tune
// names the set, so this is not Tune.)
func TuneWeights(pairs []Pair, paul map[int]Verdict) Weights {
	var tuning []Pair
	for _, pair := range pairs {
		if pair.Set == Tune && paul[pair.ID].Decided() {
			tuning = append(tuning, pair)
		}
	}
	best := tuned{weights: Even, agreed: -1}
	steps := []float64{0, 1, 2, 3}
	for _, cognitive := range steps {
		for _, nesting := range steps {
			for _, overLimit := range steps {
				for _, duplicated := range steps {
					for _, farShortNames := range steps {
						weights := Weights{cognitive, nesting, overLimit,
							duplicated, farShortNames}
						agreed, _ := Agreement(tuning, paul,
							CompositeVerdicts(tuning, weights), Tune)
						candidate := tuned{weights: weights, agreed: agreed}
						if candidate.better(best) {
							best = candidate
						}
					}
				}
			}
		}
	}
	return best.weights
}

// tuned is one set of weights and how often it agrees on tune pairs.
type tuned struct {
	weights Weights
	agreed  int
}

// better is whether one beats best by TuneWeights' order; a full tie
// keeps best, the first counted.
func (one tuned) better(best tuned) bool {
	switch {
	case one.agreed != best.agreed:
		return one.agreed > best.agreed
	case fromEven(one.weights) != fromEven(best.weights):
		return fromEven(one.weights) < fromEven(best.weights)
	case zeros(one.weights) != zeros(best.weights):
		return zeros(one.weights) < zeros(best.weights)
	default:
		return sum(one.weights) > sum(best.weights)
	}
}

func (weights Weights) list() []float64 {
	return []float64{weights.Cognitive, weights.Nesting, weights.OverLimit,
		weights.Duplicated, weights.FarShortNames}
}

func fromEven(weights Weights) float64 {
	distance := 0.0
	for _, weight := range weights.list() {
		distance += (weight - 1) * (weight - 1)
	}
	return distance
}

func zeros(weights Weights) int {
	count := 0
	for _, weight := range weights.list() {
		if weight == 0 {
			count++
		}
	}
	return count
}

func sum(weights Weights) float64 {
	total := 0.0
	for _, weight := range weights.list() {
		total += weight
	}
	return total
}

// Share is agreement with one run's pairs left out.
type Share struct {
	Without         string
	Agreed, Decided int
}

func (share Share) fraction() float64 {
	return float64(share.Agreed) / float64(share.Decided)
}

// LeaveOneRunOut is how far agreement on the set moves when the pairs
// showing any one run are left out, each run in turn: the lowest and
// highest share, the first run in name order winning a tie. A run whose
// leaving out leaves no pair with a side is passed over; when every
// run is, both are zero.
func LeaveOneRunOut(
	pairs []Pair, paul, other map[int]Verdict, set string,
) (low, high Share) {
	var runs []string
	for _, pair := range pairs {
		if countsAs(pair, set) && paul[pair.ID].Decided() {
			runs = append(runs, pair.Left.Run, pair.Right.Run)
		}
	}
	slices.Sort(runs)
	found := false
	for _, run := range slices.Compact(runs) {
		kept := slices.DeleteFunc(slices.Clone(pairs), func(pair Pair) bool {
			return pair.Left.Run == run || pair.Right.Run == run
		})
		agreed, decided := Agreement(kept, paul, other, set)
		if decided == 0 {
			continue
		}
		share := Share{Without: run, Agreed: agreed, Decided: decided}
		if !found || share.fraction() < low.fraction() {
			low = share
		}
		if !found || share.fraction() > high.fraction() {
			high = share
		}
		found = true
	}
	return low, high
}

// AstraPick asks Astra which side of the pair reads better, with the
// page's instruction and the page's left and right, and nothing about
// the runs, the job or any score. exec.Dir should be an empty
// directory: Astra judges the code in the prompt, not files.
func AstraPick(
	ctx context.Context, exec codex.Exec, pair Pair,
) (Verdict, error) {
	reply, err := exec.Run(ctx, astraPrompt(pair))
	if err != nil {
		return "", fmt.Errorf("pair %d: %w", pair.ID, err)
	}
	verdict, err := verdictFrom(reply.Text)
	if err != nil {
		return "", fmt.Errorf("pair %d: %w", pair.ID, err)
	}
	return verdict, nil
}

// astraInstructions is Astra's prompt around the two excerpts: the
// page's instruction and hint, word for word, and how to answer.
const astraInstructions = Instruction + "\n\n" + NotSameJobHint + `

Judge only from the code below. Do not open files or run commands.

Left:
~~~go
%s~~~

Right:
~~~go
%s~~~

Reply with one JSON object and nothing else: {"verdict": "left"} if
the left reads better, {"verdict": "right"} if the right reads better,
{"verdict": "same"} for no difference, or {"verdict": "not-same-job"}
if they do not do the same job.
`

func astraPrompt(pair Pair) string {
	return fmt.Sprintf(astraInstructions, pair.Left.Text, pair.Right.Text)
}

// AstraPromptHash fingerprints Astra's instructions, so picks made by
// other instructions are not mixed with today's.
func AstraPromptHash() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(astraInstructions)))[:16]
}

// verdictFrom is the "verdict" of the last JSON object in the reply
// that has one, whatever prose or code fence is around it.
func verdictFrom(reply string) (Verdict, error) {
	for end := len(reply); end > 0; {
		start := strings.LastIndex(reply[:end], "{")
		if start < 0 {
			break
		}
		var object struct {
			Verdict *string `json:"verdict"`
		}
		decoder := json.NewDecoder(strings.NewReader(reply[start:]))
		if decoder.Decode(&object) == nil && object.Verdict != nil {
			verdict := Verdict(strings.TrimSpace(*object.Verdict))
			if !verdict.known() {
				return "", fmt.Errorf("verdict %q is none of the four",
					*object.Verdict)
			}
			return verdict, nil
		}
		end = start
	}
	return "", fmt.Errorf("no verdict in the reply: %q", reply)
}
