package ui

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"testing"
)

// This file is a WCAG 2.2 AA contrast gate over tokens.css's *declared*
// custom-property values — the pairs a component's markup names directly
// (e.g. "color: var(--rst-text); background: var(--rst-bg)").
//
// LIMITATION, stated explicitly because it is easy to forget: this checks
// token pairs, not the resolved cascade. It parses each theme's :root
// block and computes contrast between the hex values two custom
// properties are *declared* as — it does not parse selectors, does not
// know which element pairs which background in the actual DOM, and does
// not see specificity, :hover, or any other cascade effect. A token can
// pass every check here and still render at the wrong contrast once a
// browser resolves the real cascade: tokens.css's own comment on
// .rst-btn--danger:hover documents exactly that happening — an earlier,
// higher-specificity .rst-btn:hover rule silently won the label colour
// back to the accent (~1.05:1 on the red fill) at the exact moment a
// user commits to a destructive action, and no token-level check would
// ever have caught it, because both --rst-tone-negative-fg and
// --rst-on-accent were, individually, still declared at a passing ratio
// the whole time. That bug was only caught by reading the rendered
// cascade by hand. Treat a green run of this file as "the palette is
// internally consistent," never as "every rendered pixel passes."

// colorMixSkip lists custom properties whose *value* uses a CSS function
// this test cannot evaluate (color-mix(), light-dark(), etc.) — computing
// those is out of scope, so they are named here, explicitly, rather than
// silently skipped by a parse failure. Empty today: no token in
// tokens.css uses color-mix()/light-dark() as of Task 5. If one is added,
// name it here with a comment instead of weakening the parser to accept
// it silently.
var colorMixSkip = map[string]bool{
	// (none yet)
}

// hexPattern matches a bare #rgb or #rrggbb custom-property value, the
// only colour syntax tokens.css uses today (no rgba(), no color-mix()).
var hexPattern = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// declPattern matches one custom-property declaration: "--rst-name:
// value;". Values never contain a semicolon in this file (no font stacks
// with quoted commas-and-semicolons, no data URIs), so splitting on ';'
// is safe here even though it would not be for arbitrary CSS.
var declPattern = regexp.MustCompile(`(--rst-[a-z0-9-]+)\s*:\s*([^;]+);`)

// parseTokens extracts every --rst-* declaration in body into a map. Only
// the last declaration of a given name wins, matching CSS's own "later
// wins" rule for same-specificity declarations in one block — not that
// tokens.css redeclares a name twice in the same block today.
func parseTokens(body string) map[string]string {
	out := map[string]string{}
	for _, m := range declPattern.FindAllStringSubmatch(body, -1) {
		out[m[1]] = strings.TrimSpace(m[2])
	}
	return out
}

