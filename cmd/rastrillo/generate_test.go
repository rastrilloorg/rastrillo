package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/generate/packs"
	"amadan.net/rastrillo/rastrillo/internal/manifest"
	"amadan.net/rastrillo/rastrillo/tmpl"
)

// repoRoot returns this repo's absolute root, computed from this
// file's own location rather than the test's working directory — same
// pattern as internal/generate/sqlcrun_test.go's helper of the same
// name (each package needs its own since it's unexported).
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(file), "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func scaffold(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const handleSrc = "//go:build rastrillo_actions\n\npackage actions\n\nimport (\n\t\"net/http\"\n\n\t\"amadan.net/rastrillo/rastrillo\"\n)\n\nfunc Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {}\n"

// untaggedHandleSrc is a pre-F9 action file: valid generator input that
// `go build ./...` would try (and fail) to compile.
const untaggedHandleSrc = "package actions\n\nimport (\n\t\"net/http\"\n\n\t\"amadan.net/rastrillo/rastrillo\"\n)\n\nfunc Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {}\n"

func TestGenerateWritesTheRouter(t *testing.T) {
	dir := scaffold(t, map[string]string{
		"go.mod":               "module demo\n\ngo 1.22\n",
		"actions/index.GET.go": handleSrc,
	})
	if err := runGenerate([]string{dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "gen", "router.go")); err != nil {
		t.Fatalf("expected gen/router.go: %v", err)
	}
}

// TestGenerateWritesAPIFileWithoutManifests: gen/api.go is written for
// every app, so a main.go that mounts gen.APIRoutes() compiles before
// the first manifest exists; with no resources it lists nothing. The
// golden is apigen's own, read from there so the two cannot drift.
func TestGenerateWritesAPIFileWithoutManifests(t *testing.T) {
	dir := scaffold(t, map[string]string{
		"go.mod":               "module demo\n\ngo 1.22\n",
		"actions/index.GET.go": handleSrc,
	})
	if err := runGenerate([]string{dir}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "gen", "api.go"))
	if err != nil {
		t.Fatalf("expected gen/api.go: %v", err)
	}
	want, err := os.ReadFile(filepath.Join(repoRoot(t), "internal", "generate", "apigen", "testdata", "api", "api_none.go.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("gen/api.go differs from api_none.go.golden:\n%s", got)
	}
	// A second run must keep it: prune removes any generated file the
	// run did not claim, and a kept api.go is the claim's proof.
	if err := runGenerate([]string{dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "gen", "api.go")); err != nil {
		t.Fatalf("gen/api.go after a second run: %v", err)
	}
}

// TestGenerateWritesNoToolsFile: the agent tool registry is gone, and
// operations replace it. A gen/tools.go would reference a type the root
// package no longer has and break the app's build, so generate must not
// write one, and must remove the one an older release left behind.
func TestGenerateWritesNoToolsFile(t *testing.T) {
	dir := scaffold(t, map[string]string{
		"go.mod":               "module demo\n\ngo 1.22\n",
		"actions/index.GET.go": handleSrc,
		"gen/tools.go":         "package gen\n",
	})
	if err := runGenerate([]string{dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "gen", "tools.go")); !os.IsNotExist(err) {
		t.Fatalf("gen/tools.go must not be written (stat err: %v)", err)
	}
}

