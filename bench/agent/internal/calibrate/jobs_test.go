package calibrate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/comprehend"
)

// fakeClaude puts a claude on PATH that records how it was called in
// the file it returns and finishes a clean session answering answer.
func fakeClaude(t *testing.T, answer string) string {
	t.Helper()
	bin := t.TempDir()
	seen := filepath.Join(t.TempDir(), "seen")
	var lines []string
	for _, event := range []any{
		map[string]any{"type": "system", "subtype": "init", "session_id": "s",
			"claude_code_version": "9.9.9", "model": comprehend.KeyModel,
			"output_style": "default", "plugins": []any{}},
		map[string]any{"type": "result", "subtype": "success",
			"num_turns": 1, "result": answer, "total_cost_usd": 0.5},
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
		"{ pwd; printf '%s\\n' \"$@\"; } > "+seen+"\ncat "+events+"\n"), 0o755)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	return seen
}

const groomerApp = `package main

func main() {}

func book() {}

func (s *Server) slots() {}

type Server struct{}
`

func jobSetup(t *testing.T) comprehend.KeySetup {
	app := t.TempDir()
	os.WriteFile(filepath.Join(app, "go.mod"), []byte("module groomer\n"),
		0o644)
	os.WriteFile(filepath.Join(app, "main.go"), []byte(groomerApp), 0o644)
	os.WriteFile(filepath.Join(app, "main_test.go"),
		[]byte("package main\n\nfunc TestBook() {}\n"), 0o644)
	return comprehend.KeySetup{AppDir: app, RastrilloDir: t.TempDir(),
		Token: "test-token", BenchRoot: "../..", Scratch: t.TempDir()}
}

var twoJobs = []string{"booking", "showing free slots", "cancelling"}

func TestJobsAreFoundAmongTheAppsFunctions(t *testing.T) {
	seen := fakeClaude(t, "Here they are:\n```json\n"+
		`[{"job": 1, "functions": ["main.go:book"]},`+
		`{"job": 2, "functions": ["main.go:Server.slots", "main.go:book"]},`+
		`{"job": 3, "functions": []}]`+"\n```")
	setup := jobSetup(t)

	jobs, err := FindJobs(context.Background(), setup, twoJobs)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string][]string{
		"booking":            {"main.go:book"},
		"showing free slots": {"main.go:Server.slots", "main.go:book"},
		"cancelling":         {},
	}
	if !reflect.DeepEqual(jobs, want) {
		t.Errorf("jobs = %#v, want %#v", jobs, want)
	}
	raw, err := os.ReadFile(seen)
	if err != nil {
		t.Fatal(err)
	}
	asked := string(raw)
	for _, want := range []string{"main.go:Server.slots", "main.go:main",
		"1. booking", setup.RastrilloDir, comprehend.KeyModel} {
		if !strings.Contains(asked, want) {
			t.Errorf("the job finder was not given %s:\n%s", want, asked)
		}
	}
	if strings.Contains(asked, "TestBook") {
		t.Error("the job finder was offered a test function")
	}
	if workDir, _, _ := strings.Cut(asked, "\n"); !strings.HasPrefix(
		workDir, setup.Scratch) {
		t.Errorf("the job finder ran in %s, not a copy in the scratch", workDir)
	}
	if _, err := os.Stat(filepath.Join(setup.Scratch,
		"jobfinder.jsonl")); err != nil {
		t.Errorf("the job finder's transcript is not kept: %v", err)
	}
}

func TestAJobAnswerNamingNoSuchFunctionIsRefused(t *testing.T) {
	for name, answer := range map[string]string{
		"an unknown function": `[{"job": 1, "functions": ["main.go:cancel"]},` +
			`{"job": 2, "functions": []}, {"job": 3, "functions": []}]`,
		"a job left out": `[{"job": 1, "functions": ["main.go:book"]},` +
			`{"job": 3, "functions": []}]`,
		"no answer": "I could not find the handlers.",
	} {
		t.Run(name, func(t *testing.T) {
			fakeClaude(t, answer)
			if jobs, err := FindJobs(context.Background(), jobSetup(t),
				twoJobs); err == nil {
				t.Errorf("accepted %v", jobs)
			}
		})
	}
}

func TestJobsAreOneALine(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "jobs.txt"),
		[]byte("booking a slot\n\n  cancelling a booking  \n"), 0o644)

	jobs, err := LoadJobs(dir)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(jobs, []string{"booking a slot",
		"cancelling a booking"}) {
		t.Errorf("jobs = %q", jobs)
	}
}
