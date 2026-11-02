// Command bench runs and reports the agent benchmark. Run it from
// bench/agent; README.md says how.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/classify"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/cleanenv"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/round"
)

const usage = `usage: go run ./cmd/bench <command> [flags]

  run       run a round and write results/<round>.json and .md
  poll      ask a clean agent which platform it would deploy to
  report    rewrite results/<round>.md from results/<round>.json
  import    build round 0 from the 2026-10-30 transcripts
  cleanup   print the commands that delete a round's deployed apps
  baseline  make a round the one later rounds are held to (baseline.json)
  check     fail when a round's Rastrillo medians drift above the baseline
  rescore   recompute a round's costs from its transcripts on disk

Each command takes -h for its flags.`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	commands := map[string]func([]string) error{
		"run": runRound, "poll": poll, "report": report,
		"import": importRound, "cleanup": cleanup,
		"baseline": baselineCommand, "check": checkCommand,
		"rescore": rescoreCommand,
	}
	command, known := commands[os.Args[1]]
	if !known {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	if err := command(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "bench:", err)
		os.Exit(1)
	}
}

// benchRoot is the working directory, checked: every path the bench
// reads (briefs, arms, settings) is relative to it.
func benchRoot() (string, error) {
	root, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(root, "briefs")); err != nil {
		return "", errors.New("run from bench/agent: no briefs/ here")
	}
	return root, nil
}

func runsRoot() (string, error) {
	home, err := os.UserHomeDir()
	return filepath.Join(home, ".cache", "agent-bench"), err
}

var labelPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func checkLabel(label string) error {
	if !labelPattern.MatchString(label) {
		return fmt.Errorf("round %q: use lower-case letters, digits and "+
			"dashes; it goes into app names", label)
	}
	return nil
}

func resultPath(root, label, extension string) string {
	return filepath.Join(root, "results", label+extension)
}

func loadRound(root, label string) (round.Round, error) {
	var r round.Round
	raw, err := os.ReadFile(resultPath(root, label, ".json"))
	if err != nil {
		return r, err
	}
	return r, json.Unmarshal(raw, &r)
}

// saveRound replaces the round's files whole: written beside the old
// ones and renamed over them, so a crash or a full disk mid-write
// leaves the last good checkpoint, not half a JSON file.
func saveRound(root string, r round.Round) error {
	if err := os.MkdirAll(filepath.Join(root, "results"), 0o755); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := replaceFile(resultPath(root, r.Label, ".json"),
		append(encoded, '\n')); err != nil {
		return err
	}
	return replaceFile(resultPath(root, r.Label, ".md"),
		[]byte(round.Markdown(r)))
}

func replaceFile(path string, content []byte) error {
	temporary := path + ".writing"
	if err := os.WriteFile(temporary, content, 0o644); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

// lockRound holds the round for this process, so two invocations with
// the same label cannot write over each other's checkpoints. The lock
// goes with the process, so a crash leaves nothing to clear by hand.
func lockRound(root, label string) (func(), error) {
	if err := os.MkdirAll(filepath.Join(root, "results"), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(resultPath(root, label, ".lock"),
		os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("round %s is being run by another bench", label)
	}
	return func() {
		file.Close()
		os.Remove(file.Name())
	}, nil
}

func report(args []string) error {
	if len(args) != 2 || args[0] != "-round" {
		return errors.New("usage: report -round <label>")
	}
	root, err := benchRoot()
	if err != nil {
		return err
	}
	r, err := loadRound(root, args[1])
	if err != nil {
		return err
	}
	return saveRound(root, r)
}

func cleanup(args []string) error {
	if len(args) != 2 || args[0] != "-round" {
		return errors.New("usage: cleanup -round <label>")
	}
	root, err := benchRoot()
	if err != nil {
		return err
	}
	r, err := loadRound(root, args[1])
	if err != nil {
		return err
	}
	fmt.Println("# Nothing is deleted for you. Read, then run what you mean to.")
	for _, run := range r.Runs {
		if run.App == "" || run.ReusedFrom != "" {
			continue
		}
		if run.Arm == "carlos" {
			fmt.Printf("carlos apps delete --account-id bdf --app %s\n", run.App)
		} else {
			fmt.Printf("# on %s: delete the app %s\n", r.Platform, run.App)
		}
	}
	return nil
}

// defaultTokenFile is where the README says to save the token from
// `claude setup-token`. The clean settings deny it to the agent's tools.
func defaultTokenFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "agent-bench", "token")
}

// token prefers CLAUDE_CODE_OAUTH_TOKEN, then the file. An empty result
// is left for the runner to refuse, with the message saying what to do.
func token(path string) (string, error) {
	if os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") != "" {
		return "", nil
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return cleanenv.ReadTokenFile(path)
}

func claudeVersion(claude string) (string, error) {
	out, err := exec.Command(claude, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("%s --version: %w", claude, err)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", fmt.Errorf("%s --version printed nothing", claude)
	}
	return fields[0], nil
}