// A hand action's copy under gen/actions is what the router serves,
// and the parity lint reads the source under actions/. While --check
// passed with the copy stale, an author could add the operation call
// to the source, skip generate, and ship a router running the old
// handler without op.Check (final review, finding 2).
func TestGenerateCheckRefusesAStaleHandActionCopy(t *testing.T) {
	dir := scaffold(t, map[string]string{
		"go.mod":               "module demo\n\ngo 1.22\n",
		"actions/index.GET.go": handleSrc,
	})
	if err := runGenerate([]string{dir}); err != nil {
		t.Fatal(err)
	}
	if err := runGenerate([]string{"--check", dir}); err != nil {
		t.Fatalf("--check must pass with a fresh copy: %v", err)
	}
	edited := strings.Replace(handleSrc, "{}", "{ w.WriteHeader(http.StatusTeapot) }", 1)
	if err := os.WriteFile(filepath.Join(dir, "actions", "index.GET.go"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runGenerate([]string{"--check", dir})
	if err == nil {
		t.Fatal("--check passed while gen/actions held a stale copy of actions/index.GET.go")
	}
	if !strings.Contains(err.Error(), "gen/actions/index_get/index.GET.go") {
		t.Errorf("--check's error must name the stale copy, got:\n%v", err)
	}
	if err := runGenerate([]string{dir}); err != nil {
		t.Fatal(err)
	}
	if err := runGenerate([]string{"--check", dir}); err != nil {
		t.Errorf("after a regenerate --check must pass again: %v", err)
	}
}

func TestGenerateCheckWritesNothing(t *testing.T) {
	dir := scaffold(t, map[string]string{
		"go.mod":               "module demo\n\ngo 1.22\n",
		"actions/index.GET.go": handleSrc,
	})
	if err := runGenerate([]string{"--check", dir}); err == nil || !strings.Contains(err.Error(), "gen/locales/packs.go") {
		t.Fatalf("a missing packs file must fail --check: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "gen")); !os.IsNotExist(err) {
		t.Fatal("--check must not write gen/")
	}
	// With the packs in place it passes, and still writes no router.
	if _, err := packs.Emit(filepath.Join(dir, "gen"), manifest.App{Locales: []string{"en"}, DefaultLocale: "en"}); err != nil {
		t.Fatal(err)
	}
	if err := runGenerate([]string{"--check", dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "gen", "router.go")); !os.IsNotExist(err) {
		t.Fatal("--check must not write gen/")
	}
}

func TestGenerateWithoutCheckIgnoresAnIncompleteCatalog(t *testing.T) {
	// Plain `generate` (what `rastrillo dev` reruns on every save, and
	// what `rastrillo new` runs once) must never hard-fail on an
	// incomplete catalog — design doc §10's "silent fallback while
	// iterating" applies here; the loud failure is --check-only.
	dir := scaffold(t, map[string]string{
		"go.mod":               "module demo\n\ngo 1.22\n",
		"actions/index.GET.go": handleSrc,
		"locales/en.toml":      "a = \"A\"\nb = \"B\"\n",
		"locales/fr.toml":      "a = \"A\"\n",
	})
	if err := runGenerate([]string{dir}); err != nil {
		t.Fatalf("plain generate must not fail on an incomplete catalog: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "gen", "router.go")); err != nil {
		t.Fatalf("expected gen/router.go: %v", err)
	}
}

func TestGenerateCheckFailsOnAnIncompleteCatalog(t *testing.T) {
	dir := scaffold(t, map[string]string{
		"go.mod":               "module demo\n\ngo 1.22\n",
		"actions/index.GET.go": handleSrc,
		"locales/en.toml":      "a = \"A\"\nb = \"B\"\n",
		"locales/fr.toml":      "a = \"A\"\n",
	})
	generateFirst(t, dir)
	err := runGenerate([]string{"--check", dir})
	if err == nil {
		t.Fatal("want a failure: fr.toml is missing a key en.toml has (design doc §10)")
	}
	if !strings.Contains(err.Error(), "catalog") {
		t.Errorf("error should name the check that failed: %v", err)
	}
}

func TestGenerateCheckPassesOnACompleteCatalog(t *testing.T) {
	// fr is not one of the framework's shipped locales, so its catalog
	// must also translate every rastrillo.ui.* key (Task 6) to pass
	// --check, on top of matching the app's own en.toml keys.
	var fr strings.Builder
	fr.WriteString("a = \"A\"\n")
	for _, k := range rastrillo.BaseKeys() {
		fmt.Fprintf(&fr, "%s = \"x\"\n", k)
	}
	dir := scaffold(t, map[string]string{
		"go.mod":               "module demo\n\ngo 1.22\n",
		"actions/index.GET.go": handleSrc,
		"locales/en.toml":      "a = \"A\"\n",
		"locales/fr.toml":      fr.String(),
	})
	generateFirst(t, dir)
	if err := runGenerate([]string{"--check", dir}); err != nil {
		t.Fatalf("want a pass, got %v", err)
	}
}

func TestGenerateCheckTakesTheDefaultFromAppTOML(t *testing.T) {
	dir := scaffold(t, map[string]string{
		"go.mod":               "module demo\n\ngo 1.22\n",
		"actions/index.GET.go": handleSrc,
		"manifest/app.toml":    "locales = [\"fr\", \"en\"]\ndefault_locale = \"fr\"\n",
		"locales/fr.toml":      "a = \"A\"\nb = \"B\"\n",
		"locales/en.toml":      "a = \"A\"\n",
	})
	generateFirst(t, dir)
	err := runGenerate([]string{"--check", dir})
	if err == nil || !strings.Contains(err.Error(), "catalog") {
		t.Fatalf("with fr as the default, en.toml is the incomplete one: %v", err)
	}
}

func TestGenerateRefusesTheRemovedDefaultLocaleFlag(t *testing.T) {
	dir := scaffold(t, map[string]string{"go.mod": "module demo\n\ngo 1.22\n"})
	err := runGenerate([]string{"--check", "--default-locale", "fr", dir})
	want := "--default-locale was removed: the default comes from default_locale in manifest/app.toml"
	if err == nil || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
}

// A language added to app.toml without regenerating leaves the binary
// without its pack; --check must say so before boot does. Once the
// packs and the catalog are in place, the same app passes. The copy's
// replace no longer reaches this checkout, so its main cannot start in
// capture mode and --check could not verify its compiled templates;
// they are not what this test is about, so it leaves them out.
func TestGenerateCheckFailsWhenAppTOMLOutrunsThePacks(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(repoRoot(t), "examples", "tickets")
	if out, err := exec.Command("cp", "-R", src+"/.", dir).CombinedOutput(); err != nil {
		t.Fatalf("copy tickets: %v\n%s", err, out)
	}
	checkArgs := []string{"--check", "--templates=false", dir}
	if err := runGenerate(checkArgs); err != nil {
		t.Fatalf("the committed example must pass first: %v", err)
	}
	app := filepath.Join(dir, "manifest", "app.toml")
	if err := os.WriteFile(app, []byte("locales = [\"en\", \"es\"]\ndefault_locale = \"en\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runGenerate([]string{"--check", "--templates=false", dir})
	if err == nil || !strings.Contains(err.Error(), "gen/locales/packs.go") {
		t.Fatalf("want a failure naming gen/locales/packs.go, got %v", err)
	}

	// The rest of adding a language to a manifest app: regenerate, then
	// the catalog --check asks for, translated from the manifest's
	// English (a copy stands in for the translation).
	generateFirst(t, dir)
	err = runGenerate(checkArgs)
	if err == nil || !strings.Contains(err.Error(), "catalog") {
		t.Fatalf("want locales/es.toml demanded, got %v", err)
	}
	en, err := os.ReadFile(filepath.Join(dir, "gen", "locales", "en.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "locales"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "locales", "es.toml"), en, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runGenerate(checkArgs); err != nil {
		t.Fatalf("a manifest app offering es with a complete locales/es.toml must pass: %v", err)
	}
}

// generateFirst runs plain generate, as an app does before its first
// --check: --check fails on a missing gen/locales/packs.go, which is
// not what the tests calling this are about.
func generateFirst(t *testing.T, dir string) {
	t.Helper()
	if err := runGenerate([]string{dir}); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateWithoutCheckToleratesAnUntaggedAction(t *testing.T) {
	// Same §10 split as catalogs: a missing build tag never blocks the
	// dev loop — the loud failure is --check-only (friction log F9).
	dir := scaffold(t, map[string]string{
		"go.mod":               "module demo\n\ngo 1.22\n",
		"actions/index.GET.go": untaggedHandleSrc,
	})
	if err := runGenerate([]string{dir}); err != nil {
		t.Fatalf("plain generate must not fail on an untagged action: %v", err)
	}
}

func TestGenerateCheckFailsOnAnUntaggedAction(t *testing.T) {
	dir := scaffold(t, map[string]string{
		"go.mod":               "module demo\n\ngo 1.22\n",
		"actions/index.GET.go": untaggedHandleSrc,
	})
	err := runGenerate([]string{"--check", dir})
	if err == nil {
		t.Fatal("want a failure: the file lacks //go:build rastrillo_actions (F9)")
	}
	if !strings.Contains(err.Error(), "rastrillo_actions") {
		t.Errorf("error should name the missing tag: %v", err)
	}
}

// notesManifestTOML declares one manifest resource, reused by the two
// tests below that prove `rastrillo generate` actually folds a
// manifest resource's generated actions into gen/router.go (not just
// that internal/generate.GenerateManifests writes the right files —
// that's covered in internal/generate/manifestgen_test.go; this is the
// cmd-level wiring that merges its routes into the SAME router hand
// actions get, which no other test exercises).
const notesManifestTOML = `name  = "notes"
route = "/admin/notes"

[list]
columns = [{ field = "Title" }]

[form]
basics = [{ name = "Title" }]
`

func TestGenerateWiresManifestActionsIntoTheRouter(t *testing.T) {
	goMod := fmt.Sprintf("module demo\n\ngo 1.25.0\n\ntool github.com/sqlc-dev/sqlc/cmd/sqlc\n\n"+
		"require amadan.net/rastrillo/rastrillo v0.0.0\n\n"+
		"replace amadan.net/rastrillo/rastrillo => %s\n", repoRoot(t))
	dir := scaffold(t, map[string]string{
		"go.mod":               goMod,
		"actions/index.GET.go": handleSrc,
		"manifest/notes.toml":  notesManifestTOML,
	})

	getCmd := exec.Command("go", "get", "-tool", "github.com/sqlc-dev/sqlc/cmd/sqlc")
	getCmd.Dir = dir
	if out, err := getCmd.CombinedOutput(); err != nil {
		t.Skipf("go get -tool sqlc failed (likely a network issue): %v\n%s", err, out)
	}

	if err := runGenerate([]string{dir}); err != nil {
		t.Fatalf("runGenerate: %v", err)
	}

	routerSrc, err := os.ReadFile(filepath.Join(dir, "gen", "router.go"))
	if err != nil {
		t.Fatalf("expected gen/router.go: %v", err)
	}
	if !strings.Contains(string(routerSrc), `"GET /admin/notes"`) {
		t.Errorf("router.go should wire the manifest resource's index.GET route, got:\n%s", routerSrc)
	}
	if !strings.Contains(string(routerSrc), `"POST /admin/notes"`) {
		t.Errorf("router.go should wire the manifest resource's index.POST (create) route, got:\n%s", routerSrc)
	}

	if _, err := os.Stat(filepath.Join(dir, "gen", "actions", "admin", "notes", "index_get", "index.GET.go")); err != nil {
		t.Errorf("expected the generated index.GET action file: %v", err)
	}

	buildCmd := exec.Command("go", "build", "./...")
	buildCmd.Dir = dir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./... after generate: %v\n%s", err, out)
	}
}

// TestGenerateFailsWhenAHandActionCollidesWithAManifestRoute uses a
// hand action at a path DIFFERENT from the manifest resource's own
// conventional one ("admin/notes.GET.go", not "admin/notes/index.GET.go")
// that still computes to the identical route "GET /admin/notes" — the
// genuine collision case. A hand file at the exact conventional path
// is instead an allowed override (see internal/generate/manifestgen_test.go's
// TestGenerateManifestsAllowsAHandFileAtTheExactGeneratedPath); this
// test must not confuse the two.
func TestGenerateFailsWhenAHandActionCollidesWithAManifestRoute(t *testing.T) {
	dir := scaffold(t, map[string]string{
		"go.mod":                     "module demo\n\ngo 1.25.0\n",
		"actions/admin/notes.GET.go": handleSrc,
		"manifest/notes.toml":        notesManifestTOML,
	})

	err := runGenerate([]string{dir})
	if err == nil {
		t.Fatal("want a route collision error: a hand action and the manifest resource both claim GET /admin/notes")
	}
	if !strings.Contains(err.Error(), "collision") {
		t.Errorf("error should say collision: %v", err)
	}
}

// TestGenerateKeepsAHandActionAtTheExactGeneratedPath is Task 10's own
// adoption shape end to end: a hand-written index.GET kept at the
// manifest resource's exact conventional path (its own custom list,
// the plan's example) must build and wire successfully — no route
// collision, and the router must dispatch that route to the HAND
// action's package, not a manifest one (EmitActions skipped writing
// its own copy there).
func TestGenerateKeepsAHandActionAtTheExactGeneratedPath(t *testing.T) {
	goMod := fmt.Sprintf("module demo\n\ngo 1.25.0\n\ntool github.com/sqlc-dev/sqlc/cmd/sqlc\n\n"+
		"require amadan.net/rastrillo/rastrillo v0.0.0\n\n"+
		"replace amadan.net/rastrillo/rastrillo => %s\n", repoRoot(t))
	dir := scaffold(t, map[string]string{
		"go.mod":                           goMod,
		"actions/admin/notes/index.GET.go": handleSrc,
		"manifest/notes.toml":              notesManifestTOML,
	})

	getCmd := exec.Command("go", "get", "-tool", "github.com/sqlc-dev/sqlc/cmd/sqlc")
	getCmd.Dir = dir
	if out, err := getCmd.CombinedOutput(); err != nil {
		t.Skipf("go get -tool sqlc failed (likely a network issue): %v\n%s", err, out)
	}

	if err := runGenerate([]string{dir}); err != nil {
		t.Fatalf("runGenerate: %v", err)
	}

	// The hand action's OWN dir/name/method ("admin/notes","index","GET")
	// is identical to the manifest resource's index.GET spec, so
	// Rewrite (a completely separate mechanism, for hand actions) and
	// EmitActions compute the exact same gen/ path for it — Rewrite
	// gets there first and EmitActions' file-level skip means it never
	// overwrites that content with its own. Assert on WHICH content
	// occupies that path (the hand-rewritten package, not a
	// manifest-generated one), not on the path being empty.
	genActionSrc, err := os.ReadFile(filepath.Join(dir, "gen", "actions", "admin", "notes", "index_get", "index.GET.go"))
	if err != nil {
		t.Fatalf("expected the hand-rewritten action at that path: %v", err)
	}
	if !strings.Contains(string(genActionSrc), "package act_admin_notes_index_get") {
		t.Errorf("expected the hand action's own rewritten package there, got:\n%s", genActionSrc)
	}

	routerSrc, err := os.ReadFile(filepath.Join(dir, "gen", "router.go"))
	if err != nil {
		t.Fatalf("expected gen/router.go: %v", err)
	}
	if !strings.Contains(string(routerSrc), `"GET /admin/notes"`) {
		t.Errorf("router.go should still wire GET /admin/notes (to the hand action), got:\n%s", routerSrc)
	}

	buildCmd := exec.Command("go", "build", "./...")
	buildCmd.Dir = dir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./... after generate: %v\n%s", err, out)
	}
}

// TestGenerateSucceedsWithNoActionsDirectory proves Task 5's own
// contract change: a missing actions/ directory is an empty
// hand-action set, not a hard error ("no actions/ directory in %s" —
// the pre-Task-5 behavior every manifest-only app would otherwise hit
// before GenerateManifests ever runs). No manifest/ directory either
// here, on purpose: this pins the narrower fix — actions/ itself is
// optional — without needing sqlc/network at all; the full
// manifest-only case (a real generated resource with no hand actions)
// is TestGenerateWiresAManifestOnlyAppIntoTheRouter below.
func TestGenerateSucceedsWithNoActionsDirectory(t *testing.T) {
	dir := scaffold(t, map[string]string{
		"go.mod": "module demo\n\ngo 1.22\n",
	})
	if err := runGenerate([]string{dir}); err != nil {
		t.Fatalf("runGenerate with no actions/ directory at all: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "gen", "router.go")); err != nil {
		t.Fatalf("expected gen/router.go: %v", err)
	}
}

// TestGenerateCheckSucceedsWithNoActionsDirectory is the --check twin
// of the above: the actions/ directory Stat happens before the --check
// branch, so both flags must tolerate its absence identically.
func TestGenerateCheckSucceedsWithNoActionsDirectory(t *testing.T) {
	dir := scaffold(t, map[string]string{
		"go.mod": "module demo\n\ngo 1.22\n",
	})
	generateFirst(t, dir)
	if err := runGenerate([]string{"--check", dir}); err != nil {
		t.Fatalf("--check with no actions/ directory at all: %v", err)
	}
}

// TestGenerateWiresAManifestOnlyAppIntoTheRouter is Task 5's full
// end-to-end case: manifest/ declares the app's only route and there is
// no actions/ directory at all (a manifest-only app, design doc §9 —
// the common shape for an app that has fully adopted the manifest
// system and hand-writes nothing). Same scaffold as
// TestGenerateWiresManifestActionsIntoTheRouter minus
// actions/index.GET.go — proving the router still gets wired from the
// manifest resource's own generated actions alone.
func TestGenerateWiresAManifestOnlyAppIntoTheRouter(t *testing.T) {
	goMod := fmt.Sprintf("module demo\n\ngo 1.25.0\n\ntool github.com/sqlc-dev/sqlc/cmd/sqlc\n\n"+
		"require amadan.net/rastrillo/rastrillo v0.0.0\n\n"+
		"replace amadan.net/rastrillo/rastrillo => %s\n", repoRoot(t))
	dir := scaffold(t, map[string]string{
		"go.mod":              goMod,
		"manifest/notes.toml": notesManifestTOML,
	})

	getCmd := exec.Command("go", "get", "-tool", "github.com/sqlc-dev/sqlc/cmd/sqlc")
	getCmd.Dir = dir
	if out, err := getCmd.CombinedOutput(); err != nil {
		t.Skipf("go get -tool sqlc failed (likely a network issue): %v\n%s", err, out)
	}

	if err := runGenerate([]string{dir}); err != nil {
		t.Fatalf("runGenerate: %v", err)
	}

	routerSrc, err := os.ReadFile(filepath.Join(dir, "gen", "router.go"))
	if err != nil {
		t.Fatalf("expected gen/router.go: %v", err)
	}
	if !strings.Contains(string(routerSrc), `"GET /admin/notes"`) {
		t.Errorf("router.go should wire the manifest resource's index.GET route, got:\n%s", routerSrc)
	}
	if !strings.Contains(string(routerSrc), `"POST /admin/notes"`) {
		t.Errorf("router.go should wire the manifest resource's index.POST (create) route, got:\n%s", routerSrc)
	}

	buildCmd := exec.Command("go", "build", "./...")
	buildCmd.Dir = dir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./... after generate: %v\n%s", err, out)
	}
}

func TestGenerateWatchesLocalesAndTemplates(t *testing.T) {
	// A save to a catalog or a template must restart the dev loop, or
	// the running binary keeps serving the old embedded copy.
	for _, want := range []string{"locales", "templates"} {
		found := false
		for _, d := range watchDirs {
			if d == want {
				found = true
			}
		}
		if !found {
			t.Errorf("watchDirs is missing %q: %v", want, watchDirs)
		}
	}
}

// templateApp is a module with no manifest/ and no actions/ whose main
// reaches rastrillo.Serve with a mux from internal/web, which builds a
// tmpl set at init. web's template is a constant, so a test edits the
// template by rewriting that one line.
func templateApp(t *testing.T) string {
	t.Helper()
	setSandboxGoEnv(t)
	dir := scaffold(t, map[string]string{
		"go.mod": "module demo\n\ngo 1.25.0\n\n" +
			"require amadan.net/rastrillo/rastrillo v0.0.0\n\n" +
			"replace amadan.net/rastrillo/rastrillo => " + repoRoot(t) + "\n",
		"cmd/demo/main.go":    templateAppMain,
		"internal/web/web.go": templateAppWeb,
	})
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = dir
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy: %v\n%s", err, out)
	}
	return dir
}

// The address is in TEST-NET-1, so a Serve that went on to listen
// instead of capturing fails to bind rather than serving.
const templateAppMain = `package main

import (
	"fmt"
	"os"

	"amadan.net/rastrillo/rastrillo"
	_ "amadan.net/rastrillo/rastrillo/locales/en"

	"demo/internal/web"
)

func main() {
	options := rastrillo.Options{Addr: "192.0.2.1:1", Mux: web.Mux()}
	if err := rastrillo.Serve(options); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
`

const templateAppWeb = `package web

import (
	"net/http"

	"amadan.net/rastrillo/rastrillo/tmpl"
)

const pageSource = "<p>{{.Title}}</p>"

var page = tmpl.Must(tmpl.New("page").Parse(pageSource))

type view struct{ Title string }

func Mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		page.Execute(w, view{Title: "hello"})
	})
	return mux
}
`

// captureStderr is captureStdout for os.Stderr, where generate prints
// what it could not compile.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = writer
	defer func() { os.Stderr = orig }()
	fn()
	writer.Close()
	var buf strings.Builder
	if _, err := io.Copy(&buf, reader); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// An app with no manifest/ still gets its templates compiled: the
// template step keys on the mains under cmd/, not on manifests. The
// generated file goes in the package that built the set, a second run
// leaves it alone, and the app still builds with it.
func TestGenerateCompilesTemplatesWithoutManifest(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and captures a scratch app")
	}
	dir := templateApp(t)
	if err := runGenerate([]string{dir}); err != nil {
		t.Fatal(err)
	}
	gen := filepath.Join(dir, "internal", "web", "rastrillo_templates_gen.go")
	first, err := os.Stat(gen)
	if err != nil {
		t.Fatalf("generate wrote no compiled templates: %v", err)
	}
	before, err := os.ReadFile(gen)
	if err != nil {
		t.Fatal(err)
	}
	if err := runGenerate([]string{dir}); err != nil {
		t.Fatal(err)
	}
	second, err := os.Stat(gen)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(gen)
	if err != nil {
		t.Fatal(err)
	}
	unchanged := string(after) == string(before)
	if !unchanged || !second.ModTime().Equal(first.ModTime()) {
		t.Error("a second generate rewrote the compiled templates")
	}
	build := exec.Command("go", "build", "-buildvcs=false", "./...")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build with the compiled templates: %v\n%s", err, out)
	}
}

// --check is the gate that catches a template edited after the last
// generate. It names the file, and writes nothing.
func TestGenerateCheckFailsOnStaleTemplates(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and captures a scratch app")
	}
	dir := templateApp(t)
	if err := runGenerate([]string{dir}); err != nil {
		t.Fatal(err)
	}
	if err := runGenerate([]string{"--check", dir}); err != nil {
		t.Fatalf("--check right after generate: %v", err)
	}
	web := filepath.Join(dir, "internal", "web", "web.go")
	edited := strings.Replace(templateAppWeb,
		"<p>{{.Title}}</p>", "<h1>{{.Title}}</h1>", 1)
	if err := os.WriteFile(web, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	gen := filepath.Join(dir, "internal", "web", "rastrillo_templates_gen.go")
	before, err := os.ReadFile(gen)
	if err != nil {
		t.Fatal(err)
	}
	err = runGenerate([]string{"--check", dir})
	if err == nil {
		t.Fatal("--check passed with a template edited since the last generate")
	}
	message := err.Error()
	if !strings.Contains(message, "internal/web/rastrillo_templates_gen.go") ||
		!strings.Contains(message, "run rastrillo generate") {
		t.Errorf("--check's error must name the file and the fix, "+
			"got:\n%v", err)
	}
	after, err := os.ReadFile(gen)
	if err != nil || string(after) != string(before) {
		t.Errorf("--check rewrote the compiled templates (read err %v)", err)
	}
}

// A compiled file that is missing fails --check like a stale one: a
// deleted or never-committed file would otherwise ship pages on
// html/template with the gate green. rastrillo new compiles a new
// app's templates, so a fresh app starts with its file.
func TestGenerateCheckFailsOnADeletedFile(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and captures a scratch app")
	}
	dir := templateApp(t)
	generateFirst(t, dir)
	gen := filepath.Join(dir, "internal", "web", "rastrillo_templates_gen.go")
	if err := os.Remove(gen); err != nil {
		t.Fatal(err)
	}
	err := runGenerate([]string{"--check", dir})
	if err == nil {
		t.Fatal("--check passed with the compiled templates deleted")
	}
	message := err.Error()
	if !strings.Contains(message, "internal/web/rastrillo_templates_gen.go") ||
		!strings.Contains(message, "run rastrillo generate") {
		t.Errorf("--check's error must name the file and the fix, "+
			"got:\n%v", err)
	}
}

// A main that cannot start in capture mode costs its templates their
// compiled code, and nothing else: generate names it on one line and
// succeeds, and writes no compiled file.
func TestGenerateReportsCaptureFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and captures a scratch app")
	}
	dir := templateApp(t)
	mainFile := filepath.Join(dir, "cmd", "demo", "main.go")
	if err := os.WriteFile(mainFile, []byte(failingMain), 0o644); err != nil {
		t.Fatal(err)
	}
	var runErr error
	out := captureStderr(t, func() { runErr = runGenerate([]string{dir}) })
	if runErr != nil {
		t.Fatalf("generate failed over a capture failure: %v", runErr)
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.Contains(line, "demo/cmd/demo") {
			lines = append(lines, line)
		}
	}
	if len(lines) != 1 {
		t.Errorf("want one line naming demo/cmd/demo, got:\n%s", out)
	}
	gen := filepath.Join(dir, "internal", "web", "rastrillo_templates_gen.go")
	if _, err := os.Stat(gen); !os.IsNotExist(err) {
		t.Errorf("a failed capture still wrote compiled templates "+
			"(stat err %v)", err)
	}
}

