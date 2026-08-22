package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosframework/rastrillo/ui"
)

// rastrillo new writes the design-token stylesheet into the new app's
// static tree, once. From then on it is an ordinary app-owned file.
func TestNewScaffoldsTokensCSS(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"blogapp"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	got, err := os.ReadFile(filepath.Join("blogapp", "internal", "blogapp", "static", "tokens.css"))
	if err != nil {
		t.Fatalf("expected a scaffolded stylesheet: %v", err)
	}
	if !bytes.Equal(got, ui.TokensCSS()) {
		t.Errorf("scaffolded tokens.css is not ui.TokensCSS() verbatim (%d bytes vs %d)", len(got), len(ui.TokensCSS()))
	}
}

// The scaffolded go.mod must pin the versions this CLI was actually
// built against — rastrillo via its build-info version (see
// version.go), chi and gorm via the CLI's own dependency list — not
// hardcoded constants that go stale every release.
func TestNewPinsCLIsOwnVersions(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"blogapp"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	got, err := os.ReadFile(filepath.Join("blogapp", "go.mod"))
	if err != nil {
		t.Fatalf("expected a scaffolded go.mod: %v", err)
	}
	// go test binaries report "(devel)" from runtime/debug.ReadBuildInfo
	// (a local checkout, not a `go install ...@vX.Y.Z`), so
	// rastrilloVersion falls back to rastrilloFallbackVersion here —
	// see version_test.go for the fallback logic itself.
	for _, want := range []string{
		"github.com/carlosframework/rastrillo " + rastrilloFallbackVersion,
		"github.com/go-chi/chi/v5 " + chiPinnedVersion,
		"gorm.io/gorm " + gormPinnedVersion,
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("go.mod missing %q:\n%s", want, got)
		}
	}
}

// The chi/gorm pin constants must match the rastrillo module's own
// go.mod — the versions this suite actually ran against. A drifted
// constant would scaffold apps onto versions the framework never
// tested with.
func TestScaffoldDepPinsMatchRootModule(t *testing.T) {
	root := repoRoot(t)
	gomod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"github.com/go-chi/chi/v5 " + chiPinnedVersion,
		"gorm.io/gorm " + gormPinnedVersion,
	} {
		if !strings.Contains(string(gomod), want+"\n") && !strings.Contains(string(gomod), want+" ") {
			t.Errorf("root go.mod does not pin %q — bump the scaffold constant in new.go alongside the module", want)
		}
	}
}

// The scaffold is SKILL.md's five-file shape plus templates, the
// harness, and the manifest landing spot.
func TestNewScaffoldsMiddleLayerShape(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"blogapp"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	for _, rel := range []string{
		"go.mod",
		filepath.Join("cmd", "blogapp", "main.go"),
		filepath.Join("internal", "blogapp", "app.go"),
		filepath.Join("internal", "blogapp", "models.go"),
		filepath.Join("internal", "blogapp", "handlers.go"),
		filepath.Join("internal", "blogapp", "render.go"),
		filepath.Join("internal", "blogapp", "templates", "layout.html"),
		filepath.Join("internal", "blogapp", "templates", "index.html"),
		filepath.Join("manifest", "README.md"),
	} {
		if _, err := os.Stat(filepath.Join("blogapp", rel)); err != nil {
			t.Errorf("missing scaffolded file %s: %v", rel, err)
		}
	}
}

// main.go wires the app the way SKILL.md §1 teaches: Resolve (not Run
// — the app owns its DB handle via db.Open), then Serve with DBPath
// cleared so Serve doesn't double-open the same file.
func TestMainTemplateWiresResolveOpenServe(t *testing.T) {
	src := fmt.Sprintf(mainTemplate, "blogapp", "blogapp")
	for _, want := range []string{
		"rastrillo.Resolve(rastrillo.Options{",
		"db.Open(opts.DBPath, logger)",
		"blogapp.App(d, logger)",
		"opts.DBPath = \"\"",
		"rastrillo.Serve(opts)",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("main.go template missing %q:\n%s", want, src)
		}
	}
	if strings.Index(src, `opts.DBPath = ""`) < strings.Index(src, "db.Open(") {
		t.Error("DBPath must be cleared only after db.Open used it")
	}
}