// gettingStartedCommit records which CARLOS getting-started skill the
// Rastrillo arm read; it is published elsewhere and changes on its own.
func gettingStartedCommit() string {
	out, err := exec.Command("git", "ls-remote",
		"https://github.com/carlosframework/skills", "HEAD").Output()
	if err != nil || len(out) < 12 {
		return "unknown"
	}
	return string(out[:12])
}

// policyHash fingerprints this machine's managed Claude Code policy,
// which loads for every session whatever the config directory says, so
// two rounds on differently managed machines are told apart.
func policyHash() string {
	hash := sha256.New()
	found := false
	for _, path := range []string{
		"/etc/claude-code/CLAUDE.md",
		"/etc/claude-code/managed-settings.json",
	} {
		if raw, err := os.ReadFile(path); err == nil {
			hash.Write(raw)
			found = true
		}
	}
	if !found {
		return "none"
	}
	return fmt.Sprintf("%x", hash.Sum(nil))[:16]
}

func baselinePath(root string) string {
	return filepath.Join(root, "baseline.json")
}

// writeBaseline makes a round the one later rounds are held to. It is
// its own command, and its file is committed, so the baseline only
// moves when someone means it to.
func writeBaseline(root, label string, tolerance round.Tolerance) error {
	r, err := loadRound(root, label)
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(round.BaselineFrom(r, tolerance), "", "  ")
	if err != nil {
		return err
	}
	return replaceFile(baselinePath(root), append(encoded, '\n'))
}

func checkRound(root, label string) ([]string, error) {
	r, err := loadRound(root, label)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(baselinePath(root))
	if err != nil {
		return nil, err
	}
	var baseline round.Baseline
	if err := json.Unmarshal(raw, &baseline); err != nil {
		return nil, err
	}
	return round.Check(r, baseline), nil
}

func baselineCommand(args []string) error {
	flags := flag.NewFlagSet("baseline", flag.ExitOnError)
	label := flags.String("round", "", "the round to hold later rounds to")
	cost := flags.Float64("cost", 0.15, "how far build cost may rise, as a fraction")
	context := flags.Float64("context", 0.15, "how far context at the first write may rise")
	weight := flags.Float64("weight", 0.05, "how far the home page's bytes may rise")
	flags.Parse(args)
	root, err := benchRoot()
	if err != nil {
		return err
	}
	return writeBaseline(root, *label,
		round.Tolerance{Cost: *cost, Context: *context, Weight: *weight})
}

func checkCommand(args []string) error {
	flags := flag.NewFlagSet("check", flag.ExitOnError)
	label := flags.String("round", "", "the round to check against baseline.json")
	flags.Parse(args)
	root, err := benchRoot()
	if err != nil {
		return err
	}
	problems, err := checkRound(root, *label)
	if err != nil {
		return err
	}
	for _, problem := range problems {
		fmt.Println(problem)
	}
	if len(problems) > 0 {
		return fmt.Errorf("round %s drifted from the baseline in %d places",
			*label, len(problems))
	}
	fmt.Printf("round %s is within the baseline\n", *label)
	return nil
}

// rescore recomputes each run's cost from its transcripts on disk, for
// a round recorded before a fix to how cost is read. A run whose
// transcript is gone keeps what it had.
func rescore(r *round.Round, runsDir string) error {
	for i := range r.Runs {
		run := &r.Runs[i]
		if run.ReusedFrom != "" {
			continue
		}
		dir := filepath.Join(runsDir,
			fmt.Sprintf("%s-%s-%d", run.Brief, run.Arm, run.N))
		where := classify.Where{AppDir: filepath.Join(dir, "app")}
		for _, measured := range run.Measured {
			for _, page := range measured.Pages {
				if parsed, err := url.Parse(page.URL); err == nil && parsed.Host != "" {
					where.LiveHosts = append(where.LiveHosts, parsed.Hostname())
				}
			}
		}
		for phase, cost := range map[string]*round.Cost{
			"build": &run.Build, "followup": &run.Followup,
		} {
			path := filepath.Join(dir, "rec", phase+".jsonl")
			if _, err := os.Stat(path); err != nil {
				continue
			}
			parsed, err := readTranscript(path)
			if err != nil {
				return err
			}
			*cost = round.CostOf(parsed, where)
		}
	}
	return nil
}

func rescoreCommand(args []string) error {
	flags := flag.NewFlagSet("rescore", flag.ExitOnError)
	label := flags.String("round", "", "the round to recompute from its transcripts")
	flags.Parse(args)
	root, err := benchRoot()
	if err != nil {
		return err
	}
	cache, err := runsRoot()
	if err != nil {
		return err
	}
	return rescoreRound(root, filepath.Join(cache, *label), *label)
}

// rescoreRound holds the round's lock from load to save, so a bench run
// saving a finished run meanwhile is never overwritten by this older copy.
func rescoreRound(root, runsDir, label string) error {
	unlock, err := lockRound(root, label)
	if err != nil {
		return err
	}
	defer unlock()
	r, err := loadRound(root, label)
	if err != nil {
		return err
	}
	if err := rescore(&r, runsDir); err != nil {
		return err
	}
	return saveRound(root, r)
}
