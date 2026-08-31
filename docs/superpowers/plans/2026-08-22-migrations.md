# Migrations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace Rastrillo's two boot-time schema mechanisms with one ordered, ledgered migration runner, plus the CLI that generates and checks migrations without a database.

**Architecture:** A `migrate` package holds ordered `Set`s of namespaced migrations (embedded SQL files, or Go funcs sharing the ID space), applied once each at boot inside a pinned-connection transaction and recorded in a `rastrillo_migrations` ledger. Development tooling computes schema by replaying migrations into an in-memory SQLite and running GORM `AutoMigrate` against it with a recording logger — the SQL GORM executes *is* the generated migration, so no DDL is hand-written anywhere.

**Tech Stack:** Go 1.25, `gorm.io/gorm` v1.31.2, the repo's `gormlite` dialector fork, `modernc.org/sqlite`, `gorm.io/plugin/dbresolver`.

**Spec:** `docs/superpowers/specs/2026-08-22-migrations-design.md`

## Global Constraints

- Module path is `amadan.net/rastrillo/rastrillo`. New package lives at `migrate/`.
- SQLite only. No new third-party dependencies — `gormlite`, `gorm`, and `modernc.org/sqlite` are already present.
- Migrations are forward-only. No `Down`, no rollback, no reverse — see spec §3.
- No CLI command applies migrations, with the single exception of `rastrillo migration baseline` (spec §4).
- Migration file names match `NNNN_name.sql` exactly (four digits, underscore, lowercase name). `FromFS` ignores everything else, which is what lets `schema.sql` share the directory.
- Ledger IDs are `"<namespace>/<file stem>"`, e.g. `sessions/0001_init`.
- Checksums are `sha256` over `strings.Join(strings.Fields(sql), " ")` — whitespace-normalised so reformatting is not a false alarm.
- Every migration runs in its own `BEGIN IMMEDIATE` transaction with its ledger row inserted inside that same transaction.
- The repo gate is `make ci` → `go vet ./... && gofmt -l . && go test ./...`. Every task ends green.
- Commit style: imperative subject, body explaining *why*. Never merge to main directly — this branch becomes a PR (see `MEMORY.md`).

### Two deliberate deviations from the spec

Both were found while planning; the spec text is otherwise authoritative.

1. **§5.1 `Apply` signature.** The spec writes `Apply(g *gorm.DB, s *Set)`. `PRAGMA foreign_keys=OFF` is per-connection and must sit *outside* the transaction (spec §8), which requires pinning one `*sql.Conn` from the writer pool. `dbresolver` makes `g.DB()`'s pool ambiguous, so the real signature is `Apply(ctx context.Context, d *db.DB, s *Set) (Result, error)` and `db.DB` gains a `Writer() *sql.DB` accessor. Go `Fn` migrations still receive a `*gorm.DB`.
2. **§5.4 diff engine.** The spec implies a hand-written structured differ emitting DDL. Instead, additive changes are produced by running `AutoMigrate` against the replayed database with a recording GORM logger and capturing the SQL it executes; the structured diff decides only *what to drop*, and the drops are emitted through `Migrator().DropColumn`/`DropTable`/`DropIndex` under the same recorder. No DDL string is ever hand-assembled, and `gormlite`'s twelve-step rebuild is reused rather than reimplemented.

---

## File Structure

**Created:**

| File | Responsibility |
|---|---|
| `migrate/set.go` | `Migration`, `Set`, `FromFS`, `MustFromFS`, `Add`, `Merge`. Pure construction and ordering; no database. |
| `migrate/schema.go` | Structured schema read (`Snapshot`) and comparison. Used by adoption, `check`, and `schema.sql`. |
| `migrate/apply.go` | The ledger, the pinned-connection transaction loop, checksum enforcement, `Result`. |
| `migrate/adopt.go` | The three first-boot branches of spec §7. |
| `migrate/memory.go` | `:memory:` helpers: replay a `Set`, `AutoMigrate` a model list, the recording logger. |
| `migrate/diff.go` | `Generate` — additive capture plus destructive diff. |
| `migrate/dump/dump.go` | The tiny library the generated loader program calls; prints a `Snapshot` as JSON. |
| `cmd/rastrillo/migration.go` | `migration` subcommand group dispatch and the `go run` loader. |
| `<pkg>/migrations/0001_init.sql` | Per-subsystem migration files (sessions, blobs, eventlog, passkey, auth). |

**Modified:**

| File | Change |
|---|---|
| `db/db.go` | Add `Writer() *sql.DB`. |
| `sessions/sessions.go`, `blobs/blobs.go`, `eventlog/eventlog.go`, `passkey/passkey.go`, `auth/store.go` | `Migrations []string` → `Schema *migrate.Set`. |
| `cmd/rastrillo/main.go` | Dispatch `migration`; extend `usage()`. |
| `cmd/rastrillo/dev.go:23` | Add `"internal"` to `watchDirs`; drift warning. |
| `cmd/rastrillo/new.go` | Scaffold `Models`, `migrations/`, `migrations.go`; `App()` calls `migrate.Apply`. |
| `examples/notes`, `examples/blog`, `examples/tickets` | Convert to the new call. |
| `SKILL.md` | §2 rewritten; additive-only rule retired. |

---

## Task 1: `Set` construction and ordering

**Files:**
- Create: `migrate/set.go`
- Test: `migrate/set_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `type Migration struct{ ID, SQL string; Fn func(*gorm.DB) error }`; `type Set struct{...}`; `func FromFS(fsys fs.FS, namespace string) (*Set, error)`; `func MustFromFS(fsys fs.FS, namespace string) *Set`; `func (s *Set) Add(m Migration) *Set`; `func Merge(sets ...*Set) *Set`; `func (s *Set) All() []Migration` (ordered, namespace-qualified IDs); `func Checksum(sql string) string`.

- [ ] **Step 1: Write the failing test**

```go
package migrate

import (
	"testing"
	"testing/fstest"
)

func TestFromFSOrdersAndIgnoresNonMigrations(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/0002_second.sql": {Data: []byte("CREATE TABLE b (n INTEGER);")},
		"migrations/0001_first.sql":  {Data: []byte("CREATE TABLE a (n INTEGER);")},
		"migrations/schema.sql":      {Data: []byte("-- snapshot, never applied")},
		"migrations/notes.md":        {Data: []byte("# hi")},
	}
	s, err := FromFS(fsys, "notes")
	if err != nil {
		t.Fatal(err)
	}
	got := s.All()
	if len(got) != 2 {
		t.Fatalf("got %d migrations, want 2: %+v", len(got), got)
	}
	if got[0].ID != "notes/0001_first" || got[1].ID != "notes/0002_second" {
		t.Fatalf("IDs = %q, %q; want notes/0001_first, notes/0002_second", got[0].ID, got[1].ID)
	}
}

func TestMergePreservesArgumentOrder(t *testing.T) {
	a := (&Set{namespace: "a"}).Add(Migration{ID: "0001_a", SQL: "SELECT 1;"})
	b := (&Set{namespace: "b"}).Add(Migration{ID: "0001_b", SQL: "SELECT 2;"})
	got := Merge(b, a).All()
	if got[0].ID != "b/0001_b" || got[1].ID != "a/0001_a" {
		t.Fatalf("Merge order = %q, %q; want b then a", got[0].ID, got[1].ID)
	}
}

func TestChecksumIgnoresWhitespace(t *testing.T) {
	if Checksum("CREATE  TABLE\n a (n INTEGER);") != Checksum("CREATE TABLE a (n INTEGER);") {
		t.Fatal("checksum changed on reformatting")
	}
	if Checksum("CREATE TABLE a (n INTEGER);") == Checksum("CREATE TABLE b (n INTEGER);") {
		t.Fatal("checksum collided on different SQL")
	}
}

