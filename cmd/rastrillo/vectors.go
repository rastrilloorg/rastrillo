package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// runVectors implements `rastrillo vectors [flags] [dir]`: Go↔JS
// parity vectors as a verb (design doc: 2026-08-23-vectors-verb).
// The derivation engine an app runs client-side exists twice by
// necessity, and two engines drifting is the E2EE bug class where a
// wrong answer looks fine — so the app's cmd/genvectors enumerates
// golden cases from the Go engine, this verb writes them to
// test/vectors.json, and the app's JS suite must reproduce every one.
//
// Flags come before the directory, exactly as runGenerate parses:
// FlagSet.Parse stops at the first non-flag argument.
func runVectors(args []string) error {
	fset := flag.NewFlagSet("vectors", flag.ContinueOnError)
	check := fset.Bool("check", false, "verify without writing: regenerate + byte-compare, then run the JS parity suite (node required)")
	initMode := fset.Bool("init", false, "scaffold cmd/genvectors, the test/ parity suite, and the go-test belt into an existing app")
	if err := fset.Parse(args); err != nil {
		return err
	}
	if *check && *initMode {
		return fmt.Errorf("-init scaffolds and -check gates; pick one")
	}

	dir := "."
	if rest := fset.Args(); len(rest) > 0 {
		dir = rest[0]
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}

	if *initMode {
		return vectorsInit(dir)
	}
	if *check {
		return vectorsCheck(dir)
	}
	return vectorsGenerate(dir)
}

// vectorsGenerate is the plain mode: run the app's own generator and
// write its stdout to test/vectors.json. The root test/ directory is
// the convention here (new for the scaffold; kass used web/test/) —
// the JS suite is neither a Go package nor a static asset, so it
// gets a home that is neither.
func vectorsGenerate(dir string) error {
	out, err := runGenvectors(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "test"), 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "test", "vectors.json")
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return err
	}
	fmt.Printf("rastrillo vectors: wrote test/vectors.json (%d bytes)\n", len(out))
	return nil
}

// runGenvectors runs the app's own `go run ./cmd/genvectors` with
// the working directory set to the app's module root — the manifest
// goEval precedent — and returns its stdout. The generator is the
// app's own package main (kass's shape verbatim): it imports the
// app's pure fold, enumerates cases, prints a vectors.Set.
// Convention over configuration: no cmd/genvectors, no vectors — the
// error says what to do about it.
func runGenvectors(dir string) ([]byte, error) {
	if _, err := os.Stat(filepath.Join(dir, "cmd", "genvectors")); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no cmd/genvectors in %s; the vectors verb runs the app's own generator — scaffold one with `rastrillo vectors -init`", dir)
		}
		return nil, err
	}
	cmd := exec.Command("go", "run", "./cmd/genvectors")
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("go run ./cmd/genvectors: %s", msg)
	}
	return stdout.Bytes(), nil
}

// vectorsCheck is the pre-ship gate, loud on purpose (spec §1.3):
// silent while iterating, failing before ship. Leg 1 regenerates and
// byte-compares against the committed test/vectors.json — a diff
// means the Go engine changed without `rastrillo vectors` in the
// same commit. Leg 2 runs the JS parity suite as an EXPLICIT file,
// never a directory: `node --test <dir>` stopped working on Node
// ≥ 21 (kass's own Makefile line is bit-rotted this way). Here a
// missing node is a FAILURE, not the skip the Go-side belt test
// allows itself, because a gate that quietly skipped one engine
// would be the drift it exists to catch. Also run by `rastrillo
// generate -check` when cmd/genvectors exists — one gate before
// ship, not two to remember.
func vectorsCheck(dir string) error {
	fresh, err := runGenvectors(dir)
	if err != nil {
		return err
	}
	committed, err := os.ReadFile(filepath.Join(dir, "test", "vectors.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no test/vectors.json to check against; run `rastrillo vectors` and commit the result")
		}
		return err
	}
	if !bytes.Equal(fresh, committed) {
		return fmt.Errorf("test/vectors.json is stale: a regenerate differs from the committed file — the Go engine changed without regenerating; run `rastrillo vectors` and commit the result in the same commit as the engine change")
	}

	node, err := exec.LookPath("node")
	if err != nil {
		return fmt.Errorf("check mode requires node to run the JS half of the gate (test/parity.test.mjs): a check that skipped one engine would be the drift it exists to catch — install node and rerun")
	}
	cmd := exec.Command(node, "--test", "test/parity.test.mjs")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("node --test test/parity.test.mjs failed — the JS engine disagrees with the Go one, or the suite is broken: %v\n%s", err, out)
	}
	fmt.Println("rastrillo vectors -check: vectors regenerate byte-identical, JS parity suite green")
	return nil
}

// vectorsInit lands in the -init task.
func vectorsInit(dir string) error {
	return fmt.Errorf("vectors -init: not implemented yet")
}
