package calibrate

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/authored"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/comprehend"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/transcript"
)

// LoadJobs reads briefDir/jobs.txt: the jobs every app of the brief
// does, one a line.
func LoadJobs(briefDir string) ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(briefDir, "jobs.txt"))
	if err != nil {
		return nil, err
	}
	var jobs []string
	for _, line := range strings.Split(string(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			jobs = append(jobs, line)
		}
	}
	return jobs, nil
}

// JobsHash fingerprints briefDir/jobs.txt.
func JobsHash(briefDir string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(briefDir, "jobs.txt"))
	return fmt.Sprintf("%x", sha256.Sum256(raw)), err
}

// JobsPromptHash fingerprints the job finder's instructions.
func JobsPromptHash() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(jobInstructions)))
}

// FindJobs has Claude (comprehend.KeyModel, on the bench token, in the
// bench's clean environment) name the functions that do each job in one
// app, reading a copy of it. It is offered every function of the app
// but its tests, as path:Key, and may name only those; the caller keeps
// the ones the agent wrote. A job the app does not do has no functions.
func FindJobs(
	ctx context.Context, setup comprehend.KeySetup, jobs []string,
) (map[string][]string, error) {
	app, err := comprehend.CopyApp(setup)
	if err != nil {
		return nil, err
	}
	functions, err := appFunctions(app,
		filepath.Join(setup.Scratch, "no-scaffold"))
	if err != nil {
		return nil, err
	}
	run, err := comprehend.Ask(ctx, setup, app,
		jobPrompt(setup.RastrilloDir, jobs, functions), "Read,Grep,Glob",
		"jobfinder")
	if err != nil {
		return nil, err
	}
	return jobsFrom(run, jobs, functions)
}

// appFunctions is every function in the app but its tests, as path:Key:
// compared with a scaffold that does not exist, all of it is authored.
func appFunctions(app, noScaffold string) ([]string, error) {
	code, err := authored.Authored(app, noScaffold)
	if err != nil {
		return nil, err
	}
	var functions []string
	for _, file := range code.Files {
		if file.Test {
			continue
		}
		for _, one := range file.Funcs {
			functions = append(functions, file.Path+":"+one.Key)
		}
	}
	return functions, nil
}

// jobAnswer is the job finder's answer for one job, by its number.
type jobAnswer struct {
	Job       int      `json:"job"`
	Functions []string `json:"functions"`
}

// jobsFrom reads the job finder's answer: its last, or failing that its
// latest message that holds one. Every job must be answered, and only
// with functions it was offered.
func jobsFrom(
	run transcript.Transcript, jobs, functions []string,
) (map[string][]string, error) {
	answers, found := parseJobs(run.Result.Text)
	for index := len(run.Messages) - 1; !found && index >= 0; index-- {
		answers, found = parseJobs(run.Messages[index].Text)
	}
	if !found {
		return nil, fmt.Errorf("the job finder's answer holds no JSON array "+
			"of jobs: %.300q", run.Result.Text)
	}
	byJob := map[string][]string{}
	for _, answer := range answers {
		if answer.Job < 1 || answer.Job > len(jobs) {
			return nil, fmt.Errorf("the job finder answered job %d of %d",
				answer.Job, len(jobs))
		}
		named := []string{}
		for _, function := range answer.Functions {
			if !slices.Contains(functions, function) {
				return nil, fmt.Errorf("the job finder named %q for %q, "+
					"which is not one of the app's functions", function,
					jobs[answer.Job-1])
			}
			if !slices.Contains(named, function) {
				named = append(named, function)
			}
		}
		if _, seen := byJob[jobs[answer.Job-1]]; !seen {
			byJob[jobs[answer.Job-1]] = named
		}
	}
	for _, job := range jobs {
		if _, answered := byJob[job]; !answered {
			return nil, fmt.Errorf("the job finder did not answer %q", job)
		}
	}
	return byJob, nil
}

// parseJobs finds the JSON array of jobs in an answer, whatever prose
// or code fence is around it.
func parseJobs(answer string) ([]jobAnswer, bool) {
	for start := strings.Index(answer, "["); start >= 0; {
		var answers []jobAnswer
		decoder := json.NewDecoder(strings.NewReader(answer[start:]))
		if decoder.Decode(&answers) == nil && len(answers) > 0 &&
			answers[0].Job > 0 {
			return answers, true
		}
		next := strings.Index(answer[start+1:], "[")
		if next < 0 {
			break
		}
		start += 1 + next
	}
	return nil, false
}

func jobPrompt(rastrilloDir string, jobs, functions []string) string {
	var listed strings.Builder
	for index, job := range jobs {
		fmt.Fprintf(&listed, "%d. %s\n", index+1, job)
	}
	return fmt.Sprintf(jobInstructions, rastrilloDir, listed.String(),
		strings.Join(functions, "\n"))
}

const jobInstructions = `This directory is a web app another agent ` +
	`built. Find the code that does each of the jobs below.

The app uses Rastrillo, a Go web framework; its source at the version ` +
	`this app uses is at %s (read only), if you need to know what a ` +
	`framework call does.

For each job, name the function where the app does the work of that ` +
	`job, and the helpers it calls that were written for that job: a ` +
	`function and its helpers, not the whole path a request takes. Where ` +
	`the work is passed along a chain (a handler that hands it to a ` +
	`service that hands it to a store), name the link where the job's own ` +
	`decisions are made (which slots are free, whether a booking is ` +
	`valid, what is saved), and its helpers; if the handler does the work ` +
	`itself, name the handler. Leave out what only passes the request on, ` +
	`renders a page or form, or is shared by many jobs (main, the route ` +
	`table, middleware, general helpers), and tests. A function may do ` +
	`more than one job. If the app does not do a job at all, give it no ` +
	`functions. Read the code to decide; do not guess from names. Do not ` +
	`change any file.

The jobs:
%s
The app's functions, as path:Name. Name each one exactly as it is ` +
	`written here; nothing else counts:
%s

End with a JSON array and nothing after it, one object per job, by its ` +
	`number:
[{"job": 1, "functions": ["path:Name", ...]}]
`
