package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
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

func TestTheGoplsArmNeedsAGoplsPath(t *testing.T) {
	both := []brief.Arm{brief.Carlos, brief.CarlosGopls}
	if _, err := goplsFor(both, "", ""); err == nil {
		t.Error("carlos-gopls ran without -gopls")
	}
	if _, err := goplsFor(both, filepath.Join(t.TempDir(), "gopls"), ""); err == nil {
		t.Error("a -gopls that does not exist was accepted")
	}
	if none, err := goplsFor([]brief.Arm{brief.Carlos}, "", ""); none != (gopls{}) ||
		err != nil {
		t.Errorf("carlos alone = %+v, %v; want no gopls", none, err)
	}

	dir := t.TempDir()
	fake := filepath.Join(dir, "gopls")
	os.WriteFile(fake, []byte("#!/bin/sh\ncase \"$*\" in\n"+
		"version) echo 'golang.org/x/tools/gopls v0.24.0' ;;\n"+
		"'mcp -instructions') printf '# The gopls MCP server\\n\\nUse it.\\n' ;;\n"+
		"esac\n"), 0o755)
	t.Chdir(dir)
	found, err := goplsFor(both, "gopls", "")
	if err != nil {
		t.Fatal(err)
	}
	if found.path != fake || found.version != "golang.org/x/tools/gopls v0.24.0" ||
		found.instructions != "# The gopls MCP server\n\nUse it." {
		t.Errorf("goplsFor = %+v; want %q, its version and its instructions",
			found, fake)
	}

	// A round that ran gopls needs it on every resume, whatever -arms
	// says, or its fingerprint would not match.
	ran := "golang.org/x/tools/gopls v0.24.0"
	_, err = goplsFor([]brief.Arm{brief.Carlos}, "", ran)
	if err == nil || !strings.Contains(err.Error(), ran) ||
		!strings.Contains(err.Error(), "-gopls") {
		t.Errorf("resuming a gopls round without -gopls: %v, want an error "+
			"naming gopls and -gopls", err)
	}
	if resumed, err := goplsFor([]brief.Arm{brief.Carlos}, "gopls", ran); err != nil ||
		resumed != found {
		t.Errorf("resuming with -gopls and -arms carlos = %+v, %v; want %+v",
			resumed, err, found)
	}

	// The server is the bench's wrapper, by absolute path, given gopls,
	// the app it watches, a log, and the Go settings a run gets.
	var config struct {
		MCPServers map[string]struct {
			Type, Command string
			Args          []string
			Env           map[string]string
		} `json:"mcpServers"`
	}
	raw := goplsServer("/c/bin/goplsmcp", found.path, "/run/app",
		"/run/rec/goplsmcp.log", map[string]string{"GOPROXY": "file:///p"})
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		t.Fatalf("%s: %v", raw, err)
	}
	server := config.MCPServers["gopls"]
	if len(config.MCPServers) != 1 || server.Type != "stdio" ||
		server.Command != "/c/bin/goplsmcp" ||
		strings.Join(server.Args, " ") != "-gopls "+fake+
			" -dir /run/app -log /run/rec/goplsmcp.log" ||
		server.Env["GOPROXY"] != "file:///p" {
		t.Errorf("MCP config = %s", raw)
	}
}

func TestTheGoplsVersionIsInTheFingerprint(t *testing.T) {
	groomer, _ := pickBriefs("../..", "groomer")
	fingerprintWith := func(gopls map[string]string) (string, string) {
		settings := map[string]string{"model": "m"}
		for name, value := range gopls {
			settings[name] = value
		}
		config, control, err := fingerprints("../..", groomer, settings)
		if err != nil {
			t.Fatal(err)
		}
		return config, control
	}
	arm := map[string]string{"gopls": "golang.org/x/tools/gopls v0.24.0",
		"gopls-instructions": "Use go_workspace first.",
		"gopls-proxy":        "abc"}
	none, controlNone := fingerprintWith(nil)
	newer, controlNewer := fingerprintWith(arm)
	if none == newer || controlNone != controlNewer {
		t.Error("gopls must change the round's fingerprint and never the control's")
	}
	for name := range arm {
		changed := map[string]string{}
		for each, value := range arm {
			changed[each] = value
		}
		changed[name] += " (changed)"
		if config, _ := fingerprintWith(changed); config == newer {
			t.Errorf("a changed %s kept the round's fingerprint", name)
		}
	}
	if hash, err := sourceHash("../..", "cmd/goplsmcp", "internal/mcpproxy"); err != nil ||
		len(hash) != 64 {
		t.Errorf("the wrapper's source hash = %q, %v", hash, err)
	}
}

