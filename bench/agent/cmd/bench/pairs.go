package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/brief"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/calibrate"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/comprehend"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/counts"
)

// jobFile is jobs.json: the functions that do each of a brief's jobs in
// one app, and what found them. Jobs found for other jobs, by another
// prompt or in other source are not today's.
type jobFile struct {
	Round, Run, Brief, Model         string
	JobsHash, PromptHash, SourceHash string
	CostUSD                          float64
	Made                             time.Time
	Jobs                             map[string][]string
}

// pairsFile is results/pairs-<date>.json.
type pairsFile struct {
	Made           time.Time
	Seed           int64
	Rounds, Briefs []string
	Arms           []brief.Arm
	Across         bool
	Judged         []string `json:",omitempty"`
	Runs           []runJobs
	Coverage       calibrate.Coverage `json:",omitempty"`
	Pairs          []calibrate.Pair
}

// runJobs is what one run's jobs came to: of the functions the job
// finder named, how many the agent wrote, the jobs with none of its
// code, and the lines of each job too long to show.
type runJobs struct {
	Run             string
	Arm             brief.Arm
	Jobs, Shown     int
	Named, Authored int
	Missing         []string       `json:",omitempty"`
	Oversized       map[string]int `json:",omitempty"`
	JobsCostUSD     float64
}

// pairedRun is one kept run to cut into excerpts.
type pairedRun struct {
	round, brief, name, dir string
	arm                     brief.Arm
	excerpts                []calibrate.Excerpt
	summary                 runJobs
	err                     error
}

// pairsCommand is `bench pairs`: cut each kept run's code into one
// excerpt per job and choose blind pairs of them for calibration, or,
// with -across, pair two arms job by job.
func pairsCommand(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("pairs", flag.ContinueOnError)
	rounds := flags.String("rounds", "", "comma-separated round labels")
	briefs := flags.String("briefs", "", "comma-separated brief names")
	arms := flags.String("arms", rastrilloArms, "comma-separated arms to pair")
	seed := flags.Int64("seed", 0, "splits the runs and shuffles the pairs")
	judged := flags.String("judged", "", "comma-separated calibration "+
		"files or globs whose pairs are never chosen again; they must "+
		"record the same -seed")
	across := flags.String("across", "", "baseline,other: pair two arms "+
		"job by job instead")
	tokenFile := flags.String("token-file", defaultTokenFile(),
		"the token from claude setup-token, "+
			"if CLAUDE_CODE_OAUTH_TOKEN is not set")
	timeout := flags.Duration("timeout", 30*time.Minute,
		"the job finder's time per app")
	jobs := flags.Int("j", 1, "apps whose jobs are found at once")
	out := flags.String("out", "pairs-"+time.Now().Format("2006-01-02"),
		"label for results/<label>.json")
	if err := flags.Parse(args); err != nil {
		return err
	}
	seeded := false
	flags.Visit(func(set *flag.Flag) { seeded = seeded || set.Name == "seed" })
	if *rounds == "" || *briefs == "" || !seeded || *jobs < 1 {
		return errors.New("usage: pairs -rounds a,b -briefs x,y -seed <n> " +
			"[-arms carlos,carlos-gopls] [-judged results/calibration-*.json] " +
			"[-across baseline,other] [-j 1] [-out <label>]")
	}
	armList := strings.Split(*arms, ",")
	if *across != "" {
		armList = strings.Split(*across, ",")
		if len(armList) != 2 || *judged != "" {
			return errors.New("-across takes two arms, and no -judged")
		}
	}
	if err := checkLabel(*out); err != nil {
		return err
	}
	root, err := benchRoot()
	if err != nil {
		return err
	}
	path := resultPath(root, *out, ".json")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s exists: a calibration may name its pairs; "+
			"pass another -out", path)
	}
	var skip []string
	if *judged != "" {
		if skip, err = judgedPairs(*judged, *seed); err != nil {
			return err
		}
	}
	cache, err := runsRoot()
	if err != nil {
		return err
	}
	authToken, err := token(*tokenFile)
	if err != nil {
		return err
	}
	runs, err := pairedRuns(root, cache, strings.Split(*rounds, ","),
		strings.Split(*briefs, ","), armList)
	if err != nil {
		return err
	}
	finder := jobFinder{root: root, cache: cache, token: authToken,
		timeout: *timeout}
	eachAtOnce(ctx, len(runs), *jobs, func(index int) {
		run := runs[index]
		run.excerpts, run.summary, run.err = finder.excerpts(ctx, run)
		fmt.Fprintln(os.Stderr, "jobs", run.round, run.name, jobsTally(run))
	})
	if ctx.Err() != nil {
		return errors.New("pairs: interrupted; the jobs found are saved, " +
			"and running again finds only the rest")
	}
	var failed []error
	var excerpts []calibrate.Excerpt
	result := pairsFile{Made: time.Now().UTC(), Seed: *seed,
		Rounds: strings.Split(*rounds, ","),
		Briefs: strings.Split(*briefs, ","), Judged: skip}
	for _, arm := range armList {
		result.Arms = append(result.Arms, brief.Arm(arm))
	}
	for _, run := range runs {
		if run.err != nil {
			failed = append(failed,
				fmt.Errorf("%s/%s: %w", run.round, run.name, run.err))
			continue
		}
		excerpts = append(excerpts, run.excerpts...)
		result.Runs = append(result.Runs, run.summary)
	}
	if len(failed) > 0 {
		return errors.Join(failed...)
	}
	if *across != "" {
		result.Across = true
		result.Pairs, result.Coverage = calibrate.ChooseAcross(excerpts,
			result.Arms[0], result.Arms[1], *seed)
	} else {
		result.Pairs = calibrate.Choose(excerpts, *seed, skip)
	}
	fmt.Fprint(os.Stderr, pairsSpread(result.Pairs))
	if !result.Across {
		if err := checkSets(result.Pairs); err != nil {
			return err
		}
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return replaceFile(path, append(encoded, '\n'))
}

