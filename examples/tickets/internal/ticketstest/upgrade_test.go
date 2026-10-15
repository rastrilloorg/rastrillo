// upgrade_test.go is the existing app's upgrade path (spec §8, §12
// items 1-3): a database today's binary left is upgraded by the app's
// own startup, and afterwards the old binary's code fails loudly
// instead of writing amounts in the wrong scale.
package ticketstest

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"

	oldeditbasics "tickets/internal/oldgen/editbasics"
	oldstore "tickets/internal/oldgen/store/ticket_types"
)

// oldTicketTypesSchema is the CREATE TABLE the generator released
// before money had a currency, copied literally from
// gen/store/ticket_types/migrations.go at that release. It is a copy
// so this test keeps what was released, whatever the generator writes
// now.
const oldTicketTypesSchema = `CREATE TABLE IF NOT EXISTS ticket_types (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL DEFAULT '',
  price INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT '',
  max_per_order TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`

func openRaw(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := rastrillo.OpenDB(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func mustExec(t *testing.T, db *sql.DB, q string) {
	t.Helper()
	if _, err := db.Exec(q); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// discardRender is the old handler's render: what it would have drawn
// does not matter, only what it stored.
func discardRender(*rastrillo.Ctx, http.ResponseWriter, string, int, any) {}

// An existing tickets database, as today's binary left it, is upgraded
// by the app's own startup: Boot runs before the router exists.
func TestLegacyDatabaseIsUpgradedAtBoot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tickets.db")
	old := openRaw(t, path)
	mustExec(t, old, oldTicketTypesSchema) // today's generated CREATE TABLE, copied literally
	mustExec(t, old, `INSERT INTO ticket_types (name, price, status, max_per_order, created_at, updated_at) VALUES ('GA', 1250, 'on_sale', '', 'x', 'x')`)
	old.Close()
	app, db := newAppAt(t, path)
	var minor int64
	var cur string
	if err := db.QueryRow(`SELECT price_minor, price_currency FROM ticket_types`).Scan(&minor, &cur); err != nil || minor != 1250 || cur != "USD" {
		t.Fatalf("after boot: %d %s %v", minor, cur, err)
	}
	wantContains(t, get(t, app, "/admin/ticket_types/1").Body.String(), "$12.50")
}

// Spec §12 item 3 with the real old code: a JPY record shows "1200" in
// its edit form; posted unchanged to the old binary's handler, it would
// parse as 1200 cents and store 120000. After the upgrade that handler
// fails and the row is unchanged.
func TestOldEditHandlerIsRefusedAfterUpgrade(t *testing.T) {
	app, db := newApp(t)
	_ = app
	mustExec(t, db, `INSERT INTO ticket_types (name, price_minor, price_currency, status, max_per_order, created_at, updated_at) VALUES ('Tokyo', 1200, 'JPY', 'on_sale', '', 'x', 'x')`)
	old := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.SetPathValue("id", "1")
		oldeditbasics.Handle(&rastrillo.Ctx{DB: db, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Render: discardRender}, w, r)
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/admin/ticket_types/1/edit-basics", strings.NewReader(url.Values{"Name": {"Tokyo"}, "Price": {"1200"}, "Status": {"on_sale"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	old.ServeHTTP(rec, req)
	if rec.Code < 500 {
		t.Errorf("the old handler answered %d; it must fail on the missing column", rec.Code)
	}
	var minor int64
	var cur string
	db.QueryRow(`SELECT price_minor, price_currency FROM ticket_types WHERE id = 1`).Scan(&minor, &cur)
	if minor != 1200 || cur != "JPY" {
		t.Errorf("the row changed to %d %s", minor, cur)
	}
}

// The SQL tripwire at the app level (Task 15 removed the prototype's
// TestMoneyColumnRename): every query the old binary was generated
// with that reads or writes money fails against the upgraded table,
// and none of them changes a row. Its money-free queries (count,
// delete, the advanced update) are left out: they cannot store an
// amount in the wrong scale.
func TestOldBinaryQueriesFailAfterUpgrade(t *testing.T) {
	_, db := newApp(t)
	mustExec(t, db, `INSERT INTO ticket_types (name, price_minor, price_currency, status, max_per_order, created_at, updated_at) VALUES ('Tokyo', 1200, 'JPY', 'on_sale', '', 'x', 'x')`)
	q := oldstore.New(db)
	ctx := context.Background()
	calls := map[string]func() error{
		"Get": func() error { _, err := q.GetTicketType(ctx, 1); return err },
		"List": func() error {
			_, err := q.ListTicketTypes(ctx, oldstore.ListTicketTypesParams{Search: "", FilterStatus: "", PageLimit: 10})
			return err
		},
		"Create": func() error {
			_, err := q.CreateTicketType(ctx, oldstore.CreateTicketTypeParams{Name: "Osaka", Price: 120000, Status: "draft", Now: "x"})
			return err
		},
		"UpdateBasics": func() error {
			return q.UpdateTicketTypeBasics(ctx, oldstore.UpdateTicketTypeBasicsParams{Name: "Tokyo", Price: 120000, Status: "on_sale", Now: "x", ID: 1})
		},
	}
	for name, call := range calls {
		if err := call(); err == nil {
			t.Errorf("the old %s succeeded against the upgraded table", name)
		}
	}
	var n int
	var minor int64
	var cur string
	db.QueryRow(`SELECT COUNT(*), MAX(price_minor), MAX(price_currency) FROM ticket_types`).Scan(&n, &minor, &cur)
	if n != 1 || minor != 1200 || cur != "JPY" {
		t.Errorf("after the old queries: %d rows, %d %s", n, minor, cur)
	}
}
