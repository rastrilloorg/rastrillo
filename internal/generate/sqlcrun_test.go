package generate

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/carlosframework/rastrillo"
)

// repoRoot returns this repo's absolute root, computed from this
// file's own location rather than the test's working directory, so
// it's stable regardless of how `go test` is invoked. (Same pattern as
// internal/manifest/goeval_test.go's helper of the same name — each
// package needs its own since it's unexported.)
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

// newScratchModule builds a standalone module in t.TempDir() that
// requires and replaces github.com/carlosframework/rastrillo with this
// repo, so RunSqlc and the compiled store it produces have a real
// module to work in. withTool controls whether the go.mod carries the
// sqlc tool directive from the start.
func newScratchModule(t *testing.T, withTool bool) string {
	t.Helper()
	root := t.TempDir()
	goMod := "module scratch\n\ngo 1.25.0\n\n"
	if withTool {
		goMod += "tool github.com/sqlc-dev/sqlc/cmd/sqlc\n\n"
	}
	goMod += "require github.com/carlosframework/rastrillo v0.0.0\n\n" +
		"replace github.com/carlosframework/rastrillo => " + repoRoot(t) + "\n"
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestRunSqlcMissingToolSaysHowToAddIt(t *testing.T) {
	root := newScratchModule(t, false)

	err := RunSqlc(root)
	if err == nil {
		t.Fatal("RunSqlc: want error for a module without the sqlc tool directive, got nil")
	}
	want := "go get -tool github.com/sqlc-dev/sqlc/cmd/sqlc"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err, want)
	}
}

// TestRunSqlcGeneratesCompilingStore is the slice's heaviest
// integration test: it fetches the real sqlc binary over the network
// (via the module's own tool directive), runs the store emitter's
// output for THREE resource shapes through it, emits Task 8's actions
// against the same real sqlc output, and builds the whole module. If
// the fetch fails for network reasons, this test skips rather than
// fails — Task 10 (blog adoption) exercises the identical path in CI,
// so a skip here is not silent coverage loss.
//
// Three fixtures, not two, on purpose. fixtureResource ("notes") has
// both List.Search and a List.Filter, so its Count/List queries always
// have TWO OR MORE bind parameters and sqlc always generates a Params
// struct for them. noAdvancedFixtureResource ("widgets") has neither —
// its Count query has ZERO bind parameters, and sqlc's own convention
// (discovered the hard way during Task 8's self-review, see
// task-8-report.md) is to drop the Params argument from the generated
// method entirely rather than emit an empty struct type.
// searchOnlyFixtureResource ("articles") is the third, previously
// missing corner: List.Search but no List.Filter, so its Count query
// has EXACTLY ONE bind parameter — sqlc's convention there (verified
// against this very real `sqlc generate` run, and matching the blog's
// own committed gen/store/posts/queries.sql.go, which has this same
// one-bind shape) is to pass that one parameter bare, neither wrapped
// in a Params struct (notes' shape) nor omitted (widgets' shape).
// EmitActions' index.GET builder used to emit the Params-struct call
// for this shape too (Critical 1: the earlier `countHasParams :=
// searchParam || len(fvars) > 0` gate only distinguished zero from "at
// least one", never "exactly one" from "two or more"), which fails to
// compile against real sqlc output — a mismatch a hand-written stub
// can hide but this real-sqlc round trip cannot: if a future sqlc
// version changes this convention, this test — not just
// TestEmitActionsCompile's stubs — will fail loudly.
func TestRunSqlcGeneratesCompilingStore(t *testing.T) {
	root := newScratchModule(t, true)

	getCmd := exec.Command("go", "get", "-tool", "github.com/sqlc-dev/sqlc/cmd/sqlc")
	getCmd.Dir = root
	if out, err := getCmd.CombinedOutput(); err != nil {
		t.Skipf("go get -tool sqlc failed (likely a network issue): %v\n%s", err, out)
	}

	genDir := filepath.Join(root, "gen")
	notes := fixtureResource()
	widgets := noAdvancedFixtureResource()
	articles := searchOnlyFixtureResource()
	// The two scoped fixtures are the fourth and fifth corners: real
	// sqlc must generate the exact shapes the scoped action emitter
	// binds against — GetBookmarkParams{ID, Owner}/DeleteBookmarkParams
	// (two binds -> struct), Owner fields on List/Count/Create/Update
	// Params, and journals' one-bind CountJournals taking the owner
	// bare. TestEmitActionsCompile's stubs encode the same assumption;
	// this run is what keeps the stubs honest.
	bookmarks := scopedFixtureResource()
	journals := scopedPlainFixtureResource()
	if _, err := EmitStore(genDir, []rastrillo.Resource{notes, widgets, articles, bookmarks, journals}); err != nil {
		t.Fatalf("EmitStore: %v", err)
	}

	if err := RunSqlc(root); err != nil {
		t.Fatalf("RunSqlc: %v", err)
	}

	if _, _, err := EmitActions(root, genDir, notes); err != nil {
		t.Fatalf("EmitActions(notes): %v", err)
	}
	if _, _, err := EmitActions(root, genDir, widgets); err != nil {
		t.Fatalf("EmitActions(widgets): %v", err)
	}
	if _, _, err := EmitActions(root, genDir, articles); err != nil {
		t.Fatalf("EmitActions(articles): %v", err)
	}
	if _, _, err := EmitActions(root, genDir, bookmarks); err != nil {
		t.Fatalf("EmitActions(bookmarks): %v", err)
	}
	if _, _, err := EmitActions(root, genDir, journals); err != nil {
		t.Fatalf("EmitActions(journals): %v", err)
	}

	// The point of this test over EmitStore/EmitActions' own golden
	// tests (which never invoke sqlc or the Go compiler): the emitted
	// store AND the emitted actions must actually compile together
	// once sqlc has generated real Go from the store side.
	buildCmd := exec.Command("go", "build", "./...")
	buildCmd.Dir = root
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./... in scratch module: %v\n%s", err, out)
	}
}
