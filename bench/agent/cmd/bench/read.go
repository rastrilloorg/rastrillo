package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/codex"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/comprehend"
)

// readApp is one kept app to read: its questions, keys, and the
// readings made so far, saved as each one comes in.
type readApp struct {
	round, brief, name, dir, version, rastrillo string
	questions                                   []comprehend.Question
	keys                                        []comprehend.Key
	file                                        readingFile
	saving                                      sync.Mutex
	stage                                       stage
}

// readingFile is readings.json, beside the app's keys: every reading
// made, by read, so a second pass only asks what is not yet answered.
// Readings made by another reader, prompt, question set or source are
// not today's.
type readingFile struct {
	Model, Effort, PromptHash, QuestionsHash, SourceHash string
	Reads                                                [][]comprehend.Reading
}

// readCommand is `bench read`: Astra answers each brief question from
// each kept app's code, a set number of times, and the answers are
// graded against the keys `bench keys` made.
func readCommand(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("read", flag.ContinueOnError)
	rounds := flags.String("rounds", "", "comma-separated round labels")
	briefs := flags.String("briefs", "", "comma-separated brief names")
	arms := flags.String("arms", rastrilloArms, "comma-separated arms to read")
	only := flags.String("runs", "",
		"comma-separated run names to read, if not every kept run")
	asked := flags.String("questions", "",
		"comma-separated question ids to ask, if not every question")
	reads := flags.Int("reads", 2, "times each question is read per app")
	budget := flags.Int("budget", 200000,
		"tokens a reading may spend; one over it is graded wrong")
	jobs := flags.Int("j", 1, "readings at once (readers share one codex login; parallel refreshes can revoke it)")
	timeout := flags.Duration("timeout", 20*time.Minute,
		"a reader's time per question")
	out := flags.String("out", "",
		"label for results/comprehension-<label>.json and .md")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *rounds == "" || *briefs == "" || *out == "" || *reads < 1 ||
		*jobs < 1 || *budget < 1 {
		return errors.New("usage: read -rounds a,b -briefs x,y " +
			"-out <label> [-reads 2] [-budget 200000] [-j 3]")
	}
	if err := checkLabel(*out); err != nil {
		return err
	}
	root, err := benchRoot()
	if err != nil {
		return err
	}
	cache, err := runsRoot()
	if err != nil {
		return err
	}
	apps, err := appsToRead(root, cache, *rounds, *briefs, *arms, *only)
	if err != nil {
		return err
	}
	reader := readerRun{cache: cache, budget: *budget, timeout: *timeout,
		asked: splitList(*asked)}
	failed := reader.readAll(ctx, apps, *reads, *jobs)
	if ctx.Err() != nil {
		return errors.New("read: interrupted; the readings made are " +
			"saved, the report is left as it was, and running again " +
			"picks up where this stopped")
	}
	report := gradeAll(apps, *reads, *budget)
	for _, failure := range failed {
		report.Failed = append(report.Failed, failure.Error())
	}
	if err := saveComprehension(root, *out, report); err != nil {
		return errors.Join(append(failed, err)...)
	}
	return errors.Join(failed...)
}

// appsToRead finds the kept runs, their keys and their saved readings.
// An app with no keys, or keys made from other code or questions, is
// refused: nothing can be graded without them.
func appsToRead(
	root, cache, rounds, briefs, arms, only string,
) ([]*readApp, error) {
	var apps []*readApp
	for _, label := range strings.Split(rounds, ",") {
		for _, brief := range strings.Split(briefs, ",") {
			kept, err := keptRuns(root, cache, label,
				[]string{brief}, strings.Split(arms, ","))
			if err != nil {
				return nil, err
			}
			for _, run := range kept {
				if only != "" && !slices.Contains(splitList(only), run.name) {
					continue
				}
				app, err := loadApp(root, cache, label, brief, run)
				if err != nil {
					return nil, fmt.Errorf("%s/%s: %w", label, run.name, err)
				}
				apps = append(apps, app)
			}
		}
	}
	if len(apps) == 0 {
		return nil, errors.New("no kept runs match")
	}
	return apps, nil
}

func loadApp(
	root, cache, round, brief string, run keptRun,
) (*readApp, error) {
	name := run.name
	briefDir := filepath.Join(root, "briefs", brief)
	questions, err := comprehend.LoadQuestions(briefDir)
	if err != nil {
		return nil, err
	}
	questionsHash, err := comprehend.QuestionsHash(briefDir)
	if err != nil {
		return nil, err
	}
	path := keysPath(cache, round, name)
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("no keys (run bench keys first): %w", err)
	}
	keys, err := madeBefore(path, questions, questionsHash)
	if err != nil {
		return nil, err
	}
	app := &readApp{round: round, brief: brief, name: name,
		dir: run.dir, questions: questions,
		keys: keys}
	sourceHash, err := comprehend.SourceHash(filepath.Join(app.dir, "app"))
	if err != nil {
		return nil, err
	}
	for _, key := range keys {
		if key.SourceHash != sourceHash {
			return nil, errors.New("the app's source is not what its " +
				"keys were made from")
		}
	}
	if app.version, err = rastrilloVersion(filepath.Join(app.dir,
		"app")); err != nil {
		return nil, err
	}
	if app.rastrillo, err = moduleSource(app.version); err != nil {
		return nil, err
	}
	app.file = readingFile{Model: comprehend.ReaderModel,
		Effort: comprehend.ReaderEffort, PromptHash: comprehend.ReaderPromptHash(),
		QuestionsHash: questionsHash, SourceHash: sourceHash}
	return app, app.loadReadings(cache)
}

