package ui

import (
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"io/fs"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/carlosframework/rastrillo"
)

// parseAll builds the template tree exactly the way an app is documented
// to: ui.Funcs() registered, then ParseFS over Templates() with the flat
// "*.html" glob.
func parseAll(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.New("").Funcs(Funcs()).ParseFS(Templates(), "*.html")
	if err != nil {
		t.Fatalf("ParseFS: %v", err)
	}
	return tmpl
}

// render executes one named partial against data and returns its output.
func render(t *testing.T, name string, data any) string {
	t.Helper()
	var buf strings.Builder
	if err := parseAll(t).ExecuteTemplate(&buf, name, data); err != nil {
		t.Fatalf("ExecuteTemplate(%q): %v", name, err)
	}
	return buf.String()
}

// Templates() must hand back a filesystem rooted at the partials, not at
// the package directory — the documented ParseFS call uses "*.html".
func TestTemplatesIsRootedAtPartials(t *testing.T) {
	entries, err := fs.ReadDir(Templates(), ".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("Templates() is empty")
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".html") {
			t.Errorf("unexpected entry %q at the root of Templates()", e.Name())
		}
	}
}

func TestTokensCSSIsEmbedded(t *testing.T) {
	css := TokensCSS()
	if len(css) == 0 {
		t.Fatal("TokensCSS() is empty")
	}
	for _, want := range []string{
		"--rst-sp-4", "--rst-fs-base",
		"var(--rst-bg)", "var(--rst-text)", "var(--rst-accent)", "var(--rst-font)",
		"var(--rst-radius)", "var(--rst-shadow-pop)",
		".rst-status", ".rst-sr-only",
	} {
		if !strings.Contains(string(css), want) {
			t.Errorf("tokens.css is missing %q", want)
		}
	}
	// The colour blocks are the theme's now. tokens.css naming one again
	// means a palette leaked back into the structural file, where a
	// theme swap could no longer reach it. The shape tokens joined them
	// in v2: a "--rst-radius:" or "--rst-shadow-pop:" *declaration* here
	// would silently outrank nothing and confuse everything, since every
	// theme declares its own.
	for _, gone := range []string{
		"prefers-color-scheme: dark", `:root[data-theme="dark"]`, `:root[data-theme="light"]`,
		"--rst-radius:", "--rst-radius-sm:", "--rst-radius-pill:",
		"--rst-shadow-pop:", "--rst-shadow-knob:", "--rst-shadow-lift:", "--rst-overlay:",
	} {
		if strings.Contains(string(css), gone) {
			t.Errorf("tokens.css declares %q; colour and shape belong in themes/", gone)
		}
	}
}

// Every shipped theme is a whole theme: the palette, the type family,
// the shape tokens, and the one-block structure the contrast gate parses.
func TestThemeCSSIsEmbedded(t *testing.T) {
	for _, name := range ThemeNames() {
		css, ok := ThemeCSS(name)
		if !ok {
			t.Fatalf("ThemeCSS(%q) reports missing, but it is in ThemeNames()", name)
		}
		for _, want := range []string{
			"--rst-bg", "--rst-surface", "--rst-text", "--rst-accent",
			"--rst-tone-positive-fg", "--rst-font", "--rst-radius", "--rst-shadow-pop",
			"color-scheme: light dark", "light-dark(",
			`:root[data-theme="dark"] { color-scheme: dark; }`,
			`:root[data-theme="light"] { color-scheme: light; }`,
		} {
			if !strings.Contains(string(css), want) {
				t.Errorf("themes/%s.css is missing %q", name, want)
			}
		}
		// The v2 format is one :root block plus exactly two toggle rules,
		// and the toggle rules carry nothing but color-scheme. A theme
		// that grew a second palette block (a prefers-color-scheme media
		// query, say, or colours restated under [data-theme]) has a copy
		// of itself to keep in sync again, which is the failure
		// light-dark() was adopted to make impossible.
		if strings.Contains(string(css), "prefers-color-scheme") {
			t.Errorf("themes/%s.css declares a prefers-color-scheme block; the scheme split is light-dark()'s job now", name)
		}
		if got := strings.Count(string(css), ":root {"); got != 1 {
			t.Errorf("themes/%s.css opens %d bare :root blocks, want exactly 1", name, got)
		}
		for _, prop := range []string{"--rst-bg:", "--rst-text:", "--rst-accent:", "--rst-radius:"} {
			if got := strings.Count(string(css), prop); got != 1 {
				t.Errorf("themes/%s.css declares %s %d times, want exactly 1 (one block, both schemes)", name, prop, got)
			}
		}
	}
	for _, bad := range []string{"nope", "", "day.css", "../tokens"} {
		if _, ok := ThemeCSS(bad); ok {
			t.Errorf("ThemeCSS(%q) reports a theme that is not shipped", bad)
		}
	}
}

// Both schemes are authored, never inverted. Before v2 that meant
// counting three copies of every colour block; now it means every themed
// colour is declared exactly once, as a light-dark() call with two
// distinct-or-deliberate halves. A theme that wrote a bare hex where a
// light-dark() belongs has a dark scheme that is just the light one
// showing through, which is the modern shape of the same rot.
//
// The shadows and the overlay are exempt from the light-dark()
// requirement but not from being declared: a shadow whose two schemes
// are genuinely identical (day's knob and lift) is allowed to say so
// once, and light-dark() may only wrap the colour inside a shadow value
// anyway, never the whole thing.
func TestBothSchemesAreAuthoredInEveryTheme(t *testing.T) {
	needsLightDark := []string{
		"--rst-bg", "--rst-surface", "--rst-surface-2", "--rst-line", "--rst-line-strong",
		"--rst-text", "--rst-text-muted", "--rst-text-faint",
		"--rst-accent", "--rst-accent-strong", "--rst-accent-soft", "--rst-on-accent",
		"--rst-tone-neutral-fg", "--rst-tone-neutral-bg",
		"--rst-tone-positive-fg", "--rst-tone-positive-bg",
		"--rst-tone-warning-fg", "--rst-tone-warning-bg",
		"--rst-tone-negative-fg", "--rst-tone-negative-bg",
		"--rst-overlay",
	}
	declaredOnce := append([]string{
		"--rst-font", "--rst-radius", "--rst-radius-sm", "--rst-radius-pill",
		"--rst-shadow-pop", "--rst-shadow-knob", "--rst-shadow-lift",
	}, needsLightDark...)

	for _, name := range ThemeNames() {
		raw, ok := ThemeCSS(name)
		if !ok {
			t.Fatalf("ThemeCSS(%q) missing", name)
		}
		css := string(raw)
		decl := regexp.MustCompile(`(--rst-[a-z0-9-]+)\s*:\s*([^;]+);`)
		values := map[string]string{}
		for _, m := range decl.FindAllStringSubmatch(css, -1) {
			values[m[1]] = strings.TrimSpace(m[2])
		}
		for _, prop := range declaredOnce {
			// Declarations are "<prop>: value"; uses are "var(<prop>)", so the
			// trailing colon counts declarations only.
			if got := strings.Count(css, prop+":"); got != 1 {
				t.Errorf("themes/%s.css declares %s %d times, want exactly 1", name, prop, got)
			}
		}
		for _, prop := range needsLightDark {
			light, dark, ok := splitLightDark(values[prop])
			if !ok {
				t.Errorf("themes/%s.css: %s is %q, want a whole-value light-dark(<light>, <dark>)", name, prop, values[prop])
				continue
			}
			if light == dark {
				t.Errorf("themes/%s.css: %s declares %s for both schemes; that is an inverted dark theme wearing light-dark()'s clothes", name, prop, light)
			}
		}
	}
}

// reducedMotionAllowlist names selectors that carry a transition/animation
// with no matching disable under @media (prefers-reduced-motion: reduce),
// found pre-existing at the time this gate was added. Task 5 only adds
// the gate; it does not silently fix CSS it did not write. Empty today —
// every transition tokens.css declares (rst-caret, rst-switch__track and
// its ::after, rst-tip::after) already has a reduce-block "transition:
// none" counterpart, so nothing needs listing. If a future change adds a
// transition without one, this test fails; add the selector here only
// with a comment explaining why it is deliberately left un-disabled, not
// as a way to silence the failure.
var reducedMotionAllowlist = map[string]bool{}

