package generate

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"amadan.net/rastrillo/rastrillo"
	"amadan.net/rastrillo/rastrillo/internal/manifest"
)

// notesManifestTOML is the plan's running "notes" example (matching
// fixtureResource's own shape), written straight into a scratch
// module's manifest/ directory — reusing Task 6's newScratchModule
// helper (sqlcrun_test.go, same package) for the module scaffolding.
const notesManifestTOML = `name  = "notes"
route = "/admin/notes"

[list]
columns = [{ field = "Title" }, { field = "Price", kind = "money" }]
search  = true
filter  = ["Title"]

[form]
basics   = [{ name = "Title" }, { name = "Body", kind = "textarea" }]
advanced = [{ name = "Price", kind = "money" }]
`

func TestGenerateManifestsFullPipelineIsIdempotent(t *testing.T) {
	root := newScratchModule(t, true)

	getCmd := exec.Command("go", "get", "-tool", "github.com/sqlc-dev/sqlc/cmd/sqlc")
	getCmd.Dir = root
	if out, err := getCmd.CombinedOutput(); err != nil {
		t.Skipf("go get -tool sqlc failed (likely a network issue): %v\n%s", err, out)
	}

	if err := os.MkdirAll(filepath.Join(root, "manifest"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest", "notes.toml"), []byte(notesManifestTOML), 0o644); err != nil {
		t.Fatal(err)
	}

	genDir := filepath.Join(root, "gen")
	if err := GenerateManifests(root, genDir, false); err != nil {
		t.Fatalf("GenerateManifests: %v", err)
	}

	wantFiles := []string{
		filepath.Join(genDir, "manifest.json"),
		filepath.Join(genDir, "store", "sqlc.yaml"),
		filepath.Join(genDir, "store", "notes", "schema.sql"),
		filepath.Join(genDir, "store", "notes", "queries.sql"),
		filepath.Join(genDir, "store", "notes", "migrations.go"),
		filepath.Join(genDir, "templates", "notes", "list.html"),
		filepath.Join(genDir, "templates", "notes", "show.html"),
		filepath.Join(genDir, "templates", "notes", "form.html"),
		filepath.Join(genDir, "actions", "admin", "notes", "index_get", "index.GET.go"),
		filepath.Join(genDir, "actions", "admin", "notes", "index_post", "index.POST.go"),
		filepath.Join(genDir, "actions", "admin", "notes", "new_get", "new.GET.go"),
		filepath.Join(genDir, "locales", "en.toml"),
		filepath.Join(genDir, "locales", "locales.go"),
	}
	for _, f := range wantFiles {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("expected %s to exist: %v", f, err)
		}
	}

	before := snapshotTree(t, genDir)
	if err := GenerateManifests(root, genDir, false); err != nil {
		t.Fatalf("second GenerateManifests: %v", err)
	}
	after := snapshotTree(t, genDir)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("second GenerateManifests run changed the generated tree")
	}

	buildCmd := exec.Command("go", "build", "./...")
	buildCmd.Dir = root
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./... in scratch module after GenerateManifests: %v\n%s", err, out)
	}
}

