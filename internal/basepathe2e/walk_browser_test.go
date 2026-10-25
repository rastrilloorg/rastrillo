//go:build browser

package basepathe2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A scaffolded app, built by the CLI exactly as a user gets it, is
// served under /acme behind a stand-in edge and driven in a real
// Chromium (testdata/walk_test.go.txt). The drive runs inside the app's
// own module, because it wires the app through the app's own packages.
func TestAScaffoldedAppWorksUnderABasePath(t *testing.T) {
	root := repoRoot(t)
	moduleEnv(t)
	dir := t.TempDir()

	cli := filepath.Join(dir, "rastrillo")
	run(t, root, "go", "build", "-o", cli, "./cmd/rastrillo")
	run(t, dir, cli, "new", "--shell=sidebar", "walkapp")
	app := filepath.Join(dir, "walkapp")
	write(t, filepath.Join(app, "manifest", "app.toml"), "locales = [\"en\", \"es\"]\ndefault_locale = \"en\"\n")
	run(t, dir, cli, "generate", "walkapp")

	drive, err := os.ReadFile(filepath.Join("testdata", "walk_test.go.txt"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(app, "internal", "walkapptest", "basepath_browser_test.go"), string(drive))
	// This checkout, not the published module: the drive tests the
	// framework as it is on this branch.
	f, err := os.OpenFile(filepath.Join(app, "go.mod"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\nreplace amadan.net/rastrillo/rastrillo => " + root + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	run(t, app, "go", "mod", "tidy")

	// The environment passes through, RASTRILLO_CHROME included, so the
	// drive finds the browser this role was given.
	cmd := exec.Command("go", "test", "-count=1", "-v", "-tags", "browser", "-run", "^TestBasePath", "./internal/walkapptest/")
	cmd.Dir = app
	out, err := cmd.CombinedOutput()
	// Both names, so a -run that matched nothing, or a drive renamed,
	// fails here instead of passing with no tests run.
	if err != nil || !strings.Contains(string(out), "--- PASS: TestBasePathRedirectsTheDirectHost") ||
		!strings.Contains(string(out), "--- PASS: TestBasePathServesTheDirectHost") {
		t.Fatalf("the drive under /acme (%v):\n%s", err, out)
	}
}

// repoRoot walks up from the package directory to the go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the package directory")
		}
		dir = parent
	}
}

// moduleEnv is cmd/rastrillo's setSandboxGoEnv: every go command below
// resolves modules from the local module cache, which already holds this
// repository's dependencies, rather than a proxy a sandboxed runner
// cannot reach. -buildvcs=false is hack/scaffold-smoke.sh's: the app is a
// throwaway in a temp directory, and a stray repository above it would
// fail the build with "error obtaining VCS status".
func moduleEnv(t *testing.T) {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Fatalf("go env GOMODCACHE: %v", err)
	}
	t.Setenv("GOFLAGS", "-mod=mod -buildvcs=false")
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOPRIVATE", "*")
	t.Setenv("GOPROXY", "file://"+strings.TrimSpace(string(out))+"/cache/download")
}

func run(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
