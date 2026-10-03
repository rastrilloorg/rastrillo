package perftest

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/internal/budgetfile"
)

// The Boot fixtures are tests in this package that only run when the
// selector variable names them; the asserting tests run this test binary
// again with that variable set, so each fixture is a real parent that
// spawns real children.
const fixtureEnv = "PERFTEST_BOOT_FIXTURE"

func fixture(t *testing.T, name string) {
	if os.Getenv(fixtureEnv) != name {
		t.Skip("a Boot fixture, run by its asserting test")
	}
}

// slowInit makes package initialisation take 600ms in the slow-init
// fixture, which a cold boot must count: init runs between exec and the
// first response, in every child.
var _ = func() int {
	if os.Getenv(fixtureEnv) == "slow-init" {
		time.Sleep(600 * time.Millisecond)
	}
	return 0
}()

var built int // package-level: zero in every fresh process

func prepare(t testing.TB) string {
	p := filepath.Join(t.TempDir(), "db")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func fast(path string) (http.Handler, func(), error) {
	built++
	if built != 1 {
		return nil, nil, os.ErrExist // a reused process, not a cold one
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "x" {
		return nil, nil, os.ErrNotExist // the parent's prepared copy did not arrive
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }), func() {}, nil
}

func TestBootFixtureFresh(t *testing.T) {
	fixture(t, "fresh")
	Boot(t, BootConfig{Prepare: prepare, Build: fast, Path: "/"})
}

func TestBootFixtureSlowInit(t *testing.T) {
	fixture(t, "slow-init")
	Boot(t, BootConfig{Prepare: prepare, Build: fast, Path: "/"})
}

// A child whose own child holds stdout open and which then never builds:
// killing only the child would leave the pipe open and the parent
// waiting forever.
func TestBootFixtureHung(t *testing.T) {
	fixture(t, "hung")
	Boot(t, BootConfig{Prepare: prepare, Path: "/", Build: func(string) (http.Handler, func(), error) {
		sleeper := exec.Command("sleep", hungSleep)
		sleeper.Stdout, sleeper.Stderr = os.Stdout, os.Stderr
		if err := sleeper.Start(); err != nil {
			return nil, nil, err
		}
		time.Sleep(time.Hour) // not select{}: the runtime would kill a deadlock at once
		return nil, nil, nil
	}})
}

// A 303 to a fast 200: the child must judge the 303 it got, not follow
// it, so it prints its first-byte line and then fails.
func TestBootFixtureRedirect(t *testing.T) {
	fixture(t, "redirect")
	Boot(t, BootConfig{Prepare: prepare, Path: "/", Build: func(string) (http.Handler, func(), error) {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/signin", http.StatusSeeOther) })
		mux.HandleFunc("GET /signin", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("sign in")) })
		return mux, func() {}, nil
	}})
}

// runFixture runs this test binary as the named fixture's parent, bounded
// so a broken kill path fails this test instead of hanging it.
func runFixture(t *testing.T, name string, env ...string) (string, error, time.Duration) {
	t.Helper()
	if testing.Short() || os.Getenv(budgetfile.BootChildEnv) != "" || os.Getenv(fixtureEnv) != "" {
		t.Skip("spawns processes")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	test := map[string]string{"fresh": "TestBootFixtureFresh", "slow-init": "TestBootFixtureSlowInit", "hung": "TestBootFixtureHung", "redirect": "TestBootFixtureRedirect"}[name]
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+test+"$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), append([]string{fixtureEnv + "=" + name, "CI=", "AMADAN_CI="}, env...)...)
	start := time.Now()
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("fixture %s ran past 90s:\n%s", name, out)
	}
	return string(out), err, time.Since(start)
}

func TestBootMeasuresAFreshProcess(t *testing.T) {
	t.Parallel()
	out, err, _ := runFixture(t, "fresh", "CI=true")
	if err != nil || !strings.Contains(out, "cold boot: median") {
		t.Fatalf("err=%v\n%s", err, out)
	}
}

func TestBootCountsPackageInit(t *testing.T) {
	t.Parallel()
	out, err, _ := runFixture(t, "slow-init", "CI=true")
	if err == nil || !strings.Contains(out, "over budget") {
		t.Fatalf("a 600ms init passed a 500ms cold budget under CI: err=%v\n%s", err, out)
	}
	out, err, _ = runFixture(t, "slow-init")
	if err != nil || !strings.Contains(out, "reported only") {
		t.Fatalf("without CI a slow start must only report: err=%v\n%s", err, out)
	}
}

// hungSleep is an unusual duration so the orphan check below can find
// this fixture's grandchild and nothing else on the machine.
const hungSleep = "617"

func TestBootKillsAHungChildAndItsDescendants(t *testing.T) {
	t.Parallel()
	out, err, took := runFixture(t, "hung")
	if err == nil || !strings.Contains(out, "killed after 10s") {
		t.Fatalf("err=%v\n%s", err, out)
	}
	if took > 30*time.Second {
		t.Fatalf("took %s: the 10s kill and 2s pipe bound did not hold", took)
	}
	// WaitDelay alone would meet the time bound and leave the grandchild
	// running; only the process-group kill reaps it.
	if pgrep, err := exec.LookPath("pgrep"); err == nil {
		if left, _ := exec.Command(pgrep, "-f", "^sleep "+hungSleep+"$").Output(); len(left) > 0 {
			exec.Command("pkill", "-f", "^sleep "+hungSleep+"$").Run()
			t.Fatalf("the hung child's grandchild survived: pid %s", strings.TrimSpace(string(left)))
		}
	}
}

func TestBootJudgesTheFirstResponseNotARedirect(t *testing.T) {
	t.Parallel()
	out, err, _ := runFixture(t, "redirect")
	if err == nil || !strings.Contains(out, "after its first byte") || !strings.Contains(out, "303") {
		t.Fatalf("err=%v\n%s", err, out)
	}
}
