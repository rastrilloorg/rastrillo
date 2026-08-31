package ui

import (
	"regexp"
	"strings"
	"testing"
)

// This file is a WCAG 2.2 AA contrast gate over every shipped theme's
// *declared* custom-property values — the pairs a component's markup
// names directly (e.g. "color: var(--rst-text); background:
// var(--rst-bg)"). tokens.css declares no colour of its own; the values
// under test all live in themes/<name>.css.
//
// Since the v2 themes each collapsed to one :root block, every token is
// declared once as light-dark(<light>, <dark>) and this file splits that
// call back into two per-scheme tables, so a theme is gated in light and
// in dark from a single set of declarations. A token whose value is not
// a light-dark() call (the font stack, the radii, a shadow whose two
// schemes are identical) is carried into both tables unchanged.
//
// The WCAG arithmetic itself is not here: contrast ratios come from
// ui.ContrastRatio in colour.go, which the colour engine is built on
// too, so this gate and the engine cannot drift apart.
//
// LIMITATION, stated explicitly because it is easy to forget: this checks
// token pairs, not the resolved cascade. It parses each theme's :root
// block and computes contrast between the hex values two custom
// properties are *declared* as — it does not parse selectors, does not
// know which element pairs which background in the actual DOM, and does
// not see specificity, :hover, or any other cascade effect. A token can
// pass every check here and still render at the wrong contrast once a
// browser resolves the real cascade: tokens.css's own comment on
// [rst-btn~="danger"]:hover documents exactly that happening — an earlier,
// higher-specificity [rst-btn]:hover rule silently won the label colour
// back to the accent (~1.05:1 on the red fill) at the exact moment a
// user commits to a destructive action, and no token-level check would
// ever have caught it, because both --rst-tone-negative-fg and
// --rst-on-accent were, individually, still declared at a passing ratio
// the whole time. That bug was only caught by reading the rendered
// cascade by hand. Treat a green run of this file as "the palette is
// internally consistent," never as "every rendered pixel passes."

// colorMixSkip lists custom properties whose *value* uses a CSS function
// this test cannot evaluate (color-mix(), a relative-colour syntax) —
// computing those is out of scope, so they are named here, explicitly,
// rather than silently skipped by a parse failure. light-dark() is no
// longer such a function: splitLightDark below evaluates it, which is
// the whole point of the v2 format. Empty today. If a theme grows a
// value this parser cannot read, name it here with a comment instead of
// weakening the parser to accept it silently.
var colorMixSkip = map[string]bool{
	// (none yet)
	//
	// --rst-header-rule is NOT listed here, and its absence is the
	// point: skipping a token implies it belongs in the pair table and
	// could not be evaluated. The header rule does not belong in the
	// table at all. day and signal derive it with color-mix(), which
	// this parser cannot read — and it is never asked to, because no
	// pair below names it. See the note above the pair table.
}

// declPattern matches one custom-property declaration: "--rst-name:
// value;". Values never contain a semicolon in this file (no font stacks
// with quoted commas-and-semicolons, no data URIs), so splitting on ';'
// is safe here even though it would not be for arbitrary CSS.
var declPattern = regexp.MustCompile(`(--rst-[a-z0-9-]+)\s*:\s*([^;]+);`)

// parseTokens extracts every --rst-* declaration in body into a map. Only
// the last declaration of a given name wins, matching CSS's own "later
// wins" rule for same-specificity declarations in one block — not that
// a theme file redeclares a name twice in the same block today.
func parseTokens(body string) map[string]string {
	out := map[string]string{}
	for _, m := range declPattern.FindAllStringSubmatch(body, -1) {
		out[m[1]] = strings.TrimSpace(m[2])
	}
	return out
}

