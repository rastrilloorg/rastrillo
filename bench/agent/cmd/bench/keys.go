package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/comprehend"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/transcript"
)

// keyedRun is one run to key, and how keying it went.
type keyedRun struct {
	round, brief, name, dir string
	keys                    []comprehend.Key
	err                     error
}

// keysCommand is `bench keys`: an answer key per kept run, written to
// ~/.cache/agent-bench/scoring/<round>/<run>/, apart from the runs,
// which it only reads, and out of every reader's sight.
func keysCommand(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("keys", flag.ContinueOnError)
	rounds := flags.String("rounds", "", "comma-separated round labels")
	briefs := flags.String("briefs", "", "comma-separated brief names")
	arms := flags.String("arms", rastrilloArms, "comma-separated arms to key")
	tokenFile := flags.String("token-file", defaultTokenFile(),
		"the token from claude setup-token, "+
			"if CLAUDE_CODE_OAUTH_TOKEN is not set")
	timeout := flags.Duration("timeout", time.Hour,
		"the key-maker's time per app")
	jobs := flags.Int("j", 1, "apps keyed at once")
	out := flags.String("out", "", "label for a results/<label>.md summary")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *rounds == "" || *briefs == "" || *jobs < 1 {
		return errors.New("usage: keys -rounds a,b -briefs x,y " +
			"[-arms carlos,carlos-gopls] [-j 1] [-out <label>]")
	}
	if *out != "" {
		if err := checkLabel(*out); err != nil {
			return err
		}
	}
	root, err := benchRoot()
	if err != nil {
		return err
	}
	cache, err := runsRoot()
	if err != nil {
		return err
	}
	authToken, err := token(*tokenFile)
	if err != nil {
		return err
	}
	var runs []*keyedRun
	for _, label := range strings.Split(*rounds, ",") {
		for _, brief := range strings.Split(*briefs, ",") {
			kept, err := keptRuns(root, cache, label,
				[]string{brief}, strings.Split(*arms, ","))
			if err != nil {
				return err
			}
			for _, run := range kept {
				runs = append(runs, &keyedRun{round: label, brief: brief,
					name: run.name, dir: run.dir})
			}
		}
	}
	maker := keyer{root: root, cache: cache, token: authToken,
		timeout: *timeout}
	eachAtOnce(ctx, len(runs), *jobs, func(index int) {
		run := runs[index]
		run.keys, run.err = maker.key(ctx, run)
		fmt.Fprintln(os.Stderr, "keys", run.round, run.name, tally(run))
	})
	if ctx.Err() != nil {
		return errors.New("keys: interrupted; the keys made are saved, " +
			"and running again makes only the rest")
	}
	if *out != "" {
		err := replaceFile(resultPath(root, *out, ".md"),
			[]byte(keysMarkdown(*out, runs)))
		if err != nil {
			return err
		}
	}
	return failures(runs)
}

// failures is every run that has no keys, joined: one failed run does
// not stop the others, but the command does not end as if it worked.
func failures(runs []*keyedRun) error {
	var failed []error
	for _, run := range runs {
		if run.err != nil {
			failed = append(failed,
				fmt.Errorf("%s/%s: %w", run.round, run.name, run.err))
		}
	}
	return errors.Join(failed...)
}

type keyer struct {
	root, cache, token string
	timeout            time.Duration
}

func keysPath(cache, round, run string) string {
	return filepath.Join(cache, "scoring", round, run, "keys.json")
}

