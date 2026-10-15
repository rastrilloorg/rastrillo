//go:build unix

package localecheck

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A probe that hangs is stopped with everything it started: killing
// only the first process would leave its children serving, holding the
// database, with nothing left to stop them.
func TestProbeTimeoutStopsTheWholeProcessTree(t *testing.T) {
	if testing.Short() {
		t.Skip("builds an app")
	}
	app := fakeApp(t, "tree", true, `package main

import (
	"os"
	"os/exec"
	"strconv"
	"time"

	_ "amadan.net/rastrillo/rastrillo"
)

func main() {
	child := exec.Command("sleep", "3600")
	if err := child.Start(); err != nil {
		panic(err)
	}
	os.WriteFile("child.pid", []byte(strconv.Itoa(child.Process.Pid)), 0o644)
	time.Sleep(time.Hour)
}
`)
	defer func(d time.Duration) { probeTimeout = d }(probeTimeout)
	probeTimeout = 3 * time.Second
	_, problems, err := Probe(context.Background(), app, "v0.0.0", "v0.29.0")
	if err != nil || len(problems) != 1 || !strings.Contains(problems[0], "did not finish within 3s") {
		t.Fatalf("Probe = %q, %v; want the timeout", problems, err)
	}
	b, err := os.ReadFile(filepath.Join(app, "child.pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(string(b))
	for deadline := time.Now().Add(5 * time.Second); alive(pid); time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("the probe's child %d outlived the timeout", pid)
		}
	}
}

// alive treats a zombie as dead: a reaper that has not collected it yet
// does not make it a running process.
func alive(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	return err != nil || !strings.Contains(string(stat), ") Z")
}
