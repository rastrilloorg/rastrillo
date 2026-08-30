package ui

// The stage-1 gate for the markup migration (design spec §6-v3).
//
// tokens.css is moving from a class vocabulary to an attribute one:
// `<div rst-box>` where an app writes `class="rst-box"` today. Stage 1
// is the non-breaking half — every rule names BOTH spellings, so an app
// can begin writing attributes while every app and every shipped
// partial writing classes keeps styling. Stage 2 flips the partials,
// the scaffold, the docs and the examples in one commit; stage 3 drops
// the class selectors before 1.0.
//
// The thing that has to be enforced rather than done once is the
// pairing. A class added to tokens.css in stage 2's absence would
// otherwise arrive with no attribute twin, and nobody would find out
// until an app wrote the attribute and got nothing. So this file parses
// tokens.css and holds the invariant in both directions: every class
// selector has its twin in the same rule, and every attribute selector
// is some class selector's twin — no orphans in either column.
//
// It also checks the part that could go wrong silently. A class and an
// attribute selector both weigh (0,1,0), so a pair is specificity-
// neutral and no cascade moves. That is a fact about CSS, and this file
// computes it per selector rather than trusting it, because the file
// has rules wrapped in :where() (weightless) and rules using :not() and
// :has() (weighted by their heaviest argument), which is exactly where
// a careless twin would shift the cascade and change how an app that
// changed nothing renders.
//
// The string check is necessary and not sufficient: two selectors can
// pair perfectly on paper and still paint differently. The browser
// drive in markup_v3_browser_test.go is the evidence — it renders the
// same fixture in both spellings and requires the computed styles to
// match, property by property, pseudo-elements included.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// ── The grammar ──────────────────────────────────────────────────────

// classKeepsItsSpelling names every rst- class that does NOT gain an
// attribute twin, with the reason. It is a closed list: the gate below
// requires each entry to still be a real selector in tokens.css, so an
// entry cannot rot, and a new class cannot slip in unpaired without
// being written down here on purpose.
//
// The utilities are the ratified grammar (spec §6-v3, "class is for
// utilities and the app's own CSS"). They are not kinds — they are
// cross-cutting styling, which is what class is for — so they stay
// class through stage 3 rather than being paired now and unpaired
// later.
//
// rst-form__foot is the one entry that is not a rule but a collision:
// it would flatten to rst-form-foot, and rst-form-foot is a different
// rule (the sticky action bar). Two names cannot share one attribute,
// so neither takes it until the stage-2 flip picks one and retires the
// other.
var classKeepsItsSpelling = map[string]string{
	"rst-sr-only":  "utility: visually-hidden text",
	"rst-mono":     "utility: monospaced value",
	"rst-m-hide":   "utility: hidden on a narrow screen",
	"rst-grow":     "utility: this flex child takes the slack",
	"rst-nm":       "utility: the name cell's type",
	"rst-danger":   "utility: a destructive item's colour",
	"rst-cell-mut": "utility: a muted, truncating cell",

	"rst-form__foot": "collides: it would flatten onto rst-form-foot, a different rule",
}

// attributeFor translates one rst- class name into the attribute that
// styles the same thing: a kind becomes a bare attribute, a BEM element
// becomes a flat attribute (rst-callout__body -> rst-callout-body), and
// a BEM modifier becomes a token in its kind's value, matched with ~=
// (rst-btn--primary -> rst-btn~="primary"), which is the same
// space-separated-token matching class lists get. ok is false for a
// class that keeps its spelling.
func attributeFor(class string) (name, variant string, ok bool) {
	if _, exempt := classKeepsItsSpelling[class]; exempt {
		return "", "", false
	}
	body := strings.TrimPrefix(class, "rst-")
	if k := strings.Index(body, "--"); k >= 0 {
		variant = body[k+2:]
		body = body[:k]
	}
	return "rst-" + strings.ReplaceAll(body, "__", "-"), variant, true
}

var (
	classInSelector = regexp.MustCompile(`\.(rst-[A-Za-z0-9_-]+)`)
	toneInSelector  = regexp.MustCompile(`\[data-tone="([a-z]+)"\]`)
	toneInMarkup    = regexp.MustCompile(`data-tone="([a-z]+)"`)
	classInMarkup   = regexp.MustCompile(`class="([^"]*)"`)
)

