package authored

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/bench/agent/internal/transcript"
)

// TestKeptRun tries the package on one kept run. BENCH_KEPT names the
// run's directory, with app/ and rec/build.jsonl in it; unset, it skips.
func TestKeptRun(t *testing.T) {
	kept := os.Getenv("BENCH_KEPT")
	if kept == "" {
		t.Skip("BENCH_KEPT is not set")
	}
	app := filepath.Join(kept, "app")
	module, err := RunModule(app)
	if err != nil {
		t.Fatal(err)
	}
	log, err := os.Open(filepath.Join(kept, "rec", "build.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	run, err := transcript.Parse(log)
	if err != nil {
		t.Fatal(err)
	}
	scaffold, err := ScaffoldOf(run, module)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("scaffold: %+v", scaffold)

	rastrillo := rastrilloDir(t, app)
	into := t.TempDir()
	if err := Rescaffold(rastrillo, scaffold, into); err != nil {
		t.Fatal(err)
	}
	code, err := Authored(app, filepath.Join(into, scaffold.Name))
	if err != nil {
		t.Fatal(err)
	}
	units := 0
	for _, file := range code.Files {
		units += len(file.Funcs)
		t.Logf("  %-50s whole=%-5v test=%-5v funcs=%d",
			file.Path, file.Whole, file.Test, len(file.Funcs))
	}
	t.Logf("files=%d funcs=%d", len(code.Files), units)
	if len(code.Files) == 0 {
		t.Error("no authored code")
	}
}

func rastrilloDir(t *testing.T, app string) string {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(app, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(text), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "amadan.net/rastrillo/rastrillo" {
			out, err := exec.Command("go", "env", "GOMODCACHE").Output()
			if err != nil {
				t.Fatal(err)
			}
			return filepath.Join(strings.TrimSpace(string(out)),
				"amadan.net/rastrillo/rastrillo@"+fields[1])
		}
	}
	t.Fatal("go.mod does not require rastrillo")
	return ""
}
