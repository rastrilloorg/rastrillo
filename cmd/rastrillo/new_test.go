package main

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/token"
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

// rastrillo new writes the fragment shim into the new app's static
// tree, once, the same way it writes tokens.css. From then on it is an
// ordinary app-owned file.
func TestNewScaffoldsRastrilloJS(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"blogapp"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	got, err := os.ReadFile(filepath.Join("blogapp", "internal", "blogapp", "static", "rastrillo.js"))
	if err != nil {
		t.Fatalf("expected a scaffolded rastrillo.js: %v", err)
	}
	if !bytes.Equal(got, ui.ShimJS()) {
		t.Errorf("scaffolded rastrillo.js is not ui.ShimJS() verbatim (%d bytes vs %d)", len(got), len(ui.ShimJS()))
	}
}

// The scaffolded layout must load the shim the same way it links
// tokens.css: through the fingerprinting {{asset ...}} helper, deferred
// so it never blocks first paint.
func TestLayoutTemplateLoadsRastrilloJS(t *testing.T) {
	if !strings.Contains(layoutTemplate, `<script defer src="{{asset "static/rastrillo.js"}}"></script>`) {
		t.Errorf("layout template does not load rastrillo.js via a deferred, fingerprinted <script> tag:\n%s", layoutTemplate)
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
	src := fmt.Sprintf(mainTemplate, "blogapp", "blogapp", "BLOGAPP")
	for _, want := range []string{
		"rastrillo.Resolve(rastrillo.Options{",
		"db.Open(opts.DBPath, logger)",
		"blogapp.App(d, origin, logger)",
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
		`scope = "user"`,
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
		"myblog.App(d, testOrigin, logger)",
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

// The defaults are the recommendation: vendored Lucide, nothing remote.
func TestNewDefaultsToInlineLucide(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"defapp"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	src, err := os.ReadFile(filepath.Join("defapp", "internal", "defapp", "icons", "icons.go"))
	if err != nil {
		t.Fatalf("no scaffolded icons package: %v", err)
	}
	got := string(src)
	if !strings.Contains(got, "<svg") {
		t.Error("default scaffold is not inline SVG")
	}
	for _, bad := range []string{"http://", "https://", "<link", "<script"} {
		if strings.Contains(got, bad) {
			t.Errorf("default scaffold reaches outside the page (%q)", bad)
		}
	}
	if _, err := os.Stat(filepath.Join("defapp", "ICONS-LICENCE.md")); err == nil {
		t.Error("lucide is ISC; no attribution file should be written")
	}
}

func TestNewHonoursIconFlags(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"--icons=font-awesome", "--icon-delivery=cdn", "faapp"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	src, err := os.ReadFile(filepath.Join("faapp", "internal", "faapp", "icons", "icons.go"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(src)
	if !strings.Contains(got, "fa-magnifying-glass") {
		t.Error("font-awesome glyphs not scaffolded")
	}
	if !strings.Contains(got, `"search":`) {
		t.Error("glyph map is not keyed by the rastrillo slug")
	}
	if !strings.Contains(got, "integrity=") || !strings.Contains(got, `crossorigin="anonymous"`) {
		t.Error("cdn delivery scaffolded without a usable SRI")
	}
}

// CC BY 4.0 is not optional: choosing Font Awesome writes the attribution.
func TestNewWritesFontAwesomeAttribution(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"--icons=font-awesome", "faapp"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	got, err := os.ReadFile(filepath.Join("faapp", "ICONS-LICENCE.md"))
	if err != nil {
		t.Fatalf("no attribution file: %v", err)
	}
	if !strings.Contains(string(got), "CC BY 4.0") {
		t.Error("attribution does not name the licence")
	}
}

// An unknown choice must fail while the working directory is still clean.
func TestNewRejectsUnknownChoices(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, args := range [][]string{
		{"--icons=dingbats", "bad1"},
		{"--icon-delivery=telepathy", "bad2"},
		{"--ux=vibes", "bad3"},
	} {
		if err := runNew(args); err == nil {
			t.Errorf("%v was accepted", args)
		}
		if _, err := os.Stat(args[len(args)-1]); err == nil {
			t.Errorf("%v created a directory despite the error", args)
		}
	}
}

// The scaffolded package must parse — it is written into someone's repo.
func TestScaffoldedIconsPackageParses(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"--icons=font-awesome", "--icon-delivery=js", "jsapp"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	src, err := os.ReadFile(filepath.Join("jsapp", "internal", "jsapp", "icons", "icons.go"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "icons.go", src, parser.AllErrors); err != nil {
		t.Errorf("scaffolded icons.go does not parse: %v", err)
	}
}

// AGENTS.md carries the instructions and the conventions; CLAUDE.md
// points at it and duplicates nothing, because two copies drift and the
// drift is silent.
func TestNewWritesAgentsMDWithClaudeMDPointingAtIt(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"--icons=font-awesome", "conv"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	agents, err := os.ReadFile(filepath.Join("conv", "AGENTS.md"))
	if err != nil {
		t.Fatalf("no AGENTS.md: %v", err)
	}
	a := string(agents)
	for _, want := range []string{
		"## UX conventions", "seeded from profile: considered",
		"- [x] Icons — font-awesome, inline", // the flag's value, not the profile's default
		"- [ ] ",                             // the honest gap stays visible
		"source of truth",
		"Rules the family holds hard", // main's own instructions survived the move
	} {
		if !strings.Contains(a, want) {
			t.Errorf("AGENTS.md is missing %q", want)
		}
	}

	claude, err := os.ReadFile(filepath.Join("conv", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("no CLAUDE.md: %v", err)
	}
	c := string(claude)
	if !strings.Contains(c, "@AGENTS.md") {
		t.Errorf("CLAUDE.md does not import AGENTS.md:\n%s", c)
	}
	for _, dup := range []string{"## UX conventions", "Rules the family holds hard"} {
		if strings.Contains(c, dup) {
			t.Errorf("CLAUDE.md duplicates %q; AGENTS.md is the only source", dup)
		}
	}
	if len(claude) >= len(agents) {
		t.Errorf("CLAUDE.md (%d bytes) should be a pointer, not a copy of AGENTS.md (%d bytes)", len(claude), len(agents))
	}
}

// standard is the opt-out: it still records the icon choice, because
// that is a fact about the app, not a convention the profile chose.
func TestStandardProfileAddsNoConventions(t *testing.T) {
	got, err := conventionsSection("standard", "lucide", "inline")
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if strings.Contains(s, "- [x] Fields") {
		t.Error("standard should not opt the app into the field conventions")
	}
	if !strings.Contains(s, "lucide") {
		t.Error("the resolved icon choice should still be recorded")
	}
}

// The layout must call iconAssets, or a cdn/js choice is inert.
func TestScaffoldedLayoutCallsIconAssets(t *testing.T) {
	if !strings.Contains(layoutTemplate, "{{iconAssets}}") {
		t.Error("layout.html never calls iconAssets; cdn and js delivery would render nothing")
	}
	if !strings.Contains(renderTemplate, `"iconAssets": icons.Assets`) {
		t.Error("render.go does not register the iconAssets seam")
	}
	if !strings.Contains(renderTemplate, `"icon":       icons.Icon`) {
		t.Error("render.go does not register the icon seam")
	}
}

// select.js is delivered once, verbatim, on the same terms as the shim,
// and the layout loads it.
func TestNewScaffoldsSelectJS(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"selapp"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	got, err := os.ReadFile(filepath.Join("selapp", "internal", "selapp", "static", "select.js"))
	if err != nil {
		t.Fatalf("select.js not scaffolded: %v", err)
	}
	if !bytes.Equal(got, ui.SelectJS()) {
		t.Error("scaffolded select.js is not ui.SelectJS() verbatim")
	}
	if !strings.Contains(layoutTemplate, `static/select.js`) {
		t.Error("the layout never loads select.js, so the enhancement can never run")
	}
}

// The shipped binary is stripped; the dev loop's is not. Both halves
// matter: -s -w roughly halves the compressed artifact carlos transfers,
// and stripping the dev build would take away the debuggability the dev
// loop exists to provide.
func TestScaffoldSeparatesReleaseBuildFromCompileCheck(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"relapp"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	mk, err := os.ReadFile(filepath.Join("relapp", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(mk)

	if !strings.Contains(got, `go build -ldflags="-s -w" -o $(RELEASE_BIN) ./cmd/$(APP)`) {
		t.Errorf("no stripped release build:\n%s", got)
	}
	// The deployment target, not the host. carlos ship defaults to
	// -target linux-arm64; a release built for the developer's own
	// machine uploads fine and then fails to exec on the instance.
	if !strings.Contains(got, "RELEASE_GOOS   ?= linux") || !strings.Contains(got, "RELEASE_GOARCH ?= arm64") {
		t.Errorf("release does not default to the platform's architecture:\n%s", got)
	}
	if !strings.Contains(got, "releases/$(APP)-$(RELEASE_GOOS)-$(RELEASE_GOARCH)") {
		t.Errorf("release artifact is not named for its architecture:\n%s", got)
	}
	// The output directory has to be ignored, or every release leaves the
	// tree dirty.
	gi, err := os.ReadFile(filepath.Join("relapp", ".gitignore"))
	if err != nil {
		t.Fatalf("no scaffolded .gitignore: %v", err)
	}
	for _, want := range []string{"/releases/", "/relapp.db", "/relapp.db-wal"} {
		if !strings.Contains(string(gi), want) {
			t.Errorf(".gitignore is missing %q:\n%s", want, gi)
		}
	}
	// build stays the plain compile check — it must not quietly become a
	// release build, or a local binary someone wants to debug is stripped.
	buildAt := strings.Index(got, "\nbuild:\n")
	releaseAt := strings.Index(got, "\nrelease:\n")
	if buildAt < 0 || releaseAt < 0 {
		t.Fatalf("expected separate build and release targets:\n%s", got)
	}
	if strings.Contains(got[buildAt:releaseAt], "ldflags") {
		t.Error("the compile-check target strips; that belongs to release only")
	}
	if !strings.Contains(got, ".PHONY: build release") {
		t.Error("release is not declared .PHONY")
	}
}

// rastrillo dev must never strip: the whole point of the loop is a
// binary you can debug.
func TestDevLoopDoesNotStrip(t *testing.T) {
	src, err := os.ReadFile("dev.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), "-s -w") || strings.Contains(string(src), "ldflags") {
		t.Error("rastrillo dev passes link flags; the dev binary must stay debuggable")
	}
}

// The scaffolded router carries csrf.Protect from day one — the
// re-review's point: SKILL.md's checklist said it, but the scaffold's
// router had no r.Use at all, so a builder had to remember. Now every
// state-changing route an app grows is born covered, the origin rides
// in from <APP>_ORIGIN with the loud dev default, and the scaffolded
// test harness presents the same-origin evidence its POSTs need.
func TestNewWiresCSRFProtection(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"blogapp"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	appGo := readScaffold(t, "blogapp", "internal", "blogapp", "app.go")
	for _, want := range []string{
		"github.com/carlosframework/rastrillo/csrf",
		"r.Use(csrf.Protect(origin))",
		"origin string",
	} {
		if !strings.Contains(appGo, want) {
			t.Errorf("scaffolded app.go does not contain %q", want)
		}
	}
	mainGo := readScaffold(t, "blogapp", "cmd", "blogapp", "main.go")
	for _, want := range []string{
		`os.Getenv("BLOGAPP_ORIGIN")`,
		`origin = "http://localhost:8080"`,
		"App(d, origin, logger)",
	} {
		if !strings.Contains(mainGo, want) {
			t.Errorf("scaffolded main.go does not contain %q", want)
		}
	}
	harness := readScaffold(t, "blogapp", "internal", "blogapptest", "harness_test.go")
	for _, want := range []string{
		`req.Header.Set("Origin", testOrigin)`,
		"App(d, testOrigin, logger)",
	} {
		if !strings.Contains(harness, want) {
			t.Errorf("scaffolded harness_test.go does not contain %q", want)
		}
	}
}

// readScaffold reads one scaffolded file or fails the test.
func readScaffold(t *testing.T, parts ...string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(parts...))
	if err != nil {
		t.Fatalf("read %v: %v", parts, err)
	}
	return string(b)
}