// The manifest README carries the equal-path framing and a real
// mounting recipe, not a dustbin note.
func TestManifestReadmeCarriesTheRecipe(t *testing.T) {
	src := fmt.Sprintf(manifestReadme, "blogapp", "blogapp")
	for _, want := range []string{
		"equal, optional paths",
		"rastrillo generate",
		"gen.Router(",
		"Render: render",
		"unscoped tables",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("manifest README missing %q", want)
		}
	}
}

// packageName derives a Go identifier from the app name for the
// scaffolded app package, since the name is also the module path
// where hyphens (and other non-identifier characters) are legal.
func TestPackageName(t *testing.T) {
	cases := []struct{ name, want string }{
		{"blogapp", "blogapp"},
		{"my-blog", "myblog"},
		{"9lives", "app9lives"},
		{"--", "app"},
		// "func" sanitizes to itself (all letters) but is a Go
		// keyword, so a bare package clause using it won't compile.
		{"func", "appfunc"},
		// A leading digit that isn't ASCII: decoding the first rune
		// properly (not just its first byte) still has to catch it.
		{"９lives", "app９lives"},
	}
	for _, c := range cases {
		if got := packageName(c.name); got != c.want {
			t.Errorf("packageName(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

// Regression pin for the packageName sanitizer wiring: a hyphenated
// app name must still scaffold compilable package clauses, while
// cmd/<name>/main.go keeps importing the app under its real,
// hyphenated module path.
func TestNewSanitizesHyphenatedAppName(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"my-blog"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	appSrc, err := os.ReadFile(filepath.Join("my-blog", "internal", "myblog", "app.go"))
	if err != nil {
		t.Fatalf("expected a scaffolded app.go: %v", err)
	}
	if !strings.Contains(string(appSrc), "package myblog") {
		t.Errorf("app.go does not have the sanitized package clause:\n%s", appSrc)
	}
	mainSrc, err := os.ReadFile(filepath.Join("my-blog", "cmd", "my-blog", "main.go"))
	if err != nil {
		t.Fatalf("expected a scaffolded main.go: %v", err)
	}
	if !strings.Contains(string(mainSrc), `myblog "my-blog/internal/myblog"`) {
		t.Errorf("main.go does not import the app package from the hyphenated module path:\n%s", mainSrc)
	}
}

// Out of the box you get a tested app: rastrillo new scaffolds a
// harness plus example tests that pass immediately and pin the
// asset-fingerprinting behavior.
func TestNewScaffoldsTestHarness(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"my-blog"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	harness, err := os.ReadFile(filepath.Join("my-blog", "internal", "myblogtest", "harness_test.go"))
	if err != nil {
		t.Fatalf("expected a scaffolded harness: %v", err)
	}
	for _, want := range []string{
		"package myblogtest",
		"func newApp(t *testing.T) http.Handler",
		`myblog "my-blog/internal/myblog"`,
		"db.Open(filepath.Join(t.TempDir()",
		"myblog.App(d, logger)",
	} {
		if !strings.Contains(string(harness), want) {
			t.Errorf("harness_test.go missing %q:\n%s", want, harness)
		}
	}
	index, err := os.ReadFile(filepath.Join("my-blog", "internal", "myblogtest", "index_test.go"))
	if err != nil {
		t.Fatalf("expected scaffolded example tests: %v", err)
	}
	for _, want := range []string{
		"func TestIndexRenders",
		"func TestIndexLinksFingerprintedStylesheet",
		"func TestBareAssetNameStaysFresh",
		"public, max-age=31536000, immutable",
	} {
		if !strings.Contains(string(index), want) {
			t.Errorf("index_test.go missing %q:\n%s", want, index)
		}
	}
}

// The scaffold's own tests pass, from zero, before the developer
// writes a line: go test ./... against the freshly generated app,
// with the rastrillo require replaced by this checkout (the same
// scratch-module pattern internal/manifest's goeval tests use).
func TestScaffoldedAppTestsPass(t *testing.T) {
	root := repoRoot(t)
	t.Chdir(t.TempDir())
	if err := runNew([]string{"blogapp"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	f, err := os.OpenFile(filepath.Join("blogapp", "go.mod"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\nreplace github.com/carlosframework/rastrillo => " + root + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = "blogapp"
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy on the scaffold fails:\n%s", out)
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = "blogapp"
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("scaffolded app's tests fail:\n%s", out)
	}
}