func readingsPath(cache, round, run string) string {
	return filepath.Join(cache, "scoring", round, run, "readings.json")
}

// loadReadings takes up the readings saved before, if they were made
// as today's would be.
func (app *readApp) loadReadings(cache string) error {
	raw, err := os.ReadFile(readingsPath(cache, app.round, app.name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var saved readingFile
	if err := json.Unmarshal(raw, &saved); err != nil {
		return fmt.Errorf("readings.json: %w", err)
	}
	today := app.file
	today.Reads, saved.Reads = saved.Reads, nil
	if !sameMaking(saved, today) {
		return errors.New("readings.json was made by another reader, " +
			"prompt, question set or source; delete it to read again")
	}
	app.file = today
	return nil
}

func sameMaking(saved, today readingFile) bool {
	return saved.Model == today.Model && saved.Effort == today.Effort &&
		saved.PromptHash == today.PromptHash &&
		saved.QuestionsHash == today.QuestionsHash &&
		saved.SourceHash == today.SourceHash
}

// answered is whether read already holds a reading of the question.
func (app *readApp) answered(read int, questionID string) bool {
	app.saving.Lock()
	defer app.saving.Unlock()
	return read < len(app.file.Reads) &&
		slices.ContainsFunc(app.file.Reads[read],
			func(reading comprehend.Reading) bool {
				return reading.QuestionID == questionID
			})
}

// save adds a reading and writes the file whole, so an interrupted run
// keeps every reading it paid for.
func (app *readApp) save(
	cache string, read int, reading comprehend.Reading,
) error {
	app.saving.Lock()
	defer app.saving.Unlock()
	for len(app.file.Reads) <= read {
		app.file.Reads = append(app.file.Reads, nil)
	}
	app.file.Reads[read] = append(app.file.Reads[read], reading)
	encoded, err := json.MarshalIndent(app.file, "", "  ")
	if err != nil {
		return err
	}
	path := readingsPath(cache, app.round, app.name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return replaceFile(path, append(encoded, '\n'))
}

// stage is an app's reader directory, made when its first reading
// starts and removed when its last one ends.
type stage struct {
	mutex   sync.Mutex
	dir     string
	err     error
	pending int
}

func (app *readApp) enter() (string, error) {
	app.stage.mutex.Lock()
	defer app.stage.mutex.Unlock()
	if app.stage.dir == "" && app.stage.err == nil {
		app.stage.dir, app.stage.err = os.MkdirTemp("", "read-")
		if app.stage.err == nil {
			app.stage.err = comprehend.Stage(filepath.Join(app.dir, "app"),
				app.rastrillo, app.stage.dir)
		}
	}
	return app.stage.dir, app.stage.err
}

func (app *readApp) leave() {
	app.stage.mutex.Lock()
	defer app.stage.mutex.Unlock()
	app.stage.pending--
	if app.stage.pending == 0 && app.stage.dir != "" {
		os.RemoveAll(app.stage.dir)
		app.stage.dir = ""
	}
}

type readerRun struct {
	cache   string
	budget  int
	timeout time.Duration
	asked   []string
}

type readJob struct {
	app      *readApp
	read     int
	question comprehend.Question
}

// readAll asks every question not yet answered, app by app, so few
// staged copies exist at once. One failed reading does not stop the
// others; each comes back as an error.
func (r readerRun) readAll(
	ctx context.Context, apps []*readApp, reads, jobs int,
) []error {
	var queue []readJob
	for _, app := range apps {
		for read := range reads {
			for _, question := range app.questions {
				if r.wanted(app, question) && !app.answered(read, question.ID) {
					queue = append(queue, readJob{app, read, question})
					app.stage.pending++
				}
			}
		}
	}
	fmt.Fprintf(os.Stderr, "read: %d readings to make\n", len(queue))
	var failed []error
	var failing sync.Mutex
	slots := make(chan struct{}, jobs)
	var group sync.WaitGroup
	for i, job := range queue {
		if !r.slot(ctx, slots) {
			for _, undone := range queue[i:] {
				undone.app.leave()
			}
			break
		}
		group.Add(1)
		go func() {
			defer group.Done()
			defer func() { <-slots }()
			defer job.app.leave()
			if err := r.readOne(ctx, job); err != nil {
				failing.Lock()
				failed = append(failed, fmt.Errorf("%s/%s read %d: %w",
					job.app.round, job.app.name, job.read+1, err))
				failing.Unlock()
				fmt.Fprintln(os.Stderr, "read failed:", job.app.round,
					job.app.name, job.read+1, err)
			}
		}()
	}
	group.Wait()
	return failed
}

// slot waits for a free reader, or for ctx to end: once it has, no
// more readings start.
func (r readerRun) slot(ctx context.Context, slots chan struct{}) bool {
	if ctx.Err() != nil {
		return false
	}
	select {
	case slots <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

// wanted is whether a question is to be asked of an app: it was asked
// for, and its key is settled. An unsettled key grades nothing, so
// reading it would spend tokens for no answer.
func (r readerRun) wanted(app *readApp, question comprehend.Question) bool {
	if len(r.asked) > 0 && !slices.Contains(r.asked, question.ID) {
		return false
	}
	return slices.ContainsFunc(app.keys, func(key comprehend.Key) bool {
		return key.ID == question.ID && key.Settled
	})
}

func (r readerRun) readOne(ctx context.Context, job readJob) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir, err := job.app.enter()
	if err != nil {
		return err
	}
	reader := codex.Exec{Model: comprehend.ReaderModel,
		Effort: comprehend.ReaderEffort, Dir: dir, Timeout: r.timeout}
	reading, err := comprehend.Read(ctx, reader, job.question, r.budget)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "read %s/%s %d %s: %q, %d tokens, %d commands\n",
		job.app.round, job.app.name, job.read+1, job.question.ID,
		reading.Answer, reading.Tokens, reading.Commands)
	return job.app.save(r.cache, job.read, reading)
}

// comprehension is results/comprehension-<label>.json.
type comprehension struct {
	Model, Effort string
	Reads, Budget int
	Apps          []comprehend.AppReads
	Gates         []comprehend.Gate
	Budgeting     budgeting
	Failed        []string `json:",omitempty"`
}

// budgeting suggests the next budget from the answers that were right
// before any budget was applied, so this run's budget does not decide
// what the next one is.
type budgeting struct {
	RightAnswers int
	Percentile95 float64
	Suggested    int // Percentile95 rounded up to the next 10,000
	Cut          int // right answers this run's budget made wrong
}

// gradeAll scores each read of each app against its keys at today's
// budget: a reading saved under another budget is judged by this one.
func gradeAll(apps []*readApp, reads, budget int) comprehension {
	report := comprehension{Model: comprehend.ReaderModel,
		Effort: comprehend.ReaderEffort, Reads: reads, Budget: budget}
	var right []float64
	for _, app := range apps {
		graded := comprehend.AppReads{Round: app.round, Run: app.name,
			Brief: app.brief, Version: app.version}
		for read := range min(reads, len(app.file.Reads)) {
			readings := inQuestionOrder(app, slices.Clone(app.file.Reads[read]))
			unbudgeted := comprehend.Score(app.name, app.questions, app.keys,
				withBudget(readings, math.MaxInt))
			budgeted := comprehend.Score(app.name, app.questions, app.keys,
				withBudget(readings, budget))
			for i, reading := range unbudgeted.Readings {
				if reading.Correct {
					right = append(right, float64(reading.Tokens))
				}
				if reading.Correct && !budgeted.Readings[i].Correct {
					report.Budgeting.Cut++
				}
			}
			graded.Reads = append(graded.Reads, budgeted)
		}
		graded.MissingReadings = missingReadings(app, reads)
		report.Apps = append(report.Apps, graded)
	}
	report.Gates = comprehend.NoiseGates(report.Apps)
	report.Budgeting.RightAnswers = len(right)
	report.Budgeting.Percentile95 = comprehend.Percentile(right, 95)
	report.Budgeting.Suggested = int(math.Ceil(
		report.Budgeting.Percentile95/10000)) * 10000
	return report
}

// missingReadings is how many readings of the app's settled questions
// its first reads reads lack.
func missingReadings(app *readApp, reads int) int {
	missing := 0
	for _, key := range app.keys {
		if !key.Settled {
			continue
		}
		for read := range reads {
			if !app.answered(read, key.ID) {
				missing++
			}
		}
	}
	return missing
}

func withBudget(
	readings []comprehend.Reading, budget int,
) []comprehend.Reading {
	budgeted := slices.Clone(readings)
	for i := range budgeted {
		budgeted[i].OverBudget = budgeted[i].Tokens > budget
	}
	return budgeted
}

func inQuestionOrder(
	app *readApp, readings []comprehend.Reading,
) []comprehend.Reading {
	position := map[string]int{}
	for i, question := range app.questions {
		position[question.ID] = i
	}
	slices.SortStableFunc(readings, func(a, b comprehend.Reading) int {
		return position[a.QuestionID] - position[b.QuestionID]
	})
	return readings
}

func saveComprehension(root, label string, report comprehension) error {
	if err := os.MkdirAll(filepath.Join(root, "results"), 0o755); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	name := "comprehension-" + label
	if err := replaceFile(resultPath(root, name, ".json"),
		append(encoded, '\n')); err != nil {
		return err
	}
	path := resultPath(root, name, ".md")
	return replaceFile(path,
		[]byte(comprehensionMarkdown(label, report)+judgement(path)))
}
