package sqlmoney_test

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	_ "modernc.org/sqlite"

	"amadan.net/rastrillo/rastrillo/currency"
	"amadan.net/rastrillo/rastrillo/currency/sqlmoney"
)

// The old schema and queries are today's generated tickets store,
// copied literally, so the tripwire is tested against what a released
// binary actually runs.
const oldSchema = `CREATE TABLE IF NOT EXISTS ticket_types (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL DEFAULT '',
  price INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT '',
  max_per_order TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

const newSchema = `CREATE TABLE IF NOT EXISTS ticket_types (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL DEFAULT '',
  price_minor INTEGER,
  price_currency TEXT,
  status TEXT NOT NULL DEFAULT '',
  max_per_order TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

// openPath opens with rastrillo.OpenDB's pragmas, busy_timeout before
// WAL, without importing the root package and everything it links.
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

func open(t *testing.T) *sql.DB {
	return openPath(t, filepath.Join(t.TempDir(), "app.db"))
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func legacyDB(t *testing.T, prices ...int64) *sql.DB {
	db := open(t)
	mustExec(t, db, oldSchema)
	for _, p := range prices {
		mustExec(t, db, `INSERT INTO ticket_types (name, price, created_at, updated_at) VALUES ('t', ?, 'x', 'x')`, p)
	}
	return db
}

func table(cur string, scale int) []sqlmoney.Table {
	return []sqlmoney.Table{{Name: "ticket_types", Fields: []string{"price"}, Legacy: &currency.Legacy{Currency: cur, Scale: scale}}}
}

func rows(t *testing.T, db *sql.DB) []string {
	var out []string
	rs, err := db.Query(`SELECT COALESCE(price_minor, 'NULL'), COALESCE(price_currency, 'NULL') FROM ticket_types ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rs.Close()
	for rs.Next() {
		var m, c string
		if err := rs.Scan(&m, &c); err != nil {
			t.Fatal(err)
		}
		out = append(out, m+" "+c)
	}
	if err := rs.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// columns lists the table's columns, so a refusal can be checked for
// leaving the schema exactly as it found it.
func columns(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rs, err := db.Query(`SELECT name FROM pragma_table_info('ticket_types') ORDER BY cid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rs.Close()
	var out []string
	for rs.Next() {
		var n string
		if err := rs.Scan(&n); err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	if err := rs.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// refusedUntouched asserts what every refusal promises: the error is
// exactly want, the table has the columns it had before (no half-added
// pair, no dropped legacy column), and no marker row says the upgrade
// happened.
func refusedUntouched(t *testing.T, db *sql.DB, err error, want string, before []string) {
	t.Helper()
	if err == nil || err.Error() != want {
		t.Errorf("refusal = %v\nwant %q", err, want)
	}
	if after := columns(t, db); !slices.Equal(after, before) {
		t.Errorf("a refused upgrade changed the columns: %v, want %v", after, before)
	}
	var markers int
	if err := db.QueryRow(`SELECT COUNT(*) FROM rastrillo_money`).Scan(&markers); err == nil && markers != 0 {
		t.Errorf("a refused upgrade left %d marker rows", markers)
	}
}

// Spec §12 item 1, every case with an exact expected value.
func TestUpgradeLegacyRows(t *testing.T) {
	for _, c := range []struct {
		name   string
		prices []int64
		cur    string
		want   []string // nil for a refusal
		err    string   // the whole message
	}{
		{"USD to USD", []int64{1250, 0}, "USD", []string{"1250 USD", "0 USD"}, ""},
		{"USD scale 2 to JPY", []int64{1200}, "JPY", []string{"12 JPY"}, ""},
		{"JPY inexact", []int64{1200, 1250}, "JPY", nil, "ticket_types.price: 1 rows cannot be converted, and nothing was changed:\nrow 2: 1250 at scale 2 is 12.50, which JPY cannot hold"},
		{"to KWD", []int64{1250}, "KWD", []string{"12500 KWD"}, ""},
		{"SLL", []int64{1250}, "SLL", []string{"1250 SLL"}, ""},
		{"overflow", []int64{math.MaxInt64 / 5}, "KWD", nil, "ticket_types.price: 1 rows cannot be converted, and nothing was changed:\nrow 1: 1844674407370955161 at scale 2 is too large for KWD"},
	} {
		db := legacyDB(t, c.prices...)
		before := columns(t, db)
		err := sqlmoney.Upgrade(context.Background(), db, table(c.cur, 2))
		if c.err != "" {
			refusedUntouched(t, db, err, c.err, before)
			var p int64
			if err := db.QueryRow(`SELECT price FROM ticket_types ORDER BY id LIMIT 1`).Scan(&p); err != nil || p != c.prices[0] {
				t.Errorf("%s: a refused upgrade changed the database (price %d, %v)", c.name, p, err)
			}
			continue
		}
		if err != nil || !slices.Equal(rows(t, db), c.want) {
			t.Errorf("%s: %v, %v; want %v", c.name, rows(t, db), err, c.want)
		}
	}
}

// The failure list stops at 20 rows so a large table's refusal stays
// readable; the rest are counted, not dropped silently.
func TestUpgradeListsAtMostTwentyRows(t *testing.T) {
	prices := make([]int64, 25)
	for i := range prices {
		prices[i] = 1250
	}
	db := legacyDB(t, prices...)
	err := sqlmoney.Upgrade(context.Background(), db, table("JPY", 2))
	if err == nil {
		t.Fatal("25 inexact rows were accepted")
	}
	msg := err.Error()
	if !strings.Contains(msg, "25 rows cannot be converted") || !strings.Contains(msg, "row 20: ") ||
		strings.Contains(msg, "row 21: ") || !strings.HasSuffix(msg, "\nand 5 more") {
		t.Errorf("refusal for 25 rows:\n%s", msg)
	}
}

// A legacy column with no rows needs no [legacy]: nothing is rescaled.
// A fresh database created by a released CREATE TABLE that still has
// the legacy column (Task 21's append-only migrations) takes this path.
func TestUpgradeEmptyLegacyColumnNeedsNoLegacy(t *testing.T) {
	db := legacyDB(t) // no rows
	if err := sqlmoney.Upgrade(context.Background(), db, []sqlmoney.Table{{Name: "ticket_types", Fields: []string{"price"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO ticket_types (name, price_minor, price_currency, created_at, updated_at) VALUES ('a', 5, 'EUR', 'x', 'x')`); err != nil {
		t.Errorf("the final columns are not there: %v", err)
	}
}

func TestUpgradeRefusesUndeclaredLegacyRows(t *testing.T) {
	db := legacyDB(t, 1250, 300)
	before := columns(t, db)
	err := sqlmoney.Upgrade(context.Background(), db, []sqlmoney.Table{{Name: "ticket_types", Fields: []string{"price"}}})
	refusedUntouched(t, db, err, "ticket_types has 2 rows from before money had a currency. Add [legacy] currency and scale to manifest/ticket_types.toml and run rastrillo generate.", before)
}

func TestUpgradeIsIdempotentAndFreshGetsMarker(t *testing.T) {
	db := legacyDB(t, 1250)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := sqlmoney.Upgrade(ctx, db, table("KWD", 2)); err != nil {
			t.Fatalf("boot %d: %v", i+1, err)
		}
	}
	if got := rows(t, db); !slices.Equal(got, []string{"12500 KWD"}) {
		t.Errorf("after two boots: %v; the second must change nothing", got)
	}
	var markers int
	db.QueryRow(`SELECT COUNT(*) FROM rastrillo_money`).Scan(&markers)
	if markers != 1 {
		t.Errorf("%d marker rows after two boots, want 1", markers)
	}
	fresh := open(t)
	mustExec(t, fresh, newSchema)
	if err := sqlmoney.Upgrade(ctx, fresh, table("KWD", 2)); err != nil {
		t.Fatal(err)
	}
	var v int
	fresh.QueryRow(`SELECT version FROM rastrillo_money`).Scan(&v)
	if v != 2 {
		t.Errorf("fresh database marker %d, want 2", v)
	}
}

// Review Focus 1: two instances boot at once against a legacy
// database. BEGIN IMMEDIATE serialises them; the second must find the
// legacy column gone and rescale nothing, or KWD 12.500 becomes 125.000.
func TestUpgradeConcurrentBootsRescaleOnce(t *testing.T) {
	const n = 2000 // enough rows that one boot's transaction is still open when the other begins
	path := filepath.Join(t.TempDir(), "app.db")
	seed := openPath(t, path)
	mustExec(t, seed, oldSchema)
	mustExec(t, seed, `INSERT INTO ticket_types (name, price, created_at, updated_at)
		WITH RECURSIVE i(k) AS (SELECT 1 UNION ALL SELECT k + 1 FROM i WHERE k < ?) SELECT 't', 1250, 'x', 'x' FROM i`, n)
	// With the marker table already there, the step's first statement
	// only reads. Without it, creating the table happens to take the
	// write lock first, which would hide a plain BEGIN.
	mustExec(t, seed, `CREATE TABLE rastrillo_money (version INTEGER NOT NULL)`)
	seed.Close()
	// A separate handle per instance, each connected before the start
	// signal, so the two upgrades begin together rather than one after
	// the other.
	dbs := []*sql.DB{openPath(t, path), openPath(t, path)}
	for _, db := range dbs {
		if err := db.Ping(); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, len(dbs))
	for i, db := range dbs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = sqlmoney.Upgrade(context.Background(), db, table("KWD", 2))
		}()
	}
	close(start)
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("boots: %v, %v", errs[0], errs[1])
	}
	if got := rows(t, openPath(t, path)); !slices.Equal(got, slices.Repeat([]string{"12500 KWD"}, n)) {
		t.Errorf("after two concurrent boots: %d rows, first %v; want %d rows of 12500 KWD, rescaled exactly once", len(got), got[:min(len(got), 1)], n)
	}
}

// Spec §12 item 2: an old binary pointed at the upgraded database
// fails every money query and changes no row. SQLite words the
// failures differently per statement, so each is asserted as it is.
// Spec §12 item 3 (the old generated handler, posted to) is Task 21's
// TestOldEditHandlerIsRefusedAfterUpgrade, which runs the real old code.
func TestOldBinaryQueriesFailAfterUpgrade(t *testing.T) {
	db := legacyDB(t, 1200)
	if err := sqlmoney.Upgrade(context.Background(), db, table("JPY", 2)); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, oldSchema) // the old binary's boot migration: a no-op on the existing table
	for q, want := range map[string]string{
		`UPDATE ticket_types SET name = 't', price = 1200, status = '', updated_at = 'y' WHERE id = 1`:                            "no such column: price",
		`INSERT INTO ticket_types (name, price, status, max_per_order, created_at, updated_at) VALUES ('n', 5, '', '', 'x', 'x')`: "has no column named price",
		`SELECT id, name, price, status FROM ticket_types`:                                                                        "no such column: price",
	} {
		_, err := db.Exec(q)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("old query %q: %v; want %q", q, err, want)
		}
	}
	if got := rows(t, db); !slices.Equal(got, []string{"12 JPY"}) {
		t.Errorf("rows after the old binary's attempts: %v", got)
	}
}

// Spec §12 item 13 and §1: a money field added to a table that has
// rows. Upgrade adds the two columns itself (no hand migration), the
// existing rows are NULL/NULL, and boot passes; a row with exactly one
// of the two NULL fails boot.
func TestAddedMoneyFieldAndPairingCheck(t *testing.T) {
	db := open(t)
	mustExec(t, db, newSchema) // no fee columns: the table predates the field
	mustExec(t, db, `INSERT INTO ticket_types (name, price_minor, price_currency, created_at, updated_at) VALUES ('a', 100, 'EUR', 'x', 'x')`)
	tables := []sqlmoney.Table{{Name: "ticket_types", Fields: []string{"price", "fee"}}}
	for i := 0; i < 2; i++ {
		if err := sqlmoney.Upgrade(context.Background(), db, tables); err != nil {
			t.Fatalf("boot %d with an added money field: %v", i+1, err)
		}
	}
	var feeMinor, feeCur sql.NullString
	db.QueryRow(`SELECT fee_minor, fee_currency FROM ticket_types`).Scan(&feeMinor, &feeCur)
	if feeMinor.Valid || feeCur.Valid {
		t.Errorf("an existing row got an amount for a field it never had: %v %v", feeMinor, feeCur)
	}
	mustExec(t, db, `UPDATE ticket_types SET fee_minor = 5`)
	want := "ticket_types.fee: 1 rows have an amount without a currency or a currency without an amount; fix them before starting"
	if err := sqlmoney.CheckPairs(context.Background(), db, tables); err == nil || err.Error() != want {
		t.Errorf("an amount without a currency: %v; want %q", err, want)
	}
	if err := sqlmoney.Upgrade(context.Background(), db, tables); err == nil || err.Error() != want {
		t.Errorf("boot with an amount without a currency: %v; want %q", err, want)
	}
}

// Half of a money field's pair is a schema someone edited by hand;
// adding the other half would hide whatever they meant, so boot stops.
func TestUpgradeRefusesHalfAPair(t *testing.T) {
	db := open(t)
	mustExec(t, db, newSchema)
	mustExec(t, db, `ALTER TABLE ticket_types ADD COLUMN fee_minor INTEGER`)
	mustExec(t, db, `INSERT INTO ticket_types (name, fee_minor, created_at, updated_at) VALUES ('a', 5, 'x', 'x')`)
	before := columns(t, db)
	err := sqlmoney.Upgrade(context.Background(), db, []sqlmoney.Table{{Name: "ticket_types", Fields: []string{"price", "fee"}}})
	refusedUntouched(t, db, err, "ticket_types.fee: 1 rows have an amount without a currency or a currency without an amount; fix them before starting", before)
}

func TestUpgradeRefusesNewerData(t *testing.T) {
	db := open(t)
	mustExec(t, db, newSchema)
	mustExec(t, db, `CREATE TABLE rastrillo_money (version INTEGER NOT NULL)`)
	mustExec(t, db, `INSERT INTO rastrillo_money VALUES (3)`)
	if err := sqlmoney.Upgrade(context.Background(), db, table("USD", 2)); err == nil || !strings.Contains(err.Error(), "version 3") {
		t.Errorf("newer data: %v", err)
	}
}

func TestUpgradeDropBlockedRollsBack(t *testing.T) {
	db := legacyDB(t, 1250)
	mustExec(t, db, `CREATE INDEX ticket_types_price ON ticket_types (price)`)
	err := sqlmoney.Upgrade(context.Background(), db, table("USD", 2))
	if err == nil {
		t.Fatal("upgrade succeeded with an index on the legacy column")
	}
	if want := "ticket_types.price cannot be replaced by price_minor because "; !strings.HasPrefix(err.Error(), want) {
		t.Errorf("blocked drop: %v; want %q", err, want)
	}
	var p int64
	if err := db.QueryRow(`SELECT price FROM ticket_types`).Scan(&p); err != nil || p != 1250 {
		t.Errorf("after a blocked drop: price %d, %v; want the database untouched", p, err)
	}
	var cols int
	db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('ticket_types') WHERE name IN ('price_minor', 'price_currency')`).Scan(&cols)
	if cols != 0 {
		t.Errorf("a rolled-back upgrade left %d new columns behind", cols)
	}
}

// Names are interpolated into SQL, so anything outside [a-z0-9_] is
// refused before a statement is built, and before anything is opened.
func TestUpgradeRefusesBadNames(t *testing.T) {
	db := open(t)
	mustExec(t, db, newSchema)
	for _, tb := range []sqlmoney.Table{
		{Name: "ticket_types; DROP TABLE x", Fields: []string{"price"}},
		{Name: "ticket_types", Fields: []string{"Price"}},
		{Name: "", Fields: []string{"price"}},
	} {
		bad := tb.Name
		if bad == "ticket_types" {
			bad = tb.Fields[0]
		}
		want := fmt.Sprintf("%q is not a table or column name the money upgrade can use: use only a-z, 0-9 and _", bad)
		for name, run := range map[string]func(context.Context, *sql.DB, []sqlmoney.Table) error{"Upgrade": sqlmoney.Upgrade, "CheckPairs": sqlmoney.CheckPairs} {
			if err := run(context.Background(), db, []sqlmoney.Table{tb}); err == nil || err.Error() != want {
				t.Errorf("%s(%+v): %v; want %q", name, tb, err, want)
			}
		}
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'rastrillo_money'`).Scan(&n)
	if n != 0 {
		t.Error("a refused name still created the marker table")
	}
}

// A legacy value SQLite stored as text or real (its columns are typed
// by affinity, not enforced) is listed like an inexact amount rather
// than read as zero or truncated.
func TestUpgradeRefusesNonIntegerLegacyValues(t *testing.T) {
	db := legacyDB(t, 100)
	mustExec(t, db, `INSERT INTO ticket_types (name, price, created_at, updated_at) VALUES ('t', 12.5, 'x', 'x')`)
	before := columns(t, db)
	err := sqlmoney.Upgrade(context.Background(), db, table("USD", 2))
	refusedUntouched(t, db, err, "ticket_types.price: 1 rows cannot be converted, and nothing was changed:\nrow 2: amount is not an integer count of minor units: got float64", before)
}