// splitLightDark evaluates a whole-value light-dark(<light>, <dark>)
// call, returning the two halves. It reports false for anything else —
// including a value that merely *contains* a light-dark() somewhere
// inside it, which is exactly what the shadow tokens look like
// ("0 8px 24px light-dark(rgba(…), rgba(…))"): those are not colours in
// their own right, are not in the pair table, and are left to both
// schemes verbatim rather than half-parsed.
//
// The split is paren-aware because both halves are usually rgba() calls
// full of commas of their own.
func splitLightDark(v string) (light, dark string, ok bool) {
	const prefix = "light-dark("
	if !strings.HasPrefix(v, prefix) || !strings.HasSuffix(v, ")") {
		return "", "", false
	}
	inner := v[len(prefix) : len(v)-1]
	depth, comma := 0, -1
	for i := 0; i < len(inner); i++ {
		switch inner[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return "", "", false // the closing ")" we trimmed was not ours
			}
		case ',':
			if depth == 0 {
				if comma >= 0 {
					return "", "", false // three arguments is not light-dark()
				}
				comma = i
			}
		}
	}
	if depth != 0 || comma < 0 {
		return "", "", false
	}
	return strings.TrimSpace(inner[:comma]), strings.TrimSpace(inner[comma+1:]), true
}

// blockBody returns the brace-matched contents following header (which
// must itself end in "{"), starting the search at css[from:]. Simple
// depth counting is enough — no CSS string literal in a theme file
// contains '{' or '}'. where names the file, so a structure failure says
// which theme drifted.
func blockBody(t *testing.T, where, css, header string, from int) string {
	t.Helper()
	i := strings.Index(css[from:], header)
	if i < 0 {
		t.Fatalf("%s structure changed: %q not found from offset %d", where, header, from)
	}
	start := from + i + len(header)
	depth := 1
	for j := start; j < len(css); j++ {
		switch css[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return css[start:j]
			}
		}
	}
	t.Fatalf("%s structure changed: unterminated block for %q", where, header)
	return ""
}

// themeTokens reads the one :root block a v2 theme declares and returns
// it twice: once resolved for the light scheme, once for dark. The
// header ":root {" cannot match the two toggle rules at the bottom of the
// file, whose selectors are ":root[data-theme=…] {".
func themeTokens(t *testing.T, theme string) map[string]map[string]string {
	t.Helper()
	raw, ok := ThemeCSS(theme)
	if !ok {
		t.Fatalf("ThemeCSS(%q) missing", theme)
	}
	css := string(raw)
	where := "themes/" + theme + ".css"

	declared := parseTokens(blockBody(t, where, css, ":root {", 0))
	if len(declared) == 0 {
		t.Fatalf("%s declares no --rst- properties in its :root block", where)
	}

	light := make(map[string]string, len(declared))
	dark := make(map[string]string, len(declared))
	for name, v := range declared {
		if l, d, ok := splitLightDark(v); ok {
			light[name], dark[name] = l, d
			continue
		}
		light[name], dark[name] = v, v
	}
	return map[string]map[string]string{"light": light, "dark": dark}
}

