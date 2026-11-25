package comprehend

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/codex"
)

func TestReadersAnswerIsTheJSONField(t *testing.T) {
	for reply, want := range map[string]string{
		`{"answer": "422"}`: "422",
		"The handler in {internal/web} returns it.\n```json\n" +
			`{"answer":"422"}` + "\n```\nThat is all.": "422",
		`{"answer": " /booked/{reference} "}`: "/booked/{reference}",
		// No JSON: the whole reply is the answer, and graded as one.
		"  303\n": "303",
	} {
		if got := answerFrom(reply); got != want {
			t.Errorf("answerFrom(%q) = %q, want %q", reply, got, want)
		}
	}
}

func TestTheReaderIsAskedOneQuestionForAJSONAnswer(t *testing.T) {
	prompt := readerPrompt(Question{ID: "remove", Kind: Choice,
		Ask:     "When the owner takes a service away, what happens?",
		Choices: []string{"they are kept", "they are deleted"}})

	for _, want := range []string{"/work/app", "/work/rastrillo",
		"When the owner takes a service away, what happens?",
		"they are kept | they are deleted", `"not built"`,
		`{"answer": "..."}`} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the prompt lacks %q:\n%s", want, prompt)
		}
	}
}

// The reader sees the app and Rastrillo, and nothing of the app's git
// history.
func TestTheReadersCopyHoldsTheAppAndRastrillo(t *testing.T) {
	appDir, rastrilloDir, scratch := t.TempDir(), t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(appDir, "main.go"), []byte("package main\n"),
		0o644)
	os.MkdirAll(filepath.Join(appDir, ".git"), 0o755)
	os.WriteFile(filepath.Join(appDir, ".git", "HEAD"), []byte("ref\n"), 0o644)
	os.WriteFile(filepath.Join(rastrilloDir, "go.mod"), []byte("module r\n"),
		0o444)

	if err := Stage(appDir, rastrilloDir, scratch); err != nil {
		t.Fatal(err)
	}

	for _, present := range []string{"app/main.go", "rastrillo/go.mod"} {
		if _, err := os.Stat(filepath.Join(scratch, present)); err != nil {
			t.Errorf("%s is not in the copy: %v", present, err)
		}
	}
	if _, err := os.Stat(filepath.Join(scratch, "app", ".git")); err == nil {
		t.Error("the app's .git is in the copy")
	}
	entries, _ := os.ReadDir(scratch)
	if len(entries) != 2 {
		t.Errorf("the copy holds %d entries, want app and rastrillo",
			len(entries))
	}
}

// fakeCodex puts a codex on PATH that runs script with the -o path in
// $out, and gives Run an auth file to copy.
func fakeCodex(t *testing.T, script string) {
	t.Helper()
	sandbox := exec.Command("bwrap", "--ro-bind", "/", "/", "true")
	if err := sandbox.Run(); err != nil {
		t.Skipf("bwrap cannot run here: %v", err)
	}
	install := t.TempDir()
	os.Mkdir(filepath.Join(install, "bin"), 0o755)
	os.WriteFile(filepath.Join(install, "bin", "codex"), []byte(`#!/bin/sh
out=
while [ $# -gt 0 ]; do
	case "$1" in -o) out=$2; shift ;; esac
	shift
done
cat > /dev/null
`+script+"\n"), 0o755)
	t.Setenv("PATH", filepath.Join(install, "bin")+":"+os.Getenv("PATH"))
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, "auth.json"), []byte(`{"fake":true}`),
		0o600)
	t.Setenv("CODEX_HOME", home)
}

func TestAReadOverItsBudgetIsMarked(t *testing.T) {
	fakeCodex(t, `echo '{"type":"item.completed","item":{"id":"i1","type":"command_execution"}}'
echo '{"type":"turn.completed","usage":{"input_tokens":150000,"cached_input_tokens":120000,"output_tokens":60000}}'
printf 'Found it.\n{"answer": "303"}' > "$out"`)
	reader := codex.Exec{Model: "gpt-test", Dir: t.TempDir(),
		Timeout: time.Minute}
	question := Question{ID: "booked", Kind: Status, Ask: "What status?"}

	over, err := Read(context.Background(), reader, question, 200000)
	if err != nil {
		t.Fatal(err)
	}
	within, err := Read(context.Background(), reader, question, 300000)
	if err != nil {
		t.Fatal(err)
	}

	want := Reading{QuestionID: "booked", Answer: "303", OverBudget: true,
		Tokens: 210000, Commands: 1}
	if over != want {
		t.Errorf("reading = %+v, want %+v", over, want)
	}
	if within.OverBudget {
		t.Errorf("210000 tokens is over a budget of 300000: %+v", within)
	}
}
