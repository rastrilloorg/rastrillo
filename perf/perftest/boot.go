package perftest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/internal/budgetfile"
	"amadan.net/rastrillo/rastrillo/perf"
)

// BootMarker is the line a Boot child prints the moment the first byte
// of its first response arrives; the parent's clock stops on it.
const BootMarker = "rastrillo-budget/v1 boot-first-byte"

// BootConfig says how to start the app cold.
type BootConfig struct {
	// Prepare returns a migrated, closed database. It runs only in the
	// parent, so no child migrates inside the window being measured.
	Prepare func(t testing.TB) string
	// Build opens dbPath and returns the app's handler and its cleanup.
	// It runs only in the child.
	Build func(dbPath string) (http.Handler, func(), error)
	// Path is requested once; Expect is the status it must answer (200
	// when zero). Redirects are not followed.
	Path   string
	Expect int
}

const (
	childLimit = 10 * time.Second
	pipeBound  = 2 * time.Second
)

// Boot measures a cold start: a new process, from exec to the first
// byte of its first response. It re-executes this test binary running
// only the calling test; the child sees BootChildEnv and does nothing
// but start. Call it from a top-level test.
func Boot(t *testing.T, cfg BootConfig) {
	t.Helper()
	if cfg.Expect == 0 {
		cfg.Expect = http.StatusOK
	}
	if p := os.Getenv(budgetfile.BootChildEnv); p != "" {
		child(t, cfg, p) // the child branch never spawns: the recursion guard
		return
	}
	if testing.Short() {
		t.Skip("perf: boot is measured in the perf lane, never under -short")
	}
	f := load(t)
	limit := perf.DefaultColdBudget
	if f.Boot != nil {
		limit = f.Boot.Limit
	}
	src := cfg.Prepare(t)
	var got []time.Duration
	for i := 0; i < 3; i++ {
		dst := filepath.Join(t.TempDir(), fmt.Sprintf("boot-%d.db", i))
		if err := copyFile(src, dst); err != nil {
			t.Fatal(err)
		}
		d, err := spawn(t.Name(), dst)
		if err != nil {
			t.Fatalf("boot child %d: %v", i, err)
		}
		got = append(got, d)
	}
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	med := got[1]
	msg := fmt.Sprintf("cold boot: median %s of %v, budget %s", med.Round(time.Millisecond), got, limit)
	switch {
	case med > limit && budgetfile.Enforce(os.Getenv):
		say("%s: over budget", msg)
		t.Errorf("%s: over budget", msg)
	case med > limit:
		say("%s: over budget (reported only; timing fails on CI)", msg)
	default:
		say("%s", msg)
	}
}

// markerWriter receives the child's stdout from exec's copying
// goroutine and stamps the moment the marker line arrives.
type markerWriter struct {
	mu    sync.Mutex
	start time.Time
	at    time.Duration
	line  []byte
	rest  bytes.Buffer
}

func (w *markerWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Since(w.start)
	for _, b := range p {
		if b != '\n' {
			w.line = append(w.line, b)
			continue
		}
		if string(w.line) == BootMarker && w.at == 0 {
			w.at = now
		} else {
			w.rest.Write(w.line)
			w.rest.WriteByte('\n')
		}
		w.line = w.line[:0]
	}
	return len(p), nil
}

func (w *markerWriter) result() (time.Duration, string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.at, w.rest.String() + string(w.line)
}

// spawn runs one child. It is killed with its whole process group at
// childLimit, and Wait stops waiting for its pipes pipeBound later, so a
// descendant holding stdout open cannot hang the parent.
func spawn(test, db string) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), childLimit)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+regexp.QuoteMeta(test)+"$", "-test.count=1")
	cmd.Env = append(os.Environ(), budgetfile.BootChildEnv+"="+db)
	out := &markerWriter{}
	var errOut bytes.Buffer // stderr is copied by its own goroutine, so it gets its own buffer
	cmd.Stdout, cmd.Stderr = out, &errOut
	cmd.WaitDelay = pipeBound
	isolate(cmd)
	out.start = time.Now()
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	werr := cmd.Wait()
	killGroup(cmd) // a descendant outliving a clean child is reaped too
	at, rest := out.result()
	diag := strings.TrimSpace(rest + "\n" + errOut.String())
	switch {
	case ctx.Err() != nil:
		return 0, fmt.Errorf("killed after %s without finishing:\n%s", childLimit, diag)
	case at == 0:
		return 0, fmt.Errorf("never printed its first-byte line (%v):\n%s", werr, diag)
	case werr != nil && !errors.Is(werr, exec.ErrWaitDelay):
		return 0, fmt.Errorf("exited %v after its first byte:\n%s", werr, diag)
	}
	return at, nil
}

func child(t *testing.T, cfg BootConfig, db string) {
	h, cleanup, err := cfg.Build(db)
	if err != nil {
		t.Fatalf("boot child: build: %v", err)
	}
	defer cleanup()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h}
	go srv.Serve(ln)
	defer srv.Close()
	var once sync.Once
	ctx := httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{GotFirstResponseByte: func() {
		// Straight to stdout, unbuffered: the parent's clock stops when
		// it reads this line.
		once.Do(func() { fmt.Fprintln(os.Stdout, BootMarker) })
	}})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+ln.Addr().String()+cfg.Path, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The first response is the one judged: following a redirect to a
	// fast sign-in page would pass a screen that never rendered.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("boot child: request: %v", err)
	}
	defer resp.Body.Close()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatalf("boot child: reading %s: %v", cfg.Path, err)
	}
	if resp.StatusCode != cfg.Expect {
		t.Fatalf("boot child: %s answered %d, want %d", cfg.Path, resp.StatusCode, cfg.Expect)
	}
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}
