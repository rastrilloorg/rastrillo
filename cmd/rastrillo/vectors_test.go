package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureGenvectorsSrc is a stand-in generator with no rastrillo
// dependency at all, so these tests run offline and fast: what the
// verb needs from cmd/genvectors is only "a package main that prints
// the vectors file to stdout".
const fixtureGenvectorsSrc = `package main

import "fmt"

func main() {
	fmt.Print("[\n  {\n    \"name\": \"one\",\n    \"why\": \"fixture\"\n  }\n]\n")
}
`

func TestVectorsWritesTheAppsVectors(t *testing.T) {
	dir := scaffold(t, map[string]string{
		"go.mod":                 "module demo\n\ngo 1.24\n",
		"cmd/genvectors/main.go": fixtureGenvectorsSrc,
	})
	if err := runVectors([]string{dir}); err != nil {
		t.Fatalf("runVectors: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "test", "vectors.json"))
	if err != nil {
		t.Fatalf("expected test/vectors.json: %v", err)
	}
	want := "[\n  {\n    \"name\": \"one\",\n    \"why\": \"fixture\"\n  }\n]\n"
	if string(b) != want {
		t.Errorf("test/vectors.json = %q, want %q", b, want)
	}
}

// Convention over configuration, with guidance when the convention
// is not met: no cmd/genvectors means an error that names both the
// missing piece and the one command that scaffolds it.
func TestVectorsErrorsWithGuidanceWithoutAGenerator(t *testing.T) {
	dir := scaffold(t, map[string]string{"go.mod": "module demo\n\ngo 1.24\n"})
	err := runVectors([]string{dir})
	if err == nil {
		t.Fatal("want an error: the app has no cmd/genvectors")
	}
	for _, want := range []string{"cmd/genvectors", "-init"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %s: %v", want, err)
		}
	}
}

// A generator that fails must surface its own stderr, not a bare
// exit status — the goEval precedent.
func TestVectorsSurfacesTheGeneratorsStderr(t *testing.T) {
	dir := scaffold(t, map[string]string{
		"go.mod": "module demo\n\ngo 1.24\n",
		"cmd/genvectors/main.go": `package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "vector case exploded")
	os.Exit(1)
}
`,
	})
	err := runVectors([]string{dir})
	if err == nil {
		t.Fatal("want the generator's failure to propagate")
	}
	if !strings.Contains(err.Error(), "vector case exploded") {
		t.Errorf("error should carry the generator's stderr: %v", err)
	}
}

func TestVectorsRejectsInitPlusCheck(t *testing.T) {
	if err := runVectors([]string{"-init", "-check", "."}); err == nil {
		t.Fatal("-init and -check together must be refused")
	}
}

// passingParityFixture and failingParityFixture stand in for an
// app's real suite: the -check contract under test here is "run this
// exact file and believe its exit code", not the suite's content.
const passingParityFixture = `import { test } from "node:test";
test("ok", () => {});
`

const failingParityFixture = `import { test } from "node:test";
test("no", () => { throw new Error("JS engine disagrees"); });
`

// needsNode skips a test leg that cannot run without node — the
// crypto/js_test.go posture, for the legs where a skip stays honest.
func needsNode(t *testing.T) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not on PATH; JS leg not exercised")
	}
	return node
}

func TestVectorsCheckFailsWithoutACommittedFile(t *testing.T) {
	dir := scaffold(t, map[string]string{
		"go.mod":                 "module demo\n\ngo 1.24\n",
		"cmd/genvectors/main.go": fixtureGenvectorsSrc,
	})
	err := runVectors([]string{"-check", dir})
	if err == nil {
		t.Fatal("want a failure: nothing committed to check against")
	}
	if !strings.Contains(err.Error(), "test/vectors.json") {
		t.Errorf("error should name the missing file: %v", err)
	}
}

// Leg 1: a diff means the Go engine changed without regenerating in
// the same commit. It must fail BEFORE the node leg, so this test
// needs no node at all.
func TestVectorsCheckFailsOnByteDrift(t *testing.T) {
	dir := scaffold(t, map[string]string{
		"go.mod":                 "module demo\n\ngo 1.24\n",
		"cmd/genvectors/main.go": fixtureGenvectorsSrc,
		"test/vectors.json":      "[]\n",
	})
	err := runVectors([]string{"-check", dir})
	if err == nil {
		t.Fatal("want a failure: the committed file does not match a regenerate")
	}
	if !strings.Contains(err.Error(), "stale") {
		t.Errorf("error should say the file is stale: %v", err)
	}
}

