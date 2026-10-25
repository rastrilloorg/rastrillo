// Command skilleval measures what a fresh agent spends building with
// Rastrillo when all it has is the skill: tokens, cost, turns, and how
// much framework source it read because the skill did not tell it
// enough. A change to SKILL.md is meant to lower those numbers; this
// is how to find out whether it did, instead of counting bytes.
//
// Each run exports HEAD (plus an optional skill overlay), scaffolds an
// app with that export's CLI, points the app's go.mod at the export,
// and gives claude -p one task from tasks/. The agent finds the skill
// the way a real one does, through the scaffold's AGENTS.md. Then the
// harness grades it: the scaffold's own make ci, with the scaffold's
// Makefile restored if the agent changed it, and the task's check
// script, which looks for the framework feature a skilled agent would
// have used instead of hand-rolling one. The checks probe discovery,
// not correctness; correctness rests on make ci and the app's tests.
//
//	go run ./hack/skilleval                                 # every task once, today's SKILL.md
//	go run ./hack/skilleval -tasks media-grid -reps 3
//	go run ./hack/skilleval -variant ../split -name split -out ~/.cache/skilleval/cmp
//	go run ./hack/skilleval -summarize ~/.cache/skilleval/cmp
//
// The agent runs as the operator, with --dangerously-skip-permissions
// and a trimmed environment: the export is read-only and its processes
// are killed when the run ends, but nothing else stops it reaching
// what the operator can. Run it where you would run claude unattended.
//
// Runs cost real money (-budget caps each one) and are not part of
// make ci: a runner must not spend tokens, and a model's variance is
// not a gate.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

type config struct {
	repo, variant, name, out, model string
	tasks                           []string
	newArgs                         []string // extra arguments to rastrillo new, before the app's name
	reps, jobs                      int
	budget                          float64
	timeout                         time.Duration
	keep, dry                       bool
}

// setupLimit bounds every step that is not the agent, so a stalled
// fetch cannot hold the harness before the agent's own deadline exists.
const setupLimit = 10 * time.Minute

// nameRE keeps task and variant names to one path element: both become
// directories the harness later deletes.
var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func main() {
	var c config
	var tasks, summarize, retally string
	flag.StringVar(&c.repo, "repo", "", "rastrillo checkout to export HEAD from (default: the enclosing git repository)")
	flag.StringVar(&c.variant, "variant", "", "directory whose SKILL.md and skill/ replace HEAD's in the export")
	flag.StringVar(&c.name, "name", "", "label for this variant in results (default head, or the variant directory's name)")
	flag.StringVar(&tasks, "tasks", "", "comma-separated task names (default: every tasks/*.md)")
	flag.IntVar(&c.reps, "reps", 1, "runs per task")
	flag.IntVar(&c.jobs, "j", 2, "runs at once")
	flag.StringVar(&c.model, "model", "sonnet", "claude -p --model")
	var newArgs string
	flag.StringVar(&newArgs, "new", "", "extra arguments to rastrillo new, space-separated (e.g. --auth=magic): the app every run starts from")
	flag.Float64Var(&c.budget, "budget", 6, "claude -p --max-budget-usd, per run")
	flag.DurationVar(&c.timeout, "timeout", 45*time.Minute, "wall-clock limit per agent run")
	flag.StringVar(&c.out, "out", "", "results directory (default ~/.cache/skilleval/<time>); reuse it to compare variants")
	flag.BoolVar(&c.keep, "keep", false, "keep each run's app and the export instead of deleting them after grading")
	flag.BoolVar(&c.dry, "dry", false, "skip the agent and grade the bare scaffold: every check but scheme's should fail, and make ci should pass")
	flag.StringVar(&summarize, "summarize", "", "print the summary of an existing results directory and exit")
	flag.StringVar(&retally, "retally", "", "re-read an existing results directory's transcripts with the current classifier, then summarize")
	flag.Parse()
	c.newArgs = strings.Fields(newArgs)
	log.SetFlags(log.Ltime)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	becomeSubreaper()
	defer killDescendants()

	var err error
	switch {
	case retally != "":
		err = retallyDir(ctx, retally)
	case summarize != "":
		err = writeSummary(summarize)
	default:
		err = run(ctx, c, tasks)
	}
	if err != nil {
		killDescendants()
		log.Fatal(err)
	}
}