// pairedRuns lists the kept runs of each round, brief and arm.
func pairedRuns(
	root, cache string, rounds, briefs, arms []string,
) ([]*pairedRun, error) {
	var runs []*pairedRun
	for _, label := range rounds {
		for _, briefName := range briefs {
			for _, arm := range arms {
				kept, err := keptRuns(root, cache, label,
					[]string{briefName}, []string{arm})
				if err != nil {
					return nil, err
				}
				for _, run := range kept {
					runs = append(runs, &pairedRun{round: label,
						brief: briefName, name: run.name, arm: brief.Arm(arm),
						dir: run.dir})
				}
			}
		}
	}
	return runs, nil
}

type jobFinder struct {
	root, cache, token string
	timeout            time.Duration
}

// excerpts cuts one excerpt per job of the run's brief out of the code
// its agent wrote: empty when the agent wrote none of it.
func (f jobFinder) excerpts(
	ctx context.Context, run *pairedRun,
) ([]calibrate.Excerpt, runJobs, error) {
	summary := runJobs{Run: run.round + "/" + run.name, Arm: run.arm}
	briefDir := filepath.Join(f.root, "briefs", run.brief)
	jobs, err := calibrate.LoadJobs(briefDir)
	if err != nil {
		return nil, summary, err
	}
	measured, err := measureRun(run.dir)
	if err != nil {
		return nil, summary, err
	}
	found, cost, err := f.find(ctx, run, measured, jobs)
	if err != nil {
		return nil, summary, err
	}
	appFuncs := slices.DeleteFunc(slices.Clone(measured.counts.Funcs),
		func(one counts.FuncCounts) bool { return one.Test })
	summary.Jobs, summary.JobsCostUSD = len(jobs), cost
	var excerpts []calibrate.Excerpt
	for _, job := range jobs {
		excerpt, fits := calibrate.Cut(measured.app, found[job], appFuncs)
		excerpt.Run, excerpt.Job, excerpt.Arm = summary.Run, job, run.arm
		excerpts = append(excerpts, excerpt)
		summary.Named += len(found[job])
		summary.Authored += authoredOf(found[job], appFuncs)
		switch {
		case len(excerpt.Funcs) == 0:
			summary.Missing = append(summary.Missing, job)
		case !fits:
			if summary.Oversized == nil {
				summary.Oversized = map[string]int{}
			}
			summary.Oversized[job] = excerpt.Lines()
		default:
			summary.Shown++
		}
	}
	return excerpts, summary, nil
}

