package routelist

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// processesUnder counts the running processes whose command line names
// a path under scratch: the go build, the compiler and linker it starts
// with their work there, and the listing program and the copy it
// starts. A process that has exited has an empty command line, so it
// is not counted while it waits to be reaped.
func processesUnder(scratch string) (all, programs int) {
	entries, _ := filepath.Glob("/proc/[0-9]*/cmdline")
	for _, entry := range entries {
		cmdline, err := os.ReadFile(entry)
		if err != nil || !strings.Contains(string(cmdline), scratch) {
			continue
		}
		all++
		if strings.HasPrefix(string(cmdline), scratch) {
			programs++
		}
	}
	return all, programs
}

// cancelWhen cancels as soon as ready says the moment has come.
func cancelWhen(
	scratch string, ready func(all, programs int) bool,
) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		defer cancel()
		for deadline := time.Now().Add(time.Minute); time.Now().
			Before(deadline); time.Sleep(5 * time.Millisecond) {
			if ready(processesUnder(scratch)) {
				return
			}
		}
	}()
	return ctx
}

func TestCancellingListCleansUp(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("reads running processes from /proc")
	}
	moments := map[string]func(all, programs int) bool{
		"during the build": func(all, programs int) bool {
			return all > 0
		},
		"while the program and its copy run": func(_, programs int) bool {
			return programs >= 2
		},
	}
	for moment, ready := range moments {
		t.Run(moment, func(t *testing.T) {
			scratch := emptyScratch(t)
			target, _ := findIn(t, "troubled",
				"example.com/troubled/internal/sleeps")
			_, err := List(cancelWhen(scratch, ready), target)
			if !errors.Is(err, context.Canceled) ||
				err.Error() != stoppedBeforeListing {
				t.Errorf("a cancelled List gave %v", err)
			}
			waitForNoneUnder(t, scratch)
		})
	}
}

// waitForNoneUnder allows the killed processes a moment to exit: a
// SIGKILL is delivered at once, but the kernel finishes a process in
// its own time.
func waitForNoneUnder(t *testing.T, scratch string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		all, _ := processesUnder(scratch)
		if all == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d processes started by List are still running",
				all)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// hangingGo puts a go first on PATH that waits 20 seconds before it
// lists pattern, so a test can cancel while go list runs: a real one
// takes a few dozen milliseconds, too short to catch. Not forever, so a
// go list that is never cancelled fails the test instead of hanging
// it. The script's own path names dir, so processesUnder sees it.
func hangingGo(t *testing.T, pattern string) string {
	t.Helper()
	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"if [ \"$1 $4\" = \"list " + pattern + "\" ]; then sleep 20; fi\n" +
		"exec " + realGo + " \"$@\"\n"
	err = os.WriteFile(filepath.Join(dir, "go"), []byte(script), 0o700)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func TestCancellingGoListStops(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("reads running processes from /proc")
	}
	running := func(all, _ int) bool { return all > 0 }
	t.Run("while Find lists the packages", func(t *testing.T) {
		dir := hangingGo(t, "./internal/...")
		_, err := Find(cancelWhen(dir, running), fixture("app"), "")
		if !errors.Is(err, context.Canceled) {
			t.Errorf("a cancelled Find gave %v", err)
		}
		wantError(t, err, stoppedBeforeListing)
		waitForNoneUnder(t, dir)
	})
	t.Run("while --check lists the packages", func(t *testing.T) {
		target, _ := findIn(t, "app", "")
		dir := hangingGo(t, "./...")
		_, err := Unmounted(cancelWhen(dir, running), target, nil)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("a cancelled Unmounted gave %v", err)
		}
		wantError(t, err, stoppedBeforeChecking)
		waitForNoneUnder(t, dir)
	})
}