func TestFromFSRejectsBadNameAndDuplicateID(t *testing.T) {
	_, err := FromFS(fstest.MapFS{
		"migrations/1_short.sql": {Data: []byte("SELECT 1;")},
	}, "x")
	if err == nil {
		t.Fatal("want error for a .sql file that is neither NNNN_name.sql nor schema.sql")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./migrate/ -run 'TestFromFS|TestMerge|TestChecksum' -v`
Expected: FAIL — package `migrate` does not exist.

- [ ] **Step 3: Write minimal implementation**

```go
// Package migrate applies an app's schema exactly once per migration
// and records what it did, replacing the two mechanisms — GORM
// AutoMigrate for models, raw Migrations []string for framework
// subsystems — that a Rastrillo app used to run side by side at boot.
//
// A Set is an ordered, namespaced list. Merge's argument order is
// apply order, which is how a package that must run after another
// (auth after sessions) states that requirement at the call site
// instead of in a comment.
//
// Migrations are forward-only. Production rollback for a CARLOS app
// is a point-in-time restore of the SQLite file the activator
// replicates, not a Down function, so none is offered.
package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"

	"gorm.io/gorm"
)

// Migration is one step. Exactly one of SQL or Fn is set: SQL is the
// default and the only thing `rastrillo migration generate` emits; Fn
// is the escape hatch for a change SQL cannot express.
//
// A Go migration must not reference the app's live model structs — a
// model changes over time and would silently change the meaning of a
// migration that already ran. Copy the struct into the migration.
type Migration struct {
	ID  string
	SQL string
	Fn  func(*gorm.DB) error
}

// Set is an ordered list of migrations sharing one namespace.
type Set struct {
	namespace  string
	migrations []Migration
}

// fileName is the only shape FromFS accepts: four digits, underscore,
// a lowercase name. schema.sql is excluded by not matching, which is
// how the accumulated snapshot lives beside the migrations it
// summarises without ever being applied.
var fileName = regexp.MustCompile(`^\d{4}_[a-z0-9_]+\.sql$`)

func FromFS(fsys fs.FS, namespace string) (*Set, error) {
	s := &Set{namespace: namespace}
	entries, err := fs.Glob(fsys, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	sort.Strings(entries)
	seen := map[string]bool{}
	for _, e := range entries {
		base := path.Base(e)
		if base == "schema.sql" {
			continue
		}
		if !fileName.MatchString(base) {
			return nil, fmt.Errorf("migrate: %s/%s: name must be NNNN_name.sql", namespace, base)
		}
		body, err := fs.ReadFile(fsys, e)
		if err != nil {
			return nil, err
		}
		id := strings.TrimSuffix(base, ".sql")
		if seen[id] {
			return nil, fmt.Errorf("migrate: %s/%s: duplicate migration id", namespace, id)
		}
		seen[id] = true
		s.migrations = append(s.migrations, Migration{ID: id, SQL: string(body)})
	}
	return s, nil
}

func MustFromFS(fsys fs.FS, namespace string) *Set {
	s, err := FromFS(fsys, namespace)
	if err != nil {
		panic(err)
	}
	return s
}

// Add appends a Go migration. It shares the ID space with the SQL
// files, so ordering between the two is just declaration order.
func (s *Set) Add(m Migration) *Set {
	s.migrations = append(s.migrations, m)
	return s
}

// Merge concatenates sets in argument order, which is apply order.
func Merge(sets ...*Set) *Set {
	out := &Set{}
	for _, s := range sets {
		if s == nil {
			continue
		}
		for _, m := range s.migrations {
			m.ID = s.namespace + "/" + m.ID
			out.migrations = append(out.migrations, m)
		}
	}
	// Already qualified; All must not qualify a second time.
	out.namespace = ""
	return out
}

// All returns the migrations in apply order, with namespace-qualified
// IDs.
func (s *Set) All() []Migration {
	out := make([]Migration, 0, len(s.migrations))
	for _, m := range s.migrations {
		if s.namespace != "" {
			m.ID = s.namespace + "/" + m.ID
		}
		out = append(out, m)
	}
	return out
}

// Checksum detects a migration edited after it was applied. Whitespace
// is normalised so reformatting does not raise a false alarm; the cost
// is that a change confined to whitespace inside a string literal goes
// unnoticed, which is an acceptable trade for a change-detector.
func Checksum(sql string) string {
	sum := sha256.Sum256([]byte(strings.Join(strings.Fields(sql), " ")))
	return hex.EncodeToString(sum[:])
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./migrate/ -v`
Expected: PASS, all four tests.

- [ ] **Step 5: Commit**

```bash
git add migrate/set.go migrate/set_test.go
git commit -m "migrate: ordered, namespaced migration sets

Merge's argument order is apply order, so a package that must run
after another states it at the call site instead of in a comment.
FromFS ignores schema.sql by pattern, which is what lets the
accumulated snapshot live beside the migrations it summarises."
```

---

## Task 2: Structured schema snapshots

**Files:**
- Create: `migrate/schema.go`
- Test: `migrate/schema_test.go`

**Interfaces:**
- Consumes: nothing from Task 1.
- Produces: `type Column struct{ Name, Type string; NotNull bool; Default string; PK int }`; `type Table struct{ Name string; Columns []Column; Indexes []Index }`; `type Index struct{ Name string; Unique bool; Columns []string }`; `type Snapshot struct{ Tables []Table }`; `func Read(ctx context.Context, q Querier) (Snapshot, error)`; `func (s Snapshot) Equal(other Snapshot) bool`; `func (s Snapshot) Diff(other Snapshot) []string`; `func (s Snapshot) SQL() string`. `Querier` is `interface{ QueryContext(context.Context, string, ...any) (*sql.Rows, error) }`, satisfied by `*sql.DB` and `*sql.Conn`.

- [ ] **Step 1: Write the failing test**

```go
package migrate

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func memDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	// One connection: a second connection to ":memory:" is a second,
	// empty database.
	d.SetMaxOpenConns(1)
	t.Cleanup(func() { d.Close() })
	return d
}

func TestReadCapturesColumnsAndIndexes(t *testing.T) {
	d := memDB(t)
	ctx := context.Background()
	mustExec(t, d, `CREATE TABLE notes (
	  id INTEGER PRIMARY KEY,
	  title TEXT NOT NULL DEFAULT '',
	  body TEXT
	);`)
	mustExec(t, d, `CREATE INDEX notes_title ON notes (title);`)

	snap, err := Read(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Tables) != 1 || snap.Tables[0].Name != "notes" {
		t.Fatalf("tables = %+v, want one table 'notes'", snap.Tables)
	}
	cols := snap.Tables[0].Columns
	if len(cols) != 3 {
		t.Fatalf("columns = %+v, want 3", cols)
	}
	if cols[1].Name != "title" || !cols[1].NotNull {
		t.Fatalf("title column = %+v, want NotNull", cols[1])
	}
	if len(snap.Tables[0].Indexes) != 1 || snap.Tables[0].Indexes[0].Name != "notes_title" {
		t.Fatalf("indexes = %+v, want notes_title", snap.Tables[0].Indexes)
	}
}

func TestReadSkipsSQLiteInternalTables(t *testing.T) {
	d := memDB(t)
	mustExec(t, d, `CREATE TABLE t (id INTEGER PRIMARY KEY AUTOINCREMENT);`)
	mustExec(t, d, `INSERT INTO t DEFAULT VALUES;`)
	snap, err := Read(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	for _, tb := range snap.Tables {
		if tb.Name == "sqlite_sequence" {
			t.Fatal("Read returned the internal sqlite_sequence table")
		}
	}
}

func TestEqualIgnoresDDLFormatting(t *testing.T) {
	a, b := memDB(t), memDB(t)
	mustExec(t, a, "CREATE TABLE t (id INTEGER PRIMARY KEY, n TEXT NOT NULL);")
	mustExec(t, b, "CREATE TABLE t (\n  id  INTEGER  PRIMARY KEY,\n  n   TEXT NOT NULL\n);")
	sa, err := Read(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	sb, err := Read(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	if !sa.Equal(sb) {
		t.Fatalf("formatting changed the snapshot:\n%v", sa.Diff(sb))
	}
}

func TestDiffNamesTheMissingColumn(t *testing.T) {
	a, b := memDB(t), memDB(t)
	mustExec(t, a, "CREATE TABLE t (id INTEGER PRIMARY KEY);")
	mustExec(t, b, "CREATE TABLE t (id INTEGER PRIMARY KEY, extra TEXT);")
	sa, _ := Read(context.Background(), a)
	sb, _ := Read(context.Background(), b)
	d := sa.Diff(sb)
	if len(d) != 1 || !contains(d[0], "extra") {
		t.Fatalf("Diff = %v, want one entry naming 'extra'", d)
	}
}

func mustExec(t *testing.T, d *sql.DB, q string) {
	t.Helper()
	if _, err := d.Exec(q); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && strings.Contains(s, sub) }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./migrate/ -run TestRead -v`
Expected: FAIL — `undefined: Read`.

- [ ] **Step 3: Write minimal implementation**

`migrate/schema.go`. Read `sqlite_master` for tables and indexes, then `PRAGMA table_info` and `PRAGMA index_info` per table. Comparing this structure rather than DDL text is what makes formatting irrelevant.

```go
package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// Querier is the read surface Read needs, satisfied by *sql.DB and by
// a pinned *sql.Conn.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type Column struct {
	Name    string
	Type    string
	NotNull bool
	Default string
	PK      int
}

type Index struct {
	Name    string
	Unique  bool
	Columns []string
}

type Table struct {
	Name    string
	Columns []Column
	Indexes []Index
}

// Snapshot is a database's structure, read in a form that compares
// cleanly: two databases built by differently-formatted DDL produce
// equal Snapshots.
type Snapshot struct{ Tables []Table }

func Read(ctx context.Context, q Querier) (Snapshot, error) {
	var snap Snapshot
	rows, err := q.QueryContext(ctx, `SELECT name FROM sqlite_master
	  WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return snap, err
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return snap, err
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return snap, err
	}
	rows.Close()

	for _, name := range names {
		t := Table{Name: name}
		if t.Columns, err = readColumns(ctx, q, name); err != nil {
			return snap, err
		}
		if t.Indexes, err = readIndexes(ctx, q, name); err != nil {
			return snap, err
		}
		snap.Tables = append(snap.Tables, t)
	}
	return snap, nil
}

func readColumns(ctx context.Context, q Querier, table string) ([]Column, error) {
	rows, err := q.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%q)", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Column
	for rows.Next() {
		var (
			cid     int
			c       Column
			notNull int
			dflt    sql.NullString
		)
		if err := rows.Scan(&cid, &c.Name, &c.Type, &notNull, &dflt, &c.PK); err != nil {
			return nil, err
		}
		c.NotNull = notNull != 0
		c.Default = dflt.String
		// SQLite reports declared types with inconsistent case
		// depending on how the DDL was written; normalise so
		// "integer" and "INTEGER" are one type.
		c.Type = strings.ToUpper(strings.TrimSpace(c.Type))
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, rows.Err()
}

func readIndexes(ctx context.Context, q Querier, table string) ([]Index, error) {
	rows, err := q.QueryContext(ctx, fmt.Sprintf("PRAGMA index_list(%q)", table))
	if err != nil {
		return nil, err
	}
	type li struct {
		name   string
		unique bool
		origin string
	}
	var list []li
	for rows.Next() {
		var (
			seq     int
			name    string
			unique  int
			origin  string
			partial int
		)
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			rows.Close()
			return nil, err
		}
		// origin "pk" and "u" are indexes SQLite creates for PRIMARY
		// KEY and UNIQUE constraints; they are already captured by the
		// column data, and their auto-generated names differ between
		// two databases with identical structure.
		if origin != "c" {
			continue
		}
		list = append(list, li{name, unique != 0, origin})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	var out []Index
	for _, l := range list {
		cols, err := indexColumns(ctx, q, l.name)
		if err != nil {
			return nil, err
		}
		out = append(out, Index{Name: l.name, Unique: l.unique, Columns: cols})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func indexColumns(ctx context.Context, q Querier, index string) ([]string, error) {
	rows, err := q.QueryContext(ctx, fmt.Sprintf("PRAGMA index_info(%q)", index))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var (
			seqno, cid int
			name       sql.NullString
		)
		if err := rows.Scan(&seqno, &cid, &name); err != nil {
			return nil, err
		}
		out = append(out, name.String)
	}
	return out, rows.Err()
}

func (s Snapshot) Equal(other Snapshot) bool { return len(s.Diff(other)) == 0 }

// Diff reports, in human-readable lines, what other has that s lacks
// and vice versa. It is both the check failure message and the input
// to Generate's destructive pass.
func (s Snapshot) Diff(other Snapshot) []string {
	var out []string
	mine, theirs := index(s), index(other)
	for name, t := range theirs {
		m, ok := mine[name]
		if !ok {
			out = append(out, "missing table "+name)
			continue
		}
		out = append(out, diffTable(m, t)...)
	}
	for name := range mine {
		if _, ok := theirs[name]; !ok {
			out = append(out, "extra table "+name)
		}
	}
	sort.Strings(out)
	return out
}

func diffTable(mine, theirs Table) []string {
	var out []string
	mc, tc := cols(mine), cols(theirs)
	for name, c := range tc {
		m, ok := mc[name]
		if !ok {
			out = append(out, fmt.Sprintf("%s: missing column %s", mine.Name, name))
			continue
		}
		if m != c {
			out = append(out, fmt.Sprintf("%s: column %s differs (%+v vs %+v)", mine.Name, name, m, c))
		}
	}
	for name := range mc {
		if _, ok := tc[name]; !ok {
			out = append(out, fmt.Sprintf("%s: extra column %s", mine.Name, name))
		}
	}
	mi, ti := idxs(mine), idxs(theirs)
	for name, i := range ti {
		m, ok := mi[name]
		if !ok {
			out = append(out, fmt.Sprintf("%s: missing index %s", mine.Name, name))
			continue
		}
		if m.Unique != i.Unique || strings.Join(m.Columns, ",") != strings.Join(i.Columns, ",") {
			out = append(out, fmt.Sprintf("%s: index %s differs", mine.Name, name))
		}
	}
	for name := range mi {
		if _, ok := ti[name]; !ok {
			out = append(out, fmt.Sprintf("%s: extra index %s", mine.Name, name))
		}
	}
	return out
}

func index(s Snapshot) map[string]Table {
	m := map[string]Table{}
	for _, t := range s.Tables {
		m[t.Name] = t
	}
	return m
}

func cols(t Table) map[string]Column {
	m := map[string]Column{}
	for _, c := range t.Columns {
		m[c.Name] = c
	}
	return m
}

func idxs(t Table) map[string]Index {
	m := map[string]Index{}
	for _, i := range t.Indexes {
		m[i.Name] = i
	}
	return m
}
```

Add `"strings"` to the test file's imports for the `contains` helper.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./migrate/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add migrate/schema.go migrate/schema_test.go
git commit -m "migrate: structured schema snapshots

Compare tables, columns and indexes read from PRAGMA rather than
DDL text, so two databases built by differently-formatted SQL
compare equal. Constraint-backing indexes (origin pk/u) are skipped:
their auto-generated names differ between structurally identical
databases."
```

---

## Task 3: The ledger and `Apply`

**Files:**
- Create: `migrate/apply.go`
- Modify: `db/db.go` (add `Writer()`)
- Test: `migrate/apply_test.go`

**Interfaces:**
- Consumes: `Set.All()`, `Checksum` (Task 1).
- Produces: `type Result struct{ Applied []string; Skipped int; Adopted bool }`; `func Apply(ctx context.Context, d *db.DB, s *Set) (Result, error)`; `func (d *DB) Writer() *sql.DB` in package `db`.

- [ ] **Step 1: Write the failing test**

```go
package migrate

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/db"
)

func openDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "app.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func set(ns string, ms ...Migration) *Set {
	s := &Set{namespace: ns}
	for _, m := range ms {
		s.Add(m)
	}
	return s
}

func TestApplyRunsOnceAndRecords(t *testing.T) {
	d := openDB(t)
	s := set("notes", Migration{ID: "0001_init", SQL: "CREATE TABLE notes (id INTEGER PRIMARY KEY);"})

	r, err := Apply(context.Background(), d, s)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Applied) != 1 || r.Applied[0] != "notes/0001_init" {
		t.Fatalf("Applied = %v, want [notes/0001_init]", r.Applied)
	}

	// Second call is a no-op: the CREATE TABLE has no IF NOT EXISTS,
	// so a re-run would error if the ledger were not consulted.
	r2, err := Apply(context.Background(), d, s)
	if err != nil {
		t.Fatal(err)
	}
	if len(r2.Applied) != 0 || r2.Skipped != 1 {
		t.Fatalf("second Apply = %+v, want 0 applied / 1 skipped", r2)
	}
}

func TestApplyRunsGoMigrations(t *testing.T) {
	d := openDB(t)
	s := set("notes",
		Migration{ID: "0001_init", SQL: "CREATE TABLE notes (id INTEGER PRIMARY KEY, n INTEGER);"},
		Migration{ID: "0002_seed", Fn: func(g *gorm.DB) error {
			return g.Exec("INSERT INTO notes (id, n) VALUES (1, 42)").Error
		}},
	)
	if _, err := Apply(context.Background(), d, s); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := d.G.Raw("SELECT n FROM notes WHERE id = 1").Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 42 {
		t.Fatalf("n = %d, want 42", n)
	}
}

func TestApplyRollsBackFailedMigrationAndLeavesLedgerClean(t *testing.T) {
	d := openDB(t)
	s := set("notes",
		Migration{ID: "0001_init", SQL: "CREATE TABLE notes (id INTEGER PRIMARY KEY);"},
		Migration{ID: "0002_bad", SQL: "CREATE TABLE ok (n INTEGER); CREATE TABLE ok (n INTEGER);"},
	)
	if _, err := Apply(context.Background(), d, s); err == nil {
		t.Fatal("want error from the duplicate CREATE TABLE")
	}
	// 0001 committed; 0002 rolled back entirely, including the table
	// its first statement created.
	var count int64
	d.G.Raw("SELECT count(*) FROM rastrillo_migrations").Scan(&count)
	if count != 1 {
		t.Fatalf("ledger rows = %d, want 1", count)
	}
	var name string
	d.G.Raw("SELECT name FROM sqlite_master WHERE type='table' AND name='ok'").Scan(&name)
	if name != "" {
		t.Fatal("table 'ok' survived a rolled-back migration")
	}
}

func TestApplyRefusesEditedMigration(t *testing.T) {
	d := openDB(t)
	orig := set("notes", Migration{ID: "0001_init", SQL: "CREATE TABLE notes (id INTEGER PRIMARY KEY);"})
	if _, err := Apply(context.Background(), d, orig); err != nil {
		t.Fatal(err)
	}
	edited := set("notes", Migration{ID: "0001_init", SQL: "CREATE TABLE notes (id INTEGER PRIMARY KEY, oops TEXT);"})
	_, err := Apply(context.Background(), d, edited)
	if err == nil {
		t.Fatal("want error when an applied migration's SQL changed")
	}
	if !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("error = %v, want it to name the immutability rule", err)
	}
}

func TestApplyToleratesReformattedMigration(t *testing.T) {
	d := openDB(t)
	if _, err := Apply(context.Background(), d,
		set("notes", Migration{ID: "0001_init", SQL: "CREATE TABLE notes (id INTEGER PRIMARY KEY);"})); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(context.Background(), d,
		set("notes", Migration{ID: "0001_init", SQL: "CREATE TABLE notes (\n  id INTEGER PRIMARY KEY\n);"})); err != nil {
		t.Fatalf("reformatting tripped the checksum: %v", err)
	}
}

var _ = errors.Is
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./migrate/ -run TestApply -v`
Expected: FAIL — `undefined: Apply`, `d.Writer undefined`.

- [ ] **Step 3: Write minimal implementation**

First, `db/db.go` — add below `Close`:

```go
// Writer is the write pool: one connection, because SQLite allows one
// writer. migrate.Apply needs it directly rather than through the
// resolver, because it pins a single connection for the whole run —
// PRAGMA foreign_keys is per-connection and a table rebuild must
// toggle it outside the transaction.
func (d *DB) Writer() *sql.DB { return d.writer }
```

Then `migrate/apply.go`:

```go
package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"amadan.net/rastrillo/rastrillo/db"
)

const ledgerDDL = `CREATE TABLE IF NOT EXISTS rastrillo_migrations (
  id         TEXT PRIMARY KEY,
  applied_at TEXT NOT NULL,
  checksum   TEXT NOT NULL
);`

// Result reports what a call did, so an app can log one line at boot.
type Result struct {
	Applied []string
	Skipped int
	Adopted bool
}

// Apply runs every migration in s that the ledger does not already
// record, in order, each in its own BEGIN IMMEDIATE transaction with
// its ledger row written inside that same transaction.
//
// Three properties follow from that shape, and all three matter for a
// hibernating app the activator may SIGKILL at any moment: a wake
// killed mid-migration rolls back cleanly and the next wake retries
// from the same point; progress is preserved across wakes, so a long
// set converges even if every wake is cut short; and BEGIN IMMEDIATE
// takes the write lock up front, so two instances booting at once
// cannot both apply.
//
// The whole run happens on one pinned connection. PRAGMA foreign_keys
// is per-connection state and SQLite's twelve-step table rebuild
// requires toggling it outside the transaction, so a pooled
// connection would be a correctness bug, not just a slow path.
func Apply(ctx context.Context, d *db.DB, s *Set) (Result, error) {
	var res Result
	conn, err := d.Writer().Conn(ctx)
	if err != nil {
		return res, err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, ledgerDDL); err != nil {
		return res, fmt.Errorf("migrate: create ledger: %w", err)
	}

	migrations := s.All()
	applied, err := readLedger(ctx, conn)
	if err != nil {
		return res, err
	}

	if len(applied) == 0 {
		adopted, err := adopt(ctx, conn, migrations)
		if err != nil {
			return res, err
		}
		if adopted {
			res.Adopted = true
			res.Skipped = len(migrations)
			return res, nil
		}
	}

	for _, m := range migrations {
		sum, ok := applied[m.ID]
		if ok {
			if m.SQL != "" && sum != Checksum(m.SQL) {
				return res, fmt.Errorf(
					"migrate: %s was applied with different SQL than the file now contains; "+
						"migrations are immutable once applied — add a new one instead of editing this", m.ID)
			}
			res.Skipped++
			continue
		}
		if err := runOne(ctx, conn, d.G, m); err != nil {
			return res, fmt.Errorf("migrate: %s: %w", m.ID, err)
		}
		res.Applied = append(res.Applied, m.ID)
	}
	return res, nil
}

func readLedger(ctx context.Context, conn *sql.Conn) (map[string]string, error) {
	rows, err := conn.QueryContext(ctx, "SELECT id, checksum FROM rastrillo_migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, sum string
		if err := rows.Scan(&id, &sum); err != nil {
			return nil, err
		}
		out[id] = sum
	}
	return out, rows.Err()
}

// needsRebuild reports whether a migration performs SQLite's
// twelve-step table rebuild, which must run with foreign keys off and
// a foreign_key_check before commit.
func needsRebuild(sql string) bool {
	u := strings.ToUpper(sql)
	return strings.Contains(u, "DROP COLUMN") ||
		strings.Contains(u, "RENAME TO") ||
		strings.Contains(u, "__TEMP__")
}

func runOne(ctx context.Context, conn *sql.Conn, g *gorm.DB, m Migration) error {
	rebuild := m.SQL != "" && needsRebuild(m.SQL)
	if rebuild {
		// Per-connection, and it must be outside the transaction:
		// SQLite silently ignores this pragma inside one.
		if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
			return err
		}
		defer conn.ExecContext(ctx, "PRAGMA foreign_keys=ON")
	}

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			conn.ExecContext(ctx, "ROLLBACK")
		}
	}()

	switch {
	case m.Fn != nil:
		if err := m.Fn(g); err != nil {
			return err
		}
	default:
		if _, err := conn.ExecContext(ctx, m.SQL); err != nil {
			return err
		}
	}

	if rebuild {
		rows, err := conn.QueryContext(ctx, "PRAGMA foreign_key_check")
		if err != nil {
			return err
		}
		bad := rows.Next()
		rows.Close()
		if bad {
			return fmt.Errorf("foreign_key_check failed after rebuild")
		}
	}

	if _, err := conn.ExecContext(ctx,
		"INSERT INTO rastrillo_migrations (id, applied_at, checksum) VALUES (?, ?, ?)",
		m.ID, time.Now().UTC().Format(time.RFC3339Nano), Checksum(m.SQL)); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}
