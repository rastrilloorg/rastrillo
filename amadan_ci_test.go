package rastrillo

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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

// TestAmadanSuiteShardsMatchRoles pins the suite step's @N to the
// Makefile's SUITE. amadan starts N shards and each runs the role at
// its index, so the two numbers are one fact written twice: a role
// added without raising N runs nowhere while every shard reports ok.
// hack/suite.sh refuses the mismatch at run time; this says so before
// a runner is involved. It also refuses a role that is a ci
// prerequisite as well, which would run it twice.
func TestAmadanSuiteShardsMatchRoles(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not installed; the gate itself runs under make")
	}
	var step, shards string
	for name, target := range amadanCIDTargets(t) {
		if target != "suite" {
			continue
		}
		if step != "" {
			t.Fatalf("two .amadan/ci.d steps exec `make suite` (%s, %s); each would run every role", step, name)
		}
		step = name
		if i := strings.LastIndex(name, "@"); i >= 0 {
			shards = name[i+1:]
		}
	}
	if step == "" {
		t.Fatal("no .amadan/ci.d step execs `make suite`, so the suite's roles run nowhere under amadan")
	}

	out, err := exec.Command("make", "-s", "--no-print-directory", "suite-roles").Output()
	if err != nil {
		t.Fatalf("make suite-roles: %v", err)
	}
	roles := strings.Fields(string(out))
	if len(roles) == 0 {
		t.Fatal("make suite-roles printed no roles")
	}
	if want := strconv.Itoa(len(roles)); shards != want {
		t.Errorf(".amadan/ci.d/%s runs as %q shards, but the Makefile's SUITE has %d roles; rename it to end in @%s", step, shards, len(roles), want)
	}

	mk, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatalf("reading Makefile: %v", err)
	}
	m := ciPrereqPattern.FindStringSubmatch(string(mk))
	if m == nil {
		t.Fatal("Makefile has no `ci:` target")
	}
	ci := map[string]bool{}
	for _, f := range strings.Fields(strings.ReplaceAll(m[1], "\\\n", " ")) {
		ci[f] = true
	}
	seen := map[string]bool{}
	for _, r := range roles {
		if seen[r] {
			t.Errorf("SUITE lists %q twice; two shards would run it", r)
		}
		seen[r] = true
		if ci[r] {
			t.Errorf("%q is both a suite role and a prerequisite of ci, so make ci runs it twice", r)
		}
	}
}

// sliceRolePattern reads a sliced role: <mode>.<pkg>~<k>of<n>, mode
// browser or test.
var sliceRolePattern = regexp.MustCompile(`^(browser|test)\.(.+)~([0-9]+)of([0-9]+)$`)

// TestSlicesPartitionTheirPackages is what lets a package run under
// -run at all. ui/ui_test.go refuses a hand-written -run on ./ui/
// because a filter that matches nothing exits 0; a slice is a -run
// too, so this proves the slices cannot leave a test out: every slice
// 1..n of a package is a suite role, and hack/gotest.sh's deal puts
// each name in exactly one slice and leaves no slice empty. The names
// are every weighted one plus synthetic ones, so this compiles
// nothing; the real list is dealt by the same function at run time.
func TestSlicesPartitionTheirPackages(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not installed; the gate itself runs under make")
	}
	out, err := exec.Command("make", "-s", "--no-print-directory", "suite-roles").Output()
	if err != nil {
		t.Fatalf("make suite-roles: %v", err)
	}
	type sliced struct{ mode, pkg string }
	slices := map[sliced]map[int]bool{}
	counts := map[sliced]int{}
	for _, r := range strings.Fields(string(out)) {
		m := sliceRolePattern.FindStringSubmatch(r)
		if m == nil {
			continue
		}
		k, _ := strconv.Atoi(m[3])
		n, _ := strconv.Atoi(m[4])
		key := sliced{m[1], strings.ReplaceAll(m[2], ".", "/")}
		if prev, ok := counts[key]; ok && prev != n {
			t.Fatalf("%s %s is sliced both %d and %d ways", key.mode, key.pkg, prev, n)
		}
		counts[key] = n
		if slices[key] == nil {
			slices[key] = map[int]bool{}
		}
		slices[key][k] = true
	}
	if len(counts) == 0 {
		t.Skip("no package is sliced")
	}

	weights, err := os.ReadFile("hack/test-weights.txt")
	if err != nil {
		t.Fatal(err)
	}
	for key, n := range counts {
		name := key.mode + " " + key.pkg
		for k := 1; k <= n; k++ {
			if !slices[key][k] {
				t.Errorf("%s is sliced %d ways but slice %d is no suite role, so its tests run nowhere", name, n, k)
			}
		}

		var names []string
		for _, l := range strings.Split(string(weights), "\n") {
			if f := strings.Fields(l); len(f) == 4 && f[0] == key.mode && f[1] == key.pkg {
				names = append(names, f[2])
			}
		}
		for i := 0; i < 5*n; i++ {
			names = append(names, "TestUnweighted"+strconv.Itoa(i))
		}
		deal := exec.Command("sh", "hack/gotest.sh", "--deal", key.mode, key.pkg, strconv.Itoa(n))
		deal.Stdin = strings.NewReader(strings.Join(names, "\n") + "\n")
		got, err := deal.Output()
		if err != nil {
			t.Fatalf("dealing %s: %v", name, err)
		}
		dealt := map[string]int{}
		used := map[int]bool{}
		for _, l := range strings.Split(strings.TrimSpace(string(got)), "\n") {
			f := strings.Fields(l)
			if len(f) != 2 {
				t.Fatalf("deal line %q is not `<k> <name>`", l)
			}
			k, err := strconv.Atoi(f[0])
			if err != nil || k < 1 || k > n {
				t.Errorf("%s: %s was dealt to slice %q, outside 1..%d", name, f[1], f[0], n)
			}
			used[k] = true
			dealt[f[1]]++
		}
		for _, nm := range names {
			if dealt[nm] != 1 {
				t.Errorf("%s: %s was dealt %d times, not once", name, nm, dealt[nm])
			}
		}
		if len(dealt) != len(names) {
			t.Errorf("%s: dealt %d names from %d", name, len(dealt), len(names))
		}
		for k := 1; k <= n; k++ {
			if !used[k] {
				t.Errorf("%s: slice %d of %d was dealt nothing from %d names", name, k, n, len(names))
			}
		}
	}
}
