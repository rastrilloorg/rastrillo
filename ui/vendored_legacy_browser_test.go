//go:build browser

package ui

import (
	"context"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"amadan.net/rastrillo/rastrillo/harness"
)

// TestLightJSOnAnOldThemeWritesOntoNothingItPaints (C2, review focus
// 3): with the new tokens.css, light.js and the v0.28 day theme, every
// property the script writes lands on a token the old theme never
// painted, so the pixels are the old pair's after a pointer move too,
// and the harness sees no console error.
func TestLightJSOnAnOldThemeWritesOntoNothingItPaints(t *testing.T) {
	oldTokens, oldTheme := legacyFile(t, "tokens-v028.css"), legacyFile(t, "day-v028.css")
	rig := legacyRig(t, oldTokens, oldTheme, TokensCSS(), oldTheme, true)
	ctx, cancel := context.WithTimeout(rig.Context(), 120*time.Second)
	defer cancel()
	shots := map[string][2][]byte{}
	for i, dir := range []string{"/old/", "/new/"} {
		mustRun(t, ctx, chromedp.EmulateViewport(1280, 1400), chromedp.Navigate(rig.Origin+dir), chromedp.WaitVisible("#field", chromedp.ByQuery),
			chromedp.Evaluate(`new Promise(r => addEventListener("load", r, {once: true}) || document.readyState === "complete" && r())`, nil, awaitPromise))
		bx, by, _, _ := rect(t, ctx, "#box")
		move(t, ctx, 1270, 400)
		move(t, ctx, bx+40, by+20)
		time.Sleep(1200 * time.Millisecond)
		still(t, ctx)
		for _, sel := range []string{"#box", "#card", "#b-primary", "#b-plain", "#sw"} {
			s := shots[sel]
			s[i] = snap(t, ctx, sel)
			shots[sel] = s
		}
	}
	for sel, s := range shots {
		if ok, why := samePixels(t, s[0], s[1]); !ok {
			t.Errorf("%s renders differently with light.js on the old theme: %s", sel, why)
		}
	}
}

// TestOldTokensWithANewThemeStillRenders (C16's reverse pair): an app
// that re-copied theme.css and kept its v0.28 tokens.css renders with
// the old rules and the new colours: the primary button is the new
// accent under white, the card a plain hairline, no console error (the
// harness fails one), in both schemes.
func TestOldTokensWithANewThemeStillRenders(t *testing.T) {
	day, _ := ThemeCSS("day")
	oldTokens := legacyFile(t, "tokens-v028.css")
	rig := legacyRig(t, oldTokens, day, oldTokens, day, true)
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	for _, scheme := range []string{"light", "dark"} {
		mustRun(t, ctx, chromedp.EmulateViewport(1280, 1400), chromedp.Navigate(rig.Origin+"/new/"), chromedp.WaitVisible("#field", chromedp.ByQuery),
			chromedp.Evaluate(`document.documentElement.setAttribute("data-theme", "`+scheme+`"), true`, nil))
		still(t, ctx)
		wantFill, wantFg := "rgb(31, 86, 214)", "rgb(255, 255, 255)"
		if scheme == "dark" {
			wantFill, wantFg = "rgb(122, 162, 255)", "rgb(12, 13, 15)"
		}
		if bg, fg := computed(t, ctx, "#b-primary", "background-color"), computed(t, ctx, "#b-primary", "color"); bg != wantFill || fg != wantFg {
			t.Errorf("%s: old tokens.css paints the primary button %s on %s with the new theme; want %s on %s (the accent and on-accent, which is all the old rules read)", scheme, fg, bg, wantFg, wantFill)
		}
		if img := computed(t, ctx, "#b-primary", "background-image"); img != "none" {
			t.Errorf("%s: old tokens.css painted a background-image %q on the primary button; it has no face rule", scheme, img)
		}
		move(t, ctx, 1270, 400)
		time.Sleep(300 * time.Millisecond)
	}
}

// TestAnAppWithLightJSDeletedIsTheStaticDesign: the shells' tag
// removed, no script, every light property at its default after a move.
func TestAnAppWithLightJSDeletedIsTheStaticDesign(t *testing.T) {
	rig := sizingRig(t, false, depthDoc(t), harness.WithHover())
	ctx, cancel := context.WithTimeout(rig.Context(), 60*time.Second)
	defer cancel()
	open(t, ctx, rig, "/", false)
	move(t, ctx, 1270, 400)
	time.Sleep(300 * time.Millisecond)
	if x, lit, px := computed(t, ctx, ":root", "--rst-light-x"), computed(t, ctx, "#box", "--rst-card-lit"), computed(t, ctx, "#b-primary", "--rst-px"); x != "0" || lit != "0" || px != "50%" {
		t.Errorf("without light.js: light-x %s card-lit %s px %s; want 0, 0, 50%%", x, lit, px)
	}
}
