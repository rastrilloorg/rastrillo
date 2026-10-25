package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
)

// mergeableFixtureResource is the running mergeable example — the same
// shape examples/tickets declares as manifest/announcements.toml: an
// unscoped "announcements" resource with a Title list column, search
// on, and Title/Body basics. Search + no filters + no scope means
// Count has exactly one bind parameter, pinning the bare-argument
// convention the exclusive path's sqlc output follows (see
// actionIndexGET's countBinds switch).
func mergeableFixtureResource() rastrillo.Resource {
	r := rastrillo.Resource{
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
	}
	if err := r.Validate(); err != nil {
		panic(err) // fixture must stay valid; a break here is a test bug
	}
	return r
}

// scopedMergeableFixtureResource is scopedFixtureResource's shape on
// the mergeable store: scope = "user" is just an owner field in the
// payloads plus the always-on owner filter (spec §6), so the scoped
// golden pins the owner clause, the owner-stamped Create, and the
// 404-not-403 Get/Update/Delete keying alongside both Update groups.
func scopedMergeableFixtureResource() rastrillo.Resource {
	r := rastrillo.Resource{
		Name:  "bookmarks",
		Route: "/bookmarks",
		Store: rastrillo.Mergeable,
		Scope: rastrillo.UserScoped,
		List: rastrillo.List{
			Columns: []rastrillo.Column{{Field: "Title"}, {Field: "Link"}},
			Search:  true,
		},
		Form: rastrillo.Form{
			Basics:   []rastrillo.Field{{Name: "Title", Required: true}, {Name: "Link"}},
			Advanced: []rastrillo.Field{{Name: "Notes", Kind: rastrillo.Textarea}},
		},
	}
	if err := r.Validate(); err != nil {
		panic(err) // fixture must stay valid; a break here is a test bug
	}
	return r
}

// pricesFixtureResource is a mergeable resource with money and a
// [legacy] table: the shape whose events carry amounts, so its store
// folds through replay's rules, writes the currency beside each amount
// and exports CheckReplay for Boot.
func pricesFixtureResource() rastrillo.Resource {
	scale := 2
	r := rastrillo.Resource{
		Name:     "prices",
		Route:    "/prices",
		Store:    rastrillo.Mergeable,
		Currency: "EUR",
		Legacy:   &rastrillo.Legacy{Currency: "USD", Scale: &scale},
		List: rastrillo.List{
			Columns: []rastrillo.Column{{Field: "Title"}, {Field: "Amount", Kind: rastrillo.Money}},
		},
		Form: rastrillo.Form{
			Basics: []rastrillo.Field{{Name: "Title", Required: true}, {Name: "Amount", Kind: rastrillo.Money}},
		},
	}
	if err := r.Validate(); err != nil {
		panic(err) // fixture must stay valid; a break here is a test bug
	}
	return r
}

func TestEmitStoreMergeableMoneyGoldenFile(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := EmitStore(dir, []rastrillo.Resource{pricesFixtureResource()}, nil); err != nil {
		t.Fatalf("EmitStore: %v", err)
	}
	path := filepath.Join(dir, "store", "prices", "store.go")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != readMergeableGolden(t, "prices_store.go.golden") {
		t.Errorf("%s differs from testdata/mergeable/prices_store.go.golden:\ngot:\n%s", path, got)
	}
}

// readMergeableGolden reads one golden from testdata/mergeable/ — the
// mergeable store's generated files carry backquoted struct tags, so
// unlike the exclusive goldens they live as files, not raw-string
// consts.
func readMergeableGolden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "mergeable", name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return string(b)
}

func TestEmitStoreMergeableGoldenFiles(t *testing.T) {
	dir := t.TempDir()
	r := mergeableFixtureResource()

	paths, _, err := EmitStore(dir, []rastrillo.Resource{r}, nil)
	if err != nil {
		t.Fatalf("EmitStore: %v", err)
	}

	// A module of only-mergeable resources needs no sqlc tool at all —
	// sqlc.yaml is not written then (spec §2).
	if _, err := os.Stat(filepath.Join(dir, "store", "sqlc.yaml")); !os.IsNotExist(err) {
		t.Errorf("sqlc.yaml written for a mergeable-only module (stat err = %v)", err)
	}

	want := []string{
		filepath.Join(dir, "store", "announcements", "store.go"),
		filepath.Join(dir, "store", "announcements", "migrations.go"),
	}
	if len(paths) != len(want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Errorf("paths[%d] = %q, want %q", i, paths[i], want[i])
		}
	}

	cases := []struct {
		path   string
		golden string
	}{
		{filepath.Join(dir, "store", "announcements", "store.go"), "announcements_store.go.golden"},
		{filepath.Join(dir, "store", "announcements", "migrations.go"), "announcements_migrations.go.golden"},
	}
	for _, c := range cases {
		got, err := os.ReadFile(c.path)
		if err != nil {
			t.Fatalf("read %s: %v", c.path, err)
		}
		if string(got) != readMergeableGolden(t, c.golden) {
			t.Errorf("%s differs from testdata/mergeable/%s:\ngot:\n%s", c.path, c.golden, got)
		}
	}
}