// Leg 2's precondition, spec §1.3: in check mode a missing node is a
// FAILURE, not a skip — silent while iterating, loud before ship.
// PATH is narrowed to a directory holding only go, so the go run in
// leg 1 still works while LookPath("node") cannot.
func TestVectorsCheckDemandsNode(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	dir := scaffold(t, map[string]string{
		"go.mod":                 "module demo\n\ngo 1.24\n",
		"cmd/genvectors/main.go": fixtureGenvectorsSrc,
		"test/parity.test.mjs":   passingParityFixture,
	})
	if err := runVectors([]string{dir}); err != nil {
		t.Fatalf("seeding test/vectors.json: %v", err)
	}
	binDir := t.TempDir()
	if err := os.Symlink(goBin, filepath.Join(binDir, "go")); err != nil {
		t.Skipf("cannot symlink go: %v", err)
	}
	t.Setenv("PATH", binDir)
	err = runVectors([]string{"-check", dir})
	if err == nil {
		t.Fatal("want a failure: check mode without node must be loud, never a skip")
	}
	if !strings.Contains(err.Error(), "node") {
		t.Errorf("error should say node is required: %v", err)
	}
}

func TestVectorsCheckGreen(t *testing.T) {
	needsNode(t)
	dir := scaffold(t, map[string]string{
		"go.mod":                 "module demo\n\ngo 1.24\n",
		"cmd/genvectors/main.go": fixtureGenvectorsSrc,
		"test/parity.test.mjs":   passingParityFixture,
	})
	if err := runVectors([]string{dir}); err != nil {
		t.Fatalf("seeding test/vectors.json: %v", err)
	}
	if err := runVectors([]string{"-check", dir}); err != nil {
		t.Fatalf("both legs should be green: %v", err)
	}
}

func TestVectorsCheckFailsWhenTheJSSuiteFails(t *testing.T) {
	needsNode(t)
	dir := scaffold(t, map[string]string{
		"go.mod":                 "module demo\n\ngo 1.24\n",
		"cmd/genvectors/main.go": fixtureGenvectorsSrc,
		"test/parity.test.mjs":   failingParityFixture,
	})
	if err := runVectors([]string{dir}); err != nil {
		t.Fatalf("seeding test/vectors.json: %v", err)
	}
	err := runVectors([]string{"-check", dir})
	if err == nil {
		t.Fatal("want a failure: the JS suite failed")
	}
	if !strings.Contains(err.Error(), "parity.test.mjs") {
		t.Errorf("error should name the suite that failed: %v", err)
	}
}

// Spec §1.4: when cmd/genvectors exists under the resolved app root,
// generate -check additionally runs the vectors check — one gate
// before ship, not two to remember. The stale committed file makes
// the byte-compare leg fail, so no node is needed here.
func TestGenerateCheckRunsTheVectorsGate(t *testing.T) {
	dir := scaffold(t, map[string]string{
		"go.mod":                 "module demo\n\ngo 1.24\n",
		"cmd/genvectors/main.go": fixtureGenvectorsSrc,
		"test/vectors.json":      "[]\n",
	})
	err := runGenerate([]string{"--check", dir})
	if err == nil {
		t.Fatal("want the vectors byte-compare failure to surface through generate --check")
	}
	if !strings.Contains(err.Error(), "stale") {
		t.Errorf("error should be the vectors staleness failure: %v", err)
	}
}

// No cmd/genvectors, no gate: vectors stay opt-in, and every
// existing app's --check is untouched.
func TestGenerateCheckIgnoresVectorsWithoutAGenerator(t *testing.T) {
	dir := scaffold(t, map[string]string{"go.mod": "module demo\n\ngo 1.24\n"})
	if err := runGenerate([]string{"--check", dir}); err != nil {
		t.Fatalf("an app with no cmd/genvectors must not grow a vectors gate: %v", err)
	}
}
