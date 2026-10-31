package routelist

import (
	"bytes"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// writeFile writes text at path, making the directories it needs.
func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// firstOfTwo is the Routes in the "two" fixture's first package, so a
// test can call List and Unmounted without Find.
func firstOfTwo(module string) Target {
	return Target{
		ModuleDir:  module,
		ImportPath: "example.com/two/internal/first",
		AppType:    "App",
		Output:     &bytes.Buffer{},
	}
}

// Go may write go.work.sum in workspace mode even with -mod=readonly,
// so every step refuses before Go runs, and nothing is written.
func TestListingRefusesAGoWorkspace(t *testing.T) {
	t.Setenv("GOWORK", "")
	module := copiedModule(t, "two")
	work := filepath.Join(module, "go.work")
	writeFile(t, work, "go 1.25.0\n\nuse .\n")
	before := snapshot(t, module)
	want := say(inWorkspace, work).Error()
	target := firstOfTwo(module)
	steps := []struct {
		name string
		run  func() error
	}{
		{"List", func() error {
			_, err := List(t.Context(), target)
			return err
		}},
		{"Unmounted", func() error {
			_, err := Unmounted(t.Context(), target, nil)
			return err
		}},
		{"Find", func() error {
			_, err := Find(t.Context(), module, target.ImportPath)
			return err
		}},
	}
	for _, step := range steps {
		if err := step.run(); err == nil || err.Error() != want {
			t.Errorf("%s in a workspace gave %v, want:\n%s",
				step.name, err, want)
		}
	}
	after := snapshot(t, module)
	if !maps.Equal(after, before) {
		t.Errorf("listing in a workspace changed the module: %q",
			slices.Sorted(maps.Keys(after)))
	}
	t.Setenv("GOWORK", "off")
	if _, err := Find(t.Context(), module, target.ImportPath); err != nil {
		t.Errorf("with GOWORK=off, Find failed: %v", err)
	}
}

// goIn runs a go command in module, outside any workspace and without
// the network, and fails the test if it fails.
func goIn(t *testing.T, module string, args ...string) {
	t.Helper()
	command := exec.Command("go", args...)
	command.Dir = module
	command.Env = append(os.Environ(),
		"GOFLAGS=-mod=mod", "GOPROXY=off", "GOWORK=off")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

// moduleFiles is what the go command could rewrite in a vendored
// module, read whole so a test can tell they were left as they were.
func moduleFiles(t *testing.T, module string) map[string]string {
	t.Helper()
	files := map[string]string{}
	for _, name := range []string{"go.mod", "go.sum", "vendor/modules.txt"} {
		content, err := os.ReadFile(filepath.Join(module, name))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = string(content)
	}
	return files
}

// The vendored action package is patched to name every action
// "vendored", so the listing shows which copy was compiled: an app
// whose vendor/ holds patched code must be listed with that code.
func TestListUsesTheAppsVendoredCode(t *testing.T) {
	module := copiedModule(t, "two")
	goIn(t, module, "mod", "vendor")
	routesFile := filepath.Join(module, "vendor", "amadan.net", "rastrillo",
		"rastrillo", "action", "routes.go")
	source, err := os.ReadFile(routesFile)
	if err != nil {
		t.Fatal(err)
	}
	named := `Handler: identity[strings.LastIndex(identity, ".")+1:],`
	if !bytes.Contains(source, []byte(named)) {
		t.Fatalf("the vendored action package no longer has %s", named)
	}
	patched := strings.Replace(string(source), named, `Handler: "vendored",`, 1)
	writeFile(t, routesFile, patched)
	t.Setenv("GOFLAGS", "-mod=vendor")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOWORK", "")
	before := moduleFiles(t, module)

	target, err := Find(t.Context(), module, "example.com/two/internal/first")
	if err != nil {
		t.Fatalf("Find in a vendored module failed: %v", err)
	}
	target.Output = &bytes.Buffer{}
	routes, err := List(t.Context(), target)
	if err != nil {
		t.Fatalf("List in a vendored module failed: %v", err)
	}
	if len(routes) != 1 || routes[0].Handler != "vendored" {
		t.Errorf("List gave %+v, want the vendored code's one route", routes)
	}
	if _, err := Unmounted(t.Context(), target, routes); err != nil {
		t.Errorf("Unmounted in a vendored module failed: %v", err)
	}
	if !maps.Equal(moduleFiles(t, module), before) {
		t.Errorf("listing a vendored module changed its module files")
	}
}

// A file that imports "C" is compiled when cgo is on, so an action in
// it is one --check must find.
func TestUnmountedFindsAnActionInACgoFile(t *testing.T) {
	t.Setenv("CGO_ENABLED", "1")
	compiler, err := exec.Command("go", "env", "CC").Output()
	if err != nil {
		t.Fatal(err)
	}
	name := strings.Fields(string(compiler) + " cc")[0]
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("cgo needs a C compiler, and %s was not found", name)
	}
	module := copiedModule(t, "two")
	writeFile(t, filepath.Join(module, "internal", "first", "cgo.go"),
		"package first\n\nimport \"C\"\n\n"+
			"import \"amadan.net/rastrillo/rastrillo/action\"\n\n"+
			"func (app App) inCgo(visit *action.Visit) {}\n")
	target, err := Find(t.Context(), module, "example.com/two/internal/first")
	if err != nil {
		t.Fatalf("Find failed: %v", err)
	}
	target.Output = &bytes.Buffer{}
	routes, err := List(t.Context(), target)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	unmounted, err := Unmounted(t.Context(), target, routes)
	if err != nil {
		t.Fatalf("Unmounted failed: %v", err)
	}
	inCgo := "example.com/two/internal/first.App.inCgo"
	if !slices.Contains(unmounted, inCgo) {
		t.Errorf("Unmounted gave %q, want it to name %s", unmounted, inCgo)
	}
}

// The listing program sits at the module's root, so it may import the
// top-level internal/ and nothing under a second internal directory.
func TestFindRefusesRoutesUnderASecondInternalDirectory(t *testing.T) {
	module := copiedModule(t, "none")
	nested := "example.com/none/internal/feature/internal/web"
	writeFile(t, filepath.Join(module, "internal", "feature", "internal",
		"web", "web.go"), "package web\n\n"+
		"import \"amadan.net/rastrillo/rastrillo/action\"\n\n"+
		"type App struct{}\n\n"+
		"func Routes(routes *action.Routes, app *App) {}\n")
	want := say(nestedInternal, nested).Error()
	for _, chosen := range []string{"", nested} {
		_, err := Find(t.Context(), module, chosen)
		if err == nil || err.Error() != want {
			t.Errorf("Find(%q) gave %v, want:\n%s", chosen, err, want)
		}
	}
}
