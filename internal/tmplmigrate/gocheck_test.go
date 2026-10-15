package tmplmigrate

import (
	"strings"
	"testing"
)

// The Go preflight, type-checked: the examples' own T bindings, and a
// fixture module covering every case the review named. tickets made
// the move to ui.Pages first (Task 23); blog and notes followed
// (Task 24), so every example now reaches no findings at all — none
// binds its catalog through ui.WithT any more.
func TestCheckGo(t *testing.T) {
	for _, ex := range []string{"blog", "notes", "tickets"} {
		if found, err := CheckGo("../../examples/" + ex); err != nil {
			t.Fatal(err)
		} else if len(found) != 0 {
			t.Errorf("%s: %+v, want no findings: it renders through ui.Pages now", ex, found)
		}
	}
	found, err := CheckGo("testdata/gocheckmod")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range found {
		line := f.Pos + " " + f.What
		if f.Review {
			line += " [review]"
		}
		got = append(got, line)
	}
	want := []string{
		`alias/alias.go:6 ui.WithT (a per-request T binding)`,                                  // an aliased import
		`alias/alias.go:7 ui.FuncsWith (a per-request T binding)`,                              //
		`alias/alias.go:9 function map assignment "Tbdi"`,                                      // a FuncMap from ui.Funcs
		`app/app.go:17 function map entry "T"`,                                                 // literal keys
		`app/app.go:18 function map entry "money"`,                                             //
		`app/app.go:23 function map entry "T"`,                                                 // a constant
		`app/app.go:24 function map entry "money"`,                                             // constants built from constants
		`app/app.go:26 function map entry "Tf"`,                                                // a conversion of a constant
		`app/app.go:27 function map entry key strings.ToUpper("t") is not a constant [review]`, // a call: Astra's first case
		`app/app.go:30 function map entry "T"`,                                                 // another package's constant
		`app/app.go:38 function map assignment "Tbdi"`,                                         // a helper's FuncMap parameter
		`app/app.go:43 function map assignment "displayValue"`,                                 // a map[string]any reaching Funcs through a helper
		`app/app.go:44 function map assignment key keyFor() is not a constant [review]`,        // a function-computed key
		`app/app.go:48 function map assignment "Tf"`,                                           //
		`app/app.go:49 function map assignment "number"`,                                       //
		`app/app.go:50 function map assignment key pick("x") is not a constant [review]`,       // a single-argument call
		`app/app.go:53 function map entry key keyAsset is not a constant [review]`,             // a shadowed local: Astra's second case
		`app/app.go:56 function map assignment "dateWords"`,                                    // make(template.FuncMap)
		`app/app.go:59 function map entry "money"`,                                             // a map[string]any literal reaching Funcs
		`app/app.go:68 function map entry "T"`,                                                 // text/template's FuncMap
		`app/app.go:70 ui.WithT (a per-request T binding)`,                                     //
		`dot/dot.go:6 ui.WithT (a per-request T binding)`,                                      // a dot-import
		`dot/dot.go:7 ui.FuncsWith (a per-request T binding)`,                                  //
		// Not reported: comments and strings, an ordinary map, a
		// map[string]any that never reaches Funcs, another package named
		// ui, and a local variable named ui (useother/use.go).
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("CheckGo fixture:\n%s\n\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
