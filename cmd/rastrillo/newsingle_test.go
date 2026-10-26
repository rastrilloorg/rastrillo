package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// single has no sign-in, so no route groups for chi to hold: it serves
// from a stdlib ServeMux. A template that still imported chi would have
// go mod tidy put the require straight back, so this looks after tidy.
func TestSingleUsesTheStdlibMux(t *testing.T) {
	if testing.Short() {
		t.Skip("scaffolds and tidies a whole app")
	}
	setSandboxGoEnv(t)
	t.Chdir(t.TempDir())
	dir := scaffoldWithReplace(t, "plain")
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), `"github.com/go-chi/`) {
			t.Errorf("%s imports chi", strings.TrimPrefix(path, dir+"/"))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// perftest imports chi, so tidy may list it as indirect; only a
	// direct require means the app's own code reaches for it.
	for _, line := range strings.Split(readScaffold(t, dir, "go.mod"), "\n") {
		if strings.Contains(line, "github.com/go-chi/chi") && !strings.Contains(line, "// indirect") {
			t.Errorf("go.mod requires chi directly after tidy: %q", line)
		}
	}
	if app := readScaffold(t, dir, "internal", "plain", "app.go"); !strings.Contains(app, `"GET /{$}"`) {
		t.Errorf(`app.go does not serve the index at "GET /{$}":%s`, "\n"+app)
	}
}

// "GET /" on a ServeMux matches every path, so an index registered that
// way would answer an unknown URL with the home page. The scaffold's own
// test says it is a 404, and it has to pass.
func TestSingleAnswers404ForUnknownPaths(t *testing.T) {
	if testing.Short() {
		t.Skip("scaffolds, tidies and tests a whole app")
	}
	setSandboxGoEnv(t)
	t.Chdir(t.TempDir())
	dir := scaffoldWithReplace(t, "plain")
	out, err := goTestIn(dir, "-v", "-run", "^TestUnknownPathIs404$", "./internal/plain/")
	if err != nil {
		t.Fatalf("TestUnknownPathIs404 fails:\n%s", out)
	}
	if !strings.Contains(out, "--- PASS: TestUnknownPathIs404") {
		t.Fatalf("the scaffold has no TestUnknownPathIs404:\n%s", out)
	}
}

// The migration check is the app's own test now: a model changed
// without its migration fails go test with the CLI's own message, and
// running rastrillo migration generate makes it pass.
func TestMigrationCheckTestCatchesDrift(t *testing.T) {
	if testing.Short() {
		t.Skip("scaffolds, tests and generates a migration for a whole app")
	}
	setSandboxGoEnv(t)
	t.Chdir(t.TempDir())
	dir := scaffoldWithReplace(t, "drift")
	models := filepath.Join(dir, "internal", "drift", "models.go")
	src := readScaffold(t, models)
	if !strings.Contains(src, "\tBody      string\n") {
		t.Fatalf("models.go has no Body field to add beside:\n%s", src)
	}
	mustWrite(t, models, strings.Replace(src, "\tBody      string\n", "\tBody      string\n\tPinned    bool\n", 1))

	run := []string{"-run", "^TestModelsMatchMigrations$", "./internal/drift/"}
	out, err := goTestIn(dir, run...)
	if err == nil {
		t.Fatalf("a model with no migration passed TestModelsMatchMigrations:\n%s", out)
	}
	for _, want := range []string{"models and migrations disagree:\n", "pinned", "run: rastrillo migration generate"} {
		if !strings.Contains(out, want) {
			t.Errorf("the failure does not say %q:\n%s", want, out)
		}
	}
	if err := runMigration([]string{"generate", dir}); err != nil {
		t.Fatalf("rastrillo migration generate: %v", err)
	}
	if out, err := goTestIn(dir, run...); err != nil {
		t.Fatalf("TestModelsMatchMigrations still fails after generate:\n%s", out)
	}
}
