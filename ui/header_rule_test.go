package ui

import (
	"regexp"
	"strings"
	"testing"
)

// The rake line is retired (design doc §6-v2.2), and "retired" has to
// mean one place. tokens.css is the single stylesheet every rastrillo
// page links, so a pseudo-element declared there once, as content:none,
// is a pseudo-element that draws nothing anywhere — provided nothing
// else in the shipped CSS declares one too.
//
// That last clause is what this test is for. A theme is colour, type
// and shape; a theme that grew a [rst-page-header]::after would put the
// flourish back on one theme and nowhere else, which is both the
// hardest version of this bug to see and the easiest one to write.
//
// The browser half of the claim — that content:none lays out no box, in
// every theme, in both schemes, in ar as well as en — is
// header_rule_browser_test.go's. This is the cheap half, and it runs in
// the ordinary suite.
func TestTheRakeLineIsDeclaredRetiredExactlyOnce(t *testing.T) {
	// The rule as it now stands, spelled in both markup vocabularies
	// because tokens.css pairs a class selector with every attribute one
	// until stage 3 of the migration.
	const retired = ".rst-page-header::after, [rst-page-header]::after {\n  content: none;\n}"
	css := string(TokensCSS())
	if got := strings.Count(css, retired); got != 1 {
		t.Errorf("tokens.css declares the retired header pseudo-element %d times as\n%s\nwant exactly 1 — the whole point of retiring it in one file is that there is one place to look", got, retired)
	}

	// Nothing else may draw one. The pattern is deliberately loose (any
	// selector naming a page header, any pseudo-element on it) so a
	// flourish reintroduced under ::before, or on the titles, is caught
	// as readily as the exact rule that was removed. It stops at a comma
	// so a selector LIST is read as the selectors it is, not as one long
	// string that happens to start and end in the right places.
	headerPseudo := regexp.MustCompile(`(\.rst-page-header|\[rst-page-header)[^{;,]*::(after|before)`)
	for _, m := range headerPseudo.FindAllString(css, -1) {
		if strings.TrimSpace(m) == ".rst-page-header::after" || strings.TrimSpace(m) == "[rst-page-header]::after" {
			continue
		}
		t.Errorf("tokens.css draws %q on the page header; the header's one decoration is its rule, and that lives on the theme axis as --rst-header-rule", m)
	}
	for _, name := range ThemeNames() {
		theme, ok := ThemeCSS(name)
		if !ok {
			t.Fatalf("ThemeCSS(%q) missing", name)
		}
		// A theme file has no selectors at all — it is one :root block
		// and two toggle rules — so any mention of the header at all is
		// the beginning of this bug.
		if strings.Contains(string(theme), "page-header") {
			t.Errorf("themes/%s.css mentions the page header. A theme sets --rst-header-rule and stops there: structure is tokens.css's, and a theme that styles a component has started a second stylesheet", name)
		}
	}
}
