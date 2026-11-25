package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/authored"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/counts"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/round"
)

const rastrilloModule = "amadan.net/rastrillo/rastrillo"

// scoreRow is one run's counts, or why it has none. App and Tests are
// spreads over functions; AppCode and TestCode are whole totals, apart.
type scoreRow struct {
	Round, Run, Version string
	Unscorable          string                   `json:",omitempty"`
	App, Tests          map[string]counts.Spread `json:",omitempty"`
	AppCode, TestCode   counts.Totals
	// Interfaces is nil when the app does not type-check.
	Interfaces *int
	Funcs      []counts.FuncCounts `json:",omitempty"`
}

// scoreCommand is `bench score`: count the code agents wrote in the
// runs kept under ~/.cache/agent-bench, which it only reads.
func scoreCommand(args []string) error {
	flags := flag.NewFlagSet("score", flag.ContinueOnError)
	rounds := flags.String("rounds", "", "comma-separated round labels")
	briefs := flags.String("briefs", "", "comma-separated brief names")
	arms := flags.String("arms", rastrilloArms, "comma-separated arms to score")
	out := flags.String("out", "", "label for results/<label>.json and .md")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *rounds == "" || *briefs == "" || *out == "" {
		return errors.New("usage: score -rounds a,b -briefs x,y " +
			"[-arms carlos,carlos-gopls] -out <label>")
	}
	if err := checkLabel(*out); err != nil {
		return err
	}
	root, err := benchRoot()
	if err != nil {
		return err
	}
	runs, err := runsRoot()
	if err != nil {
		return err
	}
	var rows []scoreRow
	for _, label := range strings.Split(*rounds, ",") {
		kept, err := keptRuns(root, runs, label,
			strings.Split(*briefs, ","), strings.Split(*arms, ","))
		if err != nil {
			return err
		}
		for _, run := range kept {
			row := scoreRun(label, run.dir)
			row.Run = run.name
			rows = append(rows, row)
			fmt.Fprintln(os.Stderr, "scored", label, run.name, row.Unscorable)
		}
	}
	return saveScores(root, *out, rows)
}

// rastrilloArms is the default -arms of the commands that read kept
// runs: both arms that build on Rastrillo, so a round with carlos-gopls
// is never read as carlos alone. A round without one finds no runs of it.
const rastrilloArms = "carlos,carlos-gopls"

// keptRun is a run its round accepted, and the directory it is kept in.
type keptRun struct{ name, dir string }

// keptRuns lists the runs of briefs and arms that round label's records,
// results/<label>.json, accepted, sorted by name. A directory under
// cache/<label> that no record accepted (a failed last attempt, a build
// still going) is skipped, and named on stderr.
func keptRuns(
	root, cache, label string, briefs, arms []string,
) ([]keptRun, error) {
	r, err := loadRound(root, label)
	if err != nil {
		return nil, fmt.Errorf("round %s's records: %w", label, err)
	}
	kept, skipped, err := acceptedRuns(r, cache, briefs, arms)
	for _, name := range skipped {
		fmt.Fprintf(os.Stderr, "skipped %s/%s: no record in "+
			"results/%s.json accepted it\n", label, name, label)
	}
	return kept, err
}

// acceptedRuns is keptRuns on a round already read: its runs of briefs
// and arms, each where it was made (a reused run, in the round it came
// from), and the directories of those briefs and arms it has no record
// of. A name with -attempt- in it is a discarded try, never listed.
func acceptedRuns(
	r round.Round, cache string, briefs, arms []string,
) (kept []keptRun, skipped []string, err error) {
	accepted := map[string]bool{}
	for _, run := range r.Runs {
		name := fmt.Sprintf("%s-%s-%d", run.Brief, run.Arm, run.N)
		if !slices.Contains(briefs, run.Brief) ||
			!slices.Contains(arms, string(run.Arm)) {
			continue
		}
		madeIn := r.Label
		if run.ReusedFrom != "" {
			madeIn = run.ReusedFrom
		}
		accepted[name] = true
		kept = append(kept, keptRun{name, filepath.Join(cache, madeIn, name)})
	}
	slices.SortFunc(kept, func(a, b keptRun) int {
		return strings.Compare(a.name, b.name)
	})
	entries, err := os.ReadDir(filepath.Join(cache, r.Label))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() && !accepted[entry.Name()] &&
			runOf(entry.Name(), briefs, arms) {
			skipped = append(skipped, entry.Name())
		}
	}
	return kept, skipped, nil
}

