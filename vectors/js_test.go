package vectors

import (
	"os/exec"
	"strings"
	"testing"
)

// TestJSHelperVocabulary pins the helper's contract the way ui's
// shim tests pin theirs: the exports the scaffolded parity suite
// imports by name, and canonical()'s exact comparison rule — the
// zero-strip included, blind spot and all — because a helper that
// quietly stopped dropping 0/false/"" would fail every app's suite
// on encoder behaviour rather than arithmetic.
func TestJSHelperVocabulary(t *testing.T) {
	js := string(JS())
	for _, want := range []string{
		"export function loadVectors",
		"export function canonical",
		"Object.keys(value).sort()",
		"if (v === undefined || v === null) continue;",
		`if (v === 0 || v === false || v === "") continue; // Go's omitempty`,
		"blind spot",
		"Empty arrays are kept",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("vectors.mjs does not contain %q", want)
		}
	}
	if strings.Contains(js, "\t") {
		t.Error("vectors.mjs uses two-space indentation, not tabs")
	}
}

// The helper must never be executed as a test by bare `node --test`
// discovery — that is why it is vectors.mjs, not *.test.mjs. Pin the
// name through the embed path.
func TestJSHelperIsNotNamedLikeATest(t *testing.T) {
	if strings.Contains(string(JS()), "node:test") {
		t.Error("the helper imports node:test; it must stay a plain module, never a test file")
	}
}

// TestJSHelperBehaviour runs the helper's own node:test suite
// (js/canonical.test.mjs) — the zero-strip cases through a real JS
// engine — when node is on PATH. Skipped otherwise: the twin is part
// of the contract, but a Go toolchain without node still gets a
// green, honest build.
func TestJSHelperBehaviour(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not on PATH; JS helper suite not exercised")
	}
	out, err := exec.Command(node, "--test", "js/canonical.test.mjs").CombinedOutput()
	if err != nil {
		t.Fatalf("node --test js/canonical.test.mjs failed: %v\n%s", err, out)
	}
}
