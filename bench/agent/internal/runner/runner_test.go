package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/brief"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/mcpproxy"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/transcript"
)

func TestCheckCleanRefusesPluginsAndVersionDrift(t *testing.T) {
	clean := transcript.Init{Version: "2.1.312", OutputStyle: "default"}
	cases := []struct {
		name    string
		init    transcript.Init
		round   string
		wantErr bool
	}{
		{"clean, first run", clean, "", false},
		{"clean, same version", clean, "2.1.312", false},
		{"a plugin", transcript.Init{Version: "2.1.312",
			Plugins: []string{"superpowers"}}, "2.1.312", true},
		{"an output style", transcript.Init{Version: "2.1.312",
			OutputStyle: "Concise"}, "2.1.312", true},
		{"a different claude", clean, "2.1.300", true},
		{"no init at all", transcript.Init{}, "", true},
	}
	for _, c := range cases {
		err := CheckClean(c.init, c.round, nil)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err = %v, want error %v", c.name, err, c.wantErr)
		}
		if err != nil && !(errors.Is(err, ErrUnclean) && errors.Is(err, ErrHarness)) {
			t.Errorf("%s: %v is not both unclean and a harness failure",
				c.name, err)
		}
	}
}

// fakeClaude writes a claude stand-in that answers every prompt with a
// finished run, logs its arguments, and loads a plugin when asked the
// mapping question (the forked session), as a leaked config would.
func fakeClaude(t *testing.T) (binary, argsLog string) {
	t.Helper()
	dir := t.TempDir()
	argsLog = filepath.Join(dir, "args.log")
	binary = filepath.Join(dir, "claude")
	script := `#!/bin/sh
printf '%s\n' "$@" >> "` + argsLog + `"
printf '%s\n' ---- >> "` + argsLog + `"
plugins='[]'
case " $* " in *" --fork-session "*) plugins='["leaked"]' ;; esac
servers='[]'
case "$*" in *'"gopls"'*) servers='[{"name":"gopls","status":"connected"}]' ;; esac
echo '{"type":"system","subtype":"init","session_id":"s1","claude_code_version":"9.9.9","output_style":"default","plugins":'"$plugins"',"mcp_servers":'"$servers"'}'
echo '{"type":"assistant","message":{"id":"m1","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}],"usage":{}}}'
echo '{"type":"result","subtype":"success","num_turns":1,"total_cost_usd":0.1,"result":"{}"}'
`
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return binary, argsLog
}

func TestRunChecksEveryInvocationAndMapsReadOnly(t *testing.T) {
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "test-token")
	claude, argsLog := fakeClaude(t)
	groomer, err := brief.Load("../../briefs/groomer")
	if err != nil {
		t.Fatal(err)
	}
	spec := Spec{
		Dir: t.TempDir(), Model: "m", BenchRoot: "../..",
		Claude: claude, ClaudeVersion: "9.9.9",
		Brief: groomer, Arm: brief.Carlos, N: 1,
		Vars:    brief.Vars{App: "a", SkillURL: "http://x/SKILL.md", DeployAccount: "d"},
		Timeout: time.Minute,
	}

	_, err = Run(context.Background(), spec)

	if !errors.Is(err, ErrUnclean) {
		t.Errorf("Run = %v, want ErrUnclean: the mapping session loaded a plugin", err)
	}
	logged, _ := os.ReadFile(argsLog)
	invocations := strings.Split(string(logged), "----\n")
	if len(invocations) < 2 {
		t.Fatalf("claude ran %d times, want the build and the mapping", len(invocations)-1)
	}
	mapping := invocations[1]
	// Reading tools only: with none, some sessions answered with nothing
	// visible three times running; with Bash or Write, the agent could
	// change its app while answering.
	if !strings.Contains(mapping, "--fork-session\n") || !strings.Contains(mapping, "--tools\nRead,Grep,Glob\n") {
		t.Errorf("the mapping question was not a read-only fork:\n%s", mapping)
	}
}

func TestRunWithoutATokenStopsTheRound(t *testing.T) {
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	groomer, err := brief.Load("../../briefs/groomer")
	if err != nil {
		t.Fatal(err)
	}

	_, err = Run(context.Background(), Spec{
		Dir: t.TempDir(), BenchRoot: "../..", Brief: groomer,
		Arm: brief.Carlos, Timeout: time.Minute,
	})

	if !errors.Is(err, ErrUnclean) {
		t.Errorf("Run = %v, want ErrUnclean so the round stops, not retries", err)
	}
}

