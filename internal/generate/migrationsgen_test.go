package generate

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
)

// released reads a file copied from examples/tickets before money had
// a currency. They are copies, not the live files, so these tests keep
// what was released rather than what the generator writes now.
func released(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "released", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ticketTypes is examples/tickets' ticket_types resource as its
// manifest declares it now (currency and [legacy] added), with edit
// applied to its form.
func ticketTypes(t *testing.T, edit func(*rastrillo.Resource)) rastrillo.Resource {
	t.Helper()
	scale := 2
	r := rastrillo.Resource{
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
	}
	if edit != nil {
		edit(&r)
	}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestReleasedMigrationsReadsTheStringLiterals(t *testing.T) {
	got, err := releasedMigrations(filepath.Join("testdata", "released", "ticket_types_migrations.go.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{released(t, "ticket_types_statement.sql")}; !reflect.DeepEqual(got, want) {
		t.Errorf("releasedMigrations = %q, want %q", got, want)
	}
}

// Review round two, findings 2 and 5: a released statement is in an
// app's ledger under a checksum, so rewriting it would stop the app
// before Boot could upgrade anything. It is kept byte for byte, and
// money columns are left to Boot.
func TestAppendMigrations(t *testing.T) {
	stmt := released(t, "ticket_types_statement.sql")
	venue := func(r *rastrillo.Resource) {
		r.Form.Advanced = append(r.Form.Advanced, rastrillo.Field{Name: "Venue"})
	}
	deposit := func(r *rastrillo.Resource) {
		r.Form.Advanced = append(r.Form.Advanced, rastrillo.Field{Name: "Deposit", Kind: rastrillo.Money})
	}
	noMaxPerOrder := func(r *rastrillo.Resource) { r.Form.Advanced = nil }
	cases := []struct {
		name string
		edit func(*rastrillo.Resource)
		want []string
	}{
		{"the released manifest plus a currency", nil, []string{stmt}},
		{"a text field added", venue, []string{stmt, `ALTER TABLE ticket_types ADD COLUMN venue TEXT NOT NULL DEFAULT ''`}},
		{"a money field added", deposit, []string{stmt}},
		{"a field removed", noMaxPerOrder, []string{stmt}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := ticketTypes(t, c.edit)
			got, err := appendMigrations([]string{stmt}, r)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("appendMigrations = %q, want %q", got, c.want)
			}
			again, err := appendMigrations(got, r)
			if err != nil || !reflect.DeepEqual(again, got) {
				t.Errorf("a second run = %q, %v; want it unchanged", again, err)
			}
		})
	}
}

// Review carry from Task 17: no table may hold both a legacy <f> and
// <f>_minor/<f>_currency, or the upgrade cannot tell which is the
// amount. Appended statements therefore never name a money column, in
// any form.
func TestAppendMigrationsNeverAddsMoneyColumns(t *testing.T) {
	stmt := released(t, "ticket_types_statement.sql")
	r := ticketTypes(t, func(r *rastrillo.Resource) {
		r.Form.Advanced = append(r.Form.Advanced, rastrillo.Field{Name: "Venue"}, rastrillo.Field{Name: "Deposit", Kind: rastrillo.Money})
	})
	got, err := appendMigrations([]string{stmt}, r)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got[1:] {
		if strings.Contains(s, "price") || strings.Contains(s, "deposit") || strings.Contains(s, "_minor") || strings.Contains(s, "_currency") {
			t.Errorf("appended a money column: %q", s)
		}
	}
}

func TestAppendMigrationsFirstRelease(t *testing.T) {
	got, err := appendMigrations(nil, ticketTypes(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !strings.HasPrefix(got[0], "CREATE TABLE IF NOT EXISTS ticket_types (") ||
		!strings.Contains(got[0], "\n  price_minor INTEGER,\n  price_currency TEXT,\n") || strings.Contains(got[0], "  price INTEGER") {
		t.Errorf("first release = %q", got)
	}
}

// A scoped resource released before it was scoped gets its owner
// column and index appended, so the appended index has a column to
// index.
func TestAppendMigrationsScopedLater(t *testing.T) {
	stmt := released(t, "ticket_types_statement.sql")
	r := ticketTypes(t, func(r *rastrillo.Resource) { r.Scope = rastrillo.UserScoped })
	got, err := appendMigrations([]string{stmt}, r)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{stmt,
		`ALTER TABLE ticket_types ADD COLUMN owner TEXT NOT NULL DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS ticket_types_owner ON ticket_types (owner)`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("appendMigrations = %q, want %q", got, want)
	}
}

// generate --check emits into an empty temp dir, so the released
// statements must come in as an input: emitting from the History alone
// reproduces exactly the file generate left in the real tree.
func TestEmitPipelineFromHistoryMatchesTheRealTree(t *testing.T) {
	root := newActionsAppRoot(t)
	genDir := filepath.Join(t.TempDir(), "gen")
	h := History{"ticket_types": {released(t, "ticket_types_statement.sql")}}
	if _, _, _, err := emitPipeline(root, genDir, []rastrillo.Resource{ticketTypes(t, nil)}, h, false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(genDir, "store", "ticket_types", "migrations.go"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(repoRoot(t), "examples", "tickets", "gen", "store", "ticket_types", "migrations.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("migrations.go from the History:\n%s\nexamples/tickets has:\n%s", got, want)
	}
	if string(want) != released(t, "ticket_types_migrations.go.golden") {
		t.Errorf("examples/tickets' released migrations.go changed")
	}
}

func TestReadHistory(t *testing.T) {
	genDir := filepath.Join(t.TempDir(), "gen")
	writeFile(t, genDir, "store/ticket_types/migrations.go", released(t, "ticket_types_migrations.go.golden"))
	rs := []rastrillo.Resource{ticketTypes(t, nil), {Name: "fresh", Store: rastrillo.Exclusive}}
	h, err := ReadHistory(genDir, rs)
	if err != nil {
		t.Fatal(err)
	}
	if want := (History{"ticket_types": {released(t, "ticket_types_statement.sql")}}); !reflect.DeepEqual(h, want) {
		t.Errorf("ReadHistory = %q, want %q", h, want)
	}
}
