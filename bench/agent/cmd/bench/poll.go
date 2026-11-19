package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/brief"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/classify"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/round"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/runner"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/source"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/transcript"
)

const pollQuestion = "Don't build anything. Which hosting platform would " +
	"you deploy this app to? Answer with one name only."

type pollResult struct {
	Date, Brief, Model, ClaudeVersion, Question string
	Answers                                     []string
	MostCommon                                  string   // "" on a tie
	Tied                                        []string // the answers level at the top
}

// mostCommon picks the answer given most often, ignoring case. A tie
// has no winner: choosing one would be the bench deciding the control's
// platform, which is the operator's call or another poll's.
func mostCommon(answers []string) (string, []string) {
	counts := map[string]int{}
	first := map[string]string{}
	var order []string
	for _, answer := range answers {
		key := strings.ToLower(answer)
		if _, seen := first[key]; !seen {
			first[key] = answer
			order = append(order, key)
		}
		counts[key]++
	}
	best := 0
	for _, key := range order {
		best = max(best, counts[key])
	}
	var top []string
	for _, key := range order {
		if counts[key] == best {
			top = append(top, first[key])
		}
	}
	if len(top) == 1 {
		return top[0], nil
	}
	return "", top
}

// pollLabel names one poll, down to the nanosecond: a second poll must
// never write over the first's transcripts or answers, least of all the
// one a tie asks for.
func pollLabel() string {
	return time.Now().UTC().Format("2006-01-02-150405.000000000")
}

// poll asks what an unguided agent would deploy the brief to, so the
// control deploys where a person with no framework would end up.
func poll(args []string) error {
	flags := flag.NewFlagSet("poll", flag.ExitOnError)
	briefName := flags.String("brief", "groomer", "the brief to ask about")
	asks := flags.Int("n", 5, "how many fresh sessions to ask")
	model := flags.String("model", "claude-opus-5-5", "the model")
	claude := flags.String("claude", "claude", "the claude binary")
	tokenFile := flags.String("token-file", defaultTokenFile(), "the token from claude setup-token, if CLAUDE_CODE_OAUTH_TOKEN is not set")
	flags.Parse(args)
	authToken, err := token(*tokenFile)
	if err != nil {
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
	b, err := brief.Load(filepath.Join(root, "briefs", *briefName))
	if err != nil {
		return err
	}
	version, err := claudeVersion(*claude)
	if err != nil {
		return err
	}
	date := time.Now().UTC().Format("2006-01-02")
	label := pollLabel()
	result := pollResult{
		Date: date, Brief: b.Name, Model: *model, ClaudeVersion: version,
		Question: pollQuestion,
	}
	for ask := 1; ask <= *asks; ask++ {
		spec := runner.Spec{
			Model: *model, BenchRoot: root, Claude: *claude,
			ClaudeVersion: version, Brief: b, Source: source.Latest(),
			Timeout:   10 * time.Minute,
			Token:     authToken,
			TokenFile: *tokenFile,
			Dir:       filepath.Join(cache, "poll-"+label, fmt.Sprint(ask)),
			Vars: brief.Vars{
				DeployAccount: "no account: this session only answers a question",
			},
		}
		run, err := runner.Ask(context.Background(), spec, "poll",
			b.Text+"\n\n"+pollQuestion)
		if err != nil {
			return err
		}
		answer := ""
		if run.Result != nil {
			answer = strings.Trim(strings.TrimSpace(run.Result.Text), "*`.")
		}
		fmt.Printf("%d: %s\n", ask, answer)
		result.Answers = append(result.Answers, answer)
	}
	result.MostCommon, result.Tied = mostCommon(result.Answers)
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(root, "results", "poll-"+label+".json")
	if result.MostCommon == "" {
		fmt.Printf("a tie between %s: ask again or choose (written to %s)\n",
			strings.Join(result.Tied, " and "), path)
	} else {
		fmt.Printf("most common: %s (written to %s)\n", result.MostCommon, path)
	}
	return os.WriteFile(path, append(encoded, '\n'), 0o644)
}

// importRound builds round 0 from the 2026-10-30 runs, which used the
// operator's own config and were never measured: kept as history, with
// notes that say so, never as a baseline.
func importRound(args []string) error {
	flags := flag.NewFlagSet("import", flag.ExitOnError)
	label := flags.String("round", "0", "the label to write")
	home, _ := os.UserHomeDir()
	from := flags.String("from", filepath.Join(home, ".cache", "demo-replays"),
		"the directory holding groomer-carlos/ and groomer-control/")
	flags.Parse(args)

	root, err := benchRoot()
	if err != nil {
		return err
	}
	r := round.Round{
		Label: *label, Date: "2026-10-30", Rastrillo: "v0.29.0",
		Platform: "none (the control did not deploy)",
		Notes: []string{
			"Imported from the 2026-10-30 transcripts: one run per arm, groomer only.",
			"Both arms ran on the operator's own Claude Code config, with eight plugins, the Concise output style and a personal CLAUDE.md.",
			"The control was asked to get ready to deploy, not to deploy; only the Rastrillo arm deployed.",
			"Nothing was measured live, so this round has costs only.",
			"The prompts differ from later rounds: the framework sentences were part of the brief.",
		},
	}
	for _, arm := range []brief.Arm{brief.Carlos, brief.Control} {
		dir := filepath.Join(*from, "groomer-"+string(arm))
		where := classify.Where{AppDir: filepath.Join(dir, "app")}
		if arm == brief.Carlos {
			where.LiveHosts = []string{"groomer.bdf.oncarlos.com"}
		}
		build, err := readTranscript(filepath.Join(dir, "rec", "session1.jsonl"))
		if err != nil {
			return err
		}
		followup, err := readTranscript(filepath.Join(dir, "rec", "session2.jsonl"))
		if err != nil {
			return err
		}
		r.Model, r.ClaudeVersion = build.Init.Model, build.Init.Version
		record := round.RunRecord{
			Brief: "groomer", Arm: arm, N: 1,
			Build:    round.CostOf(build, where),
			Followup: round.CostOf(followup, where),
		}
		if arm == brief.Carlos {
			record.RastrilloVersion = "v0.29.0"
		}
		r.Runs = append(r.Runs, record)
	}
	return saveRound(root, r)
}

func readTranscript(path string) (transcript.Transcript, error) {
	file, err := os.Open(path)
	if err != nil {
		return transcript.Transcript{}, err
	}
	defer file.Close()
	return transcript.Parse(file)
}
