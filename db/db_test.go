package db

import (
	"bytes"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestOpenWALAndPing(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "app.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var jm string
	if err := d.G.Raw("PRAGMA journal_mode").Scan(&jm).Error; err != nil {
		t.Fatal(err)
	}
	if jm != "wal" {
		t.Fatalf("journal_mode = %q, want wal", jm)
	}
}

// TestSecureDeleteOnTheWriterSticks pins the one actionable half of
// SKILL.md §2's "deleting a secret from SQLite does not unwrite it".
//
// PRAGMA secure_delete is per-connection, so an app that sets it as a
// statement rather than in the DSN is relying on the writer pool being
// capped at one connection — which it is (Open), and which is exactly
// the kind of fact that gets changed for a good reason by someone who
// has never read the paragraph that depends on it. Raise the writer
// cap and this fails, which is the moment to move the pragma into the
// DSN instead of discovering later that a value an app promised not to
// keep is sitting in a freed page.
//
// What this deliberately does NOT claim is that secure_delete makes
// the file safe. It cleans app.db after a checkpoint and never touches
// app.db-wal, where a written value lands first and where anything
// shipping WAL frames will already have read it. Measured by writing a
// value, deleting it and grepping the raw bytes; the conclusion is in
// SKILL.md because the fix is "do not write it", not a pragma.
func TestSecureDeleteOnTheWriterSticks(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "app.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if err := d.G.Exec("PRAGMA secure_delete = ON").Error; err != nil {
		t.Fatal(err)
	}
	// Real writes in between: the pragma has to survive the pool
	// handing the connection back and out again, which is the whole
	// question.
	if err := d.G.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`).Error; err != nil {
		t.Fatal(err)
	}
	for i := range 8 {
		if err := d.G.Exec(`INSERT INTO t (v) VALUES (?)`, i).Error; err != nil {
			t.Fatal(err)
		}
	}

	var on int
	// Through the writer's own *sql.DB, not d.G: dbresolver sends a
	// bare Raw to the READER pool, which never had the pragma set and
	// would report 0 no matter what the writer is doing.
	if err := d.writer.QueryRow("PRAGMA secure_delete").Scan(&on); err != nil {
		t.Fatal(err)
	}
	if on != 1 {
		t.Errorf("secure_delete on the writer reads %d after eight writes, want 1 — "+
			"the writer pool is no longer one connection, so setting this pragma as a "+
			"statement reaches only whichever connection ran it (SKILL.md §2)", on)
	}
}

// TestReadDuringOpenRows is the single-connection-deadlock regression:
// with one shared connection, an open *sql.Rows plus any second query
// hangs forever. The reader pool must allow a second concurrent read.
func TestReadDuringOpenRows(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "app.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.G.Exec("CREATE TABLE t (n INTEGER)").Error; err != nil {
		t.Fatal(err)
	}
	if err := d.G.Exec("INSERT INTO t VALUES (1), (2)").Error; err != nil {
		t.Fatal(err)
	}
	rows, err := d.reader.Query("SELECT n FROM t")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	rows.Next() // hold the first result set open...
	var n int
	// ...and issue a second read; this must not hang.
	if err := d.G.Raw("SELECT count(*) FROM t").Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("count = %d, want 2", n)
	}
}

// TestRecordNotFoundNotLogged pins IgnoreRecordNotFoundError: a scoped
// by-ID miss is the 404 contract's routine outcome and must not spam
// the log, while real errors still land there.
func TestRecordNotFoundNotLogged(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	d, err := Open(filepath.Join(t.TempDir(), "app.db"), log)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.G.Exec("CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT)").Error; err != nil {
		t.Fatal(err)
	}

	// The assertion is about the by-ID miss below, not the setup: a
	// slow CI runner can trip the 200ms SLOW SQL warning on the CREATE
	// TABLE itself (observed at 452ms on a GitHub runner), so start
	// measuring from here.
	buf.Reset()

	var got struct{ ID int64 }
	err = d.G.Table("notes").Where("id = ?", 99).Take(&got).Error
	if err != gorm.ErrRecordNotFound {
		t.Fatalf("err = %v, want ErrRecordNotFound", err)
	}
	if s := buf.String(); s != "" {
		t.Errorf("record-not-found produced log output: %q", s)
	}

	if err := d.G.Exec("SELECT * FROM no_such_table").Error; err == nil {
		t.Fatal("bad SQL did not error")
	}
	if !strings.Contains(buf.String(), "no_such_table") {
		t.Errorf("real error missing from log output: %q", buf.String())
	}
}

// TestConstraintErrorsTranslate pins TranslateError + the fork's
// Translate: a UNIQUE violation surfaces as gorm.ErrDuplicatedKey and
// an FK violation as gorm.ErrForeignKeyViolated — the sentinels GORM
// apps test against with errors.Is.
func TestConstraintErrorsTranslate(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "app.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, stmt := range []string{
		"CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT UNIQUE)",
		"CREATE TABLE notes (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id))",
		"INSERT INTO users (id, email) VALUES (1, 'a@example.com')",
	} {
		if err := d.G.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}

	err = d.G.Exec("INSERT INTO users (id, email) VALUES (2, 'a@example.com')").Error
	if !errors.Is(err, gorm.ErrDuplicatedKey) {
		t.Errorf("duplicate email: err = %v, want gorm.ErrDuplicatedKey", err)
	}
	err = d.G.Exec("INSERT INTO notes (user_id) VALUES (99)").Error
	if !errors.Is(err, gorm.ErrForeignKeyViolated) {
		t.Errorf("dangling user_id: err = %v, want gorm.ErrForeignKeyViolated", err)
	}
}

func TestWriterSerialized(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "app.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if got := d.writer.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("writer MaxOpenConnections = %d, want 1", got)
	}
}

// A time.Time in a bare numeric zone (as mail.ParseDate yields for a
// "+0100" Date header) must come back as a time, not a string the
// driver could not parse.
func TestTimesInNumericZonesRoundTrip(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "t.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.G.Exec("CREATE TABLE stamps (id INTEGER PRIMARY KEY, at DATETIME)").Error; err != nil {
		t.Fatal(err)
	}
	in := time.Date(2026, 8, 30, 13, 44, 12, 0, time.FixedZone("", 3600))
	if err := d.G.Exec("INSERT INTO stamps (at) VALUES (?)", in).Error; err != nil {
		t.Fatal(err)
	}
	var out time.Time
	if err := d.G.Raw("SELECT at FROM stamps").Scan(&out).Error; err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !out.Equal(in) {
		t.Fatalf("got %v, want %v", out, in)
	}
}

// SQLite's own date functions must read what the driver writes. The
// driver's default layout, time.Time.String(), made julianday and date
// return NULL, so raw SQL doing date arithmetic in an app got nothing
// back while every scan into a time.Time still worked. A bare
// time.Now() is the worst case: it carries a monotonic reading, which
// String() wrote into the column as " m=+0.000123".
func TestSQLiteDateFunctionsReadStoredTimes(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "t.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.G.Exec("CREATE TABLE stamps (id INTEGER PRIMARY KEY, at DATETIME)").Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if !strings.Contains(now.String(), " m=") {
		t.Fatalf("test premise wrong: time.Now() has no monotonic reading: %v", now)
	}
	earlier := time.Date(2026, 8, 30, 23, 30, 0, 0, time.FixedZone("", 3600))
	for _, at := range []time.Time{now, earlier} {
		if err := d.G.Exec("INSERT INTO stamps (at) VALUES (?)", at).Error; err != nil {
			t.Fatal(err)
		}
	}
	var row struct {
		Seconds *float64
		Day     *string
	}
	if err := d.G.Raw(`SELECT (julianday(MAX(at)) - julianday(MIN(at))) * 86400.0 AS seconds,
		(SELECT date(at) FROM stamps WHERE id = 2) AS day FROM stamps`).Scan(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Seconds == nil {
		t.Fatal("julianday returned NULL for a stored time")
	}
	if want := now.Sub(earlier).Seconds(); *row.Seconds < want-1 || *row.Seconds > want+1 {
		t.Errorf("julianday difference = %v seconds, want %v", *row.Seconds, want)
	}
	// 23:30 at +01:00 is 22:30 UTC the same day; date() reads the offset.
	if row.Day == nil || *row.Day != "2026-08-30" {
		t.Errorf("date(at) = %v, want 2026-08-30", row.Day)
	}
}