func run(ctx context.Context, c config, tasks string) error {
	if c.reps < 1 || c.jobs < 1 {
		return errors.New("-reps and -j must be at least 1")
	}
	if tasks != "" {
		c.tasks = strings.Split(tasks, ",")
	}
	for _, t := range c.tasks {
		if !nameRE.MatchString(t) {
			return fmt.Errorf("task name %q: use lower-case letters, digits and dashes", t)
		}
	}
	var err error
	if c.repo == "" {
		out, err := outputOf(ctx, "", "git", "rev-parse", "--show-toplevel")
		if err != nil {
			return fmt.Errorf("finding the repository: %w", err)
		}
		c.repo = strings.TrimSpace(string(out))
	}
	// Every path is made absolute here because the subprocesses run in
	// other directories, where a relative one means something else.
	if c.repo, err = filepath.Abs(c.repo); err != nil {
		return err
	}
	if c.variant != "" {
		if c.variant, err = filepath.Abs(c.variant); err != nil {
			return err
		}
	}
	if c.name == "" {
		c.name = "head"
		if c.variant != "" {
			c.name = filepath.Base(c.variant)
		}
	}
	if !nameRE.MatchString(c.name) {
		return fmt.Errorf("variant name %q: use lower-case letters, digits and dashes", c.name)
	}
	if c.out == "" {
		home, _ := os.UserHomeDir()
		c.out = filepath.Join(home, ".cache", "skilleval", time.Now().Format("20060102-150405"))
	}
	if c.out, err = filepath.Abs(c.out); err != nil {
		return err
	}
	if err := os.MkdirAll(c.out, 0o755); err != nil {
		return err
	}
	vdir := filepath.Join(c.out, c.name)
	if err := os.Mkdir(vdir, 0o755); err != nil {
		return fmt.Errorf("%s: %w (each variant name gets one directory per -out; pick another)", vdir, err)
	}
	export, binDir := filepath.Join(vdir, "rastrillo"), filepath.Join(vdir, "bin")
	defer func() {
		if !c.keep {
			chmodTree(export, 0o200, true)
			for _, d := range []string{export, binDir} {
				if err := os.RemoveAll(d); err != nil {
					log.Printf("cleaning up %s: %v", d, err)
				}
			}
		}
	}()

	// The commit is resolved once, so what is exported and what
	// provenance names cannot differ if the checkout moves meanwhile.
	commit, err := gitOut(ctx, c.repo, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("resolving HEAD: %w", err)
	}
	bin, err := prepareExport(ctx, c, commit, export, binDir)
	if err != nil {
		return err
	}
	// Tasks come from the export, so they are HEAD's like the framework
	// is, and are copied beside the results so they outlive it.
	tasksDir := filepath.Join(vdir, "tasks")
	if err := runIn(ctx, "", nil, nil, "cp", "-R", filepath.Join(export, "hack", "skilleval", "tasks"), tasksDir); err != nil {
		return fmt.Errorf("copying the tasks: %w", err)
	}
	chmodTree(tasksDir, 0o200, true) // the copy is the results', not the read-only export's
	if len(c.tasks) == 0 {
		files, _ := filepath.Glob(filepath.Join(tasksDir, "*.md"))
		for _, f := range files {
			c.tasks = append(c.tasks, strings.TrimSuffix(filepath.Base(f), ".md"))
		}
	}
	if len(c.tasks) == 0 {
		return errors.New("no tasks")
	}
	for _, t := range c.tasks {
		// Names found in the export are checked as strictly as names on
		// the command line: both become directories that are deleted.
		if !nameRE.MatchString(t) {
			return fmt.Errorf("task name %q: use lower-case letters, digits and dashes", t)
		}
		if _, err := os.Stat(filepath.Join(tasksDir, t+".md")); err != nil {
			return fmt.Errorf("task %s: %w", t, err)
		}
	}
	if err := writeProvenance(ctx, c, commit, vdir, export, tasksDir); err != nil {
		return err
	}
	if err := preflight(ctx, c, bin, export, vdir); err != nil {
		return fmt.Errorf("the scaffold fails its own gate before any agent ran, so a run could not tell the agent's failures from the scaffold's: %w", err)
	}
	staleBase := moduleCacheBase(ctx)
	skillBytes, topicBytes := skillSize(export)
	log.Printf("variant %s: SKILL.md %d bytes, skill/ %d bytes; results in %s", c.name, skillBytes, topicBytes, c.out)

	var mu sync.Mutex
	sem := make(chan struct{}, c.jobs)
	var wg sync.WaitGroup
	for _, task := range c.tasks {
		for rep := 1; rep <= c.reps; rep++ {
			wg.Add(1)
			go func(task string, rep int) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				if ctx.Err() != nil {
					return
				}
				r := runOne(ctx, c, bin, export, vdir, tasksDir, task, rep, staleBase)
				r.SkillBytes, r.TopicBytes = skillBytes, topicBytes
				mu.Lock()
				defer mu.Unlock()
				if err := appendResult(c.out, r); err != nil {
					log.Printf("%s/%d: recording the result: %v", task, rep, err)
				}
				log.Printf("%s/%d: finished=%v ci=%v check=%v tokens=%d cost=$%.2f source=%d calls %s",
					task, rep, r.Finished, r.CI, r.Check, r.Tokens, r.CostUSD,
					r.Touched[bSource], r.Error)
			}(task, rep)
		}
	}
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return writeSummary(c.out)
}

