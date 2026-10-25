package opsgen

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/generate/scratch"
	"amadan.net/rastrillo/rastrillo/internal/generate/shape"
	"amadan.net/rastrillo/rastrillo/internal/opsnames"
)

func TestMain(m *testing.M) {
	code := m.Run()
	scratch.Cleanup()
	os.Exit(code)
}

// The three fixtures are internal/generate's scopedFixtureResource,
// ticketTypes and mergeableFixtureResource, the shapes examples/notes
// and examples/tickets declare: scoped and exclusive, unscoped with
// money and a filter, and mergeable.
func bookmarks(t *testing.T) rastrillo.Resource {
	return valid(t, rastrillo.Resource{
		Name:  "bookmarks",
		Route: "/bookmarks",
		Store: rastrillo.Exclusive,
		Scope: rastrillo.UserScoped,
		List: rastrillo.List{
			Columns: []rastrillo.Column{{Field: "Title"}, {Field: "Link"}},
			Search:  true,
		},
		Form: rastrillo.Form{
			Basics:   []rastrillo.Field{{Name: "Title", Required: true}, {Name: "Link"}},
			Advanced: []rastrillo.Field{{Name: "Notes", Kind: rastrillo.Textarea}},
		},
	})
}

func ticketTypes(t *testing.T) rastrillo.Resource {
	scale := 2
	return valid(t, rastrillo.Resource{
		Name:     "ticket_types",
		Route:    "/admin/ticket_types",
		Store:    rastrillo.Exclusive,
		Currency: "USD",
		Legacy:   &rastrillo.Legacy{Currency: "USD", Scale: &scale},
		List: rastrillo.List{
			Columns: []rastrillo.Column{{Field: "Name"}, {Field: "Price", Kind: rastrillo.Money}, {Field: "Status"}},
			Search:  true,
			Filters: []rastrillo.Filter{{Field: "Status", Values: []string{"draft", "on_sale", "sold_out"}}},
		},
		Form: rastrillo.Form{
			Basics:   []rastrillo.Field{{Name: "Name", Required: true}, {Name: "Price", Kind: rastrillo.Money, Required: true}, {Name: "Status"}},
			Advanced: []rastrillo.Field{{Name: "MaxPerOrder"}},
		},
	})
}

func announcements(t *testing.T) rastrillo.Resource {
	return valid(t, rastrillo.Resource{
		Name:  "announcements",
		Route: "/admin/announcements",
		Store: rastrillo.Mergeable,
		List: rastrillo.List{
			Columns: []rastrillo.Column{{Field: "Title"}},
			Search:  true,
		},
		Form: rastrillo.Form{
			Basics: []rastrillo.Field{{Name: "Title", Required: true}, {Name: "Body", Kind: rastrillo.Textarea}},
		},
	})
}

func valid(t *testing.T, r rastrillo.Resource) rastrillo.Resource {
	t.Helper()
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	return r
}

// TestEmitGoldens pins the three shapes an operation package takes:
// scoped and exclusive, unscoped with money, a filter and two groups,
// and mergeable.
func TestEmitGoldens(t *testing.T) {
	for _, c := range []struct {
		name string
		r    rastrillo.Resource
	}{
		{"bookmarks", bookmarks(t)},
		{"ticket_types", ticketTypes(t)},
		{"announcements", announcements(t)},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module scratch\n\ngo 1.25.0\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			genDir := filepath.Join(root, "gen")
			written, skipped, err := Emit(root, genDir, c.r)
			if err != nil {
				t.Fatal(err)
			}
			if len(written) != 6 || skipped != nil {
				t.Fatalf("written = %v, skipped = %v; want six files and nothing skipped", written, skipped)
			}
			for _, f := range []string{"types", "list", "get", "create", "update", "delete"} {
				got, err := os.ReadFile(filepath.Join(genDir, "ops", c.name, f+".go"))
				if err != nil {
					t.Fatal(err)
				}
				want, err := os.ReadFile(filepath.Join("testdata", "ops", c.name, f+".go.golden"))
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != string(want) {
					t.Errorf("%s/%s.go differs from testdata/ops/%s/%s.go.golden:\n%s", c.name, f, c.name, f, got)
				}
			}
		})
	}
}

