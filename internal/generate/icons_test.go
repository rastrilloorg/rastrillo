package generate

import (
	"os"
	"path/filepath"
	"testing"

	"amadan.net/rastrillo/rastrillo"
)

func writeApp(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const iconsPkg = "package icons\n\nimport \"html/template\"\n\n" +
	"var icons = map[string]template.HTML{\n" +
	"\t\"check\":  `<svg/>`,\n" +
	"\t\"search\": `<svg/>`,\n" +
	"}\n\n" +
	"func Icon(slug string) template.HTML { return icons[slug] }\n"

func TestKnownSlugsReadsTheAppsOwnPackage(t *testing.T) {
	dir := writeApp(t, map[string]string{"internal/app/icons/icons.go": iconsPkg})
	got, err := KnownSlugs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !got["check"] || !got["search"] {
		t.Errorf("did not read the app's slugs: %v", got)
	}
	if got["plus"] {
		t.Error("invented a slug the app does not define")
	}
}

// An app that never scaffolded its own package still gets checked,
// against the framework's built-in set.
func TestKnownSlugsFallsBackToTheFrameworkSet(t *testing.T) {
	dir := writeApp(t, map[string]string{})
	got, err := KnownSlugs(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, slug := range rastrillo.IconSlugs() {
		if !got[slug] {
			t.Errorf("framework fallback is missing %q", slug)
		}
	}
	if len(got) != len(rastrillo.IconSlugs()) {
		t.Errorf("fallback has %d slugs, the framework answers %d", len(got), len(rastrillo.IconSlugs()))
	}
}

func TestUnknownIconSlugsFindsTypos(t *testing.T) {
	dir := writeApp(t, map[string]string{
		"internal/app/icons/icons.go": iconsPkg,
		"templates/list.html": "<h1>Posts</h1>\n" +
			"{{icon \"search\"}}\n" +
			"{{icon \"serach\"}}\n",
	})
	got, err := UnknownIconSlugs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want exactly the one typo, got %v", got)
	}
	if got[0].Slug != "serach" {
		t.Errorf("wrong slug reported: %+v", got[0])
	}
	if got[0].Line != 3 {
		t.Errorf("wrong line reported: %+v", got[0])
	}
}

// Whitespace and trim-marker variants are all real calls.
func TestUnknownIconSlugsMatchesSpacingVariants(t *testing.T) {
	dir := writeApp(t, map[string]string{
		"internal/app/icons/icons.go": iconsPkg,
		"templates/a.html":            `{{icon "nope1"}}{{ icon "nope2" }}{{-  icon  "nope3"  -}}`,
	})
	got, err := UnknownIconSlugs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 findings across spacing variants, got %v", got)
	}
}

// Templates colocated with the package that renders them are scanned
// too — examples/blog keeps them under internal/blog/templates, and a
// gate that only looked at templates/ would silently pass it.
func TestUnknownIconSlugsWalksNestedDirs(t *testing.T) {
	dir := writeApp(t, map[string]string{
		"internal/app/icons/icons.go":         iconsPkg,
		"internal/app/templates/admin/x.html": `{{icon "nope"}}`,
	})
	got, err := UnknownIconSlugs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("nested template not scanned: %v", got)
	}
}

// An app with no templates at all is fine, not an error.
func TestUnknownIconSlugsWithNoTemplates(t *testing.T) {
	dir := writeApp(t, map[string]string{"internal/app/icons/icons.go": iconsPkg})
	got, err := UnknownIconSlugs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("want no findings, got %v", got)
	}
}

// gen/ is generated output, rewritten on every generate. A finding there
// would name a file the developer must not edit.
func TestUnknownIconSlugsSkipsGeneratedOutput(t *testing.T) {
	dir := writeApp(t, map[string]string{
		"internal/app/icons/icons.go": iconsPkg,
		"gen/copied.html":             `{{icon "nope"}}`,
	})
	got, err := UnknownIconSlugs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("gen/ was scanned: %v", got)
	}
}

// The idiom this repository actually uses: the slug reaches {{icon}} as
// data, through a partial's dict. A gate that only saw bare {{icon "x"}}
// calls would miss almost every real icon reference.
func TestUnknownIconSlugsFindsSlugsPassedAsPartialData(t *testing.T) {
	dir := writeApp(t, map[string]string{
		"internal/app/icons/icons.go": iconsPkg,
		"templates/list.html": `{{template "page-header" dict "Title" "Posts" "ActionIcon" "pluss"}}` + "\n" +
			`{{template "page-header" dict "Title" "Ok" "ActionIcon" "check"}}`,
	})
	got, err := UnknownIconSlugs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want the one bad slug, got %v", got)
	}
	if got[0].Slug != "pluss" {
		t.Errorf("wrong slug: %+v", got[0])
	}
}

// "" means "no icon here", not a slug that failed to resolve.
func TestUnknownIconSlugsIgnoresTheEmptySlug(t *testing.T) {
	dir := writeApp(t, map[string]string{
		"internal/app/icons/icons.go": iconsPkg,
		"templates/a.html":            `{{template "page-header" dict "ActionIcon" ""}}`,
	})
	got, err := UnknownIconSlugs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("an empty slug was reported: %v", got)
	}
}
