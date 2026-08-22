package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/carlosframework/rastrillo/internal/devloop"
)

// watchDirs are the trees whose edits trigger the §11 loop: the design
// doc's app/, actions/, manifest/, plus cmd/ — rastrillo new scaffolds
// cmd/<name>/main.go, and a dev loop that ignores edits to it surprises
// people — plus locales/, templates/, and static/, which the app embeds into its
// binary (§9, §10, §8): without a rebuild, a saved catalog, template,
// or static asset keeps serving the copy compiled in at the last build.
// internal/ joined after the middle-layer pivot moved an app's own
// models.go, handlers.go and render.go under internal/<pkg>/ — until
// this was added, dev never rebuilt on edits to any of them. That
// matters doubly for the drift warning below: migrations/ lives under
// internal/<pkg>/ too, so a model gaining a field has no rebuild to
// hang the check off unless this directory is watched. gen/ is
// deliberately absent: it is the generator's output.
var watchDirs = []string{"actions", "app", "manifest", "cmd", "internal", "locales", "templates", "static"}

const pollInterval = 250 * time.Millisecond

// driftMessage renders the warning dev prints when an app's models
// have outrun its migrations. Generating a migration is a decision,
// not a save-side-effect, so dev only ever says so.
func driftMessage(sqls []string) string {
	var b strings.Builder
	b.WriteString("rastrillo dev: models and migrations disagree:\n")
	for _, s := range sqls {
		b.WriteString("  " + strings.TrimSpace(s) + "\n")
	}
	b.WriteString("  run: rastrillo migration generate\n")
	return b.String()
}

// computeDrift is warnOnDrift's non-printing half: it runs the same
// `go run` loadPayload always has, and reports what to say, or ""
// when there's nothing to say. Splitting the compute from the print
// lets driftChecker hold the generation check until the moment it's
// about to print, closing the window in which a stale result could
// still slip out — see driftChecker's doc comment.
//
// It is best-effort: a compile error already surfaces through the
// rebuild that triggered it, and a drift check that failed the loop
// would make the dev experience worse than the problem it reports.
func computeDrift(dir string) string {
	p, err := loadPayload(dir)
	if err != nil {
		return ""
	}
	if len(p.Changes) == 0 {
		return ""
	}
	sqls := make([]string, 0, len(p.Changes))
	for _, c := range p.Changes {
		sqls = append(sqls, c.SQL)
	}
	return driftMessage(sqls)
}

// driftRequest names the build a queued drift check belongs to.
type driftRequest struct {
	dir string
	gen int64
}

// driftChecker runs computeDrift off the rebuild/restart path: dev's
// loop rebuilds on every save (poll pollInterval), and computeDrift
// shells out to `go run` — roughly a second. Calling it inline would
// double a developer's edit-to-serving turnaround on every save,
// forever, to report something that is not urgent enough to justify
// that (a compile error already blocks the rebuild itself; drift
// doesn't). So requestAfterRebuild only ever enqueues; a single
// background goroutine does the actual work and prints the result.
//
// One worker, not one goroutine per request, for a concrete reason:
// loadPayload writes its throwaway loader into a fixed directory name
// inside the app module (rastrillo_migration_dump) and removes it
// when done. Two concurrent loadPayload calls for the same app would
// race on that directory — one's RemoveAll could delete the other's
// loader mid-`go run`. Serializing through one worker makes that
// structurally impossible rather than merely unlikely.
//
// Rapid saves must not queue up a backlog of stale checks the worker
// slowly works through while the developer watches old news scroll
// by. requestAfterRebuild keeps only the newest request pending — an
// older queued-but-not-yet-started request is dropped in favor of a
// newer one — and gen lets the worker recognize a request that was
// already in flight when a newer one arrived: it checks gen again
// immediately before printing, and stays silent if a newer build has
// since landed. Printing a diagnosis of a build that no longer exists
// would be actively misleading, not just late.
type driftChecker struct {
	compute func(dir string) string
	print   func(string)
	reqCh   chan driftRequest
	gen     atomic.Int64
	// done closes when run() returns, i.e. once reqCh is closed and
	// any request already dequeued has finished its compute/print.
	// close() waits on it so shutdown can't outrun a mid-flight check.
	done chan struct{}
	// shutdownTimeout bounds how long close() waits on done. A field
	// rather than a constant so a test can shrink it instead of
	// waiting out the real value against a deliberately wedged check.
	shutdownTimeout time.Duration
}

