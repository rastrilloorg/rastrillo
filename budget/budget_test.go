package budget_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Dir(file))
}

// fixture writes a one-package module whose TestMain calls budget.Main,
// replacing rastrillo with this checkout. The module cache stands in for
// the proxy, as cmd/rastrillo's setSandboxGoEnv does, so it runs offline.
func fixture(t *testing.T, testBody string) string {
	t.Helper()
	modcache, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOPRIVATE", "*")
	t.Setenv("GOPROXY", "file://"+strings.TrimSpace(string(modcache))+"/cache/download")
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/fx\n\ngo 1.25.0\n\nrequire amadan.net/rastrillo/rastrillo v0.0.0\n\nreplace amadan.net/rastrillo/rastrillo => " + repoRoot() + "\n",
		"fx_test.go": `package fx

import (
	"os"
	"testing"

	"amadan.net/rastrillo/rastrillo/budget"
)

func TestMain(m *testing.M) { os.Exit(budget.Main(m)) }

` + testBody,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = dir
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, out)
	}
	return dir
}

// goTest runs with -v because go test in package-list mode prints only
// "ok" for a passing package; rastrillo budget test reads the same
// output through -json, which carries it too.
func goTest(dir string, env ...string) (string, error) {
	cmd := exec.Command("go", "test", "-v", "-count=1", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

var line = regexp.MustCompile(`(?m)^rastrillo-budget/v1 ran (\S+)$`)

func TestMainPrintsOneMeasurementAndKeepsTheCode(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a fixture module")
	}
	dir := fixture(t, "func TestOK(t *testing.T) {}\n")
	out, err := goTest(dir)
	if err != nil {
		t.Fatalf("passing fixture failed: %v\n%s", err, out)
	}
	if got := line.FindAllString(out, -1); len(got) != 1 {
		t.Fatalf("want exactly one measurement line, got %q\n%s", got, out)
	}

	dir = fixture(t, "func TestBad(t *testing.T) { t.Fatal(\"no\") }\n")
	out, err = goTest(dir)
	if err == nil {
		t.Fatalf("budget.Main turned a failing package green:\n%s", out)
	}
	if !line.MatchString(out) {
		t.Fatalf("a failing package still reports its time:\n%s", out)
	}
}

func TestMainIsSilentInABootChild(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a fixture module")
	}
	dir := fixture(t, "func TestOK(t *testing.T) {}\n")
	out, err := goTest(dir, "RASTRILLO_BOOT_CHILD=/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "rastrillo-budget/v1 ran") {
		t.Fatalf("child printed a measurement that would be judged twice:\n%s", out)
	}
}