// key makes one run's keys, or reads the ones made before: a key costs
// a full Claude session, so a second pass only fills the gaps. Keys made
// from other questions or by another prompt are refused, not reused.
func (k keyer) key(
	ctx context.Context, run *keyedRun,
) ([]comprehend.Key, error) {
	briefDir := filepath.Join(k.root, "briefs", run.brief)
	questionsHash, err := comprehend.QuestionsHash(briefDir)
	if err != nil {
		return nil, err
	}
	questions, err := comprehend.LoadQuestions(briefDir)
	if err != nil {
		return nil, err
	}
	path := keysPath(k.cache, run.round, run.name)
	if _, err := os.Stat(path); err == nil {
		return madeBefore(path, questions, questionsHash)
	}
	app := filepath.Join(run.dir, "app")
	version, err := rastrilloVersion(app)
	if err != nil {
		return nil, err
	}
	rastrillo, err := moduleSource(version)
	if err != nil {
		return nil, err
	}
	scratch, err := os.MkdirTemp("", "keys-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(scratch)
	ctx, cancel := context.WithTimeout(ctx, k.timeout)
	defer cancel()
	keys, err := comprehend.MakeKeys(ctx, comprehend.KeySetup{
		AppDir: app, RastrilloDir: rastrillo, Token: k.token,
		BenchRoot: k.root, Scratch: scratch,
	}, questions)
	if keepErr := keepTranscript(scratch, filepath.Dir(path),
		"keymaker.jsonl"); keepErr != nil {
		return nil, errors.Join(err, keepErr)
	}
	if err != nil {
		return nil, err
	}
	encoded, err := json.MarshalIndent(comprehend.KeyFile{
		Round: run.round, Run: run.name, Brief: run.brief,
		RastrilloVersion: version, Model: comprehend.KeyModel,
		QuestionsHash: questionsHash, PromptHash: comprehend.PromptHash(),
		Made: time.Now().UTC(), Keys: keys,
	}, "", "  ")
	if err != nil {
		return nil, err
	}
	return keys, replaceFile(path, append(encoded, '\n'))
}

// madeBefore reads the keys.json at path, made from questions, and
// holds its behavioural keys to today's check against the key-maker's
// transcript beside it. Without that transcript none of them settles.
func madeBefore(
	path string, questions []comprehend.Question, questionsHash string,
) ([]comprehend.Key, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var made comprehend.KeyFile
	if err := json.Unmarshal(raw, &made); err != nil {
		return nil, fmt.Errorf("keys.json: %w", err)
	}
	if made.QuestionsHash != questionsHash ||
		made.PromptHash != comprehend.PromptHash() {
		return nil, errors.New("keys.json was made from other questions " +
			"or by another key-maker prompt; delete it to make it again")
	}
	var run transcript.Transcript
	file, err := os.Open(filepath.Join(filepath.Dir(path), "keymaker.jsonl"))
	if err == nil {
		defer file.Close()
		if run, err = transcript.Parse(file); err != nil {
			return nil, fmt.Errorf("keymaker.jsonl: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return comprehend.Recheck(questions, made.Keys, run), nil
}

// keepTranscript saves a session's transcript, name, beside what it
// made, failed or not, so its answer can be checked against what was
// really done.
func keepTranscript(scratch, dir, name string) error {
	raw, err := os.ReadFile(filepath.Join(scratch, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), raw, 0o644)
}

type keyCounts struct{ settled, unsettled, missing int }

func countKeys(keys []comprehend.Key) keyCounts {
	var counts keyCounts
	for _, key := range keys {
		switch {
		case !key.Settled:
			counts.unsettled++
		case key.Missing:
			counts.missing++
			counts.settled++
		default:
			counts.settled++
		}
	}
	return counts
}

func tally(run *keyedRun) string {
	if run.err != nil {
		return "failed: " + run.err.Error()
	}
	counts := countKeys(run.keys)
	return fmt.Sprintf("%d settled (%d not built), %d unsettled",
		counts.settled, counts.missing, counts.unsettled)
}

func keysMarkdown(label string, runs []*keyedRun) string {
	var text strings.Builder
	fmt.Fprintf(&text, "# Answer keys: %s\n\n", label)
	text.WriteString("Made by `bench keys` with " + comprehend.KeyModel +
		"; the keys are in ~/.cache/agent-bench/scoring/<round>/<run>/" +
		"keys.json. A settled \"not built\" key is counted in both " +
		"settled and not built.\n\n")
	text.WriteString("| Round | Run | Settled | Not built | Unsettled " +
		"| Unsettled questions |\n")
	text.WriteString("|---|---|---|---|---|---|\n")
	for _, run := range runs {
		if run.err != nil {
			fmt.Fprintf(&text, "| %s | %s | failed: %s | | | |\n", run.round,
				run.name, strings.ReplaceAll(run.err.Error(), "\n", " "))
			continue
		}
		counts := countKeys(run.keys)
		var unsettled []string
		for _, key := range run.keys {
			if !key.Settled {
				unsettled = append(unsettled, key.ID)
			}
		}
		fmt.Fprintf(&text, "| %s | %s | %d | %d | %d | %s |\n", run.round,
			run.name, counts.settled, counts.missing, counts.unsettled,
			strings.Join(unsettled, ", "))
	}
	return text.String()
}