// The model sometimes answers the mapping question with thinking and no
// visible text. That is the bench's own question failing, not the
// agent's build, so it is asked again rather than scored.
func TestAnEmptyMappingAnswerIsAskedAgain(t *testing.T) {
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "test-token")
	dir := t.TempDir()
	counter := filepath.Join(dir, "forks")
	claude := filepath.Join(dir, "claude")
	script := `#!/bin/sh
answer='{}'
case " $* " in *" --fork-session "*)
	echo x >> "` + counter + `"
	if [ "$(wc -l < "` + counter + `")" -lt 2 ]; then answer='""'; else answer='"{\"pages\":{}}"'; fi ;;
esac
echo '{"type":"system","subtype":"init","session_id":"s1","claude_code_version":"9.9.9","output_style":"default","plugins":[]}'
echo '{"type":"assistant","message":{"id":"m1","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}],"usage":{}}}'
echo '{"type":"result","subtype":"success","num_turns":1,"total_cost_usd":0.1,"result":'"$answer"'}'
`
	if err := os.WriteFile(claude, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	session := &session{spec: Spec{Claude: claude, ClaudeVersion: "9.9.9",
		Timeout: time.Minute}, app: dir, rec: dir}

	answer, err := session.askWithRetries(context.Background(), "map", "where?")

	if err != nil {
		t.Fatal(err)
	}
	if answer.Result.Text != `{"pages":{}}` {
		t.Errorf("answer = %q, want the second, non-empty one", answer.Result.Text)
	}
}

func TestThreeEmptyMappingAnswersAreTheBenchsFault(t *testing.T) {
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "test-token")
	dir := t.TempDir()
	claude := filepath.Join(dir, "claude")
	script := `#!/bin/sh
echo '{"type":"system","subtype":"init","session_id":"s1","claude_code_version":"9.9.9","output_style":"default","plugins":[]}'
echo '{"type":"assistant","message":{"id":"m1","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}],"usage":{}}}'
echo '{"type":"result","subtype":"success","num_turns":1,"total_cost_usd":0.1,"result":""}'
`
	os.WriteFile(claude, []byte(script), 0o755)
	session := &session{spec: Spec{Claude: claude, ClaudeVersion: "9.9.9",
		Timeout: time.Minute}, app: dir, rec: dir}

	_, err := session.askWithRetries(context.Background(), "map", "where?")

	if !errors.Is(err, ErrHarness) {
		t.Errorf("askWithRetries = %v after three empty answers, want a "+
			"harness failure, not an agent's", err)
	}
}

// Every prompt of a run, the mapping questions included, gets the
// spec's MCP config; an arm that names none gets no servers at all.
func TestOnlyTheGoplsArmGetsAnMCPServer(t *testing.T) {
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "test-token")
	smoke, err := brief.Load("../../briefs/smoke")
	if err != nil {
		t.Fatal(err)
	}
	gopls := `{"mcpServers":{"gopls":{"command":"/bin/gopls","args":["mcp"]}}}`
	for _, c := range []struct {
		arm        brief.Arm
		mcp, given string
	}{
		{brief.Carlos, "", `{"mcpServers":{}}`},
		{brief.CarlosGopls, gopls, gopls},
	} {
		claude, argsLog := fakeClaude(t)
		Run(context.Background(), Spec{
			Dir: t.TempDir(), Model: "m", BenchRoot: "../..",
			Claude: claude, ClaudeVersion: "9.9.9",
			Brief: smoke, Arm: c.arm, N: 1, MCPConfig: c.mcp,
			Vars:    brief.Vars{App: "a", SkillURL: "http://x/SKILL.md"},
			Timeout: time.Minute,
		})
		logged, _ := os.ReadFile(argsLog)
		invocations := strings.Split(strings.TrimSuffix(string(logged),
			"----\n"), "----\n")
		if len(invocations) < 2 {
			t.Fatalf("%s: claude ran %d times, want the build and a mapping",
				c.arm, len(invocations))
		}
		for _, invocation := range invocations {
			if !strings.Contains(invocation,
				"--strict-mcp-config\n--mcp-config\n"+c.given+"\n") {
				t.Errorf("%s: an invocation lacks the MCP config %s:\n%s",
					c.arm, c.given, invocation)
			}
		}
	}
}

