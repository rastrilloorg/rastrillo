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
		err := CheckClean(c.init, c.round)
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
echo '{"type":"system","subtype":"init","session_id":"s1","claude_code_version":"9.9.9","output_style":"default","plugins":'"$plugins"'}'
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
