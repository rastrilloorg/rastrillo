package audit

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"amadan.net/rastrillo/rastrillo/migrate"
)

// recorder keeps what a test reports so a test can inspect it instead
// of failing.
type recorder struct {
	testing.TB
	errors []string
}

func (rec *recorder) Errorf(format string, args ...any) {
	rec.errors = append(rec.errors, fmt.Sprintf(format, args...))
}

func (rec *recorder) Fatalf(format string, args ...any) {
	rec.TB.Fatalf(format, args...)
}

func (rec *recorder) Helper() {}

func setOf(t *testing.T, sql string, decls ...migrate.Data) *migrate.Set {
	t.Helper()
	set, err := migrate.FromFS(fstest.MapFS{
		"migrations/0001_init.sql": {Data: []byte(sql)},
	}, "app")
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range decls {
		set.Declare(decl)
	}
	return set
}

func TestCheckSchemaReportsEachProblem(t *testing.T) {
	set := setOf(t, `CREATE TABLE notes (id INTEGER PRIMARY KEY);
CREATE TABLE tags (id INTEGER PRIMARY KEY);`)
	rec := &recorder{TB: t}
	CheckSchema(rec, set)
	if len(rec.errors) != 2 {
		t.Fatalf("errors = %q, want two", rec.errors)
	}
	for _, table := range []string{"notes", "tags"} {
		if !slices.ContainsFunc(rec.errors, func(msg string) bool {
			return strings.HasPrefix(msg, table+":")
		}) {
			t.Errorf("no error prefixed %q in %q", table+":", rec.errors)
		}
	}
}

func TestCheckSchemaCleanSetReportsNothing(t *testing.T) {
	set := setOf(t, `CREATE TABLE notes (id INTEGER PRIMARY KEY);`,
		migrate.Data{
			Name: "notes", Key: []string{"id"}, Kind: migrate.Shared,
			NotAudited: "test",
		})
	rec := &recorder{TB: t}
	CheckSchema(rec, set)
	if len(rec.errors) != 0 {
		t.Fatalf("errors = %q, want none", rec.errors)
	}
}

func TestExemptionsListsReasons(t *testing.T) {
	set := setOf(t, "",
		migrate.Data{
			Name: "pings", Key: []string{"id"}, Kind: migrate.Shared,
			NotAudited: "noise",
		},
		migrate.Data{
			Name: "invoices", Key: []string{"id"}, Kind: migrate.Personal,
			Erase: migrate.EraseKeep, KeepReason: "tax law",
		})
	want := []Exemption{
		{"invoices", "app", "kept on erasure", "tax law"},
		{"pings", "app", "not audited", "noise"},
	}
	if got := Exemptions(set); !reflect.DeepEqual(got, want) {
		t.Fatalf("Exemptions = %+v, want %+v", got, want)
	}
}

func TestExemptionsGivesATableWithBothTwoEntries(t *testing.T) {
	set := setOf(t, "", migrate.Data{
		Name: "logs", Key: []string{"id"}, Kind: migrate.Personal,
		NotAudited: "noise", Erase: migrate.EraseKeep, KeepReason: "law",
	})
	want := []Exemption{
		{"logs", "app", "kept on erasure", "law"},
		{"logs", "app", "not audited", "noise"},
	}
	if got := Exemptions(set); !reflect.DeepEqual(got, want) {
		t.Fatalf("Exemptions = %+v, want %+v", got, want)
	}
}
