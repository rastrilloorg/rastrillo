package comprehend

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/transcript"
)

func TestKeysParseFromTheKeyMakersAnswer(t *testing.T) {
	answer := "I read the handlers and ran the app. Here are the keys " +
		"[as asked]:\n\n```json\n" + `[
  {"id": "signed-out", "answer": "303", "settled": true,
   "evidence": "$ curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:20311/owner\n303"},
  {"id": "export", "answer": "not built", "settled": true, "missing": true,
   "evidence": "no route [anywhere] serves an export"}
]` + "\n```\n\nThe server is stopped."

	keys, err := parseKeys(answer)
	if err != nil {
		t.Fatal(err)
	}

	if len(keys) != 2 {
		t.Fatalf("keys = %+v", keys)
	}
	if keys[0].ID != "signed-out" || keys[0].Answer != "303" ||
		!keys[0].Settled || !strings.HasPrefix(keys[0].Evidence, "$ curl") {
		t.Errorf("first key = %+v", keys[0])
	}
	if keys[1].ID != "export" || !keys[1].Missing {
		t.Errorf("second key = %+v", keys[1])
	}
	if _, err := parseKeys("I could not read the app."); err == nil {
		t.Error("an answer with no JSON array parsed")
	}
}

// A key-maker that left work running in the background answers again
// when it ends; the keys are in the earlier answer, not the last.
func TestKeysAreFoundInAnEarlierAnswer(t *testing.T) {
	run := transcript.Transcript{
		Messages: []transcript.Message{
			{Text: "Reading the code."},
			{Text: "All ten settled:\n" + dogAnswer},
			{Text: "The background server has stopped too."},
		},
		Result: &transcript.Result{Text: "The background server has stopped too."},
	}

	keys, err := keysFrom(run)
	if err != nil {
		t.Fatal(err)
	}

	if len(keys) != 1 || keys[0].Answer != "dog_name" {
		t.Errorf("keys = %+v", keys)
	}
}

var yesNo = Question{ID: "own-cancel", Kind: YesNo, Ask: "Can a customer cancel?"}

func TestAnUnsettledKeyIsKeptAndMarked(t *testing.T) {
	asked := []Question{yesNo,
		{ID: "dog", Kind: Word, Ask: "Which field holds the dog's name?"}}
	answered := []Key{{ID: "own-cancel", Answer: "yes",
		Evidence: "two handlers disagree", Settled: false}}

	keys := settle(asked, answered, session{before: "h", after: "h"})

	if len(keys) != 2 {
		t.Fatalf("keys = %+v, want one per question", keys)
	}
	for _, key := range keys {
		if key.Settled {
			t.Errorf("%s is settled: %+v", key.ID, key)
		}
		if key.SourceHash != "h" {
			t.Errorf("%s has source hash %q", key.ID, key.SourceHash)
		}
	}
	if keys[0].Answer != "yes" || keys[0].Evidence != "two handlers disagree" {
		t.Errorf("the key-maker's unsettled answer was not kept: %+v", keys[0])
	}
	if keys[1].ID != "dog" || keys[1].Evidence == "" {
		t.Errorf("a question the key-maker skipped is not marked: %+v", keys[1])
	}
}

