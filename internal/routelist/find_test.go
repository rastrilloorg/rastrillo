package routelist

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// fixture is a module under testdata. Each one replaces this
// repository, so it builds against the action package being tested.
func fixture(name string) string {
	return filepath.Join("testdata", name)
}

func wantError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error, want:\n%s", want)
	}
	if err.Error() != want {
		t.Errorf("the error is:\n%s\nwant:\n%s", err, want)
	}
}

func noneUnder(name string) string {
	internal, _ := filepath.Abs(filepath.Join(fixture(name), "internal"))
	return fmt.Sprintf(noRoutesFunction, internal)
}

func TestFindIgnoresTestFilesAndOtherPlatforms(t *testing.T) {
	t.Setenv("GOFLAGS", "")
	_, err := Find(t.Context(), fixture("testonly"), "")
	wantError(t, err, noneUnder("testonly"))

	// The Plan 9 file has a Routes of the wrong shape: parsed, it would
	// be explained instead of reported missing.
	_, err = Find(t.Context(), fixture("tagged"), "")
	wantError(t, err, noneUnder("tagged"))

	t.Setenv("GOFLAGS", "-tags=routelist_extra")
	target, err := Find(t.Context(), fixture("tagged"), "")
	if err != nil {
		t.Fatalf("with the tag set, Find failed: %v", err)
	}
	if target.ImportPath != "example.com/tagged/internal/app" {
		t.Errorf("Find chose %s", target.ImportPath)
	}
	routes, err := List(t.Context(), target)
	if err != nil || len(routes) != 0 {
		t.Errorf("the tagged Routes listed %v, %v; want none, "+
			"built with the same tag", routes, err)
	}
}

func TestFindSaysWhatToWriteWhenThereIsNone(t *testing.T) {
	_, err := Find(t.Context(), fixture("none"), "")
	wantError(t, err, noneUnder("none"))
	if !strings.Contains(err.Error(), signatureToWrite) {
		t.Errorf("the error does not show the signature to write")
	}
}

func TestFindSaysWhereToRunItWithoutAnInternalDirectory(t *testing.T) {
	sibling, _ := filepath.Abs(fixture("sibling"))
	_, err := Find(t.Context(), sibling, "")
	wantError(t, err, fmt.Sprintf(noInternalDir, sibling))
}

func TestFindExplainsAWrongSignature(t *testing.T) {
	_, err := Find(t.Context(), fixture("wrongshape"), "")
	wantError(t, err, fmt.Sprintf(wrongShape,
		"example.com/wrongshape/internal/app",
		"func Routes() http.Handler"))
}

func TestFindAsksWhichWhenThereAreTwo(t *testing.T) {
	_, err := Find(t.Context(), fixture("two"), "")
	wantError(t, err, fmt.Sprintf(severalRoutesFunctions,
		"\texample.com/two/internal/first\n"+
			"\texample.com/two/internal/second\n",
		"example.com/two/internal/first"))

	target, err := Find(t.Context(), fixture("two"),
		"example.com/two/internal/second")
	if err != nil {
		t.Fatalf("Find with --package failed: %v", err)
	}
	module, _ := filepath.Abs(fixture("two"))
	want := Target{
		ModuleDir:  module,
		ImportPath: "example.com/two/internal/second",
		AppType:    "App",
	}
	if target != want {
		t.Errorf("Find chose %+v, want %+v", target, want)
	}

	_, err = Find(t.Context(), fixture("two"), "example.com/two/internal/third")
	wantError(t, err, fmt.Sprintf(chosenHasNone,
		"example.com/two/internal/third"))
}
