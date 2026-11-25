// Package runner takes one brief through one arm: the build prompt,
// a measurement of what got deployed, the follow-up, and another
// measurement.
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/brief"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/cleanenv"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/mcpproxy"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/measure"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/source"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/transcript"
)

type Spec struct {
	Round, Dir, Model, BenchRoot string
	Claude                       string // the claude binary; "claude" if empty
	ClaudeVersion                string // the round's; "" for its first run
	Brief                        brief.Brief
	Arm                          brief.Arm
	N                            int
	Vars                         brief.Vars
	Source                       source.Source
	ExtraHosts                   []string
	Measure                      measure.Options
	Timeout                      time.Duration     // per prompt
	Token                        string            // claude's auth; else the environment's
	TokenFile                    string            // where Token came from, denied to the agent
	DeployEnv                    map[string]string // the arm's own deploy credential, if any
	DeniedFiles                  []string          // more files the agent's tools may not read
	MCPConfig                    string            // --mcp-config; no servers if empty
	SystemPromptExtra            string            // appended after the environment notes
	MCPLog                       string            // the gopls wrapper's log; see goplsProblem
}

type Phase struct {
	Transcript transcript.Transcript
	Mapping    measure.Mapping
	Measured   measure.Measurement
}

type Outcome struct {
	Build, Followup Phase
	AgentFailures   []string
	ClaudeVersion   string
}

// ErrHarness marks a run that says nothing about the agent: the API
// failed, a usage limit hit, or the config was not clean. The caller
// retries or aborts; it never counts.
var ErrHarness = errors.New("harness failure")

// ErrUnclean is the harness failure a retry cannot fix: the config or
// the claude version is not the round's, so the round must stop.
var ErrUnclean = errors.New("not a clean run")

// CheckClean refuses a run whose config leaked in. The 2026-10-30 runs
// loaded the operator's plugins and output style, so this is checked
// on every run, against what claude itself reported loading. servers
// are the MCP servers the run was given: each must have connected, or
// the arm ran without what defines it (a harness failure, retried), and
// any other server is a leaked config.
func CheckClean(init transcript.Init, roundVersion string, servers []string) error {
	switch {
	case init.Version == "":
		return fmt.Errorf("%w: %w: no init record", ErrHarness, ErrUnclean)
	case len(init.Plugins) > 0:
		return fmt.Errorf("%w: %w: plugins loaded: %s", ErrHarness, ErrUnclean,
			strings.Join(init.Plugins, ", "))
	case init.OutputStyle != "" && init.OutputStyle != "default":
		return fmt.Errorf("%w: %w: output style %q", ErrHarness, ErrUnclean,
			init.OutputStyle)
	case roundVersion != "" && init.Version != roundVersion:
		return fmt.Errorf("%w: %w: claude %s, but the round started on %s",
			ErrHarness, ErrUnclean, init.Version, roundVersion)
	}
	status := map[string]string{}
	for _, server := range init.MCPServers {
		if !slices.Contains(servers, server.Name) {
			return fmt.Errorf("%w: %w: MCP server %s loaded, but the run "+
				"was given %v", ErrHarness, ErrUnclean, server.Name, servers)
		}
		status[server.Name] = server.Status
	}
	for _, server := range servers {
		if status[server] != "connected" {
			return fmt.Errorf("%w: MCP server %s did not connect (%q)",
				ErrHarness, server, status[server])
		}
	}
	return nil
}

