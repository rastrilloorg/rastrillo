// Package dbtest hands each test a fresh, fully-migrated SQLite file by
// migrating ONE template per test binary and copying it per test.
//
// Why a copy (measured in Tito Go, 2026-08-10): its ~1600 database
// tests each cost almost exactly the same, median 210ms, and that
// flatness was the tell. It was not what the tests asserted but what
// they all did first: opening a fresh path ran ~400 DDL statements, and
// that alone was 207ms. It is not I/O — journal_mode=MEMORY with
// synchronous=OFF only reached 158ms, and an in-memory database 148ms —
// because the cost is SQLite parsing and running the schema, not
// writing it down. Copying an already-migrated file is 2.5ms, 82x
// cheaper, and turned that package's 387s of -short time into about
// 60s. The saving grows with the schema, which only ever grows.
//
// What keeps the copy honest is that the template is built by the real
// migration code, so a migration added tomorrow is in the template
// tomorrow with nothing to remember. What the copy cannot be honest
// about is anything the migration makes that must differ per database —
// a random namespace, a minted key. A copy carries the template's value
// into every test in the binary, which blinds the suite to exactly the
// collision a per-database value exists to prevent; re-mint it after
// the copy, the way a real database's own migration would have.
//
// The template holds schema only. Fixture data does not belong in it:
// a row stamped with the time the binary started is not a row stamped
// with the time the test did, so seeding stays in the test where the
// clock is honest. The ledger rows migrate writes are the one exception
// — they carry the template's applied_at, which is schema bookkeeping,
// not data a test should assert on.
package dbtest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	_ "modernc.org/sqlite"

	"amadan.net/rastrillo/rastrillo/migrate"
)

// Template is a migrated database built at most once, the first time a
// test asks for a copy, and shared read-only by every test after it.
// Safe for concurrent use: parallel tests all wait on the one build.
type Template struct {
	build func(path string) error

	once sync.Once
	dir  string
	path string
	err  error
}

// New returns a Template that build creates. build must leave a
// fully-migrated database at path and close every connection to it
// before returning: closing the last connection checkpoints the WAL
// into the main file, which is what makes that one file the whole
// database. A connection left open leaves the newest pages in a -wal
// file the copy would not carry, so the copy refuses it rather than
// hand out a database that is silently behind.
//
// New takes a function rather than a migrate.Set so an app whose
// migrations are not (yet) a Set can share the mechanism; FromSet is
// the usual constructor.
func New(build func(path string) error) *Template {
	return &Template{build: build}
}

// FromSet returns a Template migrated by migrate.Apply with s, on a
// database opened the way Open opens a copy.
func FromSet(s *migrate.Set) *Template {
	return New(func(path string) error {
		d, err := open(path)
		if err != nil {
			return err
		}
		if _, err := migrate.Apply(context.Background(), migrate.Pool(d), s); err != nil {
			d.Close()
			return err
		}
		return d.Close()
	})
}

// ready builds the template on first use. The error is kept rather
// than fatal here: the caller has a testing.TB and fails the test that
// asked, instead of taking the whole binary down from inside a
// sync.Once — and every later caller gets the same error, not a second
// attempt that might half-succeed.
func (tp *Template) ready() (string, error) {
	tp.once.Do(func() {
		dir, err := os.MkdirTemp("", "rastrillo-dbtest-")
		if err != nil {
			tp.err = fmt.Errorf("dbtest: template dir: %w", err)
			return
		}
		tp.dir = dir
		path := filepath.Join(dir, "template.db")
		if err := tp.build(path); err != nil {
			tp.err = fmt.Errorf("dbtest: build template: %w", err)
			return
		}
		if _, err := os.Stat(path); err != nil {
			tp.err = fmt.Errorf("dbtest: build left no database at %s: %w", path, err)
			return
		}
		if _, err := os.Stat(path + "-wal"); err == nil {
			tp.err = errors.New("dbtest: build left a -wal file beside the template, so a connection is still open; " +
				"close every connection before build returns, or copies will miss the newest pages")
			return
		}
		tp.path = path
	})
	return tp.path, tp.err
}

// CopyTo copies the template to path, for the tests that care where
// the file is: a second database, a restart, a path assertion. The
// caller opens it.
func (tp *Template) CopyTo(t testing.TB, path string) {
	t.Helper()
	src, err := tp.ready()
	if err != nil {
		t.Fatal(err)
	}
	if err := copyFile(src, path); err != nil {
		t.Fatalf("dbtest: copy template: %v", err)
	}
}

// Path copies the template into the test's own temporary directory
// and returns the copy's path, for an app that opens its database
// itself (db.Open, or its own opener). The directory, and the copy
// with it, is removed when the test ends.
func (tp *Template) Path(t testing.TB) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	tp.CopyTo(t, path)
	return path
}

// Open copies the template and opens the copy: busy_timeout before
// journal_mode=WAL, foreign keys on, one open connection — the writer
// pool db.Open builds, so migrate.Pool accepts it. The database is
// closed when the test ends.
func (tp *Template) Open(t testing.TB) *sql.DB {
	t.Helper()
	d, err := open(tp.Path(t))
	if err != nil {
		t.Fatalf("dbtest: open copy: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// Remove deletes the template. Call it from TestMain after m.Run: the
// template outlives every test that copied it, so no test's cleanup
// can own it, and without this each run leaves one file in the system
// temporary directory.
func (tp *Template) Remove() {
	if tp.dir != "" {
		os.RemoveAll(tp.dir)
	}
}

// open is db.Open's writer, without GORM. The pragma order is the one
// that matters: busy_timeout before journal_mode=WAL, because the
// reverse fails with SQLITE_BUSY when two connections open at once.
// It is repeated rather than imported because db links GORM, and this
// package is for apps that do not.
func open(path string) (*sql.DB, error) {
	d, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	d.SetMaxOpenConns(1)
	if err := d.Ping(); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
