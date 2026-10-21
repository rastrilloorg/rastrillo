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
	"sort"
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
	StaleBase  string   `json:"stale_base"`            // and on whatever machine it is read
	Base       string   `json:"base"`                  // the scaffold commit every diff and check is against
	GateEdited []string `json:"gate_edited,omitempty"` // gate files the agent changed; the Makefile is restored before grading
	CI         bool     `json:"ci"`
	Check      bool     `json:"check"`
	Diff       string   `json:"diff"`
	Error      string   `json:"error,omitempty"`
	Started    string   `json:"started"`
	AgentSec   int      `json:"agent_sec"`
}

func runOne(ctx context.Context, c config, bin, export, vdir, tasksDir, task string, rep int, staleBase string) (r result) {
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
	// Everything this run starts, the harness's own git included,
	// carries the run's mark, so whatever a hook or filter leaves behind
	// is reaped with it.
	ctx = withRun(ctx, dir)
	appName := strings.ReplaceAll(task, "-", "")
	app := filepath.Join(dir, appName)
	r.Export, r.App, r.StaleBase = export, app, staleBase
	keepApp := c.keep
	defer func() {
		// The app is deleted only if it is a child of the fresh run
		// directory, whatever a task name turned out to hold.
		if rel, ok := under(app, dir); !keepApp && ok && rel == appName {
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
	r.Base, _ = gitOut(ctx, app, "rev-parse", "HEAD") // for the record; nothing is compared through the app's git
	// The harness keeps its own copy of the scaffold, outside the app and
	// without its .git. Every diff and check compares with it, so nothing
	// the agent does to its repository (hooks, replace refs, filters,
	// index flags) can move the baseline, and the gate is this copy's
	// Makefile, not whatever the app's git can be made to report.
	scaffoldCopy, final, graded := filepath.Join(dir, "scaffold"), filepath.Join(dir, "final"), filepath.Join(dir, "graded")
	defer func() {
		if !c.keep {
			removeChild(dir, "graded")
			removeChild(dir, "scaffold")
			if !keepApp {
				removeChild(dir, "final")
			}
		}
	}()
	if err := snapshot(ctx, app, scaffoldCopy); err != nil {
		return fail("copying the scaffold", err)
	}
	trustedMakefile, err := os.ReadFile(filepath.Join(scaffoldCopy, "Makefile"))
	if err != nil {
		return fail("reading the scaffold's Makefile", err)
	}

	if !c.dry {
		transcript := filepath.Join(dir, "transcript.jsonl")
		t0 := time.Now()
		agentErr := runAgent(ctx, c, dir, app, transcript, filepath.Join(dir, "agent.stderr"), string(prompt)+promptSuffix)
		r.AgentSec = int(time.Since(t0).Seconds())
		reap(dir) // whatever the agent left running must not share the machine with the grade
		if t, err := tallyFile(transcript, export, app, staleBase); err == nil {
			r.tally = t
		} else {
			r.Error = "reading the transcript: " + err.Error()
		}
		if agentErr != nil {
			r.Error = strings.TrimSpace(r.Error + " agent: " + agentErr.Error())
		}
	}

	// The evidence is saved before grading touches anything: a snapshot
	// of the app's tree (tar, so symlinks and FIFOs are copied, never
	// followed or opened), and the patch from the harness's scaffold copy
	// to it, made without any repository or git config. The patch must
	// re-apply to the scaffold and reproduce the snapshot before the app
	// may be deleted. It gets its own deadline because an interrupt has
	// already cancelled ctx, and that is exactly when the work is most
	// worth keeping; if any of it fails, the app is kept instead.
	fctx, fcancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
	defer fcancel()
	patchPath := filepath.Join(dir, "final.patch")
	if err := snapshot(fctx, app, final); err != nil {
		// An incomplete snapshot is neither graded nor allowed to stand
		// in for the app, which is kept.
		keepApp = true
		return fail("snapshotting the app (it is kept)", err)
	}
	// The grade's copy is taken before verification, which prunes empty
	// directories from the snapshot: a test fixture may be one.
	if err := snapshot(fctx, final, graded); err != nil {
		keepApp = true
		return fail("copying the final tree to grade (the app is kept)", err)
	}
	// final.tar.gz is the whole tree as graded, empty directories and
	// all; final.patch is the readable view of it, which cannot carry an
	// empty directory, so the archive is the copy that reproduces a run.
	if err := runIn(fctx, "", nil, nil, "tar", "-C", final, "-czf", filepath.Join(dir, "final.tar.gz"), "."); err != nil {
		keepApp = true
		return fail("archiving the final tree (the app is kept)", err)
	}
	patch, err := noIndexDiff(fctx, dir, "--binary")
	if err == nil {
		err = os.WriteFile(patchPath, patch, 0o644)
	}
	if err == nil {
		err = verifyPatch(fctx, dir, patchPath)
	}
	if err != nil {
		keepApp = true
		r.Error = strings.TrimSpace(r.Error + " saving final.patch (the app is kept): " + err.Error())
	}
	if stat, err := noIndexDiff(fctx, dir, "--shortstat"); err == nil {
		r.Diff = strings.TrimSpace(string(stat))
	}
	r.GateEdited = append(gateEdited(fctx, scaffoldCopy, final), attributesEdited(scaffoldCopy, final)...)
	if ctx.Err() != nil {
		return fail("interrupted before grading", ctx.Err())
	}

	// The grade runs in that copy, with the scaffold's Makefile created
	// in it exclusively (whatever stood at that path is removed first, so
	// a symlink is not followed and a FIFO not opened) and named with -f,
	// so an added GNUmakefile cannot stand in for it. Only the Makefile
	// is restored: a new package legitimately adds a budget line, and
	// GateEdited records the rest for a reader to judge.
	mk := filepath.Join(graded, "Makefile")
	if err := os.RemoveAll(mk); err != nil {
		return fail("clearing the Makefile path", err)
	}
	f, err := os.OpenFile(mk, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err == nil {
		_, err = f.Write(trustedMakefile)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		return fail("restoring the scaffold's Makefile", err)
	}
	// The copy gets a repository of its own, made by the harness with no
	// config of the agent's, because the scaffold's gitleaks target scans
	// only a git root and would otherwise skip and pass.
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "-A", "-f"}, // every file is scanned, whatever the agent's .gitignore says
		{"-c", "user.name=skilleval", "-c", "user.email=skilleval@localhost", "commit", "-qm", "final"},
	} {
		if _, _, err := isolatedGit(ctx, graded, args...); err != nil {
			return fail("committing the graded copy", err)
		}
	}

	r.CI = grade(ctx, dir, graded, filepath.Join(dir, "ci.log"), 25*time.Minute, nil, "make", "-f", "Makefile", "ci")
	reap(dir)
	r.Check = grade(ctx, dir, graded, filepath.Join(dir, "check.log"), 5*time.Minute, []string{"SKILLEVAL_BASE_DIR=" + scaffoldCopy}, "sh", filepath.Join(tasksDir, task+".check.sh"))
	return r
}

// removeChild deletes dir/name, and only that.
func removeChild(dir, name string) {
	p := filepath.Join(dir, name)
	if rel, ok := under(p, dir); ok && rel == name {
		os.RemoveAll(p)
	}
}

// snapshot copies src's tree to a new dst, leaving out .git. tar keeps
// symlinks as symlinks and FIFOs as FIFOs, so nothing the agent planted
// is followed or opened. Both ends of the pipe are checked: a file the
// packing tar cannot read fails the snapshot, where a shell pipeline
// would report only the unpacking end and pass an incomplete copy.
func snapshot(ctx context.Context, src, dst string) error {
	if err := os.Mkdir(dst, 0o755); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, setupLimit)
	defer cancel()
	pr, pw, err := os.Pipe()
	if err != nil {
		return err
	}
	pack := exec.CommandContext(ctx, "tar", "-C", src, "--exclude=./.git", "-cf", "-", ".")
	unpack := exec.CommandContext(ctx, "tar", "-C", dst, "-xf", "-")
	var packErr, unpackErr strings.Builder
	pack.Env, pack.Stdout, pack.Stderr = envFor(ctx), pw, &packErr
	unpack.Env, unpack.Stdin, unpack.Stderr = envFor(ctx), pr, &unpackErr
	if err := startGroup(unpack); err != nil {
		pr.Close()
		pw.Close()
		return err
	}
	if err := startGroup(pack); err != nil {
		pr.Close()
		pw.Close()
		waitGroup(unpack)
		return err
	}
	pr.Close()
	pw.Close()
	perr, uerr := waitGroup(pack), waitGroup(unpack)
	if perr != nil {
		return fmt.Errorf("reading %s: %v %s", src, perr, strings.TrimSpace(packErr.String()))
	}
	if uerr != nil {
		return fmt.Errorf("writing %s: %v %s", dst, uerr, strings.TrimSpace(unpackErr.String()))
	}
	return nil
}

