package main

// markup-spelling: old-spelling begin — the fixture app is written in
// the spelling the tool converts.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/carlosframework/rastrillo/internal/markup"
	"github.com/carlosframework/rastrillo/ui"
)

// markdownAboutTheMigration is the fixture's README: prose whose
// subject is the two spellings, of the kind every repository upgrading
// through this release writes. Every line of it would be rewritten if
// .md were scanned, which is what makes it a control rather than a
// decoration — see TestMarkupSweepLeavesMarkdownAlone.
const markdownAboutTheMigration = `# Upgrading

- **Write ` + "`class=\"rst-box\"`" + ` today.** The old spelling still styles.

Once the dual-grammar release ships, ` + "`class=\"rst-box\"`" + ` and ` + "`<div rst-box>`" + `
are identical, which is what makes the gap safe to sit in.

| Was | Is |
|---|---|
| ` + "`class=\"rst-form-foot\"`" + ` | ` + "`rst-form-bar`" + ` |
`

// A small app tree with one of everything the sweep has an opinion
// about: a template, a Go file with markup in a string literal, the
// app's own stylesheet, a vendored file it must not touch, and a
// Markdown file that talks about markup rather than containing any.
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
	write("README.md", markdownAboutTheMigration)
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

// Markdown is not scanned, and this is the test that says so — with a
// fixture that would be rewritten if it were.
//
// The shipped v0.22.0 had .md in the migratable set and this fixture
// read "no markup here": a control that could not fail, which is the
// failure class of design spec §7-v2. What it cost is in the second
// assertion below. A repository documenting the migration says
// "class=\"rst-box\" and <div rst-box> are identical" to teach the
// distinction, and the tool rewrote that into a sentence claiming two
// identical-looking things are identical — destroyed, invisible, and
// the diff looked plausible.
func TestMarkupSweepLeavesMarkdownAlone(t *testing.T) {
	// First: the fixture is live wire. If a later edit makes this
	// Markdown something the codemod would not touch, the rest of this
	// test proves nothing and this line is what says so.
	if out, _ := markup.Rewrite([]byte(markdownAboutTheMigration)); string(out) == markdownAboutTheMigration {
		t.Fatal("the Markdown fixture is inert: the codemod would not rewrite it even if .md were scanned, " +
			"so it cannot show that .md is skipped")
	}

	dir := markupFixture(t)
	var out bytes.Buffer
	if err := markupSweep(&out, dir, true); err != nil {
		t.Fatalf("--fix: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != markdownAboutTheMigration {
		t.Errorf("--fix rewrote a Markdown file:\n got %s\nwant %s", got, markdownAboutTheMigration)
	}
	if strings.Contains(out.String(), "README.md") {
		t.Errorf("Markdown is counted as a file with markup to migrate:\n%s", out.String())
	}
}

// "N file(s) would be rewritten" is the number people size the
// migration by, so it has to be a count of files with markup in them.
// With .md scanned it counted documentation about markup too — the
// Sheets team got exit 3 and "2 file(s) would be rewritten" against
// repositories holding no templates at all.
func TestMarkupSweepCountsOnlyFilesWithMarkup(t *testing.T) {
	dir := markupFixture(t)
	var out bytes.Buffer
	_ = markupSweep(&out, dir, false)
	if !strings.Contains(out.String(), "2 file(s) would be rewritten") {
		t.Errorf("the fixture holds markup in exactly two files, the template and the Go source:\n%s", out.String())
	}

	// And a tree that is nothing but prose about the migration is a
	// tree with no work in it: exit 0, not exit 3.
	docs := t.TempDir()
	if err := os.WriteFile(filepath.Join(docs, "UPGRADING.md"), []byte(markdownAboutTheMigration), 0o644); err != nil {
		t.Fatal(err)
	}
	var prose bytes.Buffer
	if err := markupSweep(&prose, docs, false); err != nil {
		t.Errorf("a repository of documentation has no markup to migrate, so it exits 0: %v\n%s", err, prose.String())
	}
	if !strings.Contains(prose.String(), "0 file(s)") {
		t.Errorf("documentation about markup is counted as markup:\n%s", prose.String())
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

// The report's second half — "your own stylesheet keys off the class
// spelling" — had the Markdown defect in miniature, in Go.
//
// styleBlock is a regexp. It is line-blind and it does not know a
// comment from a quote, so a file that MENTIONS <style> in a comment
// and writes </style> inside a pattern further down handed it
// everything in between as the app's own stylesheet. markup.go was
// that file: about a hundred lines of it read as CSS, and the
// .rst-lrow in its own help text came back to the reader as a selector
// to go and change. Prose about markup, read as markup.
//
// Both halves are asserted here, because the cheap way to make the
// first one pass is to stop looking at Go files at all.
func TestMarkupSweepReadsAStyleBlockInGoOnlyWhereOneCanBe(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Discussion: the opening tag is in a comment, the closing tag is
	// in an unrelated literal, and the selector between them is a
	// sentence about what the reader should type.
	write("report.go", "package app\n\n"+
		"// Every <style> block in a template is read too, because the\n"+
		"// app's own rules are the trap wherever they are written.\n\n"+
		"const help = \"change .rst-lrow to [rst-lrow]\"\n\n"+
		"const closingTag = \"</style>\"\n")
	// Markup: one string literal holding a whole <style> block, which
	// is what a Go file with a stylesheet in it actually looks like.
	write("page.go", "package app\n\n"+
		"const page = \"<style>.rst-lgrid { display: grid }</style>\"\n")

	var out bytes.Buffer
	_ = markupSweep(&out, dir, false)

	if strings.Contains(out.String(), "report.go") {
		t.Errorf("a <style> in a Go comment made the file's prose read as the app's stylesheet:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "page.go: rst-lgrid") {
		t.Errorf("a real <style> block in a Go string literal is no longer reported:\n%s", out.String())
	}
}

// A dry run that finds only class lists it cannot take apart must not
// exit 0. A CI gate reading that code would wave through the one app
// that most needs stopping: the one whose remaining class markup is
// entirely in shapes this reports rather than rewrites.
func TestMarkupSweepDoesNotWaveThroughWhatItCannotRewrite(t *testing.T) {
	dir := t.TempDir()
	// Markup built by concatenation: reported, never rewritten.
	if err := os.WriteFile(filepath.Join(dir, "render.go"),
		[]byte("package app\n\nfunc row(kind string) string { return \"<div class=\\\"rst-\" + kind + \"\\\">x</div>\" }\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, "render.go"))

	for _, fix := range []bool{false, true} {
		var out bytes.Buffer
		err := markupSweep(&out, dir, fix)
		var ex exitError
		if !asExit(err, &ex) || ex.code != exitMarkupPending {
			t.Errorf("--fix=%v: exit %v, want %d — a note is work left", fix, err, exitMarkupPending)
		}
		if !strings.Contains(out.String(), "need a human") {
			t.Errorf("--fix=%v: the report does not say what is left:\n%s", fix, out.String())
		}
		after, _ := os.ReadFile(filepath.Join(dir, "render.go"))
		if !bytes.Equal(before, after) {
			t.Fatalf("--fix=%v rewrote source it cannot read:\n%s", fix, after)
		}
	}
}

// The app's own rules are a trap wherever they are written, and a
// <style> block in the layout template is the same trap with a
// different file extension.
func TestMarkupSweepReadsAStyleBlockToo(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "layout.html"),
		[]byte("<style>.rst-lrow > a { color: red }</style>\n<div class=\"rst-box\">x</div>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	_ = markupSweep(&out, dir, false)
	if !strings.Contains(out.String(), "layout.html: rst-lrow") {
		t.Errorf("a .rst- rule inside a <style> block is not reported:\n%s", out.String())
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