func TestEmitStoreScopedMergeableGoldenFile(t *testing.T) {
	dir := t.TempDir()
	r := scopedMergeableFixtureResource()

	if _, _, err := EmitStore(dir, []rastrillo.Resource{r}, nil); err != nil {
		t.Fatalf("EmitStore: %v", err)
	}

	path := filepath.Join(dir, "store", "bookmarks", "store.go")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != readMergeableGolden(t, "bookmarks_store.go.golden") {
		t.Errorf("%s differs from testdata/mergeable/bookmarks_store.go.golden:\ngot:\n%s", path, got)
	}
}

// TestEmitStoreMergeableFilterBinds pins the declared-filter plumbing
// no full golden covers: a mergeable resource with a Values filter
// gets the same Filter<Field> params and the same
// empty-value-disables-it equality clause the exclusive whereClauses
// emit.
func TestEmitStoreMergeableFilterBinds(t *testing.T) {
	dir := t.TempDir()
	r := filteredFixtureResource() // events: search + Status filter
	r.Store = rastrillo.Mergeable
	if err := r.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if _, _, err := EmitStore(dir, []rastrillo.Resource{r}, nil); err != nil {
		t.Fatalf("EmitStore: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "store", "events", "store.go"))
	if err != nil {
		t.Fatalf("read store.go: %v", err)
	}
	src := string(got)
	for _, wantFragment := range []string{
		"FilterStatus string", // List and Count Params field, sqlc's own spelling
		"func matches(n Event, search, filterStatus string) bool", // whereClauses order: search, then filters
		"if filterStatus != \"\" && n.Status != filterStatus {",   // empty disables, exact match otherwise
		"matches(n, arg.Search, arg.FilterStatus)",
	} {
		if !strings.Contains(src, wantFragment) {
			t.Errorf("store.go missing %q:\n%s", wantFragment, src)
		}
	}
}

// TestEmitStoreMixedStores: sqlc.yaml covers ONLY the exclusive
// resources (byte-identical to a module that never declared the
// mergeable one), while each store kind gets its own files.
func TestEmitStoreMixedStores(t *testing.T) {
	dir := t.TempDir()
	rs := []rastrillo.Resource{mergeableFixtureResource(), fixtureResource()}

	if _, _, err := EmitStore(dir, rs, nil); err != nil {
		t.Fatalf("EmitStore: %v", err)
	}

	yaml, err := os.ReadFile(filepath.Join(dir, "store", "sqlc.yaml"))
	if err != nil {
		t.Fatalf("read sqlc.yaml: %v", err)
	}
	if string(yaml) != goldenSqlcYAML {
		t.Errorf("sqlc.yaml:\ngot:\n%s\nwant (exclusive resources only):\n%s", yaml, goldenSqlcYAML)
	}
	for _, f := range []string{
		filepath.Join(dir, "store", "notes", "schema.sql"),
		filepath.Join(dir, "store", "notes", "queries.sql"),
		filepath.Join(dir, "store", "notes", "migrations.go"),
		filepath.Join(dir, "store", "announcements", "store.go"),
		filepath.Join(dir, "store", "announcements", "migrations.go"),
	} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("expected %s to exist: %v", f, err)
		}
	}
	for _, f := range []string{
		filepath.Join(dir, "store", "announcements", "schema.sql"),
		filepath.Join(dir, "store", "announcements", "queries.sql"),
	} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("%s written for a mergeable resource (stat err = %v)", f, err)
		}
	}
}

func TestEmitStoreMergeableIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	r := mergeableFixtureResource()

	paths, _, err := EmitStore(dir, []rastrillo.Resource{r}, nil)
	if err != nil {
		t.Fatalf("EmitStore (1st): %v", err)
	}
	first := readAllFiles(t, paths)

	if _, _, err := EmitStore(dir, []rastrillo.Resource{r}, nil); err != nil {
		t.Fatalf("EmitStore (2nd): %v", err)
	}
	second := readAllFiles(t, paths)

	for _, p := range paths {
		if first[p] != second[p] {
			t.Errorf("%s changed between runs", p)
		}
	}
}

