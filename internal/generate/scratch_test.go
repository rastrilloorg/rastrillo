package generate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

// The scratch-app helpers drive the real `rastrillo generate` binary
// rather than this package's functions: pruning, the router and the
// --check gate are wired together in cmd/rastrillo, and a test that
// called the pieces itself would pass while the command skipped one.

// cliDir holds the one CLI build every scratch-app test shares;
// building it per test would add a link step to each.
var (
	cliOnce sync.Once
	cliDir  string
	cliPath string
	cliErr  error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if cliDir != "" {
		os.RemoveAll(cliDir)
	}
	os.Exit(code)
}

// scratchEnv is how the Makefile runs go: -mod=mod resolves module
// data the scratch go.sum does not carry, and -buildvcs=false because
// $TMPDIR may sit under a repository the scratch module is not part of.
func scratchEnv() []string {
	return append(os.Environ(), "GOFLAGS=-mod=mod -buildvcs=false", "CGO_ENABLED=0")
}

func rastrilloCLI(t *testing.T) string {
	t.Helper()
	cliOnce.Do(func() {
		if cliDir, cliErr = os.MkdirTemp("", "rastrillo-cli-"); cliErr != nil {
			return
		}
		cliPath = filepath.Join(cliDir, "rastrillo")
		cmd := exec.Command("go", "build", "-o", cliPath, "./cmd/rastrillo")
		cmd.Dir = repoRoot(t)
		cmd.Env = scratchEnv()
		if out, err := cmd.CombinedOutput(); err != nil {
			cliErr = fmt.Errorf("building the CLI: %v\n%s", err, out)
		}
	})
	if cliErr != nil {
		t.Fatal(cliErr)
	}
	return cliPath
}

// scratchApp is a module named scratch (so its stores are
// scratch/gen/store/<name>) with the sqlc tool and files written
// relative to its root.
func scratchApp(t *testing.T, files map[string]string) string {
	t.Helper()
	if testing.Short() {
		t.Skip("builds a scratch app")
	}
	app := newScratchModule(t, true)
	get := exec.Command("go", "get", "-tool", "github.com/sqlc-dev/sqlc/cmd/sqlc")
	get.Dir = app
	get.Env = scratchEnv()
	if out, err := get.CombinedOutput(); err != nil {
		t.Skipf("go get -tool sqlc failed (likely a network issue): %v\n%s", err, out)
	}
	for rel, content := range files {
		writeFile(t, app, rel, content)
	}
	return app
}

func resourceTOML(name, store string) string {
	return fmt.Sprintf(`name  = %q
route = "/%s"
store = %q

[list]
columns = [{ field = "Title" }]

[form]
basics = [{ name = "Title", required = true }]
`, name, name, store)
}

func writeFile(t *testing.T, app, rel, content string) {
	t.Helper()
	path := filepath.Join(app, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeMain writes cmd/app/main.go around wiring, which may use db
// (the app's *sql.DB) and must define mux.
func writeMain(t *testing.T, app, wiring string) {
	t.Helper()
	writeFile(t, app, "cmd/app/main.go", `package main

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

func runIn(app string, name string, args ...string) (string, error) {
	return runInWith(app, scratchEnv(), name, args...)
}

func runInWith(app string, env []string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = app
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func mustGenerate(t *testing.T, app string) {
	t.Helper()
	if out, err := runIn(app, rastrilloCLI(t), "generate", app); err != nil {
		t.Fatalf("rastrillo generate: %v\n%s", err, out)
	}
}

// checkGenerate runs `rastrillo generate --check`; its error carries
// the command's output, so a test can look for the file it names. It
// builds the CLI if no test has yet, and fails the test if it cannot:
// returning that as the command's error would let a test that expects
// --check to fail pass without running it.
func checkGenerate(t *testing.T, app string) error {
	t.Helper()
	if out, err := runIn(app, rastrilloCLI(t), "generate", "--check", app); err != nil {
		return fmt.Errorf("%v\n%s", err, out)
	}
	return nil
}

func goBuild(app string) (string, error) { return runIn(app, "go", "build", "./...") }

func goTest(app, pkg string) (string, error) { return runIn(app, "go", "test", "-count=1", pkg) }
