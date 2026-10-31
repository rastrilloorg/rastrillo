package routelist

import (
	"bytes"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo/action"
)

const appPackage = "example.com/app/internal/app"

// findIn finds the Routes function in a fixture and gives the app's
// output to a buffer, so a test can read what the app printed.
func findIn(
	t *testing.T, name, chosen string,
) (Target, *bytes.Buffer) {
	t.Helper()
	target, err := Find(t.Context(), fixture(name), chosen)
	if err != nil {
		t.Fatalf("Find(%s) failed: %v", name, err)
	}
	printed := &bytes.Buffer{}
	target.Output = printed
	return target, printed
}

func appRoutes() []action.Route {
	return []action.Route{
		{Method: "GET", Path: "/", Handler: "index",
			Action: appPackage + ".(*App).index"},
		{Method: "GET", Path: "/admin", Handler: "adminHome",
			Action: appPackage + ".(*App).adminHome",
			Guards: []string{"admin"}},
		{Method: "POST", Path: "/admin/tags/{id}", Handler: "Show",
			Action: appPackage + ".(*Tags).Show",
			Guards: []string{"admin"}},
		{Method: "GET", Path: "/report", Handler: "Show",
			Action: appPackage + ".Report.Show"},
		{Method: "GET", Path: "/healthz", Handler: "tagEdit"},
		{Method: "*", Path: "/api/*", Handler: "api"},
	}
}

func TestListPrintsEveryRouteWithItsGuards(t *testing.T) {
	target, _ := findIn(t, "app", "")
	routes, err := List(t.Context(), target)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if !reflect.DeepEqual(routes, appRoutes()) {
		t.Errorf("List gave\n%+v\nwant\n%+v", routes, appRoutes())
	}
}

func TestListShowsWhatTheAppPrints(t *testing.T) {
	target, printed := findIn(t, "app", "")
	routes, err := List(t.Context(), target)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if !strings.Contains(printed.String(), `"Path":"/printed"`) {
		t.Errorf("what the app printed was not shown: %q", printed)
	}
	for _, route := range routes {
		if route.Path == "/printed" {
			t.Errorf("what the app printed was read as a route")
		}
	}
}

// snapshot is every file in a module and its bytes, so a test can
// tell that listing wrote, changed or removed nothing there.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(
		path string, entry fs.DirEntry, err error,
	) error {
		if err != nil || entry.IsDir() {
			return err
		}
		content, err := os.ReadFile(path)
		files[path] = string(content)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// emptyScratch makes the temporary directory one the test can watch,
// and checks at the end that listing left nothing in it.
func emptyScratch(t *testing.T) string {
	t.Helper()
	scratch := t.TempDir()
	t.Setenv("TMPDIR", scratch)
	t.Cleanup(func() {
		left, _ := os.ReadDir(scratch)
		if len(left) > 0 {
			t.Errorf("listing left %d files in TMPDIR, first %s",
				len(left), left[0].Name())
		}
	})
	return scratch
}

func TestListLeavesTheModuleUntouched(t *testing.T) {
	emptyScratch(t)
	for _, module := range []string{"app", "troubled"} {
		before := snapshot(t, fixture(module))
		t.Cleanup(func() {
			if !maps.Equal(snapshot(t, fixture(module)), before) {
				t.Errorf("listing changed the files of %s", module)
			}
		})
	}
	target, _ := findIn(t, "app", "")
	if _, err := List(t.Context(), target); err != nil {
		t.Errorf("List failed on the app: %v", err)
	}
	broken, _ := findIn(t, "troubled",
		"example.com/troubled/internal/badbuild")
	_, err := List(t.Context(), broken)
	if err == nil || !strings.Contains(err.Error(), "not a number") {
		t.Errorf("a failing build gave %v, want Go's error", err)
	}
	panics, printed := findIn(t, "troubled",
		"example.com/troubled/internal/panics")
	_, err = List(t.Context(), panics)
	wantError(t, err, fmt.Sprintf(runFailed, "exit status 2", "App"))
	if !strings.Contains(printed.String(), "registering") {
		t.Errorf("what the failing run printed was not shown: %q",
			printed)
	}
}

// The go command resolves ./.rastrillo-routes against its working
// directory, and the overlay names the module by go list's path. Run
// through a symlink from somewhere else, the two must still agree.
func TestListWorksThroughASymlinkFromElsewhere(t *testing.T) {
	repository, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(t.TempDir(), "repository")
	if err := os.Symlink(repository, linked); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	app := filepath.Join(linked, "internal", "routelist", "testdata", "app")
	target, err := Find(t.Context(), app, "")
	if err != nil {
		t.Fatalf("Find through the symlink failed: %v", err)
	}
	target.Output = &bytes.Buffer{}
	routes, err := List(t.Context(), target)
	if err != nil {
		t.Fatalf("List through the symlink failed: %v", err)
	}
	if !reflect.DeepEqual(routes, appRoutes()) {
		t.Errorf("List gave\n%+v\nwant\n%+v", routes, appRoutes())
	}
}

// copiedModule is a copy of a fixture outside the repository, its
// replace pointed back at this checkout, for a test that must add a
// file the checked-in fixture must not have.
func copiedModule(t *testing.T, name string) string {
	t.Helper()
	module := t.TempDir()
	if err := os.CopyFS(module, os.DirFS(fixture(name))); err != nil {
		t.Fatal(err)
	}
	repository, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	goMod := filepath.Join(module, "go.mod")
	text, err := os.ReadFile(goMod)
	if err != nil {
		t.Fatal(err)
	}
	text = []byte(strings.ReplaceAll(string(text), "../../../..",
		repository))
	if err := os.WriteFile(goMod, text, 0o600); err != nil {
		t.Fatal(err)
	}
	return module
}

// A real .rastrillo-routes directory would have its files compiled
// into the listing program and run, so List refuses before building.
func TestListRefusesAnAppWithItsOwnProgramDirectory(t *testing.T) {
	module := copiedModule(t, "two")
	own := filepath.Join(module, programDir)
	if err := os.Mkdir(own, 0o700); err != nil {
		t.Fatal(err)
	}
	ran := filepath.Join(module, "ran")
	source := "package main\n\nimport \"os\"\n\nfunc init() { " +
		"os.WriteFile(" + strconv.Quote(ran) + ", nil, 0o600) }\n"
	err := os.WriteFile(filepath.Join(own, "extra.go"), []byte(source),
		0o600)
	if err != nil {
		t.Fatal(err)
	}
	target, err := Find(t.Context(), module, "example.com/two/internal/first")
	if err != nil {
		t.Fatal(err)
	}
	target.Output = &bytes.Buffer{}
	_, err = List(t.Context(), target)
	if _, statErr := os.Stat(ran); statErr == nil {
		t.Errorf("the app's own %s was compiled and run", programDir)
	}
	wantError(t, err, say(reservedDirectory, own).Error())
}