// Both arms deploy to the same Carloku account; with one name, a run
// of one would deploy over, or build on, the other's app.
func TestTheTwoRastrilloArmsGetDifferentAppNames(t *testing.T) {
	carlos := appName("groomer", "read-1", brief.Carlos, 1, 0)
	gopls := appName("groomer", "read-1", brief.CarlosGopls, 1, 0)
	if carlos != "groomer-rread-1-1" || gopls != "groomer-gopls-rread-1-1" {
		t.Errorf("names = %s and %s, want groomer-rread-1-1 and "+
			"groomer-gopls-rread-1-1", carlos, gopls)
	}
	if got := appName("groomer", "read-1", brief.CarlosGopls, 2, 1); got != "groomer-gopls-rread-1-2-a2" {
		t.Errorf("gopls's second attempt = %s", got)
	}
}

func TestBothRastrilloArmsReachRecordsSummariesAndCleanup(t *testing.T) {
	r := round.Round{Label: "t", Platform: "Fly.io"}
	for _, arm := range []brief.Arm{brief.Carlos, brief.CarlosGopls} {
		deploy := deployVars(arm, "Fly.io", "the demo org", "fly.io")
		deploy.withCarloku("secret", "https://console.example")
		if deploy.account != carlokuAccount || len(deploy.hosts) != 0 ||
			deploy.env["CARLOS_SERVICE_TOKEN"] != "secret" {
			t.Errorf("%s deploys as %+v, want Carloku's demo account", arm, deploy)
		}

		dir := t.TempDir()
		os.MkdirAll(filepath.Join(dir, "app"), 0o755)
		os.WriteFile(filepath.Join(dir, "app", "go.mod"), []byte(
			"module x\n\nrequire amadan.net/rastrillo/rastrillo v0.41.0\n"), 0o644)
		record := recordOf(runner.Spec{Dir: dir, Brief: brief.Brief{Name: "smoke"},
			Arm: arm, N: 1, Vars: brief.Vars{App: appName("smoke", "t", arm, 1, 0)}},
			runner.Outcome{})
		if record.RastrilloVersion != "v0.41.0" {
			t.Errorf("%s recorded Rastrillo %q, want v0.41.0", arm,
				record.RastrilloVersion)
		}
		r.Runs = append(r.Runs, record)
	}

	if _, present := round.Summarise(r)["smoke"][brief.CarlosGopls]; !present {
		t.Error("carlos-gopls has no column in the summary")
	}
	commands := strings.Join(cleanupCommands(r), "\n")
	for _, want := range []string{
		"carlos apps delete --account-id bdf --app smoke-rt-1",
		"carlos apps delete --account-id bdf --app smoke-gopls-rt-1",
	} {
		if !strings.Contains(commands, want) {
			t.Errorf("cleanup lacks %q:\n%s", want, commands)
		}
	}
}

// Arms alternate by run index, so a drift over the round's two days
// (the model, Carloku, the token's limit) falls on both arms alike.
// The order is saved, and a resume keeps it whatever order -arms says.
func TestTheScheduleInterleavesArmsAndSurvivesAResume(t *testing.T) {
	root := t.TempDir()
	briefs := []string{"groomer", "lending"}
	arms := []brief.Arm{brief.Carlos, brief.CarlosGopls}
	slot := func(name string, arm brief.Arm, n int) round.Slot {
		return round.Slot{Brief: name, Arm: arm, N: n}
	}
	want := []round.Slot{
		slot("groomer", brief.Carlos, 1), slot("groomer", brief.CarlosGopls, 1),
		slot("lending", brief.Carlos, 1), slot("lending", brief.CarlosGopls, 1),
		slot("groomer", brief.Carlos, 2), slot("groomer", brief.CarlosGopls, 2),
		slot("lending", brief.Carlos, 2), slot("lending", brief.CarlosGopls, 2),
	}

	r := round.Round{Label: "s"}
	r.Schedule = extendSchedule(r.Schedule, briefs, arms, 2)
	if !slices.Equal(r.Schedule, want) {
		t.Fatalf("schedule = %v,\nwant %v", r.Schedule, want)
	}
	r.Runs = append(r.Runs, round.RunRecord{Brief: "groomer",
		Arm: brief.Carlos, N: 1})
	if err := saveRound(root, r); err != nil {
		t.Fatal(err)
	}

	resumed, err := loadRound(root, "s")
	if err != nil {
		t.Fatal(err)
	}
	reversed := []brief.Arm{brief.CarlosGopls, brief.Carlos}
	resumed.Schedule = extendSchedule(resumed.Schedule, briefs, reversed, 3)
	if !slices.Equal(resumed.Schedule[:8], want) {
		t.Errorf("a resume reordered the schedule: %v", resumed.Schedule)
	}
	if !slices.Equal(resumed.Schedule[8:], []round.Slot{
		slot("groomer", brief.Carlos, 3), slot("groomer", brief.CarlosGopls, 3),
		slot("lending", brief.Carlos, 3), slot("lending", brief.CarlosGopls, 3),
	}) {
		t.Errorf("a third run was not added after the saved schedule: %v",
			resumed.Schedule[8:])
	}

	got := pendingJobs(resumed, briefs, []brief.Arm{brief.CarlosGopls}, 3)
	if len(got) != 6 || got[0] != slot("groomer", brief.CarlosGopls, 1) ||
		got[1] != slot("lending", brief.CarlosGopls, 1) {
		t.Errorf("pending carlos-gopls jobs = %v", got)
	}
}

