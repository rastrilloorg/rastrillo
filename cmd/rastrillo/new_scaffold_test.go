package main

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/generate/packs"
	"amadan.net/rastrillo/rastrillo/internal/manifest"
	"amadan.net/rastrillo/rastrillo/internal/tmplmigrate"
	"amadan.net/rastrillo/rastrillo/migrate"
)

// TestNewScaffoldsCIAndManifest covers the host-awareness half of the
// scaffold: the Makefile gate, the amadan CI scripts (executable — a
// non-executable step is the runner's known silent-skip failure mode),
// the manifest/ landing spot, and the CLAUDE.md preload (§12).
func TestNewScaffoldsCIAndManifest(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)

	if err := runNew([]string{"demoapp"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}

	steps := []string{".amadan/ci", ".amadan/ci.d/10-vet", ".amadan/ci.d/15-budget", ".amadan/ci.d/20-fmt",
		".amadan/ci.d/25-staticcheck", ".amadan/ci.d/26-govulncheck", ".amadan/ci.d/27-gitleaks",
		".amadan/ci.d/30-test", ".amadan/ci.d/35-perf"}
	for _, rel := range append([]string{"Makefile", "CLAUDE.md", "manifest/README.md", ".rastrillo/budgets.txt"}, steps...) {
		if _, err := os.Stat(filepath.Join("demoapp", rel)); err != nil {
			t.Errorf("scaffold missing %s: %v", rel, err)
		}
	}

	for _, rel := range steps {
		fi, err := os.Stat(filepath.Join("demoapp", rel))
		if err != nil {
			continue
		}
		if fi.Mode()&0o111 == 0 {
			t.Errorf("%s is not executable — amadan's runner would resolve the job 'skipped'", rel)
		}
	}

	mk, _ := os.ReadFile(filepath.Join("demoapp", "Makefile"))
	if !strings.Contains(string(mk), "ci: vet fmt-check staticcheck govulncheck gitleaks budget test perf migration-check generate-check") {
		t.Fatalf("Makefile must define the one ci gate:\n%s", mk)
	}
	for _, want := range []string{
		".NOTPARALLEL:",
		"$(RASTRILLO) budget size",
		"$(RASTRILLO) budget test ./...",
		"$(RASTRILLO) budget test -no-time -require TestPerfScreens,TestPerfBoot -tags perf -count=1 -p 1 -parallel 1 -run '^TestPerf' ./...",
		"go run $(STATICCHECK) -tags browser,perf ./...",
	} {
		if !strings.Contains(string(mk), want) {
			t.Errorf("Makefile missing %q", want)
		}
	}
	for step, target := range map[string]string{"15-budget": "budget", "35-perf": "perf"} {
		if b, _ := os.ReadFile(filepath.Join("demoapp", ".amadan", "ci.d", step)); !strings.Contains(string(b), "exec make "+target+"\n") {
			t.Errorf("%s must exec make %s:\n%s", step, target, b)
		}
	}
	if !strings.Contains(string(mk), "honnef.co/go/tools/cmd/staticcheck@"+staticcheckVersion) {
		t.Fatalf("Makefile must pin staticcheck at %s, the version rastrillo's own gate runs:\n%s", staticcheckVersion, mk)
	}
	for step, target := range map[string]string{"25-staticcheck": "staticcheck", "26-govulncheck": "govulncheck", "27-gitleaks": "gitleaks"} {
		if b, _ := os.ReadFile(filepath.Join("demoapp", ".amadan", "ci.d", step)); !strings.Contains(string(b), "exec make "+target+"\n") {
			t.Fatalf("step %s must exec make %s:\n%s", step, target, b)
		}
	}
	for _, pin := range []string{
		"golang.org/x/vuln/cmd/govulncheck@" + govulncheckVersion,
		"github.com/zricethezav/gitleaks/v8@" + gitleaksVersion,
		"export GOTOOLCHAIN = " + goToolchain + "\n",
	} {
		if !strings.Contains(string(mk), pin) {
			t.Fatalf("Makefile must carry %q, the pin rastrillo's own gate uses:\n%s", pin, mk)
		}
	}
	step, _ := os.ReadFile(filepath.Join("demoapp", ".amadan", "ci.d", "10-vet"))
	if !strings.Contains(string(step), "exec make vet") {
		t.Fatalf("steps must exec Makefile targets, never their own commands:\n%s", step)
	}
	// The preload lives in AGENTS.md — the cross-agent file, so it reaches
	// whatever agent someone uses. CLAUDE.md is an @AGENTS.md import and
	// nothing else: two copies of this would drift, silently.
	agents, _ := os.ReadFile(filepath.Join("demoapp", "AGENTS.md"))
	for _, want := range []string{"ISO 4217", "scope.Owned", "SKILL.md", "CGO_ENABLED=0"} {
		if !strings.Contains(string(agents), want) {
			t.Fatalf("AGENTS.md preload missing %q:\n%s", want, agents)
		}
	}
	// The skill is found by the app's own module version, never by
	// looking in the module cache: two skilleval baseline runs searched
	// the cache, found an older copy and followed a SKILL.md that
	// predated the feature they were building (discussion #97).
	for _, want := range []string{"go list -m -f '{{.Dir}}' amadan.net/rastrillo/rastrillo", "skill/"} {
		if !strings.Contains(string(agents), want) {
			t.Fatalf("AGENTS.md must say where the app's own skill is (%q):\n%s", want, agents)
		}
	}
	if strings.Contains(string(agents), "or in the module cache") {
		t.Fatalf("AGENTS.md still sends agents to the module cache, where an older SKILL.md waits:\n%s", agents)
	}
	claude, _ := os.ReadFile(filepath.Join("demoapp", "CLAUDE.md"))
	if !strings.Contains(string(claude), "@AGENTS.md") {
		t.Fatalf("CLAUDE.md must import AGENTS.md:\n%s", claude)
	}
}

