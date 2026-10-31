package corpus

import (
	"bytes"
	"html/template"
	"testing"

	"amadan.net/rastrillo/rastrillo/tmpl"
)

// TestSequentialEntriesMatchAFreshSet runs the entries of the set
// sharedPartial compiles one after the other, in both orders, and wants
// each to write what html/template writes for it executed first on a
// fresh set: the contract for compiled code (the spec's section 3),
// which does not depend on what the set ran before. In shadow mode the
// comparison is tmpl's own, and must pass too.
func TestSequentialEntriesMatchAFreshSet(t *testing.T) {
	for _, shadow := range []string{"", "1"} {
		t.Setenv(tmpl.ShadowEnv, shadow)
		for _, order := range [][]string{{"p", "page"}, {"page", "p"}} {
			set := tmpl.Must(tmpl.New("page").Parse(sharedText))
			for _, entry := range order {
				if !set.IsCompiled(entry) {
					t.Fatalf(
						"%s has no compiled code; "+regenerate,
						entry,
					)
				}
				fresh := template.Must(template.New("page").Parse(sharedText))
				var got, want bytes.Buffer
				err := fresh.ExecuteTemplate(&want, entry, "<b>")
				if err != nil {
					t.Fatal(err)
				}
				if err := set.ExecuteTemplate(&got, entry, "<b>"); err != nil {
					t.Errorf("shadow %q, %v, %s: %v", shadow, order, entry, err)
					continue
				}
				if got.String() != want.String() {
					t.Errorf(
						"shadow %q, %v, %s: wrote %q, "+
							"a fresh html/template set writes %q",
						shadow,
						order,
						entry,
						got.String(),
						want.String(),
					)
				}
			}
		}
	}
}
