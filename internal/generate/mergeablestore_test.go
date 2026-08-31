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

	paths, err := EmitStore(dir, []rastrillo.Resource{r})
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

	if _, err := EmitStore(dir, []rastrillo.Resource{r}); err != nil {
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

	if _, err := EmitStore(dir, []rastrillo.Resource{r}); err != nil {
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

	if _, err := EmitStore(dir, rs); err != nil {
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

	paths, err := EmitStore(dir, []rastrillo.Resource{r})
	if err != nil {
		t.Fatalf("EmitStore (1st): %v", err)
	}
	first := readAllFiles(t, paths)

	if _, err := EmitStore(dir, []rastrillo.Resource{r}); err != nil {
		t.Fatalf("EmitStore (2nd): %v", err)
	}
	second := readAllFiles(t, paths)

	for _, p := range paths {
		if first[p] != second[p] {
			t.Errorf("%s changed between runs", p)
		}
	}
}