```

Note on `m.Fn`: it runs through `g`, not the pinned connection, so a Go migration is *not* inside the pinned transaction. Document that in the `Migration.Fn` doc comment — a Go migration must be idempotent, because it cannot be rolled back with the ledger row. Add to `migrate/set.go`:

```go
	// Fn runs outside the pinned transaction that wraps a SQL
	// migration, because it needs the *gorm.DB and its own pool. It
	// must therefore be idempotent: a failure after Fn's own writes
	// leaves no ledger row, and the next boot runs it again.
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./migrate/ ./db/ -v`
Expected: PASS. (`TestApplyRunsGoMigrations` needs `"gorm.io/gorm"` imported in the test file.)

- [ ] **Step 5: Commit**

```bash
git add migrate/apply.go migrate/apply_test.go db/db.go
git commit -m "migrate: the ledger, on one pinned connection

Each migration gets its own BEGIN IMMEDIATE with its ledger row
inside the same transaction: a SIGKILLed wake rolls back and the
next one retries, progress survives across wakes, and two instances
booting at once cannot both apply.

The run pins a single *sql.Conn because PRAGMA foreign_keys is
per-connection and a table rebuild must toggle it outside the
transaction — a pooled connection would be a correctness bug."
```

---

## Task 4: Adoption of existing databases

**Files:**
- Create: `migrate/adopt.go`, `migrate/memory.go`
- Test: `migrate/adopt_test.go`

**Interfaces:**
- Consumes: `Read`, `Snapshot.Equal`, `Snapshot.Diff` (Task 2); `Migration` (Task 1).
- Produces: `func adopt(ctx context.Context, conn *sql.Conn, ms []Migration) (bool, error)` (unexported, called from `Apply`); `func Replay(ctx context.Context, ms []Migration) (*sql.DB, error)` — an in-memory database with every migration's SQL applied; `func Stamp(ctx context.Context, conn *sql.Conn, ms []Migration, through string) error`.

- [ ] **Step 1: Write the failing test**

```go
package migrate

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/db"
)

// legacy is the shape a deployed app already has: the tables exist,
// created by the old Migrations []string path, and there is no ledger.
const legacy = `CREATE TABLE IF NOT EXISTS sessions (
	  token_hash TEXT PRIMARY KEY,
	  subject    TEXT NOT NULL,
	  created_at TEXT NOT NULL
	);`

func TestAdoptStampsMatchingDatabaseWithoutRunningDDL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	d, err := db.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.G.Exec(legacy).Error; err != nil {
		t.Fatal(err)
	}
	// A row proves no DDL re-ran: a CREATE TABLE would have failed,
	// and a DROP/rebuild would have lost this.
	if err := d.G.Exec(`INSERT INTO sessions VALUES ('h','s','now')`).Error; err != nil {
		t.Fatal(err)
	}

	s := set("sessions", Migration{ID: "0001_init", SQL: legacy})
	r, err := Apply(context.Background(), d, s)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Adopted {
		t.Fatalf("Result = %+v, want Adopted", r)
	}
	if len(r.Applied) != 0 {
		t.Fatalf("Applied = %v, want none — adoption must run zero DDL", r.Applied)
	}
	var n int64
	d.G.Raw("SELECT count(*) FROM sessions").Scan(&n)
	if n != 1 {
		t.Fatalf("row count = %d, want the pre-existing row intact", n)
	}
	var ledger int64
	d.G.Raw("SELECT count(*) FROM rastrillo_migrations").Scan(&ledger)
	if ledger != 1 {
		t.Fatalf("ledger rows = %d, want 1 stamped", ledger)
	}
	d.Close()
}

func TestAdoptRefusesMismatchedDatabase(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "app.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.G.Exec("CREATE TABLE sessions (token_hash TEXT PRIMARY KEY, unexpected TEXT)").Error; err != nil {
		t.Fatal(err)
	}
	_, err = Apply(context.Background(), d, set("sessions", Migration{ID: "0001_init", SQL: legacy}))
	if err == nil {
		t.Fatal("want refusal for a non-empty database that does not match the migration set")
	}
	if !strings.Contains(err.Error(), "unexpected") {
		t.Fatalf("error = %v, want it to name the structural difference", err)
	}
}