// attributeTwin is the selector that must sit beside a class selector:
// every translatable rst- class swapped for its attribute, and
// data-tone swapped for rst-tone. Everything else — type selectors,
// combinators, pseudo-classes, the .icon class, an app's own class,
// and the data-* attributes that carry runtime state (data-busy,
// data-theme, data-lead, data-rst-select) — is left exactly as it is.
// A selector with nothing to translate comes back unchanged, which is
// how the gate tells "needs a twin" from "is one".
func attributeTwin(selector string) string {
	out := classInSelector.ReplaceAllStringFunc(selector, func(m string) string {
		name, variant, ok := attributeFor(m[1:])
		if !ok {
			return m
		}
		if variant == "" {
			return "[" + name + "]"
		}
		return fmt.Sprintf("[%s~=%q]", name, variant)
	})
	return toneInSelector.ReplaceAllString(out, `[rst-tone~="$1"]`)
}

// attributeMarkup is the same translation for markup rather than
// selectors — the stage-2 codemod in miniature, and what lets the
// browser drive render one fixture in both spellings from a single
// source. class="rst-btn rst-btn--primary" becomes rst-btn="primary";
// a utility, and any class that is not ours, stays in class.
func attributeMarkup(markup string) string {
	out := classInMarkup.ReplaceAllStringFunc(markup, func(m string) string {
		value := m[len(`class="`) : len(m)-1]
		var kept []string
		variants := map[string][]string{}
		var order []string
		for _, token := range strings.Fields(value) {
			name, variant, ok := "", "", false
			if strings.HasPrefix(token, "rst-") {
				name, variant, ok = attributeFor(token)
			}
			if !ok {
				kept = append(kept, token)
				continue
			}
			if _, seen := variants[name]; !seen {
				variants[name] = nil
				order = append(order, name)
			}
			if variant != "" {
				variants[name] = append(variants[name], variant)
			}
		}
		var parts []string
		if len(kept) > 0 {
			parts = append(parts, fmt.Sprintf("class=%q", strings.Join(kept, " ")))
		}
		sort.Strings(order)
		for _, name := range order {
			if len(variants[name]) == 0 {
				parts = append(parts, name)
				continue
			}
			parts = append(parts, fmt.Sprintf("%s=%q", name, strings.Join(variants[name], " ")))
		}
		return strings.Join(parts, " ")
	})
	return toneInMarkup.ReplaceAllString(out, `rst-tone="$1"`)
}

// ── A small CSS reader ───────────────────────────────────────────────

// cssRule is one rule's selector list, split on top-level commas, and
// the line its prelude ends on so a failure can be walked to.
type cssRule struct {
	selectors []string
	line      int
}

// parseCSSRules reads tokens.css into its rules. It is deliberately
// small — comment-aware, string-aware, brace-counting — because
// tokens.css is a hand-written file with no preprocessor, and a
// full CSS parser would be a dependency this file has no need of. The
// preludes it returns include at-rules (@media, @supports); those carry
// no rst- selector and fall out of every check below on their own.
func parseCSSRules(css string) []cssRule {
	var rules []cssRule
	start, i, n := 0, 0, len(css)
	for i < n {
		if strings.HasPrefix(css[i:], "/*") {
			if j := strings.Index(css[i+2:], "*/"); j >= 0 {
				i += 2 + j + 2
			} else {
				i = n
			}
			continue
		}
		switch c := css[i]; c {
		case '"', '\'':
			i++
			for i < n && css[i] != c {
				if css[i] == '\\' {
					i++
				}
				i++
			}
			i++
		case '{':
			rules = append(rules, cssRule{
				selectors: splitSelectorList(stripCSSComments(css[start:i])),
				line:      1 + strings.Count(css[:i], "\n"),
			})
			i++
			start = i
		case '}':
			i++
			start = i
		default:
			i++
		}
	}
	return rules
}

func stripCSSComments(s string) string {
	var b strings.Builder
	for {
		k := strings.Index(s, "/*")
		if k < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:k])
		j := strings.Index(s[k+2:], "*/")
		if j < 0 {
			return b.String()
		}
		s = s[k+2+j+2:]
	}
}

// splitSelectorList splits on the commas that separate selectors,
// leaving alone the ones inside :is(), :where(), :not(), :has() and
// attribute values.
func splitSelectorList(prelude string) []string {
	var out []string
	depth := 0
	cur := strings.Builder{}
	for _, r := range prelude {
		switch r {
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, collapseSpace(cur.String()))
				cur.Reset()
				continue
			}
		}
		cur.WriteRune(r)
	}
	if s := collapseSpace(cur.String()); s != "" {
		out = append(out, s)
	}
	return out
}

func collapseSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

