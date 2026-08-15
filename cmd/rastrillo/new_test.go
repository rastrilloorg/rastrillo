package main

import (
	"bytes"
	"fmt"
	"os"
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
	got, err := os.ReadFile(filepath.Join("blogapp", "static", "tokens.css"))
	if err != nil {
		t.Fatalf("expected a scaffolded stylesheet: %v", err)
	}
	if !bytes.Equal(got, ui.TokensCSS()) {
		t.Errorf("scaffolded tokens.css is not ui.TokensCSS() verbatim (%d bytes vs %d)", len(got), len(ui.TokensCSS()))
	}
}

// The scaffolded go.mod must pin the version of the rastrillo module
// that actually built this CLI, not a hardcoded constant that goes
// stale every release: a hardcoded v0.1.0 predates rastrillo.Run
// entirely, so every fresh scaffold failed `go build` with "undefined:
// rastrillo.Run" against a v0.5.0-or-later CLI.
func TestNewPinsCLIsOwnRastrilloVersion(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"blogapp"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	got, err := os.ReadFile(filepath.Join("blogapp", "go.mod"))
	if err != nil {
		t.Fatalf("expected a scaffolded go.mod: %v", err)
	}
	if strings.Contains(string(got), "v0.1.0") {
		t.Errorf("go.mod still pins the stale hardcoded v0.1.0:\n%s", got)
	}
	// go test binaries report "(devel)" from runtime/debug.ReadBuildInfo
	// (a local checkout, not a `go install ...@vX.Y.Z`), so
	// rastrilloVersion falls back to rastrilloFallbackVersion here —
	// see version_test.go for the fallback logic itself.
	want := "require github.com/carlosframework/rastrillo " + rastrilloFallbackVersion
	if !strings.Contains(string(got), want) {
		t.Errorf("go.mod does not pin the fallback rastrillo version:\ngot:\n%s\nwant substring %q", got, want)
	}
}

// The scaffolded app still builds the same way it did before: go.mod,
// the starter action, main.go, and a generated router.
func TestNewStillScaffoldsTheRestOfTheApp(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"blogapp"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	for _, rel := range []string{
		"go.mod",
		filepath.Join("actions", "index.GET.go"),
		filepath.Join("cmd", "blogapp", "main.go"),
		filepath.Join("gen", "router.go"),
	} {
		if _, err := os.Stat(filepath.Join("blogapp", rel)); err != nil {
			t.Errorf("missing scaffolded file %s: %v", rel, err)
		}
	}
}

// The generated app serves its own static directory, fingerprinted:
// rastrillo.Serve never serves CSS — that is the app's job, in the
// app's own code — and the scaffold wires rastrillo.Assets so those
// files are immutable-cacheable from day one.
func TestMainTemplateServesFingerprintedStatic(t *testing.T) {
	src := fmt.Sprintf(mainTemplate, "blogapp")
	for _, want := range []string{
		`assets := rastrillo.NewAssets(app.StaticFS)`,
		`mux.Handle("GET /static/", assets.Handler())`,
		`Assets: assets`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("main.go template missing %q:\n%s", want, src)
		}
	}
	// The mount has to come after the router exists, or it has nothing
	// to attach to.
	if strings.Index(src, "gen.Router(") > strings.Index(src, `assets.Handler()`) {
		t.Error("the static handler is registered before gen.Router builds the mux")
	}
}

// The starter action is a real HTML page linking the stylesheet by its
// content-hashed URL — the scaffold demonstrating its own asset story.
func TestActionTemplateLinksFingerprintedStylesheet(t *testing.T) {
	for _, want := range []string{
		`ctx.Assets.Path("static/tokens.css")`,
		`<h1>Hello, World — this is a rastrillo app.</h1>`,
		`text/html; charset=utf-8`,
	} {
		if !strings.Contains(actionTemplate, want) {
			t.Errorf("action template missing %q", want)
		}
	}
}

// The scaffolded app embeds static/ — the platform deploys the binary
// alone, so a loose static directory would not travel with it (F8).
func TestNewScaffoldsEmbeddedStatic(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"blogapp"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	src, err := os.ReadFile(filepath.Join("blogapp", "assets.go"))
	if err != nil {
		t.Fatalf("expected a scaffolded assets.go: %v", err)
	}
	for _, want := range []string{"//go:embed static", "var StaticFS embed.FS", "package blogapp"} {
		if !strings.Contains(string(src), want) {
			t.Errorf("assets.go missing %q:\n%s", want, src)
		}
	}
}

// packageName derives a Go identifier from the app name for the
// scaffolded root package, since the name is also the module path
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
// app name must still scaffold a compilable assets.go package clause,
// while cmd/<name>/main.go keeps importing the app under its real,
// hyphenated name (the module path, not the sanitized identifier).
func TestNewSanitizesHyphenatedAppName(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"my-blog"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	assets, err := os.ReadFile(filepath.Join("my-blog", "assets.go"))
	if err != nil {
		t.Fatalf("expected a scaffolded assets.go: %v", err)
	}
	if !strings.Contains(string(assets), "package myblog") {
		t.Errorf("assets.go does not have the sanitized package clause:\n%s", assets)
	}
	main, err := os.ReadFile(filepath.Join("my-blog", "cmd", "my-blog", "main.go"))
	if err != nil {
		t.Fatalf("expected a scaffolded main.go: %v", err)
	}
	if !strings.Contains(string(main), `app "my-blog"`) {
		t.Errorf("main.go does not import the app under its hyphenated name:\n%s", main)
	}
}

// F9 end to end: a fresh scaffold's action file carries the build
// constraint, the gen copy doesn't, and --check passes as scaffolded.
func TestNewScaffoldsTaggedActions(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runNew([]string{"tagapp"}); err != nil {
		t.Fatalf("runNew: %v", err)
	}
	src, err := os.ReadFile(filepath.Join("tagapp", "actions", "index.GET.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "//go:build rastrillo_actions") {
		t.Errorf("scaffolded action lacks the build constraint:\n%s", src)
	}
	gen, err := os.ReadFile(filepath.Join("tagapp", "gen", "actions", "index_get", "index.GET.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(gen), "//go:build") {
		t.Errorf("gen copy kept the build constraint:\n%s", gen)
	}
	if err := runGenerate([]string{"--check", "tagapp"}); err != nil {
		t.Errorf("--check must pass on a fresh scaffold: %v", err)
	}
}