func TestEmptyDatabaseAppliesNormally(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "app.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	r, err := Apply(context.Background(), d, set("sessions", Migration{ID: "0001_init", SQL: legacy}))
	if err != nil {
		t.Fatal(err)
	}
	if r.Adopted {
		t.Fatal("an empty database must take the normal apply path, not adoption")
	}
	if len(r.Applied) != 1 {
		t.Fatalf("Applied = %v, want one", r.Applied)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./migrate/ -run 'TestAdopt|TestEmptyDatabase' -v`
Expected: FAIL — `undefined: adopt`.

- [ ] **Step 3: Write minimal implementation**

`migrate/memory.go`:

```go
package migrate

import (
	"context"
	"database/sql"

	_ "modernc.org/sqlite"
)

// Memory opens an empty in-memory database. Connections are capped at
// one because a second connection to ":memory:" is a second, empty
// database — the caller closes it.
func Memory() (*sql.DB, error) {
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}
	d.SetMaxOpenConns(1)
	return d, nil
}

// Replay applies every SQL migration in ms to a fresh in-memory
// database and returns it. Go migrations are skipped: they may need
// data or services that do not exist here, and the structure they
// produce is not what a schema comparison is about.
//
// This is the "current schema" side of every comparison in the
// package — adoption, check, and generate all start here, and none of
// them touches a real database.
func Replay(ctx context.Context, ms []Migration) (*sql.DB, error) {
	d, err := Memory()
	if err != nil {
		return nil, err
	}
	for _, m := range ms {
		if m.SQL == "" {
			continue
		}
		if _, err := d.ExecContext(ctx, m.SQL); err != nil {
			d.Close()
			return nil, fmt.Errorf("migrate: replay %s: %w", m.ID, err)
		}
	}
	return d, nil
}
```

(Add `"fmt"` to that file's imports.)

`migrate/adopt.go`:

```go
package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// adopt handles first boot against a database that has no ledger.
//
// An empty ledger cannot simply mean "replay everything": a deployed
// app already has its tables, and migration 0005_add_column would
// fail on a column that is already there. That is the failure the old
// isDuplicateColumn swallow existed to paper over.
//
// It returns true when it stamped the ledger and the caller should run
// nothing.
func adopt(ctx context.Context, conn *sql.Conn, ms []Migration) (bool, error) {
	empty, err := isEmpty(ctx, conn)
	if err != nil {
		return false, err
	}
	if empty {
		// New app. Normal path.
		return false, nil
	}

	live, err := Read(ctx, conn)
	if err != nil {
		return false, err
	}
	mem, err := Replay(ctx, ms)
	if err != nil {
		return false, err
	}
	defer mem.Close()
	want, err := Read(ctx, mem)
	if err != nil {
		return false, err
	}

	if diff := live.Diff(want); len(diff) > 0 {
		return false, fmt.Errorf(
			"migrate: this database has tables but no migration ledger, and its schema does not match "+
				"the migration set, so it cannot be adopted safely:\n  %s\n"+
				"Read the differences, then stamp the ledger with: rastrillo migration baseline --db <path>",
			strings.Join(diff, "\n  "))
	}
	return true, Stamp(ctx, conn, ms, "")
}

// isEmpty reports whether the database has no user tables. The ledger
// itself is excluded: Apply creates it before calling adopt.
func isEmpty(ctx context.Context, conn *sql.Conn) (bool, error) {
	var n int
	err := conn.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master
	  WHERE type = 'table' AND name NOT LIKE 'sqlite_%' AND name <> 'rastrillo_migrations'`).Scan(&n)
	return n == 0, err
}

// Stamp records migrations as applied without running them. When
// through is non-empty, it stops after that ID — the escape hatch for
// a database that is partway through the set.
func Stamp(ctx context.Context, conn *sql.Conn, ms []Migration, through string) error {
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	for _, m := range ms {
		if _, err := conn.ExecContext(ctx,
			"INSERT OR IGNORE INTO rastrillo_migrations (id, applied_at, checksum) VALUES (?, ?, ?)",
			m.ID, time.Now().UTC().Format(time.RFC3339Nano), Checksum(m.SQL)); err != nil {
			conn.ExecContext(ctx, "ROLLBACK")
			return err
		}
		if through != "" && m.ID == through {
			break
		}
	}
	_, err := conn.ExecContext(ctx, "COMMIT")
	return err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./migrate/ -v`
Expected: PASS, all tests including Tasks 1–3.

- [ ] **Step 5: Commit**

```bash
git add migrate/adopt.go migrate/memory.go migrate/adopt_test.go
git commit -m "migrate: adopt databases that predate the ledger

Three branches on first boot: an empty database applies normally, a
non-empty one whose structure matches the replayed set is stamped
without running any DDL, and a non-empty one that does not match
refuses to boot and prints the difference.

Refusing is the right default because there is no operator moment in
a hibernating deploy — applying arbitrary DDL to a database the
runner cannot account for is worse than failing where /healthz sees
it."
```

---

## Task 5: The diff engine

**Files:**
- Create: `migrate/diff.go`
- Test: `migrate/diff_test.go`

**Interfaces:**
- Consumes: `Replay`, `Memory` (Task 4); `Read`, `Snapshot` (Task 2).
- Produces: `type Change struct{ SQL string; Destructive bool }`; `func Generate(ctx context.Context, ms []Migration, models []any) ([]Change, error)`; `func SchemaSQL(ctx context.Context, ms []Migration) (string, error)`.

**Approach.** `Generate` never assembles DDL by hand. It replays the existing migrations into `:memory:`, then runs GORM `AutoMigrate(models...)` against that database with a GORM logger that records every statement executed — the recorded SQL *is* the additive part of the migration, `gormlite`'s rebuild path included. `AutoMigrate` never drops, so a second pass structurally diffs the result against a clean `AutoMigrate` of the models alone and emits any leftover drops through `Migrator().DropColumn`/`DropTable`/`DropIndex` under the same recorder, marked `Destructive`.

- [ ] **Step 1: Write the failing test**

```go
package migrate

import (
	"context"
	"strings"
	"testing"
)

type genNote struct {
	ID    int64
	Title string
}

type genNoteWithBody struct {
	ID    int64
	Title string
	Body  string
}

func (genNoteWithBody) TableName() string { return "gen_notes" }
func (genNote) TableName() string         { return "gen_notes" }

func TestGenerateEmitsCreateTableForNewModel(t *testing.T) {
	changes, err := Generate(context.Background(), nil, []any{&genNote{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) == 0 {
		t.Fatal("no changes generated for a model with no migrations")
	}
	joined := allSQL(changes)
	if !strings.Contains(strings.ToUpper(joined), "CREATE TABLE") || !strings.Contains(joined, "gen_notes") {
		t.Fatalf("generated SQL = %q, want a CREATE TABLE for gen_notes", joined)
	}
	for _, c := range changes {
		if c.Destructive {
			t.Fatalf("creating a table was marked destructive: %q", c.SQL)
		}
	}
}

func TestGenerateEmitsAddColumnForNewField(t *testing.T) {
	// The existing migration set already created the narrow table.
	existing := []Migration{{ID: "0001_init", SQL: "CREATE TABLE gen_notes (id INTEGER PRIMARY KEY, title TEXT);"}}
	changes, err := Generate(context.Background(), existing, []any{&genNoteWithBody{}})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.ToUpper(allSQL(changes))
	if !strings.Contains(joined, "ADD COLUMN") || !strings.Contains(strings.ToLower(allSQL(changes)), "body") {
		t.Fatalf("generated SQL = %q, want ALTER TABLE ... ADD COLUMN body", allSQL(changes))
	}
}

func TestGenerateIsEmptyWhenModelsAndMigrationsAgree(t *testing.T) {
	existing := []Migration{{ID: "0001_init", SQL: "CREATE TABLE gen_notes (id INTEGER PRIMARY KEY, title TEXT);"}}
	changes, err := Generate(context.Background(), existing, []any{&genNote{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("changes = %q, want none — models and migrations already agree", allSQL(changes))
	}
}

func TestGenerateMarksDroppedColumnDestructive(t *testing.T) {
	// Migrations have a column the model no longer declares.
	existing := []Migration{{ID: "0001_init",
		SQL: "CREATE TABLE gen_notes (id INTEGER PRIMARY KEY, title TEXT, gone TEXT);"}}
	changes, err := Generate(context.Background(), existing, []any{&genNote{}})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, c := range changes {
		if c.Destructive {
			found = true
		}
	}
	if !found {
		t.Fatalf("changes = %q, want one marked Destructive for the dropped column", allSQL(changes))
	}
}

func TestSchemaSQLReflectsAppliedMigrations(t *testing.T) {
	out, err := SchemaSQL(context.Background(), []Migration{
		{ID: "0001_init", SQL: "CREATE TABLE gen_notes (id INTEGER PRIMARY KEY);"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "gen_notes") {
		t.Fatalf("schema.sql = %q, want it to contain gen_notes", out)
	}
}

func allSQL(cs []Change) string {
	var b strings.Builder
	for _, c := range cs {
		b.WriteString(c.SQL)
		b.WriteString("\n")
	}
	return b.String()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./migrate/ -run 'TestGenerate|TestSchemaSQL' -v`
Expected: FAIL — `undefined: Generate`.

- [ ] **Step 3: Write minimal implementation**

```go
package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"sync"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"amadan.net/rastrillo/rastrillo/gormlite"
)

// Change is one generated statement.
type Change struct {
	SQL         string
	Destructive bool
}

// recorder is a GORM logger that captures every statement GORM
// executes. It is how this package generates DDL without writing any:
// AutoMigrate already knows how to produce correct SQLite DDL,
// including gormlite's twelve-step rebuild, so the generator runs it
// against a throwaway in-memory copy and keeps what it said.
type recorder struct {
	logger.Interface
	mu   sync.Mutex
	sqls []string
}

func (r *recorder) Trace(ctx context.Context, begin time.Time,
	fc func() (string, int64), err error) {
	s, _ := fc()
	r.mu.Lock()
	r.sqls = append(r.sqls, s)
	r.mu.Unlock()
}

func (r *recorder) take() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.sqls
	r.sqls = nil
	return out
}

// gormOn wraps an existing *sql.DB in GORM with a recording logger.
func gormOn(d *sql.DB) (*gorm.DB, *recorder, error) {
	rec := &recorder{Interface: logger.Discard}
	g, err := gorm.Open(gormlite.Dialector{Conn: d}, &gorm.Config{Logger: rec})
	return g, rec, err
}

// Generate computes the migration that would bring a database built
// from ms up to what models declares.
//
// It emits nothing by hand. The additive pass runs AutoMigrate against
// a replay of ms and keeps the SQL GORM executed. The destructive pass
// structurally compares that result against a clean AutoMigrate of
// models alone — AutoMigrate never drops, so anything left over is a
// column, index or table the models no longer declare — and emits the
// drops through the migrator, also captured.
func Generate(ctx context.Context, ms []Migration, models []any) ([]Change, error) {
	current, err := Replay(ctx, ms)
	if err != nil {
		return nil, err
	}
	defer current.Close()

	g, rec, err := gormOn(current)
	if err != nil {
		return nil, err
	}
	if err := g.AutoMigrate(models...); err != nil {
		return nil, fmt.Errorf("migrate: automigrate models: %w", err)
	}
	var out []Change
	for _, s := range rec.take() {
		if isDDL(s) {
			out = append(out, Change{SQL: ensureSemicolon(s)})
		}
	}

	// Desired: models alone, in a clean database.
	clean, err := Memory()
	if err != nil {
		return nil, err
	}
	defer clean.Close()
	cg, _, err := gormOn(clean)
	if err != nil {
		return nil, err
	}
	if err := cg.AutoMigrate(models...); err != nil {
		return nil, err
	}
	want, err := Read(ctx, clean)
	if err != nil {
		return nil, err
	}
	have, err := Read(ctx, current)
	if err != nil {
		return nil, err
	}

	drops, err := dropChanges(ctx, g, rec, have, want)
	if err != nil {
		return nil, err
	}
	return append(out, drops...), nil
}

// dropChanges emits, through the migrator so the SQL is gormlite's own,
// the removals AutoMigrate will never perform.
func dropChanges(ctx context.Context, g *gorm.DB, rec *recorder, have, want Snapshot) ([]Change, error) {
	var out []Change
	wantTables := index(want)
	haveTables := index(have)

	names := make([]string, 0, len(haveTables))
	for n := range haveTables {
		names = append(names, n)
	}
	sort.Strings(names)

	m := g.Migrator()
	for _, name := range names {
		if name == "rastrillo_migrations" {
			continue
		}
		wt, ok := wantTables[name]
		if !ok {
			if err := m.DropTable(name); err != nil {
				return nil, err
			}
			out = append(out, recorded(rec, true)...)
			continue
		}
		wc := cols(wt)
		for _, c := range haveTables[name].Columns {
			if _, ok := wc[c.Name]; ok {
				continue
			}
			if err := m.DropColumn(tableRef{name}, c.Name); err != nil {
				return nil, err
			}
			out = append(out, recorded(rec, true)...)
		}
		wi := idxs(wt)
		for _, i := range haveTables[name].Indexes {
			if _, ok := wi[i.Name]; ok {
				continue
			}
			if err := m.DropIndex(tableRef{name}, i.Name); err != nil {
				return nil, err
			}
			out = append(out, recorded(rec, true)...)
		}
	}
	return out, nil
}

// tableRef lets the migrator address a table by name when there is no
// model for it — a table the models dropped has no struct left.
type tableRef struct{ name string }

func (t tableRef) TableName() string { return t.name }

func recorded(rec *recorder, destructive bool) []Change {
	var out []Change
	for _, s := range rec.take() {
		if isDDL(s) {
			out = append(out, Change{SQL: ensureSemicolon(s), Destructive: destructive})
		}
	}
	return out
}

// isDDL filters the recorder's capture down to schema statements.
// AutoMigrate also issues introspection queries (PRAGMA, sqlite_master
// SELECTs) that must not end up in a migration file.
func isDDL(s string) bool {
	u := strings.ToUpper(strings.TrimSpace(s))
	for _, p := range []string{"CREATE TABLE", "CREATE UNIQUE INDEX", "CREATE INDEX",
		"ALTER TABLE", "DROP TABLE", "DROP INDEX", "INSERT INTO"} {
		if strings.HasPrefix(u, p) {
			return true
		}
	}
	return false
}

func ensureSemicolon(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, ";") {
		return s
	}
	return s + ";"
}

// SchemaSQL is the accumulated snapshot written to migrations/schema.sql:
// the DDL of a database with every migration applied, read back from
// sqlite_master so it is SQLite's own normalised text rather than the
// concatenation of the migration files.
func SchemaSQL(ctx context.Context, ms []Migration) (string, error) {
	d, err := Replay(ctx, ms)
	if err != nil {
		return "", err
	}
	defer d.Close()
	rows, err := d.QueryContext(ctx, `SELECT sql FROM sqlite_master
	  WHERE sql IS NOT NULL AND name NOT LIKE 'sqlite_%' ORDER BY type DESC, name`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var b strings.Builder
	b.WriteString("-- Generated by rastrillo migration generate; DO NOT EDIT.\n")
	b.WriteString("-- The schema every migration in this directory adds up to.\n\n")
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return "", err
		}
		b.WriteString(strings.TrimSpace(s))
		b.WriteString(";\n\n")
	}
	return b.String(), rows.Err()
}
```

Add `"time"` to the import block for `recorder.Trace`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./migrate/ -v`
Expected: PASS. If `TestGenerateMarksDroppedColumnDestructive` fails because `gormlite`'s `DropColumn` needs a real model, confirm `tableRef` satisfies GORM's table-name interface; if not, pass the raw table name string, which `Migrator.DropColumn` also accepts.

- [ ] **Step 5: Commit**

```bash
git add migrate/diff.go migrate/diff_test.go
git commit -m "migrate: generate migrations without writing DDL

AutoMigrate already knows how to emit correct SQLite DDL, gormlite's
twelve-step rebuild included. So the generator replays the existing
migrations into :memory:, runs AutoMigrate against that with a
recording logger, and keeps what GORM said.

AutoMigrate never drops, so a second structural pass compares the
result against a clean AutoMigrate of the models and emits the
leftovers through the migrator, marked destructive."
```

---

## Task 6: The schema dump helper

**Files:**
- Create: `migrate/dump/dump.go`
- Test: `migrate/dump/dump_test.go`

**Interfaces:**
- Consumes: `Generate`, `SchemaSQL`, `Replay` (Task 5), `Read` (Task 2).
- Produces: `func Main(ms []migrate.Migration, models []any)` — the entry point the CLI's generated loader program calls; writes a JSON `Payload{Changes []migrate.Change; Schema string; Diff []string}` to stdout.

**Why a separate package.** `rastrillo` is a standalone binary and cannot import an app's model structs. The CLI writes a tiny `package main` into the app module that imports both the app package and this one, `go run`s it, and reads JSON back (spec §5.5). Keeping `Main` here means that generated program is four lines and has nothing to get wrong.

- [ ] **Step 1: Write the failing test**

```go
package dump

import (
	"encoding/json"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/migrate"
)

type dumpNote struct {
	ID    int64
	Title string
}

func TestComputeReportsChangesAndSchema(t *testing.T) {
	p, err := Compute(nil, []any{&dumpNote{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Changes) == 0 {
		t.Fatal("want a CREATE TABLE change for a model with no migrations")
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("payload must marshal to JSON: %v", err)
	}
	if !strings.Contains(string(b), "dump_notes") {
		t.Fatalf("payload = %s, want it to mention the table", b)
	}
}

func TestComputeIsQuietWhenInSync(t *testing.T) {
	ms := []migrate.Migration{{ID: "0001_init",
		SQL: "CREATE TABLE dump_notes (id INTEGER PRIMARY KEY, title TEXT);"}}
	p, err := Compute(ms, []any{&dumpNote{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Changes) != 0 {
		t.Fatalf("Changes = %+v, want none", p.Changes)
	}
	if !strings.Contains(p.Schema, "dump_notes") {
		t.Fatalf("Schema = %q, want the replayed DDL", p.Schema)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./migrate/dump/ -v`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Write minimal implementation**

```go
// Package dump is the bridge between the rastrillo binary and an
// app's model structs.
//
// rastrillo cannot import an app's models: it is a separate binary,
// and parsing models.go to reimplement GORM's struct-tag-to-DDL
// mapping would duplicate GORM badly and drift from it. So
// `rastrillo migration generate` writes a tiny program into the app
// module that imports the app package and this one, runs it with
// `go run`, and reads the JSON this package prints.
package dump

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"amadan.net/rastrillo/rastrillo/migrate"
)

// Payload is what the loader program prints and the CLI parses.
type Payload struct {
	Changes []migrate.Change `json:"changes"`
	Schema  string           `json:"schema"`
	Diff    []string         `json:"diff"`
}

func Compute(ms []migrate.Migration, models []any) (Payload, error) {
	ctx := context.Background()
	var p Payload
	var err error
	if p.Changes, err = migrate.Generate(ctx, ms, models); err != nil {
		return p, err
	}
	if p.Schema, err = migrate.SchemaSQL(ctx, ms); err != nil {
		return p, err
	}
	for _, c := range p.Changes {
		p.Diff = append(p.Diff, c.SQL)
	}
	return p, nil
}

// Main is the whole body of the generated loader program.
func Main(ms []migrate.Migration, models []any) {
	p, err := Compute(ms, models)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(p); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./migrate/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add migrate/dump/
git commit -m "migrate/dump: the bridge to an app's model structs

rastrillo is a separate binary and cannot import an app's models,
and parsing models.go to reimplement GORM's tag-to-DDL mapping would
duplicate GORM and drift from it. The CLI instead generates a
four-line program inside the app module that calls dump.Main."
```

---

## Task 7: Convert sessions, blobs, eventlog, passkey

**Files:**
- Create: `sessions/migrations/0001_init.sql`, `blobs/migrations/0001_init.sql`, `eventlog/migrations/0001_init.sql`, `passkey/migrations/0001_init.sql`
- Modify: `sessions/sessions.go:72-81`, `blobs/blobs.go:70-79`, `eventlog/eventlog.go:41-56`, `passkey/passkey.go` (its `var Migrations` block)
- Test: `migrate/adoption_packages_test.go` (one table-driven test covering all four)

**Interfaces:**
- Consumes: `MustFromFS`, `Set` (Task 1); `Apply` (Task 3).
- Produces: `sessions.Schema`, `blobs.Schema`, `eventlog.Schema`, `passkey.Schema`, each a `*migrate.Set`. `Migrations []string` is removed from all four.

**The constraint (spec §7).** Each `0001_init.sql` must produce structure equivalent to today's `Migrations []string`, or every deployed app hits the refuse-to-boot branch. Move the SQL verbatim — do not tidy it.

- [ ] **Step 1: Write the failing test**

```go
package migrate_test

import (
	"context"
	"path/filepath"
	"testing"

	"amadan.net/rastrillo/rastrillo/blobs"
	"amadan.net/rastrillo/rastrillo/db"
	"amadan.net/rastrillo/rastrillo/eventlog"
	"amadan.net/rastrillo/rastrillo/migrate"
	"amadan.net/rastrillo/rastrillo/passkey"
	"amadan.net/rastrillo/rastrillo/sessions"
)

// legacySQL is each package's schema exactly as it shipped before the
// migrate conversion. A deployed database was built from these
// statements, so adopting one must produce zero DDL.
var legacySQL = map[string][]string{
	"sessions": {`CREATE TABLE IF NOT EXISTS sessions (
	  token_hash TEXT PRIMARY KEY,
	  subject    TEXT NOT NULL,
	  method     TEXT NOT NULL DEFAULT '',
	  auth_time  TEXT NOT NULL DEFAULT '',
	  created_at TEXT NOT NULL,
	  expires_at TEXT NOT NULL
	);`},
	"blobs": {`CREATE TABLE IF NOT EXISTS blobs (
	  hash         TEXT    PRIMARY KEY,
	  content_type TEXT    NOT NULL,
	  size         INTEGER NOT NULL,
	  data         BLOB    NOT NULL
	);`},
	"eventlog": {`CREATE TABLE IF NOT EXISTS eventlog (
	  stream  TEXT    NOT NULL,
	  writer  TEXT    NOT NULL,
	  seq     INTEGER NOT NULL,
	  lamport INTEGER NOT NULL,
	  ts      TEXT    NOT NULL,
	  actor   TEXT    NOT NULL,
	  kind    TEXT    NOT NULL,
	  payload TEXT    NOT NULL,
	  PRIMARY KEY (stream, writer, seq)
	);`,
		`CREATE INDEX IF NOT EXISTS eventlog_merge_order
	  ON eventlog (stream, lamport, ts, writer, seq);`},
}

func TestPackagesAdoptLegacyDatabases(t *testing.T) {
	sets := map[string]*migrate.Set{
		"sessions": sessions.Schema,
		"blobs":    blobs.Schema,
		"eventlog": eventlog.Schema,
	}
	for name, stmts := range legacySQL {
		t.Run(name, func(t *testing.T) {
			d, err := db.Open(filepath.Join(t.TempDir(), name+".db"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			for _, s := range stmts {
				if err := d.G.Exec(s).Error; err != nil {
					t.Fatal(err)
				}
			}
			r, err := migrate.Apply(context.Background(), d, sets[name])
			if err != nil {
				t.Fatalf("a database built from the shipped Migrations must adopt cleanly: %v", err)
			}
			if !r.Adopted || len(r.Applied) != 0 {
				t.Fatalf("Result = %+v, want adopted with zero DDL applied", r)
			}
		})
	}
}

func TestPackagesApplyToEmptyDatabase(t *testing.T) {
	for name, s := range map[string]*migrate.Set{
		"sessions": sessions.Schema,
		"blobs":    blobs.Schema,
		"eventlog": eventlog.Schema,
		"passkey":  passkey.Schema,
	} {
		t.Run(name, func(t *testing.T) {
			d, err := db.Open(filepath.Join(t.TempDir(), name+".db"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			if _, err := migrate.Apply(context.Background(), d, s); err != nil {
				t.Fatal(err)
			}
			// Idempotent second boot.
			if _, err := migrate.Apply(context.Background(), d, s); err != nil {
				t.Fatalf("second boot: %v", err)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./migrate/ -run TestPackages -v`
Expected: FAIL — `sessions.Schema` undefined.

- [ ] **Step 3: Write minimal implementation**

For each package, move the SQL verbatim into `<pkg>/migrations/0001_init.sql`. For `eventlog` and `passkey`, both statements go in the one file, separated by blank lines — they shipped as one unit and must adopt as one.

Then in each package, replace the `var Migrations` block. `sessions/sessions.go`:

```go
//go:embed migrations/*.sql
var migrationFS embed.FS

// Schema is the package's migration set, applied with migrate.Apply
// alongside the app's own. It replaces the exported Migrations
// []string: the ledger records what ran, so these statements are no
// longer re-executed on every boot.
var Schema = migrate.MustFromFS(migrationFS, "sessions")
```

Add `"embed"` and `"amadan.net/rastrillo/rastrillo/migrate"` to each package's imports. Repeat for `blobs`, `eventlog`, `passkey`, changing only the namespace string.

Then fix the packages' own tests: `sessions/sessions_test.go`, `blobs/blobs_test.go`, `eventlog/eventlog_test.go`, `passkey/passkey_test.go` currently call `rastrillo.OpenDB(path, Migrations)`. Replace with:

```go
d, err := db.Open(filepath.Join(t.TempDir(), "x.db"), nil)
if err != nil {
	t.Fatal(err)
}
t.Cleanup(func() { d.Close() })
if _, err := migrate.Apply(context.Background(), d, Schema); err != nil {
	t.Fatal(err)
}
```

Where a test then needs a `*sql.DB` (sessions and blobs take one in their `Config`), use `d.Writer()`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./... 2>&1 | tail -30`
Expected: PASS for `migrate`, `sessions`, `blobs`, `eventlog`, `passkey`. `auth` still fails — it is Task 8.

- [ ] **Step 5: Commit**

```bash
git add sessions/ blobs/ eventlog/ passkey/ migrate/adoption_packages_test.go
git commit -m "sessions, blobs, eventlog, passkey: Migrations -> Schema

The SQL moves verbatim into migrations/0001_init.sql. It has to:
a deployed database was built from these exact statements, and
adoption compares structure — tidying the DDL here would send every
existing app down the refuse-to-boot branch.

Each package's test asserts exactly that: build a database the old
way, apply the new Set, expect adoption with zero DDL."
```

---

## Task 8: Convert auth

**Files:**
- Create: `auth/migrations/0001_init.sql`, `auth/migrations/0002_sessions_backfill.sql`
- Modify: `auth/store.go:45-64`, `auth/auth.go:75-76` (doc comment), `auth/auth_test.go`
- Test: `auth/auth_test.go` (adapt the existing legacy-upgrade test)

**Interfaces:**
- Consumes: `MustFromFS`, `Merge` (Task 1); `sessions.Schema` (Task 7).
- Produces: `auth.Schema` — a `*migrate.Set` containing only auth's own migrations. `auth.Migrations` is removed.

**Why this one is different.** Today `auth.Migrations` *embeds* `sessions.Migrations` via `append`, and ends with a data migration that copies `auth_sessions` rows into `sessions` and then empties `auth_sessions`. Two consequences:

1. `auth.Schema` must **not** embed `sessions.Schema`. The ordering requirement — auth's backfill runs after the `sessions` table exists — is now expressed by the caller writing `migrate.Merge(sessions.Schema, auth.Schema)`. That is the requirement `auth/store.go`'s comment states in prose today.
2. The backfill currently re-runs on every boot and is safe only because `DELETE FROM auth_sessions` leaves nothing to copy — `auth/auth_test.go:489` guards a revoked session being resurrected by a re-run. Under the ledger it runs exactly once, which is strictly stronger. Keep the test; it should now pass trivially.

- [ ] **Step 1: Write the failing test**

Replace `auth/auth_test.go`'s `legacyMigrations` upgrade test body with:

```go
// TestUpgradeFromLegacyAuthSessions is the pre-sessions-core upgrade:
// a database with rows in auth_sessions and no sessions table gets the
// backfill exactly once, and a revoked session is not resurrected by a
// later boot.
func TestUpgradeFromLegacyAuthSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	d, err := db.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, stmt := range legacyMigrations {
		if err := d.G.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := d.G.Exec(`INSERT INTO auth_sessions
	  (token_hash, address, method, auth_time, created_at, expires_at)
	  VALUES ('h1','a@example.com','link','','now','2099-01-01T00:00:00Z')`).Error; err != nil {
		t.Fatal(err)
	}

	full := migrate.Merge(sessions.Schema, Schema)
	if _, err := migrate.Apply(context.Background(), d, full); err != nil {
		t.Fatal(err)
	}
	var n int64
	d.G.Raw("SELECT count(*) FROM sessions WHERE token_hash = 'h1'").Scan(&n)
	if n != 1 {
		t.Fatalf("backfilled sessions rows = %d, want 1", n)
	}

	// Revoke, then boot again. The backfill must not resurrect it.
	if err := d.G.Exec("DELETE FROM sessions WHERE token_hash = 'h1'").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.Apply(context.Background(), d, full); err != nil {
		t.Fatal(err)
	}
	d.G.Raw("SELECT count(*) FROM sessions WHERE token_hash = 'h1'").Scan(&n)
	if n != 0 {
		t.Fatal("a second boot resurrected a revoked session — the backfill re-ran")
	}
}

func TestAuthSchemaDoesNotEmbedSessions(t *testing.T) {
	for _, m := range Schema.All() {
		if strings.Contains(m.SQL, "CREATE TABLE IF NOT EXISTS sessions") {
			t.Fatalf("%s embeds the sessions table; callers must Merge(sessions.Schema, auth.Schema)", m.ID)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./auth/ -run 'TestUpgrade|TestAuthSchema' -v`
Expected: FAIL — `auth.Schema` undefined.

- [ ] **Step 3: Write minimal implementation**

`auth/migrations/0001_init.sql` — auth's own two tables, verbatim from today's first two entries:

```sql
CREATE TABLE IF NOT EXISTS auth_links (
  hash       TEXT PRIMARY KEY,
  address    TEXT NOT NULL,
  purpose    TEXT NOT NULL,
  expires_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS auth_sessions (
  token_hash TEXT PRIMARY KEY,
  address    TEXT NOT NULL,
  method     TEXT NOT NULL,
  auth_time  TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL
);
```

`auth/migrations/0002_sessions_backfill.sql`:

```sql
-- Move any pre-sessions-core rows into the sessions core, then empty
-- the old table. Under the ledger this runs exactly once, where the
-- old Migrations []string re-ran it on every boot and relied on the
-- DELETE leaving nothing to copy.
INSERT OR IGNORE INTO sessions (token_hash, subject, method, auth_time, created_at, expires_at)
  SELECT token_hash, address, method, auth_time, created_at, expires_at FROM auth_sessions;

DELETE FROM auth_sessions;
```

`auth/store.go` — replace the whole `var Migrations = append(append(...))` block:

```go
//go:embed migrations/*.sql
var migrationFS embed.FS

// Schema is auth's own migrations, and only its own. The backfill in
// 0002 reads the sessions table, so a caller must order the sets:
//
//	migrate.Apply(ctx, d, migrate.Merge(sessions.Schema, auth.Schema))
//
// Merge's argument order is apply order, which is how that
// requirement is now stated at the call site — it used to be a
// comment here and an append() that embedded sessions.Migrations into
// this package's own list.
var Schema = migrate.MustFromFS(migrationFS, "auth")
```

Update `auth/auth.go:75-76`'s doc comment from "auth.Migrations must be in the app's Options.Migrations" to name `Merge(sessions.Schema, auth.Schema)`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./... 2>&1 | tail -20`
Expected: PASS everywhere except the examples, which are Task 12.

- [ ] **Step 5: Commit**

```bash
git add auth/
git commit -m "auth: Migrations -> Schema, without embedding sessions

auth.Migrations append()ed sessions.Migrations into its own list so
the backfill would land after the sessions table existed. That
requirement now lives at the call site, where Merge's argument order
is apply order.

The backfill also stops re-running: it ran on every boot before, and
was safe only because its own DELETE left nothing to copy. Under the
ledger it runs once. The revoked-session-resurrection test still
guards it."
```

---

## Task 9: `migration check` and `migration generate`

**Files:**
- Create: `cmd/rastrillo/migration.go`
- Modify: `cmd/rastrillo/main.go:19-30` (dispatch), `cmd/rastrillo/main.go:36-44` (usage)
- Test: `cmd/rastrillo/migration_test.go`

**Interfaces:**
- Consumes: `dump.Payload` (Task 6).
- Produces: `func runMigration(args []string) error`; internally `func appPackage(dir string) (importPath, pkgName string, err error)` and `func loadPayload(dir string) (dump.Payload, error)`.

**The loader.** `generate` and `check` write `<dir>/rastrillo_migration_dump/main.go`, run `go run ./rastrillo_migration_dump`, parse JSON from stdout, and remove the directory. The directory name has no leading dot: the go tool ignores directories beginning with `.` or `_`, so `go run` could not find it.

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationCheckPassesOnScaffold(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a scaffolded app")
	}
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := runNew([]string{"checkapp"}); err != nil {
		t.Fatal(err)
	}
	if err := runMigration([]string{"check", "checkapp"}); err != nil {
		t.Fatalf("a fresh scaffold must be in sync: %v", err)
	}
}

func TestMigrationCheckFailsWhenModelGainsAField(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a scaffolded app")
	}
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := runNew([]string{"driftapp"}); err != nil {
		t.Fatal(err)
	}
	models := filepath.Join("driftapp", "internal", "driftapp", "models.go")
	b, err := os.ReadFile(models)
	if err != nil {
		t.Fatal(err)
	}
	// Add a field the migrations do not have.
	out := strings.Replace(string(b), "type Note struct {", "type Note struct {\n\tArchived bool", 1)
	if out == string(b) {
		t.Skip("scaffold shape changed; update this test")
	}
	if err := os.WriteFile(models, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	err = runMigration([]string{"check", "driftapp"})
	if err == nil {
		t.Fatal("want check to fail when a model has a field the migrations lack")
	}
	if !strings.Contains(err.Error(), "generate") {
		t.Fatalf("error = %v, want it to name the fix", err)
	}
}

func TestMigrationGenerateWritesFileAndThenChecksClean(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a scaffolded app")
	}
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := runNew([]string{"genapp"}); err != nil {
		t.Fatal(err)
	}
	models := filepath.Join("genapp", "internal", "genapp", "models.go")
	b, _ := os.ReadFile(models)
	out := strings.Replace(string(b), "type Note struct {", "type Note struct {\n\tArchived bool", 1)
	os.WriteFile(models, []byte(out), 0o644)

	if err := runMigration([]string{"generate", "genapp"}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join("genapp", "internal", "genapp", "migrations"))
	var found string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "0002_") {
			found = e.Name()
		}
	}
	if found == "" {
		t.Fatalf("no 0002_*.sql written; got %v", entries)
	}
	if err := runMigration([]string{"check", "genapp"}); err != nil {
		t.Fatalf("check must be clean after generate: %v", err)
	}
	// The temporary loader must not survive.
	if _, err := os.Stat(filepath.Join("genapp", "rastrillo_migration_dump")); !os.IsNotExist(err) {
		t.Fatal("the generated loader directory was left behind")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/rastrillo/ -run TestMigration -v`
Expected: FAIL — `undefined: runMigration`.

- [ ] **Step 3: Write minimal implementation**

`cmd/rastrillo/migration.go`:

```go
// rastrillo migration — the schema-change tooling.
//
// The group is a noun on purpose. `rastrillo migrate`, typed bare,
// reads as "apply my migrations now", and this CLI deliberately never
// applies: migrations run at boot, because a hibernating route has no
// operator moment between a new binary landing and the activator
// exec'ing it. The one exception is `baseline`, which is manual by
// design.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"amadan.net/rastrillo/rastrillo/migrate/dump"
)

func runMigration(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: rastrillo migration <generate|new|status|check|baseline> [dir]")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "generate":
		return migrationGenerate(rest)
	case "check":
		return migrationCheck(rest)
	case "new":
		return migrationNew(rest)
	case "status":
		return migrationStatus(rest)
	case "baseline":
		return migrationBaseline(rest)
	default:
		return fmt.Errorf("unknown migration subcommand %q", sub)
	}
}

// appPackage locates the app's package: the single directory under
// internal/ that is not the <name>test harness. rastrillo new
// scaffolds exactly one, and dev.go already assumes the same
// single-app shape for cmd/.
func appPackage(dir string) (importPath, pkgName string, err error) {
	mod, err := modulePath(dir)
	if err != nil {
		return "", "", err
	}
	entries, err := os.ReadDir(filepath.Join(dir, "internal"))
	if err != nil {
		return "", "", fmt.Errorf("no internal/ directory in %s: %w", dir, err)
	}
	var found []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasSuffix(e.Name(), "test") {
			found = append(found, e.Name())
		}
	}
	sort.Strings(found)
	if len(found) != 1 {
		return "", "", fmt.Errorf("expected exactly one app package under %s/internal, found %v", dir, found)
	}
	return mod + "/internal/" + found[0], found[0], nil
}

const loaderDir = "rastrillo_migration_dump"

const loaderTemplate = `//go:build ignore_me_not

// Code generated by rastrillo migration; removed immediately after use.
package main

import (
	app %q

	"amadan.net/rastrillo/rastrillo/migrate/dump"
)

func main() { dump.Main(app.Schema.All(), app.Models) }
`

// loadPayload compiles and runs a throwaway program inside the app
// module. rastrillo cannot import the app's models directly — it is a
// separate binary — and parsing models.go to reimplement GORM's
// tag-to-DDL mapping would duplicate GORM and drift from it.
//
// The directory name has no leading dot: the go tool skips
// directories beginning with "." or "_", so `go run` could not find
// it.
func loadPayload(dir string) (dump.Payload, error) {
	var p dump.Payload
	importPath, _, err := appPackage(dir)
	if err != nil {
		return p, err
	}
	ldir := filepath.Join(dir, loaderDir)
	if err := os.MkdirAll(ldir, 0o755); err != nil {
		return p, err
	}
	defer os.RemoveAll(ldir)

	body := fmt.Sprintf(loaderTemplate, importPath)
	// Strip the build tag line: it exists only to keep an
	// accidentally-surviving file out of a normal build.
	body = strings.Replace(body, "//go:build ignore_me_not\n\n", "", 1)
	if err := os.WriteFile(filepath.Join(ldir, "main.go"), []byte(body), 0o644); err != nil {
		return p, err
	}

	cmd := exec.Command("go", "run", "./"+loaderDir)
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return p, fmt.Errorf("loading the app's models failed — the app must compile:\n%s", stderr.String())
	}
	if err := json.Unmarshal(out, &p); err != nil {
		return p, fmt.Errorf("could not parse the loader's output: %w", err)
	}
	return p, nil
}

func migrationsDir(dir string) (string, error) {
	_, pkg, err := appPackage(dir)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "internal", pkg, "migrations"), nil
}

func dirArg(args []string) string {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return "."
}

func migrationCheck(args []string) error {
	dir := dirArg(args)
	p, err := loadPayload(dir)
	if err != nil {
		return err
	}
	if len(p.Changes) == 0 {
		fmt.Println("rastrillo migration check: models and migrations agree")
		return nil
	}
	var b strings.Builder
	b.WriteString("models and migrations disagree:\n")
	for _, c := range p.Changes {
		b.WriteString("  " + strings.TrimSpace(c.SQL) + "\n")
	}
	b.WriteString("\nrun: rastrillo migration generate")
	return errors.New(b.String())
}

func migrationGenerate(args []string) error {
	dir := dirArg(args)
	allowDestructive := false
	for _, a := range args {
		if a == "--allow-destructive" {
			allowDestructive = true
		}
	}
	p, err := loadPayload(dir)
	if err != nil {
		return err
	}
	if len(p.Changes) == 0 {
		fmt.Println("rastrillo migration generate: nothing to do")
		return nil
	}

	var destructive []string
	for _, c := range p.Changes {
		if c.Destructive {
			destructive = append(destructive, strings.TrimSpace(c.SQL))
		}
	}
	if len(destructive) > 0 && !allowDestructive {
		return fmt.Errorf(
			"this change drops data:\n  %s\n\n"+
				"Re-run with --allow-destructive if that is what you want.\n"+
				"If you meant to rename rather than drop, write it by hand instead:\n"+
				"  rastrillo migration new rename_something\n"+
				"  ALTER TABLE t RENAME COLUMN old TO new;",
			strings.Join(destructive, "\n  "))
	}

	mdir, err := migrationsDir(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(mdir, 0o755); err != nil {
		return err
	}
	name, err := nextMigrationName(mdir, describe(p.Changes))
	if err != nil {
		return err
	}
	var body strings.Builder
	for _, c := range p.Changes {
		body.WriteString(strings.TrimSpace(c.SQL))
		body.WriteString("\n\n")
	}
	if err := os.WriteFile(filepath.Join(mdir, name), []byte(body.String()), 0o644); err != nil {
		return err
	}
	fmt.Printf("rastrillo migration generate: wrote %s\n", filepath.Join(mdir, name))

	// schema.sql must reflect the migration just written, so recompute
	// it with the new file in place.
	p2, err := loadPayload(dir)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(mdir, "schema.sql"), []byte(p2.Schema), 0o644); err != nil {
		return err
	}
	fmt.Printf("rastrillo migration generate: refreshed %s\n", filepath.Join(mdir, "schema.sql"))
	return nil
}

// describe names a migration after what it does, so the filename is
// readable in a diff without opening it.
func describe(changes []dump.Change) string {
	for _, c := range changes {
		u := strings.ToUpper(c.SQL)
		switch {
		case strings.HasPrefix(u, "CREATE TABLE"):
			return "create_" + tableIn(c.SQL)
		case strings.Contains(u, "ADD COLUMN"):
			return "alter_" + tableIn(c.SQL)
		case strings.Contains(u, "DROP COLUMN"):
			return "drop_column_" + tableIn(c.SQL)
		}
	}
	return "schema"
}

func tableIn(sqlText string) string {
	fields := strings.Fields(strings.ReplaceAll(sqlText, "`", " "))
	for i, f := range fields {
		if strings.EqualFold(f, "TABLE") && i+1 < len(fields) {
			return strings.Trim(strings.ToLower(fields[i+1]), `"'();`)
		}
	}
	return "schema"
}

func nextMigrationName(mdir, label string) (string, error) {
	entries, err := os.ReadDir(mdir)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	n := 0
	for _, e := range entries {
		if len(e.Name()) < 4 {
			continue
		}
		var got int
		if _, err := fmt.Sscanf(e.Name()[:4], "%04d", &got); err == nil && got > n {
			n = got
		}
	}
	return fmt.Sprintf("%04d_%s.sql", n+1, label), nil
}
```

Note: `dump.Change` in `describe` — `Payload.Changes` is `[]migrate.Change`, so import `migrate` and use `[]migrate.Change`. Adjust the signature accordingly.

`cmd/rastrillo/main.go` — add to the switch:

```go
	case "migration":
		err = runMigration(os.Args[2:])
```

and to `usage()`:

```go
  rastrillo migration <cmd> [dir]                schema changes (generate, new, status, check, baseline)
       generate [--allow-destructive]            diff models against migrations, write the delta
       check                                     CI gate: models and migrations agree (no database)
```

`modulePath` should already exist in `cmd/rastrillo/modpath.go`; if the existing helper has a different name or signature, use it rather than adding a second one.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/rastrillo/ -run TestMigration -v`
Expected: PASS. These tests compile a scaffolded app, so they are slow — that is why they check `testing.Short()`.

- [ ] **Step 5: Commit**

```bash
git add cmd/rastrillo/migration.go cmd/rastrillo/main.go cmd/rastrillo/migration_test.go
git commit -m "rastrillo migration: check and generate, without a database

Both sides of the diff are computed in memory, so CI needs no
database, no fixtures and no network. The models side comes from a
throwaway program compiled inside the app module — rastrillo is a
separate binary and cannot import an app's structs.

generate refuses to write a destructive change without
--allow-destructive, and says what to do instead when the user meant
a rename."
```

---

## Task 10: `migration new`, `status`, and `baseline`

**Files:**
- Modify: `cmd/rastrillo/migration.go`
- Test: `cmd/rastrillo/migration_test.go`

**Interfaces:**
- Consumes: `migrationsDir`, `nextMigrationName`, `loadPayload` (Task 9); `migrate.Apply`, `migrate.Stamp`, `migrate.Read` (Tasks 2–4).
- Produces: `func migrationNew(args []string) error`, `func migrationStatus(args []string) error`, `func migrationBaseline(args []string) error`.

- [ ] **Step 1: Write the failing test**

```go
func TestMigrationNewWritesNumberedStub(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(cwd) })
	os.Chdir(dir)
	if err := runNew([]string{"stubapp"}); err != nil {
		t.Fatal(err)
	}
	if err := runMigration([]string{"new", "rename_title", "stubapp"}); err != nil {
		t.Fatal(err)
	}
	mdir := filepath.Join("stubapp", "internal", "stubapp", "migrations")
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
		t.Fatalf("name = %q, want it numbered after the scaffold's 0001", found)
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/rastrillo/ -run TestMigrationNew -v`
Expected: FAIL — `undefined: migrationNew`.

- [ ] **Step 3: Write minimal implementation**

```go
var migrationNameOK = regexp.MustCompile(`^[a-z0-9_]+$`)

func migrationNew(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: rastrillo migration new <name> [dir]")
	}
	name := args[0]
	if !migrationNameOK.MatchString(name) {
		return fmt.Errorf("migration name %q must be lowercase letters, digits and underscores", name)
	}
	dir := dirArg(args[1:])
	mdir, err := migrationsDir(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(mdir, 0o755); err != nil {
		return err
	}
	file, err := nextMigrationName(mdir, name)
	if err != nil {
		return err
	}
	stub := `-- ` + name + `
--
-- Write the SQL this migration should run. It applies once, at boot,
-- inside its own transaction, and is never re-run or reversed.
--
-- Renames belong here rather than in a generated migration, because a
-- rename is indistinguishable from a drop plus an add:
--   ALTER TABLE notes RENAME COLUMN title TO heading;
`
	path := filepath.Join(mdir, file)
	if err := os.WriteFile(path, []byte(stub), 0o644); err != nil {
		return err
	}
	fmt.Printf("rastrillo migration new: wrote %s\n", path)
	return nil
}

func migrationStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	dbPath := fs.String("db", "", "path to the app's SQLite database")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dbPath == "" {
		return errors.New("rastrillo migration status needs --db <path>: it reports what a real database has applied")
	}
	dir := dirArg(fs.Args())
	p, err := loadPayload(dir)
	if err != nil {
		return err
	}

	d, err := db.Open(*dbPath, nil)
	if err != nil {
		return err
	}
	defer d.Close()
	ctx := context.Background()
	conn, err := d.Writer().Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	rows, err := conn.QueryContext(ctx,
		"SELECT id, applied_at FROM rastrillo_migrations ORDER BY id")
	if err != nil {
		fmt.Println("no migration ledger in this database — it has never been booted with this version")
		return nil
	}
	defer rows.Close()
	fmt.Println("applied:")
	for rows.Next() {
		var id, at string
		if err := rows.Scan(&id, &at); err != nil {
			return err
		}
		fmt.Printf("  %-40s %s\n", id, at)
	}
	if len(p.Changes) > 0 {
		fmt.Println("\npending (models and migrations disagree — run rastrillo migration generate):")
		for _, c := range p.Changes {
			fmt.Printf("  %s\n", strings.TrimSpace(c.SQL))
		}
	}
	return rows.Err()
}

func migrationBaseline(args []string) error {
	fs := flag.NewFlagSet("baseline", flag.ContinueOnError)
	dbPath := fs.String("db", "", "path to the app's SQLite database")
	through := fs.String("through", "", "stop stamping after this migration ID")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dbPath == "" {
		return errors.New("rastrillo migration baseline needs --db <path>")
	}
	return fmt.Errorf(
		"baseline stamps a database as already migrated without running anything, which is only " +
			"safe after you have read the difference Apply printed.\n" +
			"It is wired in Task 11 of the plan; until then, read the boot error and fix the schema by hand")
}
```

**Then finish `migrationBaseline` properly** — the stub above exists only to keep the step honest about ordering. Replace its body with:

```go
	d, err := db.Open(*dbPath, nil)
	if err != nil {
		return err
	}
	defer d.Close()
	ctx := context.Background()
	conn, err := d.Writer().Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, migrate.LedgerDDL); err != nil {
		return err
	}
	set, err := appSet(dir)
	if err != nil {
		return err
	}
	if err := migrate.Stamp(ctx, conn, set, *through); err != nil {
		return err
	}
	fmt.Printf("rastrillo migration baseline: stamped %s\n", *dbPath)
	return nil
```

This needs two exports added to `migrate`: rename the `ledgerDDL` constant in `migrate/apply.go` to exported `LedgerDDL`, and add to `cmd/rastrillo/migration.go` an `appSet(dir string) ([]migrate.Migration, error)` that reads the migration files off disk directly (no `go run` needed — baseline does not need models):

```go
// appSet reads the app's migration files straight off disk. baseline
// does not need the models, so it skips the go run loader entirely.
func appSet(dir string) ([]migrate.Migration, error) {
	mdir, err := migrationsDir(dir)
	if err != nil {
		return nil, err
	}
	_, pkg, err := appPackage(dir)
	if err != nil {
		return nil, err
	}
	s, err := migrate.FromFS(os.DirFS(filepath.Dir(mdir)), pkg)
	if err != nil {
		return nil, err
	}
	return s.All(), nil
}
```

Add imports: `context`, `flag`, `regexp`, `amadan.net/rastrillo/rastrillo/db`, `amadan.net/rastrillo/rastrillo/migrate`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/rastrillo/ -v && go vet ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/rastrillo/migration.go cmd/rastrillo/migration_test.go migrate/apply.go
git commit -m "rastrillo migration: new, status, baseline

new writes a numbered stub whose comment points at the case that
brings people here — a rename, which cannot be generated because it
is indistinguishable from a drop plus an add.

baseline is the one command that writes to a real database, for the
one case Apply refuses to handle on its own: a deployed schema that
does not match the migration set."
```

---

## Task 11: `dev` drift warning

**Files:**
- Modify: `cmd/rastrillo/dev.go:23` (`watchDirs`), and the loop body
- Test: `cmd/rastrillo/dev_test.go`

**Interfaces:**
- Consumes: `loadPayload` (Task 9).
- Produces: no new exported API.

**Note on `watchDirs`.** It is currently `{"actions", "app", "manifest", "cmd", "locales", "templates", "static"}` — no `internal`. Since the middle-layer pivot, an app's models, handlers and now migrations all live under `internal/<pkg>/`, so `dev` does not currently rebuild when they change. Adding `"internal"` is required for the drift warning to fire at all, and incidentally fixes the rebuild gap. Call that out in the commit message; it is a real behaviour change beyond the migrations spec.

- [ ] **Step 1: Write the failing test**

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/rastrillo/ -run 'TestWatchDirs|TestDrift' -v`
Expected: FAIL — `internal` missing; `undefined: driftMessage`.

- [ ] **Step 3: Write minimal implementation**

In `cmd/rastrillo/dev.go`, extend the list and its comment:

```go
// watchDirs are the trees whose edits trigger the §11 loop: the design
// doc's app/, actions/, manifest/, plus cmd/ — rastrillo new scaffolds
// cmd/<name>/main.go, and a dev loop that ignores edits to it surprises
// people — plus locales/, templates/, and static/, which the app embeds into its
// binary (§9, §10, §8): without a rebuild, a saved catalog, template,
// or static asset keeps serving the copy compiled in at the last build.
// internal/ carries the middle layer's own shape — models.go,
// handlers.go, render.go and migrations/ — so edits there must rebuild
// too. gen/ is deliberately absent: it is the generator's output.
var watchDirs = []string{"actions", "app", "manifest", "cmd", "internal", "locales", "templates", "static"}
```

Add the drift check. After a successful rebuild in the loop body, run it and print — never generate:

```go
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

// warnOnDrift is best-effort: a compile error already surfaces through
// the rebuild, and a drift check that failed the loop would make the
// dev experience worse than the problem it reports.
func warnOnDrift(dir string) {
	p, err := loadPayload(dir)
	if err != nil {
		return
	}
	if len(p.Changes) == 0 {
		return
	}
	sqls := make([]string, 0, len(p.Changes))
	for _, c := range p.Changes {
		sqls = append(sqls, c.SQL)
	}
	fmt.Fprint(os.Stderr, driftMessage(sqls))
}
```

Call `warnOnDrift(dir)` after each successful rebuild.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/rastrillo/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/rastrillo/dev.go cmd/rastrillo/dev_test.go
git commit -m "dev: watch internal/, and warn when models outrun migrations

watchDirs predates the middle-layer pivot and never listed internal/,
so dev has not been rebuilding on edits to models.go, handlers.go or
render.go. Adding it is required for the drift warning to fire and
fixes that gap on the way past.

dev warns and never generates: writing a migration is a decision, not
a side effect of hitting save."
```

---

## Task 12: Scaffold

**Files:**
- Modify: `cmd/rastrillo/new.go` — `files` map (~line 60), `appTemplate` (~line 220), `modelsTemplate` (~line 261), `makefileTemplate` (~line 541), `claudeMDTemplate` (~line 576)
- Create: templates for `migrations.go` and `migrations/0001_init.sql`
- Test: `cmd/rastrillo/new_scaffold_test.go`

**Interfaces:**
- Consumes: `migrate.Apply`, `migrate.MustFromFS` (Tasks 1, 3).
- Produces: a scaffold whose `App()` calls `migrate.Apply` and whose `models.go` declares `var Models []any`.

- [ ] **Step 1: Write the failing test**

```go
func TestScaffoldMigratesAndPassesCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and runs a scaffolded app")
	}
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(cwd) })
	os.Chdir(dir)
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
	b, _ := os.ReadFile(filepath.Join("freshapp", "internal", "freshapp", "app.go"))
	if strings.Contains(string(b), "AutoMigrate") {
		t.Fatal("app.go still calls AutoMigrate; boot must go through migrate.Apply")
	}
	if !strings.Contains(string(b), "migrate.Apply") {
		t.Fatal("app.go must call migrate.Apply")
	}
	m, _ := os.ReadFile(filepath.Join("freshapp", "internal", "freshapp", "models.go"))
	if !strings.Contains(string(m), "var Models") {
		t.Fatal("models.go must declare Models for the generator to read")
	}
	if err := runMigration([]string{"check", "freshapp"}); err != nil {
		t.Fatalf("a fresh scaffold must pass check: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/rastrillo/ -run TestScaffoldMigrates -v`
Expected: FAIL — files missing, `app.go` still calls `AutoMigrate`.

- [ ] **Step 3: Write minimal implementation**

`appTemplate` — replace the `AutoMigrate` block:

```go
	// Apply the schema: this app's migrations, plus any framework
	// subsystem's. Merge's argument order is apply order, so a
	// subsystem whose migrations another one reads goes first.
	if _, err := migrate.Apply(context.Background(), d, Schema); err != nil {
		return nil, err
	}
```

and add `"context"` plus `"amadan.net/rastrillo/rastrillo/migrate"` to its imports.

`modelsTemplate` — add below the existing comment:

```go
// Models is every model the schema generator manages. Keep it in step
// with the structs above: `rastrillo migration generate` reads it to
// work out what the database should look like, and `rastrillo
// migration check` fails CI when it and the migrations disagree.
var Models = []any{
	&Note{},
}

type Note struct {
	ID        int64
	Title     string
	Body      string
	CreatedAt time.Time
	UpdatedAt time.Time
}
```

(The scaffold previously showed `Note` only in a comment. Making it real gives the generator something to work with and gives `0001_init.sql` a reason to exist.)

New `migrationsTemplate`:

```go
const migrationsTemplate = `package %[1]s

import (
	"embed"

	"amadan.net/rastrillo/rastrillo/migrate"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Schema is this app's migrations, in order. Add to it with
// ` + "`rastrillo migration generate`" + ` after changing a model, or
// ` + "`rastrillo migration new <name>`" + ` to write one by hand.
//
// Migrations apply at boot and are never re-run or reversed. Never
// edit one that has shipped — add a new one.
var Schema = migrate.MustFromFS(migrationFS, %[1]q)
`
```

Add both to the `files` map:

```go
		filepath.Join(appDir, "migrations.go"): fmt.Sprintf(migrationsTemplate, pkg),
```

The scaffold cannot hand-write `0001_init.sql` correctly for the `Note` model — that is exactly what the generator is for. So after writing the files, `runNew` calls the generator, the same way it already runs `generate` once so `go build` works immediately:

```go
	// Generate the initial migration from models.go, the same way new
	// already runs the routing generator once: a scaffold that does
	// not build, or that fails `migration check` on the first commit,
	// is a broken scaffold.
	if err := migrationGenerate([]string{name}); err != nil {
		return fmt.Errorf("scaffolding the initial migration: %w", err)
	}
```

`makefileTemplate` — add the check to the CI gate:

```make
ci: vet fmt-check test
	rastrillo migration check
```

Update the surrounding comment to mention it.

`claudeMDTemplate` — add a migrations line to the rules list:

```
- Schema changes: edit models.go, then run `rastrillo migration generate`
  and read the SQL it writes. Never edit a migration that has shipped.
  Renames are hand-written (`rastrillo migration new rename_x`) because
  a rename is indistinguishable from a drop plus an add.
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/rastrillo/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/rastrillo/new.go cmd/rastrillo/new_scaffold_test.go
git commit -m "new: scaffold migrations, and a real model to migrate

models.go now declares an actual Note struct and the Models slice the
generator reads, instead of showing one in a comment. new then runs
the generator once, the way it already runs the routing generator, so
a fresh scaffold builds and passes migration check on its first
commit.

make ci gains rastrillo migration check: the drift gate is only
useful if it is wired in before anyone has drifted."
```

---

## Task 13: Examples and SKILL.md

**Files:**
- Modify: `examples/notes/internal/notes/app.go`, `examples/blog/internal/blog/*`, `examples/tickets/internal/tickets*/*`, and each example's test harness
- Create: `examples/*/internal/*/migrations/` and `migrations.go`
- Modify: `SKILL.md` §2 and the checklist at its end; `README.md` where it describes `Options.Migrations`
- Test: `migrate/examples_test.go`

**Interfaces:**
- Consumes: everything.
- Produces: no new API.

- [ ] **Step 1: Write the failing test**

```go
package migrate_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestExamplesPassMigrationCheck is the framework catching its own
// drift: if an example's models and migrations disagree, the shape
// SKILL.md tells people to copy is already broken.
func TestExamplesPassMigrationCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles every example")
	}
	bin := filepath.Join(t.TempDir(), "rastrillo")
	build := exec.Command("go", "build", "-o", bin, "./cmd/rastrillo")
	build.Dir = repoRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the CLI: %v\n%s", err, out)
	}
	for _, ex := range []string{"notes", "blog", "tickets"} {
		t.Run(ex, func(t *testing.T) {
			cmd := exec.Command(bin, "migration", "check")
			cmd.Dir = filepath.Join(repoRoot(t), "examples", ex)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("examples/%s is out of sync:\n%s", ex, out)
			}
		})
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(trimNewline(out))
}

func trimNewline(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

var _ = os.Stat
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./migrate/ -run TestExamples -v`
Expected: FAIL — examples still call `AutoMigrate` and have no `migrations/`.

- [ ] **Step 3: Write minimal implementation**

For each example, in order (`notes` first — it is the one SKILL.md points at):

1. Add `var Models = []any{...}` to its models file, listing every struct it currently passes to `AutoMigrate`.
2. Add `migrations.go` with the `//go:embed` + `MustFromFS` pair, namespaced to the package name.
3. Run the CLI against it to produce `0001_init.sql` and `schema.sql`:
   ```bash
   go run ./cmd/rastrillo migration generate examples/notes
   ```
4. Replace the two-mechanism block in `App()`:
   ```go
   if _, err := migrate.Apply(context.Background(), d, migrate.Merge(sessions.Schema, Schema)); err != nil {
       return nil, err
   }
   ```
   The generated-store `Migrations` under `gen/store/` are emitted by `internal/generate/store.go`; update that emitter to produce a `*migrate.Set` in the same shape, and add the generated set to the `Merge` call.
5. Update the example's test harness to apply the same set.

Then `SKILL.md` §2. Replace the `AutoMigrate` + `sessions.Migrations` block with the single `migrate.Apply` call, and replace the additive-only paragraph:

> Schema changes go through `rastrillo migration generate`: edit a model, run it,
> and read the SQL it writes before committing. Migrations apply at boot, once
> each, recorded in a ledger — they are never re-run and never reversed.
>
> Never edit a migration that has shipped; add a new one. Renames are written by
> hand (`rastrillo migration new rename_x`), because a rename is
> indistinguishable from a drop plus an add and no tool can tell them apart.
> Destructive changes need `--allow-destructive`.
>
> A framework subsystem exports a `Schema *migrate.Set` rather than raw SQL.
> `migrate.Merge`'s argument order is apply order, so a subsystem another one
> reads goes first: `migrate.Merge(sessions.Schema, auth.Schema, Schema)`.

Update the numbered checklist item 5 from "`AutoMigrate` plus `sessions.Migrations` both run at boot" to "one `migrate.Apply` runs at boot, and `make ci` runs `rastrillo migration check`."

In `README.md`, leave the `Options.Migrations` description alone — that path is unchanged — but add a sentence pointing GORM-path apps at `migrate`.

- [ ] **Step 4: Run the full gate**

Run: `go vet ./... && gofmt -l . && go test ./...`
Expected: all PASS, `gofmt -l` silent.

- [ ] **Step 5: Commit**

```bash
git add examples/ SKILL.md README.md internal/generate/store.go migrate/examples_test.go
git commit -m "examples, SKILL.md: one migrate.Apply at boot

The two-mechanism block SKILL.md had to explain — AutoMigrate for
models, a d.G.Exec loop for raw subsystem SQL — becomes one call
whose argument order carries the ordering requirement.

The additive-only rule retires with it: generate can emit a rebuild
now, so the doc's job is the immutability rule and the two cases a
tool cannot infer, renames and drops.

TestExamplesPassMigrationCheck makes the examples CI's problem: the
shape people are told to copy cannot silently drift."
```

---

## Self-Review

**Spec coverage:**

| Spec section | Task |
|---|---|
| §3 no rollback / no Atlas | Enforced by omission; `Migration` has no `Down` (Task 1) |
| §4 boot-only application | Task 3 (`Apply`), Task 9 (no apply subcommand) |
| §5.1 types | Task 1 — with the documented `Apply` signature deviation |
| §5.2 ledger | Task 3 |
| §5.3 call site, subsystem conversion | Tasks 7, 8, 12, 13 |
| §5.4 diff engine | Tasks 2, 5 — with the documented recording-logger deviation |
| §5.5 model loader | Tasks 6, 9 |
| §6 CLI, on-disk layout, daily loop | Tasks 9, 10, 12 |
| §6.3 destructive consent, renames | Task 9 (`--allow-destructive`), Task 10 (`new` stub) |
| §6.4 dev drift warning | Task 11 |
| §7 adoption, `baseline` | Tasks 4, 10 |
| §8 transactions, foreign-key bracketing | Task 3 |
| §9 checksums, `check` vs boot split | Tasks 3, 9 |
| §10 tests (4 suites) | Task 3 (ledger), Tasks 7/8 (adoption), Task 13 (examples), Tasks 7/13 (harnesses) |
| §11 phasing | Task order |

**Gaps found and closed during review:**

- Task 10's `migrationBaseline` initially depended on `migrate.LedgerDDL`, which Task 3 defines unexported. Task 10 now names the rename explicitly.
- `internal/generate/store.go` emits `Migrations` for manifest-generated stores; it was in the file table but had no task. Folded into Task 13 step 3.
- `passkey` had no legacy-adoption entry in Task 7's `legacySQL` map (its four statements are long); it is covered by `TestPackagesApplyToEmptyDatabase` and its own package tests. Noted rather than duplicated.

**Type consistency:** `Set`, `Migration`, `Change`, `Snapshot`, `Result`, `Payload` are each defined once and used with the same shape throughout. `Apply(ctx, *db.DB, *Set)` is consistent across Tasks 3, 4, 7, 8, 12, 13. `Read(ctx, Querier)` is used with both `*sql.DB` and `*sql.Conn`, which the interface admits.
