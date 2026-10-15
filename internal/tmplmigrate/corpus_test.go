package tmplmigrate

import (
	"os"
	"testing"
)

// The repo's own templates, live. Each migration task lowers these
// counts; once every corpus is migrated they are all zero, and the
// test then guards against a new template written in the old syntax.
// The generated templates under examples/*/gen are counted here too
// (the command skips gen/, the codemod does not); the generator emits
// them migrated, so the counts are the hand-written templates alone.
func TestRepoTemplates(t *testing.T) {
	want := map[string]Stats{
		// Migrated: a partial or layout written in the old
		// syntax would break every app that renders it with a localizer,
		// so any nonzero count here is a regression, not pending work.
		"ui/partials": {},
		"ui/layouts":  {},
		"examples":    {},
	}
	for root, w := range want {
		out, got, err := RewriteFS(os.DirFS("../.."), root)
		if err != nil {
			t.Fatalf("%s: %v", root, err)
		}
		if got.Calls != w.Calls || got.Dicts != w.Dicts || got.Wrapped != w.Wrapped || got.Preludes != w.Preludes || len(got.NoData) != 0 {
			t.Errorf("%s: calls %d dicts %d wrapped %d preludes %d no-data %d; want %+v",
				root, got.Calls, got.Dicts, got.Wrapped, got.Preludes, len(got.NoData), w)
		}
		for path, f := range out {
			again, st, err := Rewrite(path, string(f.Data))
			if err != nil || again != string(f.Data) || st.Calls+st.Dicts+st.Wrapped+st.Preludes != 0 {
				t.Errorf("%s: a second run changed it (%v)", path, err)
			}
		}
	}
}
