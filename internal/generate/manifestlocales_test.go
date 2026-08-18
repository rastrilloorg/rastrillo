package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/carlosframework/rastrillo"
	"github.com/carlosframework/rastrillo/internal/catalog"
)

// maxPerOrderFixture exercises titleCase's multi-hump case
// ("MaxPerOrder" -> "Max per order") — fixtureResource's own fields
// (Title, Price, Body) are all single words, which would not catch a
// titleCase regression on multi-word identifiers.
func maxPerOrderFixture() rastrillo.Resource {
	r := rastrillo.Resource{
		Name:  "widgets",
		Route: "/admin/widgets",
		List: rastrillo.List{
			Columns: []rastrillo.Column{{Field: "MaxPerOrder"}},
		},
	}
	if err := r.Validate(); err != nil {
		panic(err)
	}
	return r
}

func TestTitleCase(t *testing.T) {
	cases := []struct{ in, want string }{
		{"MaxPerOrder", "Max per order"},
		{"notes", "Notes"},
		{"ticket_types", "Ticket types"},
		{"Title", "Title"},
		{"ID", "Id"},
	}
	for _, tc := range cases {
		if got := titleCase(tc.in); got != tc.want {
			t.Errorf("titleCase(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestEmitLocalesKeySetMatchesTemplates pins the binding contract:
// EmitLocales' emitted key set must exactly cover every `(T "...")`
// call templates.go makes for a resource, no more, no less — grepped
// by hand against templates.go (see manifestlocales.go's own doc
// comment for the derivation) rather than trusted by construction,
// since a drift here is exactly the kind of thing a missing-key bug in
// production would come from. The "declared filter" case covers
// filteredFixtureResource() ("events", Status: draft/live) — every
// SummaryKey/LabelKey the generated index.GET action can pass to
// list.html's (T .Filter.SummaryKey)/(T .LabelKey) calls
// (filter<Field>LabelKey in actions.go: "ui.all" for "", else
// resource.<name>.filter.<sql>.<value>) must be covered here too.
func TestEmitLocalesKeySetMatchesTemplates(t *testing.T) {
	cases := []struct {
		name string
		r    rastrillo.Resource
		want []string
	}{
		{
			name: "no declared filter",
			r:    fixtureResource(), // notes: List Title/Price, Basics Title/Body, Advanced Price
			want: []string{
				"resource.notes.delete.confirm",
				"resource.notes.delete.title",
				"resource.notes.empty.body",
				"resource.notes.empty.title",
				"resource.notes.field.body",
				"resource.notes.field.price",
				"resource.notes.field.title",
				"resource.notes.name",
				"ui.cancel",
				"ui.delete",
				"ui.edit",
				"ui.new",
				"ui.save",
				"ui.search",
			},
		},
		{
			name: "declared filter",
			r:    filteredFixtureResource(), // events: List Title/Status, Filters Status(draft,live)
			want: []string{
				"resource.events.delete.confirm",
				"resource.events.delete.title",
				"resource.events.empty.body",
				"resource.events.empty.title",
				"resource.events.field.status",
				"resource.events.field.title",
				"resource.events.filter.status.draft",
				"resource.events.filter.status.live",
				"resource.events.name",
				"ui.all",
				"ui.cancel",
				"ui.delete",
				"ui.edit",
				"ui.new",
				"ui.save",
				"ui.search",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := EmitLocales(dir, "en", []rastrillo.Resource{tc.r}); err != nil {
				t.Fatalf("EmitLocales: %v", err)
			}

			data, err := os.ReadFile(filepath.Join(dir, "locales", "en.toml"))
			if err != nil {
				t.Fatal(err)
			}
			m, err := catalog.Decode(data)
			if err != nil {
				t.Fatalf("decode emitted catalog: %v", err)
			}

			var got []string
			for k := range m {
				got = append(got, k)
			}
			sort.Strings(got)

			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("key set = %v,\nwant     %v", got, tc.want)
			}
		})
	}
}

func TestEmitLocalesUIKeyValues(t *testing.T) {
	dir := t.TempDir()
	if err := EmitLocales(dir, "en", nil); err != nil {
		t.Fatalf("EmitLocales: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "locales", "en.toml"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := catalog.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"ui.new":    "New",
		"ui.save":   "Save",
		"ui.search": "Search",
		"ui.cancel": "Cancel",
		"ui.edit":   "Edit",
		"ui.delete": "Delete",
	}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("m[%q] = %q, want %q", k, m[k], v)
		}
	}
	if len(m) != len(want) {
		t.Errorf("with no resources declared, only the ui.* keys should be emitted: got %v", m)
	}
}

// onSaleFilterFixture exercises titleCase's multi-hump case on a
// declared filter VALUE, not just a field name: "on_sale" -> "On sale"
// (the binding contract's own pinned example) — filteredFixtureResource's
// own values ("draft", "live") are single words, which would not catch
// a regression on a multi-word value's fallback.
func onSaleFilterFixture() rastrillo.Resource {
	r := rastrillo.Resource{
		Name:  "products",
		Route: "/admin/products",
		List: rastrillo.List{
			Columns: []rastrillo.Column{{Field: "Title"}, {Field: "Status"}},
			Filter:  []string{"Status"},
			Filters: []rastrillo.Filter{{Field: "Status", Values: []string{"on_sale", "sold_out"}}},
		},
		Form: rastrillo.Form{
			Basics: []rastrillo.Field{{Name: "Title"}, {Name: "Status"}},
		},
	}
	if err := r.Validate(); err != nil {
		panic(err)
	}
	return r
}

func TestEmitLocalesTitleCasesMultiWordFilterValues(t *testing.T) {
	dir := t.TempDir()
	if err := EmitLocales(dir, "en", []rastrillo.Resource{onSaleFilterFixture()}); err != nil {
		t.Fatalf("EmitLocales: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "locales", "en.toml"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := catalog.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := m["resource.products.filter.status.on_sale"], "On sale"; got != want {
		t.Errorf("filter value label = %q, want %q", got, want)
	}
	if got, want := m["resource.products.filter.status.sold_out"], "Sold out"; got != want {
		t.Errorf("filter value label = %q, want %q", got, want)
	}
	if got, want := m["ui.all"], "All"; got != want {
		t.Errorf(`m["ui.all"] = %q, want %q`, got, want)
	}
}

func TestEmitLocalesTitleCasesMultiWordFieldLabels(t *testing.T) {
	dir := t.TempDir()
	if err := EmitLocales(dir, "en", []rastrillo.Resource{maxPerOrderFixture()}); err != nil {
		t.Fatalf("EmitLocales: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "locales", "en.toml"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := catalog.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := m["resource.widgets.field.max_per_order"], "Max per order"; got != want {
		t.Errorf("field label = %q, want %q", got, want)
	}
	if got, want := m["resource.widgets.name"], "Widgets"; got != want {
		t.Errorf("resource name label = %q, want %q", got, want)
	}
}

// TestEmitLocalesGoVarMatchesTOML proves gen/locales/locales.go's
// BaseCatalog is built from the exact same map as locales/en.toml —
// the entire point of emitting both from one source (manifestlocales.go's
// own doc comment): they cannot drift because there is only one map.
func TestEmitLocalesGoVarMatchesTOML(t *testing.T) {
	dir := t.TempDir()
	r := fixtureResource()
	if err := EmitLocales(dir, "en", []rastrillo.Resource{r}); err != nil {
		t.Fatalf("EmitLocales: %v", err)
	}

	tomlData, err := os.ReadFile(filepath.Join(dir, "locales", "en.toml"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := catalog.Decode(tomlData)
	if err != nil {
		t.Fatal(err)
	}

	goData, err := os.ReadFile(filepath.Join(dir, "locales", "locales.go"))
	if err != nil {
		t.Fatal(err)
	}
	goSrc := string(goData)

	if !strings.Contains(goSrc, "package locales") {
		t.Errorf("locales.go missing `package locales`: %s", goSrc)
	}
	if !strings.Contains(goSrc, "var BaseCatalog = rastrillo.Catalog{") {
		t.Errorf("locales.go missing the BaseCatalog var literal: %s", goSrc)
	}
	// gofmt column-aligns composite-literal entries (variable spacing
	// after the colon), so match key/value as a pair rather than a
	// fixed-width literal string.
	for k, v := range m {
		pattern := regexp.QuoteMeta(fmt.Sprintf("%q:", k)) + `\s*` + regexp.QuoteMeta(fmt.Sprintf("%q,", v))
		if !regexp.MustCompile(pattern).MatchString(goSrc) {
			t.Errorf("locales.go missing entry %q: %q, (from locales/en.toml's %q = %q)", k, v, k, v)
		}
	}
}

func TestEmitLocalesIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	r := fixtureResource()
	if err := EmitLocales(dir, "en", []rastrillo.Resource{r}); err != nil {
		t.Fatalf("first EmitLocales: %v", err)
	}
	tomlPath := filepath.Join(dir, "locales", "en.toml")
	goPath := filepath.Join(dir, "locales", "locales.go")
	wantTOML, err := os.ReadFile(tomlPath)
	if err != nil {
		t.Fatal(err)
	}
	wantGo, err := os.ReadFile(goPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := EmitLocales(dir, "en", []rastrillo.Resource{r}); err != nil {
		t.Fatalf("second EmitLocales: %v", err)
	}
	gotTOML, err := os.ReadFile(tomlPath)
	if err != nil {
		t.Fatal(err)
	}
	gotGo, err := os.ReadFile(goPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotTOML) != string(wantTOML) {
		t.Errorf("second run changed locales/en.toml")
	}
	if string(gotGo) != string(wantGo) {
		t.Errorf("second run changed locales/locales.go")
	}
}
