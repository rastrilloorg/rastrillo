package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestVectorsEndToEndOnAFixtureApp drives the whole story the way an
// app would live it: -init into a bare module, regenerate, gate
// green, then each engine mutated in turn and the gate failing the
// correct leg. The fixture resolves rastrillo to THIS checkout via a
// replace directive (the new_test.go dance), so the scaffolded
// generator compiles against the code under test, never a published
// version.
func TestVectorsEndToEndOnAFixtureApp(t *testing.T) {
	goMod := fmt.Sprintf("module fixtureapp\n\ngo 1.25.0\n\n"+
		"require github.com/carlosframework/rastrillo v0.0.0\n\n"+
		"replace github.com/carlosframework/rastrillo => %s\n", repoRoot(t))
	dir := scaffold(t, map[string]string{"go.mod": goMod})
	t.Setenv("GOFLAGS", "-mod=mod")

	if err := runVectors([]string{"-init", dir}); err != nil {
		t.Fatalf("vectors -init: %v", err)
	}

	// Resolve the fixture's dependency graph (rastrillo's own,
	// through the replace). A cold module cache needs the network —
	// the sqlc-shaped skip, not a failure.
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = dir
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Skipf("go mod tidy in the fixture failed (likely a network issue): %v\n%s", err, out)
	}

	if err := runVectors([]string{dir}); err != nil {
		t.Fatalf("vectors: %v", err)
	}
	vectorsJSON, err := os.ReadFile(filepath.Join(dir, "test", "vectors.json"))
	if err != nil {
		t.Fatalf("expected test/vectors.json: %v", err)
	}
	for _, want := range []string{`"name": "empty-log"`, `"why"`, `"events": []`} {
		if !strings.Contains(string(vectorsJSON), want) {
			t.Errorf("test/vectors.json is missing %s:\n%s", want, vectorsJSON)
		}
	}

	_, nodeErr := exec.LookPath("node")
	if nodeErr == nil {
		if err := runVectors([]string{"-check", dir}); err != nil {
			t.Fatalf("-check on a fresh -init should be green: %v", err)
		}
	}

	// Mutate the Go fold: the byte-compare leg must catch the engine
	// changing without a regenerate. This leg needs no node — leg 1
	// fails before leg 2 runs.
	genPath := filepath.Join(dir, "cmd", "genvectors", "main.go")
	genSrc, err := os.ReadFile(genPath)
	if err != nil {
		t.Fatal(err)
	}
	mutated := strings.Replace(string(genSrc), "t.Count++", "t.Count += 2", 1)
	if mutated == string(genSrc) {
		t.Fatal("mutation found nothing to change; the template's fold moved")
	}
	if err := os.WriteFile(genPath, []byte(mutated), 0o644); err != nil {
		t.Fatal(err)
	}
	err = runVectors([]string{"-check", dir})
	if err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("a changed Go engine must fail the byte-compare leg, got: %v", err)
	}

	if nodeErr != nil {
		t.Log("node not on PATH; the JS-mutation leg is not exercised")
		return
	}

	// Restore the Go engine, regenerate, then mutate the JS fold:
	// the node leg must catch the JS engine drifting.
	if err := os.WriteFile(genPath, genSrc, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runVectors([]string{dir}); err != nil {
		t.Fatalf("regenerate after restore: %v", err)
	}
	parityPath := filepath.Join(dir, "test", "parity.test.mjs")
	paritySrc, err := os.ReadFile(parityPath)
	if err != nil {
		t.Fatal(err)
	}
	jsMutated := strings.Replace(string(paritySrc), "t.count += 1", "t.count += 2", 1)
	if jsMutated == string(paritySrc) {
		t.Fatal("JS mutation found nothing to change; the template's fold moved")
	}
	if err := os.WriteFile(parityPath, []byte(jsMutated), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runVectors([]string{"-check", dir}); err == nil {
		t.Fatal("a changed JS engine must fail the node leg")
	}
}
