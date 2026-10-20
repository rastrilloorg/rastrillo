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
// harness, not the agent, grades it: the app's own make ci, and the
// task's check script, which looks for the framework feature a skilled
// agent would have used instead of hand-rolling one.
//
//	go run ./hack/skilleval                                 # every task once, today's SKILL.md
//	go run ./hack/skilleval -tasks media-grid -reps 3
//	go run ./hack/skilleval -variant ../split -name split -out ~/.cache/skilleval/cmp
//	go run ./hack/skilleval -summarize ~/.cache/skilleval/cmp
//
// Runs cost real money (-budget caps each one) and are not part of
// make ci: a runner must not spend tokens, and a model's variance is
// not a gate.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

type config struct {
	repo, variant, name, out, model string
	tasks                           []string
	reps, jobs                      int
	budget                          float64
	timeout                         time.Duration
	keep, dry                       bool
}

type result struct {
	Variant    string `json:"variant"`
	Task       string `json:"task"`
	Rep        int    `json:"rep"`
	SkillBytes int    `json:"skill_bytes"` // SKILL.md alone: what every session pays
	TopicBytes int    `json:"topic_bytes"` // skill/*.md: what a session pays only if it reads them
	tally
	CI       bool   `json:"ci"`
	Check    bool   `json:"check"`
	Diff     string `json:"diff"`
	Error    string `json:"error,omitempty"`
	Started  string `json:"started"`
	AgentSec int    `json:"agent_sec"`
}

func main() {
	var c config
	var tasks, summarize string
	flag.StringVar(&c.repo, "repo", "", "rastrillo checkout to export HEAD from (default: the enclosing git repository)")
	flag.StringVar(&c.variant, "variant", "", "directory whose SKILL.md and skill/ replace HEAD's in the export")
	flag.StringVar(&c.name, "name", "", "label for this variant in results (default head, or the variant directory's name)")
	flag.StringVar(&tasks, "tasks", "", "comma-separated task names (default: every tasks/*.md)")
	flag.IntVar(&c.reps, "reps", 1, "runs per task")
	flag.IntVar(&c.jobs, "j", 2, "runs at once")
	flag.StringVar(&c.model, "model", "sonnet", "claude -p --model")
	flag.Float64Var(&c.budget, "budget", 6, "claude -p --max-budget-usd, per run")
	flag.DurationVar(&c.timeout, "timeout", 45*time.Minute, "wall-clock limit per agent run")
	flag.StringVar(&c.out, "out", "", "results directory (default ~/.cache/skilleval/<time>); reuse it to compare variants")
	flag.BoolVar(&c.keep, "keep", false, "keep each run's app and the export instead of deleting them after grading")
	flag.BoolVar(&c.dry, "dry", false, "skip the agent and grade the bare scaffold: every check but scheme's should fail, and make ci should pass")
	flag.StringVar(&summarize, "summarize", "", "print the summary of an existing results directory and exit")
	flag.Parse()
	log.SetFlags(log.Ltime)

	if summarize != "" {
		if err := writeSummary(summarize); err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := run(c, tasks); err != nil {
		log.Fatal(err)
	}
}

func run(c config, tasks string) error {
	if c.repo == "" {
		out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
		if err != nil {
			return fmt.Errorf("finding the repository: %w", err)
		}
		c.repo = strings.TrimSpace(string(out))
	}
	if c.name == "" {
		c.name = "head"
		if c.variant != "" {
			c.name = filepath.Base(filepath.Clean(c.variant))
		}
	}
	if c.out == "" {
		home, _ := os.UserHomeDir()
		c.out = filepath.Join(home, ".cache", "skilleval", time.Now().Format("20060102-150405"))
	}
	taskDir := filepath.Join(c.repo, "hack", "skilleval", "tasks")
	if tasks == "" {
		files, _ := filepath.Glob(filepath.Join(taskDir, "*.md"))
		for _, f := range files {
			c.tasks = append(c.tasks, strings.TrimSuffix(filepath.Base(f), ".md"))
		}
	} else {
		c.tasks = strings.Split(tasks, ",")
	}
	if len(c.tasks) == 0 {
		return errors.New("no tasks")
	}

	vdir := filepath.Join(c.out, c.name)
	export := filepath.Join(vdir, "rastrillo")
	if _, err := os.Stat(export); err == nil {
		return fmt.Errorf("%s already exists: pick another -name or -out", vdir)
	}
	bin, err := prepareExport(c, export, filepath.Join(vdir, "bin"))
	if err != nil {
		return err
	}
	defer func() {
		if !c.keep {
			chmodTree(export, 0o200, true)
			os.RemoveAll(export)
			os.RemoveAll(filepath.Join(vdir, "bin"))
		}
	}()
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
				r := runOne(c, bin, export, taskDir, task, rep)
				r.SkillBytes, r.TopicBytes = skillBytes, topicBytes
				mu.Lock()
				defer mu.Unlock()
				if err := appendResult(c.out, r); err != nil {
					log.Printf("%s/%d: recording the result: %v", task, rep, err)
				}
				log.Printf("%s/%d: finished=%v ci=%v check=%v tokens=%d cost=$%.2f source=%d calls %s",
					task, rep, r.Finished, r.CI, r.Check, r.Tokens, r.CostUSD,
					r.Buckets[bSource].Calls, r.Error)
			}(task, rep)
		}
	}
	wg.Wait()
	return writeSummary(c.out)
}

