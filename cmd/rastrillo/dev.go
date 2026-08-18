package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/carlosframework/rastrillo/internal/devloop"
)

// watchDirs are the trees whose edits trigger the §11 loop: the design
// doc's app/, actions/, manifest/, plus cmd/ — rastrillo new scaffolds
// cmd/<name>/main.go, and a dev loop that ignores edits to it surprises
// people — plus locales/, templates/, and static/, which the app embeds into its
// binary (§9, §10, §8): without a rebuild, a saved catalog, template,
// or static asset keeps serving the copy compiled in at the last build. gen/ is
// deliberately absent: it is the generator's output.
var watchDirs = []string{"actions", "app", "manifest", "cmd", "locales", "templates", "static"}

const pollInterval = 250 * time.Millisecond

// runDev implements `rastrillo dev [dir] [-- app args...]` (design doc
// §11): watch, and on any change regenerate → rebuild → restart. It
// calls the same runGenerate that CI calls — one code path, not two.
// Everything after -- is passed to the app verbatim (e.g. -addr :9000).
func runDev(args []string) error {
	dir, appArgs, help, err := parseDevArgs(args)
	if err != nil {
		return err
	}
	if help {
		devUsage()
		return nil
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return err
	}

	appPkg, appName, err := findAppPkg(dir)
	if err != nil {
		return err
	}

	binDir, err := os.MkdirTemp("", "rastrillo-dev-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(binDir)
	bin := filepath.Join(binDir, appName)

	rebuild := func() error {
		if err := runGenerate([]string{dir}); err != nil {
			return err
		}
		cmd := exec.Command("go", "build", "-o", bin, appPkg)
		cmd.Dir = dir
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("go build: %w", err)
		}
		return nil
	}

	// child and exitCh are only ever touched from the main loop's
	// goroutine (the exit-watcher goroutine below only writes to its own
	// exitCh, never reads child) — no locking needed. exitCh is remade on
	// every start so a stale exit notification from a previous child can
	// never be read as the current one's.
	var child *exec.Cmd
	var exitCh chan error
	start := func() error {
		c := exec.Command(bin, appArgs...)
		c.Dir = dir
		c.Stdout, c.Stderr = os.Stdout, os.Stderr
		if err := c.Start(); err != nil {
			return err
		}
		child = c
		ch := make(chan error, 1)
		exitCh = ch
		// Own the one legal Wait() on this process: the main loop learns
		// of a self-exit via exitCh instead of polling, and stop() reads
		// from this same channel instead of calling Wait() itself, which
		// would race two waiters on one process.
		go func() { ch <- c.Wait() }()
		return nil
	}
	// stop SIGTERMs the child — rastrillo.Serve shuts down gracefully on
	// SIGTERM — and SIGKILLs only if it lingers past 5s.
	stop := func() {
		if child == nil || child.Process == nil {
			return
		}
		child.Process.Signal(syscall.SIGTERM)
		select {
		case <-exitCh:
		case <-time.After(5 * time.Second):
			child.Process.Kill()
			<-exitCh
		}
		child, exitCh = nil, nil
	}

	// The first build must succeed — fail loudly, like generate itself.
	// Snapshot before it, not after: an edit saved during the build
	// would otherwise be folded into the baseline and never trigger a
	// rebuild.
	snap, err := devloop.Snapshot(dir, watchDirs)
	if err != nil {
		return err
	}
	if err := rebuild(); err != nil {
		return fmt.Errorf("initial build: %w", err)
	}
	if err := start(); err != nil {
		return fmt.Errorf("initial start: %w", err)
	}
	defer stop()

	fmt.Printf("rastrillo dev: watching %s (poll %s)\n", strings.Join(watchDirs, ", "), pollInterval)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-sigCh:
			fmt.Println("rastrillo dev: shutting down")
			return nil
		case err := <-exitCh:
			// Nothing is serving until the next change — an app that
			// compiles but dies at startup (port already bound, a panic
			// in init) would otherwise sit invisible until someone
			// noticed. child/exitCh go nil so stop() won't later signal
			// a process that's already gone.
			if err != nil {
				fmt.Fprintf(os.Stderr, "rastrillo dev: app exited: %v — will restart on next change\n", err)
			} else {
				fmt.Fprintln(os.Stderr, "rastrillo dev: app exited (status 0) — will restart on next change")
			}
			child, exitCh = nil, nil
		case <-ticker.C:
			next, err := devloop.Snapshot(dir, watchDirs)
			if err != nil {
				fmt.Fprintf(os.Stderr, "rastrillo dev: watch: %v\n", err)
				continue
			}
			changed := devloop.Diff(snap, next)
			snap = next
			if len(changed) == 0 {
				continue
			}
			fmt.Printf("rastrillo dev: changed: %s\n", strings.Join(changed, ", "))
			if err := rebuild(); err != nil {
				// The old build keeps serving; fix and save again.
				fmt.Fprintf(os.Stderr, "rastrillo dev: %v — still serving the previous build\n", err)
				continue
			}
			stop()
			// A failed restart must not exit the loop: that would leave
			// nothing serving and require the user to manually restart
			// `rastrillo dev`, defeating §11's no-manual-intervention
			// goal. Log and keep watching; the next save retries.
			if err := start(); err != nil {
				fmt.Fprintf(os.Stderr, "rastrillo dev: start: %v — will retry on next change\n", err)
				continue
			}
		}
	}
}

