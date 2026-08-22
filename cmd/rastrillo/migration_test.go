package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosframework/rastrillo/db"
	"github.com/carlosframework/rastrillo/migrate"
)

// captureStdout runs fn with os.Stdout redirected to a pipe and
// returns everything it wrote — the only way to check the human-facing
// message migrationStatus and migrationBaseline print, since neither
// takes an io.Writer of its own.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	w.Close()
	var buf strings.Builder
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// --- unit tests for describe/tableIn/renamedTable: no compilation of
// a fixture app, so these run even under -short. ---

func TestDescribeNamesCreateTable(t *testing.T) {
	changes := []migrate.Change{{SQL: "CREATE TABLE `notes` (`id` integer,`title` text,PRIMARY KEY (`id`))"}}
	if got := describe(changes); got != "create_notes" {
		t.Errorf("describe = %q, want create_notes", got)
	}
}

// gormlite's AddColumn (and upstream GORM's) never writes the word
// COLUMN — "ALTER TABLE ... ADD <col> <type>" is the real shape
// (gorm.io/gorm/migrator/migrator.go AddColumn) — so a matcher
// grepping for "ADD COLUMN" never fires on real output.
func TestDescribeNamesAddColumnWithoutTheWordColumn(t *testing.T) {
	changes := []migrate.Change{{SQL: "ALTER TABLE `notes` ADD `archived` numeric"}}
	if got := describe(changes); got != "alter_notes" {
		t.Errorf("describe = %q, want alter_notes", got)
	}
}

// SQLite has no ALTER TABLE DROP COLUMN, so gormlite's DropColumn
// rebuilds the table: CREATE TABLE notes__temp, INSERT INTO, DROP
// TABLE notes, ALTER TABLE notes__temp RENAME TO notes. A matcher that
// returns on the first CREATE TABLE would mislabel this "create_notes__temp".
func TestDescribeNamesDropColumnRebuildByItsFinalRename(t *testing.T) {
	changes := []migrate.Change{
		{SQL: "CREATE TABLE `notes__temp` (`id` integer,PRIMARY KEY (`id`))", Destructive: true},
		{SQL: "INSERT INTO `notes__temp`(id) SELECT id FROM `notes`", Destructive: true},
		{SQL: "DROP TABLE `notes`", Destructive: true},
		{SQL: "ALTER TABLE `notes__temp` RENAME TO `notes`", Destructive: true},
	}
	if got := describe(changes); got != "drop_column_notes" {
		t.Errorf("describe = %q, want drop_column_notes (not create_notes__temp)", got)
	}
}

func TestDescribeNamesWholeTableDrop(t *testing.T) {
	changes := []migrate.Change{{SQL: "DROP TABLE IF EXISTS `notes`", Destructive: true}}
	if got := describe(changes); got != "drop_notes" {
		t.Errorf("describe = %q, want drop_notes", got)
	}
}

func TestDescribeFallsBackToSchema(t *testing.T) {
	if got := describe(nil); got != "schema" {
		t.Errorf("describe(nil) = %q, want schema", got)
	}
}

// --- fixture: a minimal app module, built by hand rather than via
// runNew (rastrillo new's scaffold doesn't grow a Models var, a
// migrations.go or a Schema until Task 12 — using it here would be
// circular: Task 12's own test needs `migration check` to already
// work). ---

// Note mirrors, field for field, the Note type baked into
// fixtureModelsGo below. GORM's table-naming strategy keys off the Go
// type name alone (schema.Parse: namer.TableName(modelType.Name())),
// not the package it lives in, so this local copy produces the exact
// same "notes" table gormlite would build from the fixture app's own
// copy — which lets genesisSQL compute real DDL in-process instead of
// hand-transcribing what GORM would say.
type Note struct {
	ID    int64
	Title string
}