// serverNames are the servers an --mcp-config names.
func serverNames(config string) []string {
	var parsed struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	json.Unmarshal([]byte(config), &parsed)
	var names []string
	for name := range parsed.MCPServers {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// settleGrace is how long after go.mod and go.sum last changed, past
// mcpproxy.DefaultSettle, the wrapper may take to notice: two of its
// one-second polls.
const settleGrace = 2 * time.Second

// goplsProblem says why the wrapper's log does not show gopls there for
// a whole session, in the app's module, or "". from is where the
// session's lines start in the log, and end is when it ended. A missing
// or unreadable log shows nothing, so it is a problem too. A module
// whose go.mod and go.sum settled too late for the wrapper to restart
// gopls in it asks for nothing.
func goplsProblem(log string, from int64, app string, end time.Time) string {
	raw, err := os.ReadFile(log)
	if err != nil {
		return fmt.Sprintf("its log cannot be read: %v", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, mcpproxy.TroubleMarker) {
			return "gopls was not there for all of it: " + line
		}
	}
	if from > int64(len(raw)) {
		from = 0
	}
	module := mcpproxy.ModuleDir(app)
	if module == "" ||
		end.Sub(lastChanged(module)) <= mcpproxy.DefaultSettle+settleGrace {
		return ""
	}
	active := ""
	for _, line := range strings.Split(string(raw[from:]), "\n") {
		if _, after, found := strings.Cut(line,
			mcpproxy.ModuleActiveMarker); found {
			if i := strings.LastIndex(after, " in "); i >= 0 {
				active = after[i+len(" in "):]
			}
		}
	}
	if active != module {
		return fmt.Sprintf("gopls never ran in the app's module, %s "+
			"(the log's last module active in this session: %q)",
			module, active)
	}
	return ""
}

// lastChanged is when the module's go.mod or go.sum last changed.
func lastChanged(module string) time.Time {
	var last time.Time
	for _, name := range []string{"go.mod", "go.sum"} {
		info, err := os.Stat(filepath.Join(module, name))
		if err == nil && info.ModTime().After(last) {
			last = info.ModTime()
		}
	}
	return last
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// Run returns the outcome, or an error wrapping ErrHarness. Anything
// the agent got wrong is in Outcome.AgentFailures, not the error.
func Run(ctx context.Context, spec Spec) (Outcome, error) {
	outcome := Outcome{ClaudeVersion: spec.ClaudeVersion}
	session, err := prepare(spec)
	if err != nil {
		// No token, an unwritable directory: a retry meets the same.
		return outcome, fmt.Errorf("%w: %w: %v", ErrHarness, ErrUnclean, err)
	}

	prompt, err := spec.Brief.Prompt(spec.Arm, spec.Vars)
	if err != nil {
		return outcome, err
	}
	build, err := session.ask(ctx, "build", prompt, "", false)
	if err != nil {
		return outcome, err
	}
	outcome.ClaudeVersion = build.Init.Version
	session.id = build.Init.SessionID
	if outcome.Build, err = session.measure(ctx, "build", build, &outcome); err != nil {
		return outcome, err
	}

	followup, err := session.ask(ctx, "followup", spec.Brief.Followup,
		session.id, false)
	if err != nil {
		return outcome, err
	}
	outcome.Followup, err = session.measure(ctx, "followup", followup, &outcome)
	return outcome, err
}

type session struct {
	spec      Spec
	app, rec  string
	settings  string
	env       []string
	appendEnv string
	id        string
}

func prepare(spec Spec) (*session, error) {
	s := &session{
		spec: spec,
		app:  filepath.Join(spec.Dir, "app"),
		rec:  filepath.Join(spec.Dir, "rec"),
	}
	config := filepath.Join(spec.Dir, "config")
	for _, dir := range []string{s.app, s.rec, config} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	// The same starting point the 2026-10-30 runs had: an empty repo
	// with a made-up identity, so commits never carry a real name.
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.name", "Demo agent"},
		{"config", "user.email", "demo@example.com"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", s.app}, args...)...).CombinedOutput(); err != nil {
			return nil, fmt.Errorf("git %v: %v: %s", args, err, out)
		}
	}
	hosts := append(append([]string(nil), spec.ExtraHosts...), spec.Source.Hosts...)
	var err error
	denied := append([]string(nil), spec.DeniedFiles...)
	if spec.TokenFile != "" {
		denied = append(denied, spec.TokenFile)
	}
	if s.settings, err = cleanenv.Config(spec.BenchRoot, config, hosts, denied); err != nil {
		return nil, err
	}
	extra := map[string]string{}
	for name, value := range spec.Source.GoEnv {
		extra[name] = value
	}
	if spec.Token != "" {
		extra["CLAUDE_CODE_OAUTH_TOKEN"] = spec.Token
	}
	for name, value := range spec.DeployEnv {
		extra[name] = value
	}
	if s.env, err = cleanenv.Env(os.Environ(), config, extra); err != nil {
		return nil, err
	}
	if s.appendEnv, err = brief.Env(spec.BenchRoot, spec.Vars); err != nil {
		return s, err
	}
	if spec.SystemPromptExtra != "" {
		s.appendEnv += "\n\n" + spec.SystemPromptExtra
	}
	return s, nil
}

// ask runs one prompt. resume continues a session; fork asks a side
// question whose turns never enter the session the follow-up resumes.
func (s *session) ask(
	ctx context.Context, name, prompt, resume string, fork bool,
) (transcript.Transcript, error) {
	ctx, cancel := context.WithTimeout(ctx, s.spec.Timeout)
	defer cancel()
	args := []string{"-p", prompt,
		"--model", s.spec.Model,
		"--output-format", "stream-json", "--verbose",
		"--dangerously-skip-permissions", "--disable-slash-commands",
		"--strict-mcp-config", "--mcp-config", s.mcpConfig(),
		"--settings", s.settings,
		"--append-system-prompt", s.appendEnv,
	}
	if resume != "" {
		args = append(args, "--resume", resume)
	}
	if fork {
		// The side question may read but not act: with Bash or Write,
		// the agent could fix or redeploy its app while answering, and
		// that work would never be billed to the build. With no tools
		// at all, some sessions answered with nothing visible, three
		// times running, wanting to look their own URLs up.
		args = append(args, "--fork-session", "--tools", "Read,Grep,Glob")
	}
	claude := s.spec.Claude
	if claude == "" {
		claude = "claude"
	}
	out, err := os.Create(filepath.Join(s.rec, name+".jsonl"))
	if err != nil {
		return transcript.Transcript{}, fmt.Errorf("%w: %v", ErrHarness, err)
	}
	defer out.Close()
	stderr, _ := os.Create(filepath.Join(s.rec, name+".stderr"))
	defer stderr.Close()
	command := exec.CommandContext(ctx, claude, args...)
	command.Dir, command.Env = s.app, s.env
	command.Stdout, command.Stderr = out, stderr
	command.Stdin = nil
	// A timeout must stop what the agent started too (a dev server, a
	// build, a deploy), not only claude, or a retry starts beside it.
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	command.WaitDelay = 10 * time.Second
	logFrom := fileSize(s.spec.MCPLog)
	exitErr := command.Run()
	ended := time.Now()

	if _, err := out.Seek(0, 0); err != nil {
		return transcript.Transcript{}, fmt.Errorf("%w: %v", ErrHarness, err)
	}
	run, err := transcript.Parse(out)
	if err != nil {
		return run, fmt.Errorf("%w: %s: %v", ErrHarness, name, err)
	}
	if err := harnessProblem(name, run, exitErr); err != nil {
		return run, err
	}
	// Every invocation, not only the build: claude can update itself
	// between prompts, and the agent can write project settings.
	if err := CheckClean(run.Init, s.spec.ClaudeVersion,
		serverNames(s.mcpConfig())); err != nil {
		return run, fmt.Errorf("%s: %w", name, err)
	}
	if s.spec.MCPLog != "" {
		problem := goplsProblem(s.spec.MCPLog, logFrom, s.app, ended)
		if problem != "" {
			return run, fmt.Errorf("%w: %s: %s", ErrHarness, name, problem)
		}
	}
	return run, nil
}

// noServers is the MCP config of every arm that names none.
const noServers = `{"mcpServers":{}}`

func (s *session) mcpConfig() string {
	if s.spec.MCPConfig == "" {
		return noServers
	}
	return s.spec.MCPConfig
}

// harnessProblem separates a run that failed for reasons outside the
// agent from one the agent simply did badly. A timeout is the agent's:
// it was given the same hour as everyone else.
func harnessProblem(name string, run transcript.Transcript, exitErr error) error {
	switch {
	case run.Init.Version == "":
		return fmt.Errorf("%w: %s: claude wrote no init record (%v)",
			ErrHarness, name, exitErr)
	case run.Result != nil && run.Result.APIError != "":
		return fmt.Errorf("%w: %s: API error %s", ErrHarness, name,
			run.Result.APIError)
	case run.Result != nil && strings.Contains(run.Result.Subtype, "limit"):
		return fmt.Errorf("%w: %s: %s", ErrHarness, name, run.Result.Subtype)
	case run.Result == nil && toolCalls(run) == 0:
		return fmt.Errorf("%w: %s: claude stopped before its first tool call (%v)",
			ErrHarness, name, exitErr)
	}
	return nil
}

func toolCalls(run transcript.Transcript) int {
	calls := 0
	for _, message := range run.Messages {
		calls += len(message.Tools)
	}
	return calls
}

// measuring lets one measurement run at a time across every worker:
// Lighthouse's numbers drift when two audits share the machine.
var measuring sync.Mutex

func (s *session) measure(
	ctx context.Context, name string, run transcript.Transcript,
	outcome *Outcome,
) (Phase, error) {
	phase := Phase{Transcript: run}
	if run.Result == nil || run.Result.IsError {
		outcome.AgentFailures = append(outcome.AgentFailures,
			name+": the agent did not finish")
	}
	var roles []string
	var measured []measure.Role
	for _, page := range s.spec.Brief.Pages {
		roles = append(roles, page.Role)
		measured = append(measured,
			measure.Role{Name: page.Role, SignedIn: page.SignedIn})
	}
	question, err := brief.Mapping(s.spec.BenchRoot, s.spec.Brief)
	if err != nil {
		return phase, err
	}
	answer, err := s.askWithRetries(ctx, "map-"+name, question)
	if err != nil {
		return phase, err
	}
	mapping, failures := measure.ParseMapping(answer.Result.Text, roles)
	phase.Mapping = mapping
	for _, failure := range failures {
		outcome.AgentFailures = append(outcome.AgentFailures, name+": "+failure)
	}
	options := s.spec.Measure
	options.ShotsDir = filepath.Join(s.rec, "shots", name)
	measuring.Lock()
	phase.Measured, err = measure.Measure(ctx, options, mapping, measured)
	measuring.Unlock()
	if err != nil {
		return phase, fmt.Errorf("%w: %s: %v", ErrHarness, name, err)
	}
	for _, page := range phase.Measured.Pages {
		if page.Failure != "" {
			outcome.AgentFailures = append(outcome.AgentFailures,
				fmt.Sprintf("%s: %s: %s", name, page.Role, page.Failure))
		}
	}
	if !phase.Measured.DeployOK {
		outcome.AgentFailures = append(outcome.AgentFailures,
			name+": the deploy does not work")
	}
	return phase, nil
}

// askWithRetries asks the mapping question up to three times when the
// harness fails or the answer comes back empty, so an API blip costs a retry of one cheap question
// rather than a paid build. A run that never answers is the agent's.
func (s *session) askWithRetries(
	ctx context.Context, name, question string,
) (transcript.Transcript, error) {
	var answer transcript.Transcript
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		answer, err = s.ask(ctx, fmt.Sprintf("%s-%d", name, attempt),
			question, s.id, true)
		// An empty answer (thinking, no visible text) is the bench's
		// question failing, not the agent's app: ask again.
		if err == nil && answer.Result != nil &&
			strings.TrimSpace(answer.Result.Text) == "" {
			continue
		}
		if err == nil || errors.Is(err, ErrUnclean) || !errors.Is(err, ErrHarness) {
			break
		}
	}
	if err == nil && (answer.Result == nil ||
		strings.TrimSpace(answer.Result.Text) == "") {
		return answer, fmt.Errorf("%w: %s: three empty answers", ErrHarness, name)
	}
	return answer, err
}

// Ask puts one question to a fresh session under the same clean config
// as a run, for the platform poll.
func Ask(ctx context.Context, spec Spec, name, prompt string) (transcript.Transcript, error) {
	session, err := prepare(spec)
	if err != nil {
		return transcript.Transcript{}, fmt.Errorf("%w: %v", ErrHarness, err)
	}
	return session.ask(ctx, name, prompt, "", false)
}