// isolatedGit is git with no config but what is passed, and no
// repository discovered above dir: nothing of the agent's or the
// operator's can change what a diff prints or how a patch applies.
func isolatedGit(ctx context.Context, dir string, args ...string) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, setupLimit)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.hooksPath=/dev/null"}, args...)...)
	var out, errb strings.Builder
	cmd.Dir, cmd.Stdout, cmd.Stderr = dir, &out, &errb
	cmd.Env = append(envFor(ctx), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_CEILING_DIRECTORIES="+filepath.Dir(dir), "GIT_ATTR_NOSYSTEM=1")
	err := runGroup(cmd)
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	}
	if err != nil && code == 0 {
		return nil, -1, err
	}
	if code > 1 {
		return nil, code, fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(errb.String()))
	}
	return []byte(out.String()), code, nil
}

// noIndexDiff compares dir/scaffold with dir/final. Its paths read
// a/scaffold/... and b/final/..., so the patch applies with -p2.
func noIndexDiff(ctx context.Context, dir string, args ...string) ([]byte, error) {
	a := append([]string{"diff", "--no-index", "--no-color", "--no-ext-diff", "--no-textconv", "--no-renames"}, args...)
	out, _, err := isolatedGit(ctx, dir, append(a, "--", "scaffold", "final")...)
	return out, err
}

