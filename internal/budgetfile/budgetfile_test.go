package budgetfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseValidFile(t *testing.T) {
	src := `# comment line

size internal/designsystem 10500 -  generated samples; split owed
size ui                    -    20500 one test per component, by design
time internal/importer     25s        replays a 40MB fixture
perf GET /notes/export     1200ms     streams every row
perf GET /jobs/{id}/events skip       server-sent events, never finishes
boot                       800ms      parses 12 locales at start
`
	f, err := Parse(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if s := f.Sizes["internal/designsystem"]; s.Source != 10500 || s.Test != 0 || s.Line != 3 {
		t.Errorf("designsystem size = %+v", s)
	}
	if s := f.Sizes["ui"]; s.Source != 0 || s.Test != 20500 {
		t.Errorf("ui size = %+v", s)
	}
	if tm := f.Times["internal/importer"]; tm.Limit != 25*time.Second || tm.Reason != "replays a 40MB fixture" {
		t.Errorf("importer time = %+v", tm)
	}
	if p := f.Perfs["GET /notes/export"]; p.Limit != 1200*time.Millisecond || p.Skip {
		t.Errorf("export perf = %+v", p)
	}
	if p := f.Perfs["GET /jobs/{id}/events"]; !p.Skip {
		t.Errorf("events perf = %+v, want skip", p)
	}
	if f.Boot == nil || f.Boot.Limit != 800*time.Millisecond {
		t.Errorf("boot = %+v", f.Boot)
	}
}

// Each refusal is its own case so a parser that stops checking one rule
// cannot hide behind another case's failure.
func TestParseRefusals(t *testing.T) {
	cases := map[string]string{
		"unknown kind":             "speed internal/x 5s why",
		"missing reason":           "time internal/x 5s",
		"bad number":               "size internal/x lots - why",
		"bad duration":             "time internal/x soon why",
		"zero":                     "time internal/x 0s why",
		"negative":                 "size internal/x -5 - why",
		"both dashes":              "size internal/x - - why",
		"source not above default": "size internal/x 5000 - why",
		"test not above default":   "size internal/x - 8000 why",
		"leading dot slash":        "time ./internal/x 5s why",
		"trailing slash":           "time internal/x/ 5s why",
		"absolute":                 "time /internal/x 5s why",
		"dot dot":                  "time ../x 5s why",
		"unclean":                  "time internal/a/../b 5s why",
		"duplicate":                "time internal/x 5s why\ntime internal/x 6s why",
		"lower-case method":        "perf get /x 1s why",
		"perf missing pattern":     "perf GET 1s why",
		"boot missing reason":      "boot 900ms",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(src))
			if err == nil {
				t.Fatalf("accepted %q", src)
			}
			if !strings.Contains(err.Error(), "line ") {
				t.Errorf("error %q does not name a line", err)
			}
		})
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	f, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Sizes)+len(f.Times)+len(f.Perfs) != 0 || f.Boot != nil {
		t.Fatalf("missing file should mean no exemptions, got %+v", f)
	}
}

func TestModuleRootAndPath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(root, "internal", "app")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := ModuleRoot(deep)
	if err != nil || got != root {
		t.Fatalf("ModuleRoot = %q, %v; want %q", got, err, root)
	}
	mp, err := ModulePath(root)
	if err != nil || mp != "example.com/app" {
		t.Fatalf("ModulePath = %q, %v", mp, err)
	}
}

func TestEnforce(t *testing.T) {
	env := map[string]string{}
	get := func(k string) string { return env[k] }
	if Enforce(get) {
		t.Fatal("enforcing with neither variable set")
	}
	env["AMADAN_CI"] = "1"
	if !Enforce(get) {
		t.Fatal("AMADAN_CI=1 must enforce")
	}
	env = map[string]string{"CI": "true"}
	if !Enforce(get) {
		t.Fatal("CI=true must enforce")
	}
}