// prepareExport writes HEAD as the published module would see it, so
// the agent reads committed files and nothing a working tree happens to
// hold, then builds the CLI from it and makes the export read-only: an
// agent that edits the framework to make its task pass has not used it.
func prepareExport(c config, export, binDir string) (string, error) {
	if err := os.MkdirAll(export, 0o755); err != nil {
		return "", err
	}
	archive := exec.Command("sh", "-c", `git -C "$1" archive HEAD | tar -x -C "$2"`, "sh", c.repo, export)
	if out, err := archive.CombinedOutput(); err != nil {
		return "", fmt.Errorf("exporting HEAD: %v\n%s", err, out)
	}
	if c.variant != "" {
		os.Remove(filepath.Join(export, "SKILL.md"))
		os.RemoveAll(filepath.Join(export, "skill"))
		if err := copyFile(filepath.Join(c.variant, "SKILL.md"), filepath.Join(export, "SKILL.md")); err != nil {
			return "", fmt.Errorf("the variant needs a SKILL.md: %w", err)
		}
		if _, err := os.Stat(filepath.Join(c.variant, "skill")); err == nil {
			cp := exec.Command("cp", "-R", filepath.Join(c.variant, "skill"), filepath.Join(export, "skill"))
			if out, err := cp.CombinedOutput(); err != nil {
				return "", fmt.Errorf("copying skill/: %v\n%s", err, out)
			}
		}
	}
	bin := filepath.Join(binDir, "rastrillo")
	build := exec.Command("go", "build", "-buildvcs=false", "-o", bin, "./cmd/rastrillo")
	build.Dir = export
	build.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		return "", fmt.Errorf("building the CLI: %v\n%s", err, out)
	}
	chmodTree(export, 0o222, false)
	return bin, nil
}