// prepareExport writes HEAD as the published module would see it, so
// the agent reads committed files and nothing a working tree happens to
// hold, then builds the CLI from it and makes the export read-only: an
// agent that edits the framework to make its task pass has not used it.
func prepareExport(ctx context.Context, c config, commit, export, binDir string) (string, error) {
	if err := os.MkdirAll(export, 0o755); err != nil {
		return "", err
	}
	if err := runIn(ctx, "", goEnv(), nil, "sh", "-c", `git -C "$1" archive "$2" | tar -x -C "$3"`, "sh", c.repo, commit, export); err != nil {
		return "", fmt.Errorf("exporting %s: %w", commit, err)
	}
	if c.variant != "" {
		os.Remove(filepath.Join(export, "SKILL.md"))
		os.RemoveAll(filepath.Join(export, "skill"))
		if err := runIn(ctx, "", nil, nil, "cp", filepath.Join(c.variant, "SKILL.md"), filepath.Join(export, "SKILL.md")); err != nil {
			return "", fmt.Errorf("the variant needs a SKILL.md: %w", err)
		}
		if _, err := os.Stat(filepath.Join(c.variant, "skill")); err == nil {
			if err := runIn(ctx, "", nil, nil, "cp", "-R", filepath.Join(c.variant, "skill"), filepath.Join(export, "skill")); err != nil {
				return "", fmt.Errorf("copying skill/: %w", err)
			}
		}
	}
	bin := filepath.Join(binDir, "rastrillo")
	if err := runIn(ctx, export, goEnv(), nil, "go", "build", "-o", bin, "./cmd/rastrillo"); err != nil {
		return "", fmt.Errorf("building the CLI: %w", err)
	}
	chmodTree(export, 0o222, false)
	return bin, nil
}

// preflight scaffolds one app the way every run will and runs its
// gate, once per variant, before any token is spent.
func preflight(ctx context.Context, c config, bin, export, vdir string) error {
	dir := filepath.Join(vdir, "preflight")
	ctx = withRun(ctx, dir)
	if err := os.Mkdir(dir, 0o755); err != nil {
		return err
	}
	defer func() {
		if !c.keep {
			os.RemoveAll(dir)
		}
	}()
	logf, err := os.Create(filepath.Join(vdir, "preflight.log"))
	if err != nil {
		return err
	}
	defer logf.Close()
	app, err := scaffold(ctx, bin, export, dir, "preflight", c.newArgs, logf)
	if err != nil {
		return err
	}
	defer reap(dir)
	if err := runIn(ctx, app, nil, logf, "make", "-f", "Makefile", "ci"); err != nil {
		return fmt.Errorf("make ci (see %s): %w", logf.Name(), err)
	}
	return nil
}

// scaffold makes the app a run starts from and commits it, so the
// agent's work is a diff against a known commit. The require is pinned
// to the zero pseudo-version because the scaffold's own version names
// a module-cache copy: a baseline agent read go.mod, opened that older
// copy, and followed a SKILL.md that predated the feature it was asked
// to build.
func scaffold(ctx context.Context, bin, export, dir, appName string, newArgs []string, logf *os.File) (string, error) {
	app := filepath.Join(dir, appName)
	for _, step := range [][]string{
		append(append([]string{dir, bin, "new"}, newArgs...), appName),
		{app, "go", "mod", "edit", "-replace", "amadan.net/rastrillo/rastrillo=" + export,
			"-require", "amadan.net/rastrillo/rastrillo@v0.0.0-00010101000000-000000000000"},
		{app, "go", "mod", "tidy"},
		{app, "git", "init", "-q"},
		{app, "git", "add", "-A"},
		{app, "git", "-c", "user.name=skilleval", "-c", "user.email=skilleval@localhost", "-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false", "commit", "-qm", "scaffold"},
	} {
		if err := runIn(ctx, step[0], nil, logf, step[1], step[2:]...); err != nil {
			return "", fmt.Errorf("%s: %w", strings.Join(step[1:], " "), err)
		}
	}
	return app, nil
}

// runIn runs one setup step under setupLimit, with its output going to
// w (or into the error when w is nil), in env or, when env is nil, the
// allowlisted environment with the run's mark.
func runIn(ctx context.Context, dir string, env []string, w *os.File, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, setupLimit)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	if env == nil {
		env = envFor(ctx) // never the operator's whole environment
	}
	cmd.Dir, cmd.Env = dir, env
	if w != nil {
		cmd.Stdout, cmd.Stderr = w, w
		return runGroup(cmd)
	}
	var buf strings.Builder
	cmd.Stdout, cmd.Stderr = &buf, &buf
	if err := runGroup(cmd); err != nil {
		return fmt.Errorf("%v\n%s", err, buf.String())
	}
	return nil
}

func skillSize(export string) (skill, topics int) {
	if fi, err := os.Stat(filepath.Join(export, "SKILL.md")); err == nil {
		skill = int(fi.Size())
	}
	filepath.Walk(filepath.Join(export, "skill"), func(_ string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			topics += int(fi.Size())
		}
		return nil
	})
	return skill, topics
}

func chmodTree(root string, bits os.FileMode, add bool) {
	filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		m := fi.Mode().Perm()
		if add {
			m |= bits
		} else {
			m &^= bits
		}
		os.Chmod(p, m)
		return nil
	})
}