// tokensStyleSelector reports whether tokens.css carries a rule whose
// selector list contains exactly sel. It is what a test should ask now
// that every rule has two selectors in it: ".rst-x {" stopped being a
// substring of a file that styles .rst-x the day the twins landed.
func tokensStyleSelector(sel string) bool {
	for _, rule := range parseCSSRules(string(TokensCSS())) {
		for _, s := range rule.selectors {
			if s == sel {
				return true
			}
		}
	}
	return false
}

// ── Specificity ──────────────────────────────────────────────────────

// specificity computes (ids, classes, elements) for one selector, the
// way the selectors spec does: an attribute selector and a class weigh
// the same, :where() weighs nothing at all, and :is()/:not()/:has()
// weigh as much as their heaviest argument. It exists so the gate can
// say "this pair is specificity-neutral" as a measurement rather than
// as a belief.
func specificity(selector string) [3]int {
	var s [3]int
	i, n := 0, len(selector)
	for i < n {
		c := selector[i]
		switch {
		case c == '#':
			s[0]++
			i += 1 + identLen(selector[i+1:])
		case c == '.':
			s[1]++
			i += 1 + identLen(selector[i+1:])
		case c == '[':
			s[1]++
			i = skipTo(selector, i, '[', ']')
		case c == ':':
			if i+1 < n && selector[i+1] == ':' {
				// A pseudo-element. Never functional in this file.
				i += 2
				i += identLen(selector[i:])
				s[2]++
				continue
			}
			i++
			name := selector[i : i+identLen(selector[i:])]
			i += len(name)
			if i < n && selector[i] == '(' {
				end := skipTo(selector, i, '(', ')')
				args := splitSelectorList(selector[i+1 : end-1])
				switch strings.ToLower(name) {
				case "where":
					// Contributes nothing, by definition.
				case "is", "not", "has", "matches", "any":
					var best [3]int
					for _, a := range args {
						if v := specificity(a); heavier(v, best) {
							best = v
						}
					}
					s[0] += best[0]
					s[1] += best[1]
					s[2] += best[2]
				default:
					// :nth-child(2) and friends: an ordinary pseudo-class.
					s[1]++
				}
				i = end
				continue
			}
			switch strings.ToLower(name) {
			case "before", "after", "first-line", "first-letter":
				s[2]++ // the four legacy one-colon pseudo-elements
			default:
				s[1]++
			}
		case c == '*':
			i++
		case isIdentByte(c):
			i += identLen(selector[i:])
			s[2]++
		default: // whitespace, > + ~ | and anything else that weighs nothing
			i++
		}
	}
	return s
}

func heavier(a, b [3]int) bool {
	for k := 0; k < 3; k++ {
		if a[k] != b[k] {
			return a[k] > b[k]
		}
	}
	return false
}

func isIdentByte(c byte) bool {
	return c == '-' || c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 0x80
}

func identLen(s string) int {
	i := 0
	for i < len(s) && isIdentByte(s[i]) {
		i++
	}
	return i
}

