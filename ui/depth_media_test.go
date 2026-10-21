package ui

import (
	"strings"
	"testing"
)

// The two media blocks that undo the depth language where it would do
// harm (spec C6, C17, A17). Each flattens the axis at html:root, so no
// state rule can keep a shadow, a gradient, a tint or a drop; and each
// names the components that need a border or a system colour the
// tokens cannot give them. A token or a component left out keeps a
// shadow forced colours would strip half of, or a tint a printer would
// drop. The summary's edge is in the flat list too: it is a ring drawn
// in the shadow by rules at (0,4,1) and up, so a box-shadow: none at
// the summary's own selector never reaches it, and the token is the
// one thing that wins in every state.
func TestForcedColoursAndPrintFlattenTheAxisAndCoverEveryComponent(t *testing.T) {
	css := string(TokensCSS())
	flat := []string{"--rst-depth-hl: transparent", "--rst-depth-contact: transparent", "--rst-depth-ambient: transparent", "--rst-depth-face: 0", "--rst-press-drop: 0px",
		"--rst-sheen-fill: transparent", "--rst-sheen-plain: transparent", "--rst-light-wash: transparent", "--rst-light-rim: var(--rst-line)",
		"--rst-press-wash: 0", "--rst-press-spot: 0", "--rst-press-wash-plain: 0", "--rst-press-spot-plain: 0",
		"--rst-tint-hover: var(--rst-surface)", "--rst-tint-press: var(--rst-surface)", "--rst-tint-tap: transparent", "--rst-row-tint-hover: var(--rst-surface)", "--rst-row-tint-press: var(--rst-surface)", "--rst-focus-halo: transparent",
		"--rst-summary-edge: transparent", "--rst-summary-edge-hover: transparent"}
	printEnd := 0
	for _, block := range []struct {
		header string
		want   []string
	}{
		{"@media (forced-colors: active) {", append(flat, "html:root {", "[rst-btn]", "[rst-box]", "[rst-card]", "[rst-stats]", "[rst-list]", "[rst-input]", "[rst-textarea]", "[rst-search]", "[rst-dropdown]", "[rst-switch-track]", "[rst-switch-track]::after",
			"[rst-shell-sidebar] [rst-shell-nav] a[aria-current]", "[rst-shell-console] [rst-shell-nav] a[aria-current]", "[rst-shell-topbar] [rst-shell-tail] > [rst-shell-nav] a[aria-current]", "[rst-dropdown-menu] a[aria-current]", "[rst-pagination] [aria-current=\"page\"]", "[rst-seg-tabs] a[aria-current]",
			`[rst-shell-sidebar][rst-shell-sidebar~="index"] [rst-shell-nav] > a[aria-current]:hover`, `[rst-shell-console][rst-shell-console~="index"] [rst-shell-nav] > a[aria-current]:active`,
			"ButtonBorder", "CanvasText", "ButtonText", "Highlight", "HighlightText", "box-shadow: none")},
		{"@media print {", append(flat, "html:root {", "--rst-accent-fill: transparent", "--rst-accent-fill-hover: transparent", "--rst-on-accent-fill: var(--rst-text)", "--rst-danger-fill: transparent", "--rst-on-danger-fill-hover: var(--rst-text)", "--rst-danger-hover-filter: none", "--rst-control-face-hover: transparent", "--rst-control-text-hover: var(--rst-text)", "--rst-knob: var(--rst-text)",
			"[rst-btn]", "[rst-box]", "[rst-card]", "[rst-stats]", "[rst-list]", "[rst-input]", "[rst-textarea]", "[rst-search]", "[rst-dropdown]", "[rst-switch-track]", "[rst-switch-track]::after", "[rst-switch]:has(input:checked) [rst-switch-track]", "[rst-tblock]:has([rst-tblock-head] input:checked) [rst-switch-track]", "[rst-seg-tabs] a[aria-current]", "[rst-pagination] [aria-current=\"page\"]", "[rst-dropdown-menu]", "[rst-row-menu-panel]",
			"[rst-shell-sidebar] [rst-shell-nav] a[aria-current]", "[rst-shell-console] [rst-shell-nav] a[aria-current]", "var(--rst-line-strong)", "background-image: none", "box-shadow: none", "outline: 1px solid var(--rst-line-strong)")},
	} {
		i := strings.LastIndex(css, block.header)
		if i < 0 {
			t.Fatalf("tokens.css has no %s block at the end of the file", block.header)
		}
		body := blockBody(t, "tokens.css", css, block.header, i)
		if block.header == "@media print {" {
			printEnd = i + len(block.header) + len(body) + 1
		}
		for _, w := range block.want {
			if !strings.Contains(body, w) {
				t.Errorf("the %s block does not carry %q", block.header, w)
			}
		}
		if strings.Contains(body, "!important") {
			t.Errorf("the %s block uses !important (C11)", block.header)
		}
	}
	// Nothing that paints a button, a card or a nav item may follow the
	// print block: its rules win by order. The search starts after the
	// block's closing brace, because the block names those selectors
	// itself.
	for _, sel := range []string{`[rst-btn~="primary"]`, `[rst-shell-nav] a[aria-current]`, `[rst-box]`} {
		if strings.Contains(css[printEnd:], sel+" {") || strings.Contains(css[printEnd:], sel+",") {
			t.Errorf("%q is styled after the print block; the print rules rely on source order", sel)
		}
	}
}
