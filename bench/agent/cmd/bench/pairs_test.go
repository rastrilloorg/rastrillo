package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/calibrate"
)

func TestJudgedPairsAreReadFromEveryCalibrationFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "calibration-2026-11-23.json"),
		[]byte(`{"Seed": 7, "Pairs": [{"ID": 1, "Canonical": "a | b"}],
		"Judgements": [{"PairID": 1, "Verdict": "left"}]}`), 0o644)
	os.WriteFile(filepath.Join(dir, "calibration-2026-11-24.json"),
		[]byte(`{"Seed": 7, "Pairs": [{"Pair": {"Canonical": "c | d"}},
		{"Canonical": "a | b"}]}`), 0o644)
	os.WriteFile(filepath.Join(dir, "empty.json"),
		[]byte(`{"Seed": 7, "Pairs": []}`), 0o644)

	judged, err := judgedPairs(filepath.Join(dir, "calibration-*.json"), 7)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(judged)
	if !reflect.DeepEqual(judged, []string{"a | b", "a | b", "c | d"}) {
		t.Errorf("judged = %q", judged)
	}

	for _, spec := range []string{filepath.Join(dir, "empty.json"),
		filepath.Join(dir, "nothing-*.json")} {
		if found, err := judgedPairs(spec, 7); err == nil {
			t.Errorf("%s gave %q, want an error", spec, found)
		}
	}
}

// A seed splits the runs into tune and check halves; pairs chosen to
// revise a calibration with another seed could check on tuning runs.
func TestJudgedPairsMustShareTheSeed(t *testing.T) {
	dir := t.TempDir()
	judged := filepath.Join(dir, "calibration.json")
	os.WriteFile(judged,
		[]byte(`{"Seed": 7, "Pairs": [{"Canonical": "a | b"}]}`), 0o644)
	unseeded := filepath.Join(dir, "unseeded.json")
	os.WriteFile(unseeded, []byte(`{"Pairs": [{"Canonical": "a | b"}]}`),
		0o644)

	if _, err := judgedPairs(judged, 7); err != nil {
		t.Errorf("the same seed was refused: %v", err)
	}
	if _, err := judgedPairs(judged, 8); err == nil {
		t.Error("another seed was accepted")
	}
	if _, err := judgedPairs(unseeded, 7); err == nil {
		t.Error("a file recording no seed was accepted")
	}
}

func TestJobsFoundBeforeAreReusedOnlyForTheSameJobsPromptAndSource(
	t *testing.T,
) {
	made := jobFile{Model: "opus", JobsHash: "jobs", PromptHash: "prompt",
		SourceHash: "source", CostUSD: 0.42,
		Jobs: map[string][]string{"booking": {"a.go:book"}}}
	raw, err := json.Marshal(made)
	if err != nil {
		t.Fatal(err)
	}

	before, err := jobsMadeBefore(raw, jobFile{Model: "opus",
		JobsHash: "jobs", PromptHash: "prompt", SourceHash: "source"})
	if err != nil || !reflect.DeepEqual(before.Jobs, made.Jobs) {
		t.Errorf("jobs = %v, %v; want %v", before.Jobs, err, made.Jobs)
	}
	if before.CostUSD != 0.42 {
		t.Errorf("cost = %v, want the $0.42 it cost when made",
			before.CostUSD)
	}
	for name, want := range map[string]jobFile{
		"other jobs": {Model: "opus", JobsHash: "new", PromptHash: "prompt",
			SourceHash: "source"},
		"other prompt": {Model: "opus", JobsHash: "jobs", PromptHash: "new",
			SourceHash: "source"},
		"other source": {Model: "opus", JobsHash: "jobs",
			PromptHash: "prompt", SourceHash: "new"},
		"other model": {Model: "sonnet", JobsHash: "jobs",
			PromptHash: "prompt", SourceHash: "source"},
	} {
		if _, err := jobsMadeBefore(raw, want); err == nil {
			t.Errorf("%s: reused", name)
		}
	}
}

func TestTooFewPairsForASetIsAnError(t *testing.T) {
	var pairs []calibrate.Pair
	for set, count := range map[string]int{"tune": 15, "check": 15,
		"spare": 10} {
		for range count {
			pairs = append(pairs, calibrate.Pair{Set: set})
		}
	}
	if err := checkSets(pairs); err != nil {
		t.Errorf("a full choice failed: %v", err)
	}
	if err := checkSets(pairs[1:]); err == nil {
		t.Error("a set one pair short passed")
	}
}