// braceMatchEnd returns the index of the '}' matching the '{' implicitly
// opened at css[start-1] (i.e. depth starts at 1) — tokens.css nests at
// most one level (a media query wrapping plain rules), and no selector or
// value in this file contains a literal brace, so simple depth counting
// is exact here.
func braceMatchEnd(css string, start int) int {
	depth := 1
	for j := start; j < len(css); j++ {
		switch css[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return len(css)
}

// reducedMotionBlocks returns the raw contents of every
// @media (prefers-reduced-motion: reduce) { ... } block in css, and rest
// = css with those blocks (header, braces and all) removed — so a
// transition-selector search over rest never mistakes a reduce block's
// own "transition: none" for an active, undisabled transition.
func reducedMotionBlocks(css string) (blocks []string, rest string) {
	const header = "@media (prefers-reduced-motion: reduce) {"
	var b strings.Builder
	last, idx := 0, 0
	for {
		i := strings.Index(css[idx:], header)
		if i < 0 {
			break
		}
		blockStart := idx + i
		contentStart := blockStart + len(header)
		end := braceMatchEnd(css, contentStart)
		blocks = append(blocks, css[contentStart:end])
		b.WriteString(css[last:blockStart])
		last = end + 1
		idx = end + 1
	}
	b.WriteString(css[last:])
	return blocks, b.String()
}

// leafRule is one selector plus its (brace-matched, non-nested)
// declaration body.
type leafRule struct{ selector, body string }

// leafRulePattern's selector group ([^{}]+) is greedy over everything
// since the previous rule's closing brace, which includes any comment
// sitting between the two rules — for the very first rule in the file,
// that is tokens.css's entire, ~100-line file-header comment. blockCommentPattern
// strips /* ... */ spans (across lines: (?s) makes '.' match '\n') out of
// a raw selector capture before it is trimmed and used as a map key or
// error-message value, so neither is unusably long or noisy.
var blockCommentPattern = regexp.MustCompile(`(?s)/\*.*?\*/`)

// leafRulePattern extracts every innermost selector{...} rule in css —
// this naturally includes rules nested inside an @media block (matched
// before the wrapping @media's own, still-open brace), since none of
// them nest further themselves.
var leafRulePattern = regexp.MustCompile(`([^{}]+)\{([^{}]*)\}`)

func leafRules(css string) []leafRule {
	var out []leafRule
	for _, m := range leafRulePattern.FindAllStringSubmatch(css, -1) {
		sel := blockCommentPattern.ReplaceAllString(m[1], "")
		// Fields+Join collapses any remaining internal whitespace/newlines
		// left by a stripped multi-line comment down to single spaces, on
		// top of trimming the ends.
		sel = strings.Join(strings.Fields(sel), " ")
		out = append(out, leafRule{selector: sel, body: m[2]})
	}
	return out
}

// selectorList splits a (possibly comma-separated) selector into its
// individual, trimmed selectors — ".a, .a::after" becomes [".a",
// ".a::after"], each compared for exact equality elsewhere in this file,
// never by substring: a substring check would let a new, unguarded
// ".rst-tip" rule pass by riding on ".rst-tip::after" already being
// listed in a reduce block, which is exactly the false negative this
// gate exists to catch.
func selectorList(sel string) []string {
	var out []string
	for _, s := range strings.Split(sel, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// motionDecl is one transition/animation declaration: the CSS property
// name and its (trimmed) value.
type motionDecl struct{ prop, val string }

// motionPropertyPattern finds every transition/animation declaration in
// a rule body and captures its value, so "transition: none" (the disable
// declaration itself) can be told apart from an actual animated
// property, and so a rule declaring both properties is not silently
// reduced to only the first FindAll would otherwise stop at.
var motionPropertyPattern = regexp.MustCompile(`(?:^|;)\s*(transition|animation)\s*:\s*([^;]+)`)

func motionDecls(body string) []motionDecl {
	var out []motionDecl
	for _, m := range motionPropertyPattern.FindAllStringSubmatch(body, -1) {
		out = append(out, motionDecl{prop: m[1], val: strings.TrimSpace(m[2])})
	}
	return out
}

// reduceIndex maps an exact selector to the set of properties some
// prefers-reduced-motion: reduce rule disables (declares "none") for
// that exact selector.
type reduceIndex map[string]map[string]bool

func buildReduceIndex(blocks []string) reduceIndex {
	idx := reduceIndex{}
	for _, block := range blocks {
		for _, rule := range leafRules(block) {
			for _, sel := range selectorList(rule.selector) {
				for _, d := range motionDecls(rule.body) {
					if d.val != "none" {
						continue
					}
					if idx[sel] == nil {
						idx[sel] = map[string]bool{}
					}
					idx[sel][d.prop] = true
				}
			}
		}
	}
	return idx
}

// TestReducedMotionDisablesEveryTransition is the motion gate (task 5):
// every selector that declares a real transition or animation outside a
// prefers-reduced-motion block must have that exact selector — not a
// selector merely containing or contained by it — disable that exact
// same property (transition disabled by "transition: none", animation by
// "animation: none") somewhere inside a
// @media (prefers-reduced-motion: reduce) block. Two failure modes this
// specifically guards against, both found by mutation during review:
// declaring a *different* motion property on an already-listed selector
// (selector reappearing is not enough — the property must actually be
// neutralized), and a new selector merely overlapping textually with one
// already covered (".rst-tip" vs ".rst-tip::after") — hence the exact,
// comma-split selector match in selectorList/buildReduceIndex rather than
// a substring check.
func TestReducedMotionDisablesEveryTransition(t *testing.T) {
	css := string(TokensCSS())
	reduceBlocks, rest := reducedMotionBlocks(css)
	if len(reduceBlocks) == 0 {
		t.Fatal("tokens.css declares no @media (prefers-reduced-motion: reduce) block at all")
	}
	idx := buildReduceIndex(reduceBlocks)

	tested := 0
	for _, rule := range leafRules(rest) {
		decls := motionDecls(rule.body)
		if len(decls) == 0 {
			continue
		}
		for _, sel := range selectorList(rule.selector) {
			for _, d := range decls {
				if d.val == "none" {
					// Already disabled unconditionally; nothing to gate.
					continue
				}
				tested++
				if reducedMotionAllowlist[sel] {
					continue
				}
				if !idx[sel][d.prop] {
					t.Errorf("selector %q declares %s: %s with no exact-selector %q: none under prefers-reduced-motion: reduce (add one, or add %q to reducedMotionAllowlist with a reason)",
						sel, d.prop, d.val, d.prop+": none", sel)
				}
			}
		}
	}
	if tested == 0 {
		t.Fatal("found no transition/animation declarations to check outside reduce blocks — the parser likely broke, not that tokens.css lost every transition")
	}
}

func TestStatusPillRendersLabelAndTone(t *testing.T) {
	got := render(t, "status-pill", map[string]any{"Tone": "positive", "Label": "Published"})
	if !strings.Contains(got, `data-tone="positive"`) {
		t.Errorf("missing tone attribute: %s", got)
	}
	if !strings.Contains(got, "Published") {
		t.Errorf("missing visible label: %s", got)
	}
}

// State is never colour alone (spec §5, addendum §4): the label is always
// real text in the output, whatever the tone.
func TestStatusPillAlwaysCarriesTextLabel(t *testing.T) {
	for _, tone := range []string{"neutral", "positive", "warning", "negative"} {
		got := render(t, "status-pill", map[string]any{"Tone": tone, "Label": "Draft"})
		if !strings.Contains(got, ">Draft<") {
			t.Errorf("tone %q rendered no text label: %s", tone, got)
		}
	}
}

// The minimal fixture: Label only. A missing Tone falls back to neutral
// rather than rendering an empty attribute.
func TestStatusPillMinimalFixture(t *testing.T) {
	got := render(t, "status-pill", map[string]any{"Label": "Draft"})
	if !strings.Contains(got, `data-tone="neutral"`) {
		t.Errorf("missing Tone did not fall back to neutral: %s", got)
	}
	if strings.Contains(got, `data-tone=""`) {
		t.Errorf("rendered an empty tone attribute: %s", got)
	}
}

func TestPageHeaderMinimalFixture(t *testing.T) {
	got := render(t, "page-header", map[string]any{"Title": "Posts"})
	if !strings.Contains(got, "<h1>Posts</h1>") {
		t.Errorf("missing title: %s", got)
	}
	if strings.Contains(got, "<a ") {
		t.Errorf("no action was supplied, so no link should render: %s", got)
	}
	if strings.Contains(got, "rst-page-header__sub") {
		t.Errorf("no Sub was supplied, so no subhead should render: %s", got)
	}
}

func TestPageHeaderWithSubAndAction(t *testing.T) {
	got := render(t, "page-header", map[string]any{
		"Title":       "Posts",
		"Sub":         "Everything you have written, newest first.",
		"ActionHref":  "/posts/new",
		"ActionLabel": "Write a post",
		"ActionIcon":  "plus",
	})
	for _, want := range []string{
		"<h1>Posts</h1>",
		"Everything you have written, newest first.",
		`href="/posts/new"`,
		"Write a post",
		"<svg", // the icon resolved through Funcs()
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
}

// The action is a link with visible text, so its accessible name is the
// text itself — the icon beside it must stay silent.
func TestPageHeaderActionIconIsAriaHidden(t *testing.T) {
	got := render(t, "page-header", map[string]any{
		"Title": "Posts", "ActionHref": "/posts/new",
		"ActionLabel": "Write a post", "ActionIcon": "plus",
	})
	if !strings.Contains(got, `aria-hidden="true"`) {
		t.Errorf("the action icon is not aria-hidden: %s", got)
	}
}

func TestEmptyStateMinimalFixture(t *testing.T) {
	got := render(t, "empty-state", map[string]any{
		"Body": "No posts yet. Your first one is a good place to start.",
	})
	if !strings.Contains(got, "No posts yet.") {
		t.Errorf("missing body: %s", got)
	}
	if strings.Contains(got, "<form") || strings.Contains(got, "<a ") {
		t.Errorf("no CTA was supplied, so none should render: %s", got)
	}
	if strings.Contains(got, "rst-empty__title") {
		t.Errorf("no Title was supplied, so no heading should render: %s", got)
	}
}

func TestEmptyStateLinkCTA(t *testing.T) {
	got := render(t, "empty-state", map[string]any{
		"Title":       "Nothing here yet",
		"Body":        "No posts yet. Your first one is a good place to start.",
		"ActionHref":  "/posts/new",
		"ActionLabel": "Write a post",
	})
	// A real heading, not a styled paragraph: styling a <p> to look like
	// a heading is WCAG 2.2 failure F2 (1.3.1) and heading navigation
	// skips it entirely.
	if !strings.Contains(got, `<h2 class="rst-empty__title">Nothing here yet</h2>`) {
		t.Errorf("title is not a real heading element: %s", got)
	}
	if !strings.Contains(got, `<a class="rst-btn rst-btn--primary" href="/posts/new">Write a post</a>`) {
		t.Errorf("missing link CTA: %s", got)
	}
	if strings.Contains(got, "<form") {
		t.Errorf("a link CTA must not also render a form: %s", got)
	}
}

// The POST CTA is an ordinary form: it works with JavaScript off, and
// hidden pairs (a CSRF token among them) are entirely app-supplied.
func TestEmptyStatePostCTACarriesHiddenPairs(t *testing.T) {
	got := render(t, "empty-state", map[string]any{
		"Body":        "No posts yet, and no sample data either.",
		"PostAction":  "/posts/seed",
		"ActionLabel": "Add sample posts",
		"Hidden":      [][2]string{{"csrf", "tok-123"}, {"count", "5"}},
	})
	for _, want := range []string{
		`method="post"`, `action="/posts/seed"`,
		`<input type="hidden" name="csrf" value="tok-123">`,
		`<input type="hidden" name="count" value="5">`,
		`type="submit"`, "Add sample posts",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
}

// A caller that sets neither CTA, and a caller that sets a POST CTA with
// no Hidden pairs, must both render without an Execute error — ranging
// over an absent key is the classic way a partial blows up.
func TestEmptyStatePostCTAWithoutHidden(t *testing.T) {
	got := render(t, "empty-state", map[string]any{
		"Body":        "No posts yet.",
		"PostAction":  "/posts/seed",
		"ActionLabel": "Add sample posts",
	})
	if !strings.Contains(got, `action="/posts/seed"`) {
		t.Errorf("missing form action: %s", got)
	}
	if strings.Contains(got, "type=\"hidden\"") {
		t.Errorf("no Hidden pairs were supplied, so none should render: %s", got)
	}
}

func TestListBarSearchMinimalFixture(t *testing.T) {
	got := render(t, "list-bar-search", map[string]any{"Action": "/posts"})
	for _, want := range []string{
		`<search><form class="rst-search"`, `method="get"`, `action="/posts"`,
		`type="search"`, `name="q"`, `<svg`, `type="submit"`, `</form></search>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
	// <search> already carries the search role. Keeping role="search" on
	// the form as well would announce two nested search landmarks for one
	// search box, so exactly one of them may be present — and it is the
	// element, not the attribute.
	if strings.Contains(got, `role="search"`) {
		t.Errorf("<search> and role=\"search\" both present: that is two nested search landmarks: %s", got)
	}
	// No Query, no Placeholder: neither attribute should appear empty.
	if strings.Contains(got, `value=""`) || strings.Contains(got, `placeholder=""`) {
		t.Errorf("empty attributes rendered instead of being omitted: %s", got)
	}
	// Action is set in this fixture, so it should never render empty either.
	if strings.Contains(got, ` action=""`) {
		t.Errorf("empty action attribute rendered instead of being omitted: %s", got)
	}
}

// The input always has a real accessible name, whether or not the caller
// supplied a placeholder.
func TestListBarSearchInputAlwaysHasAnAccessibleName(t *testing.T) {
	bare := render(t, "list-bar-search", map[string]any{"Action": "/posts"})
	if !strings.Contains(bare, `aria-label="Search"`) {
		t.Errorf("no default accessible name on the input: %s", bare)
	}
	named := render(t, "list-bar-search", map[string]any{
		"Action": "/posts", "Placeholder": "Search posts",
	})
	if !strings.Contains(named, `aria-label="Search posts"`) {
		t.Errorf("Placeholder did not become the accessible name: %s", named)
	}
	if !strings.Contains(named, `placeholder="Search posts"`) {
		t.Errorf("missing placeholder attribute: %s", named)
	}
}

func TestListBarSearchCarriesQueryAndHidden(t *testing.T) {
	got := render(t, "list-bar-search", map[string]any{
		"Action": "/posts",
		"Query":  "release notes",
		"Hidden": [][2]string{{"sort", "newest"}},
	})
	if !strings.Contains(got, `value="release notes"`) {
		t.Errorf("query not preserved: %s", got)
	}
	if !strings.Contains(got, `<input type="hidden" name="sort" value="newest">`) {
		t.Errorf("hidden pair not preserved across the search GET: %s", got)
	}
}

// The submit control is present, is a real button, and defaults to
// "Search" — a keyboard user with JS off has to be able to submit.
func TestListSearchSubmitDefaultsAndOverrides(t *testing.T) {
	def := render(t, "list-search-submit", map[string]any{})
	if !strings.Contains(def, `<button class="rst-sr-only" type="submit">Search</button>`) {
		t.Errorf("default submit control is wrong: %s", def)
	}
	over := render(t, "list-search-submit", map[string]any{"Label": "Buscar"})
	if !strings.Contains(over, ">Buscar<") {
		t.Errorf("Label override ignored: %s", over)
	}
}

func TestListBarSearchPassesLabelThroughToSubmit(t *testing.T) {
	got := render(t, "list-bar-search", map[string]any{"Action": "/posts", "Label": "Buscar"})
	if !strings.Contains(got, ">Buscar<") {
		t.Errorf("Label did not reach list-search-submit: %s", got)
	}
}

func TestListBarWrapsTheSearchFormInAToolbarStrip(t *testing.T) {
	got := render(t, "list-bar", map[string]any{
		"SearchAction": "/posts",
		"Query":        "notes",
		"Placeholder":  "Search posts",
		"Hidden":       [][2]string{{"sort", "newest"}},
	})
	for _, want := range []string{
		`<div class="rst-lbar">`, `<search><form class="rst-search"`,
		`action="/posts"`, `value="notes"`, `placeholder="Search posts"`,
		`<input type="hidden" name="sort" value="newest">`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
	// Without a Filter, the bar renders no dropdown — the key, not the
	// slice boundary, is now what gates it.
	if strings.Contains(got, "<details") {
		t.Errorf("list-bar rendered a dropdown without a Filter: %s", got)
	}
}

func TestListBarRendersAFilterDropdownWhenGivenOne(t *testing.T) {
	got := render(t, "list-bar", map[string]any{
		"SearchAction": "/admin/posts",
		"Filter": map[string]any{
			"Label": "All",
			"Aria":  "Filter by status: All",
			"Items": []any{
				map[string]any{"Href": "/admin/posts", "Label": "All", "Current": true},
				map[string]any{"Href": "/admin/posts?status=draft", "Label": "Drafts"},
			},
		},
	})
	for _, want := range []string{
		`<details class="rst-dropdown" name="rst-menus">`,
		`aria-label="Filter by status: All"`,
		`<a href="/admin/posts?status=draft">Drafts</a>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
}

// A list-bar built with only its action — the minimal fixture. Every
// other key is absent, so this is where a stray empty attribute or a
// lost default would show up.
func TestListBarMinimalFixture(t *testing.T) {
	got := render(t, "list-bar", map[string]any{"SearchAction": "/posts"})
	if !strings.Contains(got, `<div class="rst-lbar">`) {
		t.Errorf("missing toolbar strip: %s", got)
	}
	if !strings.Contains(got, `action="/posts"`) {
		t.Errorf("missing form action: %s", got)
	}
	// The default accessible name survives the trip through list-bar's dict.
	if !strings.Contains(got, `aria-label="Search"`) {
		t.Errorf("the search input lost its default accessible name: %s", got)
	}
	if strings.Contains(got, `value=""`) || strings.Contains(got, `placeholder=""`) {
		t.Errorf("empty attributes rendered instead of being omitted: %s", got)
	}
}

func TestListRowActionMinimalFixture(t *testing.T) {
	got := render(t, "list-row-action", map[string]any{
		"Href": "/posts/1", "Main": "Release notes, August",
	})
	if !strings.Contains(got, `<a href="/posts/1">Release notes, August</a>`) {
		t.Errorf("missing primary link: %s", got)
	}
	if strings.Contains(got, "rst-row__lead") || strings.Contains(got, "rst-row__action") {
		t.Errorf("optional parts rendered without data: %s", got)
	}
	if strings.Contains(got, "rst-row__sub") {
		t.Errorf("no Sub was supplied, so no meta line should render: %s", got)
	}
}

func TestListRowActionFullFixture(t *testing.T) {
	got := render(t, "list-row-action", map[string]any{
		"Href": "/posts/1", "Main": "Release notes, August",
		"Sub":        "Published 2 August · 4 min read",
		"ActionHref": "/posts/1/edit", "ActionLabel": "Edit",
		"ActionAria": "Edit Release notes, August",
		"Lead":       "positive", "LeadInitial": "RN",
	})
	for _, want := range []string{
		`data-lead="positive"`, `aria-hidden="true"`, ">RN<",
		"Published 2 August · 4 min read",
		`href="/posts/1/edit"`, `aria-label="Edit Release notes, August"`, ">Edit<",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
}

// Two separate anchors, never one nested inside the other: nested
// anchors are invalid HTML and the inner one is unreachable by keyboard.
func TestListRowActionNeverNestsAnchors(t *testing.T) {
	got := render(t, "list-row-action", map[string]any{
		"Href": "/posts/1", "Main": "Release notes",
		"ActionHref": "/posts/1/edit", "ActionLabel": "Edit",
	})
	first := strings.Index(got, "<a ")
	firstClose := strings.Index(got, "</a>")
	second := strings.LastIndex(got, "<a ")
	if first == -1 || firstClose == -1 || second == first {
		t.Fatalf("expected two anchors: %s", got)
	}
	if second < firstClose {
		t.Errorf("the action anchor opens before the name anchor closes: %s", got)
	}
}

func TestPaginationRendersEveryItemKind(t *testing.T) {
	got := render(t, "pagination", map[string]any{
		"Items": []any{
			map[string]any{"Label": "Previous", "Disabled": true},
			map[string]any{"Label": "1", "Current": true},
			map[string]any{"Label": "2", "Href": "/posts?page=2"},
			map[string]any{"Gap": true},
			map[string]any{"Label": "9", "Href": "/posts?page=9"},
			map[string]any{"Label": "Next", "Href": "/posts?page=2"},
		},
	})
	for _, want := range []string{
		`aria-label="Pagination"`,
		`<span class="rst-pagination__disabled">Previous</span>`,
		`<span aria-current="page">1</span>`,
		`<a href="/posts?page=2">2</a>`,
		`aria-hidden="true"`,
		`<a href="/posts?page=9">9</a>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
	// The gap item carries no Label, and a gap is never a link.
	if strings.Contains(got, `<a href="">`) {
		t.Errorf("the gap item rendered as an empty link: %s", got)
	}
}

// The current page is marked in the accessibility tree, not only in the
// stylesheet.
func TestPaginationCurrentPageIsNotColourAlone(t *testing.T) {
	got := render(t, "pagination", map[string]any{
		"Items": []any{map[string]any{"Label": "3", "Current": true}},
	})
	if !strings.Contains(got, `aria-current="page"`) {
		t.Errorf("current page carries no aria-current: %s", got)
	}
}

func TestPaginationLabelOverride(t *testing.T) {
	got := render(t, "pagination", map[string]any{
		"Label": "Paginación",
		"Items": []any{map[string]any{"Label": "1", "Current": true}},
	})
	if !strings.Contains(got, `aria-label="Paginación"`) {
		t.Errorf("Label override ignored: %s", got)
	}
}

// An empty page strip must render an empty nav, not an Execute error.
func TestPaginationWithNoItems(t *testing.T) {
	got := render(t, "pagination", map[string]any{})
	if !strings.Contains(got, `<nav class="rst-pagination"`) {
		t.Errorf("missing nav: %s", got)
	}
	if strings.Contains(got, "<a ") {
		t.Errorf("no items were supplied, so no links should render: %s", got)
	}
}

func TestMeterClampsAndAlwaysShowsTheNumber(t *testing.T) {
	over := render(t, "meter", map[string]any{"Percent": 140, "Text": "7/5"})
	if !strings.Contains(over, "--rst-meter-fill: 100%") {
		t.Errorf("percent not clamped high: %s", over)
	}
	under := render(t, "meter", map[string]any{"Percent": -3, "Text": "0/5"})
	if !strings.Contains(under, "--rst-meter-fill: 0%") {
		t.Errorf("percent not clamped low: %s", under)
	}
	if !strings.Contains(over, `<span class="rst-meter__num">7/5</span>`) {
		t.Errorf("the fraction text is the accessible value and must render: %s", over)
	}
}

func TestCalloutTones(t *testing.T) {
	for tone, iconFrag := range map[string]string{
		"info": "M12 16v-4", "positive": "m9 12 2 2 4-4",
		"warning": "M12 9v4", "negative": "m15 9-6 6",
	} {
		got := render(t, "callout", map[string]any{"Tone": tone, "Body": "b"})
		if !strings.Contains(got, `data-tone="`+tone+`"`) || !strings.Contains(got, iconFrag) {
			t.Errorf("tone %s: wrong attribute or icon: %s", tone, got)
		}
	}
	plain := render(t, "callout", map[string]any{"Body": "b"})
	if !strings.Contains(plain, `data-tone="info"`) {
		t.Errorf("default tone is info: %s", plain)
	}
	if strings.Contains(plain, `role="alert"`) {
		t.Errorf("role=alert must be opt-in: %s", plain)
	}
	alert := render(t, "callout", map[string]any{"Body": "b", "Alert": true})
	if !strings.Contains(alert, `role="alert"`) {
		t.Errorf("Alert did not add role=alert: %s", alert)
	}
}

func TestPersonAvatarIsDecorationOnly(t *testing.T) {
	got := render(t, "person", fixtureFor(t, "person"))
	if !strings.Contains(got, `aria-hidden="true"`) {
		t.Errorf("avatar must be aria-hidden: %s", got)
	}
	empty := render(t, "person", map[string]any{"Href": "/x", "Name": "N"})
	if !strings.Contains(empty, "rst-person__av--empty") {
		t.Errorf("missing Initial renders the empty-avatar state: %s", empty)
	}
}

// allPartials is the shipped set, with a fixture exercising every
// optional field at once. Every href is deliberately relative: the
// self-containment check below bans absolute URLs outright, so anything
// absolute in the output came from a partial rather than a caller.
func allPartials() []struct {
	Name string
	Data map[string]any
} {
	return []struct {
		Name string
		Data map[string]any
	}{
		{"page-header", map[string]any{
			"Title": "Posts", "Sub": "Everything you have written, newest first.",
			"ActionHref": "/posts/new", "ActionLabel": "Write a post", "ActionIcon": "plus",
		}},
		{"list-bar", map[string]any{
			"SearchAction": "/posts", "Query": "release", "Placeholder": "Search posts",
			"Hidden": [][2]string{{"sort", "newest"}},
		}},
		{"list-bar-search", map[string]any{
			"Action": "/posts", "Query": "release", "Placeholder": "Search posts",
			"Hidden": [][2]string{{"sort", "newest"}}, "Label": "Search posts",
		}},
		{"list-search-submit", map[string]any{"Label": "Search posts"}},
		{"list-row-action", map[string]any{
			"Href": "/posts/1", "Main": "Release notes, August",
			"Sub":        "Published 2 August · 4 min read",
			"ActionHref": "/posts/1/edit", "ActionLabel": "Edit",
			"ActionAria": "Edit Release notes, August",
			"Lead":       "accent", "LeadInitial": "RN",
		}},
		{"status-pill", map[string]any{"Tone": "positive", "Label": "Published"}},
		{"empty-state", map[string]any{
			"Title": "Nothing here yet", "Body": "No posts yet. Your first one is a good place to start.",
			"PostAction": "/posts/seed", "ActionLabel": "Add sample posts",
			"Hidden": [][2]string{{"csrf", "tok-123"}},
		}},
		{"pagination", map[string]any{
			"Label": "Pagination",
			"Items": []any{
				map[string]any{"Label": "Previous", "Disabled": true},
				map[string]any{"Label": "1", "Current": true},
				map[string]any{"Label": "2", "Href": "/posts?page=2"},
				map[string]any{"Gap": true},
				map[string]any{"Label": "9", "Href": "/posts?page=9"},
			},
		}},
		{"badge", map[string]any{"Label": "Draft"}},
		{"meter", map[string]any{"Percent": 82, "Text": "412/500"}},
		{"person", map[string]any{
			"Href": "/people/1", "Name": "Grace Hopper", "Email": "grace@example.com", "Initial": "G",
		}},
		{"callout", map[string]any{
			"Tone": "warning", "Title": "Connect payments to start selling",
			"Body": "Your event is live but can't take payment yet.",
		}},
		{"detail-list", map[string]any{
			"Items": []any{
				map[string]any{"Label": "Audience", "Value": "Members"},
				map[string]any{"Label": "Main page", "Value": "No"},
			},
		}},
		{"field", map[string]any{
			"ID": "email", "Name": "email", "Label": "Email", "Type": "email",
			"Required": true, "Help": "We'll never share this.",
		}},
		{"field-select", map[string]any{
			"ID": "role", "Name": "role", "Label": "Role",
			"Options": []any{
				map[string]any{"Value": "admin", "Label": "Admin", "Selected": true},
				map[string]any{"Value": "member", "Label": "Member"},
			},
		}},
		{"field-textarea", map[string]any{
			"Name": "bio", "Label": "Bio", "Rows": 4,
			"Hint": "Shown on your profile.",
		}},
		{"field-text", map[string]any{
			"Name": "title", "Label": "Title", "Hint": "Shown in the list.",
		}},
		{"dropdown", map[string]any{
			"Label": "Sort",
			"Items": []any{map[string]any{"Href": "/x", "Label": "Newest"}},
		}},
		{"form-foot", map[string]any{
			"Submit": "Save", "CancelHref": "/admin/posts", "CancelLabel": "Back to posts",
		}},
		{"field-check", map[string]any{
			"Name": "notify", "Label": "Email me about replies", "Checked": true,
		}},
		{"choice-field", map[string]any{
			"Legend": "Plan", "Name": "plan",
			"Options": []any{
				map[string]any{"Value": "free", "Title": "Free", "Desc": "Good to start."},
				map[string]any{"Value": "pro", "Title": "Pro", "Desc": "For growing teams.", "Checked": true},
			},
		}},
		{"seg-tabs", map[string]any{
			"Label": "Sections",
			"Items": []any{
				map[string]any{"Label": "Basics", "Href": "?tab=basics", "Current": true},
				map[string]any{"Label": "Advanced", "Href": "?tab=advanced"},
			},
		}},
		{"confirm-form", map[string]any{
			"Action": "/orders/1/refund", "Label": "Refund €10.00", "Danger": true,
			// [][2]string, caller order — csrf first, deliberately: see
			// confirm-form.html's Hidden doc comment for why the map shape
			// this used to take is a real footgun (silent CSRF-token loss),
			// not just a stylistic mismatch with the rest of the library.
			"Hidden": [][2]string{{"csrf", "tok"}, {"id", "42"}}, "CancelHref": "/orders/1",
		}},
		{"back-nav", map[string]any{"Href": "/orders/1", "Label": "Order AB3PX"}},
		{"bulk-bar", map[string]any{
			"DoneHref": "/orders", "DoneLabel": "Done selecting",
			"Count":        "3 selected",
			"EscalateHref": "/orders?select=all", "EscalateLabel": "Select all 412 matching",
			"MenuLabel": "Actions",
			"Actions": []any{
				map[string]any{"Value": "export", "Label": "Export"},
				map[string]any{"Value": "refund", "Label": "Refund…", "Danger": true},
			},
		}},
		{"error-page", map[string]any{
			"Status": 500, "Ref": "k3f9tq", "HomeHref": "/", "BackHref": "/orders",
		}},
		{"field-date", map[string]any{
			"Name": "published_on", "Label": "Published on", "Value": "2026-08-28",
			"Required": true, "Hint": "The day it goes live.", "Error": "Pick a date.",
			"Min": "2026-01-01", "Max": "2026-12-31",
		}},
		{"field-time", map[string]any{
			"Name": "doors", "Label": "Doors open", "Value": "19:30",
			"Required": true, "Hint": "Local time.", "Error": "Pick a time.",
			"Min": "09:00", "Max": "23:00",
		}},
		{"field-datetime", map[string]any{
			// Deliberately not "starts_at": the daterange fixture below
			// uses that Name, and TestRenderEverythingSmoke renders every
			// fixture into one document, where two fields sharing a Name
			// would share ids too.
			"Name": "published_at", "Label": "Publish at", "Value": "2026-08-28T19:30",
			"Required": true, "Hint": "When it goes live.", "Error": "Pick a time.",
			"Min": "2026-01-01T00:00", "Max": "2026-12-31T23:59",
		}},
		{"field-daterange", map[string]any{
			"Legend": "When", "Seed": "session",
			"Start": map[string]any{"Name": "starts_at", "Label": "Starts", "Value": "2026-08-28T19:30"},
			"End":   map[string]any{"Name": "ends_at", "Label": "Ends", "Error": "The end comes before the start."},
		}},
	}
}

// fixtureFor looks one fixture up by partial name. Assertions never index
// allPartials() positionally: reordering that slice would otherwise
// silently re-point a test at a different partial and still pass.
func fixtureFor(t *testing.T, name string) map[string]any {
	t.Helper()
	for _, p := range allPartials() {
		if p.Name == name {
			return p.Data
		}
	}
	t.Fatalf("no fixture defined for partial %q", name)
	return nil
}

// T threading (task 5, §10): a partial's hardcoded-English default now
// resolves through T, which Funcs binds to the framework base catalog —
// and FuncsWith lets an app rebind every one of those defaults at once,
// e.g. to a request-scoped rastrillo.T lookup, without touching the
// partials themselves.
func TestUIDefaultsResolveAndRebind(t *testing.T) {
	got := render(t, "pagination", map[string]any{})
	if !strings.Contains(got, `aria-label="Pagination"`) {
		t.Errorf("default label lost: %s", got)
	}
	// list-bar-search's aria-label default reuses rastrillo.ui.search_submit
	// (see basecatalog.go) rather than a key of its own — resolve and
	// rebind it the same way, so that reuse is actually exercised.
	if got := render(t, "list-bar-search", map[string]any{}); !strings.Contains(got, `aria-label="Search"`) {
		t.Errorf("default aria-label lost: %s", got)
	}
	// bulk-bar's DoneLabel default (rastrillo.ui.done) — without it the
	// icon-only close link would render aria-label="".
	if got := render(t, "bulk-bar", map[string]any{"DoneHref": "/orders", "Count": "3 selected", "MenuLabel": "Actions"}); !strings.Contains(got, `aria-label="Done selecting"`) {
		t.Errorf("default DoneLabel lost: %s", got)
	}
	// FuncsWith rebinds every default.
	tmpl := template.Must(template.New("").Funcs(FuncsWith(func(key string, _ ...any) string {
		return "X-" + key
	})).ParseFS(Templates(), "*.html"))
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "pagination", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `aria-label="X-rastrillo.ui.pagination"`) {
		t.Errorf("FuncsWith did not rebind T: %s", buf.String())
	}
	buf.Reset()
	if err := tmpl.ExecuteTemplate(&buf, "list-bar-search", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `aria-label="X-rastrillo.ui.search_submit"`) {
		t.Errorf("FuncsWith did not rebind list-bar-search's aria-label default: %s", buf.String())
	}
	buf.Reset()
	if err := tmpl.ExecuteTemplate(&buf, "bulk-bar", map[string]any{"DoneHref": "/orders", "Count": "3 selected", "MenuLabel": "Actions"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `aria-label="X-rastrillo.ui.done"`) {
		t.Errorf("FuncsWith did not rebind bulk-bar's DoneLabel default: %s", buf.String())
	}
}

// A caller-supplied value always wins over T's default — the T threading
// must never override an explicit Label/CancelLabel/Placeholder.
func TestUIDefaultsYieldToExplicitValues(t *testing.T) {
	if got := render(t, "pagination", map[string]any{"Label": "Paginación"}); !strings.Contains(got, `aria-label="Paginación"`) {
		t.Errorf("explicit Label lost to T's default: %s", got)
	}
	if got := render(t, "list-search-submit", map[string]any{"Label": "Buscar"}); !strings.Contains(got, ">Buscar<") {
		t.Errorf("explicit Label lost to T's default: %s", got)
	}
	if got := render(t, "list-bar-search", map[string]any{"Placeholder": "Buscar entradas"}); !strings.Contains(got, `aria-label="Buscar entradas"`) {
		t.Errorf("explicit Placeholder lost to T's default: %s", got)
	}
	if got := render(t, "bulk-bar", map[string]any{
		"DoneHref": "/orders", "DoneLabel": "Terminar selección", "Count": "3 selected", "MenuLabel": "Actions",
	}); !strings.Contains(got, `aria-label="Terminar selección"`) {
		t.Errorf("explicit DoneLabel lost to T's default: %s", got)
	}
	got := render(t, "confirm-form", map[string]any{
		"Action": "/x", "Label": "Delete", "CancelHref": "/x", "CancelLabel": "Never mind",
	})
	if !strings.Contains(got, ">Never mind</a>") {
		t.Errorf("explicit CancelLabel lost to T's default: %s", got)
	}
}

// All partials are present and named exactly as documented.
func TestAllPartialsAreDefined(t *testing.T) {
	tmpl := parseAll(t)
	want := []string{
		"page-header", "list-bar", "list-bar-search", "list-search-submit",
		"list-row-action", "status-pill", "empty-state", "pagination",
		"badge", "meter", "person", "callout", "detail-list", "dropdown",
		"field", "field-select", "field-text", "field-textarea", "field-check", "choice-field", "seg-tabs",
		"confirm-form", "back-nav", "notice", "form-error", "form-foot", "bulk-bar", "job-status",
		"locale-menu", "error-page",
		"field-date", "field-time", "field-datetime", "field-daterange",
	}
	for _, name := range want {
		if tmpl.Lookup(name) == nil {
			t.Errorf("partial %q is not defined", name)
		}
	}
	if len(want) != 34 {
		t.Fatalf("the shipped set is 34 partials, this list has %d", len(want))
	}
}

// Rendered output reaches nothing outside the page: no off-origin fetch,
// no script, no remote asset. Mirrors icons_test.go's
// TestVendoredIconsAreSelfContained, adapted from one SVG string to one
// rendered partial's HTML.
func TestRenderedPartialsAreSelfContained(t *testing.T) {
	for _, p := range allPartials() {
		got := render(t, p.Name, p.Data)
		for _, bad := range []string{
			"http://", "https://", "//cdn", "<script", "<iframe", "<img",
			"<link ", "url(", "xlink:href", "@import",
		} {
			if strings.Contains(got, bad) {
				t.Errorf("partial %q reaches outside the page (%q):\n%s", p.Name, bad, got)
			}
		}
	}
}

// The one non-partial asset this package ships gets the same bar as the
// partials and the vendored icons.
func TestTokensCSSIsSelfContained(t *testing.T) {
	reachOut := []string{"@import", "url(", "http://", "https://", "//fonts", "src:"}
	css := string(TokensCSS())
	for _, bad := range reachOut {
		if strings.Contains(css, bad) {
			t.Errorf("tokens.css reaches outside the page (%q)", bad)
		}
	}
	// The promise spans both scaffolded stylesheets: a theme is where a
	// webfont would be most tempting, and it is exactly as banned there.
	for _, name := range ThemeNames() {
		theme, ok := ThemeCSS(name)
		if !ok {
			t.Fatalf("ThemeCSS(%q) missing", name)
		}
		for _, bad := range reachOut {
			if strings.Contains(string(theme), bad) {
				t.Errorf("themes/%s.css reaches outside the page (%q)", name, bad)
			}
		}
	}
}

// Every interactive element renders with a real accessible name
// (spec §10). Checked here rather than per-partial so a new partial
// cannot quietly opt out.
func TestEveryControlHasAnAccessibleName(t *testing.T) {
	search := render(t, "list-bar-search", fixtureFor(t, "list-bar-search"))
	if !strings.Contains(search, "aria-label=") {
		t.Errorf("the search input has no accessible name: %s", search)
	}
	if !strings.Contains(search, `type="submit">Search posts</button>`) {
		t.Errorf("the submit control has no text: %s", search)
	}
	row := render(t, "list-row-action", fixtureFor(t, "list-row-action"))
	if !strings.Contains(row, `aria-label="Edit Release notes, August"`) {
		t.Errorf("the row action pill has no disambiguating name: %s", row)
	}
	page := render(t, "pagination", fixtureFor(t, "pagination"))
	if !strings.Contains(page, `aria-label="Pagination"`) {
		t.Errorf("the pagination nav has no accessible name: %s", page)
	}
	field := render(t, "field", fixtureFor(t, "field"))
	if !strings.Contains(field, `<label class="rst-field__label" for="email">`) {
		t.Errorf("the field's input has no wired label: %s", field)
	}
	choice := render(t, "choice-field", fixtureFor(t, "choice-field"))
	if !strings.Contains(choice, "<legend>Plan</legend>") {
		t.Errorf("choice-field's legend did not render: %s", choice)
	}
	check := render(t, "field-check", fixtureFor(t, "field-check"))
	trackEnd := strings.Index(check, `</span>`)
	if trackEnd == -1 || !strings.Contains(check[trackEnd:], "Email me about replies") {
		t.Errorf("field-check's label text must render outside the aria-hidden track: %s", check)
	}
}

// The styleguide equivalent: one pass renders every partial together,
// the combined output is balanced, and each partial left its marker.
func TestRenderEverythingSmoke(t *testing.T) {
	tmpl := parseAll(t)
	var buf strings.Builder
	buf.WriteString(`<div class="rst-page">`)
	for _, p := range allPartials() {
		if err := tmpl.ExecuteTemplate(&buf, p.Name, p.Data); err != nil {
			t.Fatalf("ExecuteTemplate(%q): %v", p.Name, err)
		}
	}
	buf.WriteString(`</div>`)
	out := buf.String()

	markers := map[string]string{
		"page-header":        `<header class="rst-page-header">`,
		"list-bar":           `<div class="rst-lbar">`,
		"list-bar-search":    `<form class="rst-search"`,
		"list-search-submit": `<button class="rst-sr-only" type="submit">`,
		"list-row-action":    `<div class="rst-row">`,
		"status-pill":        `<span class="rst-status"`,
		"empty-state":        `<div class="rst-empty">`,
		"pagination":         `<nav class="rst-pagination"`,
	}
	for name, marker := range markers {
		if !strings.Contains(out, marker) {
			t.Errorf("smoke output is missing %s (%q)", name, marker)
		}
	}

	for _, tag := range []string{"div", "form", "header", "nav", "a", "p", "h1", "h2", "span", "small", "button", "svg"} {
		open, closed := countOpenTags(out, tag), strings.Count(out, "</"+tag+">")
		if open != closed {
			t.Errorf("<%s> is unbalanced: %d opened, %d closed", tag, open, closed)
		}
	}

	// One page, one id. Every partial above renders into the same
	// document, which is the situation a real screen is in — and a
	// duplicate id there is not cosmetic: a label's `for` points at the
	// first match, aria-describedby and aria-controls resolve to the
	// first match, and the second field quietly loses its name to a
	// screen reader. Cheap to check here and invisible everywhere else,
	// because each partial rendered alone is always fine.
	seen := make(map[string]int)
	var dupes []string
	for _, m := range idAttr.FindAllStringSubmatch(out, -1) {
		seen[m[1]]++
		if seen[m[1]] == 2 {
			dupes = append(dupes, m[1])
		}
	}
	sort.Strings(dupes)
	if len(dupes) > 0 {
		t.Errorf("the combined render repeats id %v; give the fixtures distinct Names or the partials distinct ids", dupes)
	}
}

// idAttr matches a rendered id attribute. The partials only ever emit
// double-quoted attributes (html/template writes them that way), so one
// shape is enough.
var idAttr = regexp.MustCompile(`\bid="([^"]*)"`)

// countOpenTags counts opening tags for one element name, matching both
// "<tag " and "<tag>" so <p> is never confused with <path>.
func countOpenTags(s, tag string) int {
	return strings.Count(s, "<"+tag+" ") + strings.Count(s, "<"+tag+">")
}

func TestListRowActionRendersAStatusPill(t *testing.T) {
	got := render(t, "list-row-action", map[string]any{
		"Href": "/admin/posts/1/edit", "Main": "Release notes",
		"StatusTone": "positive", "StatusLabel": "Published",
		"ActionHref": "/posts/1", "ActionLabel": "View",
	})
	if !strings.Contains(got, `<span class="rst-status" data-tone="positive">Published</span>`) {
		t.Errorf("status pill missing or wrong: %s", got)
	}
	// The pill sits in the right-hand group, before the action pill.
	if strings.Index(got, `class="rst-status"`) > strings.Index(got, `class="rst-row__action"`) {
		t.Errorf("status pill rendered after the action pill: %s", got)
	}
}

func TestListRowActionStatusPillAbsentByDefault(t *testing.T) {
	got := render(t, "list-row-action", map[string]any{"Href": "/p", "Main": "M"})
	if strings.Contains(got, "rst-status") {
		t.Errorf("status pill rendered without StatusLabel: %s", got)
	}
}

// F10 regression (examples/blog friction log): the class the partial
// emits for a disabled chip and the selector tokens.css styles must be
// the same string — they drifted apart once, leaving a disabled
// Previous visually identical to a live link.
func TestDisabledPaginationChipIsStyled(t *testing.T) {
	got := render(t, "pagination", fixtureFor(t, "pagination"))
	if !strings.Contains(got, `class="rst-pagination__disabled"`) {
		t.Errorf("disabled item lost its class: %s", got)
	}
	css := string(TokensCSS())
	if !strings.Contains(css, ".rst-pagination__disabled") {
		t.Errorf("tokens.css no longer styles .rst-pagination__disabled")
	}
	if strings.Contains(css, `.rst-pagination [aria-disabled=`) {
		t.Errorf("tokens.css still carries the dead aria-disabled pagination rule no partial emits")
	}
}

// Same drift check as TestDisabledPaginationChipIsStyled, extended to the
// classes the five display partials added in this batch emit: every class
// a partial can produce must have a styled selector in tokens.css, so
// nothing new ships unstyled.
func TestDisplayPartialClassesAreStyled(t *testing.T) {
	css := string(TokensCSS())
	for _, class := range []string{
		"rst-badge", "rst-badge--warning", "rst-meter", "rst-meter__bar", "rst-meter__num",
		"rst-person", "rst-person__av", "rst-callout", "rst-callout__ic", "rst-callout__body",
		"rst-detail", "rst-mono",
	} {
		if !strings.Contains(css, "."+class) {
			t.Errorf("tokens.css has no selector for %q", class)
		}
	}
}

// Same drift check again, for the form family this task adds: field,
// field-select, field-textarea, field-check and choice-field between
// them can emit every one of these classes.
func TestFormPartialClassesAreStyled(t *testing.T) {
	css := string(TokensCSS())
	for _, class := range []string{
		"rst-field", "rst-field__label", "rst-field__hint", "rst-field__help", "rst-field__error",
		"rst-input", "rst-input--short",
		"rst-switch", "rst-switch__track",
		"rst-choice", "rst-choice__cards", "rst-choice__title", "rst-choice__desc",
		"rst-seg-tabs",
	} {
		if !strings.Contains(css, "."+class) {
			t.Errorf("tokens.css has no selector for %q", class)
		}
	}
}

// Help renders under the control wired via aria-describedby; Error
// replaces it (never both at once) and additionally marks the control
// aria-invalid and its own message role=alert.
func TestFieldWiresHelpAndError(t *testing.T) {
	help := render(t, "field", map[string]any{"ID": "f1", "Name": "n", "Label": "L", "Help": "h"})
	if !strings.Contains(help, `aria-describedby="f1-help"`) || !strings.Contains(help, `id="f1-help"`) {
		t.Errorf("Help not wired via aria-describedby: %s", help)
	}
	errd := render(t, "field", map[string]any{"ID": "f1", "Name": "n", "Label": "L", "Help": "h", "Error": "bad"})
	if !strings.Contains(errd, `aria-invalid="true"`) || !strings.Contains(errd, `role="alert"`) {
		t.Errorf("Error not wired: %s", errd)
	}
	if strings.Contains(errd, "f1-help") {
		t.Errorf("Error replaces Help — both rendered: %s", errd)
	}
}

// The switch is a real checkbox: keyboard and AT operate the actual
// input, and the visible track is aria-hidden decoration on top of it.
func TestFieldCheckIsARealCheckbox(t *testing.T) {
	got := render(t, "field-check", map[string]any{"Name": "on", "Label": "Enable", "Checked": true})
	for _, want := range []string{`type="checkbox"`, "checked", `aria-hidden="true"`, "rst-switch__track"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
}

// Exactly one tab is aria-current at a time — the accessibility signal,
// not only the CSS that highlights it.
func TestSegTabsMarksCurrent(t *testing.T) {
	got := render(t, "seg-tabs", map[string]any{"Label": "Sections", "Items": []any{
		map[string]any{"Label": "Basics", "Href": "?tab=basics", "Current": true},
		map[string]any{"Label": "Advanced", "Href": "?tab=advanced"},
	}})
	if !strings.Contains(got, `aria-current="page">Basics`) {
		t.Errorf("current tab unmarked: %s", got)
	}
	if strings.Count(got, "aria-current") != 1 {
		t.Errorf("exactly one current tab: %s", got)
	}
}

// confirm-form's Cancel is the group's first element in the DOM — no CSS
// reorders it. DOM order, visual order (.rst-form-actions is a plain
// flex row, no order property), and tab order therefore all agree:
// Cancel, then the submit. The destructive control is never the first
// focusable element in the group, so a keyboard user tabbing forward
// always meets Cancel before they can reach it.
func TestConfirmFormShape(t *testing.T) {
	got := render(t, "confirm-form", fixtureFor(t, "confirm-form"))
	for _, want := range []string{`method="post"`, `action="/orders/1/refund"`,
		`<input type="hidden" name="csrf" value="tok">`, "rst-btn--danger",
		`href="/orders/1"`, ">Cancel</a>"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
	// Cancel is an <a>, never a submit — a GET never mutates.
	if strings.Contains(got, `type="submit">Cancel`) {
		t.Errorf("cancel must be a link: %s", got)
	}
	// Cancel precedes the submit button in the DOM — the sole source of
	// both visual order and tab order now that no CSS reorders them.
	if cancel, submit := strings.Index(got, `<a class="rst-btn rst-btn--ghost"`), strings.Index(got, `<button type="submit"`); cancel == -1 || submit == -1 || cancel > submit {
		t.Errorf("cancel must precede the submit button in the DOM: %s", got)
	}
	// Hidden inputs render in the caller's slice order, not key-sorted —
	// [][2]string has no keys to sort. The fixture passes csrf before id
	// deliberately (see confirm-form.html's Hidden doc comment), so this
	// asserts the caller's own ordering came through unchanged; it is not
	// a guarantee the partial itself makes about any particular field.
	if csrf, id := strings.Index(got, `name="csrf"`), strings.Index(got, `name="id"`); csrf == -1 || id == -1 || csrf > id {
		t.Errorf("hidden inputs must render in caller order (fixture puts csrf before id): %s", got)
	}
}

func TestBackNavRendersArrowLink(t *testing.T) {
	got := render(t, "back-nav", fixtureFor(t, "back-nav"))
	if !strings.Contains(got, `<nav class="rst-back-nav">`) || !strings.Contains(got, `href="/orders/1"`) || !strings.Contains(got, "← Order AB3PX") {
		t.Errorf("missing back-nav shape: %s", got)
	}
}

// notice and form-error are string-data partials — the partial's dot is
// the message itself, not a dict-built map — so render() is called with
// a plain string. Both render nothing at all for an empty string, rather
// than an empty-but-present element: a caller can call {{template
// "notice" .Flash}} unconditionally, flash message or not.
func TestNoticeRendersMessageOrNothing(t *testing.T) {
	if got := render(t, "notice", ""); strings.TrimSpace(got) != "" {
		t.Errorf("empty notice should render nothing: %q", got)
	}
	got := render(t, "notice", "Refund sent.")
	if !strings.Contains(got, `role="status"`) || !strings.Contains(got, "Refund sent.") {
		t.Errorf("missing notice text or role: %s", got)
	}
}

func TestFormErrorRendersMessageOrNothing(t *testing.T) {
	if got := render(t, "form-error", ""); strings.TrimSpace(got) != "" {
		t.Errorf("empty form-error should render nothing: %q", got)
	}
	got := render(t, "form-error", "Amount is required.")
	if !strings.Contains(got, `role="alert"`) || !strings.Contains(got, "Amount is required.") {
		t.Errorf("missing form-error text or role: %s", got)
	}
}

// The actions menu's items are real submit buttons on the surrounding
// form (name="action", value per item) — not links, not onclick — so
// bulk operations work with JavaScript off exactly like every other
// mutation in this library. The close control is icon-only, so its
// accessible name has to come from somewhere other than visible text.
func TestBulkBarActionsAreRealSubmits(t *testing.T) {
	fixture := fixtureFor(t, "bulk-bar")
	got := render(t, "bulk-bar", fixture)
	if !strings.Contains(got, `<button type="submit" name="action" value="refund" class="rst-danger">`) {
		t.Errorf("actions must be named submit buttons on the surrounding form: %s", got)
	}
	if !strings.Contains(got, `aria-label=`) {
		t.Errorf("the close control needs an accessible name: %s", got)
	}
	// The first Actions entry is the surrounding form's implicit-Enter
	// default — Enter in any text field on the form submits it, whether
	// or not the menu is even open — so it must never be the
	// destructive one, and the rendered danger button must be the last
	// button in the menu, not merely somewhere after the first.
	actions, ok := fixture["Actions"].([]any)
	if !ok || len(actions) == 0 {
		t.Fatalf("fixture has no Actions to check ordering against")
	}
	if first, ok := actions[0].(map[string]any); ok && first["Danger"] == true {
		t.Errorf("the fixture's first action is the form's implicit-Enter default and must not be destructive: %+v", first)
	}
	lastButton, dangerAttr := strings.LastIndex(got, "<button "), strings.Index(got, `class="rst-danger"`)
	if lastButton == -1 || dangerAttr == -1 || dangerAttr < lastButton {
		t.Errorf("the danger button must be the last button in the actions menu: %s", got)
	}
}

// The samples are static HTML with no template actions, so parsing them
// through the ui funcs is enough to prove they are well-formed
// standalone markup a styleguide page can Execute verbatim.
func TestStyleguideSamplesRender(t *testing.T) {
	for name, sample := range Styleguide() {
		tmpl, err := template.New(name).Funcs(Funcs()).Parse(sample)
		if err != nil {
			t.Fatalf("%s: Parse: %v", name, err)
		}
		var buf strings.Builder
		if err := tmpl.Execute(&buf, nil); err != nil {
			t.Fatalf("%s: Execute: %v", name, err)
		}
		out := buf.String()
		if out == "" {
			t.Errorf("%s: rendered empty", name)
		}
		// No sample reaches for a <script>: the modal shell, toggle-block
		// reveal, and bulk-bar actions menu are all zero-JS by design
		// (own-URL navigation, :has(), and real submit buttons,
		// respectively), and this check applies across every sample —
		// old and new — so a future one cannot quietly opt out.
		if strings.Contains(out, "<script") {
			t.Errorf("%s: reaches for <script>; this vocabulary is zero-JS: %s", name, out)
		}
		// dialog, nav and search joined the list when the interactive
		// idioms stopped being divs playing those roles: an unbalanced
		// <dialog> is the one that would silently swallow the rest of a
		// page rather than merely look wrong.
		for _, tag := range []string{"div", "details", "section", "a", "span", "dialog", "nav", "search"} {
			open, closed := countOpenTags(out, tag), strings.Count(out, "</"+tag+">")
			if open != closed {
				t.Errorf("%s: <%s> is unbalanced: %d opened, %d closed", name, tag, open, closed)
			}
		}
	}
}

// rstClassPattern extracts one rst- class token, including its optional
// BEM __element and --modifier suffixes.
var rstClassPattern = regexp.MustCompile(`rst-[a-z-]+(?:__[a-z-]+)?(?:--[a-z-]+)?`)

// classAttrPattern isolates class="..." attribute values, so extraction
// runs over actual class tokens rather than the whole sample string —
// the list-grid sample's inline `style="--rst-cols: …"` also matches
// rstClassPattern (as "rst-cols"), but --rst-cols is a custom property
// read with var(), never a class selector, and checking it against
// tokens.css with a leading "." would be a false positive.
var classAttrPattern = regexp.MustCompile(`class="([^"]*)"`)

// TestIdiomClassesAreStyled is the F10 lesson in both directions: every
// class a sample emits must have a selector in tokens.css (a sample
// cannot reference a class that does not exist), and every selector this
// task added must be exercised by some sample (an idiom cannot ship
// undemonstrated).
func TestIdiomClassesAreStyled(t *testing.T) {
	css := string(TokensCSS())
	seen := map[string]bool{}
	for _, sample := range Styleguide() {
		for _, attr := range classAttrPattern.FindAllStringSubmatch(sample, -1) {
			for _, class := range rstClassPattern.FindAllString(attr[1], -1) {
				seen[class] = true
			}
		}
	}
	for class := range seen {
		if !strings.Contains(css, "."+class) {
			t.Errorf("tokens.css has no selector for %q (used in a styleguide sample)", class)
		}
	}

	// The selectors this task's Step 1 added to tokens.css, listed
	// literally: each one must appear in at least one sample above.
	for _, class := range []string{
		"rst-box", "rst-box-head", "rst-box-foot",
		"rst-card", "rst-lrow", "rst-lrow--head", "rst-m-hide", "rst-nm", "rst-cell-mut",
		"rst-no-match", "rst-count-line",
		"rst-row-menu", "rst-row-menu__panel", "rst-danger",
		"rst-dropdown", "rst-dropdown__menu", "rst-menu-group", "rst-caret",
		"rst-ftok",
	} {
		if !seen[class] {
			t.Errorf("selector %q was added to tokens.css this task but no styleguide sample uses it", class)
		}
	}

	// Task 3's form-layout selectors: no partial emits these (they wrap a
	// caller-composed run of fields, not a single data shape), so the
	// "form-layout" sample above is their only exercise.
	for _, class := range []string{
		"rst-form-flow", "rst-field-row", "rst-grow", "rst-form-foot", "rst-form-foot__note", "rst-form-actions",
	} {
		if !seen[class] {
			t.Errorf("selector %q was added to tokens.css in the form-layout task but no styleguide sample uses it", class)
		}
	}

	// Task 4's class-idiom selectors (toggle-block, modal route, help,
	// selbox) — the "tblock", "modal", "help", and "selbox" samples above
	// are their only exercise, the same way box/list-grid/dropdown/ftok
	// are Task 2's and form-layout is Task 3's. bulk-bar's own classes
	// (rst-bulkbar*) are excluded here on purpose: bulk-bar is a real
	// partial, already exercised by allPartials()/TestRenderEverythingSmoke,
	// and checked directly in TestRoutesFamilyPartialClassesAreStyled below.
	for _, class := range []string{
		"rst-tblock", "rst-tblock__head", "rst-tblock__title", "rst-tblock__desc", "rst-tblock__body",
		"rst-backdrop", "rst-modal-overlay", "rst-modal-panel", "rst-modal-close",
		"rst-help", "rst-tip", "rst-selbox",
	} {
		if !seen[class] {
			t.Errorf("selector %q was added to tokens.css in the routes-family task but no styleguide sample uses it", class)
		}
	}
	// The shell selectors: same rule again for the page frames. A shell
	// is markup an app's own layout template writes, so no partial and
	// no other sample can carry these — the two shell samples above are
	// their only exercise, in both directions.
	for _, class := range []string{
		"rst-skip",
		"rst-shell-topbar", "rst-shell__bar", "rst-shell__brand", "rst-shell__nav",
		"rst-shell__account", "rst-shell__foot",
		"rst-shell-sidebar", "rst-shell__chrome", "rst-shell__rail", "rst-shell__group", "rst-shell__main",
	} {
		if !seen[class] {
			t.Errorf("selector %q was added to tokens.css in the shells task but no styleguide sample uses it", class)
		}
	}
}

// TestEveryEmbeddedThemeAndLayoutIsNamed closes the ungated-theme path.
// ThemeCSS and Layout read the embedded FS by filename, but every gate
// that checks a theme or a shell — the token-set parity check, the WCAG
// contrast sweep, TestLayoutsParseAndRender — iterates ThemeNames() and
// LayoutNames() instead. So a themes/foo.css added without its slice
// entry is scaffoldable (rastrillo new --theme=foo finds the file) while
// no gate ever looks at it. This test is the one place the directory
// itself is read, so the file set and the name list cannot drift apart.
func TestEveryEmbeddedThemeAndLayoutIsNamed(t *testing.T) {
	for _, tc := range []struct {
		dir   string
		ext   string
		fsys  fs.FS
		names []string
	}{
		{"themes", ".css", themesFS, ThemeNames()},
		{"layouts", ".html", layoutsFS, LayoutNames()},
	} {
		t.Run(tc.dir, func(t *testing.T) {
			ents, err := fs.ReadDir(tc.fsys, tc.dir)
			if err != nil {
				t.Fatalf("ReadDir(%s): %v", tc.dir, err)
			}
			onDisk := map[string]bool{}
			for _, e := range ents {
				onDisk[e.Name()] = true
			}
			named := map[string]bool{}
			for _, n := range tc.names {
				named[n+tc.ext] = true
			}
			for f := range onDisk {
				if !named[f] {
					t.Errorf("%s/%s is embedded but not in the name list: it is scaffoldable and ungated", tc.dir, f)
				}
			}
			for f := range named {
				if !onDisk[f] {
					t.Errorf("the name list promises %s/%s, which is not embedded", tc.dir, f)
				}
			}
		})
	}
}

// The same drift check the styleguide samples get, for the shells that
// ship as whole templates: every rst- class the layout files emit must
// resolve to a selector in tokens.css. TestIdiomClassesAreStyled covers
// the samples; a layout is markup no sample carries, so without this a
// shell could name a class the stylesheet never defines.
func TestLayoutClassesAreStyled(t *testing.T) {
	css := string(TokensCSS())
	for _, name := range LayoutNames() {
		src, ok := Layout(name)
		if !ok {
			t.Fatalf("Layout(%q) missing", name)
		}
		for _, attr := range classAttrPattern.FindAllStringSubmatch(string(src), -1) {
			for _, class := range rstClassPattern.FindAllString(attr[1], -1) {
				if !strings.Contains(css, "."+class) {
					t.Errorf("tokens.css has no selector for %q (used in layouts/%s.html)", class, name)
				}
			}
		}
	}
}

// Same drift check again, direct rather than sample-driven: the classes
// this task's partials (confirm-form, back-nav, notice, form-error,
// bulk-bar) emit themselves, so there is no arbitrary caller-composed
// body for a styleguide sample to carry them — allPartials() already
// exercises confirm-form/back-nav/bulk-bar's fixtures, and this pins
// that every class they can render resolves to a tokens.css selector.
func TestRoutesFamilyPartialClassesAreStyled(t *testing.T) {
	css := string(TokensCSS())
	for _, class := range []string{
		"rst-btn--ghost", "rst-btn--danger",
		"rst-back-nav", "rst-notice", "rst-form-error",
		"rst-bulkbar", "rst-bulkbar__close", "rst-bulkbar__count", "rst-bulkbar__escalate",
	} {
		if !strings.Contains(css, "."+class) {
			t.Errorf("tokens.css has no selector for %q", class)
		}
	}
}

// The same drift check once more, for the jobs task's one partial.
// job-status emits exactly two classes and only one of them belongs
// here: rst-spin is real styling (the working indicator, checked
// against tokens.css below), while rst-job is a deliberate exception —
// a semantic hook the shim finds by data-poll and an app can key its
// own CSS off, never a class tokens.css styles. Recording that here in
// a comment, the way reducedMotionAllowlist records its exceptions, so
// a future reader does not "fix" it by inventing a rule for it.
func TestJobStatusPartialClassesAreStyled(t *testing.T) {
	css := string(TokensCSS())
	for _, class := range []string{"rst-spin"} {
		if !strings.Contains(css, "."+class) {
			t.Errorf("tokens.css has no selector for %q", class)
		}
	}
}

// The dropdown's exclusivity (only one open at a time) is the native
// <details name> attribute, not JavaScript — this pins both halves of
// that promise.
func TestDropdownExclusivityIsNative(t *testing.T) {
	sample := Styleguide()["dropdown"]
	if !strings.Contains(sample, `<details class="rst-dropdown" name=`) {
		t.Errorf("dropdown sample's outer <details> carries no name attribute: %s", sample)
	}
	if strings.Contains(sample, "<script") {
		t.Errorf("dropdown sample reaches for <script>; exclusivity must stay native: %s", sample)
	}
}

// cssComment matches a whole /* … */ block, so a check can read
// tokens.css's selectors without its prose about them.
var cssComment = regexp.MustCompile(`(?s)/\*.*?\*/`)

// detailsNamePattern reads the name attribute off a <details> open tag,
// whichever order the attributes are written in.
var detailsNamePattern = regexp.MustCompile(`<details [^>]*name="([^"]*)"`)

// Every menu the library emits joins one shared exclusivity group by
// default, so opening a row menu closes an open header dropdown without
// a line of script. The default is a value, not an absence: a <details>
// with no name is in no group at all and would stay open beside the one
// the user just opened, which is the bug this defends.
func TestEveryMenuDefaultsToTheSharedExclusivityGroup(t *testing.T) {
	const group = `name="rst-menus"`

	for _, c := range []struct {
		partial string
		data    map[string]any
	}{
		{"dropdown", map[string]any{"Label": "Sort", "Items": []any{
			map[string]any{"Href": "/x", "Label": "Newest"}}}},
		{"locale-menu", map[string]any{"Items": []rastrillo.LocaleItem{{Code: "ga", Name: "Gaeilge", Href: "/ga"}}}},
		{"bulk-bar", map[string]any{
			"DoneHref": "/x", "Count": "3 selected", "MenuLabel": "Actions",
			"Actions": []any{map[string]any{"Value": "archive", "Label": "Archive"}}}},
	} {
		got := render(t, c.partial, c.data)
		if !strings.Contains(got, group) {
			t.Errorf("%s renders no %s, so it is in no exclusivity group: %s", c.partial, group, got)
		}
	}

	// The topbar shell's account menu is a dropdown the layout owns
	// rather than a partial, so it needs the same default written into
	// the layout — otherwise the one menu a user meets on every screen is
	// the one that never closes.
	layout, ok := Layout("topbar")
	if !ok {
		t.Fatal(`Layout("topbar") reports no such layout`)
	}
	if !strings.Contains(string(layout), `rst-shell__account" `+group) {
		t.Errorf("the topbar shell's account dropdown is outside the exclusivity group:\n%s", layout)
	}

	// The class idioms carry it in their canonical samples, which is the
	// only place an app copying markup by hand will read it from.
	for _, name := range []string{"dropdown", "list-grid", "shell-topbar"} {
		if !strings.Contains(Styleguide()[name], group) {
			t.Errorf("styleguide sample %q shows a menu outside the exclusivity group", name)
		}
	}

	// And the two <details> that are NOT menus stay out of it. The
	// sidebar's chrome strip is the narrow-screen nav rail: closing it
	// because a filter opened would take the whole navigation away.
	sidebar, ok := Layout("sidebar")
	if !ok {
		t.Fatal(`Layout("sidebar") reports no such layout`)
	}
	if strings.Contains(string(sidebar), group) {
		t.Errorf("the sidebar shell's chrome strip joined the menu exclusivity group:\n%s", sidebar)
	}
	if strings.Contains(Styleguide()["shell-sidebar"], group) ||
		strings.Contains(Styleguide()["tblock"], group) {
		t.Error("shell chrome or the toggle-block joined the menu exclusivity group")
	}
}

// A caller who wants a menu in a group of its own says so, and their
// name wins over the default everywhere the default is offered.
func TestMenuGroupOverrideWins(t *testing.T) {
	for _, c := range []struct {
		partial string
		data    map[string]any
	}{
		{"dropdown", map[string]any{"MenuGroup": "list-controls", "Label": "Sort",
			"Items": []any{map[string]any{"Href": "/x", "Label": "Newest"}}}},
		{"locale-menu", map[string]any{"MenuGroup": "list-controls",
			"Items": []rastrillo.LocaleItem{{Code: "ga", Name: "Gaeilge", Href: "/ga"}}}},
		{"bulk-bar", map[string]any{"MenuGroup": "list-controls",
			"DoneHref": "/x", "Count": "3 selected", "MenuLabel": "Actions",
			"Actions": []any{map[string]any{"Value": "archive", "Label": "Archive"}}}},
	} {
		got := render(t, c.partial, c.data)
		if !strings.Contains(got, `name="list-controls"`) {
			t.Errorf("%s ignored MenuGroup: %s", c.partial, got)
		}
		if strings.Contains(got, `name="rst-menus"`) {
			t.Errorf("%s emitted both the override and the default: %s", c.partial, got)
		}
	}
}

// The submenu trap, pinned. <details name> exclusivity is DOCUMENT-wide,
// not sibling-scoped: two elements sharing a name close each other even
// when one is inside the other. A nested rst-menu-group that inherited
// its parent's group would therefore close the parent the instant it
// opened — the submenu flashes and the whole menu vanishes under the
// pointer. The nested sample must carry a name of its own, and it must
// not be the parent's.
func TestNestedMenuGroupNeverSharesItsParentsName(t *testing.T) {
	sample := Styleguide()["dropdown"]
	names := detailsNamePattern.FindAllStringSubmatch(sample, -1)
	if len(names) != 2 {
		t.Fatalf("expected two named <details> in the dropdown sample, found %d: %s", len(names), sample)
	}
	if !strings.Contains(sample, "rst-menu-group") {
		t.Fatalf("the dropdown sample no longer nests an rst-menu-group: %s", sample)
	}
	if names[0][1] == names[1][1] {
		t.Errorf("the nested rst-menu-group shares its parent's group %q; opening it would close the parent", names[0][1])
	}
}

// The modal panel is <dialog open>: the rendered-open, non-modal dialog,
// which is exactly what a modal-as-a-URL already was. Nothing calls
// showModal(), so nothing enters the top layer, ::backdrop never paints,
// and the overlay div stays the scrim. The UA's own dialog block has to
// be undone or the panel lays out absolutely positioned with 1em of
// padding.
func TestModalPanelIsARenderedOpenDialog(t *testing.T) {
	sample := Styleguide()["modal"]
	for _, want := range []string{
		`<dialog class="rst-modal-panel" open `, `</dialog>`,
		`<div class="rst-modal-overlay">`, `<div class="rst-backdrop" inert>`,
	} {
		if !strings.Contains(sample, want) {
			t.Errorf("modal sample missing %q: %s", want, sample)
		}
	}
	if strings.Contains(sample, `<div class="rst-modal-panel">`) {
		t.Errorf("the modal panel is still a div: %s", sample)
	}
	// A dialog role with no accessible name is announced as "dialog" and
	// nothing else, and fails axe's aria-dialog-name — a failure this
	// vocabulary created for itself the moment the panel stopped being a
	// div. The name comes from the panel's own heading rather than a
	// string in this library, so it is already in the page's language.
	id := regexp.MustCompile(`aria-labelledby="([^"]+)"`).FindStringSubmatch(sample)
	if id == nil {
		t.Fatalf("the dialog has no accessible name: %s", sample)
	}
	if !strings.Contains(sample, `<h2 id="`+id[1]+`">`) {
		t.Errorf("aria-labelledby=%q points at no heading in the panel: %s", id[1], sample)
	}
	// Comments stripped first: this file explains ::backdrop and names
	// the reset rule in prose, and a check that cannot tell a selector
	// from a sentence about one is not a check.
	css := cssComment.ReplaceAllString(string(TokensCSS()), "")
	i := strings.Index(css, "dialog.rst-modal-panel")
	if i < 0 {
		t.Fatal("tokens.css has no scoped dialog reset; the UA dialog block would position the panel absolutely")
	}
	rule := css[i:]
	rule = rule[:strings.Index(rule, "}")]
	for _, want := range []string{"position: static", "padding: 0", "margin: 0", "color: inherit"} {
		if !strings.Contains(rule, want) {
			t.Errorf("the dialog reset does not undo the UA block's %q: %s", want, rule)
		}
	}
	// ::backdrop belongs to the top layer, which a rendered-open dialog
	// never reaches. Styling it would be dead CSS that reads as if the
	// scrim came from the element.
	if strings.Contains(css, "::backdrop") {
		t.Error("tokens.css styles ::backdrop; a rendered-open dialog never paints one — the overlay div is the scrim")
	}
}

// The search form is inside a <search> landmark, the element HTML added
// for exactly this, and the toolbar strip sizes the landmark rather than
// the form it now wraps — the direct-child selector that used to reach
// the form no longer does.
func TestSearchFormSitsInASearchLandmark(t *testing.T) {
	got := render(t, "list-bar", map[string]any{"SearchAction": "/posts"})
	if !strings.Contains(got, "<search><form") || !strings.Contains(got, "</form></search>") {
		t.Errorf("the search form is not wrapped in a <search> landmark: %s", got)
	}
	css := string(TokensCSS())
	for _, want := range []string{".rst-lbar > search", ".rst-list > search"} {
		if !strings.Contains(css, want) {
			t.Errorf("tokens.css has no %q rule; wrapping the form broke the layout it used to size", want)
		}
	}
}

func TestDropdownRendersADetailsMenuOfLinks(t *testing.T) {
	got := render(t, "dropdown", map[string]any{
		"Label": "All",
		"Aria":  "Filter by status: All",
		"Items": []any{
			map[string]any{"Href": "/admin/posts", "Label": "All", "Current": true},
			map[string]any{"Href": "/admin/posts?status=draft", "Label": "Drafts"},
		},
	})
	for _, want := range []string{
		`<details class="rst-dropdown" name="rst-menus">`,
		`<summary class="rst-btn rst-dropdown__summary" aria-label="Filter by status: All">All`,
		`<a href="/admin/posts" aria-current="true">All`,
		`<a href="/admin/posts?status=draft">Drafts</a>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
	// The current item is marked twice — attribute and check icon; the
	// non-current one carries neither.
	if !strings.Contains(got, `path d="M20 6 9 17l-5-5"`) {
		t.Errorf("current item lost its check icon: %s", got)
	}
	if strings.Count(got, "aria-current") != 1 {
		t.Errorf("aria-current should mark exactly the current item: %s", got)
	}
}

func TestDropdownMinimalFixture(t *testing.T) {
	got := render(t, "dropdown", map[string]any{
		"Label": "Sort",
		"Items": []any{map[string]any{"Href": "/x", "Label": "Newest"}},
	})
	if strings.Contains(got, "aria-label") {
		t.Errorf("Aria was absent but an aria-label rendered: %s", got)
	}
	if !strings.Contains(got, `path d="m6 9 6 6 6-6"`) {
		t.Errorf("summary lost its disclosure chevron: %s", got)
	}
}

func TestFieldTextMaximalFixture(t *testing.T) {
	got := render(t, "field-text", map[string]any{
		"Name": "title", "Label": "Title", "Value": "Hello", "Type": "text",
		"Required": true, "Hint": "Shown in the list.", "Error": "Title is required.",
		"Autocomplete": "off",
	})
	for _, want := range []string{
		`<div class="rst-field">`,
		`<label class="rst-field__label" for="title">Title`,
		`<span class="rst-field__required" aria-hidden="true">*</span>`,
		`<input class="rst-input" id="title" name="title" type="text" value="Hello" autocomplete="off" required aria-invalid="true" aria-describedby="title-hint title-error">`,
		`<small class="rst-field__hint" id="title-hint">Shown in the list.</small>`,
		`<small class="rst-field__error" id="title-error">Title is required.</small>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
}

func TestFieldTextMinimalFixture(t *testing.T) {
	got := render(t, "field-text", map[string]any{"Name": "q", "Label": "Query"})
	if !strings.Contains(got, `<input class="rst-input" id="q" name="q" type="text">`) {
		t.Errorf("minimal input wrong: %s", got)
	}
	for _, absent := range []string{"aria-describedby", "aria-invalid", "required", "value=", "rst-field__hint", "rst-field__error"} {
		if strings.Contains(got, absent) {
			t.Errorf("%q rendered without its key: %s", absent, got)
		}
	}
}

// aria-describedby lists only ids that exist: hint alone, error alone.
func TestFieldTextDescribedByMatchesRenderedIds(t *testing.T) {
	hintOnly := render(t, "field-text", map[string]any{"Name": "a", "Label": "A", "Hint": "h"})
	if !strings.Contains(hintOnly, `aria-describedby="a-hint"`) {
		t.Errorf("hint-only describedby wrong: %s", hintOnly)
	}
	errOnly := render(t, "field-text", map[string]any{"Name": "a", "Label": "A", "Error": "e"})
	if !strings.Contains(errOnly, `aria-describedby="a-error"`) {
		t.Errorf("error-only describedby wrong: %s", errOnly)
	}
}

func TestFieldTextareaMaximalFixture(t *testing.T) {
	got := render(t, "field-textarea", map[string]any{
		"Name": "body", "Label": "Body", "Value": "Hello\n\nWorld",
		"Rows": 18, "Required": true, "Hint": "Plain text.", "Error": "Too long.",
	})
	for _, want := range []string{
		`<label class="rst-field__label" for="body">Body`,
		`<textarea class="rst-textarea" id="body" name="body" rows="18" required aria-invalid="true" aria-describedby="body-hint body-error">Hello`,
		`<small class="rst-field__hint" id="body-hint">Plain text.</small>`,
		`<small class="rst-field__error" id="body-error">Too long.</small>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
}

func TestFieldTextareaMinimalFixture(t *testing.T) {
	got := render(t, "field-textarea", map[string]any{"Name": "notes", "Label": "Notes"})
	if !strings.Contains(got, `<textarea class="rst-textarea" id="notes" name="notes"></textarea>`) {
		t.Errorf("minimal textarea wrong: %s", got)
	}
	if strings.Contains(got, "rows=") {
		t.Errorf("rows rendered without the key: %s", got)
	}
}

func TestFormFootRendersSubmitAndCancel(t *testing.T) {
	got := render(t, "form-foot", map[string]any{
		"Submit": "Save", "CancelHref": "/admin/posts", "CancelLabel": "Back to posts",
	})
	for _, want := range []string{
		`<div class="rst-form__foot">`,
		`<button class="rst-btn rst-btn--primary" type="submit">Save</button>`,
		`<a class="rst-btn" href="/admin/posts">Back to posts</a>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
}

func TestFormFootMinimalFixture(t *testing.T) {
	got := render(t, "form-foot", map[string]any{"Submit": "Create"})
	if strings.Contains(got, "<a ") {
		t.Errorf("cancel link rendered without CancelHref: %s", got)
	}
}

// F2's second half: the focus ring covers the whole app column, so a
// hand-rolled control inside .rst-page no longer restates the outline.
func TestFocusRingScopeIncludesThePageColumn(t *testing.T) {
	css := string(TokensCSS())
	if !strings.Contains(css, ":where(.rst-page,") {
		t.Error("tokens.css :focus-visible scope does not start with .rst-page")
	}
}

func TestDetailListRendersLabelValueRows(t *testing.T) {
	got := render(t, "detail-list", map[string]any{
		"Items": []any{
			map[string]any{"Label": "Title", "Value": "Hello"},
			map[string]any{"Label": "Price", "Value": "$1.00", "Mono": true},
		},
	})
	for _, want := range []string{
		`<dl class="rst-detail">`,
		`<dt>Title</dt>`, `<dd>Hello</dd>`,
		`<dt>Price</dt>`, `<dd class="rst-mono">$1.00</dd>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
}

func TestDetailListEmptyItemsRendersEmptyList(t *testing.T) {
	got := render(t, "detail-list", map[string]any{"Items": []any{}})
	if !strings.Contains(got, `<dl class="rst-detail">`) || strings.Contains(got, "<dt>") {
		t.Errorf("empty detail-list wrong: %s", got)
	}
}

// job-status emits data-poll (and its spinner) only while running — that
// attribute's presence is how the shim knows to keep polling, and its
// absence is how the shim stops. A done job must never carry it.
func TestJobStatusPollsWhileRunningAndStopsWhenDone(t *testing.T) {
	running := render(t, "job-status", map[string]any{
		"Name": "Export", "Status": "running", "Progress": "42%",
		"PollURL": "/jobs/1/status", "PollSeconds": 2,
	})
	for _, want := range []string{
		`data-poll="/jobs/1/status"`, `data-poll-every="2"`, "rst-spin",
	} {
		if !strings.Contains(running, want) {
			t.Errorf("missing %q: %s", want, running)
		}
	}

	done := render(t, "job-status", map[string]any{"Name": "Export", "Status": "done"})
	if strings.Contains(done, "data-poll") {
		t.Errorf("a done job must not carry data-poll: %s", done)
	}
	if strings.Contains(done, "rst-spin") {
		t.Errorf("a done job must not carry the spinner: %s", done)
	}
}

// PushURL is the partial's opt-in server-push key: set, a running job
// carries data-poll-push beside data-poll; unset, the markup is
// byte-for-byte today's polling; and a job that stopped running
// carries neither — the shim's signal to stop everything.
func TestJobStatusPushURLIsOptIn(t *testing.T) {
	pushed := render(t, "job-status", map[string]any{
		"Name": "Export", "Status": "running",
		"PollURL": "/jobs/1/fragment", "PollSeconds": 2,
		"PushURL": "/jobs/1/events",
	})
	for _, want := range []string{
		`data-poll="/jobs/1/fragment"`, `data-poll-push="/jobs/1/events"`,
	} {
		if !strings.Contains(pushed, want) {
			t.Errorf("missing %q: %s", want, pushed)
		}
	}

	polled := render(t, "job-status", map[string]any{
		"Name": "Export", "Status": "running",
		"PollURL": "/jobs/1/fragment", "PollSeconds": 2,
	})
	if strings.Contains(polled, "data-poll-push") {
		t.Errorf("data-poll-push rendered without PushURL: %s", polled)
	}

	done := render(t, "job-status", map[string]any{
		"Name": "Export", "Status": "done", "PushURL": "/jobs/1/events",
	})
	if strings.Contains(done, "data-poll-push") {
		t.Errorf("a done job must not carry data-poll-push: %s", done)
	}
}

// .rst-sr-only and the component rules it has to beat are all single
// class selectors, so the cascade falls to source order: declared before
// them it loses, and anything carrying both classes ends up a full-width
// absolutely-positioned box hidden only by clip-path. That is how a
// hidden <select> nearly shipped at full width — the utility must come
// last, and this stops it drifting back.
func TestSrOnlyUtilityComesAfterTheRulesItMustBeat(t *testing.T) {
	css := string(TokensCSS())
	srOnly := strings.Index(css, ".rst-sr-only {")
	if srOnly < 0 {
		t.Fatal("tokens.css no longer defines .rst-sr-only")
	}
	for _, competitor := range []string{".rst-input {", ".rst-btn {"} {
		at := strings.Index(css, competitor)
		if at < 0 {
			continue // that component may have been renamed; not this test's business
		}
		if at > srOnly {
			t.Errorf("%s is declared after .rst-sr-only; the utility must come last or it loses the cascade to it", competitor)
		}
	}
}

// A separator inside a menu panel is an <hr>, and a UA <hr> is a thick
// inset 3D rule that looks nothing like the hairline the menus draw. The
// rule that neutralises it existed but was scoped to one panel —
// .rst-row-menu__panel — so the topbar account menu, which is a
// .rst-dropdown__menu, rendered the browser's own. The panels are one
// surface with three entry points, so the rule has to name all three.
//
// Selectors, not substrings: ".rst-dropdown__menu hr" is not satisfied
// by ".rst-dropdown__menu hr" appearing inside some longer selector that
// does not actually match a menu separator.
func TestEveryMenuSurfaceStylesItsSeparator(t *testing.T) {
	styled := map[string]bool{}
	for _, rule := range leafRules(string(TokensCSS())) {
		if !strings.Contains(rule.body, "border-top") {
			continue
		}
		for _, sel := range selectorList(rule.selector) {
			styled[sel] = true
		}
	}
	for _, want := range []string{
		".rst-row-menu__panel hr",
		".rst-dropdown__menu hr",
		".rst-locale hr",
	} {
		if !styled[want] {
			t.Errorf("no rule draws the separator for %q; that menu renders the UA's thick inset <hr>", want)
		}
	}
}

// The sidebar rail's foot: the person block last, the language switcher
// directly above it, and both in a wrapper the LAYOUT owns — the account
// and locale blocks are app-supplied markup, so the shell cannot style
// them by class, only by the box it puts them in. The geometry (pinned
// to the foot, the menu opening upward) is measured on a real engine in
// shell_browser_test.go; this pins the markup the geometry needs.
func TestTheSidebarRailGroupsLocaleAndAccountAtItsFoot(t *testing.T) {
	src, ok := Layout("sidebar")
	if !ok {
		t.Fatal("no sidebar layout")
	}
	s := string(src)
	foot := strings.Index(s, `<div class="rst-shell__rail-foot">`)
	if foot < 0 {
		t.Fatal("layouts/sidebar.html has no rail foot; the profile has nothing to sit at the bottom of")
	}
	nav := strings.Index(s, `<nav class="rst-shell__nav">`)
	if nav < 0 || nav > foot {
		t.Error("the rail's nav does not come before its foot")
	}
	locale := strings.Index(s, `{{block "locale" .}}`)
	account := strings.Index(s, `{{block "account" .}}`)
	if locale < foot || account < foot {
		t.Error("the locale and account blocks are not inside the rail's foot")
	}
	if locale > account {
		t.Error("the account block comes before the locale block; the language switcher belongs directly above the profile")
	}
	// An un-overridden shell renders an empty wrapper, so the foot has
	// to be genuinely empty for :empty to hide it — no whitespace text
	// node between the two blocks. Same discipline as .rst-shell__foot.
	if !strings.Contains(s, `<div class="rst-shell__rail-foot">{{block "locale" .}}{{end}}{{block "account" .}}{{end}}</div>`) {
		t.Error("the rail foot carries whitespace between its blocks; :empty will never match it and an un-overridden rail grows a stray gap")
	}
	if !strings.Contains(string(TokensCSS()), ".rst-shell__rail-foot:empty") {
		t.Error("tokens.css does not hide an empty rail foot")
	}
}

func TestLocaleMenuRenders(t *testing.T) {
	tmpl := template.Must(template.New("").Funcs(Funcs()).ParseFS(Templates(), "*.html"))
	items := []rastrillo.LocaleItem{
		{Code: "en", Name: "English", Href: "/en/orders"},
		{Code: "ga", Name: "Gaeilge", Href: "/ga/orders", Current: true},
	}
	var b strings.Builder
	if err := tmpl.ExecuteTemplate(&b, "locale-menu", map[string]any{"Items": items, "Return": "/orders?page=2"}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		`<details class="rst-dropdown rst-locale"`,
		`action="/_locale"`,
		`name="locale" value="ga"`,
		`name="return" value="/orders?page=2"`,
		`aria-current="true"`,
		`lang="ga"`,
		`>Gaeilge<`,
		`Language`, // the summary, from the base catalog via defaultT
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	b.Reset()
	if err := tmpl.ExecuteTemplate(&b, "locale-menu", map[string]any{"Items": []rastrillo.LocaleItem{}}); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(b.String()) != "" {
		t.Errorf("empty Items must render nothing, got %q", b.String())
	}
}

// Every theme file must declare exactly the same set of --rst-*
// properties as day, the reference theme. A theme that forgets a token
// does not fail loudly at render time — it silently falls back to
// whatever the cascade already had, which in a scaffolded app is
// nothing at all, so the affected element renders unstyled rather than
// wrong. Since v2 that set includes the shape tokens (the radii and the
// four depth values), which used to be tokens.css's and are now part of
// what a theme has to answer for. Names only: values are the theme's
// whole point, and their contrast is contrast_test.go's job.
func TestThemesDeclareIdenticalTokenSets(t *testing.T) {
	names := ThemeNames()
	if len(names) == 0 || names[0] != "day" {
		t.Fatalf("ThemeNames = %v; day must exist and come first", names)
	}
	want := themePropSet(t, "day")
	if len(want) == 0 {
		t.Fatal("day declares no --rst- properties")
	}
	for _, shape := range []string{
		"--rst-radius", "--rst-radius-sm", "--rst-radius-pill",
		"--rst-shadow-pop", "--rst-shadow-knob", "--rst-shadow-lift", "--rst-overlay",
	} {
		if !want[shape] {
			t.Errorf("day does not declare %s; the shape axis is the theme's since v2", shape)
		}
	}
	for _, n := range names[1:] {
		got := themePropSet(t, n)
		for p := range want {
			if !got[p] {
				t.Errorf("theme %s is missing %s", n, p)
			}
		}
		for p := range got {
			if !want[p] {
				t.Errorf("theme %s declares %s, which day does not", n, p)
			}
		}
	}
}

// themePropSet: every --rst-* name declared anywhere in the theme file.
func themePropSet(t *testing.T, name string) map[string]bool {
	t.Helper()
	css, ok := ThemeCSS(name)
	if !ok {
		t.Fatalf("ThemeCSS(%q) missing", name)
	}
	out := map[string]bool{}
	for _, m := range regexp.MustCompile(`(--rst-[a-z0-9-]+)\s*:`).FindAllStringSubmatch(string(css), -1) {
		out[m[1]] = true
	}
	return out
}

func TestTokensCSSHasNoColourLiterals(t *testing.T) {
	// After the split, structural tokens.css may reference colours only
	// via var(); bare hex in a *declaration value* means a colour leaked
	// back in. Exempt: none — the three rgba() literals that used to live
	// here (the switch knob shadow, the seg-tab lift shadow, the modal
	// scrim) are gone too, replaced with var(--rst-shadow-knob),
	// var(--rst-shadow-lift) and var(--rst-overlay) declared per theme —
	// so this gate also matches rgba()/rgb()/hsl() function values, not
	// just bare hex, to keep that claim enforced instead of just stated.
	decl := regexp.MustCompile(`:\s*[^;]*(#[0-9a-fA-F]{3,6}|rgba?\(|hsl\()`)
	for i, line := range strings.Split(string(TokensCSS()), "\n") {
		if (strings.Contains(line, "#") || strings.Contains(line, "rgb") || strings.Contains(line, "hsl")) && decl.MatchString(line) {
			t.Errorf("tokens.css line %d declares a colour literal: %s", i+1, strings.TrimSpace(line))
		}
	}
}

// The three shells are real templates, not documentation: each one
// parses with the ui funcs, defines "layout", executes against nil data
// (so a struct-vs-map decision in an app cannot break a shell), and
// resolves every catalog key it names.
func TestLayoutsParseAndRender(t *testing.T) {
	if got := LayoutNames(); !reflect.DeepEqual(got, []string{"column", "topbar", "sidebar"}) {
		t.Fatalf("LayoutNames = %v", got)
	}
	for _, name := range LayoutNames() {
		t.Run(name, func(t *testing.T) {
			src, ok := Layout(name)
			if !ok {
				t.Fatal("missing")
			}
			tmpl := template.Must(template.New("layout").Funcs(Funcs()).Funcs(template.FuncMap{
				"asset":      func(p string) string { return "/" + p },
				"iconAssets": func() template.HTML { return "" },
			}).Parse(string(src)))
			template.Must(tmpl.Parse(`{{define "content"}}CONTENT-SENTINEL{{end}}`))
			var b strings.Builder
			if err := tmpl.ExecuteTemplate(&b, "layout", nil); err != nil {
				t.Fatal(err)
			}
			out := b.String()
			for _, want := range []string{"CONTENT-SENTINEL", "static/theme.css", "static/tokens.css", `id="main"`, "Skip to content"} {
				if !strings.Contains(out, want) {
					t.Errorf("%s: missing %q", name, want)
				}
			}
			if strings.Contains(out, "rastrillo.ui.") {
				t.Errorf("%s: an unresolved catalog key leaked into the page", name)
			}
		})
	}
}

// Every shell leaves the app a slot in <head>. It is the one extension
// point a page frame cannot do without and the three shells did not
// have: a favicon, an Open Graph tag, one more stylesheet, an inline
// script that has to run before the body. Without it the only way to
// add any of those was to stop using ui.Layout at all.
//
// Last in the head on purpose, and asserted so: an app's own
// stylesheet has to come after tokens.css and the theme, or it loses
// every tie it should win.
func TestEveryShellLeavesTheAppASlotInTheHead(t *testing.T) {
	for _, name := range LayoutNames() {
		src, ok := Layout(name)
		if !ok {
			t.Fatalf("Layout(%q) missing", name)
		}
		s := string(src)
		i := strings.Index(s, `{{block "head" .}}{{end}}`)
		if i < 0 {
			t.Errorf("layouts/%s.html declares no head block — an app has nowhere to put a favicon", name)
			continue
		}
		head := strings.Index(s, "</head>")
		if head < 0 || i > head {
			t.Errorf("layouts/%s.html's head block is not inside <head>", name)
			continue
		}
		if last := strings.LastIndex(s[:head], "<link"); last > i {
			t.Errorf("layouts/%s.html loads a stylesheet after the head block; an app's own CSS would lose the tie", name)
		}
	}
}

// The error page renders one of the five statuses the base catalog
// words, falls back to the generic wording for anything else, and never
// invents navigation the app did not give it.
func TestErrorPageWordsTheFiveKnownStatuses(t *testing.T) {
	for status, want := range map[int]string{
		// Escaped as html/template writes them: the apostrophes in the
		// catalog's English come out as &#39; in the page.
		404: "We can&#39;t find that page",
		403: "You can&#39;t see this",
		422: "That didn&#39;t go through",
		500: "Something went wrong on our side",
		503: "We&#39;re briefly unavailable",
	} {
		got := render(t, "error-page", map[string]any{"Status": status})
		if !strings.Contains(got, want) {
			t.Errorf("status %d: title %q missing from:\n%s", status, want, got)
		}
		if !strings.Contains(got, `<p class="rst-error__status">`+fmt.Sprint(status)+`</p>`) {
			t.Errorf("status %d: the status itself is not shown:\n%s", status, got)
		}
		if strings.Contains(got, "rastrillo.ui.") {
			t.Errorf("status %d: an unresolved catalog key leaked into the page:\n%s", status, got)
		}
	}
}

// A status the catalog has no wording for must not render the key
// itself: the partial guards it back to the generic pair.
func TestErrorPageFallsBackToGenericWording(t *testing.T) {
	got := render(t, "error-page", map[string]any{"Status": 418})
	for _, want := range []string{"Something&#39;s not right", "That request couldn&#39;t be completed", ">418<"} {
		if !strings.Contains(got, want) {
			t.Errorf("418 page missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "error_418") || strings.Contains(got, "rastrillo.ui.") {
		t.Errorf("418 page leaked a catalog key:\n%s", got)
	}
}

// Caller-supplied Title/Body always win over the catalog, as everywhere
// else in this package.
func TestErrorPageCallerWordingWins(t *testing.T) {
	got := render(t, "error-page", map[string]any{
		"Status": 404, "Title": "No such order", "Body": "That order was cancelled.",
	})
	if !strings.Contains(got, "No such order") || !strings.Contains(got, "That order was cancelled.") {
		t.Errorf("caller wording lost:\n%s", got)
	}
	if strings.Contains(got, "We can't find that page") {
		t.Errorf("catalog default rendered alongside caller wording:\n%s", got)
	}
}

// The reference is a run of LTR alphanumerics inside a sentence that may
// itself be RTL, so its paragraph carries dir="auto" — the whole of the
// bidi fix, and the reason the ref is not pre-wrapped by the caller.
func TestErrorPageReferenceLine(t *testing.T) {
	got := render(t, "error-page", map[string]any{"Status": 500, "Ref": "k3f9tq"})
	if !strings.Contains(got, `<p class="rst-error__ref rst-mono" dir="auto">Reference: k3f9tq</p>`) {
		t.Errorf("reference line is not interpolated with dir=auto:\n%s", got)
	}
	if strings.Contains(got, "{ref}") {
		t.Errorf("the {ref} placeholder survived interpolation:\n%s", got)
	}
	if none := render(t, "error-page", map[string]any{"Status": 500}); strings.Contains(none, "rst-error__ref") {
		t.Errorf("a page with no Ref still rendered the reference line:\n%s", none)
	}
}

// Back renders only when the app supplies a href it has validated; the
// partial never reaches for javascript:history.back().
func TestErrorPageBackIsOptionalAndNeverJavascript(t *testing.T) {
	with := render(t, "error-page", map[string]any{"Status": 404, "BackHref": "/orders"})
	if !strings.Contains(with, `href="/orders"`) || !strings.Contains(with, "Go back") {
		t.Errorf("BackHref did not render a back link:\n%s", with)
	}
	without := render(t, "error-page", map[string]any{"Status": 404})
	if strings.Contains(without, "Go back") {
		t.Errorf("back link rendered with no BackHref:\n%s", without)
	}
	for _, got := range []string{with, without} {
		if strings.Contains(got, "javascript:") {
			t.Errorf("the partial emitted a javascript: URL:\n%s", got)
		}
	}
	// The home CTA is always there, defaulting to the site root.
	if !strings.Contains(without, `href="/"`) || !strings.Contains(without, "Start page") {
		t.Errorf("home CTA missing or not defaulted:\n%s", without)
	}
	if custom := render(t, "error-page", map[string]any{"Status": 404, "HomeHref": "/app"}); !strings.Contains(custom, `href="/app"`) {
		t.Errorf("HomeHref ignored:\n%s", custom)
	}
}

// The same drift check the other partial families get: every class the
// error page emits has a rule in tokens.css.
func TestErrorPageClassesAreStyled(t *testing.T) {
	css := string(TokensCSS())
	for _, class := range []string{
		"rst-error", "rst-error__status", "rst-error__title",
		"rst-error__body", "rst-error__cta", "rst-error__ref",
	} {
		if !strings.Contains(css, "."+class) {
			t.Errorf("tokens.css has no selector for %q", class)
		}
	}
}

// ── The date and time fields ──────────────────────────────────────────

// dateVocabulary is the seventeen parser words the enhancement needs,
// short name → base catalog key suffix. It is the same list dateWords
// builds, spelled out again here so a silent drop in funcs.go fails.
var dateVocabulary = []string{
	"today", "tomorrow", "yesterday", "next", "last", "in", "ago", "at",
	"day", "week", "month", "hour", "minute", "noon", "midnight", "am", "pm",
}

var wordsAttrRe = regexp.MustCompile(`data-rst-date-words="([^"]*)"`)

// wordsAttr decodes the words attribute the way a browser does: html/template
// escapes the JSON's quotes into entities on the way out, getAttribute
// unescapes them, and JSON.parse reads the result. html.UnescapeString plus
// json.Unmarshal is that round trip.
func wordsAttr(t *testing.T, markup string) map[string]string {
	t.Helper()
	m := wordsAttrRe.FindStringSubmatch(markup)
	if m == nil {
		t.Fatalf("no data-rst-date-words attribute in:\n%s", markup)
	}
	var words map[string]string
	if err := json.Unmarshal([]byte(html.UnescapeString(m[1])), &words); err != nil {
		t.Fatalf("data-rst-date-words is not JSON after unescaping (%v): %s", err, m[1])
	}
	return words
}

// The three singular fields are native inputs first: the type carries the
// value, the enhancement rides on data attributes beside it.
func TestDateFieldsRenderNativeInputs(t *testing.T) {
	for _, c := range []struct{ partial, typ, flag string }{
		{"field-date", "date", "data-rst-date"},
		{"field-time", "time", "data-rst-time"},
		{"field-datetime", "datetime-local", "data-rst-date"},
	} {
		got := render(t, c.partial, fixtureFor(t, c.partial))
		fixture := fixtureFor(t, c.partial)
		name, _ := fixture["Name"].(string)
		for _, want := range []string{
			`<div class="rst-field">`,
			`<label class="rst-field__label" for="` + name + `">`,
			`type="` + c.typ + `"`,
			`id="` + name + `"`, `name="` + name + `"`,
			`value="` + fixture["Value"].(string) + `"`,
			`min="` + fixture["Min"].(string) + `"`,
			`max="` + fixture["Max"].(string) + `"`,
			" required", ` aria-invalid="true"`,
			" " + c.flag + " ",
			`data-rst-date-set="Set"`,
			// The hint rides out as its RAW template. datetime.js
			// substitutes {example} with a date its own Intl formatter
			// wrote, in the page's locale — filling it in here would
			// need Tf, and a rebound T that ignores its arguments (which
			// rastrillo.T(r, key) is) would print the placeholder at a
			// person.
			`data-rst-date-hint="Try: {example}"`,
			`data-rst-date-pick="Open the calendar"`,
			`data-rst-date-results="{n} suggestions"`,
			`data-rst-date-result-one="1 suggestion"`,
			`data-rst-date-quick-today="Today"`,
			`data-rst-date-quick-tomorrow="Tomorrow"`,
			`data-rst-date-quick-next-week="In a week"`,
			`data-rst-date-quick-plus-1h="An hour later"`,
			`data-rst-date-quick-plus-2h="Two hours later"`,
			`data-rst-date-quick-end-of-day="End of that day"`,
			`data-rst-date-quick-next-day="Same time next day"`,
		} {
			if !strings.Contains(got, want) {
				t.Errorf("%s is missing %q:\n%s", c.partial, want, got)
			}
		}
		// The arming flag is one attribute, and only one: the display
		// attributes are data-rst-date-* on all three fields (they are the
		// same catalog strings), so this looks for the bare flag with
		// spaces either side rather than for the prefix.
		other := map[string]string{"data-rst-date": "data-rst-time", "data-rst-time": "data-rst-date"}[c.flag]
		if strings.Contains(got, " "+other+" ") {
			t.Errorf("%s carries %q as well as %q:\n%s", c.partial, other, c.flag, got)
		}
	}
}

// The words attribute is one JSON object holding the whole parser
// vocabulary, localised through the bound T.
func TestDateFieldsCarryTheVocabularyAsJSON(t *testing.T) {
	for _, partial := range []string{"field-date", "field-time", "field-datetime"} {
		words := wordsAttr(t, render(t, partial, fixtureFor(t, partial)))
		if len(words) != len(dateVocabulary) {
			t.Errorf("%s: words attribute has %d entries, want %d", partial, len(words), len(dateVocabulary))
		}
		for _, short := range dateVocabulary {
			want := rastrillo.BaseCatalog()["rastrillo.ui.date_"+short]
			if want == "" {
				t.Fatalf("base catalog has no rastrillo.ui.date_%s", short)
			}
			if words[short] != want {
				t.Errorf("%s: words[%q] = %q, want %q", partial, short, words[short], want)
			}
		}
		// Vocabulary keys carry their alternative spellings; the split
		// happens browser-side, so the pipes must survive the attribute.
		if !strings.Contains(words["day"], "|") {
			t.Errorf("%s: words[\"day\"] = %q, want the |-separated spellings intact", partial, words["day"])
		}
	}
}

// A rebound T localises the whole attribute set, words included — the
// per-request seam FuncsWith documents.
func TestDateFieldsLocaliseThroughABoundT(t *testing.T) {
	tmpl := template.Must(template.New("").Funcs(FuncsWith(func(key string, _ ...any) string {
		return "X-" + key
	})).ParseFS(Templates(), "*.html"))
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "field-date", fixtureFor(t, "field-date")); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, `data-rst-date-set="X-rastrillo.ui.date_set"`) {
		t.Errorf("display attribute did not rebind: %s", got)
	}
	if words := wordsAttr(t, got); words["today"] != "X-rastrillo.ui.date_today" {
		t.Errorf("words attribute did not rebind: %q", words["today"])
	}
}

// Plain is the escape hatch: the native input, nothing else. Not one
// data-rst attribute, so the script never sees the field.
func TestDateFieldsPlainEmitNoDataAttributes(t *testing.T) {
	for _, partial := range []string{"field-date", "field-time", "field-datetime"} {
		fixture := map[string]any{}
		for k, v := range fixtureFor(t, partial) {
			fixture[k] = v
		}
		fixture["Plain"] = true
		got := render(t, partial, fixture)
		if strings.Contains(got, "data-rst") {
			t.Errorf("%s with Plain emitted a data-rst attribute:\n%s", partial, got)
		}
		// Everything else survives: Plain drops the enhancement, not the field.
		if !strings.Contains(got, `value="`+fixture["Value"].(string)+`"`) {
			t.Errorf("%s with Plain lost its value:\n%s", partial, got)
		}
	}
}

// Hint and error wiring is field-text's, repeated: ids derived from Name,
// aria-describedby listing exactly what renders, aria-invalid with an error.
func TestDateFieldsWireHintAndError(t *testing.T) {
	for _, partial := range []string{"field-date", "field-time", "field-datetime"} {
		both := render(t, partial, map[string]any{
			"Name": "when", "Label": "When", "Hint": "Any time today.", "Error": "Pick one.",
		})
		for _, want := range []string{
			`aria-describedby="when-hint when-error"`, ` aria-invalid="true"`,
			`<small class="rst-field__hint" id="when-hint">Any time today.</small>`,
			`<small class="rst-field__error" id="when-error">Pick one.</small>`,
		} {
			if !strings.Contains(both, want) {
				t.Errorf("%s is missing %q:\n%s", partial, want, both)
			}
		}
		hintOnly := render(t, partial, map[string]any{"Name": "when", "Label": "When", "Hint": "H"})
		if !strings.Contains(hintOnly, `aria-describedby="when-hint"`) || strings.Contains(hintOnly, "when-error") {
			t.Errorf("%s: hint-only describedby is wrong:\n%s", partial, hintOnly)
		}
		errOnly := render(t, partial, map[string]any{"Name": "when", "Label": "When", "Error": "E"})
		if !strings.Contains(errOnly, `aria-describedby="when-error"`) || strings.Contains(errOnly, "when-hint") {
			t.Errorf("%s: error-only describedby is wrong:\n%s", partial, errOnly)
		}
		bare := render(t, partial, map[string]any{"Name": "when", "Label": "When"})
		for _, gone := range []string{"aria-describedby", "aria-invalid", "rst-field__hint", "rst-field__error", " required", " min=", " max=", " value="} {
			if strings.Contains(bare, gone) {
				t.Errorf("%s emitted %q with nothing to put in it:\n%s", partial, gone, bare)
			}
		}
	}
}

// The required marker is presentation on top of the required attribute,
// never a substitute for it.
func TestDateFieldsRequiredMarkerIsAriaHidden(t *testing.T) {
	for _, partial := range []string{"field-date", "field-time", "field-datetime"} {
		got := render(t, partial, map[string]any{"Name": "when", "Label": "When", "Required": true})
		if !strings.Contains(got, `<span class="rst-field__required" aria-hidden="true">*</span>`) {
			t.Errorf("%s: required marker missing or exposed:\n%s", partial, got)
		}
	}
}

// field-daterange is a labelled pair: one fieldset, one legend, the two
// singular partials side by side in a row the script can find.
func TestFieldDaterangeWrapsTwoFields(t *testing.T) {
	got := render(t, "field-daterange", fixtureFor(t, "field-daterange"))
	for _, want := range []string{
		"<fieldset", "<legend", ">When</legend>",
		`<div class="rst-field-row" data-rst-range="session">`,
		`name="starts_at"`, `name="ends_at"`,
		`type="datetime-local"`,
		`<small class="rst-field__error" id="ends_at-error">The end comes before the start.</small>`,
		`aria-describedby="ends_at-error"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("field-daterange is missing %q:\n%s", want, got)
		}
	}
	// Both halves get the enhancement, so either can be typed into.
	if n := strings.Count(got, "data-rst-date-words="); n != 2 {
		t.Errorf("field-daterange enhanced %d halves, want 2:\n%s", n, got)
	}
}

// Seed is optional: without it the marker attribute is still there (the
// script uses it to pair the two inputs), just with no seeding rule.
func TestFieldDaterangeSeedIsOptional(t *testing.T) {
	got := render(t, "field-daterange", map[string]any{
		"Legend": "When",
		"Start":  map[string]any{"Name": "a", "Label": "From"},
		"End":    map[string]any{"Name": "b", "Label": "To"},
	})
	if !strings.Contains(got, `<div class="rst-field-row" data-rst-range>`) {
		t.Errorf("unseeded range lost its marker attribute:\n%s", got)
	}
}

// Kind picks the halves' input type — a whole-day range is two date
// fields, not two datetime-locals.
func TestFieldDaterangeKindDate(t *testing.T) {
	got := render(t, "field-daterange", map[string]any{
		"Legend": "When", "Kind": "date",
		"Start": map[string]any{"Name": "a", "Label": "From"},
		"End":   map[string]any{"Name": "b", "Label": "To"},
	})
	if n := strings.Count(got, `type="date"`); n != 2 {
		t.Errorf("Kind \"date\" produced %d date inputs, want 2:\n%s", n, got)
	}
	if strings.Contains(got, "datetime-local") {
		t.Errorf("Kind \"date\" still rendered a datetime-local input:\n%s", got)
	}
}

// F10's lesson, applied to the new group: the class the partial emits and
// the selector tokens.css styles are one string, so a rename cannot leave
// the fieldset with the browser's default border and nobody notice.
func TestDaterangeFieldsetIsStyled(t *testing.T) {
	got := render(t, "field-daterange", fixtureFor(t, "field-daterange"))
	if !strings.Contains(got, `<fieldset class="rst-field-range">`) {
		t.Errorf("daterange fieldset lost its class:\n%s", got)
	}
	if !strings.Contains(string(TokensCSS()), ".rst-field-range {") {
		t.Error("tokens.css does not style .rst-field-range")
	}
}

// A hidden legend is still a legend: the group keeps its accessible name
// when the visible heading would be noise.
func TestFieldDaterangeLegendCanBeHidden(t *testing.T) {
	got := render(t, "field-daterange", map[string]any{
		"Legend": "When", "LegendHidden": true,
		"Start": map[string]any{"Name": "a", "Label": "From"},
		"End":   map[string]any{"Name": "b", "Label": "To"},
	})
	if !strings.Contains(got, `<legend class="rst-sr-only">When</legend>`) {
		t.Errorf("LegendHidden did not hide the legend:\n%s", got)
	}
}
