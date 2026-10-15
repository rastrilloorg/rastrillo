package localecheck

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo/internal/generate/packs"
	"amadan.net/rastrillo/rastrillo/internal/manifest"
)

func TestStatic(t *testing.T) {
	app := t.TempDir()
	os.MkdirAll(filepath.Join(app, "manifest"), 0o755)
	os.MkdirAll(filepath.Join(app, "gen", "locales"), 0o755)
	// de: the framework's own strings are not translated into it, so
	// locales/de.toml is required even with no app strings (Task 6's rule).
	os.WriteFile(filepath.Join(app, "manifest", "app.toml"), []byte("locales = [\"en\", \"de\"]\n"), 0o644)
	if _, err := packs.Emit(filepath.Join(app, "gen"), manifest.App{Locales: []string{"en"}, DefaultLocale: "en"}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(Static(app), "\n")
	if !strings.Contains(got, "run rastrillo generate") || !strings.Contains(got, "locales/de.toml") {
		t.Errorf("Static = %q; want the stale packs file and the missing de catalog", got)
	}
}

// An app with no manifest/ is not a generated app: it has no
// gen/locales/packs.go to be stale, so Static claims nothing about it.
func TestStaticSkipsAnAppWithoutAManifest(t *testing.T) {
	if got := Static(t.TempDir()); len(got) != 0 {
		t.Errorf("Static on an app with no manifest/ = %q; want nothing", got)
	}
}

// The probe runs in the app's own binary and opens nothing: run it on
// a scaffolded app whose database file does not exist, and the file
// must still not exist afterwards (spec §12 item 12).
func TestProbeOpensNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a scaffolded app")
	}
	app := scaffold(t) // `rastrillo new` into t.TempDir(), via the CLI built from this checkout
	report, problems, err := Probe(context.Background(), app, "v0.0.0", "v9.9.9")
	if err != nil || len(problems) != 0 || !strings.Contains(string(report), `"ok":true`) {
		t.Fatalf("probe: %s, %v, %v", report, problems, err)
	}
	t.Logf("report: %s", report)
	matches, _ := filepath.Glob(filepath.Join(app, "*.db*"))
	if len(matches) != 0 {
		t.Errorf("the probe created %v", matches)
	}
}

// An app whose binary does not link the probe (one on a rastrillo
// released before it) would ignore RASTRILLO_CHECK_CONFIG and start for
// real: database, migrations, listener. So it is never executed, and
// the report says why.
func TestProbeNeverRunsABinaryWithoutTheProbe(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go list")
	}
	app := fakeApp(t, "oldapp", false, `package main

import "os"

func main() { os.WriteFile("ran", nil, 0o644) }
`)
	report, problems, err := Probe(context.Background(), app, "v0.20.0", "v0.29.0")
	if err != nil || len(problems) != 0 {
		t.Fatalf("Probe = %q, %v; want no problems", problems, err)
	}
	if want := "skipped the configuration probe: this app's rastrillo is v0.20.0, and the probe needs v0.29.0 or later; upgrade, then run doctor again"; !strings.Contains(string(report), want) {
		t.Errorf("report %q lacks %q", report, want)
	}
	if _, err := os.Stat(filepath.Join(app, "ran")); err == nil {
		t.Error("the app's binary ran")
	}
}

// Only cmd/<module name> is the app. Anything else under cmd/ may be a
// tool (cmd/genvectors writes files), so with no cmd/<module name>
// nothing runs.
func TestProbeRunsOnlyTheCommandNamedAfterTheModule(t *testing.T) {
	if testing.Short() {
		t.Skip("builds an app")
	}
	app := fakeApp(t, "shop", true, `package main

import (
	"os"

	_ "amadan.net/rastrillo/rastrillo"
)

func main() { os.WriteFile("ran", nil, 0o644) }
`)
	if err := os.Rename(filepath.Join(app, "cmd", "shop"), filepath.Join(app, "cmd", "genvectors")); err != nil {
		t.Fatal(err)
	}
	report, problems, err := Probe(context.Background(), app, "v0.0.0", "v0.29.0")
	if err != nil || len(problems) != 0 || !strings.Contains(string(report), "no cmd/shop/main.go to run the configuration probe against; nothing was checked") {
		t.Errorf("Probe = %s, %q, %v; want the no-command note", report, problems, err)
	}
	if _, err := os.Stat(filepath.Join(app, "ran")); err == nil {
		t.Error("cmd/genvectors ran")
	}
}

