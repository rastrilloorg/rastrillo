package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/calibrate"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/codex"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/comprehend"
)

// astraPicks is results/astra-picks-<date>.json: Astra's blind verdict
// on each pair of one pairs file, and what made them, so that picks
// made by another model or instruction are never mixed in.
type astraPicks struct {
	Pairs                     string
	Seed                      int64
	Model, Effort, PromptHash string
	Picks                     []astraPick
}

type astraPick struct {
	PairID    int
	Canonical string
	Verdict   calibrate.Verdict
	At        time.Time
}

// agreeCommand is `bench agree`: have Astra pick a side of every pair,
// or every pair Paul judged, and, given his calibration, tune the
// composite's weights (or take -weights) and add an attempt to the
// report saying how often each layer agrees with him.
func agreeCommand(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("agree", flag.ContinueOnError)
	pairsPath := flags.String("pairs", "", "results/pairs-<date>.json")
	calibrationPath := flags.String("calibration", "", "results/"+
		"calibration-<date>.json; without it, Astra picks every pair "+
		"and nothing is reported")
	picksPath := flags.String("picks", "", "Astra's picks, made once "+
		"and reused (default results/astra-picks-<date>.json, the date "+
		"of -pairs)")
	jobs := flags.Int("j", 1, "Astra's picks made at once (readers share one codex login; parallel refreshes can revoke it)")
	timeout := flags.Duration("timeout", 15*time.Minute, "each pick's time")
	out := flags.String("out", "", "label for results/<label>.md "+
		"(default agreement-<date>, the date of -pairs); a revision "+
		"passes the first attempt's label, and is added to its attempts")
	fixed := flags.String("weights", "", "cognitive,nesting,overlimit,"+
		"duplicated,farshortnames: check these weights instead of tuning")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *pairsPath == "" || *jobs < 1 {
		return errors.New("usage: agree -pairs results/pairs-<date>.json " +
			"[-calibration results/calibration-<date>.json] " +
			"[-weights c,n,o,d,f] [-j 3] [-out agreement-<date>]")
	}
	var weights *calibrate.Weights
	if *fixed != "" {
		parsed, err := parseWeights(*fixed)
		if err != nil {
			return err
		}
		weights = &parsed
	}
	root, err := benchRoot()
	if err != nil {
		return err
	}
	chosen, err := loadPairsFile(*pairsPath)
	if err != nil {
		return err
	}
	if *picksPath == "" {
		*picksPath = resultPath(root, labelFor(*pairsPath, "astra-picks"),
			".json")
	}
	if *out == "" {
		*out = labelFor(*pairsPath, "agreement")
	}
	if err := checkLabel(*out); err != nil {
		return err
	}
	picks, err := loadPicks(*picksPath, astraPicks{
		Pairs: filepath.Base(*pairsPath), Seed: chosen.Seed,
		Model: comprehend.ReaderModel, Effort: comprehend.ReaderEffort,
		PromptHash: calibrate.AstraPromptHash()}, chosen.Pairs)
	if err != nil {
		return err
	}
	wanted := chosen.Pairs
	var calibration calibrate.Calibration
	if *calibrationPath != "" {
		if calibration, err = readCalibration(*calibrationPath); err != nil {
			return err
		}
		if calibration.Seed != chosen.Seed || filepath.Base(
			calibration.Pairs) != filepath.Base(*pairsPath) {
			return fmt.Errorf("%s judges %s (seed %d), not %s",
				*calibrationPath, calibration.Pairs, calibration.Seed,
				*pairsPath)
		}
		judged := verdictsOf(calibration)
		wanted = slices.DeleteFunc(slices.Clone(wanted),
			func(pair calibrate.Pair) bool {
				_, found := judged[pair.ID]
				return !found
			})
	}
	if err := pickAll(ctx, &picks, *picksPath, wanted, *jobs,
		*timeout); err != nil {
		return err
	}
	astra := map[int]calibrate.Verdict{}
	for _, pick := range picks.Picks {
		astra[pick.PairID] = pick.Verdict
	}
	if *calibrationPath == "" {
		fmt.Printf("Astra picked %d pairs, saved in %s:\n", len(astra),
			*picksPath)
		fmt.Print(judgedTally(chosen.Pairs, astra))
		return nil
	}
	report := agreementReport{PairsPath: *pairsPath,
		CalibrationPath: *calibrationPath, PicksPath: *picksPath,
		Seed: chosen.Seed, Model: picks.Model, Effort: picks.Effort,
		PromptHash: picks.PromptHash, Pairs: chosen.Pairs,
		Paul: verdictsOf(calibration), Astra: astra, Fixed: weights}
	path := resultPath(root, *out, ".md")
	if err := writeAgreement(path, *out, report); err != nil {
		return err
	}
	fmt.Println("wrote", path)
	return nil
}

