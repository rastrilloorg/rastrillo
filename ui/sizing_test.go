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

// tapEntry is one row of spec §1.4's table: Key is a substring of the
// attribute spelling of the rules that style the idiom, Marker a string
// the sizing fixture (or the modal sample) must contain for the idiom to
// be measured at all (for controls a script draws at run time, the
// attribute that asks for them), and Probe the selector of the element
// the target drive measures for it. The drive fails if no element
// matching Probe was measured, so an idiom cannot drop out of the
// measurement silently. Probe may be prefixed "narrow:" (rendered only
// below 800px, so not expected at 1024), "modal:" (on the modal page),
// or be "elsewhere:<Test>" for a control a drive of its own measures in
// a state the sizing page never reaches.
type tapEntry struct{ Key, Idiom, Marker, Probe string }

var tapInventory = []tapEntry{
	{"[rst-btn]", "Buttons, all sizes", "rst-btn", "[rst-btn]"},
	{"[rst-input]", "Inputs and selects", "rst-input", "[rst-input]"},
	{"[rst-textarea]", "Textareas", "rst-textarea", "[rst-textarea]"},
	{"[rst-search]", "Search box", "rst-search", "[rst-search] input[type=search]"},
	{"[rst-search-clear]", "Search box: its clear link", "rst-search-clear", "[rst-search-clear]"},
	{"[rst-ftok] a", "Filter chip's remove link", "rst-ftok", "[rst-ftok] a"},
	{"[rst-help]", "Help link", "rst-help", "[rst-help]"},
	{"[rst-dropdown] > summary", "Dropdown summaries (list-bar, header, account, locale)", "rst-dropdown", "[rst-dropdown] > summary"},
	{"[rst-menu-group] > summary", "Nested menu-group summaries", "rst-menu-group", "[rst-menu-group] > summary"},
	{"[rst-dropdown-menu] a", "Menu items: dropdown", "rst-dropdown-menu", "[rst-dropdown-menu] a"},
	{"[rst-dropdown-menu] button", "Menu items: dropdown buttons", "rst-dropdown-menu", "[rst-dropdown-menu] button"},
	{"[rst-row-menu-panel] a", "Menu items: row menu", "rst-row-menu-panel", "[rst-row-menu-panel] a"},
	{"[rst-row-menu-panel] button", "Menu items: row menu buttons", "rst-row-menu-panel", "[rst-row-menu-panel] button"},
	{"[rst-locale] button", "Menu items: locale", "rst-locale", "[rst-locale] button"},
	{"[rst-combo-option]", "Combobox options", "data-rst-select", "[rst-combo-option]"},
	{"[rst-dtp-row]", "Date-picker rows", "data-rst-date", "[rst-dtp-row]"},
	{"[rst-dtp-pick]", "Date-picker pick button", "data-rst-date", "[rst-dtp-pick]"},
	{"[rst-cal-nav]", "Calendar nav", "data-rst-date", "elsewhere:TestTheCalendarDocksAndItsDaysAreTaps"},
	{"[rst-cal-day]", "Calendar days", "data-rst-date", "elsewhere:TestTheCalendarDocksAndItsDaysAreTaps"},
	{"[rst-row-action]", "Row action pill", "rst-row-action", "[rst-row-action]"},
	{"[rst-row-menu] > summary", "Row kebab", "rst-row-menu", "[rst-row-menu] > summary"},
	{"[rst-selbox]", "Row checkbox", "rst-selbox", "[rst-selbox] input"},
	{"[rst-person]", "Standalone person link", "rst-person", "a[rst-person]"},
	{"[rst-pagination] a", "Pagination chips", "rst-pagination", "[rst-pagination] a"},
	{"[rst-seg-tabs] a", "Segmented tabs", "rst-seg-tabs", "[rst-seg-tabs] a"},
	{"[rst-switch]", "Switch", "rst-switch", "[rst-switch] input"},
	{"[rst-choice-cards] label", "Choice cards", "rst-choice-cards", "[rst-choice-cards] input"},
	{"[rst-tblock-head]", "Toggle-block head", "rst-tblock-head", "[rst-tblock-head] input"},
	{"[rst-bulkbar-close]", "Bulk bar: close", "rst-bulkbar-close", "[rst-bulkbar-close]"},
	{"[rst-bulkbar-escalate]", "Bulk bar: escalate link", "rst-bulkbar-escalate", "[rst-bulkbar-escalate]"},
	{"[rst-modal-close]", "Modal close", "rst-modal-close", "modal:[rst-modal-close]"},
	{"[rst-modal-panel] > nav a", "Modal panel nav links", "rst-modal-panel", "modal:[rst-modal-panel] > nav a"},
	{"[rst-back-nav] a", "Back-nav link", "rst-back-nav", "[rst-back-nav] a"},
	{"[rst-shell-brand]", "Shell: brand", "rst-shell-brand", "[rst-shell-brand]"},
	{"[rst-shell-nav] a", "Shell: nav links", "rst-shell-nav", "[rst-shell-nav] a"},
	{"[rst-shell-menu] > summary", "Shell: Menu summary", "rst-shell-menu", "narrow:[rst-shell-menu] > summary"},
	{"[rst-shell-menu][open] > summary", "Shell: the open Menu summary", "rst-shell-menu", "elsewhere:TestTheCardsControlsAreTaps"},
	{"[rst-shell-account] > summary", "Shell: the account summary inside the card", "rst-shell-account", "[rst-shell-account] > summary"},
	{"[rst-shell-chrome] > summary", "Legacy sidebar drawer summary", "rst-shell-chrome", "narrow:[rst-shell-chrome] > summary"},
}

