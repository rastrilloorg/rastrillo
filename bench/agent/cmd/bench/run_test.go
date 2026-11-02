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
	"amadan.net/rastrillo/rastrillo/bench/agent/internal/round"
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

// A groomer-only round reuses a three-brief round's groomer controls:
// the earlier fingerprint is over its own briefs, so it is compared on
// those, not on this round's.
func TestReuseControlAcrossADifferentSetOfBriefs(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"arms", "briefs", "env.txt", "mapping.txt", "settings.json"} {
		copyTree(t, filepath.Join("../..", path), filepath.Join(root, path))
	}
	settings := map[string]string{"platform": "Fly.io", "model": "m", "claude": "9"}
	all, err := pickBriefs(root, "groomer,lending,noticeboard")
	if err != nil {
		t.Fatal(err)
	}
	_, controlAll, err := fingerprints(root, all, settings)
	if err != nil {
		t.Fatal(err)
	}
	earlier := round.Round{Label: "1", ControlConfig: controlAll}
	for n := 1; n <= 3; n++ {
		earlier.Runs = append(earlier.Runs,
			round.RunRecord{Brief: "groomer", Arm: brief.Control, N: n},
			round.RunRecord{Brief: "lending", Arm: brief.Control, N: n},
			round.RunRecord{Brief: "noticeboard", Arm: brief.Control, N: n})
	}
	if err := saveRound(root, earlier); err != nil {
		t.Fatal(err)
	}
	groomer, _ := pickBriefs(root, "groomer")
	_, controlGroomer, _ := fingerprints(root, groomer, settings)
	now := round.Round{Label: "2", ControlConfig: controlGroomer}

	err = reuseControlRuns(root, &now, "1", groomer, 3, settings)

	if err != nil {
		t.Fatalf("reuse refused a groomer-only round: %v", err)
	}
	if len(now.Runs) != 3 {
		t.Errorf("reused %d runs, want groomer's 3", len(now.Runs))
	}
	settings["platform"] = "Render"
	if err := reuseControlRuns(root, &round.Round{Label: "3"}, "1", groomer, 3, settings); err == nil {
		t.Error("reuse accepted a control on a different platform")
	}
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(to, strings.TrimPrefix(path, from))
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, raw, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCheckRoundFailsARoundWorseThanTheBaseline(t *testing.T) {
	root := t.TempDir()
	good := round.Round{Label: "good", Runs: []round.RunRecord{
		{Brief: "groomer", Arm: brief.Carlos, N: 1,
			Build: round.Cost{Complete: true, CostUSD: 6, FirstWriteContext: 100000}},
	}}
	bad := good
	bad.Label = "bad"
	bad.Runs = []round.RunRecord{good.Runs[0]}
	bad.Runs[0].Build.CostUSD = 9
	for _, r := range []round.Round{good, bad} {
		if err := saveRound(root, r); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeBaseline(root, "good", round.Tolerance{Cost: 0.15}); err != nil {
		t.Fatal(err)
	}

	if problems, err := checkRound(root, "good"); err != nil || len(problems) != 0 {
		t.Errorf("good against itself: %v, %v", problems, err)
	}
	problems, err := checkRound(root, "bad")
	if err != nil || len(problems) != 1 {
		t.Errorf("bad: %v, %v; want one regression", problems, err)
	}
}

func TestRescoreRecomputesCostsFromTheTranscripts(t *testing.T) {
	root, cache := t.TempDir(), t.TempDir()
	rec := filepath.Join(cache, "r", "groomer-spec-3", "rec")
	os.MkdirAll(rec, 0o755)
	os.WriteFile(filepath.Join(rec, "build.jsonl"), []byte(strings.Join([]string{
		`{"type":"system","subtype":"init","claude_code_version":"9"}`,
		`{"type":"result","num_turns":80,"total_cost_usd":6.31,"model_usage_since_last_result":{"a":{"costUSD":6.3}}}`,
		`{"type":"result","num_turns":5,"total_cost_usd":7.03,"model_usage_since_last_result":{"a":{"costUSD":0.73}}}`,
	}, "\n")+"\n"), 0o644)
	r := round.Round{Label: "r", Runs: []round.RunRecord{{
		Brief: "groomer", Arm: brief.Spec, N: 3,
		Build: round.Cost{Complete: true, CostUSD: 0.73, NumTurns: 5},
	}}}

	if err := rescore(&r, filepath.Join(cache, "r")); err != nil {
		t.Fatal(err)
	}

	build := r.Runs[0].Build
	if build.NumTurns != 85 || build.CostUSD < 7.0299 || build.CostUSD > 7.0301 {
		t.Errorf("build = %+v, want 85 turns and $7.03", build)
	}
	_ = root
}

// The carlos arm deploys with a service credential scoped to the demo
// account, which has its own creation budget; nobody else is given it.
func TestOnlyTheCarlosArmGetsTheCarlokuCredential(t *testing.T) {
	carlos := deployVars(brief.Carlos, "", "", "")
	carlos.withCarloku("secret", "https://console.example")
	if carlos.env["CARLOS_SERVICE_TOKEN"] != "secret" ||
		carlos.env["CARLOS_CONSOLE"] != "https://console.example" {
		t.Errorf("carlos env = %v", carlos.env)
	}
	for _, arm := range []brief.Arm{brief.Control, brief.Spec} {
		other := deployVars(arm, "Fly.io", "the demo org", "")
		other.withCarloku("secret", "https://console.example")
		if _, given := other.env["CARLOS_SERVICE_TOKEN"]; given {
			t.Errorf("%s was given the Carloku credential", arm)
		}
	}
}

func TestTheCarlokuCredentialIsOnlyEverAskedFor(t *testing.T) {
	if token, err := carlokuCredential(""); token != "" || err != nil {
		t.Errorf("no flag: %q, %v; want no credential", token, err)
	}
	if _, err := carlokuCredential(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("a named file that is missing was ignored")
	}
}

func TestDeployAuthIsInTheConfigNotTheControl(t *testing.T) {
	groomer, _ := pickBriefs("../..", "groomer")
	operator := map[string]string{"carlos-auth": "operator"}
	service := map[string]string{"carlos-auth": "service https://console.example"}
	configA, controlA, _ := fingerprints("../..", groomer, operator)
	configB, controlB, _ := fingerprints("../..", groomer, service)
	if configA == configB {
		t.Error("changing the carlos arm's credential kept the round's fingerprint")
	}
	if controlA != controlB {
		t.Error("the carlos arm's credential changed the control's fingerprint")
	}
}

func TestRescoreWaitsForTheRoundsLock(t *testing.T) {
	root := t.TempDir()
	if err := saveRound(root, round.Round{Label: "r"}); err != nil {
		t.Fatal(err)
	}
	unlock, err := lockRound(root, "r")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	if err := rescoreRound(root, t.TempDir(), "r"); err == nil {
		t.Error("rescore ran while another bench held the round")
	}
}
