package transcript

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestParseCountsEachMessageOnce(t *testing.T) {
	transcript := parseFixture(t, false)

	if got := len(transcript.Messages); got != 2 {
		t.Fatalf("messages = %d, want 2: one per message id", got)
	}
	first, second := transcript.Messages[0], transcript.Messages[1]
	if first.Context != 1203 {
		t.Errorf("first context = %d, want 1203", first.Context)
	}
	if first.Output != 50 {
		t.Errorf("first output = %d, want 50, counted once", first.Output)
	}
	if first.Text == "" {
		t.Error("first message lost its text block")
	}
	if len(first.Tools) != 1 || first.Tools[0].ResultLen != 40 {
		t.Errorf("first tools = %+v, want one with a 40-char result",
			first.Tools)
	}
	if len(second.Tools) != 2 {
		t.Errorf("second message has %d tools, want 2", len(second.Tools))
	}
	if second.Tools[0].ResultLen != 2 {
		t.Errorf("a list-shaped result measured %d, want 2",
			second.Tools[0].ResultLen)
	}
	if transcript.Result == nil || transcript.Result.CostUSD != 0.5 {
		t.Fatalf("result = %+v, want cost 0.5", transcript.Result)
	}
	// A resumed session's total_cost_usd includes every earlier prompt;
	// the phase's own cost is the per-model cost since the last result.
	if got := transcript.Result.PhaseCostUSD; got < 0.2999 || got > 0.3001 {
		t.Errorf("phase cost = %v, want 0.30", got)
	}
	if transcript.Result.NumTurns != 2 {
		t.Errorf("num_turns = %d, want 2", transcript.Result.NumTurns)
	}
	if transcript.Init.Version != "9.9.9" {
		t.Errorf("version = %q, want 9.9.9", transcript.Init.Version)
	}
	if transcript.Init.SessionID != "s-1" {
		t.Errorf("session id = %q, want s-1", transcript.Init.SessionID)
	}
	if transcript.Result.Text != "All done." {
		t.Errorf("result text = %q", transcript.Result.Text)
	}
	if transcript.Init.OutputStyle != "default" {
		t.Errorf("output style = %q", transcript.Init.OutputStyle)
	}
}

func TestParseWithoutResult(t *testing.T) {
	transcript := parseFixture(t, true)

	if transcript.Result != nil {
		t.Errorf("result = %+v, want nil for a run that never finished",
			transcript.Result)
	}
	if len(transcript.Messages) != 2 {
		t.Errorf("messages = %d, want 2", len(transcript.Messages))
	}
}

func parseFixture(t *testing.T, cutLastLine bool) Transcript {
	t.Helper()
	raw, err := os.ReadFile("testdata/run.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if cutLastLine {
		raw = raw[:bytes.LastIndex(bytes.TrimRight(raw, "\n"), []byte("\n"))]
	}
	transcript, err := Parse(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return transcript
}

// Claude Code ships some plugins with itself; every user has them, so
// they are recorded, not counted as the operator's config leaking in.
func TestParseSeparatesBuiltinPlugins(t *testing.T) {
	raw := `{"type":"system","subtype":"init","claude_code_version":"9","plugins":[` +
		`{"name":"cc-plugin-telemetry","path":"builtin","source":"cc-plugin-telemetry@builtin"},` +
		`{"name":"superpowers","path":"/home/x/.claude/plugins/superpowers","source":"superpowers@market"}]}` + "\n"

	transcript, err := Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(transcript.Init.Plugins, ","); got != "superpowers" {
		t.Errorf("Plugins = %s, want only the installed one", got)
	}
	if got := strings.Join(transcript.Init.BuiltinPlugins, ","); got != "cc-plugin-telemetry" {
		t.Errorf("BuiltinPlugins = %s", got)
	}
}