// scopedMergeableBookmarksTOML is examples/notes' bookmarks on the
// mergeable store: the shape whose concurrent creates used to share an
// id and fold into one record with the later owner.
const scopedMergeableBookmarksTOML = `name  = "bookmarks"
route = "/bookmarks"
store = "mergeable"
scope = "user"

[list]
columns = [{ field = "Title" }, { field = "Link" }]
search  = true

[form]
basics   = [{ name = "Title", required = true }, { name = "Link" }]
advanced = [{ name = "Notes", kind = "textarea" }]
`

// TestMergeableStoreWritesRuntime runs the generated store's new writes
// in a scratch app: concurrent creates by two owners, a partial update
// carrying only its fields and its actor, missing and foreign records,
// and keyset pages across a deleted cursor row.
func TestMergeableStoreWritesRuntime(t *testing.T) {
	app := scratchApp(t, map[string]string{"manifest/bookmarks.toml": scopedMergeableBookmarksTOML})
	mustGenerate(t, app)
	writeFile(t, app, "gen/store/bookmarks/writes_test.go", mergeableWritesTest)
	if out, err := goTest(app, "./gen/store/bookmarks"); err != nil {
		t.Fatalf("the generated store's writes:\n%s", out)
	}
}

const mergeableWritesTest = `package bookmarksstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/eventlog"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	var migrations []string
	for _, m := range eventlog.Schema.All() {
		migrations = append(migrations, m.SQL)
	}
	db, err := rastrillo.OpenDB(filepath.Join(t.TempDir(), "app.db"), migrations)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestConcurrentCreatesKeepTheirOwners(t *testing.T) {
	q := New(testDB(t))
	ctx := context.Background()
	owners := []string{"a", "b"}
	ids := make([]int64, len(owners))
	errs := make([]error, len(owners))
	var wg sync.WaitGroup
	for i, owner := range owners {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids[i], errs[i] = q.CreateBookmark(ctx, "user:"+owner, CreateBookmarkParams{Owner: owner, Title: "t-" + owner, Now: "2026-10-05T18:00:00Z"})
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if ids[0] == ids[1] {
		t.Fatalf("both creates got id %d", ids[0])
	}
	for i, owner := range owners {
		row, err := q.GetBookmark(ctx, GetBookmarkParams{ID: ids[i], Owner: owner})
		if err != nil || row.Owner != owner || row.Title != "t-"+owner {
			t.Fatalf("record %d = %+v, %v; want %s's", ids[i], row, err, owner)
		}
	}
}

// The largest id has no next: plus one wrapped to the smallest, which
// never beat the largest, so every later create took the same wrapped
// id and joined one record. Create refuses instead, and appends
// nothing. (Carried from the branch's eventlog.AppendNew when main's
// AppendUnder replaced it; the check lives in the generated nextID.)
func TestCreateRefusesAnExhaustedID(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	log, err := eventlog.Open(db, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := log.Append(ctx, StreamPrefix+"9223372036854775807", "created", "app", nil); err != nil {
		t.Fatal(err)
	}
	q := New(db)
	for i := 0; i < 3; i++ {
		if id, err := q.CreateBookmark(ctx, "user:a", CreateBookmarkParams{Owner: "a", Title: "t", Now: "2026-10-05T18:00:00Z"}); err == nil {
			t.Errorf("attempt %d: CreateBookmark = %d; want an error, no id is left", i, id)
		}
	}
	events, err := log.EventsByPrefix(ctx, StreamPrefix)
	if err != nil || len(events) != 1 {
		t.Fatalf("events under %s = %d, %v; want only the existing one", StreamPrefix, len(events), err)
	}
}

func TestUpdateCarriesOnlyTheSentFieldsAndTheActor(t *testing.T) {
	q := New(testDB(t))
	ctx := context.Background()
	id, err := q.CreateBookmark(ctx, "user:a", CreateBookmarkParams{Owner: "a", Title: "Old", Link: "https://x.test", Notes: "n", Now: "2026-10-05T18:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	title := "New"
	agent := "agent:Claude for user:a token:k3Jx9QpL2mWa"
	n, err := q.UpdateBookmark(ctx, agent, UpdateBookmarkParams{Title: &title, Now: "2026-10-05T19:00:00Z", ID: id, Owner: "a"})
	if err != nil || n != 1 {
		t.Fatalf("UpdateBookmark = %d, %v; want 1", n, err)
	}
	row, err := q.GetBookmark(ctx, GetBookmarkParams{ID: id, Owner: "a"})
	if err != nil || row.Title != "New" || row.Link != "https://x.test" || row.Notes != "n" || row.UpdatedAt != "2026-10-05T19:00:00Z" {
		t.Fatalf("after a title-only update: %+v, %v", row, err)
	}
	log, _, err := q.open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	events, err := log.Events(ctx, StreamPrefix+fmt.Sprint(id))
	if err != nil || len(events) != 2 {
		t.Fatalf("events = %d, %v; want 2", len(events), err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(events[1].Payload, &keys); err != nil {
		t.Fatal(err)
	}
	for k := range keys {
		if k != "V" && k != "title" && k != "updated_at" {
			t.Errorf("the update event carries %q; want only V, title and updated_at", k)
		}
	}
	if events[0].Actor != "user:a" || events[1].Actor != agent {
		t.Errorf("actors = %q, %q", events[0].Actor, events[1].Actor)
	}
}

func TestMissingForeignAndDeletedRecordsAreZero(t *testing.T) {
	q := New(testDB(t))
	ctx := context.Background()
	id, err := q.CreateBookmark(ctx, "user:a", CreateBookmarkParams{Owner: "a", Title: "Mine", Now: "2026-10-05T18:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	title := "Theirs now"
	for _, c := range []struct {
		name  string
		id    int64
		owner string
	}{{"missing", 999, "a"}, {"foreign", id, "b"}} {
		if n, err := q.UpdateBookmark(ctx, "user:"+c.owner, UpdateBookmarkParams{Title: &title, Now: "x", ID: c.id, Owner: c.owner}); err != nil || n != 0 {
			t.Errorf("%s update = %d, %v; want 0", c.name, n, err)
		}
		if n, err := q.DeleteBookmark(ctx, "user:"+c.owner, DeleteBookmarkParams{ID: c.id, Owner: c.owner}); err != nil || n != 0 {
			t.Errorf("%s delete = %d, %v; want 0", c.name, n, err)
		}
	}
	if n, err := q.DeleteBookmark(ctx, "user:a", DeleteBookmarkParams{ID: id, Owner: "a"}); err != nil || n != 1 {
		t.Fatalf("delete = %d, %v; want 1", n, err)
	}
	if n, err := q.UpdateBookmark(ctx, "user:a", UpdateBookmarkParams{Title: &title, Now: "x", ID: id, Owner: "a"}); err != nil || n != 0 {
		t.Errorf("update after delete = %d, %v; want 0", n, err)
	}
	log, _, err := q.open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	events, err := log.Events(ctx, StreamPrefix+fmt.Sprint(id))
	if err != nil || len(events) != 2 {
		t.Errorf("stream has %d events, %v; want created and deleted only", len(events), err)
	}
}

func TestListAfterPagesWithoutGapsOrRepeats(t *testing.T) {
	q := New(testDB(t))
	ctx := context.Background()
	// Two rows share a created_at, so the id has to break the tie.
	for i, at := range []string{"2026-10-01T00:00:00Z", "2026-10-02T00:00:00Z", "2026-10-02T00:00:00Z", "2026-10-03T00:00:00Z", "2026-10-04T00:00:00Z"} {
		if _, err := q.CreateBookmark(ctx, "user:a", CreateBookmarkParams{Owner: "a", Title: fmt.Sprint("t", i), Now: at}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := q.CreateBookmark(ctx, "user:b", CreateBookmarkParams{Owner: "b", Title: "b's", Now: "2026-10-05T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	all, err := q.ListBookmarks(ctx, ListBookmarksParams{Owner: "a", PageLimit: 100})
	if err != nil || len(all) != 5 {
		t.Fatalf("ListBookmarks = %d rows, %v", len(all), err)
	}
	var got []int64
	after, afterID := "", int64(0)
	for {
		page, err := q.ListBookmarksAfter(ctx, ListBookmarksAfterParams{Owner: "a", AfterCreatedAt: after, AfterID: afterID, PageLimit: 2})
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		for _, r := range page {
			got = append(got, r.ID)
		}
		last := page[len(page)-1]
		after, afterID = last.CreatedAt, last.ID
		if len(got) == 2 {
			// The cursor row goes between pages: the cursor carries its
			// own values, so the next page still starts after it.
			if n, err := q.DeleteBookmark(ctx, "user:a", DeleteBookmarkParams{ID: last.ID, Owner: "a"}); err != nil || n != 1 {
				t.Fatalf("delete the cursor row = %d, %v", n, err)
			}
		}
	}
	var want []int64
	for _, r := range all {
		want = append(want, r.ID)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("paged ids = %v; want List's order %v", got, want)
	}
}
`
