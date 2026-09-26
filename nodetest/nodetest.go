// Package nodetest runs Node from a Go test, for the JavaScript that
// has a Go twin or a pure-logic core: a crypto envelope both languages
// must open, a date parser, a golden corpus. Node here is a dev
// toolchain, never a build dependency — application JS stays
// browser-only, and a browser test stays the engine of record for
// anything that needs a DOM.
//
// It exists because every package that did this carried its own copy
// of the same ten lines, and the copies had drifted in the ways that
// matter: some skipped with a message naming what went unverified and
// some did not, and none could be told that a missing node is a
// failure rather than a skip. A skip is the right answer on a laptop
// without Node and the wrong one on CI, where a runner image that lost
// node would retire every JavaScript check in the tree without anybody
// deciding to. RequireEnv is that switch, read in one place.
package nodetest

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// RequireEnv names the environment variable that turns a missing node
// from a skip into a failure. Set it (to anything) wherever the suite
// must not quietly shrink — CI.
const RequireEnv = "RASTRILLO_TEST_REQUIRE_NODE"

// DefaultTimeout bounds a run whose Cmd sets none. It is a backstop
// against a script that never exits (an unresolved promise, a stdin
// read nobody feeds), not a performance assertion: a hang fails the
// test with its output instead of holding the whole package until go
// test's own ten-minute alarm, which names no culprit.
const DefaultTimeout = 2 * time.Minute

// Node returns the path of the node binary. With none on PATH it skips
// the test, naming the test so the skip says what went unverified — or
// fails it when RequireEnv is set.
func Node(t testing.TB) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv(RequireEnv) != "" {
			t.Fatalf("%s is set, so a Node test may not skip, but node is not on PATH: %v", RequireEnv, err)
		}
		t.Skipf("node is not on PATH, so %s's JavaScript is unverified here: install Node and rerun it", t.Name())
	}
	return node
}

// Cmd is one node invocation.
type Cmd struct {
	// Args are node's arguments: its flags, the script, and the
	// script's own arguments — {"--test", "js/golden.test.mjs"}, or
	// {"--import", hook, "harness.mjs", "--mode"}.
	Args []string
	// Dir is the working directory. Empty means the test's own, which
	// go test makes the package directory.
	Dir string
	// Stdin is fed to the script; nil is an empty stdin, never the
	// test binary's own.
	Stdin []byte
	// Env is added to the test process's environment, so PATH and
	// HOME survive and a caller only names what it changes (TZ=UTC).
	Env []string
	// Timeout overrides DefaultTimeout.
	Timeout time.Duration
}

// Run runs node (skipping as Node does when there is none) and returns
// its stdout. A non-zero exit or a timeout fails the test with BOTH
// streams: node --test reports its failures on stdout and a crashed
// script on stderr, so either alone hides half of what went wrong.
func Run(t testing.TB, c Cmd) []byte {
	t.Helper()
	node := Node(t)
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, node, c.Args...)
	// The timeout kills node, but a child it spawned can still hold
	// the output pipes open and Wait would block on them; WaitDelay
	// bounds that too, so the backstop is a backstop.
	cmd.WaitDelay = 5 * time.Second
	cmd.Dir = c.Dir
	cmd.Stdin = bytes.NewReader(c.Stdin)
	if len(c.Env) > 0 {
		cmd.Env = append(os.Environ(), c.Env...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		what := "node " + strings.Join(c.Args, " ")
		if ctx.Err() == context.DeadlineExceeded {
			t.Fatalf("%s: timed out after %s\nstdout:\n%s\nstderr:\n%s", what, timeout, stdout.String(), stderr.String())
		}
		t.Fatalf("%s: %v\nstdout:\n%s\nstderr:\n%s", what, err, stdout.String(), stderr.String())
	}
	return stdout.Bytes()
}