func runOne(c config, bin, export, taskDir, task string, rep int) (r result) {
	r = result{Variant: c.name, Task: task, Rep: rep, Started: time.Now().Format(time.RFC3339)}
	r.Buckets = map[bucket]count{}
	dir := filepath.Join(c.out, c.name, task, fmt.Sprint(rep))
	appName := strings.ReplaceAll(task, "-", "")
	app := filepath.Join(dir, appName)
	defer func() {
		if !c.keep {
			os.RemoveAll(app)
		}
	}()
	fail := func(step string, err error) result {
		r.Error = fmt.Sprintf("%s: %v", step, err)
		return r
	}
	prompt, err := os.ReadFile(filepath.Join(taskDir, task+".md"))
	if err != nil {
		return fail("reading the task", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fail("making the run directory", err)
	}
	logf, err := os.Create(filepath.Join(dir, "setup.log"))
	if err != nil {
		return fail("opening setup.log", err)
	}
	defer logf.Close()
	// The scaffold must already pass its own gate, or a failed run would
	// be the scaffold's fault and not the agent's; the setup commits it
	// so the diff afterwards is the agent's work alone.
	for _, step := range [][]string{
		{dir, bin, "new", appName},
		{app, "go", "mod", "edit", "-replace", "amadan.net/rastrillo/rastrillo=" + export},
		{app, "go", "mod", "tidy"},
		{app, "git", "init", "-q"},
		{app, "git", "add", "-A"},
		{app, "git", "-c", "user.name=skilleval", "-c", "user.email=skilleval@localhost", "-c", "core.hooksPath=/dev/null", "commit", "-qm", "scaffold"},
	} {
		cmd := exec.Command(step[1], step[2:]...)
		cmd.Dir, cmd.Stdout, cmd.Stderr = step[0], logf, logf
		cmd.Env = goEnv()
		if err := cmd.Run(); err != nil {
			return fail("setup "+strings.Join(step[1:], " "), err)
		}
	}

	transcript := filepath.Join(dir, "transcript.jsonl")
	t0 := time.Now()
	var agentErr error
	if !c.dry {
		agentErr = runAgent(c, app, transcript, filepath.Join(dir, "agent.stderr"), string(prompt)+promptSuffix)
	}
	r.AgentSec = int(time.Since(t0).Seconds())
	if f, err := os.Open(transcript); err == nil {
		roots := append([]string{export}, moduleCacheCopies()...)
		r.tally, err = tallyTranscript(f, classifier{roots: roots, app: app})
		f.Close()
		if err != nil {
			r.Error = "reading the transcript: " + err.Error()
		}
	}
	if agentErr != nil {
		r.Error = strings.TrimSpace(r.Error + " agent: " + agentErr.Error())
	}

	r.CI = grade(app, filepath.Join(dir, "ci.log"), 25*time.Minute, "make", "ci")
	r.Check = grade(app, filepath.Join(dir, "check.log"), 5*time.Minute, "sh", filepath.Join(taskDir, task+".check.sh"))
	stat := exec.Command("sh", "-c", "git add -A && git diff --cached --shortstat")
	stat.Dir = app
	if out, err := stat.Output(); err == nil {
		r.Diff = strings.TrimSpace(string(out))
	}
	return r
}

// promptSuffix holds what every task needs and none should have to say.
// It does not say where the skill is: finding it through the scaffold's
// AGENTS.md is part of what is measured.
const promptSuffix = `

Work in the current directory, a freshly scaffolded app. You are done
when the feature works and make ci passes. Do not start servers or
anything else that outlives you, and do not change files outside this
directory.
`

func runAgent(c config, app, transcript, stderr, prompt string) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	out, err := os.Create(transcript)
	if err != nil {
		return err
	}
	defer out.Close()
	errf, err := os.Create(stderr)
	if err != nil {
		return err
	}
	defer errf.Close()
	// User settings are left out so the operator's plugins, hooks and
	// MCP servers do not land in every run's context: they would cost
	// the same in every variant, but they are noise in the absolute
	// numbers and they change from week to week.
	cmd := exec.CommandContext(ctx, "claude", "-p", prompt,
		"--model", c.model,
		"--setting-sources", "project",
		"--strict-mcp-config",
		"--output-format", "stream-json", "--verbose",
		"--dangerously-skip-permissions",
		"--no-session-persistence",
		"--max-budget-usd", fmt.Sprint(c.budget))
	cmd.Dir, cmd.Stdout, cmd.Stderr = app, out, errf
	cmd.Env = agentEnv()
	return runGroup(cmd)
}

func grade(app, logPath string, limit time.Duration, name string, args ...string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	f, err := os.Create(logPath)
	if err != nil {
		return false
	}
	defer f.Close()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = app, f, f
	cmd.Env = goEnv()
	return runGroup(cmd) == nil
}

