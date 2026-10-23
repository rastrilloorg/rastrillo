// Package scratch builds throwaway apps for the generator's tests: a
// module named scratch that requires this repository, with the real
// rastrillo binary run over it. The tests drive the binary rather than
// the generator's functions because pruning, the router and the
// --check gate are wired together in cmd/rastrillo, and a test that
// called the pieces itself would pass while the command skipped one.
// It is a package rather than a _test file so the emitters that live
// in their own packages can run the same apps.
package scratch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

// cliDir holds the one CLI build every scratch-app test shares;
// building it per test would add a link step to each.
var (
	cliOnce sync.Once
	cliDir  string
	cliPath string
	cliErr  error
)

// Cleanup removes the shared CLI build. A package's TestMain calls it
// after m.Run, because no one test owns the binary.
func Cleanup() {
	if cliDir != "" {
		os.RemoveAll(cliDir)
	}
}

// Env is how the Makefile runs go: -mod=mod resolves module data the
// scratch go.sum does not carry, and -buildvcs=false because $TMPDIR
// may sit under a repository the scratch module is not part of.
func Env() []string {
	return append(os.Environ(), "GOFLAGS=-mod=mod -buildvcs=false", "CGO_ENABLED=0")
}

// RepoRoot is this repository's root, found from this file's own path.
func RepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// CLI builds cmd/rastrillo once per test binary and returns its path.
func CLI(t *testing.T) string {
	t.Helper()
	cliOnce.Do(func() {
		if cliDir, cliErr = os.MkdirTemp("", "rastrillo-cli-"); cliErr != nil {
			return
		}
		cliPath = filepath.Join(cliDir, "rastrillo")
		cmd := exec.Command("go", "build", "-o", cliPath, "./cmd/rastrillo")
		cmd.Dir = RepoRoot(t)
		cmd.Env = Env()
		if out, err := cmd.CombinedOutput(); err != nil {
			cliErr = fmt.Errorf("building the CLI: %v\n%s", err, out)
		}
	})
	if cliErr != nil {
		t.Fatal(cliErr)
	}
	return cliPath
}

// Module builds a standalone module in t.TempDir() that requires and
// replaces amadan.net/rastrillo/rastrillo with this repo, so RunSqlc
// and the compiled store it produces have a real module to work in.
// withTool controls whether the go.mod carries the sqlc tool directive
// from the start.
func Module(t *testing.T, withTool bool) string {
	t.Helper()
	root := t.TempDir()
	goMod := "module scratch\n\ngo 1.25.0\n\n"
	if withTool {
		goMod += "tool github.com/sqlc-dev/sqlc/cmd/sqlc\n\n"
	}
	goMod += "require amadan.net/rastrillo/rastrillo v0.0.0\n\n" +
		"replace amadan.net/rastrillo/rastrillo => " + RepoRoot(t) + "\n"
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// App is a module named scratch (so its stores are
// scratch/gen/store/<name>) with the sqlc tool and files written
// relative to its root. It skips under -short, and when the tool
// cannot be fetched.
func App(t *testing.T, files map[string]string) string {
	t.Helper()
	if testing.Short() {
		t.Skip("builds a scratch app")
	}
	app := Module(t, true)
	get := exec.Command("go", "get", "-tool", "github.com/sqlc-dev/sqlc/cmd/sqlc")
	get.Dir = app
	get.Env = Env()
	if out, err := get.CombinedOutput(); err != nil {
		t.Skipf("go get -tool sqlc failed (likely a network issue): %v\n%s", err, out)
	}
	for rel, content := range files {
		Write(t, app, rel, content)
	}
	return app
}

// ResourceTOML is the smallest manifest: one searchable Title column
// and a required Title field, on the store given.
func ResourceTOML(name, store string) string {
	return fmt.Sprintf(`name  = %q
route = "/%s"
store = %q

[list]
columns = [{ field = "Title" }]

[form]
basics = [{ name = "Title", required = true }]
`, name, name, store)
}

// Write puts content at rel under app, creating directories as needed.
func Write(t *testing.T, app, rel, content string) {
	t.Helper()
	path := filepath.Join(app, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// WriteMain writes cmd/app/main.go around wiring, which may use db
// (the app's *sql.DB) and must define mux.
func WriteMain(t *testing.T, app, wiring string) {
	t.Helper()
	Write(t, app, "cmd/app/main.go", `package main

import (
	"context"
	"net/http"

	"amadan.net/rastrillo/rastrillo"
	"scratch/gen"
)

var _ = context.Background

func main() {
	db, err := rastrillo.OpenDB("app.db", nil)
	if err != nil {
		panic(err)
	}
	`+wiring+`
	_ = mux
}
`)
}

// Run runs name with args in app under Env, returning its combined output.
func Run(app string, name string, args ...string) (string, error) {
	return RunWith(app, Env(), name, args...)
}

// RunWith is Run with an environment of the caller's choosing.
func RunWith(app string, env []string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = app
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// Generate runs `rastrillo generate` over app and fails the test if it
// does not succeed.
func Generate(t *testing.T, app string) {
	t.Helper()
	if out, err := Run(app, CLI(t), "generate", app); err != nil {
		t.Fatalf("rastrillo generate: %v\n%s", err, out)
	}
}

// Check runs `rastrillo generate --check`; its error carries the
// command's output, so a test can look for the file it names. It
// builds the CLI if no test has yet, and fails the test if it cannot:
// returning that as the command's error would let a test that expects
// --check to fail pass without running it.
func Check(t *testing.T, app string) error {
	t.Helper()
	if out, err := Run(app, CLI(t), "generate", "--check", app); err != nil {
		return fmt.Errorf("%v\n%s", err, out)
	}
	return nil
}

// GoBuild builds every package in app.
func GoBuild(app string) (string, error) { return Run(app, "go", "build", "./...") }

// GoTest runs pkg's tests in app, uncached. Under -race it passes the
// detector through: a scratch test that races two requests on one
// Ctx proves nothing about the data race unless the scratch app's own
// build carries the detector, which needs cgo.
func GoTest(app, pkg string) (string, error) {
	if !raceDetector {
		return Run(app, "go", "test", "-count=1", pkg)
	}
	return RunWith(app, append(Env(), "CGO_ENABLED=1"), "go", "test", "-race", "-count=1", pkg)
}