// gopls's own instructions are appended to the gopls arm's system
// prompt, after the environment notes; carlos's is the notes alone.
func TestOnlyTheGoplsArmGetsGoplsInstructions(t *testing.T) {
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "test-token")
	smoke, err := brief.Load("../../briefs/smoke")
	if err != nil {
		t.Fatal(err)
	}
	instructions := "# The gopls MCP server\n\nUse go_workspace first."
	prompts := map[brief.Arm]string{}
	for _, c := range []struct {
		arm   brief.Arm
		extra string
	}{{brief.Carlos, ""}, {brief.CarlosGopls, instructions}} {
		claude, argsLog := fakeClaude(t)
		Run(context.Background(), Spec{
			Dir: t.TempDir(), Model: "m", BenchRoot: "../..",
			Claude: claude, ClaudeVersion: "9.9.9",
			Brief: smoke, Arm: c.arm, N: 1, SystemPromptExtra: c.extra,
			Vars:    brief.Vars{App: "a", DeployAccount: "the demo account"},
			Timeout: time.Minute,
		})
		logged, _ := os.ReadFile(argsLog)
		build := strings.Split(string(logged), "----\n")[0]
		_, after, _ := strings.Cut(build, "--append-system-prompt\n")
		prompts[c.arm] = after
	}

	if strings.Contains(prompts[brief.Carlos], "gopls") {
		t.Errorf("carlos's system prompt carries gopls's instructions:\n%s",
			prompts[brief.Carlos])
	}
	want := strings.TrimSuffix(prompts[brief.Carlos], "\n") + "\n\n" +
		instructions + "\n"
	if prompts[brief.CarlosGopls] != want {
		t.Errorf("gopls's system prompt =\n%s\nwant carlos's, then the "+
			"instructions:\n%s", prompts[brief.CarlosGopls], want)
	}
}

// A gopls run without gopls is a carlos run under the wrong label, and
// any run with a server it was not given has a leaked config.
func TestCheckCleanWantsExactlyTheGivenServers(t *testing.T) {
	gopls := []string{"gopls"}
	init := func(servers ...transcript.MCPServer) transcript.Init {
		return transcript.Init{Version: "9", OutputStyle: "default",
			MCPServers: servers}
	}
	connected := transcript.MCPServer{Name: "gopls", Status: "connected"}
	for _, c := range []struct {
		name             string
		init             transcript.Init
		want             []string
		harness, unclean bool
	}{
		{"carlos, no servers", init(), nil, false, false},
		{"gopls, connected", init(connected), gopls, false, false},
		{"gopls, failed", init(transcript.MCPServer{Name: "gopls",
			Status: "failed"}), gopls, true, false},
		{"gopls, missing", init(), gopls, true, false},
		{"carlos given gopls", init(connected), nil, true, true},
		{"gopls and another", init(connected, transcript.MCPServer{
			Name: "other", Status: "connected"}), gopls, true, true},
	} {
		err := CheckClean(c.init, "9", c.want)
		if errors.Is(err, ErrHarness) != c.harness ||
			errors.Is(err, ErrUnclean) != c.unclean {
			t.Errorf("%s: %v; want harness %v, unclean %v", c.name, err,
				c.harness, c.unclean)
		}
	}
}

// fakeClaudeWithGopls answers like fakeClaude, reporting gopls
// connected, and appends line to log on every invocation.
func fakeClaudeWithGopls(t *testing.T, log, line string) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "claude")
	script := `#!/bin/sh
[ -n "` + line + `" ] && echo "` + line + `" >> "` + log + `"
echo '{"type":"system","subtype":"init","session_id":"s1","claude_code_version":"9.9.9","output_style":"default","plugins":[],"mcp_servers":[{"name":"gopls","status":"connected"}]}'
echo '{"type":"assistant","message":{"id":"m1","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}],"usage":{}}}'
echo '{"type":"result","subtype":"success","num_turns":1,"total_cost_usd":0.1,"result":"{}"}'
`
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return binary
}

// Trouble in the wrapper's log (gopls died, or did not restart in the
// module) means gopls was not there for part of the phase: a harness
// failure, retried like an API error, never a measured run.
func TestTroubleInTheGoplsLogIsAHarnessFailure(t *testing.T) {
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "test-token")
	smoke, err := brief.Load("../../briefs/smoke")
	if err != nil {
		t.Fatal(err)
	}
	gopls := `{"mcpServers":{"gopls":{"command":"/bin/goplsmcp"}}}`
	for name, line := range map[string]string{
		"quiet":   "2026-11-22T20:36:20Z goplsmcp: started gopls in /app",
		"trouble": "2026-11-22T20:36:20Z " + mcpproxy.TroubleMarker + "gopls 1 exited",
	} {
		dir := t.TempDir()
		log := filepath.Join(dir, "rec", "goplsmcp.log")
		_, err := Run(context.Background(), Spec{
			Dir: dir, Model: "m", BenchRoot: "../..",
			Claude: fakeClaudeWithGopls(t, log, line), ClaudeVersion: "9.9.9",
			Brief: smoke, Arm: brief.CarlosGopls, N: 1, MCPConfig: gopls,
			MCPLog:  log,
			Vars:    brief.Vars{App: "a"},
			Timeout: time.Minute,
		})
		trouble := errors.Is(err, ErrHarness) && err != nil &&
			strings.Contains(err.Error(), "gopls 1 exited")
		if name == "trouble" && (!trouble || errors.Is(err, ErrUnclean)) {
			t.Errorf("trouble in the log: Run = %v, want a retried harness "+
				"failure naming it", err)
		}
		if name == "quiet" && trouble {
			t.Errorf("a quiet log: Run = %v", err)
		}
	}
}

