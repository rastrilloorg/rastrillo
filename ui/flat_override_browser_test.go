//go:build browser

package ui

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// appOverrides is an app stylesheet that rebrands by setting the
// semantic tokens on :root, the way docs/site/design-system.md says to:
// the accent and its strong and soft forms, the two surfaces, the two
// lines and the negative tone. Every value is far from what plain and
// signal declare in either scheme, so a token that kept a stock colour
// instead of following its semantic token shows as a pixel difference.
const appOverrides = `<style>:root {
  --rst-accent: light-dark(#b4005a, #ff7ab8);
  --rst-accent-strong: light-dark(#7d003e, #ffb3d6);
  --rst-accent-soft: light-dark(#ffe1ee, #3b1026);
  --rst-surface: light-dark(#fff7e3, #1d1a11);
  --rst-surface-2: light-dark(#f2e7cb, #2a251a);
  --rst-line: light-dark(#d6c184, #514830);
  --rst-line-strong: light-dark(#86753a, #ab9c5c);
  --rst-tone-negative-fg: light-dark(#6a1b9a, #d7a4ff);
}</style>`

// TestFlatThemesFollowAnAppsOverrides is Astra A2 (A15 for an app that
// customised): under the v0.28 pair, an app's override of --rst-accent
// reached a plain button's hovered text and border, --rst-accent-soft
// reached the hover tints and the current nav item, --rst-surface the
// control faces and the switch's knob, --rst-surface-2 the linked rows,
// --rst-line the card's hairline, --rst-line-strong the control borders,
// the off track and the focused field's edge, and --rst-tone-negative-fg
// the danger button, because tokens.css read those tokens at those
// sites. The new tokens read a new token at each site, and plain and
// signal declared each one as a copy of the stock colour, so the
// override stopped reaching them while the primary button still
// followed it. This is the matrix again, with appOverrides on both
// pages: every crop under the new pair must match the old pair's,
// for plain and signal, in both schemes.
func TestFlatThemesFollowAnAppsOverrides(t *testing.T) {
	oldTokens := legacyFile(t, "tokens-v028.css")
	for _, theme := range []string{"plain", "signal"} {
		t.Run(theme, func(t *testing.T) {
			oldTheme := legacyFile(t, theme+"-v028.css")
			newTheme, ok := ThemeCSS(theme)
			if !ok {
				t.Fatalf("no theme %q", theme)
			}
			rig := legacyRig(t, oldTokens, oldTheme, TokensCSS(), newTheme, false, appOverrides)
			ctx, cancel := context.WithTimeout(rig.Context(), 300*time.Second)
			defer cancel()
			for _, scheme := range []string{"light", "dark"} {
				old := matrixShots(t, ctx, rig.Origin, "/old/", scheme)
				now := matrixShots(t, ctx, rig.Origin, "/new/", scheme)
				// The override reached the page: the primary button is
				// the app's accent, or the comparison is between two
				// stock renders and proves nothing.
				want := "rgb(180, 0, 90)"
				if scheme == "dark" {
					want = "rgb(255, 122, 184)"
				}
				if got := computed(t, ctx, "#b-primary", "background-color"); got != want {
					t.Fatalf("%s: the primary button is %s, want the app's accent %s; the override did not reach the page", scheme, got, want)
				}
				keys := make([]string, 0, len(old))
				for key := range old {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				// 40 per channel, not the matrix's 24: the only pixels
				// left differing after the aliases are the rounded
				// corners the matrix comment describes (A9), 44 to 88
				// per card, measured at up to 32 with this hairline
				// (#d6c184 on #fff7e3) where the stock pair's measured
				// 9 to 16. A token that kept a stock colour is a whole
				// face or border off, which the 1% count rule catches:
				// before the aliases, 70 of 95 crops failed in light.
				var differ []string
				for _, key := range keys {
					if ok, why := samePixelsWithin(t, old[key], now[key], 40); !ok {
						differ = append(differ, key+": "+why)
					}
				}
				if len(differ) > 0 {
					t.Errorf("%s: with the app's overrides, %d of %d crops render differently under the new pair than under the old:\n  %s", scheme, len(differ), len(keys), strings.Join(differ, "\n  "))
				}
			}
			// Keep the page open for the fatal above: a rig closes with
			// the test, and a navigation after it is the next scheme's.
			mustRun(t, ctx, chromedp.Evaluate(`true`, nil))
		})
	}
}
