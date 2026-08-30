package rastrillo

import (
	"html/template"
	"strings"
	"testing"
)

// Every vendored icon is one self-contained, colour-inheriting,
// screen-reader-silent 24x24 SVG. Mirrors
// carlosframework/platform's internal/console/icons_test.go
// TestVendoredIconsAreSelfContained.
func TestVendoredIconsAreSelfContained(t *testing.T) {
	if len(icons) == 0 {
		t.Fatal("the icon set is empty")
	}
	for slug, svg := range icons {
		s := string(svg)
		for _, want := range []string{
			"<svg", "</svg>", `viewBox="0 0 24 24"`,
			`stroke="currentColor"`, `aria-hidden="true"`, `class="icon"`,
		} {
			if !strings.Contains(s, want) {
				t.Errorf("icon %q is missing %s: %s", slug, want, s)
			}
		}
		// Real vector content, not an empty shell -- most icons draw in
		// <path>, but kebab (Lucide's three-dot "more-vertical") is drawn
		// entirely in <circle>, so either primitive satisfies this.
		if !strings.Contains(s, "<path") && !strings.Contains(s, "<circle") {
			t.Errorf("icon %q has no drawing primitive (<path> or <circle>): %s", slug, s)
		}
		for _, bad := range []string{"http://", "https://", "<image", "xlink:href", "url("} {
			if strings.Contains(s, bad) {
				t.Errorf("icon %q reaches outside the page (%q): %s", slug, bad, s)
			}
		}
		if strings.Contains(s, " width=") || strings.Contains(s, " height=") {
			t.Errorf("icon %q hardcodes a size instead of sizing from the caller's CSS: %s", slug, s)
		}
	}
}

// An unknown slug must not panic a caller mid-render -- it renders nothing,
// same rule as console's icon().
func TestIconUnknownSlugRendersNothing(t *testing.T) {
	if got := Icon("not-a-real-icon"); got != "" {
		t.Errorf("Icon(%q) = %q, want empty string", "not-a-real-icon", got)
	}
}

// All twelve expected icon slugs are registered and non-empty.
func TestExpectedIconSlugsRegistered(t *testing.T) {
	expected := []string{
		"chevron-down", "check", "plus", "search",
		"kebab", "x", "info", "check-circle", "alert-triangle", "x-circle", "help-circle",
		"menu",
	}
	for _, slug := range expected {
		if got := Icon(slug); got == "" {
			t.Errorf("Icon(%q) is empty or not registered", slug)
		}
	}
}

// The seven display-partial icons resolve to non-empty, well-formed SVG
// and stay silent to assistive tech, identically to the original four —
// TestVendoredIconsAreSelfContained already covers shape and self-
// containment for every entry in icons, this asserts the specific new
// names exist with the same aria-hidden contract.
func TestNewIconsResolveWithAriaHidden(t *testing.T) {
	for _, slug := range []string{
		"kebab", "x", "info", "check-circle", "alert-triangle", "x-circle", "help-circle",
	} {
		got := string(Icon(slug))
		if got == "" {
			t.Errorf("Icon(%q) is empty", slug)
			continue
		}
		if !strings.Contains(got, "<svg") || !strings.Contains(got, `aria-hidden="true"`) {
			t.Errorf("Icon(%q) missing <svg> or aria-hidden: %s", slug, got)
		}
	}
	// An unknown slug still behaves as before: empty, no panic.
	if got := Icon("not-a-real-icon"); got != "" {
		t.Errorf("Icon(%q) = %q, want empty string", "not-a-real-icon", got)
	}
}

// menu is navigation and kebab is "more actions on this row". They are
// two different words in this vocabulary, so they have to be two
// different glyphs: reusing kebab for the shells' collapsed navigation
// was the cheap option and was ruled out precisely because it would
// have made the distinction unreadable at the only place a reader meets
// both. A future edit that points one at the other fails here.
func TestMenuAndKebabAreDifferentGlyphs(t *testing.T) {
	menu, kebab := string(Icon("menu")), string(Icon("kebab"))
	if menu == "" {
		t.Fatal(`Icon("menu") is empty: the shells' collapsed navigation has no icon`)
	}
	if menu == kebab {
		t.Error(`Icon("menu") and Icon("kebab") render the same glyph; navigation and "more actions" must stay distinguishable`)
	}
	// Three full-width horizontal strokes, which is what a hamburger is
	// and what neither kebab (three dots) nor any other slug draws.
	if n := strings.Count(menu, `<path d="M4 `); n != 3 {
		t.Errorf(`Icon("menu") draws %d horizontal strokes, want the three of a hamburger: %s`, n, menu)
	}
}

// The actual value proposition: Icon must work as a real html/template
// FuncMap entry, end to end -- not just type-check as template.HTML.
func TestIconWorksAsTemplateFunc(t *testing.T) {
	tmpl := template.Must(
		template.New("t").Funcs(template.FuncMap{"icon": Icon}).Parse(`{{icon "check"}}`),
	)
	var buf strings.Builder
	if err := tmpl.Execute(&buf, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "<svg") || !strings.Contains(got, `aria-hidden="true"`) {
		t.Errorf("rendered template output missing expected icon markup: %s", got)
	}
}

// IconSlugs must describe the map exactly: it is what internal/iconsets
// checks every scaffoldable set against, so a slug missing here is a set
// that silently stops covering the framework's own vocabulary.
func TestIconSlugsMatchesTheMap(t *testing.T) {
	got := IconSlugs()
	if len(got) != len(icons) {
		t.Fatalf("IconSlugs() has %d entries, the map has %d", len(got), len(icons))
	}
	for _, slug := range got {
		if Icon(slug) == "" {
			t.Errorf("IconSlugs() lists %q, which Icon does not answer", slug)
		}
	}
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Errorf("IconSlugs() is not sorted: %q before %q", got[i-1], got[i])
		}
	}
}