// NoteWithArchived is Note plus the field the drift tests add to the
// fixture's models.go by hand. GORM's default table name comes from
// the Go type name (schema.Parse: namer.TableName(modelType.Name())),
// which would make this "note_with_archiveds" instead of "notes" —
// a different table, not a drifted one — so TableName pins it to the
// same table Note and the fixture's own Note both use, same as
// migrate/diff_test.go's genNote/genNoteWithBody pair does.
type NoteWithArchived struct {
	ID       int64
	Title    string
	Archived bool
}

func (NoteWithArchived) TableName() string { return "notes" }

// genesisSQL runs the real generator against models, in-process, to
// get the exact CREATE TABLE gormlite would emit — so the fixture's
// hand-shipped 0001_init.sql never drifts from what generate itself
// considers "in sync".
func genesisSQL(t *testing.T, models ...any) string {
	t.Helper()
	changes, err := migrate.Generate(context.Background(), nil, models)
	if err != nil {
		t.Fatalf("computing genesis SQL: %v", err)
	}
	var b strings.Builder
	for _, c := range changes {
		// Change.SQL already carries its trailing ";" (diff.go's
		// ensureSemicolon) — same body shape migrationGenerate itself
		// writes, so the fixture's 0001_init.sql looks like a file
		// generate would actually produce.
		b.WriteString(strings.TrimSpace(c.SQL))
		b.WriteString("\n\n")
	}
	return b.String()
}

const fixtureModelsGo = `package app

type Note struct {
	ID    int64
	Title string
}

var Models = []any{&Note{}}
`

const fixtureModelsGoWithArchived = `package app

type Note struct {
	ID       int64
	Title    string
	Archived bool
}

var Models = []any{&Note{}}
`

const fixtureMigrationsGo = `package app

import (
	"embed"

	"github.com/carlosframework/rastrillo/migrate"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

var Schema = migrate.MustFromFS(migrationFS, "app")
`

const fixtureGoModTemplate = `module fixtureapp

go 1.24

require (
	github.com/carlosframework/rastrillo v0.0.0
	gorm.io/gorm v1.31.2
)

replace github.com/carlosframework/rastrillo => %s
`

// setSandboxGoEnv points every `go` invocation this test starts —
// this fixture's own `go mod tidy`, and, transitively, the `go run`
// loadPayload shells out to inside runMigration — at the local module
// cache instead of proxy.golang.org, which this sandbox blocks.
//
// It uses t.Setenv rather than trusting the ambient shell to already
// have these exported: os/exec.Cmd.Env, left nil, is captured from
// os.Environ() at Start() time, so a t.Setenv here reaches loadPayload's
// go run too even though that call (production code, migration.go) sets
// no cmd.Env of its own — deliberately: a real developer has a real
// network, and hardcoding a sandbox-only proxy there would break them.
// Explicit on the fixture's own go-mod-tidy exec.Command as well, so
// this doesn't silently depend on env-var propagation working as
// described above.
func setSandboxGoEnv(t *testing.T) {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Fatalf("go env GOMODCACHE: %v", err)
	}
	modcache := strings.TrimSpace(string(out))
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOPRIVATE", "*")
	t.Setenv("GOPROXY", "file://"+modcache+"/cache/download")
}

