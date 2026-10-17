package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/manifest"
)

// A resource whose rows predate scope = "user" has them hidden from
// every list once it boots: an empty owner matches no session. The
// warning has to fire on the transition itself, read from what the
// last generate recorded, because the appended ALTER it used to key
// on misses two cases: a mergeable store has no statements, and an
// SQL resource unscoped and scoped again keeps its owner column.
func TestScopeNotesFireOnTheTransitionForBothStores(t *testing.T) {
	sql := func(scope rastrillo.ScopeKind) rastrillo.Resource {
		return rastrillo.Resource{Name: "ticket_types", Route: "/t", Scope: scope, Form: rastrillo.Form{Basics: []rastrillo.Field{{Name: "Name"}}}}
	}
	mergeable := func(scope rastrillo.ScopeKind) rastrillo.Resource {
		return rastrillo.Resource{Name: "notes", Route: "/n", Store: rastrillo.Mergeable, Scope: scope, Form: rastrillo.Form{Basics: []rastrillo.Field{{Name: "Title"}}}}
	}
	for name, c := range map[string]struct {
		previous, now []rastrillo.Resource
		want          []string
	}{
		"mergeable scoped": {
			[]rastrillo.Resource{mergeable(rastrillo.Unscoped)}, []rastrillo.Resource{mergeable(rastrillo.UserScoped)},
			[]string{fmt.Sprintf(scopedRecords, "notes")}},
		"sql scoped again after the column was kept": {
			// The column from the first scoping is still released, so
			// no ALTER is appended this time; the transition is real.
			[]rastrillo.Resource{sql(rastrillo.Unscoped)}, []rastrillo.Resource{sql(rastrillo.UserScoped)},
			[]string{fmt.Sprintf(scopedRows, "ticket_types")}},
		"second generate is quiet": {
			[]rastrillo.Resource{sql(rastrillo.UserScoped), mergeable(rastrillo.UserScoped)},
			[]rastrillo.Resource{sql(rastrillo.UserScoped), mergeable(rastrillo.UserScoped)},
			nil},
		"a new resource has no rows to hide": {
			nil, []rastrillo.Resource{sql(rastrillo.UserScoped), mergeable(rastrillo.UserScoped)},
			nil},
		"unscoping says nothing": {
			[]rastrillo.Resource{sql(rastrillo.UserScoped)}, []rastrillo.Resource{sql(rastrillo.Unscoped)},
			nil},
	} {
		if got := scopeNotes(c.previous, c.now); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: scopeNotes = %q, want %q", name, got, c.want)
		}
	}
}

// The previous generation is gen/manifest.json as the last run wrote
// it, read before this run overwrites it. No file is a first generate.
func TestPreviousGenerationReadsTheArtifact(t *testing.T) {
	genDir := t.TempDir()
	if rs, err := previousGeneration(genDir); err != nil || rs != nil {
		t.Fatalf("no artifact: %v, %v; want nil, nil", rs, err)
	}
	want := []rastrillo.Resource{{Name: "notes", Route: "/n", Store: rastrillo.Mergeable, Scope: rastrillo.UserScoped, Form: rastrillo.Form{Basics: []rastrillo.Field{{Name: "Title"}}}}}
	if err := os.WriteFile(filepath.Join(genDir, "manifest.json"), manifest.Artifact(want), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := previousGeneration(genDir)
	if err != nil || len(got) != 1 || got[0].Name != "notes" || got[0].Scope != rastrillo.UserScoped || got[0].Store != rastrillo.Mergeable {
		t.Errorf("previousGeneration = %+v, %v", got, err)
	}
}

// A generate that fails after the manifest was written used to record
// the new scope anyway: the CLI returned before printing the warning,
// and the repaired retry compared scoped with scoped and said nothing,
// so the rows were hidden for good. The artifact must be the last
// thing the pipeline writes, so a failed run leaves the old one and the
// retry still sees the transition. The failing step is EmitLocales,
// made to fail by a file sitting where its directory goes.
func TestScopeWarningSurvivesAFailedGenerate(t *testing.T) {
	root := newActionsAppRoot(t)
	genDir := filepath.Join(t.TempDir(), "gen")
	before := []rastrillo.Resource{ticketTypes(t, nil)}
	after := []rastrillo.Resource{ticketTypes(t, func(r *rastrillo.Resource) { r.Scope = rastrillo.UserScoped })}
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := manifest.Artifact(before)
	if err := os.WriteFile(filepath.Join(genDir, "manifest.json"), old, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(genDir, "locales"), []byte("in the way"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := History{"ticket_types": {released(t, "ticket_types_statement.sql")}}
	if _, _, _, err := emitPipeline(root, genDir, after, h, false); err == nil {
		t.Fatal("the blocked pipeline did not fail; the test proves nothing")
	}
	if got, _ := os.ReadFile(filepath.Join(genDir, "manifest.json")); string(got) != string(old) {
		t.Fatalf("a failed generate rewrote manifest.json:\n%s", got)
	}

	// The repair, then the retry: the transition is still there to see.
	if err := os.Remove(filepath.Join(genDir, "locales")); err != nil {
		t.Fatal(err)
	}
	previous, err := previousGeneration(genDir)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := scopeNotes(previous, after), []string{fmt.Sprintf(scopedRows, "ticket_types")}; !reflect.DeepEqual(got, want) {
		t.Errorf("the retry lost the warning: %q, want %q", got, want)
	}
	if _, _, _, err := emitPipeline(root, genDir, after, h, false); err != nil {
		t.Fatal(err)
	}
	previous, err = previousGeneration(genDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := scopeNotes(previous, after); got != nil {
		t.Errorf("the run after a successful generate still warns: %q", got)
	}
}