// verifyPatch applies the patch to a fresh copy of the scaffold and
// checks that it reproduces the final snapshot.
func verifyPatch(ctx context.Context, dir, patchPath string) error {
	defer removeChild(dir, "verify")
	verify := filepath.Join(dir, "verify")
	if err := snapshot(ctx, filepath.Join(dir, "scaffold"), verify); err != nil {
		return err
	}
	if _, _, err := isolatedGit(ctx, verify, "apply", "-p2", "--allow-empty", "--whitespace=nowarn", patchPath); err != nil {
		return fmt.Errorf("the patch does not apply to the scaffold: %w", err)
	}
	// A patch carries no empty directories, so they are left out of the
	// comparison on both sides.
	if err := runIn(ctx, "", nil, nil, "find", verify, filepath.Join(dir, "final"), "-depth", "-type", "d", "-empty", "-delete"); err != nil {
		return err
	}
	if err := runIn(ctx, "", nil, nil, "diff", "-r", "--no-dereference", "-q", verify, filepath.Join(dir, "final")); err != nil {
		return fmt.Errorf("the patch does not reproduce the final tree: %w", err)
	}
	return nil
}

// gateFiles are what the scaffold's gate reads besides the code. The
// gitleaks policy files are among them: gitleaks loads them from the
// tree it scans, so one can widen what passes. .gitattributes is too,
// since -diff there hides a file's content from the scan, but it can
// sit at any depth, so attributesEdited compares every one.
var gateFiles = []string{"Makefile", "GNUmakefile", "makefile", ".amadan", ".rastrillo", ".gitleaks.toml", ".gitleaksignore"}