// The probe's own exit code reaches the operator: 1 for problems, 2
// for a bad RASTRILLO_CHECK_CONFIG value. A build failure and a probe
// that never finishes are named too.
func TestProbeReportsAFailingBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("builds an app")
	}
	app := fakeApp(t, "p", true, `package main

import (
	"fmt"
	"os"
	"time"

	_ "amadan.net/rastrillo/rastrillo"
)

func main() {
	switch os.Getenv("FAKE_PROBE") {
	case "1":
		fmt.Println(`+"`"+`{"rastrillo_check_config":1,"ok":false,"problems":["no pack"]}`+"`"+`)
		os.Exit(1)
	case "2":
		fmt.Fprintln(os.Stderr, "set it to 1")
		os.Exit(2)
	}
	time.Sleep(time.Hour)
}
`)
	for code, want := range map[string]string{
		"1": "the configuration probe failed (exit 1): no pack",
		"2": "the configuration probe failed (exit 2): set it to 1",
	} {
		t.Setenv("FAKE_PROBE", code)
		_, problems, err := Probe(context.Background(), app, "v0.0.0", "v0.29.0")
		if err != nil || len(problems) != 1 || problems[0] != want {
			t.Errorf("exit %s: Probe = %q, %v; want %q", code, problems, err, want)
		}
	}

	t.Setenv("FAKE_PROBE", "hang")
	defer func(d time.Duration) { probeTimeout = d }(probeTimeout)
	probeTimeout = 2 * time.Second
	_, problems, err := Probe(context.Background(), app, "v0.0.0", "v0.29.0")
	if want := "the configuration probe did not finish within 2s and was stopped"; err != nil || len(problems) != 1 || problems[0] != want {
		t.Errorf("hang: Probe = %q, %v; want %q", problems, err, want)
	}

	os.WriteFile(filepath.Join(app, "cmd", "p", "broken.go"), []byte("package main\n\nvar x int = \"s\"\n"), 0o644)
	_, problems, err = Probe(context.Background(), app, "v0.0.0", "v0.29.0")
	if err != nil || len(problems) != 1 || !strings.HasPrefix(problems[0], "could not build ./cmd/p to run the configuration probe: ") {
		t.Errorf("broken: Probe = %q, %v; want the build failure", problems, err)
	}
}

// fakeApp writes module example.com/<name> with cmd/<name>/main.go.
// linked apps require this checkout (whose root package links the
// probe) and are tidied offline; others depend on nothing, like an app
// on a rastrillo from before the probe.
func fakeApp(t *testing.T, name string, linked bool, mainSrc string) string {
	t.Helper()
	app := t.TempDir()
	mod := "module example.com/" + name + "\n\ngo 1.25.0\n"
	if linked {
		offlineGo(t)
		root, err := filepath.Abs("../..")
		if err != nil {
			t.Fatal(err)
		}
		mod += "\nrequire amadan.net/rastrillo/rastrillo v0.0.0\n\nreplace amadan.net/rastrillo/rastrillo => " + root + "\n"
	}
	os.WriteFile(filepath.Join(app, "go.mod"), []byte(mod), 0o644)
	os.MkdirAll(filepath.Join(app, "cmd", name), 0o755)
	os.WriteFile(filepath.Join(app, "cmd", name, "main.go"), []byte(mainSrc), 0o644)
	if linked {
		run(t, app, "go", "mod", "tidy")
	}
	return app
}

// offlineGo makes the module cache the proxy, which keeps tidy
// offline: the sandbox has no network, and a checkout's tests must not
// need one.
func offlineGo(t *testing.T) {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Fatalf("go env GOMODCACHE: %v", err)
	}
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOPRIVATE", "*")
	t.Setenv("GOPROXY", "file://"+strings.TrimSpace(string(out))+"/cache/download")
}

// scaffold builds the CLI from this checkout, runs `rastrillo new
// probeapp` in a temp directory (new scaffolds into its working
// directory), and points the app at this checkout, as
// cmd/rastrillo/new_scaffold_test.go does.
func scaffold(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	offlineGo(t)

	bin := filepath.Join(t.TempDir(), "rastrillo")
	run(t, root, "go", "build", "-buildvcs=false", "-o", bin, "./cmd/rastrillo")
	work := t.TempDir()
	run(t, work, bin, "new", "probeapp")
	app := filepath.Join(work, "probeapp")
	f, err := os.OpenFile(filepath.Join(app, "go.mod"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\nreplace amadan.net/rastrillo/rastrillo => " + root + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	run(t, app, "go", "mod", "tidy")
	return app
}

func run(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}
