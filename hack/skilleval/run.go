package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type result struct {
	Variant    string `json:"variant"`
	Task       string `json:"task"`
	Rep        int    `json:"rep"`
	Dry        bool   `json:"dry,omitempty"`
	SkillBytes int    `json:"skill_bytes"` // SKILL.md alone: what every session pays
	TopicBytes int    `json:"topic_bytes"` // skill/*.md: what a session pays only if it reads them
	tally
	Export     string   `json:"export"`                // the paths the run used, kept so -retally classifies with them
	App        string   `json:"app"`                   // wherever the results directory has since moved
	Base       string   `json:"base"`                  // the scaffold commit every diff and check is against
	GateEdited []string `json:"gate_edited,omitempty"` // gate files the agent changed; the Makefile is restored before grading
	CI         bool     `json:"ci"`
	Check      bool     `json:"check"`
	Diff       string   `json:"diff"`
	Error      string   `json:"error,omitempty"`
	Started    string   `json:"started"`
	AgentSec   int      `json:"agent_sec"`
}

func runOne(ctx context.Context, c config, bin, export, vdir, tasksDir, task string, rep int, stale []string) (r result) {
	r = result{Variant: c.name, Task: task, Rep: rep, Dry: c.dry, Started: time.Now().Format(time.RFC3339)}
	r.Buckets, r.Touched = map[bucket]count{}, map[bucket]int{}
	fail := func(step string, err error) result {
		r.Error = fmt.Sprintf("%s: %v", step, err)
		return r
	}
	taskDir := filepath.Join(vdir, task)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		return fail("making the task directory", err)
	}
	// A fresh directory per run, created here, is what makes deleting
	// the app inside it afterwards safe.
	dir := filepath.Join(taskDir, fmt.Sprint(rep))
	if err := os.Mkdir(dir, 0o755); err != nil {
		return fail("making the run directory", err)
	}
	defer reap(dir)
	appName := strings.ReplaceAll(task, "-", "")
	app := filepath.Join(dir, appName)
	r.Export, r.App = export, app
	keepApp := c.keep
	defer func() {
		if !keepApp {
			os.RemoveAll(app)
		}
	}()
	prompt, err := os.ReadFile(filepath.Join(tasksDir, task+".md"))
	if err != nil {
		return fail("reading the task", err)
	}
	logf, err := os.Create(filepath.Join(dir, "setup.log"))
	if err != nil {
		return fail("opening setup.log", err)
	}
	defer logf.Close()
	if _, err := scaffold(ctx, bin, export, dir, appName, logf); err != nil {
		return fail("setup", err)
	}
	base, err := gitOut(ctx, app, "rev-parse", "HEAD")
	if err != nil {
		return fail("reading the scaffold commit", err)
	}
	r.Base = base

	if !c.dry {
		transcript := filepath.Join(dir, "transcript.jsonl")
		t0 := time.Now()
		agentErr := runAgent(ctx, c, dir, app, transcript, filepath.Join(dir, "agent.stderr"), string(prompt)+promptSuffix)
		r.AgentSec = int(time.Since(t0).Seconds())
		reap(dir) // whatever the agent left running must not share the machine with the grade
		if t, err := tallyFile(transcript, export, app, stale); err == nil {
			r.tally = t
		} else {
			r.Error = "reading the transcript: " + err.Error()
		}
		if agentErr != nil {
			r.Error = strings.TrimSpace(r.Error + " agent: " + agentErr.Error())
		}
	}

	// The evidence is saved before grading touches anything: the whole
	// change against the scaffold commit, whether or not the agent
	// committed, so it survives the app's deletion. It gets its own
	// deadline because an interrupt has already cancelled ctx, and that
	// is exactly when the work is most worth keeping; if it cannot be
	// saved, the app is kept instead.
	fctx, fcancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer fcancel()
	patch, err := gitRaw(fctx, app, "add", "-A")
	if err == nil {
		patch, err = gitRaw(fctx, app, "diff", "--cached", "--binary", base)
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "final.patch"), patch, 0o644)
	}
	if err != nil {
		keepApp = true
		r.Error = strings.TrimSpace(r.Error + " saving final.patch (the app is kept): " + err.Error())
	}
	r.Diff, _ = gitOut(fctx, app, "diff", "--cached", "--shortstat", base)
	if edited, err := gitOut(fctx, app, "diff", "--cached", "--name-only", base, "--", "Makefile", "GNUmakefile", "makefile", ".amadan", ".rastrillo"); err == nil && edited != "" {
		r.GateEdited = strings.Split(edited, "\n")
	}
	if ctx.Err() != nil {
		return fail("interrupted before grading", ctx.Err())
	}
	// The gate is the scaffold's, not whatever the agent made of it: its
	// Makefile is restored and named with -f, so an added GNUmakefile
	// cannot stand in for it. A gate that cannot be restored is a failed
	// run, not a pass. Only the Makefile is restored: a new package
	// legitimately adds a budget line, and GateEdited records the rest
	// for a reader to judge.
	if _, err := gitRaw(ctx, app, "checkout", base, "--", "Makefile"); err != nil {
		return fail("restoring the scaffold's Makefile", err)
	}
	if out, err := gitRaw(ctx, app, "diff", "--quiet", base, "--", "Makefile"); err != nil {
		return fail("the restored Makefile differs from the scaffold's", fmt.Errorf("%v %s", err, out))
	}

	r.CI = grade(ctx, dir, app, filepath.Join(dir, "ci.log"), 25*time.Minute, nil, "make", "-f", "Makefile", "ci")
	reap(dir)
	r.Check = grade(ctx, dir, app, filepath.Join(dir, "check.log"), 5*time.Minute, []string{"SKILLEVAL_BASE=" + base}, "sh", filepath.Join(tasksDir, task+".check.sh"))
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