// sleepingClaude puts a claude on PATH that starts a long sleep, writes
// the sleep's process ID to the file it returns, and waits.
func sleepingClaude(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	pidFile := filepath.Join(t.TempDir(), "pid")
	script := "#!/bin/sh\nsleep 300 &\necho $! > " + pidFile + ".writing\n" +
		"mv " + pidFile + ".writing " + pidFile + "\nwait\n"
	os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	return pidFile
}

// interruptedApp is a kept app on Rastrillo v0.0.1, which a module
// cache made for the test holds, with TMPDIR emptied so that scratch
// left behind shows.
func interruptedApp(t *testing.T) (app, temporary string) {
	t.Helper()
	app = t.TempDir()
	os.WriteFile(filepath.Join(app, "go.mod"), []byte("module groomer\n\n"+
		"require amadan.net/rastrillo/rastrillo v0.0.1\n"), 0o644)
	os.WriteFile(filepath.Join(app, "main.go"),
		[]byte("package main\n\nfunc main() {}\n"), 0o644)
	modules := t.TempDir()
	os.MkdirAll(filepath.Join(modules, "amadan.net", "rastrillo",
		"rastrillo@v0.0.1"), 0o755)
	t.Setenv("GOMODCACHE", modules)
	temporary = t.TempDir()
	t.Setenv("TMPDIR", temporary)
	return app, temporary
}

// interrupt cancels the context once the sleeping claude has started,
// runs session, and checks that it returned, that claude's children
// were stopped and that no scratch was left.
func interrupt(t *testing.T, temporary string, session func(
	context.Context) error) {
	t.Helper()
	pidFile := sleepingClaude(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for {
			if _, err := os.Stat(pidFile); err == nil {
				cancel()
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	done := make(chan error, 1)
	go func() { done <- session(ctx) }()

	select {
	case err := <-done:
		if err == nil {
			t.Error("an interrupted session reported no error")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the session did not stop when its context was cancelled")
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if !gone(pid) {
		t.Errorf("claude's child %d is still running", pid)
		syscall.Kill(pid, syscall.SIGKILL)
	}
	if left, _ := os.ReadDir(temporary); len(left) > 0 {
		t.Errorf("scratch left behind: %v", left)
	}
}

// gone waits a while for a process to exit and be reaped.
func gone(pid int) bool {
	for range 100 {
		if syscall.Kill(pid, 0) != nil {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func TestACancelledJobFinderStopsClaudeAndRemovesItsScratch(t *testing.T) {
	app, temporary := interruptedApp(t)
	finder := jobFinder{root: "../..", cache: t.TempDir(), token: "test",
		timeout: time.Hour}
	run := &pairedRun{round: "1", brief: "groomer", name: "groomer-carlos-1"}

	interrupt(t, temporary, func(ctx context.Context) error {
		_, _, err := finder.find(ctx, run,
			measuredRun{app: app, version: "v0.0.1"}, []string{"booking"})
		return err
	})
}

func TestACancelledKeyMakerStopsClaudeAndRemovesItsScratch(t *testing.T) {
	app, temporary := interruptedApp(t)
	maker := keyer{root: "../..", cache: t.TempDir(), token: "test",
		timeout: time.Hour}
	run := &keyedRun{round: "1", brief: "groomer", name: "groomer-carlos-1",
		dir: filepath.Dir(app)}
	os.Rename(app, filepath.Join(run.dir, "app"))

	interrupt(t, temporary, func(ctx context.Context) error {
		_, err := maker.key(ctx, run)
		return err
	})
}

// The pairs Paul judged were chosen while the cap could still give way;
// they never needed it to. No set of them shows an excerpt more than
// twice, and every set is full, so they stand under the hard cap.
func TestTheJudgedPairsKeepTheCap(t *testing.T) {
	raw, err := os.ReadFile("../../results/pairs-2026-11-22.json")
	if err != nil {
		t.Fatal(err)
	}
	var judged pairsFile
	if err := json.Unmarshal(raw, &judged); err != nil {
		t.Fatal(err)
	}
	if err := checkSets(judged.Pairs); err != nil {
		t.Error(err)
	}
	shown := map[string]int{}
	for _, pair := range judged.Pairs {
		for _, side := range []calibrate.Excerpt{pair.Left, pair.Right} {
			key := pair.Set + " " + side.Run + ": " + side.Job
			if shown[key]++; shown[key] > 2 {
				t.Errorf("%s is shown %d times", key, shown[key])
			}
		}
	}
}
