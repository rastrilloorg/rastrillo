package eventlog_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	_ "modernc.org/sqlite"

	"amadan.net/rastrillo/rastrillo/eventlog"
	"amadan.net/rastrillo/rastrillo/migrate"
)

// openPath opens path as rastrillo.OpenDB does: busy_timeout before
// WAL, one connection. Each call is a separate handle, which is what
// the concurrent-start test needs.
func openPath(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

// openMem is a fresh database file (in-memory SQLite loses the
// database across connections, so "mem" means "throwaway").
func openMem(t *testing.T) *sql.DB { return openPath(t, filepath.Join(t.TempDir(), "app.db")) }

func mustExec(t *testing.T, w *sql.DB, q string) {
	t.Helper()
	if _, err := w.Exec(q); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// payloadColumn reads the column straight from SQLite rather than
// through Open, so the tests check the database, not the detection
// they are also testing.
func payloadColumn(t *testing.T, w *sql.DB) string {
	t.Helper()
	rows, err := w.Query(`SELECT name FROM pragma_table_info('eventlog') WHERE name IN ('payload', 'payload_v2')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		rows.Scan(&n)
		names = append(names, n)
	}
	if len(names) != 1 {
		t.Fatalf("eventlog payload columns %v: want exactly one", names)
	}
	return names[0]
}

// appendOld is a currency-unaware binary appending: the SQL today's
// Append ran before eventlog learned to detect its column. It cannot
// call eventlog.Open, which now finds payload_v2 and succeeds.
func appendOld(t *testing.T, ctx context.Context, w *sql.DB) error {
	t.Helper()
	const stream, writer = "ticket_types/1", "edge-a"
	var seq, lamport int64
	if err := w.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq), 0) + 1, COALESCE(MAX(lamport), 0) + 1 FROM eventlog WHERE stream = ?`,
		stream).Scan(&seq, &lamport); err != nil {
		t.Fatal(err)
	}
	_, err := w.ExecContext(ctx,
		`INSERT INTO eventlog (stream, writer, seq, lamport, ts, actor, kind, payload)
		 VALUES (?, ?, ?, ?, ?, 'human', 'updated', '{"Price":1250}')`,
		stream, writer, seq, lamport, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// ledger lists the migration ledger's ids, or none when the database
// has no ledger (an app that keeps its tables in Options.Migrations).
func ledger(t *testing.T, w *sql.DB) []string {
	t.Helper()
	var has bool
	if err := w.QueryRow(`SELECT EXISTS (SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'rastrillo_migrations')`).Scan(&has); err != nil {
		t.Fatal(err)
	}
	if !has {
		return nil
	}
	rows, err := w.Query(`SELECT id FROM rastrillo_migrations ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	return ids
}

var withMoney = migrate.Merge(eventlog.Schema, eventlog.MoneySchema)

// TestEventlogMoneyUpgrade runs the real migration set over the three
// starting points an app can upgrade from, and checks that a
// currency-unaware binary then fails while today's eventlog reads the
// renamed column.
func TestEventlogMoneyUpgrade(t *testing.T) {
	ctx := context.Background()
	cases := map[string]func(t *testing.T, w *sql.DB){
		"fresh": func(*testing.T, *sql.DB) {},
		"ledger-backed": func(t *testing.T, w *sql.DB) {
			if _, err := migrate.Apply(ctx, migrate.Pool(w), eventlog.Schema); err != nil {
				t.Fatal(err)
			}
			if err := appendOld(t, ctx, w); err != nil {
				t.Fatal(err)
			}
		},
		"pre-ledger": func(t *testing.T, w *sql.DB) {
			// Built by the old Migrations []string: eventlog's DDL, no ledger.
			for _, m := range eventlog.Schema.All() {
				mustExec(t, w, m.SQL)
			}
			if err := appendOld(t, ctx, w); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			w := openMem(t)
			setup(t, w)
			res, err := migrate.Apply(ctx, migrate.Pool(w), withMoney)
			if err != nil {
				t.Fatalf("upgrade: %v", err)
			}
			if name == "pre-ledger" && !res.Adopted {
				t.Error("pre-ledger database was not adopted")
			}
			if got := payloadColumn(t, w); got != "payload_v2" {
				t.Errorf("column after upgrade: %s", got)
			}
			if got := strings.Join(ledger(t, w), " "); got != "eventlog/0001_init eventlog_money/0001_payload_v2" {
				t.Errorf("ledger after upgrade: %s", got)
			}
			if err := appendOld(t, ctx, w); err == nil || !strings.Contains(err.Error(), "payload") {
				t.Errorf("old append after upgrade: %v, want a payload column error", err)
			}
			l, err := eventlog.Open(w, "edge-b")
			if err != nil || l.PayloadColumn() != "payload_v2" {
				t.Fatalf("Open after upgrade: %v, %v", l, err)
			}
			if name != "fresh" {
				var n int
				w.QueryRow(`SELECT COUNT(*) FROM eventlog WHERE payload_v2 IS NOT NULL`).Scan(&n)
				if n != 1 {
					t.Errorf("events after upgrade: %d, want 1", n)
				}
				evs, err := l.Events(ctx, "ticket_types/1")
				if err != nil || len(evs) != 1 || string(evs[0].Payload) != `{"Price":1250}` {
					t.Errorf("Events after upgrade: %v, %v", evs, err)
				}
			}
			// The old binary reruns its schema at boot: nothing changes.
			migrate.Apply(ctx, migrate.Pool(w), eventlog.Schema)
			if got := payloadColumn(t, w); got != "payload_v2" {
				t.Errorf("old schema re-apply changed the column to %s", got)
			}
		})
	}
}

// TestEventlogMoneyUpgradeNeedsTheMarker shows the post-adoption
// marker is load-bearing: without it, a pre-ledger database matches
// neither the whole set nor a base set, and adoption refuses it.
func TestEventlogMoneyUpgradeNeedsTheMarker(t *testing.T) {
	ctx := context.Background()
	body := eventlog.MoneySchema.All()[0].SQL
	unmarked := strings.Replace(body, "-- rastrillo: post-adoption\n", "", 1)
	if unmarked == body {
		t.Fatal("0001_payload_v2 does not start with the post-adoption marker")
	}
	noMarker := migrate.MustFromFS(fstest.MapFS{
		"migrations/0001_payload_v2.sql": {Data: []byte(unmarked)},
	}, "eventlog_money")
	w := openMem(t)
	for _, m := range eventlog.Schema.All() {
		mustExec(t, w, m.SQL)
	}
	if err := appendOld(t, ctx, w); err != nil {
		t.Fatal(err)
	}
	_, err := migrate.Apply(ctx, migrate.Pool(w), migrate.Merge(eventlog.Schema, noMarker))
	if err == nil || !strings.Contains(err.Error(), "cannot be adopted safely") {
		t.Errorf("upgrade without the marker: %v, want the adoption refusal", err)
	}
	if got := payloadColumn(t, w); got != "payload" {
		t.Errorf("refused upgrade left the column %s", got)
	}
}

// TestEventlogWithoutMoneyIsUntouched: an app whose events carry no
// money upgrades the framework, keeps payload, and the old code still
// works, so it needs no rollback path at all.
func TestEventlogWithoutMoneyIsUntouched(t *testing.T) {
	ctx := context.Background()
	w := openMem(t)
	if _, err := migrate.Apply(ctx, migrate.Pool(w), eventlog.Schema); err != nil {
		t.Fatal(err)
	}
	if got := payloadColumn(t, w); got != "payload" {
		t.Errorf("column: %s", got)
	}
	if err := appendOld(t, ctx, w); err != nil {
		t.Errorf("old code without money: %v", err)
	}
	l, err := eventlog.Open(w, "edge-b")
	if err != nil || l.PayloadColumn() != "payload" {
		t.Fatalf("Open without money: %v, %v", l, err)
	}
	if evs, err := l.Events(ctx, "ticket_types/1"); err != nil || len(evs) != 1 {
		t.Errorf("Events without money: %v, %v", evs, err)
	}
}

// TestEventlogUpgradeRollbackUpgrade is the money app's rollback: a
// backup restore, which brings the ledger back with the data, so the
// next upgrade applies the rename again.
func TestEventlogUpgradeRollbackUpgrade(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path, backup := filepath.Join(dir, "app.db"), filepath.Join(dir, "backup.db")
	w := openPath(t, path)
	if _, err := migrate.Apply(ctx, migrate.Pool(w), eventlog.Schema); err != nil {
		t.Fatal(err)
	}
	if err := appendOld(t, ctx, w); err != nil {
		t.Fatal(err)
	}
	mustExec(t, w, `VACUUM INTO '`+backup+`'`) // the backup the upgrade notes require
	if _, err := migrate.Apply(ctx, migrate.Pool(w), withMoney); err != nil {
		t.Fatal(err)
	}
	w.Close()

	// Roll back: restore the backup, files and all.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		os.Remove(path + suffix)
	}
	data, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	w = openPath(t, path)
	if got := payloadColumn(t, w); got != "payload" {
		t.Errorf("after restore: %s", got)
	}
	if got := strings.Join(ledger(t, w), " "); got != "eventlog/0001_init" {
		t.Errorf("ledger after restore: %s", got)
	}
	if err := appendOld(t, ctx, w); err != nil {
		t.Errorf("old code after restore: %v", err)
	}
	if _, err := eventlog.UpgradeBoundary(ctx, w); err == nil {
		t.Error("the restore kept an upgrade boundary")
	}

	// Upgrade again: the rename runs again and the ledger agrees.
	if _, err := migrate.Apply(ctx, migrate.Pool(w), withMoney); err != nil {
		t.Fatal(err)
	}
	if got := payloadColumn(t, w); got != "payload_v2" {
		t.Errorf("after second upgrade: %s", got)
	}
	if got := strings.Join(ledger(t, w), " "); got != "eventlog/0001_init eventlog_money/0001_payload_v2" {
		t.Errorf("ledger after second upgrade: %s", got)
	}
}

func TestOpenDetectsThePayloadColumn(t *testing.T) {
	for name, ddl := range map[string]string{
		"payload":    "CREATE TABLE eventlog (stream TEXT, writer TEXT, seq INTEGER, lamport INTEGER, ts TEXT, actor TEXT, kind TEXT, payload TEXT)",
		"payload_v2": "CREATE TABLE eventlog (stream TEXT, writer TEXT, seq INTEGER, lamport INTEGER, ts TEXT, actor TEXT, kind TEXT, payload_v2 TEXT)",
		"both":       "CREATE TABLE eventlog (stream TEXT, writer TEXT, seq INTEGER, lamport INTEGER, ts TEXT, actor TEXT, kind TEXT, payload TEXT, payload_v2 TEXT)",
		"neither":    "CREATE TABLE eventlog (stream TEXT, writer TEXT, seq INTEGER, lamport INTEGER, ts TEXT, actor TEXT, kind TEXT)",
	} {
		db := openMem(t)
		mustExec(t, db, ddl)
		l, err := eventlog.Open(db, "w1")
		switch name {
		case "payload", "payload_v2":
			if err != nil || l.PayloadColumn() != name {
				t.Errorf("%s: %v, %v", name, l, err)
			}
		default:
			if err == nil {
				t.Errorf("%s: Open accepted a table with %s", name, name)
			}
		}
	}
}

// Detection must not cache a failure: Open before the schema exists
// fails, and Open after migrate.Apply creates it succeeds.
func TestOpenBeforeSchemaThenAfter(t *testing.T) {
	db := openMem(t)
	if _, err := eventlog.Open(db, "w1"); err == nil {
		t.Fatal("Open succeeded with no eventlog table")
	}
	if _, err := migrate.Apply(context.Background(), migrate.Pool(db), eventlog.Schema); err != nil {
		t.Fatal(err)
	}
	if _, err := eventlog.Open(db, "w1"); err != nil {
		t.Errorf("Open after the schema exists: %v", err)
	}
}

func TestUpgradeBoundarySnapshotsEveryPair(t *testing.T) {
	db := openMem(t)
	ctx := context.Background()
	migrate.Apply(ctx, migrate.Pool(db), eventlog.Schema)
	for _, w := range []string{"w1", "w2"} {
		l, _ := eventlog.Open(db, w)
		for i := 0; i < 3; i++ {
			l.Append(ctx, "ticket/1", "updated", "human", map[string]any{"Price": 100})
		}
	}
	migrate.Apply(ctx, migrate.Pool(db), migrate.Merge(eventlog.Schema, eventlog.MoneySchema))
	b, err := eventlog.UpgradeBoundary(ctx, db)
	if err != nil || len(b) != 2 || b[eventlog.StreamWriter{"ticket/1", "w1"}] != 3 {
		t.Errorf("boundary = %v, %v", b, err)
	}
}

func TestUpgradeBoundaryWithoutTheUpgrade(t *testing.T) {
	db := openMem(t)
	ctx := context.Background()
	migrate.Apply(ctx, migrate.Pool(db), eventlog.Schema)
	_, err := eventlog.UpgradeBoundary(ctx, db)
	if err == nil || err.Error() != "eventlog: this database has no eventlog_upgrade table, so the money upgrade has not run on it" {
		t.Errorf("boundary without the upgrade: %v", err)
	}
}

// A handle that opened the log before the money upgrade (Open cached
// "payload") must see payload_v2 afterwards: UpgradeForMoney drops the
// handle's cache entry when it renames.
func TestOpenAfterUpgradeOnTheSameHandle(t *testing.T) {
	ctx := context.Background()
	db := openMem(t)
	migrate.Apply(ctx, migrate.Pool(db), eventlog.Schema)
	before, err := eventlog.Open(db, "w1")
	if err != nil || before.PayloadColumn() != "payload" {
		t.Fatalf("before: %v, %v", before, err)
	}
	if err := eventlog.UpgradeForMoney(ctx, db); err != nil {
		t.Fatal(err)
	}
	after, err := eventlog.Open(db, "w1")
	if err != nil || after.PayloadColumn() != "payload_v2" {
		t.Fatalf("after: %v, %v", after, err)
	}
	if _, err := after.Append(ctx, "s/1", "created", "human", map[string]any{"V": 2}); err != nil {
		t.Errorf("append after the upgrade: %v", err)
	}
}

func TestUpgradeForMoneyEveryStartPoint(t *testing.T) {
	ctx := context.Background()
	for name, setup := range map[string]func(*sql.DB){
		"fresh, ledger app":      func(db *sql.DB) { migrate.Apply(ctx, migrate.Pool(db), eventlog.Schema) },
		"ledger app with events": func(db *sql.DB) { migrate.Apply(ctx, migrate.Pool(db), eventlog.Schema); appendOld(t, ctx, db) },
		"Options.Migrations app": func(db *sql.DB) {
			for _, m := range eventlog.Schema.All() {
				mustExec(t, db, m.SQL)
			}
			mustExec(t, db, `CREATE TABLE IF NOT EXISTS ticket_types (id INTEGER PRIMARY KEY, name TEXT)`) // other tables, no ledger
			appendOld(t, ctx, db)
		},
		"ledger app, event log made outside it": func(db *sql.DB) {
			migrate.Apply(ctx, migrate.Pool(db), migrate.MustFromFS(fstest.MapFS{"migrations/0001_x.sql": {Data: []byte("CREATE TABLE x (id INTEGER);")}}, "app"))
			for _, m := range eventlog.Schema.All() {
				mustExec(t, db, m.SQL)
			}
			appendOld(t, ctx, db)
		},
	} {
		db := openMem(t)
		setup(db)
		for i := 0; i < 2; i++ { // the second call is a no-op
			if err := eventlog.UpgradeForMoney(ctx, db); err != nil {
				t.Fatalf("%s, call %d: %v", name, i+1, err)
			}
		}
		if col := payloadColumn(t, db); col != "payload_v2" {
			t.Errorf("%s: column %s", name, col)
		}
		if _, err := eventlog.UpgradeBoundary(ctx, db); err != nil {
			t.Errorf("%s: no boundary: %v", name, err)
		}
		if appendOld(t, ctx, db) == nil {
			t.Errorf("%s: today's append still works after the upgrade", name)
		}
		ledgered := strings.Contains(strings.Join(ledger(t, db), " "), "eventlog_money/0001_payload_v2")
		if ledgerExpected := !strings.Contains(name, "Options.Migrations"); ledgered != ledgerExpected {
			t.Errorf("%s: ledger records the rename = %v, want %v", name, ledgered, ledgerExpected)
		}
	}
}

// An event log that does not exist is not upgraded into one: the
// money upgrade renames, it never creates.
func TestUpgradeForMoneyRefusesAMissingLog(t *testing.T) {
	db := openMem(t)
	err := eventlog.UpgradeForMoney(context.Background(), db)
	if err == nil || !strings.Contains(err.Error(), "the table has neither payload nor payload_v2") {
		t.Errorf("upgrade with no event log: %v", err)
	}
	if got := ledger(t, db); got != nil {
		t.Errorf("a refused upgrade wrote a ledger: %v", got)
	}
}

// Two instances starting at once against the same Options.Migrations
// database: one renames, the other finds payload_v2 and does nothing.
func TestUpgradeForMoneyConcurrentStarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	seed := openPath(t, path)
	for _, m := range eventlog.Schema.All() {
		mustExec(t, seed, m.SQL)
	}
	seed.Close()
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = eventlog.UpgradeForMoney(context.Background(), openPath(t, path))
		}(i)
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Errorf("concurrent starts: %v, %v", errs[0], errs[1])
	}
	// Two nil errors alone would pass if a detection bug made both
	// starts no-ops; the rename must actually have happened, once.
	if got := payloadColumn(t, openPath(t, path)); got != "payload_v2" {
		t.Errorf("after two concurrent starts the payload column is %q, want payload_v2", got)
	}
}
