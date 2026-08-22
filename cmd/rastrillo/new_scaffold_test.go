package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosframework/rastrillo/migrate"
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

	for _, rel := range []string{
		"Makefile", "CLAUDE.md", "manifest/README.md",
		".amadan/ci", ".amadan/ci.d/10-vet", ".amadan/ci.d/20-fmt", ".amadan/ci.d/30-test",
	} {
		if _, err := os.Stat(filepath.Join("demoapp", rel)); err != nil {
			t.Errorf("scaffold missing %s: %v", rel, err)
		}
	}

	for _, rel := range []string{".amadan/ci", ".amadan/ci.d/10-vet", ".amadan/ci.d/20-fmt", ".amadan/ci.d/30-test"} {
		fi, err := os.Stat(filepath.Join("demoapp", rel))
		if err != nil {
			continue
		}
		if fi.Mode()&0o111 == 0 {
			t.Errorf("%s is not executable — amadan's runner would resolve the job 'skipped'", rel)
		}
	}

	mk, _ := os.ReadFile(filepath.Join("demoapp", "Makefile"))
	if !strings.Contains(string(mk), "ci: vet fmt-check test") {
		t.Fatalf("Makefile must define the one ci gate:\n%s", mk)
	}
	step, _ := os.ReadFile(filepath.Join("demoapp", ".amadan", "ci.d", "10-vet"))
	if !strings.Contains(string(step), "exec make vet") {
		t.Fatalf("steps must exec Makefile targets, never their own commands:\n%s", step)
	}
	claude, _ := os.ReadFile(filepath.Join("demoapp", "CLAUDE.md"))
	for _, want := range []string{"integer cents", "scope.Owned", "SKILL.md", "CGO_ENABLED=0"} {
		if !strings.Contains(string(claude), want) {
			t.Fatalf("CLAUDE.md preload missing %q:\n%s", want, claude)
		}
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
		"var BootSchema = migrate.Merge(Schema)",
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
	if !strings.Contains(string(mk), "go run github.com/carlosframework/rastrillo/cmd/rastrillo migration check") {
		t.Errorf("migration-check must go through `go run` so the gate stays toolchain-only:\n%s", mk)
	}
}

// CLAUDE.md must teach the schema-change rules an app author needs:
// edit models.go, regenerate, never touch a shipped migration, and
// hand-write renames since a rename is indistinguishable from a drop
// plus an add.
func TestNewScaffoldsClaudeMDMigrationRules(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"blogapp"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	c, err := os.ReadFile(filepath.Join("blogapp", "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"rastrillo migration generate",
		"Never edit a migration that has shipped",
		"rename",
	} {
		if !strings.Contains(string(c), want) {
			t.Errorf("CLAUDE.md missing %q:\n%s", want, c)
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
	if _, err := f.WriteString("\nreplace github.com/carlosframework/rastrillo => " + root + "\n"); err != nil {
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
	mk.Env = append(os.Environ(), "PATH="+filepath.Dir(goBin)+":/usr/bin:/bin")
	if out, err := mk.CombinedOutput(); err != nil {
		t.Fatalf("make migration-check must pass with only the Go toolchain on PATH:\n%s", out)
	}
}