// driftCheckerShutdownTimeout is the production bound on close()'s
// wait: generous next to computeDrift's normal ~1s `go run`, but
// finite, so a wedged check (network hang, runaway build) can't hang
// `rastrillo dev`'s own shutdown forever. It does not risk the
// loaderDir leak close() exists to prevent — that cleanup is a
// deferred RemoveAll inside the very call being waited on, so it
// still runs whenever the stuck call eventually returns; the timeout
// only bounds how long shutdown waits around for it.
const driftCheckerShutdownTimeout = 5 * time.Second

func newDriftChecker(compute func(dir string) string, print func(string)) *driftChecker {
	d := &driftChecker{
		compute:         compute,
		print:           print,
		reqCh:           make(chan driftRequest, 1),
		done:            make(chan struct{}),
		shutdownTimeout: driftCheckerShutdownTimeout,
	}
	go d.run()
	return d
}

func (d *driftChecker) run() {
	defer close(d.done)
	for req := range d.reqCh {
		msg := d.compute(req.dir)
		if req.gen != d.gen.Load() {
			// A newer build has already landed and superseded this
			// request; whatever this one found is stale.
			continue
		}
		if msg != "" {
			d.print(msg)
		}
	}
}

// requestAfterRebuild schedules a drift check for the build that just
// started serving. It never blocks: enqueueing is either an
// immediate send into the channel's one free slot, or, if that slot
// is already occupied by an older unstarted request, a swap that
// drops that older request in favor of this one.
func (d *driftChecker) requestAfterRebuild(dir string) {
	req := driftRequest{dir: dir, gen: d.gen.Add(1)}
	for {
		select {
		case d.reqCh <- req:
			return
		default:
			select {
			case <-d.reqCh:
			default:
			}
		}
	}
}

// close stops the worker and blocks until it's done — including a
// check already mid-compute when close is called. Without that wait,
// a `rastrillo dev` shutdown that doesn't outlive the in-flight `go
// run` (a SIGTERM to just this process, not its group, never reaches
// that child; Ctrl-C's 5s grace for the app only self-heals this by
// coincidence) can leave loadPayload's throwaway loader directory
// sitting in the user's own module — its cleanup is the deferred
// RemoveAll inside the very call this wait is for. Bounded by
// shutdownTimeout so this stays a shutdown-path wait, not a hang: it
// runs once per `rastrillo dev` process, not once per save, so it
// does not reintroduce the restart-waits-on-drift-check problem
// requestAfterRebuild exists to avoid.
func (d *driftChecker) close() {
	close(d.reqCh)
	select {
	case <-d.done:
	case <-time.After(d.shutdownTimeout):
	}
}

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

	// Drift checks run in the background, never on the rebuild/restart
	// path — see driftChecker's doc comment. Requesting one here too,
	// not only after later rebuilds, means a developer who starts `dev`
	// on an app that already has drift sees the warning without having
	// to touch a file first.
	drift := newDriftChecker(computeDrift, func(msg string) { fmt.Fprint(os.Stderr, msg) })
	defer drift.close()
	drift.requestAfterRebuild(dir)

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
			// Enqueue and move on immediately — the app is already
			// serving the new build, and the loop must keep polling
			// rather than wait on a `go run`.
			drift.requestAfterRebuild(dir)
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

Watches actions/, app/, manifest/, cmd/, internal/, locales/, templates/,
and static/ (default dir: .); on any change it regenerates, rebuilds,
and restarts the app, and warns — without ever writing one — when the
app's models have outrun its migrations. Everything after "--" is
passed to the app verbatim (e.g. rastrillo dev . -- -addr :9000).
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