// tapExempt are the interactive rules that are deliberately not held to
// the floor, each with its reason. Nothing else may be added here: a
// control that does not fit the floor gets a rule in the touch block.
var tapExempt = map[string]string{
	"[rst-row-main] > a":         "the stretched primary link: the tap target is the whole row (spec §2)",
	"a.rst-nm":                   "the list grid's stretched identity link: the tap target is the whole row (spec §2)",
	"[rst-lrow] > a[rst-person]": "the list grid's stretched person link: the tap target is the whole row (spec §2)",
	".rst-lrow > a.rst-person":   "the class spelling of the same stretched person link",
	"[rst-no-match] a":           "a link in running text, WCAG 2.5.8's inline exception (spec §1.4, Exempt)",
	".rst-sr-only:focus-visible": "the hidden search submit: it exists for the keyboard and un-hides on focus; no pointer reaches it",
}

// endsOnControlElement reports whether a selector's last compound is an
// a, button, summary or label: the rule styles that element.
func endsOnControlElement(sel string) bool {
	depth, last := 0, 0
	for i := 0; i < len(sel); i++ {
		switch c := sel[i]; c {
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case ' ', '>', '+', '~':
			if depth == 0 {
				last = i + 1
			}
		}
	}
	return regexp.MustCompile(`^(a|button|summary|label)($|[^a-zA-Z0-9_-])`).MatchString(strings.TrimSpace(sel[last:]))
}

// interactiveCSSRules is every innermost rule in css that carries cursor:
// pointer or styles an a, button, summary or label.
func interactiveCSSRules(css string) []leafRule {
	var out []leafRule
	for _, r := range leafRules(stripCSSComments(css)) {
		hit := strings.Contains(r.body, "cursor: pointer")
		for _, s := range splitSelectorList(r.selector) {
			hit = hit || endsOnControlElement(s)
		}
		if hit {
			out = append(out, r)
		}
	}
	return out
}

// TestEveryInteractiveRuleIsInTheTapInventory is §10.1's inventory
// gate: a new control cannot arrive in tokens.css without a decision
// about its touch size, because its rule has to name an idiom in the
// table above (or an exemption with a reason), and every idiom in the
// table has to be on the page the browser drive measures.
func TestEveryInteractiveRuleIsInTheTapInventory(t *testing.T) {
	rules := interactiveCSSRules(string(TokensCSS()))
	if len(rules) < 40 {
		t.Fatalf("found only %d interactive rules in tokens.css; the reader is broken, not the file", len(rules))
	}
	for _, r := range rules {
		named := false
		for _, s := range splitSelectorList(r.selector) {
			for _, e := range tapInventory {
				named = named || strings.Contains(s, e.Key)
			}
			for key := range tapExempt {
				named = named || strings.Contains(s, key)
			}
		}
		if !named {
			t.Errorf("tokens.css styles a control no row of §1.4's inventory names:\n\t%s\n\tadd a touch rule and a tapInventory row, or say why it is exempt", r.selector)
		}
	}
	page := sizingFixture(t) + Styleguide()["modal"]
	for _, e := range tapInventory {
		if !strings.Contains(page, e.Marker) {
			t.Errorf("%s (%s): the sizing fixture renders nothing carrying %q, so the drive never measures it", e.Idiom, e.Key, e.Marker)
		}
	}
}