// loadPicks reads the picks already made, or starts none when there
// are none. Picks made for another pairs file or seed, by another
// model or effort, or with other instructions are refused, as is a
// pick whose pair is not the one in the pairs file under its ID.
func loadPicks(
	path string, want astraPicks, pairs []calibrate.Pair,
) (astraPicks, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return want, nil
	}
	if err != nil {
		return want, err
	}
	var made astraPicks
	if err := json.Unmarshal(raw, &made); err != nil {
		return want, fmt.Errorf("%s: %w", path, err)
	}
	if made.Pairs != want.Pairs || made.Seed != want.Seed ||
		made.Model != want.Model || made.Effort != want.Effort ||
		made.PromptHash != want.PromptHash {
		return want, fmt.Errorf("%s was picked for other pairs, by another "+
			"model or with other instructions; delete it to pick again",
			path)
	}
	canonicals := map[int]string{}
	for _, pair := range pairs {
		canonicals[pair.ID] = pair.Canonical
	}
	for _, pick := range made.Picks {
		if canonicals[pick.PairID] != pick.Canonical {
			return want, fmt.Errorf("%s picked pair %d as %q, which %s "+
				"does not have; delete it to pick again", path, pick.PairID,
				pick.Canonical, want.Pairs)
		}
	}
	return made, nil
}

func savePicks(path string, picks astraPicks) error {
	slices.SortFunc(picks.Picks, func(a, b astraPick) int {
		return cmp.Compare(a.PairID, b.PairID)
	})
	encoded, err := json.MarshalIndent(picks, "", "  ")
	if err != nil {
		return err
	}
	return replaceFile(path, append(encoded, '\n'))
}

