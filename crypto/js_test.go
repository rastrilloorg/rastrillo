package crypto

import (
	"os/exec"
	"testing"
)

// TestJSTwin runs the JS twin's own test file (js/golden.test.mjs) —
// the same pinned vectors, through WebCrypto — when node is on PATH.
// Skipped otherwise: the twin is part of the compatibility contract,
// but a Go toolchain without node still gets a green, honest build.
func TestJSTwin(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not on PATH; JS twin not exercised")
	}
	out, err := exec.Command(node, "--test", "js/golden.test.mjs").CombinedOutput()
	if err != nil {
		t.Fatalf("node --test js/golden.test.mjs failed: %v\n%s", err, out)
	}
}
