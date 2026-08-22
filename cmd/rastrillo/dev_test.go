package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWatchDirsIncludesInternal(t *testing.T) {
	var found bool
	for _, d := range watchDirs {
		if d == "internal" {
			found = true
		}
	}
	if !found {
		t.Fatal("watchDirs must include internal/: since the middle-layer pivot, models, handlers and migrations all live there")
	}
}

func TestDriftMessageNamesTheCommand(t *testing.T) {
	msg := driftMessage([]string{"ALTER TABLE notes ADD COLUMN archived numeric;"})
	if !strings.Contains(msg, "rastrillo migration generate") {
		t.Fatalf("drift message = %q, want it to name the fix", msg)
	}
	if !strings.Contains(msg, "archived") {
		t.Fatalf("drift message = %q, want it to show what changed", msg)
	}
}

// --- driftChecker: the background scheduler that keeps warnOnDrift's
// slow `go run` off the rebuild/restart path. These tests drive it
// with a fake compute function so they don't need a real fixture app
// — the property under test is the scheduling, not loadPayload. ---

// TestDriftCheckerNeverOverlaps guards the reason a single worker
// goroutine (rather than one goroutine per request) exists at all:
// loadPayload writes into a fixed directory name inside the app
// module (rastrillo_migration_dump) and removes it when done, so two
// concurrent computations for the same app would race on that
// directory. If a future edit changed driftChecker to spawn a
// goroutine per request, this test would catch the regression even
// though the race itself is too timing-dependent to catch directly.
func TestDriftCheckerNeverOverlaps(t *testing.T) {
	started := make(chan struct{}, 10)
	proceed := make(chan struct{})
	var running int32
	var mu sync.Mutex
	var overlapped bool
	compute := func(dir string) string {
		mu.Lock()
		running++
		if running > 1 {
			overlapped = true
		}
		mu.Unlock()
		started <- struct{}{}
		<-proceed
		mu.Lock()
		running--
		mu.Unlock()
		return ""
	}
	dc := newDriftChecker(compute, func(string) {})
	defer dc.close()

	dc.requestAfterRebuild("d")
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first compute never started")
	}
	// Queued while the first request is still blocked in compute — a
	// stand-in for a developer saving again before the previous drift
	// check finished.
	dc.requestAfterRebuild("d")
	dc.requestAfterRebuild("d")
	close(proceed)

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("second compute never ran")
	}
	if overlapped {
		t.Fatal("two drift computations ran concurrently")
	}
}

// TestDriftCheckerDropsStalePrint verifies the other half of the
// safety property: a slow check for a build that has since been
// superseded by a newer rebuild must never print, even though it was
// already running when the newer request arrived. Printing it would
// show a developer a diagnosis of a build that no longer exists.
func TestDriftCheckerDropsStalePrint(t *testing.T) {
	firstStarted := make(chan struct{})
	unblockFirst := make(chan struct{})
	compute := func(dir string) string {
		if dir == "first" {
			close(firstStarted)
			<-unblockFirst
			return "STALE MESSAGE"
		}
		return "FRESH MESSAGE"
	}
	var mu sync.Mutex
	var printed []string
	dc := newDriftChecker(compute, func(msg string) {
		mu.Lock()
		printed = append(printed, msg)
		mu.Unlock()
	})
	defer dc.close()

	dc.requestAfterRebuild("first")
	select {
	case <-firstStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("first compute never started")
	}
	// A newer build lands and requests its own check before the first
	// one has finished computing.
	dc.requestAfterRebuild("second")
	close(unblockFirst)

	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		n := len(printed)
		mu.Unlock()
		if n >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for a print")
		case <-time.After(10 * time.Millisecond):
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(printed) != 1 || printed[0] != "FRESH MESSAGE" {
		t.Fatalf("printed = %v, want only [FRESH MESSAGE] — the stale check must never print", printed)
	}
}

func TestFindAppPkg(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "cmd", "helloworld"), 0o755); err != nil {
		t.Fatal(err)
	}
	pkg, name, err := findAppPkg(dir)
	if err != nil {
		t.Fatal(err)
	}
	if pkg != "./cmd/helloworld" || name != "helloworld" {
		t.Fatalf("got (%q, %q), want (./cmd/helloworld, helloworld)", pkg, name)
	}
}

func TestFindAppPkgNoCmdDir(t *testing.T) {
	_, _, err := findAppPkg(t.TempDir())
	if err == nil {
		t.Fatal("want error for missing cmd/, got nil")
	}
	if !strings.Contains(err.Error(), "cmd/") {
		t.Fatalf("error should mention cmd/: %v", err)
	}
}