// runGroup runs the command in its own process group and kills the
// whole group when it ends, timed out or not, so a server or watcher
// the agent started does not outlive the run on a shared machine.
func runGroup(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 10 * time.Second
	if err := cmd.Start(); err != nil {
		return err
	}
	err := cmd.Wait()
	syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	return err
}

func goEnv() []string {
	// -buildvcs=false: the run directory may sit under a .git that is
	// not the app's, and VCS stamping then fails the build for nothing
	// the agent did.
	return append(os.Environ(), "GOFLAGS=-mod=mod -buildvcs=false", "CGO_ENABLED=0")
}

// agentEnv drops the operator's forge credentials: an eval agent has
// no business posting anywhere, and nothing it builds needs them.
func agentEnv() []string {
	var env []string
	for _, kv := range goEnv() {
		if strings.HasPrefix(kv, "AMADAN_") || strings.HasPrefix(kv, "CLAUDE_CODE_SESSION") {
			continue
		}
		env = append(env, kv)
	}
	return env
}

func moduleCacheCopies() []string {
	out, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		return nil
	}
	dirs, _ := filepath.Glob(filepath.Join(strings.TrimSpace(string(out)), "amadan.net", "rastrillo", "rastrillo@*"))
	return dirs
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

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func appendResult(out string, r result) error {
	f, err := os.OpenFile(filepath.Join(out, "results.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}

func readResults(out string) ([]result, error) {
	b, err := os.ReadFile(filepath.Join(out, "results.jsonl"))
	if err != nil {
		return nil, err
	}
	var rs []result
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line == "" {
			continue
		}
		var r result
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return nil, err
		}
		rs = append(rs, r)
	}
	sort.Slice(rs, func(i, j int) bool {
		a, b := rs[i], rs[j]
		if a.Task != b.Task {
			return a.Task < b.Task
		}
		if a.Variant != b.Variant {
			return a.Variant < b.Variant
		}
		return a.Rep < b.Rep
	})
	return rs, nil
}

func writeSummary(out string) error {
	rs, err := readResults(out)
	if err != nil {
		return err
	}
	var b strings.Builder
	kb := func(n int) string { return fmt.Sprintf("%.1f", float64(n)/1024) }
	fmt.Fprintf(&b, "| task | variant | rep | done | ci | check | tokens (k) | out (k) | cost | turns | min | skill KB | docs KB | source calls / KB | examples KB | go doc | web |\n")
	fmt.Fprintf(&b, "|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	yes := func(v bool) string {
		if v {
			return "yes"
		}
		return "**no**"
	}
	for _, r := range rs {
		fmt.Fprintf(&b, "| %s | %s | %d | %s | %s | %s | %d | %d | $%.2f | %d | %d | %s | %s | %d / %s | %s | %d | %d |\n",
			r.Task, r.Variant, r.Rep, yes(r.Finished), yes(r.CI), yes(r.Check),
			r.Tokens/1000, r.OutputTokens/1000, r.CostUSD, r.Turns, r.AgentSec/60,
			kb(r.Buckets[bSkill].Bytes), kb(r.Buckets[bDocs].Bytes),
			r.Buckets[bSource].Calls, kb(r.Buckets[bSource].Bytes), kb(r.Buckets[bExamples].Bytes),
			r.Buckets[bGoDoc].Calls, r.Buckets[bWeb].Calls)
	}
	for _, r := range rs {
		if r.Error == "" && len(r.SourceReads) == 0 && r.Buckets[bFWWrite].Calls == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n**%s / %s / %d**", r.Task, r.Variant, r.Rep)
		if r.Error != "" {
			fmt.Fprintf(&b, " error: %s", r.Error)
		}
		if n := r.Buckets[bFWWrite].Calls; n > 0 {
			fmt.Fprintf(&b, " — tried to write the framework %d times", n)
		}
		b.WriteString("\n")
		for _, s := range r.SourceReads {
			fmt.Fprintf(&b, "- `%s`\n", strings.ReplaceAll(s, "`", "'"))
		}
	}
	if err := os.WriteFile(filepath.Join(out, "summary.md"), []byte(b.String()), 0o644); err != nil {
		return err
	}
	fmt.Print(b.String())
	return nil
}