// read, keys, score, waste and pairs read both Rastrillo arms unless
// told otherwise, and carlos never picks up carlos-gopls's runs.
func TestKeptRunsReadBothRastrilloArmsByDefault(t *testing.T) {
	cache := t.TempDir()
	r := round.Round{Label: "1"}
	for _, run := range []round.RunRecord{
		{Brief: "groomer", Arm: brief.Carlos, N: 1},
		{Brief: "groomer", Arm: brief.CarlosGopls, N: 1},
		{Brief: "groomer", Arm: brief.Control, N: 1},
	} {
		r.Runs = append(r.Runs, run)
		os.MkdirAll(filepath.Join(cache, "1",
			fmt.Sprintf("%s-%s-%d", run.Brief, run.Arm, run.N)), 0o755)
	}
	os.MkdirAll(filepath.Join(cache, "1", "groomer-carlos-gopls-1-attempt-1"),
		0o755)
	names := func(kept []keptRun) []string {
		var names []string
		for _, run := range kept {
			names = append(names, run.name)
		}
		return names
	}

	both, _, err := acceptedRuns(r, cache, []string{"groomer"},
		strings.Split(rastrilloArms, ","))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(names(both), []string{"groomer-carlos-1",
		"groomer-carlos-gopls-1"}) {
		t.Errorf("default arms found %v", both)
	}
	carlos, _, _ := acceptedRuns(r, cache, []string{"groomer"},
		[]string{"carlos"})
	if !slices.Equal(names(carlos), []string{"groomer-carlos-1"}) {
		t.Errorf("-arms carlos found %v", carlos)
	}
}

// Only the runs a round's records accepted are read. The directory a
// failed last attempt left at the run's own path, or a build still
// going there, is skipped and named; a reused run is read where it was
// made. Every command finds its runs through results/<round>.json.
func TestKeptRunsAreTheRunsTheRoundAccepted(t *testing.T) {
	root, cache := t.TempDir(), t.TempDir()
	r := round.Round{Label: "read-1", Runs: []round.RunRecord{
		{Brief: "groomer", Arm: brief.Carlos, N: 1},
		{Brief: "groomer", Arm: brief.Control, N: 1, ReusedFrom: "1"},
	}}
	for _, dir := range []string{"read-1/groomer-carlos-1",
		"read-1/groomer-carlos-2", "read-1/groomer-carlos-gopls-1",
		"read-1/groomer-carlos-2-attempt-1", "1/groomer-control-1"} {
		os.MkdirAll(filepath.Join(cache, dir), 0o755)
	}
	if err := saveRound(root, r); err != nil {
		t.Fatal(err)
	}

	kept, err := keptRuns(root, cache, "read-1", []string{"groomer"},
		[]string{"carlos", "carlos-gopls", "control"})
	if err != nil {
		t.Fatal(err)
	}
	_, skipped, _ := acceptedRuns(r, cache, []string{"groomer"},
		[]string{"carlos", "carlos-gopls", "control"})

	want := []keptRun{
		{"groomer-carlos-1", filepath.Join(cache, "read-1", "groomer-carlos-1")},
		{"groomer-control-1", filepath.Join(cache, "1", "groomer-control-1")},
	}
	if !slices.Equal(kept, want) {
		t.Errorf("kept = %v, want %v", kept, want)
	}
	if !slices.Equal(skipped, []string{"groomer-carlos-2",
		"groomer-carlos-gopls-1"}) {
		t.Errorf("skipped = %v", skipped)
	}
	if _, err := keptRuns(root, cache, "no-such-round", []string{"groomer"},
		[]string{"carlos"}); err == nil {
		t.Error("a round with no records found runs")
	}
}

// A round's decision is kept by hand beside the generated report, which
// points to it, so writing the report again never loses it.
func TestTheRoundReportPointsToItsDecision(t *testing.T) {
	root := t.TempDir()
	r := round.Round{Label: "read-1"}
	if err := saveRound(root, r); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(resultPath(root, "read-1", ".md"))
	if strings.Contains(string(raw), "Decision") {
		t.Errorf("a round with no decision points to one:\n%s", raw)
	}

	decision := resultPath(root, "read-1-decision", ".md")
	os.WriteFile(decision, []byte("# Decision\n\nnot accepted\n"), 0o644)
	if err := saveRound(root, r); err != nil {
		t.Fatal(err)
	}

	raw, _ = os.ReadFile(resultPath(root, "read-1", ".md"))
	if !strings.Contains(string(raw), "[read-1-decision.md](read-1-decision.md)") {
		t.Errorf("the report does not point to the decision:\n%s", raw)
	}
	kept, _ := os.ReadFile(decision)
	if string(kept) != "# Decision\n\nnot accepted\n" {
		t.Errorf("writing the report changed the decision: %q", kept)
	}
}