// TestSplitLightDarkReadsTheV2Format pins the one piece of parsing the
// whole gate rests on. A splitter that quietly stopped recognising
// light-dark() would not fail anything — it would hand both schemes the
// same unsplit string, every hex parse would fail, and the pair table
// would report "add to colorMixSkip" instead of a contrast number. These
// cases say what the format is, including the two shapes that must NOT
// split: a shadow that merely contains a light-dark(), and a plain
// single value.
func TestSplitLightDarkReadsTheV2Format(t *testing.T) {
	for _, tt := range []struct {
		in           string
		wantL, wantD string
		wantOK       bool
	}{
		{"light-dark(#ffffff, #111418)", "#ffffff", "#111418", true},
		{"light-dark(rgba(0, 0, 0, 0.45), rgba(0, 0, 0, 0.6))", "rgba(0, 0, 0, 0.45)", "rgba(0, 0, 0, 0.6)", true},
		{"0 8px 24px light-dark(rgba(0, 0, 0, 0.12), rgba(0, 0, 0, 0.5))", "", "", false},
		{"0 1px 2px rgba(0, 0, 0, 0.3)", "", "", false},
		{"8px", "", "", false},
		{"light-dark(#fff)", "", "", false},
		{"light-dark(#fff, #000, #111)", "", "", false},
	} {
		gotL, gotD, ok := splitLightDark(tt.in)
		if ok != tt.wantOK || gotL != tt.wantL || gotD != tt.wantD {
			t.Errorf("splitLightDark(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tt.in, gotL, gotD, ok, tt.wantL, tt.wantD, tt.wantOK)
		}
	}
}

// TestContrastMathMatchesDocumentedDangerFillRatios calibrates the WCAG
// arithmetic against numbers tokens.css's own comment already published
// and hand-verified (the [rst-btn~="danger"] comment — the one colour
// commentary that stayed with the component rule): --rst-on-accent on
// --rst-tone-negative-fg, both schemes of the default theme. If this test
// ever fails, suspect the formula before suspecting the published ratios.
//
// The arithmetic it calibrates is ui.ContrastRatio in colour.go — shipped
// code, not a test helper. There is one implementation of the WCAG
// formula in this package: this gate, the colour engine and any app
// gating its own colours all measure with the same one, so these two
// hand-verified ratios calibrate all three.
func TestContrastMathMatchesDocumentedDangerFillRatios(t *testing.T) {
	for _, tt := range []struct {
		name     string
		fg, bg   string
		wantLow  float64 // the comment's published value, rounded to 2dp; allow ±0.02 for rounding
		wantHigh float64
	}{
		{"light: on-accent on tone-negative-fg", "#ffffff", "#b91c1c", 6.45, 6.49},
		{"dark: on-accent on tone-negative-fg", "#0b1220", "#f79aa0", 8.97, 9.01},
	} {
		got, err := ContrastRatio(tt.fg, tt.bg)
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if got < tt.wantLow || got > tt.wantHigh {
			t.Errorf("%s: ContrastRatio(%s, %s) = %.2f, want in [%.2f, %.2f] (tokens.css's published ratio)", tt.name, tt.fg, tt.bg, got, tt.wantLow, tt.wantHigh)
		}
	}
}

// TestThemeTokenContrastMeetsWCAG is the real gate: the full pair table
// each theme's own "WCAG 2.2 AA, measured" header comment documents,
// enforced at each row's documented floor, in every shipped theme and in
// BOTH schemes. See the file doc comment above for exactly what this does
// and does not verify.
//
// --rst-text-faint is held to the same 4.5:1 body-text floor as
// --rst-text/--rst-text-muted, not AA's lower 3:1 large-text/graphic
// floor: this branch puts it on normal-size, normal-weight text
// ([rst-count-line], [rst-field-hint]) and on [rst-lrow~="head"] (11.5px,
// well under WCAG's ~18pt/14pt-bold "large text" threshold even with its
// letter-spacing and uppercase transform), so 3:1 would be the wrong bar
// for what this token is actually used for.
func TestThemeTokenContrastMeetsWCAG(t *testing.T) {
	type pair struct {
		fg, bg string
		min    float64
		why    string
	}
	// Transcribed from each theme's own "WCAG 2.2 AA, measured" header
	// table plus the [rst-btn~="danger"] comment's documented pair (not in
	// the main table, since it reuses --rst-tone-negative-fg as a solid
	// fill rather than declaring a dedicated --rst-danger-* token) — every
	// row those tables publish, at its documented floor. If a header
	// table grows a row, add it here too; the two are meant to stay in
	// lockstep.
	//
	// --rst-header-rule is deliberately absent. The page header's rule
	// is decorative and carries no contrast floor (design doc §6-v2.2):
	// the heading's size, weight and spacing carry the structure, the
	// rule does not, so 1.4.11's 3:1 was never its bar. day and signal
	// tint it with the accent at 18% and 45%, which on a light surface
	// is under 3:1 by construction. That is not a defect and must not be
	// "fixed" by adding a row here — the ruling says so in writing
	// precisely so this argument has already been answered.
	pairs := []pair{
		{"--rst-text", "--rst-surface", 4.5, "body text on a card"},
		{"--rst-text", "--rst-bg", 4.5, "body text"},
		{"--rst-text", "--rst-surface-2", 4.5, "body text on a card"},
		{"--rst-text", "--rst-accent-soft", 4.5, "body text on an accent-tinted surface"},
		{"--rst-text-muted", "--rst-surface", 4.5, "muted text on a card"},
		{"--rst-text-muted", "--rst-bg", 4.5, "muted text"},
		{"--rst-text-muted", "--rst-surface-2", 4.5, "muted text on a card"},
		{"--rst-text-muted", "--rst-accent-soft", 4.5, "muted text on an accent-tinted surface"},
		{"--rst-text-faint", "--rst-surface", 4.5, "faint text on a card ([rst-field-hint])"},
		{"--rst-text-faint", "--rst-bg", 4.5, "faint text"},
		{"--rst-text-faint", "--rst-surface-2", 4.5, "faint text on a card"},
		{"--rst-text-faint", "--rst-accent-soft", 4.5, "faint text on an accent-tinted surface"},
		{"--rst-accent", "--rst-surface", 4.5, "text + focus ring"},
		{"--rst-accent", "--rst-bg", 4.5, "text + focus ring"},
		{"--rst-accent", "--rst-surface-2", 4.5, "text"},
		{"--rst-accent", "--rst-accent-soft", 4.5, "text"},
		{"--rst-on-accent", "--rst-accent", 4.5, "primary button label"},
		{"--rst-on-accent", "--rst-accent-strong", 4.5, "primary button label, hover"},
		{"--rst-line-strong", "--rst-surface", 3.0, "control border (1.4.11 boundary, not text)"},
		{"--rst-line-strong", "--rst-bg", 3.0, "control border"},
		{"--rst-line-strong", "--rst-surface-2", 3.0, "control border"},
		{"--rst-tone-neutral-fg", "--rst-tone-neutral-bg", 4.5, "status pill text"},
		{"--rst-tone-positive-fg", "--rst-tone-positive-bg", 4.5, "status pill text"},
		{"--rst-tone-warning-fg", "--rst-tone-warning-bg", 4.5, "status pill text"},
		{"--rst-tone-negative-fg", "--rst-tone-negative-bg", 4.5, "status pill text"},
		// Not in the main header tables — documented separately in
		// tokens.css's [rst-btn~="danger"] comment, which reuses
		// --rst-tone-negative-fg as a solid fill rather than declaring a
		// dedicated --rst-danger-* pair. This is the real pair the danger
		// button's label renders against, so it belongs in the gate anyway.
		{"--rst-on-accent", "--rst-tone-negative-fg", 4.5, "danger button label"},
	}

	for _, theme := range ThemeNames() {
		for _, scheme := range []string{"light", "dark"} {
			tokens := themeTokens(t, theme)[scheme]
			t.Run(theme+"/"+scheme, func(t *testing.T) {
				for _, p := range pairs {
					if colorMixSkip[p.fg] || colorMixSkip[p.bg] {
						t.Logf("skipping %s on %s: listed in colorMixSkip", p.fg, p.bg)
						continue
					}
					fgVal, ok := tokens[p.fg]
					if !ok {
						t.Errorf("token %s is not declared in this theme", p.fg)
						continue
					}
					bgVal, ok := tokens[p.bg]
					if !ok {
						t.Errorf("token %s is not declared in this theme", p.bg)
						continue
					}
					ratio, err := ContrastRatio(fgVal, bgVal)
					if err != nil {
						t.Errorf("%s (%s) on %s (%s): %v — add to colorMixSkip if this is a value the parser cannot evaluate", p.fg, fgVal, p.bg, bgVal, err)
						continue
					}
					if ratio < p.min {
						t.Errorf("%s (%s) on %s (%s) = %.2f:1, want >= %.1f:1 (%s)", p.fg, fgVal, p.bg, bgVal, ratio, p.min, p.why)
					}
				}
			})
		}
	}
}