// blockBody returns the brace-matched contents following header (which
// must itself end in "{"), starting the search at css[from:]. tokens.css
// nests at most one level (the prefers-color-scheme media query wrapping
// a :root block), so simple depth counting is enough — no CSS string
// literals here contain '{' or '}'.
func blockBody(t *testing.T, css, header string, from int) string {
	t.Helper()
	i := strings.Index(css[from:], header)
	if i < 0 {
		t.Fatalf("tokens.css structure changed: %q not found from offset %d", header, from)
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
	t.Fatalf("tokens.css structure changed: unterminated block for %q", header)
	return ""
}

// themeTokens reads the three theme blocks tokens.css declares (light,
// dark-by-OS, dark-by-toggle — the same structure
// TestBothThemesDeclareEveryColourToken checks token names against) and
// returns each as its own name→value map, keyed for test output.
func themeTokens(t *testing.T) map[string]map[string]string {
	t.Helper()
	css := string(TokensCSS())

	light := parseTokens(blockBody(t, css, `:root[data-theme="light"] {`, 0))

	mediaBody := blockBody(t, css, `@media (prefers-color-scheme: dark) {`, 0)
	darkOS := parseTokens(blockBody(t, mediaBody, `:root {`, 0))

	darkToggle := parseTokens(blockBody(t, css, `:root[data-theme="dark"] {`, 0))

	return map[string]map[string]string{
		"light (:root / [data-theme=light])": light,
		"dark (prefers-color-scheme)":        darkOS,
		"dark ([data-theme=dark])":           darkToggle,
	}
}

// srgbToLinear converts one sRGB channel (0..1) to its linearized form —
// the standard WCAG 2.x formula (also used by the WCAG 2.2 spec this
// project targets).
func srgbToLinear(c float64) float64 {
	if c <= 0.03928 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// relLuminance is the WCAG relative luminance of an sRGB colour.
func relLuminance(r, g, b uint8) float64 {
	rl := srgbToLinear(float64(r) / 255)
	gl := srgbToLinear(float64(g) / 255)
	bl := srgbToLinear(float64(b) / 255)
	return 0.2126*rl + 0.7152*gl + 0.0722*bl
}

// parseHex reads a #rgb or #rrggbb literal. Anything else (var(),
// color-mix(), rgba(), a bare name) is reported so a caller can route it
// through colorMixSkip instead of guessing.
func parseHex(s string) (r, g, b uint8, err error) {
	if !hexPattern.MatchString(s) {
		return 0, 0, 0, fmt.Errorf("not a #rgb/#rrggbb literal: %q", s)
	}
	h := s[1:]
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	var v [3]uint8
	for i := 0; i < 3; i++ {
		n, err := parseHexByte(h[i*2 : i*2+2])
		if err != nil {
			return 0, 0, 0, err
		}
		v[i] = n
	}
	return v[0], v[1], v[2], nil
}

func parseHexByte(s string) (uint8, error) {
	var n int
	if _, err := fmt.Sscanf(s, "%02x", &n); err != nil {
		return 0, err
	}
	return uint8(n), nil
}

// contrastRatio is the WCAG contrast ratio between two hex colours:
// (lighter + 0.05) / (darker + 0.05) over relative luminance.
func contrastRatio(fgHex, bgHex string) (float64, error) {
	fr, fg, fb, err := parseHex(fgHex)
	if err != nil {
		return 0, fmt.Errorf("fg: %w", err)
	}
	br, bgc, bb, err := parseHex(bgHex)
	if err != nil {
		return 0, fmt.Errorf("bg: %w", err)
	}
	lf := relLuminance(fr, fg, fb)
	lb := relLuminance(br, bgc, bb)
	lighter, darker := lf, lb
	if lb > lf {
		lighter, darker = lb, lf
	}
	return (lighter + 0.05) / (darker + 0.05), nil
}

// TestContrastMathMatchesDocumentedDangerFillRatios sanity-checks this
// file's WCAG arithmetic against numbers tokens.css's own comment already
// published and hand-verified (the .rst-btn--danger comment, Task 4):
// --rst-on-accent on --rst-tone-negative-fg, both themes. If this test
// ever fails, suspect the formula in this file before suspecting the
// published ratios.
func TestContrastMathMatchesDocumentedDangerFillRatios(t *testing.T) {
	for _, tt := range []struct {
		name     string
		fg, bg   string
		wantLow  float64 // the comment's published value, rounded to 2dp; allow ±0.02 for rounding
		wantHigh float64
	}{
		{"light: on-accent on tone-negative-fg", "#ffffff", "#93262f", 8.19, 8.23},
		{"dark: on-accent on tone-negative-fg", "#1a1030", "#f58c95", 7.79, 7.83},
	} {
		got, err := contrastRatio(tt.fg, tt.bg)
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if got < tt.wantLow || got > tt.wantHigh {
			t.Errorf("%s: contrastRatio(%s, %s) = %.2f, want in [%.2f, %.2f] (tokens.css's published ratio)", tt.name, tt.fg, tt.bg, got, tt.wantLow, tt.wantHigh)
		}
	}
}

// TestThemeTokenContrastMeetsWCAG is the real gate: the full pair table
// tokens.css's own "WCAG 2.2 AA, measured" header comment documents
// (§ near the top of tokens.css), enforced at each row's documented
// floor, in every authored theme. See the file doc comment above for
// exactly what this does and does not verify.
//
// --rst-text-faint is held to the same 4.5:1 body-text floor as
// --rst-text/--rst-text-muted, not AA's lower 3:1 large-text/graphic
// floor: this branch puts it on normal-size, normal-weight text
// (.rst-count-line, .rst-field__hint) and on .rst-lrow--head (11.5px,
// well under WCAG's ~18pt/14pt-bold "large text" threshold even with its
// letter-spacing and uppercase transform), so 3:1 would be the wrong bar
// for what this token is actually used for. tokens.css's own header
// table already publishes it at 4.5:1 throughout (its tightest pair,
// 4.67:1 in light on --rst-bg) — this test previously asserted only one
// --rst-text-faint pair at a 3:1 floor, which is loose enough that a
// real regression (a token change bringing that pair down to, say,
// 3.52:1) would still pass; raising it to 4.5:1, and adding the other
// three --rst-text-faint pairs the header table documents, closes that
// gap.
func TestThemeTokenContrastMeetsWCAG(t *testing.T) {
	type pair struct {
		fg, bg string
		min    float64
		why    string
	}
	// Transcribed from tokens.css's own "WCAG 2.2 AA, measured" header
	// table plus the .rst-btn--danger comment's documented pair (not in
	// the main table, since it reuses --rst-tone-negative-fg as a solid
	// fill rather than declaring a dedicated --rst-danger-* token) — every
	// row that table publishes, at its documented floor. If tokens.css's
	// header table grows a row, add it here too; the two are meant to
	// stay in lockstep.
	pairs := []pair{
		{"--rst-text", "--rst-surface", 4.5, "body text on a card"},
		{"--rst-text", "--rst-bg", 4.5, "body text"},
		{"--rst-text", "--rst-surface-2", 4.5, "body text on a card"},
		{"--rst-text", "--rst-accent-soft", 4.5, "body text on an accent-tinted surface"},
		{"--rst-text-muted", "--rst-surface", 4.5, "muted text on a card"},
		{"--rst-text-muted", "--rst-bg", 4.5, "muted text"},
		{"--rst-text-muted", "--rst-surface-2", 4.5, "muted text on a card"},
		{"--rst-text-muted", "--rst-accent-soft", 4.5, "muted text on an accent-tinted surface"},
		{"--rst-text-faint", "--rst-surface", 4.5, "faint text on a card (.rst-field__hint)"},
		{"--rst-text-faint", "--rst-bg", 4.5, "faint text — the tightest pair in the set (4.67 light / 6.17 dark)"},
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
		// Not in tokens.css's main header table — documented separately in
		// the .rst-btn--danger comment (Task 4), which reuses
		// --rst-tone-negative-fg as a solid fill rather than declaring a
		// dedicated --rst-danger-* pair. This is the real pair the danger
		// button's label renders against, so it belongs in the gate anyway.
		{"--rst-on-accent", "--rst-tone-negative-fg", 4.5, "danger button label"},
	}

	for themeName, tokens := range themeTokens(t) {
		t.Run(themeName, func(t *testing.T) {
			for _, p := range pairs {
				if colorMixSkip[p.fg] || colorMixSkip[p.bg] {
					t.Logf("skipping %s on %s: listed in colorMixSkip", p.fg, p.bg)
					continue
				}
				fgVal, ok := tokens[p.fg]
				if !ok {
					t.Errorf("%s: token %s is not declared in this theme", themeName, p.fg)
					continue
				}
				bgVal, ok := tokens[p.bg]
				if !ok {
					t.Errorf("%s: token %s is not declared in this theme", themeName, p.bg)
					continue
				}
				ratio, err := contrastRatio(fgVal, bgVal)
				if err != nil {
					t.Errorf("%s: %s (%s) on %s (%s): %v — add to colorMixSkip if this is a color-mix()/light-dark() value", themeName, p.fg, fgVal, p.bg, bgVal, err)
					continue
				}
				if ratio < p.min {
					t.Errorf("%s: %s (%s) on %s (%s) = %.2f:1, want >= %.1f:1 (%s)", themeName, p.fg, fgVal, p.bg, bgVal, ratio, p.min, p.why)
				}
			}
		})
	}
}