// TestRuntime builds a scratch app with every store shape and runs the
// operations against a real database: trimming and ownership, required
// fields with nothing written, an empty update that writes nothing,
// both list modes, money defaults, checks and conflicts, actors on
// events, and Invoke's decoding.
func TestRuntime(t *testing.T) {
	app := scratch.App(t, map[string]string{
		"manifest/bookmarks.toml":     bookmarksTOML,
		"manifest/ticket_types.toml":  ticketTypesTOML,
		"manifest/announcements.toml": announcementsTOML,
		"manifest/prices.toml":        pricesTOML,
		"manifest/fees.toml":          feesTOML,
		"manifest/logs.toml":          logsTOML,
	})
	scratch.Generate(t, app)
	// fees and logs have no operation test of their own; the build is
	// what proves a list-only money column and a formless resource
	// compile.
	if out, err := scratch.GoBuild(app); err != nil {
		t.Fatalf("building every generated package:\n%s", out)
	}
	scratch.Write(t, app, "opstest/ops_test.go", runtimeTest)
	if out, err := scratch.GoTest(app, "./opstest"); err != nil {
		t.Fatalf("the generated operations:\n%s", out)
	}
}

const bookmarksTOML = `name  = "bookmarks"
route = "/bookmarks"
scope = "user"

[list]
columns = [{ field = "Title" }, { field = "Link" }]
search  = true

[form]
basics   = [{ name = "Title", required = true }, { name = "Link" }]
advanced = [{ name = "Notes", kind = "textarea" }]
`

// ticketTypesTOML is examples/tickets' ticket_types without [legacy]: a
// fresh scratch database has no old integers to upgrade.
const ticketTypesTOML = `name  = "ticket_types"
route = "/admin/ticket_types"
currency = "USD"

[list]
columns = [{ field = "Name" }, { field = "Price", kind = "money" }, { field = "Status" }]
search  = true

[[list.filters]]
field  = "Status"
values = ["draft", "on_sale", "sold_out"]

[form]
basics   = [{ name = "Name", required = true }, { name = "Price", kind = "money", required = true }, { name = "Status" }]
advanced = [{ name = "MaxPerOrder" }]
`

const announcementsTOML = `name  = "announcements"
route = "/admin/announcements"
store = "mergeable"

[list]
columns = [{ field = "Title" }]
search  = true

[form]
basics = [{ name = "Title", required = true }, { name = "Body", kind = "textarea" }]
`

// pricesTOML is the shape no example has: mergeable with an optional
// amount, so the clear, the refill and the store's own currency check
// under its write lock are all exercised.
const pricesTOML = `name     = "prices"
route    = "/prices"
store    = "mergeable"
currency = "EUR"

[list]
columns = [{ field = "Title" }, { field = "Amount", kind = "money" }]

[form]
basics = [{ name = "Title", required = true }, { name = "Amount", kind = "money" }]
`

// feesTOML has a money column the form does not carry, so the result
// shows an amount nothing can set.
const feesTOML = `name     = "fees"
route    = "/fees"
currency = "EUR"

[list]
columns = [{ field = "Title" }, { field = "Amount", kind = "money" }]

[form]
basics = [{ name = "Title", required = true }]
`

// logsTOML is list columns and no form: nothing a client can send.
const logsTOML = `name  = "logs"
route = "/logs"

[list]
columns = [{ field = "Title" }]
`