// TestNewScaffoldsTwoMigrationSets pins the shape that makes generate
// and check safe to run against a scaffold that later grows framework
// subsystems: Schema (this app's own migrations, what generate/check
// read and write) and BootSchema (everything applied at boot, Schema
// merged with whatever subsystems the app adds) must be two separate
// variables, not one. If they were the same variable, adding
// sessions.Schema to it would make generate/check see the sessions
// tables and want to drop them — they work by diffing a replay of the
// set against Models, which knows nothing about a subsystem's tables.
func TestNewScaffoldsTwoMigrationSets(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"blogapp"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	src, err := os.ReadFile(filepath.Join("blogapp", "internal", "blogapp", "migrations.go"))
	if err != nil {
		t.Fatalf("expected a scaffolded migrations.go: %v", err)
	}
	s := string(src)
	for _, want := range []string{
		"var Schema = migrate.MustFromFS(migrationFS,",
		"var BootSchema = migrate.Merge(apitoken.Schema, Schema)",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("migrations.go missing %q:\n%s", want, s)
		}
	}

	app, err := os.ReadFile(filepath.Join("blogapp", "internal", "blogapp", "app.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(app), "AutoMigrate") {
		t.Fatal("app.go still calls AutoMigrate; boot must go through migrate.Apply")
	}
	if !strings.Contains(string(app), "migrate.Apply(context.Background(), d, BootSchema)") {
		t.Errorf("app.go must apply BootSchema (the composed set), not Schema alone:\n%s", app)
	}
}

// rastrillo new must never touch the network or compile the app it
// just wrote — a scaffold that shelled out to the generator here would
// make `rastrillo new` require a working module proxy and a full
// `go build` just to produce files on disk, breaking it offline and
// making the common case slow. So the initial migration ships as a
// static template, not something new computes.
func TestNewScaffoldsStaticMigrationFiles(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"blogapp"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	init, err := os.ReadFile(filepath.Join("blogapp", "internal", "blogapp", "migrations", "0001_init.sql"))
	if err != nil {
		t.Fatalf("expected a scaffolded 0001_init.sql: %v", err)
	}
	if !strings.Contains(string(init), "CREATE TABLE `notes`") {
		t.Errorf("0001_init.sql does not create the notes table:\n%s", init)
	}
	if _, err := os.Stat(filepath.Join("blogapp", "internal", "blogapp", "migrations", "schema.sql")); err != nil {
		t.Fatalf("expected a scaffolded schema.sql: %v", err)
	}
}

// schemaSQLTemplate is a static snapshot of what 0001_init.sql adds up
// to, and nothing recomputes it at scaffold time (Amendment 1) or at
// check time (migration check diffs Models against a replay of
// Schema, never reads schema.sql at all) — so a future hand-edit to
// initMigrationTemplate that forgets schema.sql would otherwise go
// unnoticed by every test and by CI. This test ties the two together
// through the same function `rastrillo migration generate` itself
// calls to write schema.sql, so they cannot silently drift apart.
func TestNewScaffoldsSchemaSQLMatchesInitMigration(t *testing.T) {
	got, err := migrate.SchemaSQL(context.Background(), []migrate.Migration{
		{ID: "0001_init", SQL: initMigrationTemplate},
	})
	if err != nil {
		t.Fatalf("migrate.SchemaSQL: %v", err)
	}
	if got != schemaSQLTemplate {
		t.Errorf("schemaSQLTemplate is stale relative to initMigrationTemplate.\ngot:\n%s\nwant:\n%s", got, schemaSQLTemplate)
	}
}

