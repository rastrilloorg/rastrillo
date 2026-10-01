package ui

import (
	"regexp"
	"strings"
	"testing"
)

// touchQuery is the one media query every touch-only rule sits in (spec
// §1.1). Written once so a typo in one floor cannot quietly scope it to
// a query nothing matches.
const touchQuery = "@media (pointer: coarse), (max-width: 40rem) {"

// zoomFloorRule finds a floor: the touch query holding exactly one rule
// whose only declaration is the floor. Read off comment-stripped CSS.
var zoomFloorRule = regexp.MustCompile(`@media \(pointer: coarse\), \(max-width: 40rem\) \{\s*([^{}]+?)\s*\{\s*font-size: max\(1rem, 1em\);\s*\}\s*\}`)

// bareZoomFloor is the zero-weight floor for an app's own inputs: an
// input with no rst- attribute is sized by the UA stylesheet, which any
// author rule beats, so :where() still lifts it and any app rule that
// sizes it still wins.
const bareZoomFloor = ":where(input:not([type=checkbox]):not([type=radio]):not([type=range]), select, textarea)"

// TestTheZoomFloorsSitWhereTheyCannotBeResetOrWin is §1.3's position
// gate. Each component floor must come DIRECTLY after the rule that
// resets its font (`font: inherit` resets font-size, so a floor before
// it is switched off — round 1's textarea finding) and, for the input,
// before the rule that makes the primary field big (at equal weight the
// later rule wins, and max(1rem, 1em) would pull the 19px field down to
// 16px — the 1em trap this rewrite exists to remove). Moving any floor
// one rule up or down fails here.
func TestTheZoomFloorsSitWhereTheyCannotBeResetOrWin(t *testing.T) {
	css := stripCSSComments(string(TokensCSS()))
	floors := map[string][2]int{}
	for _, m := range zoomFloorRule.FindAllStringSubmatchIndex(css, -1) {
		sel := collapseSpace(css[m[2]:m[3]])
		if _, dup := floors[sel]; dup {
			t.Errorf("two zoom floors for %q", sel)
		}
		floors[sel] = [2]int{m[0], m[1]}
	}
	if len(floors) != 4 {
		t.Errorf("tokens.css carries %d zoom floors, want 4 (bare elements, input, textarea, search): %v", len(floors), floors)
	}
	if _, ok := floors[bareZoomFloor]; !ok {
		t.Errorf("no bare-element floor spelled %q", bareZoomFloor)
	}
	if !strings.HasPrefix(bareZoomFloor, ":where(") || !strings.HasSuffix(bareZoomFloor, ")") || specificity(bareZoomFloor) != [3]int{} {
		t.Errorf("the bare-element floor is not wholly inside :where(): %v", specificity(bareZoomFloor))
	}
	base := func(prelude string) (start, end int) {
		loc := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(prelude) + ` \{`).FindStringIndex(css)
		if loc == nil {
			t.Fatalf("tokens.css has no top-level rule %q", prelude)
		}
		close := strings.IndexByte(css[loc[1]:], '}')
		return loc[0], loc[1] + close + 1
	}
	for _, c := range []struct{ floor, after, before string }{
		{".rst-input, [rst-input]", ".rst-input, [rst-input]", `.rst-input--primary, [rst-input~="primary"]`},
		{".rst-textarea, [rst-textarea]", ".rst-textarea, [rst-textarea]", ""},
		{`.rst-search input[type="search"], [rst-search] input[type="search"]`, `.rst-search input[type="search"], [rst-search] input[type="search"]`, ""},
	} {
		at, ok := floors[c.floor]
		if !ok {
			t.Errorf("no zoom floor for %q", c.floor)
			continue
		}
		_, end := base(c.after)
		// A floor above its reset is the dead-floor case itself; slicing
		// the gap backwards would panic instead of naming it.
		if at[0] < end {
			t.Errorf("the floor for %q is not directly after the rule that resets its font: it comes before it, so `font: inherit` switches it off", c.floor)
		} else if gap := strings.TrimSpace(css[end:at[0]]); gap != "" {
			t.Errorf("the floor for %q is not directly after the rule that resets its font; between them:\n%.200s", c.floor, gap)
		}
		if c.before != "" {
			if start, _ := base(c.before); at[1] > start {
				t.Errorf("the floor for %q comes after %q, so it overrides the field that is big on purpose", c.floor, c.before)
			}
		}
	}
	// The block this replaces: one pointer-only query sizing the
	// components at a weight that beat the primary field.
	if strings.Contains(css, "@media (pointer: coarse) {") {
		t.Error("tokens.css still has a pointer-only query; every touch rule uses the one query of §1.1")
	}
}

// The type step itself (§1.2), read as text so a desktop-only edit
// cannot move it: the four tokens, in the touch query, at :root.
func TestTheTypeScaleMovesUpOneStepOnSmallOrTouchScreens(t *testing.T) {
	css := stripCSSComments(string(TokensCSS()))
	re := regexp.MustCompile(regexp.QuoteMeta(touchQuery) + `\s*:root\s*\{([^{}]*)\}`)
	m := re.FindStringSubmatch(css)
	if m == nil {
		t.Fatal("no :root block inside the touch query")
	}
	for _, want := range []string{"--rst-fs-lg: 1.1875rem;", "--rst-fs-base: 1rem;", "--rst-fs-sm: 0.875rem;", "--rst-fs-xs: 0.8125rem;"} {
		if !strings.Contains(m[1], want) {
			t.Errorf("the touch :root block lacks %q:\n%s", want, m[1])
		}
	}
	if !regexp.MustCompile(`--rst-tap:\s*2\.75rem;`).MatchString(css) {
		t.Error("--rst-tap: 2.75rem is not declared")
	}
	// The two components that spelled the xs step as a literal paint
	// with the token now, or they would stay 11.5px on a phone.
	for _, sel := range []string{".rst-lrow--head, [rst-lrow~=\"head\"]", ".rst-tip::after, [rst-tip]::after"} {
		loc := strings.Index(css, sel+" {")
		if loc < 0 {
			t.Fatalf("no rule %q", sel)
		}
		body := css[loc : loc+strings.IndexByte(css[loc:], '}')]
		if strings.Contains(body, "0.71875rem") || !strings.Contains(body, "font-size: var(--rst-fs-xs)") {
			t.Errorf("%s does not paint with var(--rst-fs-xs): %s", sel, body)
		}
	}
}