// runtimeTest is Go source, carried as a constant because it compiles
// against the scratch app's generated packages, not this one.
const runtimeTest = `package opstest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/eventlog"
	"amadan.net/rastrillo/rastrillo/op"

	"scratch/gen"
	ops_announcements "scratch/gen/ops/announcements"
	ops_bookmarks "scratch/gen/ops/bookmarks"
	ops_prices "scratch/gen/ops/prices"
	ops_ticket_types "scratch/gen/ops/ticket_types"
	announcementsstore "scratch/gen/store/announcements"
	bookmarksstore "scratch/gen/store/bookmarks"
	feesstore "scratch/gen/store/fees"
	logsstore "scratch/gen/store/logs"
	pricesstore "scratch/gen/store/prices"
	ticket_typesstore "scratch/gen/store/ticket_types"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	var migrations []string
	// Every store, because Boot checks each exclusive table's money
	// columns and refuses a database missing one.
	for _, m := range [][]string{bookmarksstore.Migrations, ticket_typesstore.Migrations, announcementsstore.Migrations, pricesstore.Migrations, feesstore.Migrations, logsstore.Migrations} {
		migrations = append(migrations, m...)
	}
	db, err := rastrillo.OpenDB(filepath.Join(t.TempDir(), "app.db"), migrations)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := gen.Boot(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

func as(db *sql.DB, subject string) *rastrillo.Ctx {
	return &rastrillo.Ctx{DB: db, Actor: rastrillo.Actor{Subject: subject, Access: rastrillo.Write}}
}

func wantInvalid(t *testing.T, err error, field, msg string) {
	t.Helper()
	var inv op.Invalid
	if !errors.As(err, &inv) || inv.Fields[field] != msg {
		t.Fatalf("err = %v, want Invalid with %s = %q", err, field, msg)
	}
}

func TestBookmarksCreateTrimsAndBelongsToTheCaller(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	b, err := ops_bookmarks.Create(ctx, as(db, "alice"), ops_bookmarks.CreateInput{
		Title: op.Some("  Doors  "), Link: op.Some(" https://x.test "), Notes: op.Some(" keep "),
	})
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != "Doors" || b.Link != "https://x.test" || b.Notes != " keep " {
		t.Fatalf("created %+v; want text trimmed and the textarea kept as sent", b)
	}
	if _, err := ops_bookmarks.Get(ctx, as(db, "bob"), b.ID); !errors.As(err, new(op.NotFound)) {
		t.Fatalf("bob's Get = %v, want NotFound", err)
	}
	if _, err := ops_bookmarks.Get(ctx, &rastrillo.Ctx{DB: db}, b.ID); !errors.As(err, new(op.Forbidden)) {
		t.Fatalf("Get with no subject = %v, want Forbidden", err)
	}
}

func TestBookmarksCreateRequiresATitleAndWritesNothing(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	_, err := ops_bookmarks.Create(ctx, as(db, "alice"), ops_bookmarks.CreateInput{Title: op.Some("   ")})
	wantInvalid(t, err, "Title", "Title is required")
	p, err := ops_bookmarks.List(ctx, as(db, "alice"), ops_bookmarks.ListInput{PageNumber: 1})
	if err != nil || p.Total != 0 {
		t.Fatalf("after a refused create: %+v, %v", p, err)
	}
}

func TestBookmarksEmptyUpdateWritesNothing(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	alice := as(db, "alice")
	b, err := ops_bookmarks.Create(ctx, alice, ops_bookmarks.CreateInput{Title: op.Some("Doors"), Link: op.Some("x")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE bookmarks SET updated_at = '2000-01-01T00:00:00Z'"); err != nil {
		t.Fatal(err)
	}
	got, err := ops_bookmarks.Update(ctx, alice, b.ID, ops_bookmarks.UpdateInput{})
	if err != nil || got.UpdatedAt != "2000-01-01T00:00:00Z" || got.Title != "Doors" {
		t.Fatalf("empty update = %+v, %v; want the row untouched", got, err)
	}
	got, err = ops_bookmarks.Update(ctx, alice, b.ID, ops_bookmarks.UpdateInput{Title: op.Some("Gates")})
	if err != nil || got.Title != "Gates" || got.Link != "x" || got.UpdatedAt == "2000-01-01T00:00:00Z" {
		t.Fatalf("title-only update = %+v, %v; want Gates, the link kept, updated_at moved", got, err)
	}
	if _, err := ops_bookmarks.Update(ctx, as(db, "bob"), b.ID, ops_bookmarks.UpdateInput{Title: op.Some("Mine")}); !errors.As(err, new(op.NotFound)) {
		t.Fatalf("bob's update = %v, want NotFound", err)
	}
	if err := ops_bookmarks.Delete(ctx, as(db, "bob"), b.ID); !errors.As(err, new(op.NotFound)) {
		t.Fatalf("bob's delete = %v, want NotFound", err)
	}
	if err := ops_bookmarks.Delete(ctx, alice, b.ID); err != nil {
		t.Fatal(err)
	}
	if err := ops_bookmarks.Delete(ctx, alice, b.ID); !errors.As(err, new(op.NotFound)) {
		t.Fatalf("second delete = %v, want NotFound", err)
	}
}

func TestBookmarksListPagesByCursorAndByNumber(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	alice := as(db, "alice")
	for _, title := range []string{"a", "b", "c", "d", "e"} {
		if _, err := ops_bookmarks.Create(ctx, alice, ops_bookmarks.CreateInput{Title: op.Some(title)}); err != nil {
			t.Fatal(err)
		}
	}
	all, err := ops_bookmarks.List(ctx, alice, ops_bookmarks.ListInput{PageNumber: 1, Limit: 100})
	if err != nil || all.Total != 5 || len(all.Items) != 5 {
		t.Fatalf("page 1 = %+v, %v", all, err)
	}
	var got []int64
	after := ""
	for {
		p, err := ops_bookmarks.List(ctx, alice, ops_bookmarks.ListInput{After: after, Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range p.Items {
			got = append(got, b.ID)
		}
		if p.NextAfter == "" {
			break
		}
		after = p.NextAfter
	}
	for i, b := range all.Items {
		if i >= len(got) || got[i] != b.ID {
			t.Fatalf("cursor pages %v; want the numbered order %+v", got, all.Items)
		}
	}
	if len(got) != 5 {
		t.Fatalf("cursor pages %v; want five rows", got)
	}
	_, err = ops_bookmarks.List(ctx, alice, ops_bookmarks.ListInput{After: "nonsense"})
	wantInvalid(t, err, "after", "Use the next_after value from the previous page.")
	empty, err := ops_bookmarks.List(ctx, as(db, "bob"), ops_bookmarks.ListInput{})
	if err != nil || empty.Items == nil || len(empty.Items) != 0 {
		t.Fatalf("bob's list = %#v, %v; want an empty, non-nil page", empty.Items, err)
	}
}

func TestTicketTypesMoneyDefaultsAndChecks(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	app := &rastrillo.Ctx{DB: db}
	tt, err := ops_ticket_types.Create(ctx, app, ops_ticket_types.CreateInput{Name: op.Some("GA"), Price: op.Some(op.Money{Amount: 1490})})
	if err != nil || tt.Price == nil || *tt.Price != (op.Money{Amount: 1490, Currency: "USD"}) {
		t.Fatalf("create with no currency = %+v, %v; want USD, the manifest's", tt, err)
	}
	tt, err = ops_ticket_types.Create(ctx, app, ops_ticket_types.CreateInput{Name: op.Some("VIP"), Price: op.Some(op.Money{Amount: 0, Currency: " eur "})})
	if err != nil || *tt.Price != (op.Money{Amount: 0, Currency: "EUR"}) {
		t.Fatalf("create with eur = %+v, %v; want EUR 0: a zero amount is sent, not missing", tt, err)
	}
	_, err = ops_ticket_types.Create(ctx, app, ops_ticket_types.CreateInput{Name: op.Some("x")})
	wantInvalid(t, err, "Price", "Price is required")
	_, err = ops_ticket_types.Create(ctx, app, ops_ticket_types.CreateInput{Name: op.Some("x"), Price: op.Some(op.Money{Amount: 1, Currency: "ZZZ"})})
	wantInvalid(t, err, "Price", "Use a currency code this app supports, like USD.")
	_, err = ops_ticket_types.Create(ctx, app, ops_ticket_types.CreateInput{Name: op.Some("x"), Price: op.Some(op.Money{Amount: -1})})
	wantInvalid(t, err, "Price", "Enter an amount of 0 or more.")
}

func TestTicketTypesUpdateKeepsTheCurrency(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	app := &rastrillo.Ctx{DB: db}
	tt, err := ops_ticket_types.Create(ctx, app, ops_ticket_types.CreateInput{Name: op.Some("GA"), Price: op.Some(op.Money{Amount: 1490})})
	if err != nil {
		t.Fatal(err)
	}
	got, err := ops_ticket_types.Update(ctx, app, tt.ID, ops_ticket_types.UpdateInput{Price: op.Some(op.Money{Amount: 2000})})
	if err != nil || *got.Price != (op.Money{Amount: 2000, Currency: "USD"}) || got.Name != "GA" {
		t.Fatalf("update with no currency = %+v, %v; want USD 2000, the name kept", got, err)
	}
	_, err = ops_ticket_types.Update(ctx, app, tt.ID, ops_ticket_types.UpdateInput{Name: op.Some("Changed"), Price: op.Some(op.Money{Amount: 2000, Currency: "EUR"})})
	var conflict op.Conflict
	if !errors.As(err, &conflict) || conflict.Message != "Price is in USD. Send the amount in USD, or leave out currency." {
		t.Fatalf("update in EUR = %v, want Conflict", err)
	}
	got, err = ops_ticket_types.Get(ctx, app, tt.ID)
	if err != nil || got.Name != "GA" || *got.Price != (op.Money{Amount: 2000, Currency: "USD"}) {
		t.Fatalf("after the refused update: %+v, %v; want nothing written", got, err)
	}
	got, err = ops_ticket_types.Update(ctx, app, tt.ID, ops_ticket_types.UpdateInput{Price: op.Some(op.Money{Amount: 2100, Currency: "usd"})})
	if err != nil || *got.Price != (op.Money{Amount: 2100, Currency: "USD"}) {
		t.Fatalf("update in usd = %+v, %v", got, err)
	}
}

func TestTicketTypesListFilters(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	app := &rastrillo.Ctx{DB: db}
	for _, s := range []string{"draft", "on_sale"} {
		if _, err := ops_ticket_types.Create(ctx, app, ops_ticket_types.CreateInput{Name: op.Some(s), Price: op.Some(op.Money{Amount: 1}), Status: op.Some(s)}); err != nil {
			t.Fatal(err)
		}
	}
	p, err := ops_ticket_types.List(ctx, app, ops_ticket_types.ListInput{FilterStatus: "draft", PageNumber: 1, Limit: 10})
	if err != nil || p.Total != 1 || len(p.Items) != 1 || p.Items[0].Status != "draft" {
		t.Fatalf("draft page = %+v, %v", p, err)
	}
	p, err = ops_ticket_types.List(ctx, app, ops_ticket_types.ListInput{FilterStatus: "on_sale"})
	if err != nil || len(p.Items) != 1 || p.Items[0].Status != "on_sale" {
		t.Fatalf("on_sale cursor page = %+v, %v", p, err)
	}
}

func TestAnnouncementsEventsNameTheActor(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	agent := &rastrillo.Ctx{
		DB:    db,
		Actor: rastrillo.Actor{Subject: "7", Agent: "Claude", Credential: "k3Jx9QpL2mWa", Access: rastrillo.Write},
		// Unscoped writes refuse tokens by default; this test is about
		// what the event records, so its policy allows.
		Authorize: func(context.Context, rastrillo.Actor, rastrillo.Operation) error { return nil },
	}
	a, err := ops_announcements.Create(ctx, agent, ops_announcements.CreateInput{Title: op.Some(" Doors "), Body: op.Some("at six")})
	if err != nil || a.Title != "Doors" {
		t.Fatalf("create = %+v, %v", a, err)
	}
	if _, err := ops_announcements.Update(ctx, agent, a.ID, ops_announcements.UpdateInput{}); err != nil {
		t.Fatal(err)
	}
	got, err := ops_announcements.Update(ctx, agent, a.ID, ops_announcements.UpdateInput{Body: op.Some("at seven")})
	if err != nil || got.Body != "at seven" || got.Title != "Doors" {
		t.Fatalf("body-only update = %+v, %v", got, err)
	}
	writer, err := eventlog.LocalWriter(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	log, err := eventlog.Open(db, writer)
	if err != nil {
		t.Fatal(err)
	}
	events, err := log.Events(ctx, announcementsstore.StreamPrefix+"1")
	if err != nil || len(events) != 2 {
		t.Fatalf("events = %d, %v; want created and one update: an empty update appends nothing", len(events), err)
	}
	for _, ev := range events {
		if ev.Actor != "agent:Claude for user:7 token:k3Jx9QpL2mWa" {
			t.Errorf("%s event actor = %q", ev.Kind, ev.Actor)
		}
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(events[1].Payload, &keys); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 3 || keys["body"] == nil || keys["V"] == nil || keys["updated_at"] == nil {
		t.Errorf("update payload %s; want only V, body and updated_at", events[1].Payload)
	}
	if err := ops_announcements.Delete(ctx, agent, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := ops_announcements.Delete(ctx, agent, a.ID); !errors.As(err, new(op.NotFound)) {
		t.Fatalf("second delete = %v, want NotFound", err)
	}
	if _, err := ops_announcements.Update(ctx, agent, a.ID, ops_announcements.UpdateInput{Body: op.Some("x")}); !errors.As(err, new(op.NotFound)) {
		t.Fatalf("update after delete = %v, want NotFound", err)
	}
}

func TestPricesUpdateRefusesAChangedCurrency(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	app := &rastrillo.Ctx{DB: db}
	p, err := ops_prices.Create(ctx, app, ops_prices.CreateInput{Title: op.Some("Lunch"), Amount: op.Nullable[op.Money]{Value: op.Money{Amount: 1250}, Set: true}})
	if err != nil || *p.Amount != (op.Money{Amount: 1250, Currency: "EUR"}) {
		t.Fatalf("create = %+v, %v", p, err)
	}
	_, err = ops_prices.Update(ctx, app, p.ID, ops_prices.UpdateInput{Amount: op.Nullable[op.Money]{Value: op.Money{Amount: 1, Currency: "USD"}, Set: true}})
	if !errors.As(err, new(op.Conflict)) {
		t.Fatalf("update in USD = %v, want Conflict", err)
	}
	got, err := ops_prices.Update(ctx, app, p.ID, ops_prices.UpdateInput{Amount: op.Nullable[op.Money]{Set: true, Null: true}})
	if err != nil || got.Amount != nil {
		t.Fatalf("clear = %+v, %v", got, err)
	}
	got, err = ops_prices.Update(ctx, app, p.ID, ops_prices.UpdateInput{Amount: op.Nullable[op.Money]{Value: op.Money{Amount: 900, Currency: "usd"}, Set: true}})
	if err != nil || *got.Amount != (op.Money{Amount: 900, Currency: "USD"}) {
		t.Fatalf("refill in USD after a clear = %+v, %v", got, err)
	}
}

func TestDescriptorsInvoke(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	alice := as(db, "alice")
	ds := ops_bookmarks.Descriptors()
	byID := map[string]op.Descriptor{}
	for _, d := range ds {
		if !d.Exposed || !json.Valid(d.Input) || !json.Valid(d.Result) || d.Invoke == nil {
			t.Fatalf("%s: %+v", d.ID, d)
		}
		byID[d.ID] = d
	}
	if len(byID) != 5 {
		t.Fatalf("descriptors = %v", byID)
	}
	res, err := byID["bookmarks.create"].Invoke(ctx, alice, json.RawMessage("{\"title\":\"Doors\"}"))
	b, ok := res.(ops_bookmarks.Bookmark)
	if err != nil || !ok || b.Title != "Doors" {
		t.Fatalf("Invoke create = %#v, %v", res, err)
	}
	_, err = byID["bookmarks.create"].Invoke(ctx, alice, json.RawMessage("{\"title\":null}"))
	wantInvalid(t, err, "title", "Send a value, or leave this field out.")
	_, err = byID["bookmarks.update"].Invoke(ctx, alice, json.RawMessage("{\"id\":1,\"nope\":1}"))
	wantInvalid(t, err, "nope", "Unknown field. Check the spelling and case.")
	res, err = byID["bookmarks.update"].Invoke(ctx, alice, json.RawMessage("{\"id\":1,\"link\":\"x\"}"))
	if b, ok := res.(ops_bookmarks.Bookmark); err != nil || !ok || b.Link != "x" || b.Title != "Doors" {
		t.Fatalf("Invoke update = %#v, %v", res, err)
	}
	res, err = byID["bookmarks.list"].Invoke(ctx, alice, json.RawMessage("{\"limit\":1}"))
	if l, ok := res.(ops_bookmarks.ListResult); err != nil || !ok || len(l.Bookmarks) != 1 || l.NextAfter != nil {
		t.Fatalf("Invoke list = %#v, %v", res, err)
	}
	if _, err := byID["bookmarks.delete"].Invoke(ctx, alice, json.RawMessage("{\"id\":1}")); err != nil {
		t.Fatal(err)
	}
	for _, d := range ops_ticket_types.Descriptors() {
		if d.Exposed {
			t.Errorf("%s is exposed; ticket_types is unscoped with no [api]", d.ID)
		}
	}
}
`