// attributesEdited lists the .gitattributes files, at any depth, that
// the final tree adds, removes or changes.
func attributesEdited(scaffold, final string) []string {
	collect := func(root string) map[string]string {
		m := map[string]string{}
		filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.Name() != ".gitattributes" {
				return nil
			}
			rel, _ := filepath.Rel(root, p)
			if !d.Type().IsRegular() {
				m[rel] = "\x00not a regular file" // never opened
				return nil
			}
			b, _ := os.ReadFile(p)
			m[rel] = string(b)
			return nil
		})
		return m
	}
	a, b := collect(scaffold), collect(final)
	var edited []string
	for rel, v := range b {
		if a[rel] != v {
			edited = append(edited, rel)
		}
	}
	for rel := range a {
		if _, ok := b[rel]; !ok {
			edited = append(edited, rel)
		}
	}
	sort.Strings(edited)
	return edited
}

// gateEdited lists the gate files that differ between the scaffold and
// the final tree. Anything at those paths that is not a regular file or
// a directory counts as edited without being opened.
func gateEdited(ctx context.Context, scaffold, final string) []string {
	var edited []string
	for _, name := range gateFiles {
		a, aerr := os.Lstat(filepath.Join(scaffold, name))
		b, berr := os.Lstat(filepath.Join(final, name))
		switch {
		case aerr != nil && berr != nil:
			continue
		case aerr != nil || berr != nil, a.Mode().Type() != b.Mode().Type(), !a.Mode().IsRegular() && !a.IsDir():
			edited = append(edited, name)
		default:
			if runIn(ctx, "", nil, nil, "diff", "-r", "--no-dereference", "-q", filepath.Join(scaffold, name), filepath.Join(final, name)) != nil {
				edited = append(edited, name)
			}
		}
	}
	return edited
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
	cmd.Env = envFor(ctx)
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
	cmd.Env = append(envFor(ctx), extra...)
	return runGroup(cmd) == nil
}

// gitOut is for git's one-line answers; gitRaw keeps bytes as they
// are, which a patch needs (trimming its last blank context line
// corrupts the hunk).
func gitOut(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := gitRaw(ctx, dir, args...)
	return strings.TrimSpace(string(out)), err
}

// gitRaw runs git with hooks off: the app's repository is the agent's,
// and a hook it installed must not run inside the harness's own steps.
func gitRaw(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return outputOf(ctx, dir, "git", append([]string{"-c", "core.hooksPath=/dev/null"}, args...)...)
}

// outputOf runs a command for its stdout under setupLimit, in its own
// process group, as every external command here is.
func outputOf(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, setupLimit)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var out strings.Builder
	cmd.Dir, cmd.Env, cmd.Stdout = dir, envFor(ctx), &out
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

func tallyFile(transcript, export, app, staleBase string) (tally, error) {
	f, err := os.Open(transcript)
	if err != nil {
		return tally{}, err
	}
	defer f.Close()
	return tallyTranscript(f, classifier{roots: []string{export}, staleBase: staleBase, app: app})
}

// moduleCacheBase is where every cached version of the framework sits
// (each as <base>@<version>), asked once per invocation under a deadline.
func moduleCacheBase(ctx context.Context) string {
	out, err := outputOf(ctx, "", "go", "env", "GOMODCACHE")
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return ""
	}
	return filepath.Join(strings.TrimSpace(string(out)), "amadan.net", "rastrillo", "rastrillo")
}

type runKey struct{}

// withRun marks ctx as belonging to the run in dir; envFor passes the
// mark to every command started under it.
func withRun(ctx context.Context, dir string) context.Context {
	return context.WithValue(ctx, runKey{}, dir)
}

func envFor(ctx context.Context) []string {
	env := goEnv()
	if dir, ok := ctx.Value(runKey{}).(string); ok {
		env = append(env, runMark+dir)
	}
	return env
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
