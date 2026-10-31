package tmplgen

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const hangMain = `package main

import (
	"os"
	"os/exec"
	"strconv"
	"time"

	_ "amadan.net/rastrillo/rastrillo/tmpl"
)

// pidfile is written into the source: the capture child gets no
// environment of the test's to read it from.
const pidfile = PIDFILE

func main() {
	child := exec.Command("sleep", "300")
	if err := child.Start(); err != nil {
		panic(err)
	}
	os.WriteFile(pidfile, []byte(strconv.Itoa(child.Process.Pid)), 0o644)
	time.Sleep(time.Hour)
}
`

// TestRunStopsAHangingCapture runs a main that serves instead of
// capturing, and has started a child: generate reports it, and leaves
// neither process nor scratch directory behind. It imports tmpl, as a
// main that could build a set does, or Run would not start it.
func TestRunStopsAHangingCapture(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for the capture timeout")
	}
	saved := runTimeout
	runTimeout = 2 * time.Second
	t.Cleanup(func() { runTimeout = saved })
	scratchParent := t.TempDir()
	pidfile := filepath.Join(t.TempDir(), "pid")
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	app := t.TempDir()
	writeFiles(t, app, map[string]string{
		"go.mod": goMod("example.com/hang", root),
		"cmd/hang/main.go": strings.Replace(
			hangMain,
			"PIDFILE",
			strconv.Quote(pidfile),
			1,
		),
	})
	offlineGo(t)
	run(t, app, "go", "mod", "tidy")
	t.Setenv("TMPDIR", scratchParent)

	res, err := Run(app, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.CaptureFailures) != 1 ||
		!strings.Contains(res.CaptureFailures[0].Reason, "did not finish") {
		t.Fatalf("CaptureFailures = %+v, want one timeout", res.CaptureFailures)
	}

	data, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	// The child is reparented when its parent dies, and reaped soon
	// after; a zombie is gone in all but name.
	for deadline := time.Now().Add(5 * time.Second); ; {
		stat, err := os.ReadFile(
			filepath.Join("/proc", strconv.Itoa(pid), "stat"),
		)
		if err != nil || bytes.Contains(stat, []byte(") Z ")) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the capture's child %d is still running", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}

	left, err := filepath.Glob(
		filepath.Join(scratchParent, "rastrillo-capture-*"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) > 0 {
		t.Errorf("scratch left behind: %v", left)
	}
}