// models.go must declare a real Note struct and the Models slice the
// generator reads — not a struct shown only in a comment, which gives
// the generator nothing to work with.
func TestNewScaffoldsRealModel(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"blogapp"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	m, err := os.ReadFile(filepath.Join("blogapp", "internal", "blogapp", "models.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"var Models = []any{",
		"&Note{}",
		"type Note struct {",
	} {
		if !strings.Contains(string(m), want) {
			t.Errorf("models.go missing %q:\n%s", want, m)
		}
	}
}

// make ci must run the drift gate, or the gate is only decorative:
// a scaffold that never wires `rastrillo migration check` into CI
// lets models.go and the migrations directory drift silently.
func TestNewScaffoldsMakefileMigrationCheck(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"blogapp"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	mk, err := os.ReadFile(filepath.Join("blogapp", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mk), "rastrillo migration check") {
		t.Errorf("Makefile's ci target is missing rastrillo migration check:\n%s", mk)
	}
	// Before this branch, `make ci` needed only the Go toolchain. A
	// bare `rastrillo migration check` broke that on the first CI run
	// of a fresh clone — "make: rastrillo: Command not found", with
	// nothing in the scaffold naming what to install.
	if !strings.Contains(string(mk), "go run amadan.net/rastrillo/rastrillo/cmd/rastrillo migration check") {
		t.Errorf("migration-check must go through `go run` so the gate stays toolchain-only:\n%s", mk)
	}
}

// The other half of that gate: the go run above only resolves because
// go.mod declares the CLI as a tool, which is what makes go mod tidy
// record the CLI's own build dependencies in the app's go.sum. The app
// imports nothing that reaches them — internal/manifest's TOML parser
// is the one that bites — so tidy prunes them without this line and a
// clean scaffold fails its very first make ci with "missing go.sum
// entry", before a line of app code exists.
func TestNewScaffoldsCLIToolDirective(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"blogapp"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	gomod, err := os.ReadFile(filepath.Join("blogapp", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gomod), "\ntool amadan.net/rastrillo/rastrillo/cmd/rastrillo\n") {
		t.Errorf("scaffolded go.mod must declare the CLI as a tool, or `make migration-check` "+
			"fails on a clean scaffold with a missing go.sum entry:\n%s", gomod)
	}
}

// The scaffold's agent instructions must teach the schema-change rules
// an app author needs: edit models.go, regenerate, never touch a
// shipped migration, and hand-write renames since a rename is
// indistinguishable from a drop plus an add.
//
// They live in AGENTS.md, not CLAUDE.md: main moved the instructions to
// the cross-agent file and left CLAUDE.md an @AGENTS.md import with
// nothing duplicated in it. Assert on the file that carries them, or
// this passes on a pointer.
func TestNewScaffoldsAgentsMDMigrationRules(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"blogapp"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	c, err := os.ReadFile(filepath.Join("blogapp", "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"rastrillo migration generate",
		"Never edit a migration that has shipped",
		"rename",
	} {
		if !strings.Contains(string(c), want) {
			t.Errorf("AGENTS.md missing %q:\n%s", want, c)
		}
	}
}

// TestScaffoldMigratesAndPassesCheck is the backstop for the whole
// design: it proves the static 0001_init.sql this package ships
// actually matches the Note model new also ships, by running the real
// CLI's `migration check` against a freshly scaffolded app. The CLI's
// own generate/check tests (migration_test.go) run against a
// hand-built fixture rather than the real scaffold, precisely so they
// don't depend on Task 12 — this is the test that would catch the two
// drifting apart.
//
// It compiles and runs the scaffolded app (migration check shells out
// to `go run`), so it needs the sandbox's local module cache
// (setSandboxGoEnv, migration_test.go) and a replace pointing this
// module at the checkout instead of the published one
// (TestScaffoldedAppTestsPass, new_test.go) — both gated behind
// -short since they're slow.
func TestScaffoldMigratesAndPassesCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and runs a scaffolded app")
	}
	setSandboxGoEnv(t)
	root := repoRoot(t)
	t.Chdir(t.TempDir())

	if err := runNew([]string{"freshapp"}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		filepath.Join("freshapp", "internal", "freshapp", "migrations.go"),
		filepath.Join("freshapp", "internal", "freshapp", "migrations", "0001_init.sql"),
		filepath.Join("freshapp", "internal", "freshapp", "migrations", "schema.sql"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("scaffold missing %s: %v", p, err)
		}
	}
	b, err := os.ReadFile(filepath.Join("freshapp", "internal", "freshapp", "app.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "AutoMigrate") {
		t.Fatal("app.go still calls AutoMigrate; boot must go through migrate.Apply")
	}
	if !strings.Contains(string(b), "migrate.Apply") {
		t.Fatal("app.go must call migrate.Apply")
	}
	m, err := os.ReadFile(filepath.Join("freshapp", "internal", "freshapp", "models.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(m), "var Models") {
		t.Fatal("models.go must declare Models for the generator to read")
	}

	f, err := os.OpenFile(filepath.Join("freshapp", "go.mod"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\nreplace amadan.net/rastrillo/rastrillo => " + root + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = "freshapp"
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy on the scaffold fails:\n%s", out)
	}

	if err := runMigration([]string{"check", "freshapp"}); err != nil {
		t.Fatalf("a fresh scaffold must pass check: %v", err)
	}

	// And the same check through the scaffold's own gate, with no
	// rastrillo binary on PATH. Before this branch `make ci` needed
	// only the Go toolchain; the migration-check step must not have
	// quietly added a second install step that a fresh clone's first
	// CI run discovers as "make: rastrillo: Command not found".
	mk := exec.Command("make", "migration-check")
	mk.Dir = "freshapp"
	// A PATH holding the toolchain and the usual system directories,
	// and demonstrably no rastrillo: this is a CI runner that cloned
	// the app and nothing else.
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	// -mod=readonly, overriding the -mod=mod this test's sandbox env
	// sets: readonly is what an app's CI actually runs under, and the
	// difference is the whole bug this guards. Under -mod=mod a
	// missing go.sum entry is not an error — go run just writes the
	// requirement into go.mod and carries on — so this step passed
	// here for as long as it failed for everyone else, who saw
	// "missing go.sum entry for github.com/BurntSushi/toml" on the
	// very first make ci of a clean scaffold. Keep it readonly.
	mk.Env = append(os.Environ(),
		"PATH="+filepath.Dir(goBin)+":/usr/bin:/bin",
		"GOFLAGS=-mod=readonly",
	)
	if out, err := mk.CombinedOutput(); err != nil {
		t.Fatalf("make migration-check must pass with only the Go toolchain on PATH:\n%s", out)
	}
}

// TestScaffoldedReleaseStampsAVersionTheBinaryReports is the control for
// the version stamp, and it is deliberately not a test that the Makefile
// contains a string.
//
// Every app the framework has ever scaffolded answered "dev" from
// GET /api/version, forever: serve.go said the version was stamped "see
// cmd/rastrillo", and the scaffolded release target built with
// -ldflags="-s -w" and no -X. A test asserting the Makefile holds the
// right flags would have been green the moment the flags were typed and
// would not have told anyone whether the binary reports anything, which
// is the substitution this branch has now found six times: a string
// assertion standing in for a behavioural one.
//
// So this scaffolds an app, builds it twice from identical source, and
// asks each binary over HTTP what it is.
//
//   - built with `make release` off a tagged, clean tree, it reports the tag
//   - built with a plain `go build`, it reports "dev"
//
// The second half is the calibration. One binary reporting a version
// proves nothing on its own — it could be reading a default that happens
// to match. Two builds of the same source giving different answers is
// what shows the stamp is doing the work.
//
// Then the two refusals, because a stamp is only evidence if it can
// name exactly one artifact: a tree with uncommitted changes, and a
// version that cannot be determined at all.
func TestScaffoldedReleaseStampsAVersionTheBinaryReports(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a scaffolded app")
	}
	setSandboxGoEnv(t)
	root := repoRoot(t)
	t.Chdir(t.TempDir())

	if err := runNew([]string{"verapp"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	app := "verapp"

	f, err := os.OpenFile(filepath.Join(app, "go.mod"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\nreplace amadan.net/rastrillo/rastrillo => " + root + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	run := func(name string, args ...string) (string, error) {
		cmd := exec.Command(name, args...)
		cmd.Dir = app
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	mustRun := func(name string, args ...string) string {
		t.Helper()
		out, err := run(name, args...)
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
		return out
	}

	mustRun("go", "mod", "tidy")

	// A repository with one commit and one tag, which is what git
	// describe reads. The identity is passed per-command so this does
	// not depend on whatever the machine has configured.
	const tag = "v9.9.9-control"
	mustRun("git", "init", "-q", "-b", "main")
	mustRun("git", "add", "-A")
	mustRun("git", "-c", "user.email=control@example.invalid", "-c", "user.name=control",
		"commit", "-q", "-m", "scaffold")
	mustRun("git", "tag", tag)

	// The release target defaults to the deployment architecture, which
	// is the right default and not runnable here, so this asks for the
	// host's.
	mustRun("make", "release", "RELEASE_GOOS="+runtime.GOOS, "RELEASE_GOARCH="+runtime.GOARCH)
	released := filepath.Join(app, "releases", app+"-"+runtime.GOOS+"-"+runtime.GOARCH)
	if _, err := os.Stat(released); err != nil {
		t.Fatalf("make release produced no binary: %v", err)
	}
	if got := apiVersion(t, released, app); got != tag {
		t.Errorf("GET /api/version on the released binary = %q, want %q — the stamp is not reaching the process", got, tag)
	}

	// The calibration: same source, no -X, and the answer must change.
	// Without this, "it reported the tag" could mean the harness is
	// reading something other than the binary under test.
	mustRun("go", "build", "-o", "unstamped", "./cmd/"+app)
	if got := apiVersion(t, filepath.Join(app, "unstamped"), app); got != "dev" {
		t.Errorf("GET /api/version on an unstamped build = %q, want \"dev\" — "+
			"if this is not dev the released binary's answer proves nothing", got)
	}

	// A dirty tree is refused. git describe would spell it v9.9.9-control-dirty,
	// a string two different binaries can carry, which is worse than no
	// stamp because it looks like evidence.
	if err := os.WriteFile(filepath.Join(app, "README.md"), []byte("edited, uncommitted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := run("make", "release", "RELEASE_GOOS="+runtime.GOOS, "RELEASE_GOARCH="+runtime.GOARCH)
	if err == nil {
		t.Errorf("make release built from a dirty tree:\n%s", out)
	}
	if !strings.Contains(out, "dirty") {
		t.Errorf("the refusal does not say what is wrong:\n%s", out)
	}
	mustRun("git", "checkout", "--", "README.md")

	// And a version that cannot be determined at all refuses rather than
	// stamping an empty string, which would look like a value.
	out, err = run("make", "release", "VERSION=")
	if err == nil {
		t.Errorf("make release stamped an empty version:\n%s", out)
	}
	if !strings.Contains(out, "no version to stamp") {
		t.Errorf("the refusal does not say what is wrong:\n%s", out)
	}
}

// apiVersion starts a built app on a listener handed to it as fd 3 —
// the platform's own socket activation, and the only way to reach a
// scaffolded binary without guessing a port — and returns what it says
// at GET /api/version.
func apiVersion(t *testing.T, bin, dir string) string {
	t.Helper()
	abs, err := filepath.Abs(bin)
	if err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	lf, err := l.(*net.TCPListener).File()
	if err != nil {
		t.Fatal(err)
	}
	defer lf.Close()

	cmd := exec.Command(abs)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LISTEN_FDS=1")
	cmd.ExtraFiles = []*os.File{lf}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", abs, err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	url := "http://" + l.Addr().String() + "/api/version"
	deadline := time.Now().Add(30 * time.Second)
	for {
		resp, err := http.Get(url)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return strings.TrimSpace(string(body))
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never answered %s: %v\napp stderr:\n%s", abs, url, err, stderr.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestGatePinsMatchRastrillosGate keeps each pair of pins one pin. The
// scaffold's Makefile and rastrillo's own are separate files that would
// otherwise drift: bump one and a fresh app is checked by a different
// release, or built on a different Go, than the framework that generated
// it, so a finding can pass one gate and fail the other.
func TestGatePinsMatchRastrillosGate(t *testing.T) {
	mk, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	for constant, want := range map[string]string{
		"staticcheckVersion": "STATICCHECK := honnef.co/go/tools/cmd/staticcheck@" + staticcheckVersion + "\n",
		"govulncheckVersion": "GOVULNCHECK := golang.org/x/vuln/cmd/govulncheck@" + govulncheckVersion + "\n",
		"gitleaksVersion":    "GITLEAKS := github.com/zricethezav/gitleaks/v8@" + gitleaksVersion + "\n",
		"goToolchain":        "export GOTOOLCHAIN = " + goToolchain + "\n",
	} {
		if !strings.Contains(string(mk), want) {
			t.Errorf("rastrillo's Makefile and cmd/rastrillo's %s disagree; want the line %q", constant, want)
		}
	}
}

// TestScaffoldWholeGatePassesOffline is the end-to-end proof the budgets
// spec asks for: a fresh app's entire make ci (vet, fmt, staticcheck,
// budget, test, perf, migration-check) passes with only the Go toolchain
// on PATH, a readonly module graph, and no network. Anything a step
// fetched at run time, or a step that quietly did nothing, fails here.
//
// No network, not GOPROXY=off: a versioned go run asks its proxy for
// the module's latest version, so the proxy here is the local module
// cache, and every variable that would route around it is cleared.
func TestScaffoldWholeGatePassesOffline(t *testing.T) {
	if testing.Short() {
		t.Skip("runs a scaffolded app's whole gate")
	}
	setSandboxGoEnv(t)
	root := repoRoot(t)
	t.Chdir(t.TempDir())
	if err := runNew([]string{"gateapp"}); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join("gateapp", "go.mod"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("\nreplace amadan.net/rastrillo/rastrillo => " + root + "\n")
	f.Close()
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = "gateapp"
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, out)
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	modcache, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Fatal(err)
	}
	// Every target on the scaffold's own ci: line, read from its Makefile
	// so the two cannot drift, except govulncheck: it reads the live
	// vulnerability database, which is the network by design.
	mkfile, err := os.ReadFile(filepath.Join("gateapp", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	var targets []string
	for _, line := range strings.Split(string(mkfile), "\n") {
		if strings.HasPrefix(line, "ci: ") {
			for _, tg := range strings.Fields(strings.TrimPrefix(line, "ci: ")) {
				if tg != "govulncheck" {
					targets = append(targets, tg)
				}
			}
		}
	}
	if len(targets) < 5 {
		t.Fatalf("read only %v from the scaffold's ci: line", targets)
	}
	mk := exec.Command("make", targets...)
	mk.Dir = "gateapp"
	mk.Env = append(os.Environ(),
		"PATH="+filepath.Dir(goBin)+":/usr/bin:/bin",
		// -buildvcs=false: the scaffold sits in a temp directory with no
		// repository of its own, and a .git above $TMPDIR (there is one on
		// this machine) would otherwise fail staticcheck's VCS stamping.
		"GOFLAGS=-mod=readonly -buildvcs=false",
		"GOPROXY=file://"+strings.TrimSpace(string(modcache))+"/cache/download",
		"GOPRIVATE=", "GONOPROXY=", "GONOSUMDB=", "GOSUMDB=off",
		// Timing reports rather than fails here: this proves the gate's
		// wiring, and a dev box's load is not evidence about speed.
		"AMADAN_CI=", "CI=",
	)
	out, err := mk.CombinedOutput()
	if err != nil {
		t.Fatalf("make ci on a fresh scaffold:\n%s", out)
	}
	if !strings.Contains(string(out), "budget size:") {
		t.Errorf("make ci did not run budget size:\n%s", out)
	}
	// One receipt from the suite and one from the perf lane: both steps
	// ran through budget test, and -require passed in the perf lane.
	if n := strings.Count(string(out), "receipt: test step"); n != 2 {
		t.Errorf("want 2 receipts (test, perf), got %d:\n%s", n, out)
	}
	for _, want := range []string{"perf: GET /: median", "perf: cold boot: median"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("the perf lane did not report %q:\n%s", want, out)
		}
	}
	if strings.Contains(string(out), "required test") {
		t.Errorf("the perf lane's required tests did not both pass:\n%s", out)
	}
	// generate --check passing on a scaffold with no manifest resources
	// is what proves the scaffold writes gen/api.go and api/v1.json the
	// way generate would: a missing contract fails it.
	if !strings.Contains(string(out), "rastrillo generate --check:") {
		t.Errorf("make ci did not run generate --check:\n%s", out)
	}
}

// A new app declares one locale and already links its pack, so it
// builds and boots before its first rastrillo generate; and --check
// agrees the packs file matches app.toml from the start.
func TestNewDeclaresOneLocaleAndLinksItsPack(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)
	if err := runNew([]string{"demoapp"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	app, err := manifest.LoadApp(filepath.Join("demoapp", "manifest"))
	if err != nil || !slices.Equal(app.Locales, []string{"en"}) || app.DefaultLocale != "en" {
		t.Errorf("manifest/app.toml: %+v, %v; want en only", app, err)
	}
	if stale, err := packs.Stale(filepath.Join("demoapp", "gen"), app); stale != "" || err != nil {
		t.Errorf("the scaffold's packs file disagrees with its app.toml: %q, %v", stale, err)
	}
	src, _ := os.ReadFile(filepath.Join("demoapp", "internal", "demoapp", "app.go"))
	if !strings.Contains(string(src), "\t_ \"demoapp/gen/locales\"\n") {
		t.Errorf("app.go must link gen/locales so the app's tests link the packs:\n%s", src)
	}
	mainSrc, _ := os.ReadFile(filepath.Join("demoapp", "cmd", "demoapp", "main.go"))
	for _, want := range []string{"Locales:       genlocales.Locales,", "DefaultLocale: genlocales.DefaultLocale,"} {
		if !strings.Contains(string(mainSrc), want) {
			t.Errorf("main.go lacks %q", want)
		}
	}
}

// scaffoldApp scaffolds name with flags in a fresh temp directory,
// points it at this checkout and tidies it, failing the test on error.
func scaffoldApp(t *testing.T, name string, flags ...string) string {
	t.Helper()
	setSandboxGoEnv(t)
	t.Chdir(t.TempDir())
	return scaffoldWithReplace(t, name, flags...)
}

// runNewInTemp runs rastrillo new in a fresh temp directory and returns
// that directory with runNew's error, for the refusals.
func runNewInTemp(t *testing.T, args ...string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	return dir, runNew(args)
}

// A new app's templates are already migrated: the preflight finds
// nothing to do, and doctor is clean on day one. The sidebar shell is
// the one with a second page, overview.html.
func TestScaffoldNeedsNoTemplateMigration(t *testing.T) {
	if testing.Short() {
		t.Skip("scaffolds, tidies and type-checks whole apps")
	}
	for _, flags := range [][]string{nil, {"--shell=sidebar"}} {
		app := scaffoldApp(t, "notes", flags...)
		var out strings.Builder
		if code := tmplmigrate.Run([]string{app}, &out, &out); code != 0 {
			t.Errorf("migrate-templates dry run on a new app %v: exit %d\n%s", flags, code, out.String())
		}
	}
}

func TestNewWithLocale(t *testing.T) {
	if testing.Short() {
		t.Skip("scaffolds and tidies a whole app")
	}
	app := scaffoldApp(t, "boutique", "--locale=es")
	toml, _ := os.ReadFile(filepath.Join(app, "manifest", "app.toml"))
	if !strings.Contains(string(toml), `locales = ["es"]`) || !strings.Contains(string(toml), `default_locale = "es"`) {
		t.Errorf("app.toml:\n%s", toml)
	}
	packsSrc, _ := os.ReadFile(filepath.Join(app, "gen", "locales", "packs.go"))
	if !strings.Contains(string(packsSrc), `"amadan.net/rastrillo/rastrillo/locales/es"`) || strings.Contains(string(packsSrc), "locales/en\"") {
		t.Errorf("packs.go links the wrong packs:\n%s", packsSrc)
	}
	// de has CLDR data but no framework strings, so generate --check
	// would fail it on a missing catalog before the author wrote a line.
	dir, err := runNewInTemp(t, "--locale=de", "x")
	want := fmt.Sprintf(newLocaleUnsupportedMsg, "de", strings.Join(rastrillo.BaseLocales(), ", "))
	if err == nil || err.Error() != want {
		t.Errorf("rastrillo new --locale=de: %v, want %q", err, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "x")); !os.IsNotExist(err) {
		t.Errorf("a refused --locale left ./x behind: %v", err)
	}
	// pt was a shipped code until the framework's Portuguese became
	// pt-PT, so a script or a habit that still types it gets pt-PT.
	dir, err = runNewInTemp(t, "--locale=pt", "y")
	toml, _ = os.ReadFile(filepath.Join(dir, "y", "manifest", "app.toml"))
	if err != nil || !strings.Contains(string(toml), `default_locale = "pt-PT"`) {
		t.Errorf("rastrillo new --locale=pt: %v\n%s", err, toml)
	}
}

// localeExtraTest is written into a scaffolded app's own test package
// and run by its go test: the page's language, direction and switcher,
// and the error page's language, as the app really serves them, through
// rastrillo.Handler with the Options main.go builds. %[1]s is the app
// package, %[2]s the module, %[3]s the assertions, %[4]s any imports
// only those assertions use.
const localeExtraTest = `package %[1]stest

import (%[4]s
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"amadan.net/rastrillo/rastrillo"

	genlocales "%[2]s/gen/locales"
	%[1]s "%[2]s/internal/%[1]s"
)

func testHandler(t *testing.T) http.Handler {
	t.Helper()
	opts := rastrillo.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Locales: genlocales.Locales, DefaultLocale: genlocales.DefaultLocale}
	%[1]s.Configure(&opts, newApp(t).(*http.ServeMux), time.Now())
	h, closeAll, err := rastrillo.Handler(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeAll() })
	return h
}

func TestLanguageDirectionSwitcherAndErrorPage(t *testing.T) {
	h := testHandler(t)
	get := func(path string, header map[string]string) string {
		r := httptest.NewRequest("GET", path, nil)
		for k, v := range header {
			r.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec.Body.String()
	}
	%[3]s
}
`

// Language, direction, switcher and error page in real scaffolds: one
// locale has neither prefixes nor a switcher; two have both, and the
// error page finds the request's language outside the middleware; an
// Arabic app is right to left from its first page.
func TestScaffoldServesItsLocales(t *testing.T) {
	if testing.Short() {
		t.Skip("scaffolds, tidies and tests whole apps")
	}
	es := rastrillo.BaseCatalogs()["es"]
	en := rastrillo.BaseCatalogs()["en"]
	for _, c := range []struct {
		name    string
		flags   []string
		locales string // rewrites app.toml and runs generate when set
		body    string
		imports string
	}{
		{"one locale", nil, "", `
	if b := get("/", nil); !strings.Contains(b, ` + "`" + `<html lang="en" dir="ltr">` + "`" + `) || strings.Contains(b, "rst-locale") {
		t.Errorf("/:\n%s", b)
	}
	// No prefixes: with one locale /es/ is an ordinary path, not the index.
	if b := get("/es/", nil); strings.Contains(b, "this is a rastrillo app") {
		t.Errorf("/es/ served the index in a one-locale app:\n%s", b)
	}
	// The colour scheme, through the scaffold's own renderer with no
	// script: choose Dark on the real route, then reload with the cookie
	// the route set; the page and the error page both paint Dark.
	post := httptest.NewRequest("POST", "/_theme", strings.NewReader("scheme=dark&return=/"))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, post)
	var cookie string
	for _, c := range rec.Result().Cookies() {
		if c.Name == "rastrillo_scheme" {
			cookie = c.String()
		}
	}
	if rec.Code != http.StatusSeeOther || cookie == "" {
		t.Fatalf("POST /_theme: status %d cookie %q", rec.Code, cookie)
	}
	const dark = ` + "`" + `value="dark"><input type="hidden" name="return" value="/"><button type="submit" aria-current="true">` + "`" + `
	if b := get("/", map[string]string{"Cookie": cookie}); !strings.Contains(b, ` + "`" + `data-theme="dark"` + "`" + `) || !strings.Contains(b, dark) {
		t.Errorf("reload with the scheme cookie does not stay Dark:\n%s", b)
	}
	if b := get("/", nil); strings.Contains(b, "data-theme") {
		t.Errorf("without the cookie the page is not System:\n%s", b)
	}
	errReq := httptest.NewRequest("GET", "/x", nil)
	errReq.Header.Set("Cookie", cookie)
	errRec := httptest.NewRecorder()
	notes.ErrorPage(errRec, errReq, http.StatusInternalServerError, "r1")
	if !strings.Contains(errRec.Body.String(), ` + "`" + `data-theme="dark"` + "`" + `) {
		t.Errorf("the error page does not paint the chosen scheme:\n%s", errRec.Body.String())
	}`, ""},
		{"two locales", nil, `["en", "es"]`, fmt.Sprintf(`
	b := get("/", nil)
	for _, want := range []string{"rst-locale", %q, %q} {
		if !strings.Contains(b, want) {
			t.Errorf("/ lacks %%q:\n%%s", want, b)
		}
	}
	if b := get("/es/", nil); !strings.Contains(b, `+"`"+`<html lang="es" dir="ltr">`+"`"+`) {
		t.Errorf("/es/:\n%%s", b)
	}
	byHeader := httptest.NewRequest("GET", "/x", nil)
	byHeader.Header.Set("Accept-Language", "es")
	for _, r := range []*http.Request{httptest.NewRequest("GET", "/es/x", nil), byHeader} {
		rec := httptest.NewRecorder()
		%s.ErrorPage(rec, r, http.StatusInternalServerError, "r1")
		if !strings.Contains(rec.Body.String(), template.HTMLEscapeString(%q)) {
			t.Errorf("ErrorPage %%s %%v: not in Spanish:\n%%s", r.URL, r.Header, rec.Body.String())
		}
	}`, template.HTMLEscapeString(en["rastrillo.ui.locale_name"]), template.HTMLEscapeString(es["rastrillo.ui.locale_name"]), "notes", es["rastrillo.ui.error_500_title"]), "\n\t\"html/template\""},
		{"arabic", []string{"--locale=ar"}, "", `
	if b := get("/", nil); !strings.Contains(b, ` + "`" + `<html lang="ar" dir="rtl">` + "`" + `) || strings.Contains(b, "rst-locale") {
		t.Errorf("/:\n%s", b)
	}`, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			app := scaffoldApp(t, "notes", c.flags...)
			if c.locales != "" {
				mustWrite(t, filepath.Join(app, "manifest", "app.toml"), "locales = "+c.locales+"\ndefault_locale = \"en\"\n")
				if err := runGenerate([]string{app}); err != nil {
					t.Fatalf("rastrillo generate: %v", err)
				}
			}
			mustWrite(t, filepath.Join(app, "internal", "notestest", "locale_extra_test.go"), fmt.Sprintf(localeExtraTest, "notes", "notes", c.body, c.imports))
			// render's request values go in a copy: a map the caller
			// reuses would otherwise carry one request's localizer into
			// the next, and race when two requests share it.
			mustWrite(t, filepath.Join(app, "internal", "notes", "render_extra_test.go"), `package notes

import (
	"net/http/httptest"
	"testing"
)

func TestRenderLeavesTheCallersMapAlone(t *testing.T) {
	data := map[string]any{"X": 1}
	render(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil), "index", data)
	if len(data) != 1 {
		t.Errorf("render wrote into the caller's map: %v", data)
	}
}
`)
			// The app's whole suite, not only the extra test: the
			// scaffold's own tests must pass in every locale it can start
			// in, and so must its perf lane, which builds its Options
			// apart from main.go. Timing reports rather than fails here,
			// as in the whole-gate test: a dev box's load says nothing
			// about speed.
			t.Setenv("AMADAN_CI", "")
			t.Setenv("CI", "")
			if out, err := goTestIn(app, "-tags", "perf", "-p", "1", "./..."); err != nil {
				t.Fatalf("the scaffolded app's tests fail:\n%s", out)
			}
		})
	}
}

// A locale configuration boot refuses never panics the app's own entry
// points: App refuses, and Mux and ErrorPage, reached without App by
// Options.ErrorPage and the perf lane, answer a plain 500.
func TestScaffoldWithABadLocaleConfigFailsSafely(t *testing.T) {
	if testing.Short() {
		t.Skip("scaffolds, tidies and tests a whole app")
	}
	app := scaffoldApp(t, "notes")
	packsGo := filepath.Join(app, "gen", "locales", "packs.go")
	src, _ := os.ReadFile(packsGo)
	bad := strings.Replace(string(src), `const DefaultLocale = "en"`, `const DefaultLocale = "de"`, 1)
	if bad == string(src) {
		t.Fatalf("packs.go has no English default to break:\n%s", src)
	}
	mustWrite(t, packsGo, bad)
	mustWrite(t, filepath.Join(app, "internal", "notestest", "badconfig_test.go"), `package notestest

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"amadan.net/rastrillo/rastrillo/db"

	"notes/internal/notes"
)

func TestBadConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d, err := db.Open(schema.Path(t), logger)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := notes.App(d, testOrigin, logger); err == nil {
		t.Error("App accepted a default locale whose pack is not linked")
	}
	rec := httptest.NewRecorder()
	notes.Mux(http.NotFoundHandler(), http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Mux: %d, want 500", rec.Code)
	}
	rec = httptest.NewRecorder()
	notes.ErrorPage(rec, httptest.NewRequest("GET", "/", nil), http.StatusNotFound, "")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("ErrorPage: %d, want 500", rec.Code)
	}
}
`)
	if out, err := goTestIn(app, "-run", "^TestBadConfig$", "./internal/notestest/"); err != nil {
		t.Fatalf("%s", out)
	}
}

// basepathExtraTest is written into a scaffolded two-locale sidebar app
// as package notestest, and serves the app as main.go does with a base
// path set. A root-absolute link left in one of the scaffold's own pages
// would take a tenant's reader to the hub's page of the same name.
const basepathExtraTest = `package notestest

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/basepath"
	"amadan.net/rastrillo/rastrillo/basepath/basepathtest"
	"amadan.net/rastrillo/rastrillo/csrf"
	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/sessions"

	genlocales "notes/gen/locales"
	"notes/internal/notes"
)

var acme = basepath.Config{Origin: "http://localhost:8080", Slug: "acme"}

func underAcme(t *testing.T, mux *http.ServeMux) http.Handler {
	t.Helper()
	opts := rastrillo.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Locales: genlocales.Locales, DefaultLocale: genlocales.DefaultLocale, BasePath: acme}
	notes.Configure(&opts, mux, time.Now())
	h, closeAll, err := rastrillo.Handler(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeAll() })
	return h
}

func send(h http.Handler, method, host, target string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
	r.Host = host
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Sec-Fetch-Site", "same-origin")
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func leaks(t *testing.T, what string, rec *httptest.ResponseRecorder) {
	t.Helper()
	if l := basepathtest.Leaks(rec.Body.Bytes(), "/acme"); len(l) > 0 {
		t.Errorf("%s links outside /acme: %q\n%s", what, l, rec.Body.String())
	}
}

var stylesheet = regexp.MustCompile("<link rel=\"stylesheet\" href=\"([^\"]+)\"")

const product, direct = "localhost:8080", "127.0.0.1:8080"

func TestUnderABasePath(t *testing.T) {
	h := underAcme(t, newApp(t).(*http.ServeMux))
	for _, p := range []string{"/acme", "/acme/overview", "/acme/es/overview"} {
		rec := send(h, "GET", product, p, nil)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s: %d, want 200:\n%s", p, rec.Code, rec.Body.String())
			continue
		}
		leaks(t, "GET "+p, rec)
		m := stylesheet.FindStringSubmatch(rec.Body.String())
		if m == nil {
			t.Errorf("GET %s links no stylesheet:\n%s", p, rec.Body.String())
		} else if css := send(h, "GET", product, m[1], nil); css.Code != http.StatusOK {
			t.Errorf("GET %s's stylesheet %s: %d, want 200", p, m[1], css.Code)
		}
	}
	if rec := send(h, "GET", product, "/static/tokens.css", nil); rec.Code != http.StatusNotFound {
		t.Errorf("GET /static/tokens.css outside the base: %d, want 404", rec.Code)
	}

	for _, c := range []struct{ target, field, value, want string }{
		{"/acme/_theme", "scheme", "dark", "/acme/overview"},
		{"/acme/_locale", "locale", "es", "/acme/es/overview"},
	} {
		rec := send(h, "POST", product, c.target, url.Values{c.field: {c.value}, "return": {"/overview"}})
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != c.want {
			t.Errorf("POST %s: %d to %q, want 303 to %q", c.target, rec.Code, rec.Header().Get("Location"), c.want)
		}
	}

	// Through the middleware, but outside rastrillo.Handler: a recovered
	// panic reaches ErrorPage the same way.
	errPage := acme.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		notes.ErrorPage(w, r, http.StatusInternalServerError, "r1")
	}))
	rec := send(errPage, "GET", product, "/acme/x", nil)
	if !strings.Contains(rec.Body.String(), "href=\"/acme\"") {
		t.Errorf("the error page's home link is not /acme:\n%s", rec.Body.String())
	}
	leaks(t, "the error page", rec)

	rec = tokenPage(t)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<form rst-form method=\"post\" action=\"/acme/tokens\">") {
		t.Errorf("GET /acme/tokens: %d, want 200 and a form that posts to /acme/tokens:\n%s", rec.Code, rec.Body.String())
	}
	leaks(t, "the token page", rec)

	rec = send(h, "GET", direct, "/overview", nil)
	if rec.Code != http.StatusTemporaryRedirect || rec.Header().Get("Location") != "http://localhost:8080/acme/overview" {
		t.Errorf("GET /overview on the direct host: %d to %q, want 307 to the product path", rec.Code, rec.Header().Get("Location"))
	}
	if rec := send(h, "GET", direct, "/healthz", nil); rec.Code != http.StatusOK {
		t.Errorf("GET /healthz on the direct host: %d, want 200", rec.Code)
	}
}

// tokenPage is GET /acme/tokens, mounted as tokens.go says. The row
// store is inert under a base path (a tenant verifies the hub's session
// and never mints one), so this signs in the way a path-routed tenant
// is signed in: a signed session, minted here at the root as the hub
// would.
func tokenPage(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	key, err := sessions.NewSigningKey("k1")
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessions.New(sessions.Config{Origin: acme.Origin, Logger: logger, Signed: &sessions.Signed{Name: "notes", Key: key,
		Epoch: func(context.Context, string) (int64, error) { return 1, nil }}})
	if err != nil {
		t.Fatal(err)
	}
	signin := httptest.NewRecorder()
	if err := sess.SignIn(signin, httptest.NewRequest("POST", "/signin", nil), sessions.Session{Subject: "1", Method: "test"}); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(schema.Path(t), logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	tokens, err := notes.Tokens(d, sess)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Use(csrf.Protect(testOrigin))
	r.Group(tokens)
	apiTree, err := notes.API(d, logger)
	if err != nil {
		t.Fatal(err)
	}
	return send(underAcme(t, notes.Mux(r, apiTree)), "GET", product, "/acme/tokens", nil, signin.Result().Cookies()...)
}
`

// A scaffolded app serves its own pages, the framework's switches, its
// token page and its error page under a base path with no link outside
// it, and its direct host sends a reader to the product path.
func TestScaffoldServesUnderABasePath(t *testing.T) {
	if testing.Short() {
		t.Skip("scaffolds, tidies and tests a whole app")
	}
	app := scaffoldApp(t, "notes", "--shell=sidebar")
	mustWrite(t, filepath.Join(app, "manifest", "app.toml"), "locales = [\"en\", \"es\"]\ndefault_locale = \"en\"\n")
	if err := runGenerate([]string{app}); err != nil {
		t.Fatalf("rastrillo generate: %v", err)
	}
	mustWrite(t, filepath.Join(app, "internal", "notestest", "basepath_extra_test.go"), basepathExtraTest)
	if out, err := goTestIn(app, "-v", "-run", "TestUnderABasePath", "./internal/notestest/"); err != nil || !strings.Contains(out, "--- PASS: TestUnderABasePath") {
		t.Fatalf("the scaffolded app under /acme:\n%s", out)
	}
}

// The scaffold's main.go reads the base path from the environment the
// platform gives a path-routed tenant, and refuses a slug it cannot
// serve rather than serving at the root.
func TestScaffoldBinaryReadsTheBasePathFromItsEnvironment(t *testing.T) {
	if testing.Short() {
		t.Skip("scaffolds and builds a whole app")
	}
	app := scaffoldApp(t, "notes")
	build := exec.Command("go", "build", "-buildvcs=false", "-o", "notes.bin", "./cmd/notes")
	build.Dir = app
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build:\n%s", out)
	}
	// A listener handed over as fd 3, as apiVersion does, rather than a
	// port picked and then bound by the app, which another process can
	// take in between.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	lf, err := l.(*net.TCPListener).File()
	if err != nil {
		t.Fatal(err)
	}
	defer lf.Close()
	origin := "http://" + l.Addr().String()
	// stderr is read only after the child has exited (stop waits for
	// it), because exec's copying goroutine writes the buffer until then.
	// Cleanup kills each child even when a check stops the test early.
	type proc struct {
		cmd    *exec.Cmd
		stderr bytes.Buffer
		done   chan struct{}
		err    error
	}
	stop := func(p *proc) string {
		_ = p.cmd.Process.Kill()
		<-p.done
		return p.stderr.String()
	}
	start := func(slug string) *proc {
		p := &proc{cmd: exec.Command(filepath.Join(app, "notes.bin")), done: make(chan struct{})}
		p.cmd.Dir = app
		p.cmd.Env = append(os.Environ(), "LISTEN_FDS=1", "STATE_DIRECTORY="+t.TempDir(),
			"NOTES_ORIGIN="+origin, "RASTRILLO_SLUG="+slug, "RASTRILLO_SLUG_FROM_HOST=")
		p.cmd.ExtraFiles = []*os.File{lf}
		p.cmd.Stderr = &p.stderr
		if err := p.cmd.Start(); err != nil {
			t.Fatal(err)
		}
		go func() { p.err = p.cmd.Wait(); close(p.done) }()
		t.Cleanup(func() { stop(p) })
		return p
	}
	// Bounded: this process holds the listener too, so once the child
	// has gone a connection is accepted by the kernel and never answered.
	client := &http.Client{Timeout: 5 * time.Second}
	get := func(path string) (int, string, error) {
		resp, err := client.Get(origin + path)
		if err != nil {
			return 0, "", err
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body), nil
	}

	p := start("acme")
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		select {
		case <-p.done:
			t.Fatalf("the app exited before it answered: %v\n%s", p.err, stop(p))
		default:
		}
		if _, _, err := get("/acme/healthz"); err == nil {
			break
		} else if time.Now().After(deadline) {
			t.Fatalf("the app never answered: %v\n%s", err, stop(p))
		}
	}
	code, body, err := get("/acme/healthz")
	rootCode, _, rootErr := get("/healthz")
	logs := stop(p)
	if err != nil || code != http.StatusOK || body != "ok" {
		t.Errorf("GET /acme/healthz: %d %q %v, want 200 ok\n%s", code, body, err, logs)
	}
	if rootErr != nil || rootCode != http.StatusNotFound {
		t.Errorf("GET /healthz on the product host: %d %v, want 404 outside the base\n%s", rootCode, rootErr, logs)
	}

	p = start("Bad/slug")
	select {
	case <-p.done:
		if logs := stop(p); p.err == nil || !strings.Contains(logs, "RASTRILLO_SLUG") {
			t.Errorf("RASTRILLO_SLUG=Bad/slug: exit %v, want a refusal naming RASTRILLO_SLUG:\n%s", p.err, logs)
		}
	case <-time.After(30 * time.Second):
		t.Errorf("RASTRILLO_SLUG=Bad/slug: still serving after 30s, want a refusal:\n%s", stop(p))
	}
}

// The scaffold's AGENTS.md states the money rule as Appendix C.7's
// scaffold.agents.money, verbatim: integer cents is no longer the
// whole rule once every amount carries its currency.
func TestNewAgentsMDStatesTheMoneyRule(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"demoapp"}); err != nil {
		t.Fatal(err)
	}
	agents, _ := os.ReadFile(filepath.Join("demoapp", "AGENTS.md"))
	want := "Money is a whole number of the currency's smallest unit, stored with its ISO 4217 code beside it, and shown with `money`. A float never touches a value a person will be held to."
	if !strings.Contains(string(agents), want) || strings.Contains(string(agents), "integer cents") {
		t.Errorf("AGENTS.md does not state the money rule as approved:\n%s", agents)
	}
}
