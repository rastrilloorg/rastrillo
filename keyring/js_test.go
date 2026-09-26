package keyring

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"amadan.net/rastrillo/rastrillo/crypto"
	"amadan.net/rastrillo/rastrillo/nodetest"
)

// TestJSEmbedded pins the embed wiring: the twin travels with the
// binary, and it is the keyring module — sibling import intact — not
// an accidental file.
func TestJSEmbedded(t *testing.T) {
	js := JS()
	if len(js) == 0 {
		t.Fatal("JS() returned no bytes; embed broken")
	}
	if !bytes.Contains(js, []byte("export function ring(")) {
		t.Fatal("JS() does not look like the keyring twin")
	}
	if !bytes.Contains(js, []byte(`from "./crypto.mjs"`)) {
		t.Fatal("JS() lost its ./crypto.mjs sibling import — the serve-time contract")
	}
}

// TestJSTwin materialises the serve-time layout — keyring.mjs beside
// crypto.mjs, the vectors one directory up — in a temp dir, then runs
// the twin's own test file through `node --test`. The layout is built
// from crypto.JS() and JS() themselves, so the test proves exactly
// what an app deploys: the two embedded modules, served as siblings.
// Skipped without node (crypto's rule): the twin is part of the
// compatibility contract, but a Go toolchain without node still gets
// a green, honest build.
func TestJSTwin(t *testing.T) {
	// Before the fixture is built: no node means none of it is needed.
	nodetest.Node(t)

	dir := t.TempDir()
	jsDir := filepath.Join(dir, "js")
	tdDir := filepath.Join(dir, "testdata")
	for _, d := range []string{jsDir, tdDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	write := func(dst string, src []byte) {
		t.Helper()
		if err := os.WriteFile(dst, src, 0o644); err != nil {
			t.Fatalf("write %s: %v", dst, err)
		}
	}
	copyFile := func(dst, src string) {
		t.Helper()
		b, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("read %s: %v", src, err)
		}
		write(dst, b)
	}

	write(filepath.Join(jsDir, "crypto.mjs"), crypto.JS())
	write(filepath.Join(jsDir, "keyring.mjs"), JS())
	copyFile(filepath.Join(jsDir, "golden.test.mjs"), filepath.Join("js", "golden.test.mjs"))
	copyFile(filepath.Join(tdDir, "golden.json"), filepath.Join("testdata", "golden.json"))

	nodetest.Run(t, nodetest.Cmd{Dir: jsDir, Args: []string{"--test", "golden.test.mjs"}})
}
