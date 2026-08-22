package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNewScaffoldsCIAndManifest covers the host-awareness half of the
// scaffold: the Makefile gate, the amadan CI scripts (executable — a
// non-executable step is the runner's known silent-skip failure mode),
// the manifest/ landing spot, and the CLAUDE.md preload (§12).
func TestNewScaffoldsCIAndManifest(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)

	if err := runNew([]string{"demoapp"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}

	for _, rel := range []string{
		"Makefile", "CLAUDE.md", "manifest/README.md",
		".amadan/ci", ".amadan/ci.d/10-vet", ".amadan/ci.d/20-fmt", ".amadan/ci.d/30-test",
	} {
		if _, err := os.Stat(filepath.Join("demoapp", rel)); err != nil {
			t.Errorf("scaffold missing %s: %v", rel, err)
		}
	}

	for _, rel := range []string{".amadan/ci", ".amadan/ci.d/10-vet", ".amadan/ci.d/20-fmt", ".amadan/ci.d/30-test"} {
		fi, err := os.Stat(filepath.Join("demoapp", rel))
		if err != nil {
			continue
		}
		if fi.Mode()&0o111 == 0 {
			t.Errorf("%s is not executable — amadan's runner would resolve the job 'skipped'", rel)
		}
	}

	mk, _ := os.ReadFile(filepath.Join("demoapp", "Makefile"))
	if !strings.Contains(string(mk), "ci: vet fmt-check test") {
		t.Fatalf("Makefile must define the one ci gate:\n%s", mk)
	}
	step, _ := os.ReadFile(filepath.Join("demoapp", ".amadan", "ci.d", "10-vet"))
	if !strings.Contains(string(step), "exec make vet") {
		t.Fatalf("steps must exec Makefile targets, never their own commands:\n%s", step)
	}
	// The preload lives in AGENTS.md — the cross-agent file, so it reaches
	// whatever agent someone uses. CLAUDE.md is an @AGENTS.md import and
	// nothing else: two copies of this would drift, silently.
	agents, _ := os.ReadFile(filepath.Join("demoapp", "AGENTS.md"))
	for _, want := range []string{"integer cents", "scope.Owned", "SKILL.md", "CGO_ENABLED=0"} {
		if !strings.Contains(string(agents), want) {
			t.Fatalf("AGENTS.md preload missing %q:\n%s", want, agents)
		}
	}
	claude, _ := os.ReadFile(filepath.Join("demoapp", "CLAUDE.md"))
	if !strings.Contains(string(claude), "@AGENTS.md") {
		t.Fatalf("CLAUDE.md must import AGENTS.md:\n%s", claude)
	}
}