// TestEmitFormless pins the shape Validate allows but no example has:
// list columns and no form fields. Nothing can be sent, so Update is a
// read, and the file must still parse.
func TestEmitFormless(t *testing.T) {
	r := valid(t, rastrillo.Resource{
		Name:  "logs",
		Route: "/logs",
		List:  rastrillo.List{Columns: []rastrillo.Column{{Field: "Title"}}},
	})
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module scratch\n\ngo 1.25.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Emit(root, filepath.Join(root, "gen"), r); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "gen", "ops", "logs", "update.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "return load(ctx, rc, id)") || strings.Contains(string(got), "func update(") {
		t.Errorf("update.go for a formless resource should only read:\n%s", got)
	}
	wantValidSchemas(t, filepath.Join(root, "gen", "ops", "logs", "types.go"))
}

// emitTo runs Emit for r in a fresh module and returns the gen/ops/<name> directory.
func emitTo(t *testing.T, r rastrillo.Resource) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module scratch\n\ngo 1.25.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Emit(root, filepath.Join(root, "gen"), r); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "gen", "ops", r.Name)
}

// wantValidSchemas evaluates every constant in typesPath's const blocks
// (raw strings joined with +) and requires each to be JSON: a schema
// with a stray comma would be served to every client of the API.
func wantValidSchemas(t *testing.T, typesPath string) {
	t.Helper()
	consts := constStrings(t, typesPath)
	var names []string
	for name, v := range consts {
		if strings.HasSuffix(name, "Schema") {
			names = append(names, name)
			if !json.Valid([]byte(v)) {
				t.Errorf("%s is not valid JSON: %s", name, v)
			}
		}
	}
	if len(names) < 7 {
		t.Errorf("found only %v; want every descriptor schema", names)
	}
}