func runAgent(ctx context.Context, c config, dir, app, transcript, stderr, prompt string) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
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
	cmd.Env = append(goEnv(), runMark+dir)
	return runGroup(cmd)
}

func grade(ctx context.Context, dir, app, logPath string, limit time.Duration, extra []string, name string, args ...string) bool {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	f, err := os.Create(logPath)
	if err != nil {
		return false
	}
	defer f.Close()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = app, f, f
	cmd.Env = append(append(goEnv(), runMark+dir), extra...)
	return runGroup(cmd) == nil
}

// gitOut is for git's one-line answers; gitRaw keeps bytes as they
// are, which a patch needs (trimming its last blank context line
// corrupts the hunk).
func gitOut(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := gitRaw(ctx, dir, args...)
	return strings.TrimSpace(string(out)), err
}

func gitRaw(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return outputOf(ctx, dir, "git", args...)
}

// outputOf runs a command for its stdout under setupLimit, in its own
// process group, as every external command here is.
func outputOf(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, setupLimit)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var out strings.Builder
	cmd.Dir, cmd.Env, cmd.Stdout = dir, goEnv(), &out
	err := runGroup(cmd)
	return []byte(out.String()), err
}

// keepEnv is what the agent, the grade and the setup inherit. It is an
// allowlist because what an operator's shell exports (forge tokens, an
// SSH agent socket, cloud keys) is not the eval's to hand to agent-
// written code. HOME stays: claude finds its login there.
// Go's own variables are named one by one: a GO prefix would also pass
// GOOGLE_API_KEY and GORELEASER_GITHUB_TOKEN.
var keepEnv = []string{"PATH", "HOME", "USER", "LOGNAME", "SHELL", "TERM", "LANG", "TZ", "TMPDIR",
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy", "CLAUDE_CONFIG_DIR",
	"GOPATH", "GOROOT", "GOMODCACHE", "GOCACHE", "GOENV", "GOTOOLCHAIN", "GOPROXY", "GOSUMDB", "GOPRIVATE",
	"GONOPROXY", "GONOSUMDB", "GOINSECURE", "GOVCS", "GOTELEMETRY", "GOTMPDIR", "GOEXPERIMENT", "GODEBUG"}
var keepPrefixes = []string{"LC_", "XDG_", "ANTHROPIC_"}

func goEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		keep := false
		for _, n := range keepEnv {
			keep = keep || k == n
		}
		for _, p := range keepPrefixes {
			keep = keep || strings.HasPrefix(k, p)
		}
		if keep && k != "GOFLAGS" && k != "CGO_ENABLED" {
			env = append(env, kv)
		}
	}
	// -buildvcs=false: the run directory may sit under a .git that is
	// not the app's, and VCS stamping then fails the build for nothing
	// the agent did.
	return append(env, "GOFLAGS=-mod=mod -buildvcs=false", "CGO_ENABLED=0")
}

func tallyFile(transcript, export, app string, stale []string) (tally, error) {
	f, err := os.Open(transcript)
	if err != nil {
		return tally{}, err
	}
	defer f.Close()
	return tallyTranscript(f, classifier{roots: []string{export}, stale: stale, app: app})
}

// moduleCacheCopies is asked once per invocation, under a deadline.
func moduleCacheCopies(ctx context.Context) []string {
	out, err := outputOf(ctx, "", "go", "env", "GOMODCACHE")
	if err != nil {
		return nil
	}
	dirs, _ := filepath.Glob(filepath.Join(strings.TrimSpace(string(out)), "amadan.net", "rastrillo", "rastrillo@*"))
	return dirs
}

// provenance is everything a comparison between two variants has to
// hold equal, or at least has to be able to see differ.
type provenance struct {
	Variant    string            `json:"variant"`
	VariantDir string            `json:"variant_dir,omitempty"`
	Commit     string            `json:"commit"`
	Skill      map[string]string `json:"skill"` // sha256 of SKILL.md and each skill/ file as exported
	Tasks      map[string]string `json:"tasks"` // sha256 of each task and check
	Model      string            `json:"model"`
	BudgetUSD  float64           `json:"budget_usd"`
	Timeout    string            `json:"timeout"`
	Dry        bool              `json:"dry"`
	Claude     string            `json:"claude"`
	Go         string            `json:"go"`
	Started    string            `json:"started"`
}

func writeProvenance(ctx context.Context, c config, commit, vdir, export, tasksDir string) error {
	p := provenance{Variant: c.name, VariantDir: c.variant, Model: c.model, BudgetUSD: c.budget,
		Timeout: c.timeout.String(), Dry: c.dry, Started: time.Now().Format(time.RFC3339)}
	p.Commit = commit
	p.Skill, p.Tasks = hashTree(export, "SKILL.md", "skill"), hashTree(tasksDir, ".")
	if out, err := outputOf(ctx, "", "claude", "--version"); err == nil {
		p.Claude = strings.TrimSpace(string(out))
	}
	if out, err := outputOf(ctx, "", "go", "version"); err == nil {
		p.Go = strings.TrimSpace(string(out))
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(vdir, "provenance.json"), b, 0o644)
}

func hashTree(root string, paths ...string) map[string]string {
	sums := map[string]string{}
	for _, p := range paths {
		filepath.Walk(filepath.Join(root, p), func(f string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() {
				return nil
			}
			b, err := os.ReadFile(f)
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(root, f)
			s := sha256.Sum256(b)
			sums[rel] = hex.EncodeToString(s[:])
			return nil
		})
	}
	return sums
}