// runOf is whether name is a run directory, <brief>-<arm>-<n>, of one
// of briefs and arms.
func runOf(name string, briefs, arms []string) bool {
	for _, brief := range briefs {
		for _, arm := range arms {
			pattern := "^" + regexp.QuoteMeta(brief+"-"+arm) + `-\d+$`
			if regexp.MustCompile(pattern).MatchString(name) {
				return true
			}
		}
	}
	return false
}

// scoreRun counts one run. Whatever goes wrong becomes the row's
// reason: one run that cannot be scored must not stop the others.
func scoreRun(round, dir string) scoreRow {
	row := scoreRow{Round: round}
	if err := fillRow(&row, dir); err != nil {
		row.Unscorable = strings.TrimSpace(err.Error())
	}
	return row
}

func fillRow(row *scoreRow, dir string) error {
	run, err := measureRun(dir)
	if err != nil {
		return err
	}
	measured := run.counts
	row.Version = run.version
	row.App = counts.Summarise(measured)
	row.Tests = counts.SummariseTests(measured)
	row.AppCode, row.TestCode = measured.App, measured.Tests
	row.Interfaces, row.Funcs = measured.SingleImplementationInterfaces,
		measured.Funcs
	return nil
}

// measuredRun is one kept run's authored code, counted, and the version
// of Rastrillo it uses.
type measuredRun struct {
	app, version string
	code         authored.Code
	counts       counts.RunCounts
}

// measureRun finds the authored code of the run in dir, by making its
// scaffold again, and counts it.
func measureRun(dir string) (measuredRun, error) {
	run := measuredRun{app: filepath.Join(dir, "app")}
	module, err := authored.RunModule(run.app)
	if err != nil {
		return run, err
	}
	build, err := readTranscript(filepath.Join(dir, "rec", "build.jsonl"))
	if err != nil {
		return run, err
	}
	scaffold, err := authored.ScaffoldOf(build, module)
	if err != nil {
		return run, err
	}
	run.version, err = rastrilloVersion(run.app)
	if err != nil {
		return run, err
	}
	source, err := moduleSource(run.version)
	if err != nil {
		return run, err
	}
	run.code, err = rescaffolded(source, scaffold, run.app)
	if err != nil {
		return run, err
	}
	run.counts, err = counts.Measure(run.app, run.code)
	return run, err
}

// rescaffolded makes the app's scaffold again in a scratch directory,
// compares, and removes the scratch.
func rescaffolded(
	source string, scaffold authored.Scaffold, app string,
) (authored.Code, error) {
	scratch, err := os.MkdirTemp("", "score-")
	if err != nil {
		return authored.Code{}, err
	}
	defer os.RemoveAll(scratch)
	if err := authored.Rescaffold(source, scaffold, scratch); err != nil {
		return authored.Code{}, err
	}
	return authored.Authored(app, filepath.Join(scratch, scaffold.Name))
}

// rastrilloVersion is the version the app's go.mod requires of Rastrillo.
func rastrilloVersion(app string) (string, error) {
	text, err := os.ReadFile(filepath.Join(app, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(text), "\n") {
		fields := strings.Fields(line)
		if fields = withoutRequire(fields); len(fields) >= 2 &&
			fields[0] == rastrilloModule {
			return fields[1], nil
		}
	}
	return "", errors.New("go.mod does not require " + rastrilloModule)
}

// moduleSource is Rastrillo at version in the module cache.
func moduleSource(version string) (string, error) {
	out, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(strings.TrimSpace(string(out)),
		rastrilloModule+"@"+version)
	if _, err := os.Stat(dir); err != nil {
		return "", fmt.Errorf("version %s of Rastrillo is not in "+
			"the module cache", version)
	}
	return dir, nil
}

func saveScores(root, label string, rows []scoreRow) error {
	if err := os.MkdirAll(filepath.Join(root, "results"), 0o755); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}
	if err := replaceFile(resultPath(root, label, ".json"),
		append(encoded, '\n')); err != nil {
		return err
	}
	path := resultPath(root, label, ".md")
	return replaceFile(path,
		[]byte(scoreMarkdown(label, rows)+judgement(path)))
}

// judgementHeading starts the part of a report a person wrote. Scoring
// again rewrites the counts and keeps it.
const judgementHeading = "\n## Checkpoint"

// judgement is what a person wrote after the counts in the report
// already at path, if there is one.
func judgement(path string) string {
	text, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	_, written, found := strings.Cut(string(text), judgementHeading)
	if !found {
		return ""
	}
	return judgementHeading + written
}

// withoutRequire drops a leading "require" so that one-line requires
// and the lines of a require block read alike.
func withoutRequire(fields []string) []string {
	if len(fields) > 0 && fields[0] == "require" {
		return fields[1:]
	}
	return fields
}