// newFixtureApp builds a minimal app module under t.TempDir(): a
// go.mod replacing rastrillo with this checkout, internal/app/models.go
// declaring Models, internal/app/migrations.go embedding migrations/,
// and whatever *.sql files extraMigrations names. It runs `go mod
// tidy` and returns the app's directory (an absolute path, so callers
// need not chdir). Same replace-and-tidy shape as
// TestScaffoldedAppTestsPass (new_test.go) — the working example this
// task's brief points at.
func newFixtureApp(t *testing.T, modelsGo string, extraMigrations map[string]string) string {
	t.Helper()
	setSandboxGoEnv(t)
	root := t.TempDir()
	appDir := filepath.Join(root, "app")
	dirs := []string{
		filepath.Join(appDir, "internal", "app", "migrations"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(appDir, "go.mod"):                           fmt.Sprintf(fixtureGoModTemplate, repoRoot(t)),
		filepath.Join(appDir, "internal", "app", "models.go"):     modelsGo,
		filepath.Join(appDir, "internal", "app", "migrations.go"): fixtureMigrationsGo,
	}
	for path, content := range extraMigrations {
		files[filepath.Join(appDir, "internal", "app", "migrations", path)] = content
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = appDir
	tidy.Env = os.Environ() // includes the Setenv calls above
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy on the fixture app fails:\n%s", out)
	}
	return appDir
}

// --- end-to-end tests: these compile the fixture app via `go run`,
// so they are slow — testing.Short() skips them. ---

func TestMigrationCheckPassesOnFreshFixture(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a fixture app")
	}
	appDir := newFixtureApp(t, fixtureModelsGo, map[string]string{
		"0001_init.sql": genesisSQL(t, &Note{}),
	})
	if err := runMigration([]string{"check", appDir}); err != nil {
		t.Fatalf("a fresh fixture must be in sync: %v", err)
	}
}

func TestMigrationCheckFailsWhenModelGainsAField(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a fixture app")
	}
	appDir := newFixtureApp(t, fixtureModelsGoWithArchived, map[string]string{
		// The migrations only know about Note{ID,Title}; the model on
		// disk has grown Archived.
		"0001_init.sql": genesisSQL(t, &Note{}),
	})
	err := runMigration([]string{"check", appDir})
	if err == nil {
		t.Fatal("want check to fail when a model has a field the migrations lack")
	}
	if !strings.Contains(err.Error(), "generate") {
		t.Fatalf("error = %v, want it to name the fix", err)
	}
}

func TestMigrationGenerateWritesFileAndThenChecksClean(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a fixture app")
	}
	appDir := newFixtureApp(t, fixtureModelsGoWithArchived, map[string]string{
		"0001_init.sql": genesisSQL(t, &Note{}),
	})
	if err := runMigration([]string{"generate", appDir}); err != nil {
		t.Fatal(err)
	}
	mdir := filepath.Join(appDir, "internal", "app", "migrations")
	entries, err := os.ReadDir(mdir)
	if err != nil {
		t.Fatal(err)
	}
	var found string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "0002_") {
			found = e.Name()
		}
	}
	if found == "" {
		t.Fatalf("no 0002_*.sql written; got %v", entries)
	}
	body, err := os.ReadFile(filepath.Join(mdir, found))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(string(body)), "archived") {
		t.Fatalf("%s does not mention the new column:\n%s", found, body)
	}

	if err := runMigration([]string{"check", appDir}); err != nil {
		t.Fatalf("check must be clean after generate: %v", err)
	}

	// The temporary loader must not survive, success or failure.
	if _, err := os.Stat(filepath.Join(appDir, loaderDir)); !os.IsNotExist(err) {
		t.Fatal("the generated loader directory was left behind")
	}

	// schema.sql is the second loadPayload call's whole point: it must
	// reflect the migration just written, not the state before it.
	schema, err := os.ReadFile(filepath.Join(mdir, "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(string(schema)), "archived") {
		t.Fatalf("schema.sql was not refreshed with the new column:\n%s", schema)
	}
}

func TestMigrationGenerateRefusesDestructiveWithoutFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a fixture app")
	}
	appDir := newFixtureApp(t, fixtureModelsGo, map[string]string{
		// The migrations already have Archived; the model just lost it
		// — a destructive drop.
		"0001_init.sql": genesisSQL(t, &NoteWithArchived{}),
	})
	mdir := filepath.Join(appDir, "internal", "app", "migrations")

	err := runMigration([]string{"generate", appDir})
	if err == nil {
		t.Fatal("want generate to refuse a destructive change without --allow-destructive")
	}
	if !strings.Contains(err.Error(), "--allow-destructive") {
		t.Fatalf("error = %v, want it to name the flag", err)
	}
	if !strings.Contains(err.Error(), "rename") {
		t.Fatalf("error = %v, want it to point at the hand-written rename path", err)
	}
	entries, err := os.ReadDir(mdir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "0002_") {
			t.Fatalf("generate wrote %s despite refusing", e.Name())
		}
	}

	if err := runMigration([]string{"generate", "--allow-destructive", appDir}); err != nil {
		t.Fatalf("generate --allow-destructive: %v", err)
	}
	entries, err = os.ReadDir(mdir)
	if err != nil {
		t.Fatal(err)
	}
	found := ""
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "0002_") {
			found = e.Name()
		}
	}
	if found == "" {
		t.Fatal("no 0002_*.sql written after --allow-destructive")
	}
}

