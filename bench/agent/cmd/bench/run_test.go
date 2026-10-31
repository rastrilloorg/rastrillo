package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/brief"
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/runner"
)

// A rerun must not deploy under the name an earlier attempt already
// used: that app may be live, and the agent would find and reuse it.
func TestSetAsideCountsEarlierAttemptsForAFreshAppName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "groomer-control-1")

	if earlier, err := setAside(dir); err != nil || earlier != 0 {
		t.Fatalf("no earlier attempt: setAside = %d, %v", earlier, err)
	}
	os.MkdirAll(dir, 0o755)
	if earlier, err := setAside(dir); err != nil || earlier != 1 {
		t.Fatalf("one earlier attempt: setAside = %d, %v", earlier, err)
	}
	os.MkdirAll(dir, 0o755)
	if earlier, err := setAside(dir); err != nil || earlier != 2 {
		t.Fatalf("two earlier attempts: setAside = %d, %v", earlier, err)
	}
	if got := appName("groomer", "1", brief.Carlos, 1, 0); got != "groomer-r1-1" {
		t.Errorf("carlos's first attempt = %s, want round 1's name", got)
	}
	if got := appName("groomer", "1", brief.Control, 1, 2); got != "groomer-r1-1-a3" {
		t.Errorf("control's third attempt = %s, want groomer-r1-1-a3", got)
	}
	// The spec arm deploys where the control does, so the two may never
	// share a name: one would build on, or overwrite, the other's app.
	if got := appName("groomer", "2", brief.Spec, 1, 0); got != "groomer-spec-r2-1" {
		t.Errorf("spec's first attempt = %s, want groomer-spec-r2-1", got)
	}
}

func TestRunWithRetriesReportsTheLastHarnessFailure(t *testing.T) {
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "test-token")
	dir := t.TempDir()
	silent := filepath.Join(dir, "claude")
	os.WriteFile(silent, []byte("#!/bin/sh\nexit 1\n"), 0o755)
	smoke, err := brief.Load("../../briefs/smoke")
	if err != nil {
		t.Fatal(err)
	}

	_, err = runWithRetries(context.Background(), runner.Spec{
		Dir: filepath.Join(dir, "run"), BenchRoot: "../..", Claude: silent,
		Brief: smoke, Arm: brief.Carlos, N: 1, Round: "t", Timeout: time.Minute,
	})

	if !errors.Is(err, runner.ErrHarness) {
		t.Errorf("runWithRetries = %v after three silent claudes, want the harness failure", err)
	}
	if earlier, _ := filepath.Glob(filepath.Join(dir, "run-attempt-*")); len(earlier) != 2 {
		t.Errorf("%d attempts set aside, want 2", len(earlier))
	}
}

func TestTheSpecArmDeploysLikeTheControl(t *testing.T) {
	control := deployVars(brief.Control, "Fly.io", "the demo org", "fly.io,*.fly.dev")
	spec := deployVars(brief.Spec, "Fly.io", "the demo org", "fly.io,*.fly.dev")
	carlos := deployVars(brief.Carlos, "Fly.io", "the demo org", "fly.io,*.fly.dev")

	if spec.account != control.account || spec.platform != "Fly.io" ||
		strings.Join(spec.hosts, ",") != "fly.io,*.fly.dev,spec.carlosframework.com" {
		t.Errorf("spec = %+v, want the control's platform, account and hosts, "+
			"and the spec's site", spec)
	}
	if strings.Contains(strings.Join(control.hosts, ","), "carlosframework") {
		t.Error("the control can reach the spec")
	}
	if carlos.platform != "" || len(carlos.hosts) != 0 ||
		!strings.Contains(carlos.account, "Carloku") {
		t.Errorf("carlos = %+v, want Carloku and no control hosts", carlos)
	}
}

func TestEveryPollKeepsItsOwnEvidence(t *testing.T) {
	first, second := pollLabel(), pollLabel()
	if first == second {
		t.Errorf("two polls share the label %q, so the second would "+
			"overwrite the first's transcripts and answers", first)
	}
}

func TestSpecStillMatchesStopsARoundWhoseSpecChanged(t *testing.T) {
	page := "rule 1"
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(page)) }))
	defer server.Close()
	pinned, err := fetchSpec(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	if err := specStillMatches(server.URL, pinned); err != nil {
		t.Errorf("an unchanged spec: %v", err)
	}
	page = "rule 1, revised"
	if err := specStillMatches(server.URL, pinned); !errors.Is(err, runner.ErrUnclean) {
		t.Errorf("a revised spec: %v, want the round stopped", err)
	}
}