func TestFindAppPkgAmbiguous(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"one", "two"} {
		if err := os.MkdirAll(filepath.Join(dir, "cmd", n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_, _, err := findAppPkg(dir)
	if err == nil {
		t.Fatal("want error for ambiguous cmd/, got nil")
	}
}

func TestFindAppPkgIgnoresFiles(t *testing.T) {
	// A stray file under cmd/ (README, .DS_Store) must not count.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "cmd", "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmd", "README.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	pkg, name, err := findAppPkg(dir)
	if err != nil {
		t.Fatal(err)
	}
	if pkg != "./cmd/app" || name != "app" {
		t.Fatalf("got (%q, %q), want (./cmd/app, app)", pkg, name)
	}
}

func TestSplitAppArgs(t *testing.T) {
	own, app := splitAppArgs([]string{"mydir", "--", "-addr", ":9000"})
	if len(own) != 1 || own[0] != "mydir" {
		t.Fatalf("own = %v, want [mydir]", own)
	}
	if len(app) != 2 || app[0] != "-addr" || app[1] != ":9000" {
		t.Fatalf("app = %v, want [-addr :9000]", app)
	}

	own, app = splitAppArgs([]string{"--", "-addr", ":9000"})
	if len(own) != 0 || len(app) != 2 {
		t.Fatalf("got (%v, %v), want ([], [-addr :9000])", own, app)
	}

	own, app = splitAppArgs(nil)
	if len(own) != 0 || len(app) != 0 {
		t.Fatalf("got (%v, %v), want ([], [])", own, app)
	}
}

func TestParseDevArgs(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantDir    string
		wantApp    []string
		wantHelp   bool
		wantErrSub string // non-empty: err must be non-nil and contain this
	}{
		{name: "no args defaults to .", args: nil, wantDir: "."},
		{name: "dir only", args: []string{"myapp"}, wantDir: "myapp"},
		{
			name:    "dir and app args",
			args:    []string{"myapp", "--", "-addr", ":9000"},
			wantDir: "myapp",
			wantApp: []string{"-addr", ":9000"},
		},
		{
			name:    "app args only, no dir",
			args:    []string{"--", "-addr", ":9000"},
			wantDir: ".",
			wantApp: []string{"-addr", ":9000"},
		},
		{
			name:    "dash h after separator passed to app",
			args:    []string{".", "--", "-h"},
			wantDir: ".",
			wantApp: []string{"-h"},
		},
		{
			name:       "leading dash rejected",
			args:       []string{"-addr", ":9000"},
			wantErrSub: `pass app flags after "--"`,
		},
		{name: "help -h", args: []string{"-h"}, wantHelp: true},
		{name: "help -help", args: []string{"-help"}, wantHelp: true},
		{name: "help --help", args: []string{"--help"}, wantHelp: true},
		{
			name:       "extra arg after dir rejected",
			args:       []string{"myapp", "-addr", ":9000"},
			wantErrSub: `rastrillo dev myapp --`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, appArgs, help, err := parseDevArgs(tt.args)
			if tt.wantErrSub != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrSub) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErrSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if help != tt.wantHelp {
				t.Fatalf("help = %v, want %v", help, tt.wantHelp)
			}
			if tt.wantHelp {
				return
			}
			if dir != tt.wantDir {
				t.Fatalf("dir = %q, want %q", dir, tt.wantDir)
			}
			if len(appArgs) != len(tt.wantApp) {
				t.Fatalf("appArgs = %v, want %v", appArgs, tt.wantApp)
			}
			for i := range appArgs {
				if appArgs[i] != tt.wantApp[i] {
					t.Fatalf("appArgs = %v, want %v", appArgs, tt.wantApp)
				}
			}
		})
	}
}

func TestDevUsage(t *testing.T) {
	// devUsage must go to stdout, not stderr — a help request is not an
	// error.
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	devUsage()
	w.Close()
	os.Stdout = old

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if !strings.Contains(got, "rastrillo dev [dir]") {
		t.Fatalf("usage should show the command form, got: %s", got)
	}
	if !strings.Contains(got, `"--"`) {
		t.Fatalf("usage should explain --, got: %s", got)
	}
}

// TestDriftCheckerCloseWaitsForInFlightCheck guards the leftover-
// directory risk close() exists to close off: loadPayload's throwaway
// loader lives at a fixed path inside the app's own module and is
// removed by a defer inside the very compute call in flight when
// shutdown happens. If close() returned the moment the request
// channel was closed, a `rastrillo dev` process that doesn't outlive
// that in-flight `go run` (e.g. a bare SIGTERM to just this process,
// not its process group) would leave that directory behind in the
// user's repository.
func TestDriftCheckerCloseWaitsForInFlightCheck(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	compute := func(dir string) string {
		close(started)
		<-release
		return ""
	}
	dc := newDriftChecker(compute, func(string) {})

	dc.requestAfterRebuild("d")
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("compute never started")
	}

	closeDone := make(chan struct{})
	go func() {
		dc.close()
		close(closeDone)
	}()

	// The worker is still blocked inside compute; close() must not
	// have returned yet.
	select {
	case <-closeDone:
		t.Fatal("close returned before the in-flight compute finished")
	case <-time.After(200 * time.Millisecond):
	}

	close(release)

	select {
	case <-closeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("close never returned after the in-flight compute finished")
	}
}

// TestDriftCheckerCloseTimesOutOnWedgedCheck is the other half: a
// check that never returns (a hung `go run`) must not hang `rastrillo
// dev`'s own shutdown forever. shutdownTimeout is a field precisely
// so this test can shrink it instead of waiting out the real 5s
// against a deliberately wedged worker.
func TestDriftCheckerCloseTimesOutOnWedgedCheck(t *testing.T) {
	block := make(chan struct{}) // never closed: a permanently wedged check
	started := make(chan struct{})
	compute := func(dir string) string {
		close(started)
		<-block
		return ""
	}
	dc := newDriftChecker(compute, func(string) {})
	dc.shutdownTimeout = 50 * time.Millisecond

	dc.requestAfterRebuild("d")
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("compute never started")
	}

	done := make(chan struct{})
	go func() {
		dc.close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("close did not return within its bounded shutdown timeout")
	}
}