func authoredOf(names []string, funcs []counts.FuncCounts) int {
	authored := 0
	for _, name := range names {
		if slices.ContainsFunc(funcs, func(one counts.FuncCounts) bool {
			return one.Path+":"+one.Key == name
		}) {
			authored++
		}
	}
	return authored
}

func jobsPath(cache, round, run string) string {
	return filepath.Join(cache, "scoring", round, run, "jobs.json")
}

// find has the job finder name each job's functions, or reads what it
// named before: finding costs a Claude session, so it is done once per
// app, and again only when the jobs, the prompt, the model or the app
// change. The cost is what the finding cost, whenever it was made.
func (f jobFinder) find(
	ctx context.Context, run *pairedRun, measured measuredRun,
	jobs []string,
) (map[string][]string, float64, error) {
	jobsHash, err := calibrate.JobsHash(filepath.Join(f.root, "briefs",
		run.brief))
	if err != nil {
		return nil, 0, err
	}
	sourceHash, err := comprehend.SourceHash(measured.app)
	if err != nil {
		return nil, 0, err
	}
	want := jobFile{Round: run.round, Run: run.name, Brief: run.brief,
		Model: comprehend.KeyModel, JobsHash: jobsHash,
		PromptHash: calibrate.JobsPromptHash(), SourceHash: sourceHash}
	path := jobsPath(f.cache, run.round, run.name)
	if raw, err := os.ReadFile(path); err == nil {
		made, err := jobsMadeBefore(raw, want)
		return made.Jobs, made.CostUSD, err
	}
	rastrillo, err := moduleSource(measured.version)
	if err != nil {
		return nil, 0, err
	}
	scratch, err := os.MkdirTemp("", "jobs-")
	if err != nil {
		return nil, 0, err
	}
	defer os.RemoveAll(scratch)
	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()
	found, err := calibrate.FindJobs(ctx, comprehend.KeySetup{
		AppDir: measured.app, RastrilloDir: rastrillo, Token: f.token,
		BenchRoot: f.root, Scratch: scratch,
	}, jobs)
	if keepErr := keepTranscript(scratch, filepath.Dir(path),
		"jobfinder.jsonl"); keepErr != nil {
		return nil, 0, errors.Join(err, keepErr)
	}
	if err != nil {
		return nil, 0, err
	}
	if session, err := readTranscript(filepath.Join(scratch,
		"jobfinder.jsonl")); err == nil && session.Result != nil {
		want.CostUSD = session.Result.CostUSD
	}
	want.Made, want.Jobs = time.Now().UTC(), found
	encoded, err := json.MarshalIndent(want, "", "  ")
	if err != nil {
		return nil, 0, err
	}
	return found, want.CostUSD, replaceFile(path, append(encoded, '\n'))
}

func jobsMadeBefore(raw []byte, want jobFile) (jobFile, error) {
	var made jobFile
	if err := json.Unmarshal(raw, &made); err != nil {
		return made, fmt.Errorf("jobs.json: %w", err)
	}
	if made.JobsHash != want.JobsHash || made.PromptHash != want.PromptHash ||
		made.SourceHash != want.SourceHash || made.Model != want.Model {
		return jobFile{}, errors.New("jobs.json was made for other jobs, " +
			"by another job-finder prompt or model, or from other source; " +
			"delete it to find the jobs again")
	}
	return made, nil
}

// setSizes are how many pairs of each set Choose must make.
var setSizes = map[string]int{calibrate.Tune: 15, calibrate.Check: 15,
	calibrate.Spare: 10}