// An earlier task noted that dropChanges (migrate/diff.go) refuses to
// emit DROP TABLE rastrillo_migrations, but nothing exercised it
// end-to-end because no test replayed a set containing the ledger.
// This fixture's migrations include the ledger DDL directly — the
// shape an app baselined from a live database (which has already run
// Apply, and so already has the table) would actually have — and no
// model declares it, which is exactly the condition dropChanges must
// special-case rather than emitting a DROP for.
func TestMigrationGenerateNeverDropsLedgerTable(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a fixture app")
	}
	init := genesisSQL(t, &Note{}) + migrate.LedgerDDL + "\n"
	appDir := newFixtureApp(t, fixtureModelsGo, map[string]string{
		"0001_init.sql": init,
	})

	p, err := loadPayload(appDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range p.Changes {
		if strings.Contains(strings.ToUpper(c.SQL), "RASTRILLO_MIGRATIONS") {
			t.Fatalf("generate would touch the ledger table: %q", c.SQL)
		}
	}

	if err := runMigration([]string{"check", appDir}); err != nil {
		t.Fatalf("the ledger table alone must not count as drift: %v", err)
	}
}

// --- migration new: writes a numbered stub, no database or compile
// involved. ---

// TestMigrationNewWritesNumberedStub follows the brief's test almost
// verbatim, with one adjustment: the brief assumes rastrillo new
// already scaffolds a migrations/0001_init.sql, but that lands in
// Task 12 (see the fixture comment above), not here — a fresh scaffold
// today has no migrations directory at all. So this test seeds one by
// hand first, to still exercise the property the brief cared about:
// migration new numbers after whatever is already on disk, not always
// "0001".
func TestMigrationNewWritesNumberedStub(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(cwd) })
	os.Chdir(dir)
	if err := runNew([]string{"stubapp"}); err != nil {
		t.Fatal(err)
	}
	mdir := filepath.Join("stubapp", "internal", "stubapp", "migrations")
	if err := os.MkdirAll(mdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mdir, "0001_init.sql"), []byte("-- seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runMigration([]string{"new", "rename_title", "stubapp"}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(mdir)
	var found string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "_rename_title.sql") {
			found = e.Name()
		}
	}
	if found == "" {
		t.Fatalf("no *_rename_title.sql written; got %v", entries)
	}
	if !strings.HasPrefix(found, "0002_") {
		t.Fatalf("name = %q, want it numbered after the existing 0001_init.sql", found)
	}
	b, _ := os.ReadFile(filepath.Join(mdir, found))
	if !strings.Contains(string(b), "--") {
		t.Fatal("stub should carry a comment explaining what to write")
	}
}

func TestMigrationNewRejectsBadName(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(cwd) })
	os.Chdir(dir)
	runNew([]string{"badapp"})
	if err := runMigration([]string{"new", "Rename Title!", "badapp"}); err == nil {
		t.Fatal("want rejection of a name that is not lowercase_with_underscores")
	}
}

// --- nextMigrationName: no fixture ships with an empty migrations
// directory (every existing test fixture has an 0001_init.sql already
// in place), so the empty-directory numbering path has never actually
// run before this test existed. ---

