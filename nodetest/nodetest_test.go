package nodetest

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Run round-trips stdin, Args, Env and Dir through a real node.
func TestRunRoundTrips(t *testing.T) {
	out := Run(t, Cmd{
		Dir:   "testdata",
		Args:  []string{"echo.mjs", "a", "b"},
		Stdin: []byte(`{"hello":"node"}`),
		Env:   []string{"TZ=UTC"},
	})
	if got, want := string(out), `{"HELLO":"NODE"} tz=UTC args=a,b`; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

// recorder captures a Fatal or Skip instead of acting on it, so a test
// can assert on how Run failed its caller.
type recorder struct {
	testing.TB
	fatal, skip string
}

func (r *recorder) Helper() {}
func (r *recorder) Fatalf(format string, args ...any) {
	r.fatal = fmt.Sprintf(format, args...)
	runtime.Goexit()
}
func (r *recorder) Skipf(format string, args ...any) {
	r.skip = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

func record(t *testing.T, fn func(testing.TB)) *recorder {
	t.Helper()
	r := &recorder{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(r)
	}()
	<-done
	return r
}

// A failure reports both streams: node --test explains itself on
// stdout, a crash on stderr, and dropping either (Tito's first version
// kept only stderr) leaves the reader with half the story.
func TestRunFailureCarriesBothStreams(t *testing.T) {
	Node(t)
	r := record(t, func(tb testing.TB) { Run(tb, Cmd{Dir: "testdata", Args: []string{"fail.mjs"}}) })
	for _, want := range []string{"node fail.mjs", "exit status 3", "stdout-says-why", "stderr-says-why"} {
		if !strings.Contains(r.fatal, want) {
			t.Errorf("failure message lacks %q:\n%s", want, r.fatal)
		}
	}
}

// A script that never exits fails its test at the timeout, by name,
// rather than holding the package until go test's own alarm.
func TestRunTimesOut(t *testing.T) {
	Node(t)
	start := time.Now()
	r := record(t, func(tb testing.TB) {
		Run(tb, Cmd{Dir: "testdata", Args: []string{"hang.mjs"}, Timeout: 200 * time.Millisecond})
	})
	if !strings.Contains(r.fatal, "timed out after 200ms") {
		t.Fatalf("a hung script gave %q, want the timeout", r.fatal)
	}
	if d := time.Since(start); d > 30*time.Second {
		t.Fatalf("the timeout took %s to fire", d)
	}
}

// No node skips, naming the test — and fails instead under RequireEnv,
// because a CI image that lost node must not retire every JavaScript
// check in silence.
func TestMissingNode(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	t.Setenv(RequireEnv, "")
	os.Unsetenv(RequireEnv)
	r := record(t, func(tb testing.TB) { Node(tb) })
	if r.fatal != "" || !strings.Contains(r.skip, t.Name()) {
		t.Errorf("without node: skip %q, fatal %q; want a skip naming %s", r.skip, r.fatal, t.Name())
	}

	t.Setenv(RequireEnv, "1")
	r = record(t, func(tb testing.TB) { Node(tb) })
	if r.skip != "" || !strings.Contains(r.fatal, RequireEnv) {
		t.Errorf("without node under %s: skip %q, fatal %q; want a failure", RequireEnv, r.skip, r.fatal)
	}
}