// checkSets fails when a set has fewer pairs than calibration needs:
// too few jobs could be shown, or too many pairs were judged already.
func checkSets(pairs []calibrate.Pair) error {
	counted := map[string]int{}
	for _, pair := range pairs {
		counted[pair.Set]++
	}
	var short []string
	for _, set := range []string{calibrate.Tune, calibrate.Check,
		calibrate.Spare} {
		if counted[set] < setSizes[set] {
			short = append(short, fmt.Sprintf("%d %s pairs of %d", counted[set],
				set, setSizes[set]))
		}
	}
	if len(short) > 0 {
		return fmt.Errorf("too few pairs to calibrate (%s): too few jobs "+
			"fit in %d lines, or too many pairs were judged already; "+
			"nothing was written", strings.Join(short, ", "),
			calibrate.MaxLines)
	}
	return nil
}

// judgedPairs is every Canonical in the files spec names, comma
// separated, each a path or a glob. A file naming none is refused: it
// is not a calibration, and passing it would leave judged pairs open.
// So is one whose Seed is not seed, or that records none: the seed
// splits the runs, and another would check on runs tuned with.
func judgedPairs(spec string, seed int64) ([]string, error) {
	var judged []string
	for _, pattern := range strings.Split(spec, ",") {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		if len(paths) == 0 {
			return nil, fmt.Errorf("-judged %s: no such file", pattern)
		}
		for _, path := range paths {
			raw, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			var document any
			if err := json.Unmarshal(raw, &document); err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			found := valuesIn(document, "Canonical")
			if len(found) == 0 {
				return nil, fmt.Errorf("%s names no pair (no Canonical)", path)
			}
			seeds := valuesIn(document, "Seed")
			if len(seeds) == 0 || slices.ContainsFunc(seeds, func(
				recorded any) bool {
				return recorded != float64(seed)
			}) {
				return nil, fmt.Errorf("%s records seed %v, not -seed %d: "+
					"the seed splits runs into tune and check, so new pairs "+
					"must use the judged pairs' seed", path, seeds, seed)
			}
			for _, canonical := range found {
				if text, isText := canonical.(string); isText {
					judged = append(judged, text)
				}
			}
		}
	}
	return judged, nil
}

// valuesIn is every value under name anywhere in a decoded JSON
// document.
func valuesIn(document any, name string) []any {
	var found []any
	switch typed := document.(type) {
	case map[string]any:
		for key, value := range typed {
			if key == name {
				found = append(found, value)
			} else {
				found = append(found, valuesIn(value, name)...)
			}
		}
	case []any:
		for _, value := range typed {
			found = append(found, valuesIn(value, name)...)
		}
	}
	return found
}

func jobsTally(run *pairedRun) string {
	if run.err != nil {
		return "failed: " + run.err.Error()
	}
	summary := run.summary
	return fmt.Sprintf("%d of %d jobs shown, %d missing %v, %d too long "+
		"%v; %d of %d named functions authored; $%.2f", summary.Shown,
		summary.Jobs, len(summary.Missing), summary.Missing,
		len(summary.Oversized), summary.Oversized, summary.Authored,
		summary.Named, summary.JobsCostUSD)
}

// pairsSpread says how many pairs of each set there are, and how the
// tune and check pairs fall over fifths of their composite difference.
func pairsSpread(pairs []calibrate.Pair) string {
	var text strings.Builder
	sets := map[string][]float64{}
	var calibrating []float64
	for _, pair := range pairs {
		difference := math.Abs(calibrate.Composite(pair.Left, calibrate.Even) -
			calibrate.Composite(pair.Right, calibrate.Even))
		sets[pair.Set] = append(sets[pair.Set], difference)
		if pair.Set == calibrate.Tune || pair.Set == calibrate.Check {
			calibrating = append(calibrating, difference)
		}
	}
	for _, set := range slices.Sorted(maps.Keys(sets)) {
		differences := slices.Sorted(slices.Values(sets[set]))
		fmt.Fprintf(&text, "%s: %d pairs, differences %v\n", set,
			len(differences), differences)
	}
	if len(calibrating) > 0 {
		low, high := slices.Min(calibrating), slices.Max(calibrating)
		var fifths [5]int
		for _, value := range calibrating {
			fifth := 0
			if high > low {
				fifth = min(4, int(5*(value-low)/(high-low)))
			}
			fifths[fifth]++
		}
		fmt.Fprintf(&text, "tune and check over fifths of %v..%v: %v\n",
			low, high, fifths)
	}
	return text.String()
}
