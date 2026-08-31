package rastrillo

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// This file is new_scaffold_test.go's TestNewScaffoldsCIAndManifest,
// aimed at this repo's own tree instead of a scaffold's. This branch
// (moving the repo off GitHub Actions onto .amadan/ci.d, driven by
// `make ci`) introduced .amadan/ci and .amadan/ci.d/ for the framework
// repo itself, and nothing gated them — the identical failure mode
// with no gate at all: a lost exec bit, or a ci.d step that runs its
// own commands instead of naming a Makefile target, resolves amadan's
// runner to "skipped", which renders as a pass. That is the exact
// defect shape this branch exists to close; it had already shipped
// five times before these two findings.

// amadanCIDStepFiles lists .amadan/ci.d's entries, sorted, so every
// check below walks the same list.
func amadanCIDStepFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".amadan/ci.d")
	if err != nil {
		t.Fatalf("reading .amadan/ci.d: %v", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatal(".amadan/ci.d has no step files")
	}
	return names
}

// TestAmadanFilesAreExecutableInGit checks the executable bit the way
// amadan's runner sees it: through git's index, not the working tree.
// A filesystem-only check (os.Stat) would pass on a machine where the
// bit is set locally but was never committed — the exact gap that lets
// a non-executable step ship and resolve "skipped" (a pass) on every
// other checkout.
func TestAmadanFilesAreExecutableInGit(t *testing.T) {
	out, err := exec.Command("git", "ls-files", "-s", ".amadan/").Output()
	if err != nil {
		t.Fatalf("git ls-files -s .amadan/: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatal("git ls-files -s .amadan/ reported nothing tracked")
	}

	seen := map[string]bool{}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			t.Fatalf("unexpected `git ls-files -s` line: %q", line)
		}
		mode, path := fields[0], fields[3]
		seen[path] = true
		if mode != "100755" {
			t.Errorf("%s is committed with mode %s, not 100755 — a runner reading the index (not the working tree) would resolve this job \"skipped\", which renders as a pass", path, mode)
		}
	}

	want := []string{".amadan/ci"}
	for _, name := range amadanCIDStepFiles(t) {
		want = append(want, filepath.Join(".amadan/ci.d", name))
	}
	for _, w := range want {
		if !seen[w] {
			t.Errorf("%s is not tracked in git's index at all", w)
		}
	}
}

// execMakePattern anchors on the exact shape a ci.d step is allowed to
// take: nothing but a call into the Makefile target of the same name.
var execMakePattern = regexp.MustCompile(`^exec make ([A-Za-z0-9_-]+)$`)

// amadanCIDTargets reads every .amadan/ci.d step, asserts each is a
// bare `exec make <target>` (AGENTS.md: "ci.d/ ... never keeps its own
// copy of a command"), and returns the step-file-name -> target map.
func amadanCIDTargets(t *testing.T) map[string]string {
	t.Helper()
	targets := map[string]string{}
	for _, name := range amadanCIDStepFiles(t) {
		path := filepath.Join(".amadan/ci.d", name)
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		var nonBlank []string
		for _, l := range strings.Split(string(b), "\n") {
			if strings.TrimSpace(l) != "" {
				nonBlank = append(nonBlank, l)
			}
		}
		if len(nonBlank) != 2 || nonBlank[0] != "#!/bin/sh" {
			t.Errorf("%s must be exactly a `#!/bin/sh` shebang followed by one `exec make <target>` line; got:\n%s", path, b)
			continue
		}
		m := execMakePattern.FindStringSubmatch(nonBlank[1])
		if m == nil {
			t.Errorf("%s's step is %q, not a bare `exec make <target>` — a step that runs its own command instead of a Makefile target is exactly the copy AGENTS.md says never to keep, and it can drift from what `make ci` actually runs without anything noticing", path, nonBlank[1])
			continue
		}
		targets[name] = m[1]
	}
	return targets
}

// TestAmadanStepsAreBareExecMake is that assertion on its own, so a
// broken step file fails here with the specific file and line named,
// rather than only showing up as a set mismatch in the test below.
func TestAmadanStepsAreBareExecMake(t *testing.T) {
	amadanCIDTargets(t)
}

// ciPrereqPattern pulls `ci:`'s prerequisite list out of the Makefile,
// following GNU Make's backslash-newline continuation so the
// multi-line target list is captured whole.
var ciPrereqPattern = regexp.MustCompile(`(?m)^ci:[ \t]*((?:.*\\\n)*.*)$`)

// TestAmadanCITargetsMatchStepFiles enforces the promise AGENTS.md
// makes and nothing else checks: "add to the Makefile and to ci.d/
// together, or a step-reporting runner silently skips what you
// added." A target added to `ci:` with no matching ci.d step never
// runs under amadan's runner even though `make ci` covers it; a ci.d
// step naming a target dropped from (or never added to) `ci:` reports
// a job that `make ci` — the definition AGENTS.md tells everyone to
// run — no longer executes. Either drift is silent without this.
func TestAmadanCITargetsMatchStepFiles(t *testing.T) {
	mk, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatalf("reading Makefile: %v", err)
	}
	m := ciPrereqPattern.FindStringSubmatch(string(mk))
	if m == nil {
		t.Fatal("Makefile has no `ci:` target; `make ci` is the one gate every step file execs into")
	}
	joined := strings.ReplaceAll(m[1], "\\\n", " ")
	ciTargets := map[string]bool{}
	for _, f := range strings.Fields(joined) {
		ciTargets[f] = true
	}
	if len(ciTargets) == 0 {
		t.Fatal("Makefile's `ci:` target has no prerequisites")
	}

	stepTargets := map[string]bool{}
	for step, target := range amadanCIDTargets(t) {
		stepTargets[target] = true
		if !ciTargets[target] {
			t.Errorf(".amadan/ci.d/%s execs `make %s`, but %q is not one of ci:'s prerequisites in the Makefile — this step now reports a job `make ci` does not run", step, target, target)
		}
	}
	for target := range ciTargets {
		if !stepTargets[target] {
			t.Errorf("Makefile's `ci:` target depends on %q, but no .amadan/ci.d/* step execs `make %s` — amadan's runner silently skips this half of `make ci`", target, target)
		}
	}
}
