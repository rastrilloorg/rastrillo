package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosframework/rastrillo/ui"
)

// A small app tree with one of everything the sweep has an opinion
// about: a template, a Go file with markup in a string literal, the
// app's own stylesheet, and a vendored file it must not touch.
func markupFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("templates/list.html", `<div class="rst-card">
  <div class="rst-lrow rst-lrow--head"><span class="rst-m-hide">Status</span></div>
  <div class="rst-form-foot"><span class="rst-form-foot__note">Saves.</span></div>
</div>`)
	write("internal/app/render.go", "package app\n\nconst row = \"<a class=\\\"rst-btn rst-btn--primary\\\">Go</a>\"\n")
	write("static/app.css", ".rst-lrow > a { color: red }\n.mine { color: blue }\n")
	write("static/tokens.css", string(ui.TokensCSS()))
	write("README.md", "no markup here\n")
	return dir
}

func TestMarkupSweepReportsBeforeItWrites(t *testing.T) {
	dir := markupFixture(t)
	before, err := os.ReadFile(filepath.Join(dir, "templates", "list.html"))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = markupSweep(&out, dir, false)
	var ex exitError
	if !asExit(err, &ex) || ex.code != exitMarkupPending {
		t.Fatalf("a dry run with work to do must exit %d, got %v", exitMarkupPending, err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "templates", "list.html"))
	if !bytes.Equal(before, after) {
		t.Error("a run without --fix wrote to the tree")
	}
	for _, want := range []string{"templates/list.html", "internal/app/render.go", "--fix"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the report does not mention %q:\n%s", want, out.String())
		}
	}
}

func TestMarkupSweepFixesAndIsIdempotent(t *testing.T) {
	dir := markupFixture(t)
	var out bytes.Buffer
	if err := markupSweep(&out, dir, true); err != nil {
		t.Fatalf("--fix: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "templates", "list.html"))
	want := `<div rst-card>
  <div rst-lrow="head"><span class="rst-m-hide">Status</span></div>
  <div rst-form-bar><span rst-form-bar-note>Saves.</span></div>
</div>`
	if string(got) != want {
		t.Errorf("template:\n got %s\nwant %s", got, want)
	}
	gotGo, _ := os.ReadFile(filepath.Join(dir, "internal", "app", "render.go"))
	if !strings.Contains(string(gotGo), `\"primary\"`) {
		t.Errorf("a Go string literal's markup keeps its own quoting:\n%s", gotGo)
	}

	// The vendored copy is doctor's, never this tool's: rewriting it
	// would make the app's copy differ from the library's for good.
	vend, _ := os.ReadFile(filepath.Join(dir, "static", "tokens.css"))
	if !bytes.Equal(vend, ui.TokensCSS()) {
		t.Error("static/tokens.css was rewritten; it is doctor's file, not this tool's")
	}

	var second bytes.Buffer
	if err := markupSweep(&second, dir, true); err != nil {
		t.Fatalf("second --fix: %v", err)
	}
	if !strings.Contains(second.String(), "0 file(s)") {
		t.Errorf("the second pass had work to do, so the rewrite is not idempotent:\n%s", second.String())
	}
}

// The app's own stylesheet is the trap: a rule written against
// .rst-lrow stops matching the moment the markup says rst-lrow, and
// nothing in the app fails.
func TestMarkupSweepNamesTheAppsOwnClassSelectors(t *testing.T) {
	dir := markupFixture(t)
	var out bytes.Buffer
	_ = markupSweep(&out, dir, false)
	if !strings.Contains(out.String(), "static/app.css: rst-lrow") &&
		!strings.Contains(out.String(), `static\app.css: rst-lrow`) {
		t.Errorf("the app's own .rst-lrow rule is not reported:\n%s", out.String())
	}
	if strings.Contains(out.String(), "tokens.css: rst-") {
		t.Errorf("the vendored stylesheet is reported as the app's own:\n%s", out.String())
	}
}

func asExit(err error, out *exitError) bool {
	for err != nil {
		if e, ok := err.(exitError); ok {
			*out = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
