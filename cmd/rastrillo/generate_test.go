package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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

const handleSrc = "//go:build rastrillo_actions\n\npackage actions\n\nimport (\n\t\"net/http\"\n\n\t\"github.com/carlosframework/rastrillo\"\n)\n\nfunc Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {}\n"

// untaggedHandleSrc is a pre-F9 action file: valid generator input that
// `go build ./...` would try (and fail) to compile.
const untaggedHandleSrc = "package actions\n\nimport (\n\t\"net/http\"\n\n\t\"github.com/carlosframework/rastrillo\"\n)\n\nfunc Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {}\n"

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

func TestGenerateCheckWritesNothing(t *testing.T) {
	dir := scaffold(t, map[string]string{
		"go.mod":               "module demo\n\ngo 1.22\n",
		"actions/index.GET.go": handleSrc,
	})
	if err := runGenerate([]string{"--check", dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "gen")); !os.IsNotExist(err) {
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
	err := runGenerate([]string{"--check", dir})
	if err == nil {
		t.Fatal("want a failure: fr.toml is missing a key en.toml has (design doc §10)")
	}
	if !strings.Contains(err.Error(), "catalog") {
		t.Errorf("error should name the check that failed: %v", err)
	}
}

func TestGenerateCheckPassesOnACompleteCatalog(t *testing.T) {
	dir := scaffold(t, map[string]string{
		"go.mod":               "module demo\n\ngo 1.22\n",
		"actions/index.GET.go": handleSrc,
		"locales/en.toml":      "a = \"A\"\n",
		"locales/fr.toml":      "a = \"A\"\n",
	})
	if err := runGenerate([]string{"--check", dir}); err != nil {
		t.Fatalf("want a pass, got %v", err)
	}
}

func TestGenerateCheckHonoursTheDefaultLocaleFlag(t *testing.T) {
	dir := scaffold(t, map[string]string{
		"go.mod":               "module demo\n\ngo 1.22\n",
		"actions/index.GET.go": handleSrc,
		"locales/fr.toml":      "a = \"A\"\nb = \"B\"\n",
		"locales/en.toml":      "a = \"A\"\n",
	})
	if err := runGenerate([]string{"--check", "--default-locale", "fr", dir}); err == nil {
		t.Fatal("with fr as the default, en.toml is the incomplete one")
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
		"require github.com/carlosframework/rastrillo v0.0.0\n\n"+
		"replace github.com/carlosframework/rastrillo => %s\n", repoRoot(t))
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
		"require github.com/carlosframework/rastrillo v0.0.0\n\n"+
		"replace github.com/carlosframework/rastrillo => %s\n", repoRoot(t))
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
		"require github.com/carlosframework/rastrillo v0.0.0\n\n"+
		"replace github.com/carlosframework/rastrillo => %s\n", repoRoot(t))
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
