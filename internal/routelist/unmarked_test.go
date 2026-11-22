package routelist

import (
	"fmt"
	"reflect"
	"testing"

	"amadan.net/rastrillo/rastrillo/action"
)

func TestUnmarkedListsRoutesWithNeitherMark(t *testing.T) {
	routes := []action.Route{
		{Method: "GET", Path: "/a", Operations: []string{"x"}},
		{Method: "POST", Path: "/b"},
		{Method: "*", Path: "/api/*", Handler: "api"},
		{Method: "GET", Path: "/c", PageOnly: "r"},
	}
	want := []action.Route{{Method: "POST", Path: "/b"}}
	if got := Unmarked(routes); !reflect.DeepEqual(got, want) {
		t.Errorf("Unmarked gave %+v, want %+v", got, want)
	}
	report := FormatUnmarked(want)
	if wanted := fmt.Sprintf(unmarkedRoutes, "\tPOST /b\n"); report != wanted {
		t.Errorf("FormatUnmarked gave:\n%s\nwant:\n%s", report, wanted)
	}
}

func TestDeclaresOperationsFindsACallToDefine(t *testing.T) {
	for fixtureName, want := range map[string]bool{
		"declares": true, "app": false,
	} {
		target, _ := findIn(t, fixtureName, "")
		declares, err := DeclaresOperations(t.Context(), target)
		if err != nil {
			t.Fatalf("DeclaresOperations(%s) failed: %v", fixtureName, err)
		}
		if declares != want {
			t.Errorf("DeclaresOperations(%s) is %v, want %v",
				fixtureName, declares, want)
		}
	}
}

// A call counts only through the op package, however the file names
// it, so another package's Define or a mention of op.Define that is not
// a call does not make an app declare operations.
func TestDeclaresOperationsFollowsTheImportName(t *testing.T) {
	files := map[string]bool{
		"op":           true,
		"aliased":      true,
		"dotted":       true,
		"instantiated": true,
		"another":      false,
		"uncalled":     false,
		"shadowed":     false,
	}
	for name, want := range files {
		file := parsed(t, declaringSources[name])[0]
		if got := file.callsDefine(); got != want {
			t.Errorf("the %s file calls Define: %v, want %v", name, got,
				want)
		}
	}
}

const opImport = `"amadan.net/rastrillo/rastrillo/op"`

var declaringSources = map[string]string{
	"op": `import ` + opImport + `
var create = op.Define("notes.create", op.Spec{}, run)`,
	"aliased": `import operation ` + opImport + `
var create = operation.Define("notes.create", operation.Spec{}, run)`,
	"dotted": `import . ` + opImport + `
var create = Define("notes.create", Spec{}, run)`,
	"instantiated": `import ` + opImport + `
var create = op.Define[In, Out]("notes.create", op.Spec{}, run)`,
	"another": `import op "example.com/other/op"
var create = op.Define("notes.create", op.Spec{}, run)`,
	"uncalled": `import ` + opImport + `
var define = op.Define[In, Out]`,
	"shadowed": `import ` + opImport + `
var create = Define("notes.create", op.Spec{}, run)`,
}
