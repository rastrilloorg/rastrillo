package scaffoldcheck

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The lanes every new app's gate must run, with the recipe each runs.
var requiredLanes = map[string]string{
	"code":   "$(RASTRILLO) budget code",
	"routes": "$(RASTRILLO) routes --check",
}

// make ci and the runner's steps are one gate: a lane in one and not the
// other passes on a laptop and is skipped on CI, or the other way round.
func TestCIAndTheRunnerStepsNameTheSameLanes(t *testing.T) {
	t.Parallel()
	for _, variant := range variants {
		directory := newApp(t, variant.name, variant.flags...)
		makefile := readFile(t, filepath.Join(directory, "Makefile"))
		lanes := ciLanes(makefile)
		steps := runnerSteps(t, filepath.Join(directory, ".amadan", "ci.d"))
		slices.Sort(lanes)
		slices.Sort(steps)
		if !slices.Equal(lanes, steps) {
			t.Errorf("%s: make ci runs %v, the steps run %v",
				variant.name, lanes, steps)
		}
		for lane, recipe := range requiredLanes {
			if !slices.Contains(lanes, lane) {
				t.Errorf("%s: make ci has no %s lane", variant.name, lane)
			}
			if !strings.Contains(makefile, "\n"+lane+":\n\t"+recipe+"\n") {
				t.Errorf("%s: no %s target running %s", variant.name, lane,
					recipe)
			}
		}
	}
}

// ciLanes is the targets on the Makefile's ci line.
func ciLanes(makefile string) []string {
	for _, line := range strings.Split(makefile, "\n") {
		if lanes, ok := strings.CutPrefix(line, "ci: "); ok {
			return strings.Fields(lanes)
		}
	}
	return nil
}

// runnerSteps is the target each .amadan/ci.d step runs. A step that is
// not executable, or runs anything but one make target, fails the test:
// the runner skips the first, and the second is a copy that drifts.
func runnerSteps(t *testing.T, directory string) []string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	var targets []string
	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		info, err := entry.Info()
		if err != nil || info.Mode()&0o111 == 0 {
			t.Errorf("%s is not executable: %v", path, err)
		}
		script := readFile(t, path)
		target, ok := strings.CutPrefix(script, "#!/bin/sh\nexec make ")
		if !ok || strings.Count(target, "\n") != 1 {
			t.Errorf("%s does not exec one make target:\n%s", path, script)
		}
		targets = append(targets, strings.TrimSpace(target))
	}
	return targets
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
