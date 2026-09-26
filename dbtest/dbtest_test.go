package dbtest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"amadan.net/rastrillo/rastrillo/migrate"
)

// schema is enough to cover every kind of object sqlite_master holds —
// tables, an index, a trigger, a view — plus a Go migration, so the
// fence below compares more than a table list.
func schema(t testing.TB) *migrate.Set {
	t.Helper()
	s, err := migrate.FromFS(fstest.MapFS{
		"migrations/0001_notes.sql": {Data: []byte(`CREATE TABLE notes (id INTEGER PRIMARY KEY, owner_id INTEGER NOT NULL, body TEXT NOT NULL);
CREATE INDEX notes_owner ON notes(owner_id);`)},
		"migrations/0002_audit.sql": {Data: []byte(`CREATE TABLE note_audit (note_id INTEGER NOT NULL, at TEXT NOT NULL);
CREATE TRIGGER notes_audit AFTER INSERT ON notes BEGIN INSERT INTO note_audit VALUES (NEW.id, 'now'); END;
CREATE VIEW owners AS SELECT DISTINCT owner_id FROM notes;`)},
	}, "app")
	if err != nil {
		t.Fatal(err)
	}
	return s.Add(migrate.Migration{ID: "app/0003_seed_kinds", Fn: func(ctx context.Context, tx migrate.Tx) error {
		if _, err := tx.ExecContext(ctx, `CREATE TABLE kinds (name TEXT PRIMARY KEY)`); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO kinds VALUES ('plain'), ('pinned')`)
		return err
	}})
}

// The fence. A test run against a COPY of a database migrated once,
// rather than one migrated for it, is only worth anything if the two
// cannot differ — otherwise every test is quietly exercising a schema
// production does not have. Compare the whole of sqlite_master, which
// is every table, index, trigger and view with its exact SQL, and the
// ledger, which is what the next Apply reads.
func TestCopyMatchesAFreshMigration(t *testing.T) {
	t.Parallel()
	s := schema(t)

	fresh, err := open(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	if _, err := migrate.Apply(context.Background(), migrate.Pool(fresh), s); err != nil {
		t.Fatal(err)
	}

	tpl := FromSet(s)
	defer tpl.Remove()
	copied := tpl.Open(t)

	for _, q := range []string{
		`SELECT type, name, IFNULL(sql,'') FROM sqlite_master ORDER BY type, name`,
		`SELECT id, checksum, '' FROM rastrillo_migrations ORDER BY id`,
		`SELECT name, '', '' FROM kinds ORDER BY name`,
	} {
		want, got := rowsOf(t, fresh, q), rowsOf(t, copied, q)
		if len(want) == 0 {
			t.Fatalf("a fresh migration answered nothing for %s", q)
		}
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s\n copy:  %q\n fresh: %q", q, got, want)
		}
	}

	// And the copy is a migrated database as far as migrate is
	// concerned: applying the same set again is a no-op, not a
	// re-adoption or a checksum complaint.
	res, err := migrate.Apply(context.Background(), migrate.Pool(copied), s)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Applied) != 0 || res.Adopted || res.Skipped != 3 {
		t.Errorf("re-applying to a copy = %+v, want 3 skipped and nothing else", res)
	}
}

func rowsOf(t *testing.T, d *sql.DB, q string) []string {
	t.Helper()
	rows, err := d.Query(q)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a, b, c string
		if err := rows.Scan(&a, &b, &c); err != nil {
			t.Fatal(err)
		}
		out = append(out, a+" "+b+": "+c)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// Copies share nothing: a row one test writes is invisible to the next,
// which is the isolation a per-test migration used to buy.
func TestCopiesAreIndependent(t *testing.T) {
	t.Parallel()
	tpl := FromSet(schema(t))
	defer tpl.Remove()

	a, b := tpl.Open(t), tpl.Open(t)
	if _, err := a.Exec(`INSERT INTO notes (owner_id, body) VALUES (1, 'only in a')`); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := b.QueryRow(`SELECT count(*) FROM notes`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("copy b sees %d notes written to copy a", n)
	}
	// And the template itself was never written through a copy.
	if err := tpl.Open(t).QueryRow(`SELECT count(*) FROM notes`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("a later copy sees %d notes: a write reached the template", n)
	}
}

// The copy is opened the way the app's writer is: foreign keys are
// enforced, and it is WAL. A copy opened with the driver's defaults
// would let a test pass an insert production refuses.
func TestOpenedCopyEnforcesForeignKeys(t *testing.T) {
	t.Parallel()
	s, err := migrate.FromFS(fstest.MapFS{
		"migrations/0001_init.sql": {Data: []byte(`CREATE TABLE owners (id INTEGER PRIMARY KEY);
CREATE TABLE notes (id INTEGER PRIMARY KEY, owner_id INTEGER NOT NULL REFERENCES owners(id));`)},
	}, "fk")
	if err != nil {
		t.Fatal(err)
	}
	tpl := FromSet(s)
	defer tpl.Remove()
	d := tpl.Open(t)
	if _, err := d.Exec(`INSERT INTO notes (owner_id) VALUES (99)`); err == nil {
		t.Fatal("an orphan row was accepted: foreign keys are off on the copy")
	}
	var mode string
	if err := d.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", mode)
	}
}

// Built once, however many tests ask at once. A build per caller is
// the cost this package exists to remove, and two concurrent builds
// into one path would corrupt it.
func TestBuildsOnceUnderParallelCallers(t *testing.T) {
	var builds atomic.Int32
	inner := FromSet(schema(t))
	tpl := New(func(path string) error {
		builds.Add(1)
		return inner.build(path)
	})
	defer tpl.Remove()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tpl.Path(t)
		}()
	}
	wg.Wait()
	if n := builds.Load(); n != 1 {
		t.Fatalf("template built %d times, want 1", n)
	}
}

// fatalTB records a Fatal instead of failing the real test, so a test
// can assert that a Template failed the caller — and how — rather than
// taking the binary down.
type fatalTB struct {
	testing.TB
	msg string
}

func (f *fatalTB) Helper() {}
func (f *fatalTB) Fatal(args ...any) {
	f.msg = fmt.Sprint(args...)
	runtime.Goexit()
}
func (f *fatalTB) Fatalf(format string, args ...any) {
	f.msg = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

// fatalOf runs fn against a fatalTB on its own goroutine, because
// Goexit ends the goroutine that called it.
func fatalOf(t *testing.T, fn func(testing.TB)) string {
	t.Helper()
	f := &fatalTB{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(f)
	}()
	<-done
	return f.msg
}

// A build that fails fails the test that asked, with the build's own
// error, and every later caller gets the same answer rather than a
// second build that might half-succeed.
func TestBuildErrorFailsTheCaller(t *testing.T) {
	var builds atomic.Int32
	tpl := New(func(string) error {
		builds.Add(1)
		return errors.New("migration 0007 exploded")
	})
	defer tpl.Remove()
	for i := 0; i < 2; i++ {
		msg := fatalOf(t, func(tb testing.TB) { tpl.Path(tb) })
		if !strings.Contains(msg, "migration 0007 exploded") {
			t.Fatalf("call %d failed with %q, want the build's error", i, msg)
		}
	}
	if n := builds.Load(); n != 1 {
		t.Fatalf("a failed build was retried: %d builds", n)
	}
}

// A build that returns with a connection still open leaves the newest
// pages in a -wal file, and a copy of the main file alone would be a
// database silently behind the migration. Refuse it by name.
func TestBuildLeavingAConnectionOpenIsRefused(t *testing.T) {
	var leaked *sql.DB
	tpl := New(func(path string) error {
		d, err := open(path)
		if err != nil {
			return err
		}
		leaked = d
		_, err = d.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY)`)
		return err
	})
	defer tpl.Remove()
	defer func() {
		if leaked != nil {
			leaked.Close()
		}
	}()
	msg := fatalOf(t, func(tb testing.TB) { tpl.Path(tb) })
	if !strings.Contains(msg, "-wal") {
		t.Fatalf("a build with an open connection gave %q, want the -wal refusal", msg)
	}
}

// Remove deletes the template, and is safe on one never built.
func TestRemove(t *testing.T) {
	New(func(string) error { return nil }).Remove()

	tpl := FromSet(schema(t))
	tpl.Path(t)
	dir := tpl.dir
	tpl.Remove()
	if matches, _ := filepath.Glob(filepath.Join(dir, "*")); len(matches) != 0 {
		t.Fatalf("Remove left %v", matches)
	}
}

// The comparison the package exists for, on a small schema. The gap
// widens with every migration an app adds; with three it is already
// visible.
func BenchmarkFreshMigration(b *testing.B) {
	s := schema(b)
	for i := 0; i < b.N; i++ {
		d, err := open(filepath.Join(b.TempDir(), "fresh.db"))
		if err != nil {
			b.Fatal(err)
		}
		if _, err := migrate.Apply(context.Background(), migrate.Pool(d), s); err != nil {
			b.Fatal(err)
		}
		d.Close()
	}
}

func BenchmarkCopy(b *testing.B) {
	tpl := FromSet(schema(b))
	defer tpl.Remove()
	tpl.Path(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d, err := open(tpl.Path(b))
		if err != nil {
			b.Fatal(err)
		}
		d.Close()
	}
}