// failingMain is templateAppMain made to exit before it captures, as an
// app that needs a secret it does not get would.
var failingMain = strings.Replace(templateAppMain,
	"\tif err := rastrillo.Serve",
	"\tos.Exit(1)\n\tif err := rastrillo.Serve", 1)

// --check cannot say a compiled file is current when the main that
// builds its sets will not start: the file could be stale or missing
// with the gate green. It fails, on one line naming the main and why.
func TestGenerateCheckFailsWhenCaptureCannotVerify(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and captures a scratch app")
	}
	dir := templateApp(t)
	generateFirst(t, dir)
	mainFile := filepath.Join(dir, "cmd", "demo", "main.go")
	if err := os.WriteFile(mainFile, []byte(failingMain), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runGenerate([]string{"--check", dir})
	if err == nil {
		t.Fatal("--check passed with a main that cannot be captured " +
			"beside its compiled file")
	}
	msg := err.Error()
	if !strings.Contains(msg, "demo/cmd/demo") ||
		!strings.Contains(msg, "failed in capture mode") ||
		strings.Contains(msg, "\n") {
		t.Errorf("--check's error must be one line naming the main "+
			"and why, got:\n%v", err)
	}
	// Generate itself stays graceful.
	if err := runGenerate([]string{dir}); err != nil {
		t.Errorf("generate failed over a capture failure: %v", err)
	}
}