func TestABehaviouralKeyWithoutCommandAndOutputIsUnsettled(t *testing.T) {
	question := Question{ID: "signed-out", Kind: Status, Behavioural: true,
		Ask: "What status does a visitor get?"}
	request := "curl -s -o /dev/null -w '%{http_code}' " +
		"http://127.0.0.1:20311/owner"
	ran := session{before: "h", after: "h", builds: true, calls: []call{
		{"grep -n Redirect internal/app/routes.go", "31: 303"},
		{"./app -addr 127.0.0.1:20311 & sleep 1\n" + request + "; echo\nkill %1",
			"303\n"},
		{"cat > /tmp/check.sh <<'EOF'\ncurl -s http://127.0.0.1:20311/\nEOF",
			""},
		{"curl -s http://127.0.0.1:20311/gone", "curl: (7) Failed to connect"},
		{"curl -s http://127.0.0.1:20311/signin | grep -n owner",
			"60:<p>Sign in as <code>owner</code> with password wag.</p>"},
	}}
	for name, test := range map[string]struct {
		evidence string
		settled  bool
	}{
		"read, not run":   {"internal/app/routes.go:31 redirects to /signin", false},
		"no output":       {"$ " + request, false},
		"never run":       {"$ curl -s http://127.0.0.1:20311/owner\n303", false},
		"part of a call":  {"$ curl -s -o /dev/null\n303", false},
		"a bare curl":     {"$ curl\n303", false},
		"not a request":   {"$ grep -n Redirect internal/app/routes.go\n31: 303", false},
		"a script's text": {"$ curl -s http://127.0.0.1:20311/\n303", false},
		"a request and its output": {"Signed out:\n$ " +
			strings.ReplaceAll(request, " ", "  ") + "\n303\n", true},
		"the whole line": {"$ ./app -addr 127.0.0.1:20311 & sleep 1\n" +
			"started", false},
		"output it did not print": {"$ " + request + "\n200", false},
		"a failed request": {"$ curl -s http://127.0.0.1:20311/gone\n" +
			"HTTP/1.1 303 See Other", false},
		"output and a note": {"$ " + request + "\n303\n" +
			"(registered in routes.go:31)", true},
		"output cut short": {"$ curl -s http://127.0.0.1:20311/signin | " +
			"grep -n owner\n60:...as <code>owner</code>...", true},
		"output cut short, and wrong": {"$ curl -s " +
			"http://127.0.0.1:20311/signin | grep -n owner\n" +
			"60:...as <code>admin</code>...", false},
	} {
		keys := settle([]Question{question}, []Key{{ID: "signed-out",
			Answer: "303", Evidence: test.evidence, Settled: true}}, ran)
		if keys[0].Settled != test.settled {
			t.Errorf("%s: settled = %v, want %v (%+v)", name, keys[0].Settled,
				test.settled, keys[0])
		}
	}
}

func TestAChoiceKeyOutsideItsChoicesIsUnsettled(t *testing.T) {
	question := Question{ID: "removed", Kind: Choice, Ask: "What happens?",
		Choices: []string{"they are kept", "they are deleted"}}
	for answer, settled := range map[string]bool{
		"They are kept": true, "they vanish": false,
	} {
		keys := settle([]Question{question}, []Key{{ID: "removed",
			Answer: answer, Evidence: "x.go:1", Settled: true}},
			session{before: "h", after: "h"})
		if keys[0].Settled != settled {
			t.Errorf("%q: settled = %v, want %v", answer, keys[0].Settled, settled)
		}
	}
}

func TestAMissingKeyAnswersNotBuilt(t *testing.T) {
	keys := settle([]Question{yesNo}, []Key{{ID: "own-cancel",
		Answer: "There is no cancel button", Evidence: "no handler", Settled: true,
		Missing: true}}, session{before: "h", after: "h"})

	if !keys[0].Settled || !keys[0].Missing || keys[0].Answer != NotBuilt {
		t.Errorf("key = %+v", keys[0])
	}
}

// Keys made before their output was checked against what the request
// printed are held to that check when read again.
func TestRecheckUnsettlesAKeyItsTranscriptDoesNotBearOut(t *testing.T) {
	asked := []Question{
		{ID: "shown", Kind: Status, Behavioural: true, Ask: "Status?"},
		{ID: "invented", Kind: Status, Behavioural: true, Ask: "Status?"},
		{ID: "read", Kind: Word, Ask: "Which field?"},
	}
	request := "curl -sI http://127.0.0.1:20311/owner"
	run := transcript.Transcript{Messages: []transcript.Message{{
		Tools: []transcript.Tool{{Name: "Bash",
			Input:  map[string]any{"command": request},
			Result: "curl: (7) Failed to connect"}},
	}, {
		Tools: []transcript.Tool{{Name: "Bash",
			Input:  map[string]any{"command": request + " -L"},
			Result: "HTTP/1.1 303 See Other"}},
	}}}
	made := []Key{
		{ID: "shown", Answer: "303", Settled: true,
			Evidence: "$ " + request + " -L\nHTTP/1.1 303 See Other"},
		{ID: "invented", Answer: "303", Settled: true,
			Evidence: "$ " + request + "\nHTTP/1.1 303 See Other"},
		{ID: "read", Answer: "dog_name", Settled: true, Evidence: "book.go:3"},
	}

	keys := Recheck(asked, made, run)

	if !keys[0].Settled || keys[0] != made[0] {
		t.Errorf("a key its request bears out changed: %+v", keys[0])
	}
	if keys[1].Settled || !strings.Contains(keys[1].Evidence, "really printed") {
		t.Errorf("a key with output its request never printed: %+v", keys[1])
	}
	if !keys[2].Settled {
		t.Errorf("a key that is not behavioural was unsettled: %+v", keys[2])
	}
	if !made[1].Settled {
		t.Error("Recheck changed the keys it was given")
	}
}

