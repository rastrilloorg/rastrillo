package migrate_test

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// TestExamplesPassMigrationCheck is the framework catching its own
// drift: if an example's models and migrations disagree, the shape
// SKILL.md tells people to copy is already broken.
//
// Only examples/notes is checked here. examples/blog and
// examples/tickets are on the legacy pre-GORM path — they pass
// rastrillo.Options.Migrations to rastrillo.Serve and never call
// db.Open or touch GORM, so they have no Models or Schema for
// `migration check` to read; that path is untouched by this
// subsystem, on purpose (see the design's non-goals).
func TestExamplesPassMigrationCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles the example")
	}
	bin := filepath.Join(t.TempDir(), "rastrillo")
	build := exec.Command("go", "build", "-o", bin, "./cmd/rastrillo")
	build.Dir = repoRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the CLI: %v\n%s", err, out)
	}

	cmd := exec.Command(bin, "migration", "check")
	cmd.Dir = filepath.Join(repoRoot(t), "examples", "notes")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("examples/notes is out of sync:\n%s", out)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatal(err)
	}
	return trimNewline(string(out))
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