// A main that will not start, with no compiled file in any package it
// imports, has nothing --check could find stale: it stays a note.
func TestGenerateCheckNotesACaptureFailureWithNoCompiledFile(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and captures a scratch app")
	}
	dir := templateApp(t)
	mainFile := filepath.Join(dir, "cmd", "demo", "main.go")
	if err := os.WriteFile(mainFile, []byte(failingMain), 0o644); err != nil {
		t.Fatal(err)
	}
	generateFirst(t, dir)
	var runErr error
	out := captureStderr(t, func() {
		runErr = runGenerate([]string{"--check", dir})
	})
	if runErr != nil {
		t.Fatalf("--check failed over a main with no compiled file: %v", runErr)
	}
	if !strings.Contains(out, "demo/cmd/demo") {
		t.Errorf("--check did not name the main it could not capture:\n%s", out)
	}
}

// rastrillo dev regenerates on every save, and capture rebuilds and
// starts the app, so dev's generate leaves the template step out: the
// page falls back to html/template, which shows the file on disk. The
// app it starts is told it runs under dev, which keeps the stale
// warning at debug level.
func TestDevSkipsTemplateStep(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a scratch app")
	}
	dir := templateApp(t)
	if err := runGenerate(devGenerateArgs(dir)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "gen", "router.go")); err != nil {
		t.Errorf("dev's generate skipped gen/ too: %v", err)
	}
	gen := filepath.Join(dir, "internal", "web", "rastrillo_templates_gen.go")
	if _, err := os.Stat(gen); !os.IsNotExist(err) {
		t.Errorf("dev's generate compiled templates (stat err %v)", err)
	}
	if env := devChildEnv(); !slices.Contains(env, tmpl.DevEnv+"=1") {
		t.Errorf("the app dev starts does not get %s=1", tmpl.DevEnv)
	}
	// dev's build leaves compiled templates out, so a committed file
	// that no longer compiles (a renamed field) cannot break the loop.
	args := devBuildArgs("/bin/app", "./cmd/demo")
	if !slices.Contains(args, "-tags=rastrillo_dev") {
		t.Errorf("dev builds with %v, want -tags=rastrillo_dev", args)
	}
	broken := filepath.Join(dir,
		"internal", "web", "rastrillo_templates_gen.go")
	stale := "// Code generated by rastrillo generate; DO NOT EDIT.\n\n" +
		"//go:build !rastrillo_capture && !rastrillo_dev\n\n" +
		"package web\n\nvar _ = view{}.Gone\n"
	if err := os.WriteFile(broken, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "app")
	build := exec.Command("go", devBuildArgs(bin, "./cmd/demo")...)
	build.Dir = dir
	build.Env = append(os.Environ(), "GOFLAGS=-mod=mod -buildvcs=false")
	if out, err := build.CombinedOutput(); err != nil {
		t.Errorf("dev's build failed on a stale compiled file: "+
			"%v\n%s", err, out)
	}
}