// fakeClaude puts a claude on PATH that prints a stream-json session
// answering with answer, after running script in its working directory;
// ran is the one Bash command its transcript shows.
func fakeClaude(t *testing.T, script, ran, answer string) string {
	t.Helper()
	bin := t.TempDir()
	seen := filepath.Join(t.TempDir(), "seen")
	var lines []string
	for _, event := range []any{
		map[string]any{"type": "system", "subtype": "init", "session_id": "s",
			"claude_code_version": "9.9.9", "model": KeyModel,
			"output_style": "default", "plugins": []any{}},
		map[string]any{"type": "assistant", "message": map[string]any{
			"id": "m1", "content": []any{map[string]any{"type": "tool_use",
				"id": "t1", "name": "Bash",
				"input": map[string]any{"command": ran}}}}},
		map[string]any{"type": "user", "message": map[string]any{
			"content": []any{map[string]any{"type": "tool_result",
				"tool_use_id": "t1", "content": fakeOutput}}}},
		map[string]any{"type": "result", "subtype": "success",
			"num_turns": 1, "result": answer},
	} {
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, string(encoded))
	}
	events := filepath.Join(bin, "events.jsonl")
	os.WriteFile(events, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\n"+
		"{ pwd; env; printf '%s\\n' \"$@\"; } > "+seen+"\n"+
		script+"\ncat "+events+"\n"), 0o755)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	return seen
}

const mainGo = "package main\n\nfunc main() {}\n"

// fakeOutput is what the fake key-maker's one command printed.
const fakeOutput = "HTTP/1.1 303 See Other\n"

func keySetup(t *testing.T) KeySetup {
	app := t.TempDir()
	os.WriteFile(filepath.Join(app, "main.go"), []byte(mainGo), 0o644)
	os.WriteFile(filepath.Join(app, "go.mod"), []byte("module groomer\n"), 0o644)
	return KeySetup{AppDir: app, RastrilloDir: t.TempDir(), Token: "test-token",
		BenchRoot: "../..", Scratch: t.TempDir()}
}

var dogQuestion = Question{ID: "dog", Kind: Word, Ask: "Which field?"}

const dogAnswer = `[{"id":"dog","answer":"dog_name","evidence":"book.html:12",` +
	`"settled":true}]`

func TestKeysAreUnsettledWhenTheSourceChanged(t *testing.T) {
	for name, test := range map[string]struct {
		script  string
		settled bool
	}{
		"untouched":            {"go_build_output=1", true},
		"a database is made":   {"echo data > groomer.db", true},
		"a source file edited": {"echo '// fixed' >> main.go", false},
	} {
		t.Run(name, func(t *testing.T) {
			fakeClaude(t, test.script, "go test ./...", dogAnswer)
			setup := keySetup(t)
			before, err := SourceHash(setup.AppDir)
			if err != nil {
				t.Fatal(err)
			}

			keys, err := MakeKeys(context.Background(), setup,
				[]Question{dogQuestion})
			if err != nil {
				t.Fatal(err)
			}

			if keys[0].Settled != test.settled {
				t.Errorf("settled = %v, want %v: %+v", keys[0].Settled,
					test.settled, keys[0])
			}
			if keys[0].SourceHash != before {
				t.Errorf("source hash %q, want the app's %q",
					keys[0].SourceHash, before)
			}
			raw, _ := os.ReadFile(filepath.Join(setup.AppDir, "main.go"))
			if string(raw) != mainGo {
				t.Errorf("the kept app was changed: %q", raw)
			}
		})
	}
}