// skipTo returns the index just past the bracket that closes the one at
// i, minding nesting and quoted attribute values.
func skipTo(s string, i int, open, close byte) int {
	depth := 0
	for ; i < len(s); i++ {
		switch c := s[i]; c {
		case '"', '\'':
			i++
			for i < len(s) && s[i] != c {
				if s[i] == '\\' {
					i++
				}
				i++
			}
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(s)
}

// ── The gates ────────────────────────────────────────────────────────

// TestEveryClassSelectorHasAnAttributeTwin is the stage-1 invariant.
// For every selector in tokens.css that names an rst- class, the same
// rule must also carry the attribute spelling of that selector — and
// the two must weigh the same, or an app that changed nothing would
// start rendering differently.
func TestEveryClassSelectorHasAnAttributeTwin(t *testing.T) {
	css := string(TokensCSS())
	rules := parseCSSRules(css)
	if len(rules) == 0 {
		t.Fatal("tokens.css parsed to no rules at all — the reader is broken, not the file")
	}

	paired := 0
	for _, rule := range rules {
		present := make(map[string]bool, len(rule.selectors))
		for _, sel := range rule.selectors {
			present[sel] = true
		}
		for _, sel := range rule.selectors {
			twin := attributeTwin(sel)
			if twin == sel {
				continue // nothing to translate: a twin, or a utility, or not ours
			}
			if !present[twin] {
				t.Errorf("tokens.css:%d: %q has no attribute twin in its rule.\n"+
					"\tadd %q beside it, or the attribute spelling of this rule styles nothing.\n"+
					"\tthe rule reads: %s",
					rule.line, sel, twin, strings.Join(rule.selectors, ", "))
				continue
			}
			paired++
			if a, b := specificity(sel), specificity(twin); a != b {
				t.Errorf("tokens.css:%d: the pair does not weigh the same — %q is %v but %q is %v.\n"+
					"\ta twin that shifts the cascade changes how an app that changed nothing renders.",
					rule.line, sel, a, twin, b)
			}
		}
	}
	// A floor, so a bulk deletion cannot pass by leaving nothing to check.
	if paired < 300 {
		t.Errorf("only %d selectors are paired; tokens.css carried 358 pairs when stage 1 landed, "+
			"so this is a wholesale loss rather than an edit", paired)
	}
	t.Logf("%d class selectors paired with an attribute twin", paired)
}

// TestNoAttributeSelectorIsAnOrphan is the same invariant from the
// other end: every rst- attribute selector in tokens.css must be some
// class selector's twin, in its own rule. An attribute rule with no
// class beside it would style the new spelling and not the old one,
// which is the breakage stage 1 exists to avoid.
func TestNoAttributeSelectorIsAnOrphan(t *testing.T) {
	css := string(TokensCSS())
	for _, rule := range parseCSSRules(css) {
		twins := make(map[string]bool, len(rule.selectors))
		for _, sel := range rule.selectors {
			if twin := attributeTwin(sel); twin != sel {
				twins[twin] = true
			}
		}
		for _, sel := range rule.selectors {
			if !strings.Contains(sel, "[rst-") {
				continue
			}
			if !twins[sel] {
				t.Errorf("tokens.css:%d: %q is an attribute selector with no class selector it is the twin of.\n"+
					"\tuntil stage 3 retires the class spelling, every rule styles both.\n"+
					"\tthe rule reads: %s", rule.line, sel, strings.Join(rule.selectors, ", "))
			}
		}
	}
}

// TestClassesThatKeepTheirSpellingAreStillReal keeps the exemption list
// honest. Every entry must still be a class in tokens.css (so a stale
// name cannot sit there excusing something that no longer exists), and
// no entry may have quietly grown an attribute form (so "this stays a
// class" stays true).
func TestClassesThatKeepTheirSpellingAreStillReal(t *testing.T) {
	css := stripCSSComments(string(TokensCSS()))
	for class, why := range classKeepsItsSpelling {
		if !strings.Contains(css, "."+class) {
			t.Errorf("classKeepsItsSpelling names %q (%s) and tokens.css has no such class: delete the entry", class, why)
		}
		flat := "rst-" + strings.ReplaceAll(strings.TrimPrefix(class, "rst-"), "__", "-")
		if class != "rst-form__foot" && strings.Contains(css, "["+flat+"]") {
			t.Errorf("classKeepsItsSpelling names %q (%s) but tokens.css styles [%s]: it is being migrated after all, so drop the exemption", class, why, flat)
		}
	}
	// The collision is the whole reason rst-form__foot is exempt. If it
	// stops being a collision — the flip renames or deletes one of the
	// two — the exemption has to go with it.
	if !strings.Contains(css, ".rst-form__foot") || !strings.Contains(css, ".rst-form-foot") {
		t.Error("rst-form__foot and rst-form-foot no longer both exist: the collision is resolved, so remove the exemption and pair what is left")
	}
}

// TestNoTwoClassesWantTheSameAttribute is the check that found the
// rst-form__foot/rst-form-foot collision in the first place. BEM's __
// flattens to a hyphen, so a __ name and a flat name can land on the
// same attribute — and one attribute cannot carry two different rules.
// Every new name has to be checked against this, not just the ones that
// existed on the day.
func TestNoTwoClassesWantTheSameAttribute(t *testing.T) {
	css := stripCSSComments(string(TokensCSS()))
	seen := map[string][]string{}
	for _, m := range classInSelector.FindAllStringSubmatch(css, -1) {
		class := m[1]
		name, variant, ok := attributeFor(class)
		if !ok {
			continue
		}
		key := name
		if variant != "" {
			key = name + "~=" + variant
		}
		known := false
		for _, had := range seen[key] {
			known = known || had == class
		}
		if !known {
			seen[key] = append(seen[key], class)
		}
	}
	for key, classes := range seen {
		if len(classes) > 1 {
			sort.Strings(classes)
			t.Errorf("%v all translate to %q: one attribute cannot style two different rules — rename one, or add it to classKeepsItsSpelling with the reason", classes, key)
		}
	}
}

// TestSpecificityReadsSelectorsTheWayTheSpecDoes is the gate's own
// gate. The pairing check leans on specificity() being right about
// :where() (weightless), :not()/:has() (their heaviest argument) and
// attributes (a class's weight); if it quietly returned zero for
// everything, every pair would look neutral and the check would be
// worthless.
func TestSpecificityReadsSelectorsTheWayTheSpecDoes(t *testing.T) {
	for _, c := range []struct {
		sel  string
		want [3]int
	}{
		{".rst-btn", [3]int{0, 1, 0}},
		{"[rst-btn]", [3]int{0, 1, 0}},
		{`[rst-btn~="primary"]`, [3]int{0, 1, 0}},
		{".rst-btn:hover", [3]int{0, 2, 0}},
		{"dialog.rst-modal-panel", [3]int{0, 1, 1}},
		{"dialog[rst-modal-panel]", [3]int{0, 1, 1}},
		{".rst-page-header::after", [3]int{0, 1, 1}},
		{"[rst-page-header]::after", [3]int{0, 1, 1}},
		{":where(.rst-list) > :where(:first-child)", [3]int{0, 0, 0}},
		{":where([rst-list]) > :where(:first-child)", [3]int{0, 0, 0}},
		{":where(.rst-page, .rst-page-header) :focus-visible", [3]int{0, 1, 0}},
		{".rst-lrow:not(.rst-lrow--head):has(> .rst-nm, > .rst-person):hover", [3]int{0, 4, 0}},
		{`[rst-lrow]:not([rst-lrow~="head"]):has(> .rst-nm, > [rst-person]):hover`, [3]int{0, 4, 0}},
		{".rst-form > *:not(.rst-form__foot, .rst-form-foot)", [3]int{0, 2, 0}},
		{"[rst-form] > *:not(.rst-form__foot, [rst-form-foot])", [3]int{0, 2, 0}},
		{".rst-search input[type=\"search\"]::-webkit-search-cancel-button", [3]int{0, 2, 2}},
		{"body:has(.rst-backdrop)", [3]int{0, 1, 1}},
		{"#id .c e", [3]int{1, 1, 1}},
	} {
		if got := specificity(c.sel); got != c.want {
			t.Errorf("specificity(%q) = %v, want %v", c.sel, got, c.want)
		}
	}
}

// TestAttributeTranslationIsTheGrammar pins the translation itself —
// kind, variant, part, part-with-variant, tone, and the two things that
// must NOT move: a utility class, and the data-* attributes that carry
// runtime state rather than authored vocabulary.
func TestAttributeTranslationIsTheGrammar(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{".rst-box", "[rst-box]"},
		{".rst-box-head + .rst-box", "[rst-box-head] + [rst-box]"},
		{".rst-btn--primary:hover", `[rst-btn~="primary"]:hover`},
		{".rst-callout__body > p", "[rst-callout-body] > p"},
		{".rst-dtp__row--set .rst-dtp__label", `[rst-dtp-row~="set"] [rst-dtp-label]`},
		{".rst-person__av--empty", `[rst-person-av~="empty"]`},
		{`.rst-status[data-tone="positive"]`, `[rst-status][rst-tone~="positive"]`},
		{`.rst-btn[aria-busy="true"]`, `[rst-btn][aria-busy="true"]`},
		{".rst-mono", ".rst-mono"},
		{".rst-row-menu__panel .rst-danger:hover", "[rst-row-menu-panel] .rst-danger:hover"},
		{".rst-search .icon", "[rst-search] .icon"},
		{`.rst-row__lead[data-lead="positive"]`, `[rst-row-lead][data-lead="positive"]`},
	} {
		if got := attributeTwin(c.in); got != c.want {
			t.Errorf("attributeTwin(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	for _, c := range []struct{ in, want string }{
		{`<div class="rst-box">`, `<div rst-box>`},
		{`<a class="rst-btn rst-btn--primary" href="/x">`, `<a rst-btn="primary" href="/x">`},
		{`<span class="rst-badge rst-badge--positive">`, `<span rst-badge="positive">`},
		{`<button class="rst-sr-only" type="submit">`, `<button class="rst-sr-only" type="submit">`},
		{`<summary class="rst-btn rst-dropdown__summary">`, `<summary rst-btn rst-dropdown-summary>`},
		{`<span class="rst-status" data-tone="warning">`, `<span rst-status rst-tone="warning">`},
		{`<td class="rst-m-hide rst-cell-mut">`, `<td class="rst-m-hide rst-cell-mut">`},
		{`<svg class="icon">`, `<svg class="icon">`},
		{`<form class="rst-form" data-busy="false">`, `<form rst-form data-busy="false">`},
		{`<div class="rst-form__foot">`, `<div class="rst-form__foot">`},
	} {
		if got := attributeMarkup(c.in); got != c.want {
			t.Errorf("attributeMarkup(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
