package main

import (
	"os"
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