func TestTheKeyMakerRunsCleanOnACopy(t *testing.T) {
	seen := fakeClaude(t, `touch "$TMPDIR/made-by-the-key-maker"`,
		"go test ./...", dogAnswer)
	t.Setenv("GOFLAGS", "-operator-flag")
	setup := keySetup(t)

	if _, err := MakeKeys(context.Background(), setup,
		[]Question{dogQuestion}); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(seen)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	workDir, _, _ := strings.Cut(text, "\n")
	if workDir == setup.AppDir || !strings.HasPrefix(workDir, setup.Scratch) {
		t.Errorf("the key-maker ran in %s, not a copy under the scratch", workDir)
	}
	scratchTemp := filepath.Join(setup.Scratch, "tmp")
	if _, err := os.Stat(filepath.Join(scratchTemp,
		"made-by-the-key-maker")); err != nil {
		t.Errorf("the key-maker's TMPDIR is not the scratch's: %v", err)
	}
	for _, want := range []string{"CLAUDE_CODE_OAUTH_TOKEN=test-token",
		"TMPDIR=" + scratchTemp + "\n",
		"GOPROXY=off", "--strict-mcp-config", "--settings", KeyModel,
		setup.RastrilloDir} {
		if !strings.Contains(text, want) {
			t.Errorf("the key-maker's run lacks %s:\n%s", want, text)
		}
	}
	if strings.Contains(text, "-operator-flag") {
		t.Error("the operator's GOFLAGS reached the key-maker")
	}
	if _, err := os.Stat(filepath.Join(setup.Scratch, "keymaker.jsonl")); err != nil {
		t.Errorf("the key-maker's transcript is not kept: %v", err)
	}
}

const signedOutAnswer = `[{"id":"signed-out","answer":"303",` +
	`"settled":true,"evidence":"$ curl -sI http://127.0.0.1:20311/owner\n` +
	`HTTP/1.1 303 See Other"},` +
	`{"id":"dog","answer":"not built","missing":true,"settled":true,` +
	`"evidence":"$ go build ./...\n./main.go:3:15: undefined: book"}]`

// A key-maker that cannot build the app cannot have made a request: its
// behavioural keys are unsettled, never "not built", whatever it says.
func TestABehaviouralKeyOnAnAppThatDoesNotBuildIsUnsettled(t *testing.T) {
	asked := []Question{
		{ID: "signed-out", Kind: Status, Behavioural: true, Ask: "Status?"},
		{ID: "dog", Kind: Word, Behavioural: true, Ask: "Which field?"},
	}
	for name, test := range map[string]struct {
		main    string
		settled bool
	}{
		"builds":         {mainGo, true},
		"does not build": {"package main\n\nfunc main() { book() }\n", false},
	} {
		t.Run(name, func(t *testing.T) {
			fakeClaude(t, "", "go build ./...\n"+
				"curl -sI http://127.0.0.1:20311/owner", signedOutAnswer)
			setup := keySetup(t)
			os.WriteFile(filepath.Join(setup.AppDir, "main.go"),
				[]byte(test.main), 0o644)

			keys, err := MakeKeys(context.Background(), setup, asked)
			if err != nil {
				t.Fatal(err)
			}

			if keys[0].Settled != test.settled {
				t.Errorf("signed-out settled = %v, want %v: %+v",
					keys[0].Settled, test.settled, keys[0])
			}
			if keys[1].Settled || keys[1].Missing {
				t.Errorf("a build log settled a behavioural key: %+v", keys[1])
			}
			if !test.settled &&
				!strings.Contains(keys[0].Evidence, "does not build") {
				t.Errorf("the reason is not given: %q", keys[0].Evidence)
			}
		})
	}
}
