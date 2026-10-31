package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/action"
	"amadan.net/rastrillo/rastrillo/internal/routelist"
)

// routesFixture is one of routelist's test modules. Each replaces this
// checkout, so the command lists against the action package here.
func routesFixture(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "internal", "routelist", "testdata",
		name)
}

// routesRun runs the built command and keeps stdout apart from stderr,
// so a test can tell the table from what the app printed.
func routesRun(t *testing.T, binary string, arguments ...string) (
	status int, stdout, stderr string) {
	t.Helper()
	command := exec.Command(binary, append([]string{"routes"},
		arguments...)...)
	var out, printed bytes.Buffer
	command.Stdout, command.Stderr = &out, &printed
	command.Run()
	return command.ProcessState.ExitCode(), out.String(), printed.String()
}

func TestRoutesPrintsTheTableAndChecksEveryActionIsMounted(t *testing.T) {
	binary := buildRastrillo(t)
	app := routesFixture(t, "app")
	const pkg = "example.com/app/internal/app"
	table := routelist.Format([]action.Route{
		{Method: "GET", Path: "/", Action: pkg + ".(*App).index"},
		{Method: "GET", Path: "/admin", Action: pkg + ".(*App).adminHome",
			Guards: []string{"admin"}},
		{Method: "POST", Path: "/admin/tags/{id}",
			Action: pkg + ".(*Tags).Show", Guards: []string{"admin"}},
		{Method: "GET", Path: "/report", Action: pkg + ".Report.Show"},
		{Method: "GET", Path: "/healthz", Handler: "tagEdit"},
		{Method: "*", Path: "/api/*", Handler: "api"},
	})
	status, stdout, stderr := routesRun(t, binary, app)
	if status != 0 || stdout != table {
		t.Errorf("exit %d, stdout:\n%s\nwant exit 0 and:\n%s\nstderr:\n%s",
			status, stdout, table, stderr)
	}
	unmounted := routelist.FormatUnmounted([]string{
		pkg + ".(*App).tagEdit", pkg + ".(*Tickets).Show",
	})
	status, stdout, stderr = routesRun(t, binary, "--check", app)
	if status != 1 || stdout != table ||
		!strings.Contains(stderr, unmounted) {
		t.Errorf("--check: exit %d, stderr:\n%s\nwant exit 1 and:\n%s",
			status, stderr, unmounted)
	}
	status, _, stderr = routesRun(t, binary, routesFixture(t, "none"))
	if status != 1 || !strings.Contains(stderr, "No Routes function") ||
		!strings.Contains(stderr,
			"func Routes(routes *action.Routes, app *App)") {
		t.Errorf("no Routes: exit %d, stderr:\n%s", status, stderr)
	}
}

// processesUnder counts the running processes whose command line names
// a path under scratch, and those that start there: the go build and
// its compiler and linker work there; the listing program and the copy
// it starts are there.
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

func killUnder(scratch string) {
	entries, _ := filepath.Glob("/proc/[0-9]*/cmdline")
	for _, entry := range entries {
		cmdline, _ := os.ReadFile(entry)
		if strings.Contains(string(cmdline), scratch) {
			pid, _ := strconv.Atoi(filepath.Base(filepath.Dir(entry)))
			syscall.Kill(pid, syscall.SIGKILL)
		}
	}
}

// waitFor polls until ready holds for the processes under scratch. It
// gives up when the command ends first, so a command that fails at once
// fails the test at once, not a minute later.
func waitFor(t *testing.T, scratch string, ended <-chan error,
	ready func(all, programs int) bool) {
	t.Helper()
	for deadline := time.Now().Add(time.Minute); !ready(
		processesUnder(scratch)); time.Sleep(5 * time.Millisecond) {
		select {
		case err := <-ended:
			t.Fatalf("it ended before it was signalled: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("the processes under %s never got there", scratch)
		}
	}
}

// A signal stops the listing, so the command cleans up what a Ctrl-C
// cannot reach (the children have a process group of their own), and
// then it dies of the signal: bash stops a loop only for one that did.
func TestRoutesCleansUpWhenSignalled(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("reads running processes from /proc")
	}
	binary := buildRastrillo(t)
	module := routesFixture(t, "troubled")
	moments := map[string]func(all, programs int) bool{
		"during the build": func(all, programs int) bool {
			return all > 0 && programs == 0
		},
		"while the program runs": func(_, programs int) bool {
			return programs >= 2
		},
	}
	for moment, ready := range moments {
		for _, signal := range []syscall.Signal{syscall.SIGINT,
			syscall.SIGTERM} {
			t.Run(moment+", "+signal.String(), func(t *testing.T) {
				signalRoutes(t, binary, module, ready, signal)
			})
		}
	}
}

func signalRoutes(t *testing.T, binary, module string,
	ready func(all, programs int) bool, signal syscall.Signal) {
	t.Helper()
	modules := snapshot(t, module)
	scratch := t.TempDir()
	command := exec.Command(binary, "routes", "--package",
		"example.com/troubled/internal/sleeps", module)
	command.Env = append(os.Environ(), "TMPDIR="+scratch)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	// Killed by the signal, the command would leave its children holding
	// stderr open: Wait must not wait for them, and nor may they outlive
	// the test.
	command.WaitDelay = 5 * time.Second
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { killUnder(scratch) })
	ended := make(chan error, 1)
	go func() { ended <- command.Wait() }()
	waitFor(t, scratch, ended, ready)
	command.Process.Signal(signal)
	var exit *exec.ExitError
	err := <-ended
	if !errors.As(err, &exit) || !strings.Contains(stderr.String(),
		"Stopped before") || exit.Sys().(syscall.WaitStatus).
		Signal() != signal {
		t.Errorf("signalled, it ended %v:\n%s", err, &stderr)
	}
	// A SIGKILL is delivered at once, but the kernel ends a process in
	// its own time.
	waitFor(t, scratch, nil, func(all, _ int) bool { return all == 0 })
	if left, _ := os.ReadDir(scratch); len(left) > 0 {
		t.Errorf("%d files left in TMPDIR, first %s", len(left),
			left[0].Name())
	}
	if snapshot(t, module) != modules {
		t.Errorf("the module's go.mod or go.sum changed")
	}
}

// snapshot is the module's go.mod and go.sum, which listing must not
// touch.
func snapshot(t *testing.T, module string) string {
	t.Helper()
	var both strings.Builder
	for _, name := range []string{"go.mod", "go.sum"} {
		content, err := os.ReadFile(filepath.Join(module, name))
		if err != nil {
			t.Fatal(err)
		}
		both.Write(content)
	}
	return both.String()
}