// pickAll has Astra pick each wanted pair it has not picked, jobs at
// once, saving each pick as it comes. Astra reads nothing but its
// prompt: its directory is an empty scratch one, removed after.
func pickAll(
	ctx context.Context, picks *astraPicks, path string,
	wanted []calibrate.Pair, jobs int, timeout time.Duration,
) error {
	var missing []calibrate.Pair
	for _, pair := range wanted {
		if !slices.ContainsFunc(picks.Picks, func(pick astraPick) bool {
			return pick.PairID == pair.ID
		}) {
			missing = append(missing, pair)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	scratch, err := os.MkdirTemp("", "astra-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	reader := codex.Exec{Model: picks.Model, Effort: picks.Effort,
		Dir: scratch, Timeout: timeout}
	var lock sync.Mutex
	var failed []error
	eachAtOnce(ctx, len(missing), jobs, func(index int) {
		pair := missing[index]
		verdict, err := calibrate.AstraPick(ctx, reader, pair)
		lock.Lock()
		defer lock.Unlock()
		if err == nil {
			picks.Picks = append(picks.Picks, astraPick{PairID: pair.ID,
				Canonical: pair.Canonical, Verdict: verdict,
				At: time.Now().UTC()})
			err = savePicks(path, *picks)
		}
		if err != nil {
			failed = append(failed, err)
			return
		}
		fmt.Fprintf(os.Stderr, "astra pair %d: %s\n", pair.ID, verdict)
	})
	if ctx.Err() != nil {
		return errors.New("agree: interrupted; the picks made are saved, " +
			"and running again makes only the rest")
	}
	return errors.Join(failed...)
}

// labelFor is a result's label from the pairs file's: kind and the
// date (or whatever follows "pairs-"), so that a calibration resumed
// the next day keeps its file and its order.
func labelFor(pairsPath, kind string) string {
	name := strings.TrimSuffix(filepath.Base(pairsPath), ".json")
	return kind + "-" + strings.TrimPrefix(name, "pairs-")
}

// parseWeights reads -weights: five weights, none negative.
func parseWeights(text string) (calibrate.Weights, error) {
	fields := strings.Split(text, ",")
	if len(fields) != 5 {
		return calibrate.Weights{}, fmt.Errorf("-weights %q: give five, "+
			"cognitive,nesting,overlimit,duplicated,farshortnames", text)
	}
	var values [5]float64
	for index, field := range fields {
		value, err := strconv.ParseFloat(strings.TrimSpace(field), 64)
		if err != nil || value < 0 {
			return calibrate.Weights{}, fmt.Errorf("-weights %q: %q is "+
				"not a weight of 0 or more", text, field)
		}
		values[index] = value
	}
	return calibrate.Weights{Cognitive: values[0], Nesting: values[1],
		OverLimit: values[2], Duplicated: values[3],
		FarShortNames: values[4]}, nil
}

// agreementReport is one attempt in results/agreement-<date>.md: the
// pairs, both judges' verdicts, and the weights to check, when they
// are fixed rather than tuned.
type agreementReport struct {
	PairsPath, CalibrationPath, PicksPath string
	Seed                                  int64
	Model, Effort, PromptHash             string
	Pairs                                 []calibrate.Pair
	Paul, Astra                           map[int]calibrate.Verdict
	Fixed                                 *calibrate.Weights
}

// attemptHeading starts each attempt in the report; attemptMark, on
// the line after it, names the attempt so that writing it again
// replaces it in its place.
const (
	attemptHeading = "\n## Attempt "
	attemptMark    = "<!-- attempt %s -->"
)

// key is what tells one attempt from another: its pairs, its verdicts
// and its weights. Fixed weights are named by their values, so checking
// other weights on the same pairs is another attempt, kept beside the
// first, never one written over it. Tuned weights follow from the pairs
// and verdicts.
func (report agreementReport) key() string {
	return report.PairsPath + " " + report.CalibrationPath + " " +
		report.weightsName()
}

func (report agreementReport) weightsName() string {
	if weights := report.Fixed; weights != nil {
		return fmt.Sprintf("fixed weights %g,%g,%g,%g,%g", weights.Cognitive,
			weights.Nesting, weights.OverLimit, weights.Duplicated,
			weights.FarShortNames)
	}
	return "tuned weights"
}

// writeAgreement adds the attempt to the report at path, or replaces
// the attempt with its key, keeping every other attempt and what a
// person wrote after the "## Checkpoint" heading.
func writeAgreement(path, label string, report agreementReport) error {
	kept := judgement(path)
	var attempts []string
	if raw, err := os.ReadFile(path); err == nil {
		text, _, _ := strings.Cut(string(raw), judgementHeading)
		chunks := strings.Split(text, attemptHeading)
		for _, chunk := range chunks[1:] {
			_, body, _ := strings.Cut(chunk, "\n")
			attempts = append(attempts, strings.Trim(body, "\n"))
		}
	}
	mark := fmt.Sprintf(attemptMark, report.key())
	body := mark + "\n\n" + attemptText(report)
	replaced := false
	for index, attempt := range attempts {
		if strings.HasPrefix(attempt, mark+"\n") {
			attempts[index], replaced = body, true
		}
	}
	if !replaced {
		attempts = append(attempts, body)
	}

	var text strings.Builder
	fmt.Fprintf(&text, "# %s\n\n", label)
	text.WriteString("A layer is trusted when it picks Paul's side on at " +
		"least 80% of the check pairs he gave a side, and at least 12 " +
		"check pairs have one. Spares count as check pairs. \"No " +
		"difference\" or \"not the same job\" against Paul's side is a " +
		"disagreement; pairs Paul gave no side are not counted. A layer " +
		"under 80% is revised once and checked on new pairs: each " +
		"attempt is listed below.\n")
	for index, attempt := range attempts {
		header, _, _ := strings.Cut(strings.TrimPrefix(attempt,
			"<!-- attempt "), " -->")
		fields := strings.Fields(header)
		name := strings.Join(fields[2:], " ")
		fmt.Fprintf(&text, "%s%d: %s, %s\n\n%s\n", attemptHeading,
			index+1, fields[0], name, strings.TrimRight(attempt, "\n"))
	}
	return replaceFile(path, []byte(text.String()+kept))
}

// trustedAgreed over trustedDecided is the least agreement a trusted
// layer has: 80%.
const trustedAgreed, trustedDecided = 4, 5

// attemptText is one attempt's findings.
func attemptText(report agreementReport) string {
	var text strings.Builder
	weights := calibrate.TuneWeights(report.Pairs, report.Paul)
	if report.Fixed != nil {
		weights = *report.Fixed
	}
	_, decided := calibrate.Agreement(report.Pairs, report.Paul,
		report.Paul, calibrate.Check)

	fmt.Fprintf(&text, "Pairs: `%s`, seed %d. Paul's verdicts: `%s`. "+
		"Astra's picks: `%s` (%s, %s effort, instructions %s), made "+
		"blind, before or without Paul's verdicts.\n\n", report.PairsPath,
		report.Seed, report.CalibrationPath, report.PicksPath, report.Model,
		report.Effort, report.PromptHash)
	if decided < calibrate.DecidedChecksNeeded {
		fmt.Fprintf(&text, "**No layer can be trusted: only %d check pairs "+
			"have a side, of %d needed.**\n\n", decided,
			calibrate.DecidedChecksNeeded)
	} else {
		fmt.Fprintf(&text, "%d check pairs have a side.\n\n", decided)
	}

	text.WriteString("### Paul's verdicts\n\n")
	text.WriteString(judgedTally(report.Pairs, report.Paul))

	layerName := "Composite, tuned weights"
	tuneAgreed, tuneDecided := calibrate.Agreement(report.Pairs, report.Paul,
		calibrate.CompositeVerdicts(report.Pairs, weights), calibrate.Tune)
	evenAgreed, _ := calibrate.Agreement(report.Pairs, report.Paul,
		calibrate.CompositeVerdicts(report.Pairs, calibrate.Even),
		calibrate.Tune)
	if report.Fixed != nil {
		layerName = "Composite, fixed weights"
		fmt.Fprintf(&text, "\n### Fixed weights\n\n%s, given with "+
			"-weights, not tuned. On the %d tune pairs Paul gave a side, "+
			"they agree on %d; even weights on %d.\n\n",
			weightsText(weights), tuneDecided, tuneAgreed, evenAgreed)
	} else {
		fmt.Fprintf(&text, "\n### Tuned weights\n\n%s. On the %d tune "+
			"pairs Paul gave a side, they agree on %d; even weights on %d. "+
			"Each weight is 0, 1, 2 or 3; of weights that agree as often, "+
			"the nearest even wins, then the one with fewer zeros, then "+
			"the larger sum.\n\n", weightsText(weights), tuneDecided,
			tuneAgreed, evenAgreed)
	}

	text.WriteString("### Check pairs\n\n")
	text.WriteString("| Layer | Agreed | Share | Trusted | " +
		"Each run left out |\n|---|---|---|---|---|\n")
	for _, layer := range []struct {
		name     string
		verdicts map[int]calibrate.Verdict
		judged   bool
	}{
		{layerName, calibrate.CompositeVerdicts(report.Pairs, weights), true},
		{"Astra", report.Astra, true},
		{"Composite, even weights (for comparison)",
			calibrate.CompositeVerdicts(report.Pairs, calibrate.Even), false},
	} {
		agreed, decided := calibrate.Agreement(report.Pairs, report.Paul,
			layer.verdicts, calibrate.Check)
		trusted := "no"
		switch {
		case !layer.judged:
			trusted = "-"
		case decided >= calibrate.DecidedChecksNeeded &&
			agreed*trustedDecided >= decided*trustedAgreed:
			trusted = "yes"
		}
		low, high := calibrate.LeaveOneRunOut(report.Pairs, report.Paul,
			layer.verdicts, calibrate.Check)
		fmt.Fprintf(&text, "| %s | %d of %d | %s | %s | %s to %s |\n",
			layer.name, agreed, decided, percent(agreed, decided), trusted,
			shareText(low), shareText(high))
	}
	text.WriteString("\n\"Each run left out\" is the lowest and highest " +
		"agreement with the pairs showing any one run left out, each run " +
		"in turn.\n")

	text.WriteString("\n### Each judged pair\n\n")
	text.WriteString("| Pair | Set | Left | Right | Paul | Composite | " +
		"Astra |\n|---|---|---|---|---|---|---|\n")
	composite := calibrate.CompositeVerdicts(report.Pairs, weights)
	for _, pair := range report.Pairs {
		paul, judged := report.Paul[pair.ID]
		if !judged {
			continue
		}
		fmt.Fprintf(&text, "| %d | %s | %s | %s | %s | %s | %s |\n",
			pair.ID, pair.Set, pair.Left.Run, pair.Right.Run, paul,
			verdictText(composite[pair.ID]),
			verdictText(report.Astra[pair.ID]))
	}
	return text.String()
}

func weightsText(weights calibrate.Weights) string {
	return fmt.Sprintf("Cognitive %g, Nesting %g, Over limit %g, "+
		"Duplicated %g, Far short names %g", weights.Cognitive,
		weights.Nesting, weights.OverLimit, weights.Duplicated,
		weights.FarShortNames)
}

func percent(agreed, decided int) string {
	if decided == 0 {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", 100*float64(agreed)/float64(decided))
}

func shareText(share calibrate.Share) string {
	if share.Decided == 0 {
		return "-"
	}
	return fmt.Sprintf("%s (%d of %d, without %s)",
		percent(share.Agreed, share.Decided), share.Agreed, share.Decided,
		share.Without)
}

func verdictText(verdict calibrate.Verdict) string {
	if verdict == "" {
		return "-"
	}
	return string(verdict)
}

// judgedTally is a table of the verdicts on each set's pairs.
func judgedTally(
	pairs []calibrate.Pair, verdicts map[int]calibrate.Verdict,
) string {
	var text strings.Builder
	text.WriteString("| Set | Left | Right | No difference | " +
		"Not the same job | Not judged |\n|---|---|---|---|---|---|\n")
	for _, set := range []string{calibrate.Tune, calibrate.Check,
		calibrate.Spare} {
		tally := map[calibrate.Verdict]int{}
		for _, pair := range pairs {
			if pair.Set == set {
				tally[verdicts[pair.ID]]++
			}
		}
		fmt.Fprintf(&text, "| %s | %d | %d | %d | %d | %d |\n", set,
			tally[calibrate.LeftBetter], tally[calibrate.RightBetter],
			tally[calibrate.Same], tally[calibrate.NotSameJob], tally[""])
	}
	return text.String()
}