// splitAppArgs separates rastrillo's own args from the app's: everything
// after the first "--" goes to the child process verbatim.
func splitAppArgs(args []string) (own, app []string) {
	for i, a := range args {
		if a == "--" {
			return args[:i], args[i+1:]
		}
	}
	return args, nil
}

// parseDevArgs validates and resolves `rastrillo dev`'s own arguments
// (everything before "--"): at most one positional directory, no bare
// flags. It does not touch the filesystem, so it's testable without
// starting the watch loop. When help is true, dir and appArgs are unset
// and the caller should print usage and return without doing anything
// else.
func parseDevArgs(args []string) (dir string, appArgs []string, help bool, err error) {
	args, appArgs = splitAppArgs(args)
	if len(args) == 0 {
		return ".", appArgs, false, nil
	}
	switch args[0] {
	case "-h", "-help", "--help":
		return "", nil, true, nil
	}
	if strings.HasPrefix(args[0], "-") {
		return "", nil, false, fmt.Errorf("unexpected flag %q before app directory — pass app flags after \"--\" (e.g. rastrillo dev -- %s)", args[0], args[0])
	}
	if len(args) > 1 {
		return "", nil, false, fmt.Errorf("unexpected argument %q after app directory — pass app flags after \"--\" (e.g. rastrillo dev %s -- %s)", args[1], args[0], args[1])
	}
	return args[0], appArgs, false, nil
}

// devUsage prints `rastrillo dev`'s own usage to stdout, for -h/-help/
// --help — a help request is not an error, so it must not go to stderr
// or set a non-zero exit code. Requested help is the command's output, so it goes to stdout — unlike main.go's usage(), which prints to stderr on the error path.
func devUsage() {
	fmt.Print(`usage: rastrillo dev [dir] [-- app args...]

Watches actions/, app/, manifest/, cmd/, locales/, templates/, and static/
(default dir: .); on any change it regenerates, rebuilds, and restarts
the app. Everything after "--" is passed to the app verbatim (e.g.
rastrillo dev . -- -addr :9000).
`)
}

// findAppPkg locates the app's main package: exactly one directory under
// cmd/, the shape `rastrillo new` scaffolds (cmd/<name>/main.go).
func findAppPkg(dir string) (pkg, name string, err error) {
	entries, err := os.ReadDir(filepath.Join(dir, "cmd"))
	if err != nil {
		return "", "", fmt.Errorf("read cmd/: %w (dev expects the rastrillo new layout: cmd/<name>/main.go)", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	if len(names) != 1 {
		return "", "", fmt.Errorf("expected exactly one directory under cmd/, found %d — dev expects the rastrillo new layout: cmd/<name>/main.go", len(names))
	}
	return "./cmd/" + names[0], names[0], nil
}