// A gopls run counts only on the wrapper's word that gopls had the
// app's module: a log that is not there says nothing, and a module that
// settled long before the session ended must have had a gopls started
// in it during the session.
func TestAGoplsRunNeedsGoplsInTheModule(t *testing.T) {
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "test-token")
	smoke, err := brief.Load("../../briefs/smoke")
	if err != nil {
		t.Fatal(err)
	}
	gopls := `{"mcpServers":{"gopls":{"command":"/bin/goplsmcp"}}}`
	for name, test := range map[string]struct {
		line, problem string
	}{
		"no log":           {"", "log cannot be read"},
		"no module active": {"goplsmcp: started gopls in APP", "never ran in"},
		"gopls had it":     {"goplsmcp: module active: gopls 1 in APP", ""},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			app := filepath.Join(dir, "app")
			os.MkdirAll(app, 0o755)
			os.WriteFile(filepath.Join(app, "go.mod"), []byte("module a\n"),
				0o644)
			old := time.Now().Add(-time.Hour)
			os.Chtimes(filepath.Join(app, "go.mod"), old, old)
			log := filepath.Join(dir, "rec", "goplsmcp.log")
			_, err := Run(context.Background(), Spec{
				Dir: dir, Model: "m", BenchRoot: "../..",
				Claude: fakeClaudeWithGopls(t, log,
					strings.ReplaceAll(test.line, "APP", app)),
				ClaudeVersion: "9.9.9",
				Brief:         smoke, Arm: brief.CarlosGopls, N: 1,
				MCPConfig: gopls, MCPLog: log,
				Vars: brief.Vars{App: "a"}, Timeout: time.Minute,
			})

			named := err != nil && test.problem != "" &&
				strings.Contains(err.Error(), test.problem)
			if test.problem != "" && (!named || !errors.Is(err, ErrHarness)) {
				t.Errorf("Run = %v, want a harness failure saying %q", err,
					test.problem)
			}
			if test.problem == "" && err != nil &&
				(strings.Contains(err.Error(), "never ran in") ||
					strings.Contains(err.Error(), "log cannot be read")) {
				t.Errorf("Run = %v; gopls had the module", err)
			}
		})
	}
}

func TestGoplsProblems(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "app")
	module := filepath.Join(app, "groomer")
	os.MkdirAll(module, 0o755)
	os.WriteFile(filepath.Join(module, "go.mod"), []byte("module g\n"), 0o644)
	settled := time.Now()
	os.Chtimes(filepath.Join(module, "go.mod"), settled, settled)
	earlier := "goplsmcp: module active: gopls 2 in " + module + "\n"
	log := func(text string) string {
		path := filepath.Join(t.TempDir(), "goplsmcp.log")
		os.WriteFile(path, []byte(text), 0o644)
		return path
	}
	later := settled.Add(time.Minute)
	for name, test := range map[string]struct {
		log     string
		from    int64
		end     time.Time
		problem bool
	}{
		"active in this session": {log(earlier), 0, later, false},
		"active only in an earlier session": {log(earlier + "started\n"),
			int64(len(earlier)), later, true},
		"active in a module that moved away": {log(
			"goplsmcp: module active: gopls 2 in " + app + "\n"), 0, later,
			true},
		"settled too late to restart": {log("started\n"), 0,
			settled.Add(time.Second), false},
		"no log": {filepath.Join(dir, "none.log"), 0, later, true},
	} {
		got := goplsProblem(test.log, test.from, app, test.end)
		if (got != "") != test.problem {
			t.Errorf("%s: problem %q, want one: %v", name, got, test.problem)
		}
	}
	if got := goplsProblem(log("started\n"), 0, t.TempDir(), later); got != "" {
		t.Errorf("an app with no module: %q", got)
	}
}