func TestNextMigrationName(t *testing.T) {
	cases := []struct {
		name  string
		files []string
		label string
		want  string
	}{
		{
			name:  "empty directory",
			files: nil,
			label: "init",
			want:  "0001_init.sql",
		},
		{
			name:  "only schema.sql present",
			files: []string{"schema.sql"},
			label: "init",
			want:  "0001_init.sql",
		},
		{
			name:  "a non-numeric-prefixed file is ignored",
			files: []string{"README.sql"},
			label: "second",
			want:  "0001_second.sql",
		},
		{
			name:  "numbers after the highest existing prefix",
			files: []string{"0001_init.sql", "0002_add_col.sql"},
			label: "third",
			want:  "0003_third.sql",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range tc.files {
				if err := os.WriteFile(filepath.Join(dir, f), []byte("-- stub\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got, err := nextMigrationName(dir, tc.label)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("nextMigrationName = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestNextMigrationNameOnMissingDirectory covers migrationNew's own
// call shape: the migrations directory does not exist yet the first
// time `migration new` runs in a fresh app.
func TestNextMigrationNameOnMissingDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist-yet")
	got, err := nextMigrationName(dir, "init")
	if err != nil {
		t.Fatal(err)
	}
	if got != "0001_init.sql" {
		t.Errorf("nextMigrationName = %q, want 0001_init.sql", got)
	}
}

// --- migration status: needs loadPayload (compiles the fixture app),
// so these skip under -short. ---

func TestMigrationStatusReportsNoLedgerOnFreshDatabase(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a fixture app")
	}
	appDir := newFixtureApp(t, fixtureModelsGo, map[string]string{
		"0001_init.sql": genesisSQL(t, &Note{}),
	})
	dbPath := filepath.Join(t.TempDir(), "app.db")

	var runErr error
	out := captureStdout(t, func() {
		runErr = runMigration([]string{"status", "--db", dbPath, appDir})
	})
	if runErr != nil {
		t.Fatalf("a fresh database with no ledger must not be an error: %v", runErr)
	}
	if !strings.Contains(out, "no migration ledger") {
		t.Fatalf("output = %q, want the friendly no-ledger message", out)
	}
}

func TestMigrationStatusListsAppliedMigrations(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a fixture app")
	}
	appDir := newFixtureApp(t, fixtureModelsGo, map[string]string{
		"0001_init.sql": genesisSQL(t, &Note{}),
	})
	dbPath := filepath.Join(t.TempDir(), "app.db")

	d, err := db.Open(dbPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	conn, err := d.Writer().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, migrate.LedgerDDL); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx,
		"INSERT INTO rastrillo_migrations (id, applied_at, checksum) VALUES (?, ?, ?)",
		"app/0001_init", "2026-01-01T00:00:00Z", "deadbeef"); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	d.Close()

	var runErr error
	out := captureStdout(t, func() {
		runErr = runMigration([]string{"status", "--db", dbPath, appDir})
	})
	if runErr != nil {
		t.Fatal(runErr)
	}
	if !strings.Contains(out, "app/0001_init") {
		t.Fatalf("output = %q, want it to list the applied migration id", out)
	}
}

// TestMigrationStatusDoesNotSwallowGenuineLedgerError proves status
// tells a corrupt-or-unexpected ledger apart from a database that has
// simply never been booted: a query error against a ledger table that
// *does* exist (wrong columns here, standing in for anything that
// makes the SELECT itself fail) must come back as a real error, not
// the friendly "no ledger" line.
func TestMigrationStatusDoesNotSwallowGenuineLedgerError(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a fixture app")
	}
	appDir := newFixtureApp(t, fixtureModelsGo, map[string]string{
		"0001_init.sql": genesisSQL(t, &Note{}),
	})
	dbPath := filepath.Join(t.TempDir(), "app.db")

	d, err := db.Open(dbPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	// A ledger table that exists but is missing applied_at: the SELECT
	// migrationStatus runs will fail for a real reason, not because
	// the table is absent.
	if _, err := d.Writer().Exec("CREATE TABLE rastrillo_migrations (id TEXT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	d.Close()

	if err := runMigration([]string{"status", "--db", dbPath, appDir}); err == nil {
		t.Fatal("want a real error when the ledger table exists but its schema doesn't match, not silent success")
	}
}

// --- migration baseline: reads migrations off disk directly, no
// go run compile needed, so this fixture is built by hand without
// newFixtureApp/go mod tidy. ---

// baselineFixture writes just enough of an app module for appPackage
// and migrationsDir to work: a go.mod with a module line, and
// internal/app/migrations holding the given files. baseline never
// compiles the app, so no models.go, no go.sum, no `go mod tidy`.
func baselineFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	appDir := filepath.Join(root, "app")
	mdir := filepath.Join(appDir, "internal", "app", "migrations")
	if err := os.MkdirAll(mdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "go.mod"),
		[]byte("module fixtureapp\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(mdir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return appDir
}

func TestMigrationBaselineStampsOnlyThroughGivenID(t *testing.T) {
	appDir := baselineFixture(t, map[string]string{
		"0001_init.sql":     "CREATE TABLE notes (id INTEGER PRIMARY KEY);",
		"0002_add_body.sql": "ALTER TABLE notes ADD body TEXT;",
		"0003_add_flag.sql": "ALTER TABLE notes ADD flag INTEGER;",
	})
	dbPath := filepath.Join(t.TempDir(), "app.db")

	if err := runMigration([]string{
		"baseline", "--db", dbPath, "--through", "app/0002_add_body", appDir,
	}); err != nil {
		t.Fatal(err)
	}

	d, err := db.Open(dbPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	rows, err := d.Writer().Query("SELECT id FROM rastrillo_migrations ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		got = append(got, id)
	}
	want := []string{"app/0001_init", "app/0002_add_body"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("stamped ids = %v, want exactly %v (app/0003_add_flag must NOT be stamped)", got, want)
	}
}

// TestMigrationBaselineRejectsUnknownThroughID guards the exact
// failure mode this flag exists to prevent: an operator's typo (or a
// documented recovery ID this appSet doesn't actually cover) silently
// falling through to migrate.Stamp's "stamp everything" behavior
// instead of stopping at a real ID.
func TestMigrationBaselineRejectsUnknownThroughID(t *testing.T) {
	appDir := baselineFixture(t, map[string]string{
		"0001_init.sql": "CREATE TABLE notes (id INTEGER PRIMARY KEY);",
	})
	dbPath := filepath.Join(t.TempDir(), "app.db")

	err := runMigration([]string{
		"baseline", "--db", dbPath, "--through", "app/9999_nope", appDir,
	})
	if err == nil {
		t.Fatal("want rejection of a --through id that names no migration")
	}
	if !strings.Contains(err.Error(), "app/9999_nope") {
		t.Fatalf("error = %v, want it to name the bad id", err)
	}
	if !strings.Contains(err.Error(), "app/0001_init") {
		t.Fatalf("error = %v, want it to list the valid ids", err)
	}
	if _, statErr := os.Stat(dbPath); statErr == nil {
		t.Fatal("baseline must not touch the database when --through is invalid")
	}
}

func TestMigrationBaselineWithoutThroughStampsEverything(t *testing.T) {
	appDir := baselineFixture(t, map[string]string{
		"0001_init.sql":     "CREATE TABLE notes (id INTEGER PRIMARY KEY);",
		"0002_add_body.sql": "ALTER TABLE notes ADD body TEXT;",
	})
	dbPath := filepath.Join(t.TempDir(), "app.db")

	out := captureStdout(t, func() {
		if err := runMigration([]string{"baseline", "--db", dbPath, appDir}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "stamped") {
		t.Fatalf("output = %q, want confirmation it stamped the database", out)
	}

	d, err := db.Open(dbPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var n int
	if err := d.Writer().QueryRow("SELECT count(*) FROM rastrillo_migrations").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("stamped %d migrations, want 2 (no --through means stamp everything)", n)
	}
}