// constStrings reads a generated file's string constants, resolving
// one constant spliced into another.
func constStrings(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	var eval func(e ast.Expr) string
	eval = func(e ast.Expr) string {
		switch e := e.(type) {
		case *ast.BasicLit:
			s, err := strconv.Unquote(e.Value)
			if err != nil {
				t.Fatal(err)
			}
			return s
		case *ast.Ident:
			return out[e.Name]
		case *ast.BinaryExpr:
			return eval(e.X) + eval(e.Y)
		case *ast.ParenExpr:
			return eval(e.X)
		}
		t.Fatalf("constant expression %T is not a string", e)
		return ""
	}
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.CONST {
			continue
		}
		for _, sp := range g.Specs {
			vs := sp.(*ast.ValueSpec)
			for i, n := range vs.Names {
				out[n.Name] = eval(vs.Values[i])
			}
		}
	}
	return out
}

// TestEmitListOnlyMoney pins a money column the form does not carry:
// the result still shows it, so moneyOf and moneyOutSchema are emitted,
// while the input helpers, which only a form money field needs, are not.
func TestEmitListOnlyMoney(t *testing.T) {
	dir := emitTo(t, valid(t, rastrillo.Resource{
		Name:     "fees",
		Route:    "/fees",
		Currency: "EUR",
		List:     rastrillo.List{Columns: []rastrillo.Column{{Field: "Title"}, {Field: "Amount", Kind: rastrillo.Money}}},
		Form:     rastrillo.Form{Basics: []rastrillo.Field{{Name: "Title", Required: true}}},
	}))
	got, err := os.ReadFile(filepath.Join(dir, "types.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(got)
	if !strings.Contains(src, "func moneyOf(") {
		t.Error("types.go uses but does not declare moneyOf")
	}
	if _, ok := constStrings(t, filepath.Join(dir, "types.go"))["moneyOutSchema"]; !ok {
		t.Error("types.go uses but does not declare moneyOutSchema")
	}
	for _, unwanted := range []string{"func checkMoney(", "func settle(", "moneyInSchema", "\"amadan.net/rastrillo/rastrillo/currency\""} {
		if strings.Contains(src, unwanted) {
			t.Errorf("types.go carries %q, which only a form money field needs and staticcheck would flag", unwanted)
		}
	}
	wantValidSchemas(t, filepath.Join(dir, "types.go"))
}

// TestFixedNamesMatchTheEmitter pins opsnames.Fixed, the list
// Resource.Validate refuses a resource's singular against, to what the
// emitter declares: every top-level name in gen/ops that is not the
// resource's own type, across the shapes that emit different helpers.
func TestFixedNamesMatchTheEmitter(t *testing.T) {
	declared := map[string]bool{}
	listResultMembers := map[string]bool{}
	resultMembers := map[string]bool{}
	for _, c := range []struct {
		r        rastrillo.Resource
		singular string
	}{
		{bookmarks(t), "Bookmark"},
		{ticketTypes(t), "TicketType"},
		{announcements(t), "Announcement"},
		{prices(t), "Price"},
	} {
		dir := emitTo(t, c.r)
		for _, f := range []string{"types", "list", "get", "create", "update", "delete"} {
			parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, f+".go"), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			if f == "types" {
				// The members that are not the resource's own: the
				// plural in ListResult, the columns in the result type.
				for _, m := range structMembers(parsed, "ListResult") {
					if m != shape.PascalCase(c.r.Name) {
						listResultMembers[m] = true
					}
				}
				cols := map[string]bool{}
				for _, col := range shape.Columns(c.r) {
					cols[col.Name] = true
				}
				for _, m := range structMembers(parsed, c.singular) {
					if !cols[m] {
						resultMembers[m] = true
					}
				}
				// ListInput and ListArgs hold one Filter<F> per manifest
				// filter beside their fixed members. No rule refuses a
				// filter there because none is needed: a fixed member
				// that began with Filter could be shadowed by a filter
				// of that name, so this pins that none does. This is a
				// property that already held, not a change.
				for _, typ := range []string{"ListInput", "ListArgs"} {
					for _, m := range structMembers(parsed, typ) {
						if strings.HasPrefix(m, "Filter") && !isFilter(c.r, m) {
							t.Errorf("%s.%s is a fixed member a filter called %s would shadow", typ, m, strings.TrimPrefix(m, "Filter"))
						}
					}
				}
			}
			for _, d := range parsed.Decls {
				switch d := d.(type) {
				case *ast.FuncDecl:
					if d.Recv == nil {
						declared[d.Name.Name] = true
					}
				case *ast.GenDecl:
					for _, sp := range d.Specs {
						switch sp := sp.(type) {
						case *ast.TypeSpec:
							declared[sp.Name.Name] = true
						case *ast.ValueSpec:
							for _, n := range sp.Names {
								declared[n.Name] = true
							}
						}
					}
				}
			}
		}
		delete(declared, c.singular)
	}
	wantSameNames(t, "opsnames.Fixed", declared, opsnames.Fixed)
	wantSameNames(t, "opsnames.ListResultMembers", listResultMembers, opsnames.ListResultMembers)
	wantSameNames(t, "opsnames.ResultMembers", resultMembers, opsnames.ResultMembers)
}

// isFilter reports whether member is Filter<F> for one of r's filters.
func isFilter(r rastrillo.Resource, member string) bool {
	for _, fv := range shape.FilterVars(r) {
		if fv.Param == member {
			return true
		}
	}
	return false
}

// structMembers is the field names of the struct type called name in
// the parsed file, embedded fields by their type.
func structMembers(f *ast.File, name string) []string {
	var out []string
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, sp := range g.Specs {
			ts, ok := sp.(*ast.TypeSpec)
			if !ok || ts.Name.Name != name {
				continue
			}
			for _, fld := range ts.Type.(*ast.StructType).Fields.List {
				for _, n := range fld.Names {
					out = append(out, n.Name)
				}
				if len(fld.Names) == 0 {
					out = append(out, types.ExprString(fld.Type))
				}
			}
		}
	}
	return out
}

func wantSameNames(t *testing.T, list string, got map[string]bool, want []string) {
	t.Helper()
	var names []string
	for name := range got {
		names = append(names, name)
	}
	slices.Sort(names)
	want = slices.Clone(want)
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Errorf("the emitter declares %v\n%s lists %v", names, list, want)
	}
}

// prices is the mergeable-with-optional-money shape, pricesTOML's.
func prices(t *testing.T) rastrillo.Resource {
	return valid(t, rastrillo.Resource{
		Name:     "prices",
		Route:    "/prices",
		Store:    rastrillo.Mergeable,
		Currency: "EUR",
		List:     rastrillo.List{Columns: []rastrillo.Column{{Field: "Title"}, {Field: "Amount", Kind: rastrillo.Money}}},
		Form:     rastrillo.Form{Basics: []rastrillo.Field{{Name: "Title", Required: true}, {Name: "Amount", Kind: rastrillo.Money}}},
	})
}