func TestGenerateManifestsCheckPassesOnAFreshlySeededTree(t *testing.T) {
	root := newScratchModule(t, false)
	if err := os.MkdirAll(filepath.Join(root, "manifest"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest", "notes.toml"), []byte(notesManifestTOML), 0o644); err != nil {
		t.Fatal(err)
	}

	rs, err := manifest.Load(root, filepath.Join(root, "manifest"))
	if err != nil {
		t.Fatalf("manifest.Load: %v", err)
	}
	genDir := filepath.Join(root, "gen")
	// Seed genDir the same way check-only's own temp-dir dry run would
	// (runSqlc: false), so this test never needs the real sqlc tool.
	if _, err := emitPipeline(root, genDir, rs, false); err != nil {
		t.Fatalf("seed emitPipeline: %v", err)
	}

	if err := GenerateManifests(root, genDir, true); err != nil {
		t.Errorf("check-only on a freshly generated tree should pass, got: %v", err)
	}
}

func TestGenerateManifestsCheckFailsOnAHandEditedGeneratedFile(t *testing.T) {
	root := newScratchModule(t, false)
	if err := os.MkdirAll(filepath.Join(root, "manifest"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest", "notes.toml"), []byte(notesManifestTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	rs, err := manifest.Load(root, filepath.Join(root, "manifest"))
	if err != nil {
		t.Fatalf("manifest.Load: %v", err)
	}
	genDir := filepath.Join(root, "gen")
	if _, err := emitPipeline(root, genDir, rs, false); err != nil {
		t.Fatalf("seed emitPipeline: %v", err)
	}

	edited := filepath.Join(genDir, "store", "notes", "schema.sql")
	if err := os.WriteFile(edited, []byte("-- hand-edited after generation, no longer what the generator would write\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err = GenerateManifests(root, genDir, true)
	if err == nil {
		t.Fatal("want an error: a generated file was hand-edited after generation")
	}
	rel, relErr := filepath.Rel(genDir, edited)
	if relErr != nil {
		t.Fatal(relErr)
	}
	if !strings.Contains(err.Error(), filepath.ToSlash(rel)) {
		t.Errorf("error should name the differing file %q: %v", rel, err)
	}
}

func TestGenerateManifestsCheckFailsOnAnExtraLocaleFile(t *testing.T) {
	root := newScratchModule(t, false)
	if err := os.MkdirAll(filepath.Join(root, "manifest"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest", "notes.toml"), []byte(notesManifestTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	rs, err := manifest.Load(root, filepath.Join(root, "manifest"))
	if err != nil {
		t.Fatalf("manifest.Load: %v", err)
	}
	genDir := filepath.Join(root, "gen")
	if _, err := emitPipeline(root, genDir, rs, false); err != nil {
		t.Fatalf("seed emitPipeline: %v", err)
	}

	stray := filepath.Join(genDir, "locales", "fr.toml")
	if err := os.WriteFile(stray, []byte("a = \"A\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err = GenerateManifests(root, genDir, true)
	if err == nil {
		t.Fatal("want an error: gen/locales/ has a file EmitLocales did not produce")
	}
	if !strings.Contains(err.Error(), "locales/fr.toml") {
		t.Errorf("error should name the extra file: %v", err)
	}
}

func TestGenerateManifestsCheckFailsWhenNeverGenerated(t *testing.T) {
	root := newScratchModule(t, false)
	if err := os.MkdirAll(filepath.Join(root, "manifest"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest", "notes.toml"), []byte(notesManifestTOML), 0o644); err != nil {
		t.Fatal(err)
	}

	// gen/ has never been written at all — --check must fail rather
	// than silently pass on a tree that was never generated.
	genDir := filepath.Join(root, "gen")
	err := GenerateManifests(root, genDir, true)
	if err == nil {
		t.Fatal("want an error: gen/ does not exist yet")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error should say what's missing: %v", err)
	}
}

const handIndexGETSrc = "//go:build rastrillo_actions\n\npackage actions\n\nimport (\n\t\"net/http\"\n\n\t\"amadan.net/rastrillo/rastrillo\"\n)\n\nfunc Handle(ctx *rastrillo.Ctx, w http.ResponseWriter, r *http.Request) {}\n"

// TestGenerateManifestsFailsOnRouteCollisionWithAHandAction covers the
// genuine collision case: a hand action at a DIFFERENT actions/ path
// (dir "admin", name "notes", method GET — routeFor's own convention:
// no bracket/index tricks, just the plain "admin/notes.GET.go" shape)
// that computes to the identical route the manifest's own index.GET
// spec claims ("GET /admin/notes"). This must fail loudly — it is NOT
// the file-level-skip case (see the sibling test below), because the
// two files are not the same conventional path.
func TestGenerateManifestsFailsOnRouteCollisionWithAHandAction(t *testing.T) {
	root := newScratchModule(t, false)
	if err := os.MkdirAll(filepath.Join(root, "manifest"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest", "notes.toml"), []byte(notesManifestTOML), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(root, "actions", "admin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "actions", "admin", "notes.GET.go"), []byte(handIndexGETSrc), 0o644); err != nil {
		t.Fatal(err)
	}

	genDir := filepath.Join(root, "gen")
	err := GenerateManifests(root, genDir, false)
	if err == nil {
		t.Fatal("want a route collision error")
	}
	if !strings.Contains(err.Error(), "GET /admin/notes") {
		t.Errorf("error should name the colliding route: %v", err)
	}
	if !strings.Contains(err.Error(), "collision") {
		t.Errorf("error should say collision: %v", err)
	}
	if !strings.Contains(err.Error(), "actions/admin/notes.GET.go") {
		t.Errorf("error should name the hand action file: %v", err)
	}
	if !strings.Contains(err.Error(), "manifest:notes") {
		t.Errorf("error should name the manifest resource: %v", err)
	}
}

// TestGenerateManifestsAllowsAHandFileAtTheExactGeneratedPath covers
// the OTHER file-level rule (design doc §4, restated in actions.go's
// EmitActions doc comment): a hand file sitting at the EXACT
// conventional path EmitActions would also compute for a resource's
// action ("admin/notes/index.GET.go" — the same path
// Discover/EmitActions both derive from dir="admin/notes", name=
// "index", method="GET") is an ALLOWED override, not a collision —
// Task 10 (blog adoption) explicitly relies on this to keep a
// hand-written list action next to an otherwise-generated resource.
func TestGenerateManifestsAllowsAHandFileAtTheExactGeneratedPath(t *testing.T) {
	root := newScratchModule(t, false)
	if err := os.MkdirAll(filepath.Join(root, "manifest"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest", "notes.toml"), []byte(notesManifestTOML), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(root, "actions", "admin", "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "actions", "admin", "notes", "index.GET.go"), []byte(handIndexGETSrc), 0o644); err != nil {
		t.Fatal(err)
	}

	rs, err := manifest.Load(root, filepath.Join(root, "manifest"))
	if err != nil {
		t.Fatalf("manifest.Load: %v", err)
	}
	genDir := filepath.Join(root, "gen")
	// Seed genDir the check-only way (runSqlc: false) so this test
	// never needs the real sqlc tool — it's routeCollisions/
	// ManifestActions this test is exercising, not the emitters.
	if _, err := emitPipeline(root, genDir, rs, false); err != nil {
		t.Fatalf("seed emitPipeline: %v", err)
	}

	if err := GenerateManifests(root, genDir, true); err != nil {
		t.Errorf("a hand file at the exact generated path must be an allowed override, not a collision: %v", err)
	}

	// EmitActions must have skipped writing its own index.GET at that
	// path — the hand file is untouched and still the only content
	// there.
	got, err := os.ReadFile(filepath.Join(root, "actions", "admin", "notes", "index.GET.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != handIndexGETSrc {
		t.Error("the hand action file must be left untouched")
	}
	if _, err := os.Stat(filepath.Join(genDir, "actions", "admin", "notes", "index_get", "index.GET.go")); !os.IsNotExist(err) {
		t.Errorf("EmitActions must not have written its own index.GET when a hand file occupies that exact path, got: %v", err)
	}

	// And ManifestActions itself must not have produced a router entry
	// for the skipped spec (nothing was written at its GenDir for a
	// router to import).
	manifestActions, err := ManifestActions(filepath.Join(root, "actions"), rs)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range manifestActions {
		if a.Route == "GET /admin/notes" {
			t.Errorf("ManifestActions should have excluded the hand-overridden index.GET spec, got: %+v", a)
		}
	}
}

func TestGenerateManifestsIsANoOpWithoutAnyManifestResources(t *testing.T) {
	root := newScratchModule(t, false)
	genDir := filepath.Join(root, "gen")
	if err := GenerateManifests(root, genDir, false); err != nil {
		t.Fatalf("GenerateManifests with no manifest/ dir at all: %v", err)
	}
	if _, err := os.Stat(genDir); !os.IsNotExist(err) {
		t.Errorf("expected no gen/ directory when the app declares no manifest resources, got: %v", err)
	}
	if err := GenerateManifests(root, genDir, true); err != nil {
		t.Fatalf("check-only with no manifest resources should also be a no-op: %v", err)
	}
}

// snapshotTree reads every regular file under dir into a
// path(relative)->content map, for a byte-exact idempotency
// comparison across two GenerateManifests runs.
func snapshotTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestEmitPipelineCatchesTheSanitizeIdentPathCollision covers the one
// cross-resource gen-path collision manifest.Load's own Name/Route
// uniqueness checks cannot catch (manifestgen.go's own doc comment on
// emitPipeline): two distinct, individually valid routes whose path
// segments differ only by "-" vs "_" collapse onto the identical
// gen/actions/ leaf directory once genDirFor's sanitizeIdent folds
// both separators to the same character.
func TestEmitPipelineCatchesTheSanitizeIdentPathCollision(t *testing.T) {
	r1 := rastrillo.Resource{
		Name:  "aaa",
		Route: "/foo-bar",
		List:  rastrillo.List{Columns: []rastrillo.Column{{Field: "Title"}}},
	}
	if err := r1.Validate(); err != nil {
		t.Fatalf("r1.Validate: %v", err)
	}
	r2 := rastrillo.Resource{
		Name:  "bbb",
		Route: "/foo_bar",
		List:  rastrillo.List{Columns: []rastrillo.Column{{Field: "Title"}}},
	}
	if err := r2.Validate(); err != nil {
		t.Fatalf("r2.Validate: %v", err)
	}

	root := newScratchModule(t, false)
	genDir := filepath.Join(root, "gen")
	_, err := emitPipeline(root, genDir, []rastrillo.Resource{r1, r2}, false)
	if err == nil {
		t.Fatal("want a generated-file collision error: /foo-bar and /foo_bar collapse to the same gen/actions/ path")
	}
	if !strings.Contains(err.Error(), "collision") {
		t.Errorf("error should say collision: %v", err)
	}
	if !strings.Contains(err.Error(), "aaa") || !strings.Contains(err.Error(), "bbb") {
		t.Errorf("error should name both colliding resources: %v", err)
	}
}

// announcementsManifestTOML is the spec §5 mergeable resource —
// examples/tickets declares the same one.
const announcementsManifestTOML = `name  = "announcements"
route = "/admin/announcements"
store = "mergeable"

[list]
columns = [{ field = "Title" }]
search  = true

[form]
basics = [{ name = "Title", required = true }, { name = "Body", kind = "textarea" }]
`

// TestGenerateManifestsMergeableOnlyNeedsNoSqlcTool: a module whose
// only manifest resource is mergeable runs the full pipeline without
// the sqlc tool directive — no sqlc.yaml is written and RunSqlc is
// never reached (spec §2/§4: RunSqlc runs only when at least one
// exclusive resource exists).
func TestGenerateManifestsMergeableOnlyNeedsNoSqlcTool(t *testing.T) {
	root := newScratchModule(t, false) // no sqlc tool directive on purpose

	if err := os.MkdirAll(filepath.Join(root, "manifest"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest", "announcements.toml"), []byte(announcementsManifestTOML), 0o644); err != nil {
		t.Fatal(err)
	}

	genDir := filepath.Join(root, "gen")
	if err := GenerateManifests(root, genDir, false); err != nil {
		t.Fatalf("GenerateManifests: %v", err)
	}

	for _, f := range []string{
		filepath.Join(genDir, "manifest.json"),
		filepath.Join(genDir, "store", "announcements", "store.go"),
		filepath.Join(genDir, "store", "announcements", "migrations.go"),
		filepath.Join(genDir, "templates", "announcements", "list.html"),
		filepath.Join(genDir, "actions", "admin", "announcements", "index_get", "index.GET.go"),
		filepath.Join(genDir, "locales", "en.toml"),
	} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("expected %s to exist: %v", f, err)
		}
	}
	if _, err := os.Stat(filepath.Join(genDir, "store", "sqlc.yaml")); !os.IsNotExist(err) {
		t.Errorf("sqlc.yaml written for a mergeable-only module (stat err = %v)", err)
	}
}
