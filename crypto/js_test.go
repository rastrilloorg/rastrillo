package crypto

import (
	"testing"

	"amadan.net/rastrillo/rastrillo/nodetest"
)

// TestJSTwin runs the JS twin's own test file (js/golden.test.mjs) —
// the same pinned vectors, through WebCrypto — when node is on PATH.
// Skipped otherwise: the twin is part of the compatibility contract,
// but a Go toolchain without node still gets a green, honest build.
func TestJSTwin(t *testing.T) {
	nodetest.Run(t, nodetest.Cmd{Args: []string{"--test", "js/golden.test.mjs"}})
}
