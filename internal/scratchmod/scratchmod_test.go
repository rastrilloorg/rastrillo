package scratchmod

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// repoRoot returns this repo's absolute root from this file's own
// location, so it holds regardless of how `go test` was invoked.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(file), "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// The whole point of the package: the scratch module must carry the
// checkout's own dependency set, not just a bare require on rastrillo.
// A go.mod naming only rastrillo is what forced CI to run -mod=mod, and
// that setting is what hid a missing go.sum entry for four releases.
func TestWriteCarriesTheCheckoutsRequirements(t *testing.T) {
	root := repoRoot(t)
	dir := t.TempDir()
	if err := Write(dir, "scratch", root); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	gomod := string(got)

	if !strings.HasPrefix(gomod, "module scratch\n") {
		t.Errorf("go.mod must open with the scratch module's own name:\n%s", gomod)
	}
	if strings.Contains(gomod, "module "+Path) {
		t.Errorf("the checkout's own module line must not survive:\n%s", gomod)
	}
	if !strings.Contains(gomod, "require "+Path+" v0.0.0") {
		t.Errorf("go.mod must require rastrillo:\n%s", gomod)
	}
	if !strings.Contains(gomod, "replace "+Path+" => "+root) {
		t.Errorf("go.mod must replace rastrillo with the checkout:\n%s", gomod)
	}

	// Every requirement the checkout declares must be present, or a
	// nested build resolves none of them under -mod=readonly. Checked
	// against the checkout's go.mod rather than a hardcoded list, so
	// this keeps holding as the root module's dependencies change.
	rootMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(rootMod), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "github.com/") && !strings.HasPrefix(line, "gorm.io/") &&
			!strings.HasPrefix(line, "golang.org/") && !strings.HasPrefix(line, "modernc.org/") {
			continue
		}
		mod := strings.Fields(line)[0]
		if !strings.Contains(gomod, mod) {
			t.Errorf("scratch go.mod is missing the checkout's requirement %q", mod)
		}
	}

	// And the sums, or every one of those requirements is unverifiable.
	wantSum, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	gotSum, err := os.ReadFile(filepath.Join(dir, "go.sum"))
	if err != nil {
		t.Fatalf("Write must produce a go.sum: %v", err)
	}
	if string(gotSum) != string(wantSum) {
		t.Error("go.sum must be the checkout's, verbatim")
	}
}

// Directives are how the sqlc callers get their tool line; they must
// land as their own lines rather than inside a require block.
func TestWriteInsertsDirectives(t *testing.T) {
	dir := t.TempDir()
	const tool = "tool github.com/sqlc-dev/sqlc/cmd/sqlc"
	if err := Write(dir, "demo", repoRoot(t), tool); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "\n"+tool+"\n") {
		t.Errorf("go.mod missing the directive on its own line:\n%s", got)
	}
}

func TestWithoutModuleLine(t *testing.T) {
	const in = "module example.com/x\n\ngo 1.25.0\n\nrequire foo v1.0.0\n"
	got := withoutModuleLine(in)
	if strings.Contains(got, "module example.com/x") {
		t.Errorf("module line survived: %q", got)
	}
	if !strings.Contains(got, "go 1.25.0") || !strings.Contains(got, "require foo v1.0.0") {
		t.Errorf("everything after the module line must be kept verbatim: %q", got)
	}
}
